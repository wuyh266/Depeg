package job

import "time"

// TODO: Define durable checkpoints, retry limits and idempotency-key uniqueness.
type Checkpoint struct {
	JobID          string
	AssetID        string
	Source         string
	LastObservedAt time.Time
	UpdatedAt      time.Time
}

// TODO: Use owner-checked renew/release and fencing to reject expired workers.
type Lease struct {
	Key          string
	Owner        string
	FencingToken uint64
	ExpiresAt    time.Time
}
