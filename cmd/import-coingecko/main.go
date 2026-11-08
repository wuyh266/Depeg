package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"depeg.local/platform/internal/bootstrap"
	"depeg.local/platform/internal/domain/dataset"
	"depeg.local/platform/internal/port"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, bootstrap.Default(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}


// go run ./cmd/import-coingecko `
//   -file "data/coingecko.zip" `
//   -kind daily `
//   -coin-ids "tether,usd-coin" `
//   -validate-only
func run(ctx context.Context, factory bootstrap.Factory, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("import-coingecko", flag.ContinueOnError)
	file := flags.String("file", "", "downloaded CoinGecko ZIP path")
	kind := flags.String("kind", "all", "all, daily, ohlc or news")
	coins := flags.String("coin-ids", "", "optional comma-separated CoinGecko IDs, e.g. tether,usd-coin,dai")
	quote := flags.String("quote-currency", "", "explicit USD quote confirmation required for OHLC")
	validate := flags.Bool("validate-only", false, "validate and report without loading configuration or connecting to MySQL")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *file == "" || flags.NArg() != 0 {
		return fmt.Errorf("-file is required; unexpected positional arguments are not accepted")
	}
	options := dataset.Options{Kind: *kind, QuoteCurrency: *quote}
	if *coins != "" {
		for _, id := range strings.Split(*coins, ",") {
			options.CoinIDs = append(options.CoinIDs, strings.TrimSpace(id))
		}
	}
	reader := factory.DatasetReader()
	report, err := reader.Scan(ctx, *file, options, nil)
	if err != nil {
		return err
	}
	if !*validate {
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
			return fmt.Errorf("MySQL unavailable; check MYSQL_DSN and run cmd/migrate before importing")
		}
		options.ExpectedSHA256 = report.SHA256
		if err := factory.DatasetRepository(database).Import(ctx, func(sink port.DatasetSink) error {
			_, err := reader.Scan(ctx, *file, options, func(batch dataset.Batch) error { return sink.Write(ctx, batch) })
			return err
		}); err != nil {
			return fmt.Errorf("import rolled back (check that cmd/migrate succeeded): %w", err)
		}
		report.Mode = "imported"
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}
