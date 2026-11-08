package port

import (
	"context"

	"depeg.local/platform/internal/domain/market"
	"depeg.local/platform/internal/domain/risk"
)

// PipelineGateway is a future Python-service contract, not an existing endpoint.
// TODO: Define versioned requests, minute alignment and model manifests first.
type PipelineGateway interface {
	Collect(context.Context, market.Query) ([]market.Candle, error)
	Predict(context.Context, market.Query, []risk.Horizon) ([]risk.Prediction, error)
}
