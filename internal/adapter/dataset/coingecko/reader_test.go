package coingecko

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"depeg.local/platform/internal/domain/dataset"
	"github.com/parquet-go/parquet-go"
)

type hourlyFixture struct {
	Timestamp int64   `parquet:"timestamp,timestamp(millisecond)"`
	Open      float64 `parquet:"open"`
	High      float64 `parquet:"high"`
	Low       float64 `parquet:"low"`
	Close     float64 `parquet:"close"`
	Volume    *int32  `parquet:"volume,optional"`
}

type dailyFixture struct {
	Timestamp int64   `parquet:"timestamp,timestamp(millisecond)"`
	Price     float64 `parquet:"price_usd"`
	Cap       int64   `parquet:"market_cap_usd"`
	Volume    float64 `parquet:"volume_24h_usd"`
}

func encoded(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func parquetBytes[T any](t *testing.T, rows []T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := parquet.Write(&buf, rows); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
func archiveFile(t *testing.T, entries map[string][]byte) string {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, data := range entries {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(t.TempDir(), "dataset.zip")
	if err := os.WriteFile(name, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return name
}
func tableArchive(t *testing.T, kind string, data []byte, count int, first, last time.Time) string {
	t.Helper()
	meta := map[string]any{"coin_id": "test-usd", "symbol": "TEST", "record_count": count}
	var dir, file, metadataName string
	if kind == "ohlc" {
		dir = "coingecko_ohlcv_stablecoin_list_full/test-usd/"
		file = "full_lifecycle_ohlcv.parquet"
		metadataName = "full_lifecycle.json"
		meta["timestamp_range"] = map[string]any{"first": first, "last": last}
		meta["source"] = "CoinGecko Pro API /ohlc/range"
	} else {
		dir = "coingecko_market_chart/TEST/"
		file = "daily_market_chart.parquet"
		metadataName = "daily_market_chart.json"
		meta["actual_date_range"] = map[string]any{"start": first, "end": last}
		meta["source"] = "CoinGecko Pro API"
		meta["vs_currency"] = "usd"
		meta["interval"] = "daily"
	}
	return archiveFile(t, map[string][]byte{dir + file: data, dir + metadataName: encoded(t, meta)})
}

func TestOHLCNullVolumeAndCloseTimeArePreserved(t *testing.T) {
	start := time.Date(2023, 1, 1, 1, 0, 0, 0, time.UTC)
	rows := []hourlyFixture{{Timestamp: start.UnixMilli(), Open: 1, High: 1.01, Low: .99, Close: 1}, {Timestamp: start.Add(2 * time.Hour).UnixMilli(), Open: 1, High: 1.01, Low: .99, Close: 1}}
	file := tableArchive(t, "ohlc", parquetBytes(t, rows), 2, start, start.Add(2*time.Hour))
	var batches []dataset.Batch
	report, err := New().Scan(context.Background(), file, dataset.Options{QuoteCurrency: "USD"}, func(b dataset.Batch) error { batches = append(batches, b); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if report.Candles != 2 || report.MissingVolumes != 2 || len(batches) != 1 || report.Assets[0].MissingPeriods != 1 {
		t.Fatalf("incorrect report: %+v", report)
	}
	point := batches[0].Candles[0]
	if point.Volume != nil || !point.Time.Equal(start.Add(-time.Hour)) || point.SourceTime == nil || !point.SourceTime.Equal(start) || batches[0].Asset.AnchorPrice != nil {
		t.Fatalf("lost source semantics: %+v", point)
	}
	var raw map[string]any
	if json.Unmarshal([]byte(point.RawValues), &raw) != nil || raw["volume"] != nil {
		t.Fatal("raw null volume was lost")
	}
	if _, err := New().Scan(context.Background(), file, dataset.Options{}, nil); err == nil {
		t.Fatal("OHLC without explicit quote confirmation was accepted")
	}
	if _, err := New().Scan(context.Background(), file, dataset.Options{QuoteCurrency: "USD", ExpectedSHA256: strings.Repeat("0", 64)}, nil); err == nil {
		t.Fatal("changed input was accepted")
	}
}

func TestDailyInvalidCapRoundingAndExactIntegerProvenance(t *testing.T) {
	start := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := []dailyFixture{{Timestamp: start.UnixMilli(), Price: .0002511649990551367, Cap: -1, Volume: 0}, {Timestamp: start.Add(24 * time.Hour).UnixMilli(), Price: 1, Cap: 9007199254740993, Volume: 2}}
	file := tableArchive(t, "daily", parquetBytes(t, rows), 2, start, start.Add(24*time.Hour))
	var batch dataset.Batch
	report, err := New().Scan(context.Background(), file, dataset.Options{}, func(b dataset.Batch) error { batch = b; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if report.Snapshots != 2 || report.InvalidMarketCaps != 1 || report.RoundedValues != 1 {
		t.Fatalf("wrong normalization report: %+v", report)
	}
	if batch.Snapshots[0].MarketCapUSD != nil || batch.Snapshots[0].PriceUSD != "0.000251164999055137" {
		t.Fatalf("wrong normalization: %+v", batch.Snapshots[0])
	}
	if batch.Snapshots[1].MarketCapUSD == nil || *batch.Snapshots[1].MarketCapUSD != "9007199254740993" {
		t.Fatal("integer was rounded through float64")
	}
	var raw map[string]string
	if json.Unmarshal([]byte(batch.Snapshots[0].RawValues), &raw) != nil || raw["market_cap_usd"] != "-1" || raw["price_usd"] != "0.0002511649990551367" {
		t.Fatal("original values were not retained")
	}
}

func TestMalformedTablesFailBeforeTheirBatchIsWritten(t *testing.T) {
	start := time.Date(2023, 1, 1, 1, 0, 0, 0, time.UTC)
	valid := hourlyFixture{Timestamp: start.UnixMilli(), Open: 1, High: 1, Low: 1, Close: 1}
	negative := int32(-1)
	cases := map[string]func(*hourlyFixture){"bad high": func(r *hourlyFixture) { r.High = .5 }, "NaN": func(r *hourlyFixture) { r.Open = math.NaN() }, "negative volume": func(r *hourlyFixture) { r.Volume = &negative }, "unaligned time": func(r *hourlyFixture) { r.Timestamp++ }}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			bad := valid
			change(&bad)
			file := tableArchive(t, "ohlc", parquetBytes(t, []hourlyFixture{bad}), 1, start, start)
			calls := 0
			if _, err := New().Scan(context.Background(), file, dataset.Options{QuoteCurrency: "USD"}, func(dataset.Batch) error { calls++; return nil }); err == nil || calls != 0 {
				t.Fatalf("bad row was emitted: %v, calls=%d", err, calls)
			}
		})
	}
	file := tableArchive(t, "ohlc", parquetBytes(t, []hourlyFixture{valid, valid}), 2, start, start)
	if _, err := New().Scan(context.Background(), file, dataset.Options{QuoteCurrency: "USD"}, nil); err == nil {
		t.Fatal("duplicate timestamps accepted")
	}
	file = tableArchive(t, "ohlc", parquetBytes(t, []hourlyFixture{valid}), 2, start, start)
	if _, err := New().Scan(context.Background(), file, dataset.Options{QuoteCurrency: "USD"}, nil); err == nil {
		t.Fatal("metadata row count mismatch accepted")
	}
	file = tableArchive(t, "ohlc", parquetBytes(t, []hourlyFixture{valid}), 1, start, start)
	if _, err := New().Scan(context.Background(), file, dataset.Options{CoinIDs: []string{"typo"}}, nil); err == nil {
		t.Fatal("unknown filter accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New().Scan(ctx, file, dataset.Options{QuoteCurrency: "USD"}, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored")
	}
	file = archiveFile(t, map[string][]byte{"../bad.parquet": {1, 2, 3}})
	if _, err := New().Scan(context.Background(), file, dataset.Options{}, nil); err == nil {
		t.Fatal("invalid ZIP path accepted")
	}
}

func TestNewsAuthorsFiltersAndRawLabels(t *testing.T) {
	var lines bytes.Buffer
	for i, author := range []any{"writer", []string{"first", "second"}, nil} {
		item := map[string]any{"title": "A title", "url": "https://example.com/" + string(rune('a'+i)), "author": author, "posted_at": "2023-01-01T00:00:00Z", "fetched_at": "2023-01-02T00:00:00Z", "type": "news", "matched_coin_ids": []string{"test-usd"}}
		lines.Write(encoded(t, item))
		lines.WriteByte('\n')
	}
	manifest := map[string]any{"unique_article_count": 3, "per_coin": map[string]any{"TEST": map[string]any{"coin_id": "test-usd"}}}
	file := archiveFile(t, map[string][]byte{"coingecko_news/news.jsonl": lines.Bytes(), "coingecko_news/manifest.json": encoded(t, manifest)})
	var news []dataset.News
	report, err := New().Scan(context.Background(), file, dataset.Options{Kind: "news", CoinIDs: []string{"test-usd"}}, func(b dataset.Batch) error { news = append(news, b.News...); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if report.News != 3 || len(news) != 3 || len(news[0].Authors) != 1 || len(news[1].Authors) != 2 || len(news[2].Authors) != 0 || news[2].RawJSON == "" {
		t.Fatalf("author normalization failed: %+v", news)
	}
	if news[0].ID == news[1].ID {
		t.Fatal("different URLs share an ID")
	}
	abort := errors.New("abort write")
	if _, err := New().Scan(context.Background(), file, dataset.Options{Kind: "news"}, func(dataset.Batch) error { return abort }); !errors.Is(err, abort) {
		t.Fatal("sink error was ignored")
	}
}
