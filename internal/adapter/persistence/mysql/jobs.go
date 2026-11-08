package mysql

import (
	"context"
	domain "depeg.local/platform/internal/domain/job"
	"depeg.local/platform/internal/placeholder"
	"depeg.local/platform/internal/port"
)

// TODO: Persist checkpoints transactionally and compare fencing tokens.
type jobRepository struct{ database Database }

func NewJobRepository(database Database) port.JobRepository {
	return &jobRepository{database: database}
}
func (*jobRepository) Load(context.Context, string) (domain.Checkpoint, error) {
	// TODO: Use a context-bound GORM session for queries/upserts; never fabricate rows.
	return domain.Checkpoint{}, placeholder.ErrNotImplemented
}

func (*jobRepository) Save(context.Context, domain.Checkpoint, uint64) error {
	// TODO: Use a context-bound GORM session for queries/upserts; never fabricate rows.
	return placeholder.ErrNotImplemented
}
