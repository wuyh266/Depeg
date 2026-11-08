package redis

import (
	"context"
	"depeg.local/platform/internal/domain/auth"
	"depeg.local/platform/internal/placeholder"
	"depeg.local/platform/internal/port"
)

// TODO: Store token digests with TTL; rotate/revoke atomically and fail closed.
type sessions struct{ client port.RedisClient }

func NewSessionStore(client port.RedisClient) port.SessionStore { return &sessions{client: client} }
func (*sessions) Save(context.Context, auth.Session) error {
	// TODO: Persist a refresh-token digest and session metadata.
	return placeholder.ErrNotImplemented
}
func (*sessions) Rotate(context.Context, string, auth.Session) error {
	// TODO: Compare old digest, consume it once and save the replacement atomically.
	return placeholder.ErrNotImplemented
}
func (*sessions) Revoked(context.Context, string) (bool, error) {
	// TODO: Check revocation; unavailable Redis must never authorize a request.
	return true, placeholder.ErrNotImplemented
}
func (*sessions) Revoke(context.Context, string) error {
	// TODO: Revoke with a TTL covering outstanding token lifetime.
	return placeholder.ErrNotImplemented
}

func (*sessions) Find(context.Context, string) (auth.Session, error) {
	return auth.Session{}, placeholder.ErrNotImplemented
}
