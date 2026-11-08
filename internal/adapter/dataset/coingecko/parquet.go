package coingecko

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"depeg.local/platform/internal/domain/dataset"
	"depeg.local/platform/internal/domain/market"
	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/format"
)

func (s *scan) table(name, kind string, meta metadata) (err error) {
	// The decoder accepts external binary input; malformed schemas must be CLI errors.
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("invalid Parquet data: %v", value)
		}
	}()
	if s.report.Candles+s.report.Snapshots+meta.Count > 5000000 {
		return fmt.Errorf("maximum 5000000 observations per archive")
	}
	data, err := s.read(name, maxEntryBytes)
	if err != nil {
		return err
	}
	file, err := parquet.OpenFile(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	if file.NumRows() != meta.Count {
		return fmt.Errorf("Parquet row count differs from metadata")
	}
	columns := file.Schema().Columns()
	if len(columns) > 16 {
		return fmt.Errorf("unsupported Parquet schema")
	}
	known := map[string]bool{}
	for _, column := range columns {
		if len(column) != 1 || known[column[0]] {
			return fmt.Errorf("expected distinct flat Parquet columns")
		}
		known[column[0]] = true
	}
	required := []string{"timestamp", "open", "high", "low", "close", "volume"}
	period := time.Hour
	first, last := meta.TimestampRange.First, meta.TimestampRange.Last
	if kind == "daily" {
		required = []string{"timestamp", "price_usd", "market_cap_usd", "volume_24h_usd"}
		period = 24 * time.Hour
		first, last = meta.ActualRange.Start, meta.ActualRange.End
	}
	for _, column := range required {
		if !known[column] {
			return fmt.Errorf("missing Parquet column %s", column)
		}
	}
	leaf, ok := file.Schema().Lookup("timestamp")
	if !ok || leaf.Node.Type().LogicalType() == nil {
		return fmt.Errorf("timestamp must declare UTC milliseconds")
	}
	logical, ok := leaf.Node.Type().LogicalType().Value.(*format.TimestampType)
	if !ok || !logical.IsAdjustedToUTC || logical.Unit.String() != "MILLIS" {
		return fmt.Errorf("timestamp must declare UTC milliseconds")
	}
	a := s.asset(meta)
	batch := dataset.Batch{Asset: &a}
	if kind == "ohlc" {
		batch.Candles = make([]market.Candle, 0, meta.Count)
	} else {
		batch.Snapshots = make([]market.Snapshot, 0, meta.Count)
	}
	input := parquet.NewReader(file)
	defer input.Close()
	buffer := make([]parquet.Row, 256)
	var previous time.Time
	info := dataset.AssetReport{CoinID: meta.CoinID, AssetID: a.ID, Kind: kind}
	for {
		if err := s.ctx.Err(); err != nil {
			return err
		}
		count, readErr := input.ReadRows(buffer)
		for _, row := range buffer[:count] {
			values := make(map[string]parquet.Value, len(columns))
			if len(row) != len(columns) {
				return fmt.Errorf("nested or repeated columns are unsupported")
			}
			for _, value := range row {
				if value.Column() < 0 || value.Column() >= len(columns) || value.RepetitionLevel() != 0 {
					return fmt.Errorf("invalid Parquet row layout")
				}
				values[columns[value.Column()][0]] = value
			}
			stamp := values["timestamp"]
			if stamp.IsNull() || stamp.Kind() != parquet.Int64 {
				return fmt.Errorf("invalid timestamp at row %d", info.Rows+1)
			}
			observed := time.UnixMilli(stamp.Int64()).UTC()
			if observed.Year() < 1970 || observed.After(s.now.Add(5*time.Minute)) || observed.Nanosecond() != 0 || observed.Unix()%int64(period/time.Second) != 0 || (!previous.IsZero() && !observed.After(previous)) {
				return fmt.Errorf("unaligned, future, duplicate or unsorted timestamp at row %d", info.Rows+1)
			}
			if previous.IsZero() {
				info.First = observed
			} else {
				info.MissingPeriods += int64(observed.Sub(previous)/period) - 1
			}
			previous = observed
			info.Last = observed
			info.Rows++
			if kind == "ohlc" {
				point, err := s.candle(a.ID, observed, values)
				if err != nil {
					return fmt.Errorf("row %d: %w", info.Rows, err)
				}
				batch.Candles = append(batch.Candles, point)
			} else {
				point, err := s.snapshot(a.ID, observed, values)
				if err != nil {
					return fmt.Errorf("row %d: %w", info.Rows, err)
				}
				batch.Snapshots = append(batch.Snapshots, point)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
		if count == 0 {
			return io.ErrNoProgress
		}
	}
	if info.Rows != meta.Count || !info.First.Equal(first) || !info.Last.Equal(last) {
		return fmt.Errorf("actual rows or timestamp range differs from metadata")
	}
	if s.write != nil {
		if err := s.write(batch); err != nil {
			return err
		}
	}
	s.report.Assets = append(s.report.Assets, info)
	s.report.Candles += int64(len(batch.Candles))
	s.report.Snapshots += int64(len(batch.Snapshots))
	return nil
}

// numeric keeps the source float's shortest round-trippable decimal text (or
// exact integer text), then explicitly rounds to DECIMAL(38,18). Raw text is
// persisted alongside normalized values; rounding is counted and flagged.
func numeric(value parquet.Value, mode string) (*string, string, string, error) {
	if value.IsNull() {
		if mode == "price" {
			return nil, "", "", fmt.Errorf("price is missing")
		}
		return nil, "", "", nil
	}
	var raw string
	switch value.Kind() {
	case parquet.Double:
		v := value.Double()
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, "", "", fmt.Errorf("non-finite numeric value")
		}
		raw = strconv.FormatFloat(v, 'g', -1, 64)
	case parquet.Float:
		v := value.Float()
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return nil, "", "", fmt.Errorf("non-finite numeric value")
		}
		raw = strconv.FormatFloat(float64(v), 'g', -1, 32)
	case parquet.Int64:
		raw = strconv.FormatInt(value.Int64(), 10)
	case parquet.Int32:
		raw = strconv.FormatInt(int64(value.Int32()), 10)
	default:
		return nil, "", "", fmt.Errorf("unsupported numeric physical type")
	}
	n, ok := new(big.Rat).SetString(raw)
	if !ok {
		return nil, "", "", fmt.Errorf("invalid numeric value")
	}
	if n.Sign() < 0 {
		if mode == "cap" {
			return nil, raw, "invalid", nil
		}
		return nil, "", "", fmt.Errorf("negative price or volume")
	}
	if mode == "price" && n.Sign() == 0 {
		return nil, "", "", fmt.Errorf("price must be positive")
	}
	fixed := n.FloatString(18)
	if len(strings.SplitN(fixed, ".", 2)[0]) > 20 {
		return nil, "", "", fmt.Errorf("numeric value exceeds DECIMAL(38,18)")
	}
	checked, _ := new(big.Rat).SetString(fixed)
	if mode == "price" && checked.Sign() == 0 {
		return nil, "", "", fmt.Errorf("price rounds to zero at storage precision")
	}
	flag := ""
	if checked.Cmp(n) != 0 {
		flag = "rounded"
	}
	if n.Sign() == 0 {
		flag = "zero"
	}
	text := strings.TrimSuffix(strings.TrimRight(fixed, "0"), ".")
	return &text, raw, flag, nil
}

