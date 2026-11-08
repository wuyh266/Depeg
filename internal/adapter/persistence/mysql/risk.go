package mysql

import (
	"context"
	domain "depeg.local/platform/internal/domain/risk"
	"depeg.local/platform/internal/placeholder"
	"depeg.local/platform/internal/port"
)

// TODO: Include horizon, model, feature and label versions in prediction uniqueness.
type riskRepository struct{ database Database }

func NewRiskRepository(database Database) port.RiskRepository {
	return &riskRepository{database: database}
}
func (*riskRepository) Series(context.Context, domain.Query) (domain.Series, error) {
	// TODO: Use a context-bound GORM session for queries/upserts; never fabricate rows.
	return domain.Series{}, placeholder.ErrNotImplemented
}

func (*riskRepository) UpsertPredictions(context.Context, []domain.Prediction) error {
	// TODO: Use a context-bound GORM session for queries/upserts; never fabricate rows.
	return placeholder.ErrNotImplemented
}
