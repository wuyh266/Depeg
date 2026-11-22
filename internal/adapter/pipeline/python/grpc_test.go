package python

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"depeg.local/platform/internal/adapter/pipeline/python/rpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

type fakeInferenceServer struct {
	rpc.UnimplementedInferenceServiceServer
	call func(context.Context, *rpc.PredictRequest) (*rpc.PredictResponse, error)
}

func (s *fakeInferenceServer) Predict(ctx context.Context, r *rpc.PredictRequest) (*rpc.PredictResponse, error) {
	return s.call(ctx, r)
}

func rpcFixture(t *testing.T, call func(context.Context, *rpc.PredictRequest) (*rpc.PredictResponse, error)) *GRPCClient {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	rpc.RegisterInferenceServiceServer(server, &fakeInferenceServer{call: call})
	go server.Serve(listener)
	client, err := NewGRPCClient(listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close(); server.Stop(); listener.Close() })
	return client
}

func rpcResponse(t *testing.T) *rpc.PredictResponse {
	t.Helper()
	data := validResult()
	delete(data, "ok")
	encoded, _ := json.Marshal(data)
	response := new(rpc.PredictResponse)
	if err := protojson.Unmarshal(encoded, response); err != nil {
		t.Fatal(err)
	}
	return response
}

func rpcRequest() *rpc.PredictRequest {
	stamp, _ := time.Parse(time.RFC3339, "2026-01-01T03:00:00Z")
	return &rpc.PredictRequest{SchemaVersion: ResultSchema, AssetId: "ccxt.usdc-usdt", Symbol: "USDC/USDT", Source: "ccxt:okx", Horizons: []string{"0-3h"}, Candles: []*rpc.Candle{{OpenTimeSeconds: stamp.Unix()}}}
}

func TestGRPCResultRetainsZeroAndRejectsMissingRatio(t *testing.T) {
	response := rpcResponse(t)
	client := rpcFixture(t, func(context.Context, *rpc.PredictRequest) (*rpc.PredictResponse, error) { return response, nil })
	result, err := client.Predict(context.Background(), rpcRequest())
	if err != nil {
		t.Fatal(err)
	}
	if result[0].DepegMinuteRatio == nil || *result[0].DepegMinuteRatio != 0 || result[0].Quality != "integration_only" {
		t.Fatal("lost zero or quality")
	}
	response.Predictions[0].DepegMinuteRatio = nil
	if _, err := client.Predict(context.Background(), rpcRequest()); err == nil {
		t.Fatal("missing protobuf ratio accepted")
	}
}

func TestGRPCDeadlineAndServerStatus(t *testing.T) {
	client := rpcFixture(t, func(ctx context.Context, _ *rpc.PredictRequest) (*rpc.PredictResponse, error) {
		<-ctx.Done()
		return nil, status.FromContextError(ctx.Err()).Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := client.Predict(ctx, rpcRequest()); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("expected deadline, got %v", err)
	}
	client = rpcFixture(t, func(context.Context, *rpc.PredictRequest) (*rpc.PredictResponse, error) {
		return nil, status.Error(codes.InvalidArgument, "INSUFFICIENT_DATA")
	})
	if _, err := client.Predict(context.Background(), rpcRequest()); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("lost error status: %v", err)
	}
}

func TestGRPCRejectsRemoteAndMismatchedResponse(t *testing.T) {
	if _, err := NewGRPCClient("0.0.0.0:50051", time.Second); err == nil {
		t.Fatal("non-loopback accepted")
	}
	response := rpcResponse(t)
	client := rpcFixture(t, func(context.Context, *rpc.PredictRequest) (*rpc.PredictResponse, error) { return response, nil })
	request := rpcRequest()
	request.Candles[0].OpenTimeSeconds += 60
	if _, err := client.Predict(context.Background(), request); err == nil {
		t.Fatal("different observation time accepted")
	}
}

func TestReadRPCCandles(t *testing.T) {
	header := "timestamp,datetime,open,high,low,close,volume,exchange\n"
	row := "1767225600,2026-01-01T00:00:00Z,1.00001,1.01,.99,1.00002,0,okx\n"
	candles, err := ReadRPCCandles(strings.NewReader(header + row))
	if err != nil {
		t.Fatal(err)
	}
	if candles[0].Open != "1.00001" || candles[0].Volume != "0" {
		t.Fatal("decimal text changed")
	}
	if _, err := ReadRPCCandles(strings.NewReader(header + strings.Replace(row, "1767225600", "1767225600000", 1))); err == nil {
		t.Fatal("milliseconds accepted")
	}
}
