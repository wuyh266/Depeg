package risk

import (
	"context"
	domain "depeg.local/platform/internal/domain/risk"
	"depeg.local/platform/internal/placeholder"
	"depeg.local/platform/internal/port"
)

// TODO: Serve versioned risk series; keep model inference outside API requests.
type service struct{ repository port.RiskRepository }

func New(repository port.RiskRepository) port.RiskService {
	return &service{repository: repository}
}
func (*service) Series(context.Context, domain.Query) (domain.Series, error) {
	// TODO: Validate horizon and versions; distinguish missing values from zero.
	return domain.Series{}, placeholder.ErrNotImplemented
}
