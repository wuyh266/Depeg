package importcsv

import (
	"context"
	"depeg.local/platform/internal/domain"
	"depeg.local/platform/internal/domain/market"
	marketservice "depeg.local/platform/internal/service/market"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

var decimal = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]{1,3})?$`)
var symbol = regexp.MustCompile(`^[A-Za-z0-9]{1,32}$`)
var quote = regexp.MustCompile(`^[A-Z0-9]{2,16}$`)

func number(value string) (*big.Rat, error) {
	if len(value) > 128 || !decimal.MatchString(value) {
		return nil, domain.ErrInvalid
	}
	n, ok := new(big.Rat).SetString(value)
	if !ok {
		return nil, domain.ErrInvalid
	}
	fixed := n.FloatString(18)
	checked, ok := new(big.Rat).SetString(fixed)
	if !ok || checked.Cmp(n) != 0 || len(strings.SplitN(fixed, ".", 2)[0]) > 20 {
		return nil, domain.ErrInvalid
	}
	return n, nil
}
func decimalString(value *big.Rat) string {
	return strings.TrimSuffix(strings.TrimRight(value.FloatString(18), "0"), ".")
}
func Read(ctx context.Context, input io.Reader, a market.Asset, source string, interval time.Duration) ([]market.Candle, error) {
	return ReadWithTimezone(ctx, input, a, source, interval, time.UTC)
}

func SupportedInterval(interval time.Duration) bool {
	switch interval {
	case time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, 24 * time.Hour:
		return true
	default:
		return false
	}
}

func ReadWithTimezone(ctx context.Context, input io.Reader, a market.Asset, source string, interval time.Duration, location *time.Location) ([]market.Candle, error) {
	if location == nil || !SupportedInterval(interval) {
		return nil, domain.ErrInvalid
	}
	if !marketservice.AssetIDPattern.MatchString(a.ID) || !symbol.MatchString(a.Symbol) || !quote.MatchString(a.QuoteCurrency) || len(a.Chain) > 64 || len(a.TokenAddress) > 128 || !marketservice.SourcePattern.MatchString(source) {
		return nil, fmt.Errorf("%w: invalid asset or source metadata", domain.ErrInvalid)
	}
	if a.AnchorPrice != nil {
		anchor, err := number(*a.AnchorPrice)
		if err != nil || anchor.Sign() <= 0 {
			return nil, fmt.Errorf("%w: anchor must be a positive decimal", domain.ErrInvalid)
		}
	}
	closeCutoff := time.Now().UTC().Truncate(interval)
	reader := csv.NewReader(input)
	reader.ReuseRecord = true
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("%w: cannot read CSV header", domain.ErrInvalid)
	}
	columns := map[string]int{}
	for i, name := range header {
		name = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(name, "\ufeff")))
		if _, exists := columns[name]; exists {
			return nil, fmt.Errorf("%w: duplicate column", domain.ErrInvalid)
		}
		columns[name] = i
	}
	for _, key := range []string{"open", "high", "low", "close", "volume"} {
		if _, ok := columns[key]; !ok {
			return nil, fmt.Errorf("%w: missing column %s", domain.ErrInvalid, key)
		}
	}
	_, hasTimestamp := columns["timestamp"]
	_, hasDatetime := columns["datetime"]
	if !hasTimestamp && !hasDatetime {
		return nil, fmt.Errorf("%w: timestamp or datetime is required", domain.ErrInvalid)
	}
	result := make([]market.Candle, 0, 4096)
	for line := 2; ; line++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: malformed CSV row %d", domain.ErrInvalid, line)
		}
		if len(result) >= 500000 {
			return nil, fmt.Errorf("%w: maximum 500000 rows per import", domain.ErrInvalid)
		}
		field := func(key string) string {
			if index, ok := columns[key]; ok {
				return strings.TrimSpace(record[index])
			}
			return ""
		}
		var observed time.Time
		if raw := field("timestamp"); raw != "" {
			stamp, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || stamp < 0 {
				return nil, fmt.Errorf("%w: timestamp at row %d", domain.ErrInvalid, line)
			}
			if stamp >= 1_000_000_000_000 {
				observed = time.UnixMilli(stamp).UTC()
			} else {
				observed = time.Unix(stamp, 0).UTC()
			}
		}
		if raw := field("datetime"); raw != "" {
			parsed, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				parsed, err = time.ParseInLocation("2006-01-02 15:04:05", raw, location)
			}
			if err != nil {
				return nil, fmt.Errorf("%w: datetime at row %d", domain.ErrInvalid, line)
			}
			if !observed.IsZero() && !observed.Equal(parsed) {
				return nil, fmt.Errorf("%w: timestamp/datetime mismatch at row %d", domain.ErrInvalid, line)
			}
			observed = parsed.UTC()
		}
		if observed.IsZero() || observed.Year() < 1970 || observed.Nanosecond() != 0 || observed.Unix()%int64(interval/time.Second) != 0 {
			return nil, fmt.Errorf("%w: invalid or unaligned time at row %d", domain.ErrInvalid, line)
		}
		if !observed.Before(closeCutoff) {
			return nil, fmt.Errorf("%w: candle has not closed at row %d", domain.ErrInvalid, line)
		}
		values := map[string]*big.Rat{}
		for _, key := range []string{"open", "high", "low", "close", "volume"} {
			n, err := number(field(key))
			if err != nil {
				return nil, fmt.Errorf("%w: %s at row %d must be a nonnegative decimal with at most 18 fractional digits", domain.ErrInvalid, key, line)
			}
			values[key] = n
		}
		if values["high"].Cmp(values["open"]) < 0 || values["high"].Cmp(values["close"]) < 0 || values["low"].Cmp(values["open"]) > 0 || values["low"].Cmp(values["close"]) > 0 || values["high"].Cmp(values["low"]) < 0 {
			return nil, fmt.Errorf("%w: inconsistent OHLC at row %d", domain.ErrInvalid, line)
		}
		rowSource := source
		if exchange := field("exchange"); exchange != "" {
			rowSource += ":" + exchange
		}
		if !marketservice.SourcePattern.MatchString(rowSource) {
			return nil, fmt.Errorf("%w: invalid source at row %d", domain.ErrInvalid, line)
		}
		volume := decimalString(values["volume"])
		result = append(result, market.Candle{AssetID: a.ID, Source: rowSource, Time: observed, Interval: interval, Open: decimalString(values["open"]), High: decimalString(values["high"]), Low: decimalString(values["low"]), Close: decimalString(values["close"]), Volume: &volume, Quality: "historical"})
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("%w: CSV has no data rows", domain.ErrInvalid)
	}
	return result, nil
}