func rawValue(raw string) any {
	if raw == "" {
		return nil
	}
	return raw
}

func (s *scan) candle(assetID string, closed time.Time, values map[string]parquet.Value) (market.Candle, error) {
	point := market.Candle{AssetID: assetID, Source: "coingecko:ohlc", Time: closed.Add(-time.Hour), SourceTime: &closed, Interval: time.Hour, Quality: "historical"}
	raw := map[string]any{}
	normalized := map[string]*string{}
	for _, name := range []string{"open", "high", "low", "close", "volume"} {
		mode := "price"
		if name == "volume" {
			mode = "volume"
		}
		value, text, flag, err := numeric(values[name], mode)
		if err != nil {
			return point, fmt.Errorf("%s: %w", name, err)
		}
		if flag == "rounded" {
			s.report.RoundedValues++
			point.Quality = "historical:rounded"
		}
		normalized[name] = value
		raw[name] = rawValue(text)
	}
	compare := func(a, b string) int {
		x, _ := new(big.Rat).SetString(raw[a].(string))
		y, _ := new(big.Rat).SetString(raw[b].(string))
		return x.Cmp(y)
	}
	if compare("high", "open") < 0 || compare("high", "close") < 0 || compare("low", "open") > 0 || compare("low", "close") > 0 || compare("high", "low") < 0 {
		return point, fmt.Errorf("inconsistent OHLC")
	}
	point.Open = *normalized["open"]
	point.High = *normalized["high"]
	point.Low = *normalized["low"]
	point.Close = *normalized["close"]
	point.Volume = normalized["volume"]
	if point.Volume == nil {
		s.report.MissingVolumes++
	}
	encoded, _ := json.Marshal(raw)
	point.RawValues = string(encoded)
	return point, nil
}

func (s *scan) snapshot(assetID string, observed time.Time, values map[string]parquet.Value) (market.Snapshot, error) {
	point := market.Snapshot{AssetID: assetID, Source: "coingecko:market-chart", Time: observed, QualityFlags: []string{}}
	raw := map[string]any{}
	for _, column := range []struct {
		name, mode string
		target     **string
	}{{"price_usd", "price", nil}, {"market_cap_usd", "cap", &point.MarketCapUSD}, {"volume_24h_usd", "volume", &point.Volume24hUSD}} {
		value, text, flag, err := numeric(values[column.name], column.mode)
		if err != nil {
			return point, fmt.Errorf("%s: %w", column.name, err)
		}
		if column.name == "price_usd" {
			point.PriceUSD = *value
		} else {
			*column.target = value
		}
		raw[column.name] = rawValue(text)
		if flag != "" {
			point.QualityFlags = append(point.QualityFlags, column.name+"_"+flag)
		}
		if value == nil && flag != "invalid" {
			point.QualityFlags = append(point.QualityFlags, column.name+"_missing")
		}
		if flag == "rounded" {
			s.report.RoundedValues++
		}
		if column.mode == "cap" && flag == "invalid" {
			s.report.InvalidMarketCaps++
		}
	}
	encoded, _ := json.Marshal(raw)
	point.RawValues = string(encoded)
	return point, nil
}
