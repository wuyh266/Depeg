// Package coingecko reads the downloaded ZIPs without extracting files or invoking Python.
package coingecko

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"depeg.local/platform/internal/domain/dataset"
	"depeg.local/platform/internal/domain/market"
	"depeg.local/platform/internal/port"
)

const maxEntryBytes = 64 << 20
const maxArchiveBytes = 512 << 20

var coinIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,84}$`)

type reader struct{}

func New() port.DatasetReader { return &reader{} }

type metadata struct {
	CoinID         string `json:"coin_id"`
	Symbol         string `json:"symbol"`
	Count          int64  `json:"record_count"`
	Currency       string `json:"vs_currency"`
	Source         string `json:"source"`
	Interval       string `json:"interval"`
	TimestampRange struct {
		First time.Time `json:"first"`
		Last  time.Time `json:"last"`
	} `json:"timestamp_range"`
	ActualRange struct {
		Start time.Time `json:"start"`
		End   time.Time `json:"end"`
	} `json:"actual_date_range"`
}

type scan struct {
	ctx       context.Context
	options   dataset.Options
	files     map[string]*zip.File
	selected  map[string]bool
	available map[string]bool
	tables    map[string]bool
	write     func(dataset.Batch) error
	report    dataset.Report
	now       time.Time
}

func (r *reader) Scan(ctx context.Context, filename string, options dataset.Options, write func(dataset.Batch) error) (dataset.Report, error) {
	if options.Kind == "" {
		options.Kind = "all"
	}
	if options.Kind != "all" && options.Kind != "ohlc" && options.Kind != "daily" && options.Kind != "news" {
		return dataset.Report{}, fmt.Errorf("kind must be all, ohlc, daily or news")
	}
	if options.QuoteCurrency != "" && options.QuoteCurrency != "USD" {
		return dataset.Report{}, fmt.Errorf("this adapter supports USD quotes; confirm OHLC with -quote-currency USD")
	}
	if err := ctx.Err(); err != nil {
		return dataset.Report{}, err
	}
	file, err := os.Open(filename)
	if err != nil {
		return dataset.Report{}, fmt.Errorf("open archive: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return dataset.Report{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxArchiveBytes {
		return dataset.Report{}, fmt.Errorf("archive must be a regular file no larger than 512 MiB")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, &contextReader{ctx: ctx, reader: file}); err != nil {
		return dataset.Report{}, err
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	if options.ExpectedSHA256 != "" && options.ExpectedSHA256 != digest {
		return dataset.Report{}, fmt.Errorf("archive changed after validation")
	}
	archive, err := zip.NewReader(file, info.Size())
	if err != nil {
		return dataset.Report{}, fmt.Errorf("read ZIP: %w", err)
	}
	if len(archive.File) > 4096 {
		return dataset.Report{}, fmt.Errorf("maximum 4096 ZIP entries")
	}
	s := &scan{ctx: ctx, options: options, files: map[string]*zip.File{}, selected: map[string]bool{}, available: map[string]bool{}, tables: map[string]bool{}, write: write, now: time.Now().UTC(), report: dataset.Report{Mode: "validated", SHA256: digest, Assets: []dataset.AssetReport{}}}
	for _, id := range options.CoinIDs {
		if !coinIDPattern.MatchString(id) {
			return dataset.Report{}, fmt.Errorf("invalid CoinGecko ID")
		}
		s.selected[id] = true
	}
	var total uint64
	for _, entry := range archive.File {
		name := entry.Name
		if entry.FileInfo().IsDir() {
			continue
		}
		if strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || path.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") {
			return dataset.Report{}, fmt.Errorf("invalid ZIP entry path")
		}
		if _, exists := s.files[name]; exists {
			return dataset.Report{}, fmt.Errorf("duplicate ZIP entry %s", name)
		}
		if entry.UncompressedSize64 > maxEntryBytes {
			return dataset.Report{}, fmt.Errorf("ZIP entry exceeds 64 MiB: %s", name)
		}
		total += entry.UncompressedSize64
		if total > maxArchiveBytes {
			return dataset.Report{}, fmt.Errorf("uncompressed archive exceeds 512 MiB")
		}
		s.files[name] = entry
	}
	names := make([]string, 0, len(s.files))
	for name := range s.files {
		names = append(names, name)
	}
	sort.Strings(names)
	// Tables establish the known coin IDs before optional news filtering.
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return dataset.Report{}, err
		}
		kind, metaName := "", ""
		switch path.Base(name) {
		case "full_lifecycle_ohlcv.parquet":
			kind = "ohlc"
			metaName = path.Join(path.Dir(name), "full_lifecycle.json")
		case "daily_market_chart.parquet":
			kind = "daily"
			metaName = path.Join(path.Dir(name), "daily_market_chart.json")
		default:
			if strings.HasSuffix(name, ".parquet") {
				return dataset.Report{}, fmt.Errorf("unsupported Parquet table %s", name)
			}
			continue
		}
		meta, err := s.metadata(metaName, kind)
		if err != nil {
			return dataset.Report{}, err
		}
		s.available[meta.CoinID] = true
		key := kind + "|" + meta.CoinID
		if s.tables[key] {
			return dataset.Report{}, fmt.Errorf("duplicate %s table for coin ID %s", kind, meta.CoinID)
		}
		s.tables[key] = true
		if !s.includes(meta.CoinID) || (options.Kind != "all" && options.Kind != kind) {
			continue
		}
		if kind == "ohlc" && options.QuoteCurrency != "USD" {
			return dataset.Report{}, fmt.Errorf("OHLC metadata omits quote currency; explicitly confirm -quote-currency USD")
		}
		if err := s.table(name, kind, meta); err != nil {
			return dataset.Report{}, fmt.Errorf("%s: %w", name, err)
		}
	}
	if options.Kind == "all" || options.Kind == "news" {
		if _, ok := s.files["coingecko_news/news.jsonl"]; ok {
			if err := s.news(); err != nil {
				return dataset.Report{}, err
			}
		}
	}
	for id := range s.selected {
		if !s.available[id] {
			return dataset.Report{}, fmt.Errorf("coin ID %s was not found in this archive", id)
		}
	}
	if s.report.Candles+s.report.Snapshots+s.report.News == 0 {
		return dataset.Report{}, fmt.Errorf("no matching records in this archive")
	}
	// Detect edits during the scan as well as changes between validation/import.
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return dataset.Report{}, err
	}
	after := sha256.New()
	if _, err := io.Copy(after, &contextReader{ctx: ctx, reader: file}); err != nil {
		return dataset.Report{}, err
	}
	if digest != hex.EncodeToString(after.Sum(nil)) {
		return dataset.Report{}, fmt.Errorf("archive changed during reading")
	}
	return s.report, nil
}

func (s *scan) includes(id string) bool { return len(s.selected) == 0 || s.selected[id] }

func (s *scan) read(name string, limit int64) ([]byte, error) {
	entry, ok := s.files[name]
	if !ok {
		return nil, fmt.Errorf("missing ZIP entry %s", name)
	}
	if entry.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("ZIP entry too large: %s", name)
	}
	r, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(&contextReader{ctx: s.ctx, reader: r}, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("ZIP entry exceeds read limit")
	}
	return data, nil
}

func (s *scan) metadata(name, kind string) (metadata, error) {
	data, err := s.read(name, 1<<20)
	if err != nil {
		return metadata{}, err
	}
	var meta metadata
	if json.Unmarshal(data, &meta) != nil {
		return meta, fmt.Errorf("invalid metadata JSON: %s", name)
	}
	if !coinIDPattern.MatchString(meta.CoinID) || meta.Symbol == "" || len(meta.Symbol) > 32 || !utf8.ValidString(meta.Symbol) || meta.Count < 1 || meta.Count > 500000 {
		return meta, fmt.Errorf("invalid coin metadata: %s", name)
	}
	if kind == "daily" && (meta.Currency != "usd" || meta.Interval != "daily" || meta.Source != "CoinGecko Pro API") {
		return meta, fmt.Errorf("daily metadata must declare USD daily CoinGecko observations")
	}
	if kind == "ohlc" && meta.Source != "CoinGecko Pro API /ohlc/range" {
		return meta, fmt.Errorf("OHLC source metadata is unsupported")
	}
	return meta, nil
}

func (s *scan) asset(meta metadata) market.Asset {
	return market.Asset{ID: "coingecko." + meta.CoinID, Symbol: strings.ToUpper(meta.Symbol), QuoteCurrency: "USD"}
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
