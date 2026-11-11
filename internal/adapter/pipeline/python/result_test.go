package python

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func validResult() map[string]any {
	return map[string]any{
		"schema_version": ResultSchema, "ok": true, "generated_at": "2026-01-01T04:00:00Z",
		"predictions": []any{map[string]any{
			"asset_id": "ccxt.usdc-usdt", "symbol": "USDC/USDT", "source": "ccxt:okx",
			"observed_at": "2026-01-01T03:01:00Z", "reference_open_time": "2026-01-01T03:00:00Z", "last_input_open_time": "2026-01-01T02:59:00Z",
			"horizon": "0-3h", "horizon_start_minutes": 0, "horizon_end_minutes": 180,
			"prediction_start": "2026-01-01T03:01:00Z", "prediction_end": "2026-01-01T06:01:00Z",
			"depeg_minute_ratio": 0.0, "model_version": "TEST", "feature_version": "dg-build-features-v3-window10/v1",
			"label_version": "absolute-close-minus-one-ge-0.05/v1", "quality": "integration_only",
		}},
	}
}

func TestDecodeResult(t *testing.T) {
	data, _ := json.Marshal(validResult())
	predictions, err := DecodeResult(strings.NewReader(string(data)))
	if err != nil || len(predictions) != 1 {
		t.Fatalf("decode: %v", err)
	}
	if predictions[0].DepegMinuteRatio == nil || *predictions[0].DepegMinuteRatio != 0 || predictions[0].Quality != "integration_only" {
		t.Fatal("zero and unverified quality must be preserved")
	}
	for _, test := range []struct {
		name  string
		key   string
		value any
	}{
		{"null ratio", "depeg_minute_ratio", nil}, {"out of range", "depeg_minute_ratio", 1.01},
		{"wrong source", "source", "ccxt:binance"}, {"wrong window", "horizon_end_minutes", 720},
		{"offset", "prediction_start", "2026-01-01T03:02:00Z"}, {"quality", "quality", ""},
		{"missing start", "horizon_start_minutes", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			envelope := validResult()
			envelope["predictions"].([]any)[0].(map[string]any)[test.key] = test.value
			data, _ := json.Marshal(envelope)
			if _, err := DecodeResult(strings.NewReader(string(data))); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestDecodeFailureAndInvalidEnvelope(t *testing.T) {
	data := `{"schema_version":"dg-inference/v1","ok":false,"error":{"code":"INSUFFICIENT_DATA","message":"need 181 candles"}}`
	_, err := DecodeResult(strings.NewReader(data))
	var failure *InferenceFailure
	if !errors.As(err, &failure) || failure.Code != "INSUFFICIENT_DATA" {
		t.Fatalf("expected structured error, got %v", err)
	}
	for _, data := range []string{`{}`, `{"schema_version":"v2","ok":true}`, data + `{}`, `{"schema_version":"dg-inference/v1","ok":true,"predictions":[]}`} {
		if _, err := DecodeResult(strings.NewReader(data)); err == nil {
			t.Fatal("expected rejection")
		}
	}
}
