package importcsv_test

import (
	"context"
	"depeg.local/platform/internal/domain"
	"depeg.local/platform/internal/domain/market"
	"depeg.local/platform/internal/importcsv"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestCSVRejectsBadDataBeforeAnyImport(t *testing.T) {
	a := market.Asset{ID: "usdc", Symbol: "USDC", QuoteCurrency: "USD", AnchorPrice: decimalPointer("1")}
	header := "timestamp,datetime,open,high,low,close,volume\n"
	for _, row := range []string{"1672531200,2023-01-01 00:00:01,1,1,1,1,1", "1672531201,2023-01-01 00:00:01,1,1,1,1,1", "1672531200,2023-01-01 00:00:00,1,0.5,1,1,1", "1672531200,2023-01-01 00:00:00,NaN,1,1,1,1", "1672531200,2023-01-01 00:00:00,1,1,1,1,-1"} {
		values, err := importcsv.Read(context.Background(), strings.NewReader(header+row+"\n"), a, "paper-csv", time.Minute)
		if !errors.Is(err, domain.ErrInvalid) || values != nil {
			t.Fatalf("invalid data accepted: %q %v", row, err)
		}
	}
	values, err := importcsv.Read(context.Background(), strings.NewReader(header+"1672531200,2023-01-01 00:00:00,1,1,1,1,1\n"), a, "paper-csv", time.Minute)
	if err != nil || len(values) != 1 || values[0].Close != "1" {
		t.Fatalf("valid data rejected: %v", err)
	}
}

func TestResearchCSVRequiresExplicitTimezone(t *testing.T) {
	a := market.Asset{ID: "usdc", Symbol: "USDC", QuoteCurrency: "USD", AnchorPrice: decimalPointer("1")}
	csv := "timestamp,datetime,open,high,low,close,volume\n1675180800,2023-02-01 00:00:00,1,1,1,1,1\n"
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	candles, err := importcsv.ReadWithTimezone(context.Background(), strings.NewReader(csv), a, "paper-csv", time.Minute, location)
	if err != nil || len(candles) != 1 || candles[0].Time.Unix() != 1675180800 {
		t.Fatalf("timezone alignment failed: %v", err)
	}
	if _, err := importcsv.Read(context.Background(), strings.NewReader(csv), a, "paper-csv", time.Minute); err == nil {
		t.Fatal("ambiguous timezone was silently accepted")
	}
}

func TestScientificNotationIsConvertedWithoutFloatRounding(t *testing.T) {
	a := market.Asset{ID: "usdc", Symbol: "USDC", QuoteCurrency: "USD", AnchorPrice: decimalPointer("1")}
	input := "timestamp,open,high,low,close,volume\n1672531200,1,1,1,1,9e-06\n"
	values, err := importcsv.Read(context.Background(), strings.NewReader(input), a, "paper-csv", time.Minute)
	if err != nil || len(values) != 1 || (values[0].Volume == nil || *values[0].Volume != "0.000009") {
		t.Fatalf("scientific notation failed: %+v %v", values, err)
	}
}

func TestCollectorCSVWithoutAnchorAcceptsOnlyClosedCandles(t *testing.T) {
	a := market.Asset{ID: "ccxt.usdc-usdt", Symbol: "USDC", QuoteCurrency: "USDT"}
	const header = "timestamp,datetime,open,high,low,close,volume,exchange\n"
	row := func(stamp time.Time) string {
		return fmt.Sprintf("%d,%s,1.00106,1.00106,1.00105,1.00105,628.557467,okx\n", stamp.Unix(), stamp.Format(time.RFC3339))
	}
	current := time.Now().UTC().Truncate(time.Minute)
	closed := current.Add(-time.Minute)
	candles, err := importcsv.Read(context.Background(), strings.NewReader(header+row(closed)), a, "ccxt", time.Minute)
	if err != nil || len(candles) != 1 || candles[0].Source != "ccxt:okx" || !candles[0].Time.Equal(closed) {
		t.Fatalf("collector CSV rejected or changed: %+v, %v", candles, err)
	}
	// Use the next open time to avoid a flaky assertion across a minute boundary.
	if _, err := importcsv.Read(context.Background(), strings.NewReader(header+row(current.Add(time.Minute))), a, "ccxt", time.Minute); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("unclosed candle accepted: %v", err)
	}
	a.AnchorPrice = decimalPointer("0")
	if _, err := importcsv.Read(context.Background(), strings.NewReader(header+row(closed)), a, "ccxt", time.Minute); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("explicit invalid anchor accepted: %v", err)
	}
}

func decimalPointer(value string) *string { return &value }
