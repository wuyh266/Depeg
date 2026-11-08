package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"depeg.local/platform/internal/bootstrap"
	"depeg.local/platform/internal/domain/market"
	"depeg.local/platform/internal/importcsv"
)

func main() {
	if err := run(context.Background(), bootstrap.Default()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(ctx context.Context, factory bootstrap.Factory) error {
	validateOnly := flag.Bool("validate-only", false, "validate CSV without a database connection")
	path := flag.String("file", "", "OHLCV CSV path")
	id := flag.String("asset-id", "", "stable asset identifier, e.g. usdc-ethereum")
	symbol := flag.String("symbol", "", "asset symbol")
	chain := flag.String("chain", "", "chain, if known")
	address := flag.String("token-address", "", "contract address, if known")
	quote := flag.String("quote", "USD", "quote currency")
	anchor := flag.String("anchor", "1", "anchor in quote currency")
	timezone := flag.String("timezone", "UTC", "timezone for naive datetime values, e.g. Asia/Shanghai")
	source := flag.String("source", "paper-csv", "source namespace; exchange column is appended")
	intervalText := flag.String("interval", "1m", "stored candle interval (no resampling)")
	flag.Parse()
	if *path == "" || *id == "" || *symbol == "" {
		return fmt.Errorf("-file, -asset-id and -symbol are required")
	}
	interval, err := time.ParseDuration(*intervalText)
	if err != nil {
		return fmt.Errorf("invalid interval")
	}
	input, err := os.Open(*path)
	if err != nil {
		return fmt.Errorf("open CSV: %w", err)
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return err
	}
	if info.Size() > 128*1024*1024 {
		return fmt.Errorf("maximum CSV size is 128 MiB")
	}
	asset := market.Asset{ID: *id, Symbol: *symbol, Chain: *chain, TokenAddress: *address, QuoteCurrency: *quote, AnchorPrice: anchor}
	location, err := time.LoadLocation(*timezone)
	if err != nil {
		return fmt.Errorf("invalid timezone")
	}
	candles, err := importcsv.ReadWithTimezone(ctx, input, asset, *source, interval, location)
	if err != nil {
		return err
	}
	if *validateOnly {
		fmt.Printf("Validated %d historical candles for %s; no database writes\n", len(candles), asset.ID)
		return nil
	}
	settings, err := factory.Config().Load(ctx)
	if err != nil {
		return err
	}
	logger := factory.Logger(settings.Log)
	database, err := factory.MySQL(settings.MySQL, logger)
	if err != nil {
		return err
	}
	defer database.Close()
	if err := database.Ping(ctx); err != nil {
		return fmt.Errorf("MySQL is unavailable; check MYSQL_DSN and the database service")
	}
	repository := factory.MarketRepository(database)
	if err := repository.Import(ctx, asset, candles); err != nil {
		return err
	}

	rdb,err := factory.Redis(settings.Redis, logger)
	if err != nil {
		logger.Error(ctx,"Redis is unavailable; check REDIS_DSN and the Redis service", "error", err)
	}else{
		defer rdb.Close()
		assetCache:=factory.AssetCache(rdb)
		invalidatectx,cancel:=context.WithTimeout(ctx,100*time.Millisecond)
		err:=assetCache.InvalidateAsset(invalidatectx)
		cancel()
		if err!=nil{
			logger.Error(ctx,"asset import committed, but cache invalidation failed","error",err)
		}
	}

	fmt.Printf("Processed %d historical candles for %s; duplicate source/time records were updated\n", len(candles), asset.ID)
	return nil
}
