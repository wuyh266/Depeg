package mysql

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"depeg.local/platform/internal/domain"
	"depeg.local/platform/internal/domain/market"
	"depeg.local/platform/internal/domain/monitor"
	"depeg.local/platform/internal/identity"
	marketservice "depeg.local/platform/internal/service/market"
	monitorservice "depeg.local/platform/internal/service/monitor"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
)

func TestMonitorAtomicWritesAndRuleVersion(t *testing.T) {
	raw, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "monitor.sqlite")), &gorm.Config{Logger: gormlog.Default.LogMode(gormlog.Silent)})
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
	defer db.Close()
	verifyMonitorContract(t, raw, db)
}

func verifyMonitorContract(t *testing.T, raw *gorm.DB, db Database) {
	t.Helper()
	ctx := context.Background()
	user := "monitor-contract-user"
	if err := raw.Create(&userModel{ID: user, Username: user, PasswordHash: "test-only"}).Error; err != nil {
		t.Fatal(err)
	}
	a := market.Asset{ID: "monitor-contract-usd", Symbol: "MTEST", QuoteCurrency: "USD"}
	start := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := NewMarketRepository(db).Import(ctx, a, []market.Candle{{AssetID: a.ID, Source: "test-contract", Time: start, Interval: time.Hour, Open: "1", High: "1", Low: "0.99", Close: "0.99", Quality: "test"}}); err != nil {
		t.Fatal(err)
	}
	repo := NewMonitorRepository(db)
	service := monitorservice.New(repo, marketservice.New(NewMarketRepository(db), nil, nil))
	rule, err := service.SaveRule(ctx, user, monitor.Rule{AssetID: a.ID, Kind: "ohlc", Source: "test-contract", IntervalSeconds: 3600, AnchorPrice: "1", WarningBPS: 50, CriticalBPS: 100})
	if err != nil {
		t.Fatal(err)
	}
	overview, err := service.Overview(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range overview.Assets {
		if v.Asset.ID == a.ID {
			found = true
			if v.Status != "critical" || v.Selected.Count != 1 || v.Rule == nil {
				t.Fatalf("SQL catalog %+v", v)
			}
		}
	}
	if !found {
		t.Fatal("asset missing from SQL catalog")
	}
	events := make([]monitor.Event, 201)
	for i := range events {
		at := start.Add(time.Duration(i) * time.Hour)
		events[i] = monitor.Event{ID: identity.Digest(fmt.Sprint(i)), UserID: user, AssetID: a.ID, Kind: rule.Kind, Source: rule.Source, IntervalSeconds: rule.IntervalSeconds, Time: at, ObservedAt: at.Add(time.Hour), Price: "0.99", AnchorPrice: rule.AnchorPrice, DeviationBPS: "-100.000000", Severity: "critical", Direction: "below", WarningBPS: 50, CriticalBPS: 100, RuleVersion: rule.Version, DetectedAt: start}
	}
	writes := 0
	if err := raw.Callback().Create().Before("gorm:create").Register("fail_monitor_second_batch", func(tx *gorm.DB) {
		if tx.Statement.Table == "monitor_events" {
			writes++
			if writes == 2 {
				tx.AddError(errors.New("injected second batch failure"))
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	inserted, err := repo.Record(ctx, rule, events)
	if err == nil || inserted != 0 {
		t.Fatal("failed batch reported success")
	}
	if err := raw.Callback().Create().Remove("fail_monitor_second_batch"); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := raw.Model(&monitorEventModel{}).Where("user_id = ?", user).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("partial detection committed", count, err)
	}
	inserted, err = repo.Record(ctx, rule, events)
	if err != nil || inserted != 201 {
		t.Fatalf("commit %d %v", inserted, err)
	}
	inserted, err = repo.Record(ctx, rule, events)
	if err != nil || inserted != 0 {
		t.Fatalf("idempotency %d %v", inserted, err)
	}
	if err := raw.Model(&monitorRuleModel{}).Where("user_id = ? AND asset_id = ?", user, a.ID).Update("version", strings.Repeat("a", 64)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Record(ctx, rule, events); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("stale rule committed", err)
	}
	page, err := repo.Events(ctx, monitor.EventQuery{UserID: user, AssetID: a.ID, Limit: 100})
	if err != nil || len(page.Events) != 100 || page.NextCursor == "" {
		t.Fatalf("event page %d %v", len(page.Events), err)
	}
	if page.Events[0].AnchorPrice == "" || page.Events[0].DeviationBPS != "-100.000000" {
		t.Fatal("event provenance lost")
	}
}
