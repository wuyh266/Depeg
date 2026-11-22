package python

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"depeg.local/platform/internal/adapter/pipeline/python/rpc"
	"depeg.local/platform/internal/domain/risk"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// GRPCClient is opt-in; the HTTP API and worker do not create it automatically.
type GRPCClient struct {
	connection *grpc.ClientConn
	timeout    time.Duration
}

func NewGRPCClient(address string, timeout time.Duration) (*GRPCClient, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil || !net.ParseIP(host).IsLoopback() || timeout <= 0 {
		return nil, fmt.Errorf("gRPC prototype requires a loopback IP:port and a positive timeout")
	}
	connection, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(4*1024*1024)))
	if err != nil {
		return nil, err
	}
	return &GRPCClient{connection: connection, timeout: timeout}, nil
}

func (c *GRPCClient) Close() error { return c.connection.Close() }

func (c *GRPCClient) Health(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	response, err := grpc_health_v1.NewHealthClient(c.connection).Check(ctx, &grpc_health_v1.HealthCheckRequest{Service: "depeg.inference.v1.InferenceService"})
	if err != nil {
		return err
	}
	if response.Status != grpc_health_v1.HealthCheckResponse_SERVING {
		return fmt.Errorf("DG gRPC service is not serving")
	}
	return nil
}

func (c *GRPCClient) Predict(ctx context.Context, request *rpc.PredictRequest) ([]risk.Prediction, error) {
	if request == nil {
		return nil, fmt.Errorf("missing inference request")
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	response, err := rpc.NewInferenceServiceClient(c.connection).Predict(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("DG gRPC inference: %w", err)
	}
	// Reuse the established JSON/domain contract validator rather than trusting protobuf defaults.
	data, err := (protojson.MarshalOptions{EmitDefaultValues: true, UseProtoNames: true}).Marshal(response)
	if err != nil {
		return nil, err
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	envelope["ok"] = json.RawMessage("true")
	data, err = json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	predictions, err := DecodeResult(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if len(predictions) != len(request.Horizons) || len(request.Candles) == 0 {
		return nil, fmt.Errorf("gRPC response does not match request")
	}
	windows := map[string]bool{}
	for _, h := range request.Horizons {
		windows[h] = true
	}
	for _, p := range response.Predictions {
		if !windows[p.Horizon] || p.AssetId != request.AssetId || p.Symbol != request.Symbol || p.Source != request.Source {
			return nil, fmt.Errorf("gRPC response market or horizon differs from request")
		}
	}
	cutoff := time.Unix(request.Candles[len(request.Candles)-1].OpenTimeSeconds, 0).Add(time.Minute)
	if !predictions[0].ObservedAt.Equal(cutoff) {
		return nil, fmt.Errorf("gRPC response observation time differs from request")
	}
	return predictions, nil
}

// ReadRPCCandles preserves original decimal text; Python validates numerical values.
func ReadRPCCandles(reader io.Reader) ([]*rpc.Candle, error) {
	input := csv.NewReader(reader)
	header, err := input.Read()
	if err != nil {
		return nil, err
	}
	columns := map[string]int{}
	for i, name := range header {
		if i == 0 {
			name = strings.TrimPrefix(name, "\ufeff")
		}
		if _, duplicate := columns[name]; duplicate {
			return nil, fmt.Errorf("duplicate CSV column")
		}
		columns[name] = i
	}
	for _, name := range []string{"timestamp", "datetime", "open", "high", "low", "close", "volume", "exchange"} {
		if _, ok := columns[name]; !ok {
			return nil, fmt.Errorf("missing CSV column %s", name)
		}
	}
	var result []*rpc.Candle
	for {
		row, err := input.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		stamp, err := strconv.ParseInt(row[columns["timestamp"]], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid timestamp seconds")
		}
		textTime, err := time.Parse(time.RFC3339, row[columns["datetime"]])
		if err != nil || textTime.Unix() != stamp || textTime.Nanosecond() != 0 || stamp%60 != 0 {
			return nil, fmt.Errorf("inconsistent CSV minute timestamp")
		}
		if row[columns["exchange"]] != "okx" {
			return nil, fmt.Errorf("unsupported exchange")
		}
		result = append(result, &rpc.Candle{OpenTimeSeconds: stamp, Open: row[columns["open"]], High: row[columns["high"]], Low: row[columns["low"]], Close: row[columns["close"]], Volume: row[columns["volume"]]})
		if len(result) > 4096 {
			return nil, fmt.Errorf("maximum 4096 candles per request")
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("input is empty")
	}
	return result, nil
}
