package python

import (
	"context"
	"depeg.local/platform/internal/config"
	"depeg.local/platform/internal/domain/market"
	"depeg.local/platform/internal/domain/risk"
	"depeg.local/platform/internal/placeholder"
	"depeg.local/platform/internal/port"
)

// TODO: Add a versioned HTTP gateway around the existing Python pipeline.
// Offline JSON handoff is supported by DecodeResult; HTTP endpoints are not yet deployed.
type client struct {
	settings config.Python
	logger   port.Logger
}

func New(settings config.Python, logger port.Logger) port.PipelineGateway {
	return &client{settings: settings, logger: logger}
}
func (*client) Collect(context.Context, market.Query) ([]market.Candle, error) {
	// TODO: Collect with deadlines, source timestamps, gap checks and bounded retries.
	return nil, placeholder.ErrNotImplemented
}
func (*client) Predict(context.Context, market.Query, []risk.Horizon) ([]risk.Prediction, error) {
	// TODO: Require a checkpoint/scaler/feature-order/sequence-length manifest.
	// TODO: Reject incompatible versions and future information in model inputs.
	return nil, placeholder.ErrNotImplemented
}
