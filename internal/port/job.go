package port

import (
	"context"
	"time"

	"depeg.local/platform/internal/domain/job"
)

// TODO: Commit data and checkpoints atomically and reject stale fencing tokens.
type JobRepository interface {
	Load(context.Context, string) (job.Checkpoint, error)
	Save(context.Context, job.Checkpoint, uint64) error
}

type LeaseStore interface {
	Acquire(context.Context, string, time.Duration) (job.Lease, error)
	Renew(context.Context, job.Lease, time.Duration) (job.Lease, error)
	Release(context.Context, job.Lease) error
}

type IngestionService interface {
	RunOnce(context.Context) error
}
