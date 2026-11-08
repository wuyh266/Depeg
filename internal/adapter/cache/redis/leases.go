package redis

import (
	"context"
	"depeg.local/platform/internal/domain/job"
	"depeg.local/platform/internal/placeholder"
	"depeg.local/platform/internal/port"
	"time"
)

// TODO: Implement atomic owner-checked leases and monotonically increasing fencing.
type leases struct{ client port.RedisClient }

func NewLeaseStore(client port.RedisClient) port.LeaseStore { return &leases{client: client} }
func (*leases) Acquire(context.Context, string, time.Duration) (job.Lease, error) {
	// TODO: Acquire only when absent and return a fencing token.
	return job.Lease{}, placeholder.ErrNotImplemented
}
func (*leases) Renew(context.Context, job.Lease, time.Duration) (job.Lease, error) {
	// TODO: Renew only if owner and fencing token still match.
	return job.Lease{}, placeholder.ErrNotImplemented
}
func (*leases) Release(context.Context, job.Lease) error {
	// TODO: Compare owner before deleting; never unlock another worker's lease.
	return placeholder.ErrNotImplemented
}
