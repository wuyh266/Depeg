package scheduler

import (
	"context"
	"depeg.local/platform/internal/config"
	"depeg.local/platform/internal/placeholder"
	"depeg.local/platform/internal/port"
)

// TODO: Schedule bounded, non-overlapping jobs with cancellation and backoff.
type scheduler struct {
	settings  config.Worker
	ingestion port.IngestionService
	logger    port.Logger
}

func New(settings config.Worker, ingestion port.IngestionService, logger port.Logger) port.Scheduler {
	return &scheduler{settings: settings, ingestion: ingestion, logger: logger}
}
func (*scheduler) Run(context.Context) error {
	// TODO: Tick RunOnce, expose job health and stop cleanly on cancellation.
	return placeholder.ErrNotImplemented
}
