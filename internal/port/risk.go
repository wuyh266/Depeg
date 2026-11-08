package port

import (
	"context"

	"depeg.local/platform/internal/domain/risk"
)

// TODO: Preserve model, feature and label versions during reads and writes.
type RiskRepository interface {
	Series(context.Context, risk.Query) (risk.Series, error)
	UpsertPredictions(context.Context, []risk.Prediction) error
}

type RiskService interface {
	Series(context.Context, risk.Query) (risk.Series, error)
}
