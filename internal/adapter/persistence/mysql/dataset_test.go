package mysql

import (
	"context"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"depeg.local/platform/internal/adapter/logging"
	"depeg.local/platform/internal/config"
	"depeg.local/platform/internal/domain"
	"depeg.local/platform/internal/domain/dataset"
	"depeg.local/platform/internal/domain/market"
	"depeg.local/platform/internal/port"
	"depeg.local/platform/migrations"
	"github.com/glebarez/sqlite"
	driver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
)

func TestDatasetImportAgainstSQLite(t *testing.T) {
	raw, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "dataset.sqlite")), &gorm.Config{Logger: gormlog.Default.LogMode(gormlog.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.AutoMigrate(SchemaModels()...); err != nil {
		t.Fatal(err)
	}
	if err := raw.Exec("CREATE TABLE data_versions (name VARCHAR(64) PRIMARY KEY, version BIGINT NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}
	if err := raw.Exec("INSERT INTO data_versions (name, version) VALUES ('assets', 1)").Error; err != nil {
		t.Fatal(err)
	}
	db, err := FromGORM(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	verifyImportContract(t, raw, db)
	verifyCollectorImportContract(t, raw, db)
}

func verifyCollectorImportContract(t *testing.T, raw *gorm.DB, db Database) {
	t.Helper()
	ctx := context.Background()
	repo := NewMarketRepository(db)
	asset := market.Asset{ID: "ccxt.usdc-usdt", Symbol: "USDC", QuoteCurrency: "USDT"}
	open := time.Date(2023, 2, 1, 0, 0, 0, 0, time.UTC)
	candle := market.Candle{AssetID: asset.ID, Source: "ccxt:okx", Interval: time.Minute, Time: open, Open: "1", High: "1.01", Low: "0.99", Close: "1", Quality: "historical"}
	write := func(a market.Asset, candles ...market.Candle) error {
		return NewDatasetRepository(db).Import(ctx, func(sink port.DatasetSink) error {
			return sink.Write(ctx, dataset.Batch{Asset: &a, Candles: candles})
		})
	}
	if _, found, err := repo.LatestOpenTime(ctx, asset.ID, candle.Source, candle.Interval); err != nil || found {
		t.Fatalf("expected empty cursor: found=%v err=%v", found, err)
	}
	otherSource, otherInterval := candle, candle
	otherSource.Source, otherSource.Time = "paper-csv:okx", open.Add(5*time.Minute)
	otherInterval.Interval, otherInterval.Time = 5*time.Minute, open.Add(10*time.Minute)
	if err := write(asset, candle, otherSource, otherInterval); err != nil {
		t.Fatal(err)
	}
	var saved assetModel
	if err := raw.Where("id = ?", asset.ID).Take(&saved).Error; err != nil || saved.AnchorPrice != nil || saved.QuoteCurrency != "USDT" {
		t.Fatalf("new asset did not retain NULL anchor and USDT quote: %+v, %v", saved, err)
	}
	if err := raw.Model(&assetModel{}).Where("id = ?", asset.ID).
		Updates(map[string]any{"symbol": "CUSTOM", "chain": "ethereum", "token_address": "0xverified", "anchor_price": "1.01"}).Error; err != nil {
		t.Fatal(err)
	}
	candle.Close = "1.01"
	if err := write(asset, candle); err != nil {
		t.Fatal(err)
	}
	if err := raw.Where("id = ?", asset.ID).Take(&saved).Error; err != nil || saved.Symbol != "CUSTOM" ||
		saved.Chain != "ethereum" || saved.TokenAddress != "0xverified" || saved.AnchorPrice == nil {
		t.Fatalf("replay changed asset settings: %+v, %v", saved, err)
	}
	anchor, ok := new(big.Rat).SetString(*saved.AnchorPrice)
	expected, _ := new(big.Rat).SetString("1.01")
	if !ok || anchor.Cmp(expected) != 0 {
		t.Fatalf("replay changed the verified anchor: %s", *saved.AnchorPrice)
	}
	var rows []candleModel
	if err := raw.Where("asset_id = ? AND source = ? AND interval_seconds = ?", asset.ID, candle.Source, 60).Find(&rows).Error; err != nil || len(rows) != 1 {
		t.Fatalf("replay should update a single candle: %v, %v", rows, err)
	}
	price, ok := new(big.Rat).SetString(rows[0].Close)
	if !ok || price.Cmp(expected) != 0 {
		t.Fatalf("replay did not apply the correction: %s", rows[0].Close)
	}
	wrongQuote := asset
	wrongQuote.QuoteCurrency = "USD"
	candle.Time = open.Add(time.Minute)
	if err := write(wrongQuote, candle); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected quote conflict: %v", err)
	}
	got, found, err := repo.LatestOpenTime(ctx, asset.ID, candle.Source, candle.Interval)
	if err != nil || !found || !got.Equal(open) || got.Location() != time.UTC {
		t.Fatalf("cursor advanced across sources, intervals or a rejected import: %s, %v, %v", got, found, err)
	}
}

func verifyImportContract(t *testing.T, raw *gorm.DB, db Database) {
	t.Helper()
	ctx := context.Background()
	repository := NewDatasetRepository(db)
	day := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	a := market.Asset{ID: "coingecko.test-usd", Symbol: "TEST", QuoteCurrency: "USD"}
	closed := day.Add(time.Hour)
	batch := dataset.Batch{Asset: &a, Candles: []market.Candle{{AssetID: a.ID, Source: "coingecko:ohlc", Time: day, Interval: time.Hour, Open: "1", High: "1", Low: "1", Close: "1", Volume: nil, Quality: "historical", SourceTime: &closed, RawValues: `{"volume":null}`}}, Snapshots: []market.Snapshot{{AssetID: a.ID, Source: "coingecko:market-chart", Time: day, PriceUSD: "0.000251164999055137", MarketCapUSD: nil, QualityFlags: []string{"market_cap_usd_invalid"}, RawValues: `{"market_cap_usd":"-1"}`}}, News: []dataset.News{{ID: strings.Repeat("a", 64), URL: "https://example.com/a", Title: "Article", PublishedAt: day, FetchedAt: day, Type: "news", Authors: []string{}, CoinIDs: []string{"test-usd"}, RawJSON: `{"title":"Article"}`}}}
	for range 2 {
		if err := repository.Import(ctx, func(sink port.DatasetSink) error { return sink.Write(ctx, batch) }); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range []string{"assets", "candles", "market_snapshots", "news_articles"} {
		var count int64
		if err := raw.Table(table).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("idempotency failed for %s: count=%d err=%v", table, count, err)
		}
	}
	var candle candleModel
	if err := raw.Take(&candle).Error; err != nil || candle.Volume != nil || candle.SourceTime == nil || !candle.SourceTime.Equal(closed) {
		t.Fatalf("nullable volume/time provenance lost: %+v %v", candle, err)
	}
	q := market.Query{AssetID: a.ID, From: day, To: day.Add(3 * 24 * time.Hour), Interval: 24 * time.Hour, Limit: 1}
	series, err := NewMarketRepository(db).Snapshots(ctx, q)
	if err != nil || len(series.Points) != 1 || series.Points[0].MarketCapUSD != nil {
		t.Fatalf("snapshot read failed: %+v %v", series, err)
	}
	price, ok := new(big.Rat).SetString(series.Points[0].PriceUSD)
	want, _ := new(big.Rat).SetString("0.000251164999055137")
	if !ok || price.Cmp(want) != 0 {
		t.Fatalf("storage precision lost: %s", series.Points[0].PriceUSD)
	}
	if err := raw.Table("assets").Where("id = ?", a.ID).Update("anchor_price", "1").Error; err != nil {
		t.Fatal(err)
	}
	batch.Snapshots[0].PriceUSD = "1.01"
	if err := repository.Import(ctx, func(sink port.DatasetSink) error { return sink.Write(ctx, batch) }); err != nil {
		t.Fatal(err)
	}
	var saved assetModel
	if err := raw.Where("id = ?", a.ID).Take(&saved).Error; err != nil || saved.AnchorPrice == nil {
		t.Fatal("reimport erased verified anchor")
	}
	// A second source at the same timestamp must remain visible across pages.
	extra := batch.Snapshots[0]
	extra.Source = "another-source"
	later := batch.Snapshots[0]
	later.Time = day.Add(24 * time.Hour)
	if err := repository.Import(ctx, func(sink port.DatasetSink) error {
		return sink.Write(ctx, dataset.Batch{Snapshots: []market.Snapshot{extra, later}})
	}); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	firstCursor := ""
	for page := 0; page < 4; page++ {
		series, err = NewMarketRepository(db).Snapshots(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		for _, point := range series.Points {
			key := point.Time.String() + point.Source
			if seen[key] {
				t.Fatal("duplicate page")
			}
			seen[key] = true
		}
		if firstCursor == "" {
			firstCursor = series.NextCursor
		}
		if series.NextCursor == "" {
			break
		}
		q.Cursor = series.NextCursor
	}
	if len(seen) != 3 {
		t.Fatalf("pagination lost rows: %d", len(seen))
	}
	q.Cursor = firstCursor
	q.Source = "another-source"
	if _, err := NewMarketRepository(db).Snapshots(ctx, q); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("cursor scope was not checked")
	}
	// Inject a real persistence error after the first successful batch.
	if err := raw.Callback().Create().Before("gorm:create").Register("fail_news", func(tx *gorm.DB) {
		if tx.Statement.Table == "news_articles" {
			tx.AddError(errors.New("injected write failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer raw.Callback().Create().Remove("fail_news")
	other := a
	other.ID = "coingecko.rollback"
	err = repository.Import(ctx, func(sink port.DatasetSink) error {
		if err := sink.Write(ctx, dataset.Batch{Asset: &other}); err != nil {
			return err
		}
		return sink.Write(ctx, dataset.Batch{News: batch.News})
	})
	if err == nil {
		t.Fatal("write failure committed")
	}
	var count int64
	if err := raw.Table("assets").Where("id = ?", other.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("earlier archive batch was not rolled back")
	}
}

// Set only to a disposable test database. Never use a developer's application DB.
func TestMySQLMigrationResumeAndImport(t *testing.T) {
	dsn := os.Getenv("DEPEG_MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("isolated MySQL test DSN not configured")
	}
	parsed, err := driver.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(parsed.DBName, "depeg_import_test_") {
		t.Fatal("MySQL integration tests require a disposable depeg_import_test_* database")
	}
	db, err := New(config.MySQL{DSN: dsn, MaxOpenConns: 2, MaxIdleConns: 1, ConnMaxLifetime: time.Minute}, logging.New(config.Log{Level: "error"}))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	raw, err := db.Session(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var existing int64
	if err := raw.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE()").Scan(&existing).Error; err != nil {
		t.Fatalf("connect isolated MySQL: %v", err)
	}
	if existing != 0 {
		t.Fatal("integration database must initially be empty")
	}
	initial, err := migrations.Files.ReadFile("000001_initial.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	lines := []string{}
	for _, line := range strings.Split(string(initial), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			lines = append(lines, line)
		}
	}
	for _, sql := range strings.Split(strings.Join(lines, "\n"), ";") {
		if strings.TrimSpace(sql) != "" {
			if err := raw.Exec(sql).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := raw.Exec("CREATE TABLE schema_migrations (version VARCHAR(128) PRIMARY KEY, applied_at DATETIME(3) NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}
	if err := raw.Exec("INSERT INTO schema_migrations VALUES ('000001_initial.up.sql',UTC_TIMESTAMP(3))").Error; err != nil {
		t.Fatal(err)
	}
	// Simulate a prior failure after this first column was committed by MySQL.
	if err := raw.Exec("ALTER TABLE candles ADD COLUMN source_time DATETIME(3) NULL").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal("repeat migration failed", err)
	}
	verifyImportContract(t, raw, db)
	verifyCollectorImportContract(t, raw, db)
	verifyMonitorContract(t, raw, db)
	verifyConcurrentStreamWrites(t, db)
}

func verifyConcurrentStreamWrites(t *testing.T, db Database) {
	t.Helper()
	ctx := context.Background()
	asset := market.Asset{ID: "concurrent-usd", Symbol: "RACE", QuoteCurrency: "USD"}
	repo := NewMarketRepository(db)
	if err := repo.Import(ctx, asset, nil); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	candle := market.Candle{AssetID: asset.ID, Source: "Fixture", Interval: time.Minute,
		Open: "1", High: "1", Low: "1", Close: "1", Quality: "test"}
	candle.Time = start.Add(600 * time.Microsecond)
	release := make(chan struct{})
	done := make(chan error, 2)
	for _, offset := range []time.Duration{-time.Minute, time.Minute} {
		go func() {
			<-release
			unique := candle
			unique.Time = start.Add(offset)
			done <- repo.UpsertCandles(ctx, []market.Candle{candle, unique})
		}()
	}
	close(release)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	latest := candle
	latest.Time = start.Add(time.Minute)
	latest.Close = "0.98"
	latest.Source = "fixture"
	if err := repo.UpsertCandles(ctx, []market.Candle{latest, latest}); err != nil {
		t.Fatal(err)
	}
	catalog, err := NewMonitorRepository(db).Catalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range catalog {
		if entry.Asset.ID != asset.ID {
			continue
		}
		if len(entry.Streams) != 1 {
			t.Fatalf("expected one stream: %+v", entry.Streams)
		}
		stream := entry.Streams[0]
		if stream.Source != candle.Source || stream.Count != 3 ||
			!stream.FirstAt.Equal(start.Add(-time.Minute)) || !stream.LastAt.Equal(latest.Time) ||
			!strings.HasPrefix(stream.Latest.Price, "0.98") {
			t.Fatalf("incorrect summary after concurrent inserts and replay: %+v", stream)
		}
		return
	}
	t.Fatal("concurrently imported asset missing from catalog")
}
