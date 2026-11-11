package python

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"depeg.local/platform/internal/domain/risk"
)

const ResultSchema = "dg-inference/v1"

type InferenceFailure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *InferenceFailure) Error() string { return e.Code + ": " + e.Message }

type resultPrediction struct {
	AssetID           string    `json:"asset_id"`
	Symbol            string    `json:"symbol"`
	Source            string    `json:"source"`
	ObservedAt        time.Time `json:"observed_at"`
	ReferenceOpenTime time.Time `json:"reference_open_time"`
	LastInputOpenTime time.Time `json:"last_input_open_time"`
	Horizon           string    `json:"horizon"`
	StartMinutes      *int      `json:"horizon_start_minutes"`
	EndMinutes        *int      `json:"horizon_end_minutes"`
	PredictionStart   time.Time `json:"prediction_start"`
	PredictionEnd     time.Time `json:"prediction_end"`
	Ratio             *float64  `json:"depeg_minute_ratio"`
	ModelVersion      string    `json:"model_version"`
	FeatureVersion    string    `json:"feature_version"`
	LabelVersion      string    `json:"label_version"`
	Quality           string    `json:"quality"`
}

// DecodeResult validates the offline Python JSON without calling a model or writing to a database.
// integration_only remains visible in Quality and must not be published as verified risk.
func DecodeResult(reader io.Reader) ([]risk.Prediction, error) {
	var envelope struct {
		Schema      string             `json:"schema_version"`
		OK          *bool              `json:"ok"`
		GeneratedAt time.Time          `json:"generated_at"`
		Predictions []resultPrediction `json:"predictions"`
		Failure     *InferenceFailure  `json:"error"`
	}
	decoder := json.NewDecoder(io.LimitReader(reader, 1024*1024+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return nil, fmt.Errorf("invalid inference JSON: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("expected one JSON result")
	}
	if envelope.Schema != ResultSchema || envelope.OK == nil {
		return nil, fmt.Errorf("unsupported inference envelope")
	}
	if !*envelope.OK {
		if envelope.Failure == nil || envelope.Failure.Code == "" || envelope.Failure.Message == "" || len(envelope.Predictions) != 0 {
			return nil, fmt.Errorf("invalid inference error")
		}
		return nil, envelope.Failure
	}
	if envelope.Failure != nil || envelope.GeneratedAt.IsZero() || len(envelope.Predictions) == 0 {
		return nil, fmt.Errorf("incomplete inference result")
	}
	windows := map[string][2]int{"0-3h": {0, 180}, "3-6h": {180, 360}, "6-9h": {360, 540}, "6-12h": {360, 720}, "12-18h": {720, 1080}, "18-24h": {1080, 1440}, "24-30h": {1440, 1800}, "48-54h": {2880, 3240}}
	seen := map[string]bool{}
	var result []risk.Prediction
	for _, p := range envelope.Predictions {
		window, exists := windows[p.Horizon]
		if !exists || seen[p.Horizon] || p.StartMinutes == nil || p.EndMinutes == nil || *p.StartMinutes != window[0] || *p.EndMinutes != window[1] {
			return nil, fmt.Errorf("invalid or duplicate prediction window")
		}
		seen[p.Horizon] = true
		if p.AssetID != "ccxt.usdc-usdt" || p.Symbol != "USDC/USDT" || p.Source != "ccxt:okx" {
			return nil, fmt.Errorf("unsupported prediction market")
		}
		if p.Ratio == nil || math.IsNaN(*p.Ratio) || math.IsInf(*p.Ratio, 0) || *p.Ratio < 0 || *p.Ratio > 1 {
			return nil, fmt.Errorf("invalid depeg minute ratio")
		}
		if strings.TrimSpace(p.ModelVersion) == "" || p.FeatureVersion != "dg-build-features-v3-window10/v1" || p.LabelVersion != "absolute-close-minus-one-ge-0.05/v1" {
			return nil, fmt.Errorf("unsupported model/feature/label version")
		}
		if p.Quality != "verified" && p.Quality != "integration_only" {
			return nil, fmt.Errorf("invalid prediction quality")
		}
		if p.ObservedAt.IsZero() || p.ObservedAt.Unix()%60 != 0 || p.ObservedAt.Nanosecond() != 0 || p.ObservedAt.After(envelope.GeneratedAt) || !p.ObservedAt.Equal(p.ReferenceOpenTime.Add(time.Minute)) || !p.LastInputOpenTime.Equal(p.ReferenceOpenTime.Add(-time.Minute)) || !p.PredictionStart.Equal(p.ObservedAt.Add(time.Duration(*p.StartMinutes)*time.Minute)) || !p.PredictionEnd.Equal(p.ObservedAt.Add(time.Duration(*p.EndMinutes)*time.Minute)) {
			return nil, fmt.Errorf("inconsistent inference timestamps")
		}
		if len(result) > 0 && !p.ObservedAt.Equal(result[0].ObservedAt) {
			return nil, fmt.Errorf("mixed observation times")
		}
		result = append(result, risk.Prediction{AssetID: p.AssetID, Horizon: risk.Horizon{StartMinutes: *p.StartMinutes, EndMinutes: *p.EndMinutes}, DepegMinuteRatio: p.Ratio, ModelVersion: p.ModelVersion, FeatureVersion: p.FeatureVersion, LabelVersion: p.LabelVersion, Source: p.Source, ObservedAt: p.ObservedAt, GeneratedAt: envelope.GeneratedAt, Quality: p.Quality})
	}
	return result, nil
}
