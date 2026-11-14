package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"depeg.local/platform/internal/bootstrap"
	"depeg.local/platform/internal/config"
	"depeg.local/platform/internal/domain"
	"depeg.local/platform/internal/domain/dataset"
	"depeg.local/platform/internal/domain/market"
	"depeg.local/platform/internal/importcsv"
	"depeg.local/platform/internal/port"
	marketservice "depeg.local/platform/internal/service/market"
)

func main() {
	if err := run(context.Background(), bootstrap.Default()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(ctx context.Context, factory bootstrap.Factory) error {
	validateOnly := flag.Bool("validate-only", false, "validate CSV without a database connection")
	latestOpenTime := flag.Bool("latest-open-time", false, "print the latest stored UTC open time for the specified asset, full source and interval")
	path := flag.String("file", "", "OHLCV CSV path")
	id := flag.String("asset-id", "", "stable asset identifier, e.g. usdc-ethereum")
	symbol := flag.String("symbol", "", "asset symbol")
	chain := flag.String("chain", "", "chain, if known")
	address := flag.String("token-address", "", "contract address, if known")
	quote := flag.String("quote", "USD", "quote currency")
	anchor := flag.String("anchor", "1", "anchor for a new asset in quote currency; -anchor= leaves it unset")
	timezone := flag.String("timezone", "UTC", "timezone for naive datetime values, e.g. Asia/Shanghai")
	source := flag.String("source", "paper-csv", "source namespace for import; full stored source for -latest-open-time")
	intervalText := flag.String("interval", "1m", "stored candle interval (no resampling)")
	flag.Parse()
	if flag.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	interval, err := time.ParseDuration(*intervalText)
	if err != nil || !importcsv.SupportedInterval(interval) {
		return fmt.Errorf("interval must be 1m, 5m, 15m, 1h or 24h")
	}
	var asset market.Asset
	var candles []market.Candle
	if *latestOpenTime {
		if *path != "" || *validateOnly {
			return fmt.Errorf("-latest-open-time cannot be combined with -file or -validate-only")
		}
		sourceExplicit := false
		flag.Visit(func(f *flag.Flag) { sourceExplicit = sourceExplicit || f.Name == "source" })
		if !marketservice.AssetIDPattern.MatchString(*id) || !sourceExplicit || !marketservice.SourcePattern.MatchString(*source) {
			return fmt.Errorf("-latest-open-time requires a valid -asset-id and full -source")
		}
	} else {
		if *path == "" || *id == "" || *symbol == "" {
			return fmt.Errorf("-file, -asset-id and -symbol are required")
		}
		asset = market.Asset{ID: *id, Symbol: *symbol, Chain: *chain, TokenAddress: *address, QuoteCurrency: *quote}
		if *anchor != "" {
			asset.AnchorPrice = anchor
		}
		candles, err = readCSV(ctx, *path, asset, *source, interval, *timezone)
		if err != nil {
			return err
		}
		if *validateOnly {
			fmt.Printf("Validated %d historical candles for %s; no database writes\n", len(candles), asset.ID)
			return nil
		}
	}

	mysqlSettings, logSettings, err := config.LoadDatabase(ctx)
	if err != nil {
		return err
	}
	database, err := factory.MySQL(mysqlSettings, factory.Logger(logSettings))
	if err != nil {
		return err
	}
	defer database.Close()
	if err := database.Ping(ctx); err != nil {
		return fmt.Errorf("MySQL is unavailable; check MYSQL_DSN and the database service")
	}
	if *latestOpenTime {
		openTime, found, err := factory.MarketRepository(database).LatestOpenTime(ctx, *id, *source, interval)
		if err != nil {
			return fmt.Errorf("read latest open time: %w", err)
		}
		if found {
			fmt.Println(openTime.Format(time.RFC3339))
		}
		return nil
	}
	err = factory.DatasetRepository(database).Import(ctx, func(sink port.DatasetSink) error {
		return sink.Write(ctx, dataset.Batch{Asset: &asset, Candles: candles})
	})
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return fmt.Errorf("import conflict for %s; verify the existing asset's quote currency: %w", asset.ID, err)
		}
		return fmt.Errorf("import %s: %w", asset.ID, err)
	}
	fmt.Printf("Processed %d historical candles for %s; duplicate source/time records were updated\n", len(candles), asset.ID)
	return nil
}

func readCSV(ctx context.Context, path string, asset market.Asset, source string, interval time.Duration, timezone string) ([]market.Candle, error) {
	input, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open CSV: %w", err)
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > 128*1024*1024 {
		return nil, fmt.Errorf("maximum CSV size is 128 MiB")
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone")
	}
	return importcsv.ReadWithTimezone(ctx, input, asset, source, interval, location)
}
