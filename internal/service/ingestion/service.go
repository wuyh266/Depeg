package ingestion

import (
	"context"
	"depeg.local/platform/internal/placeholder"
	"depeg.local/platform/internal/port"
)

// TODO: Define collection windows, idempotency and a data/checkpoint transaction.
type service struct {
	market   port.MarketRepository
	risks    port.RiskRepository
	jobs     port.JobRepository
	leases   port.LeaseStore
	pipeline port.PipelineGateway
	logger   port.Logger
}

func New(market port.MarketRepository, risks port.RiskRepository, jobs port.JobRepository, leases port.LeaseStore, pipeline port.PipelineGateway, logger port.Logger) port.IngestionService {
	return &service{market: market, risks: risks, jobs: jobs, leases: leases, pipeline: pipeline, logger: logger}
}
func (*service) RunOnce(context.Context) error {
	// TODO: Acquire/renew a fenced lease, collect, infer and atomically checkpoint.
	// TODO: Reject future observations, gaps and incompatible model manifests.
	return placeholder.ErrNotImplemented
}
