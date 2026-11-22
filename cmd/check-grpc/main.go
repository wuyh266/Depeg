package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"depeg.local/platform/internal/adapter/pipeline/python"
	"depeg.local/platform/internal/adapter/pipeline/python/rpc"
)

func run() error {
	address := flag.String("address", "127.0.0.1:50051", "loopback Python gRPC service")
	input := flag.String("input", "", "OKX USDC/USDT CSV file (required)")
	horizons := flag.String("horizons", "0-3h", "comma-separated explicit model windows")
	timeout := flag.Duration("timeout", 10*time.Second, "RPC deadline")
	flag.Parse()
	if *input == "" {
		return fmt.Errorf("-input is required")
	}
	file, err := os.Open(*input)
	if err != nil {
		return err
	}
	defer file.Close()
	candles, err := python.ReadRPCCandles(file)
	if err != nil {
		return err
	}
	client, err := python.NewGRPCClient(*address, *timeout)
	if err != nil {
		return err
	}
	defer client.Close()
	if err := client.Health(context.Background()); err != nil {
		return fmt.Errorf("gRPC health: %w", err)
	}
	request := &rpc.PredictRequest{SchemaVersion: python.ResultSchema, AssetId: "ccxt.usdc-usdt", Symbol: "USDC/USDT", Source: "ccxt:okx", Candles: candles, Horizons: strings.Split(*horizons, ",")}
	result, err := client.Predict(context.Background(), request)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
