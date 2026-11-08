package port

import (
	"context"
	"time"
)

// TODO: Connect real drivers; business services receive repository interfaces.
// ORM-specific sessions and transactions belong to the MySQL adapter.
type Database interface {
	Ping(context.Context) error
	Close() error
}

type RedisClient interface {
	Ping(context.Context) error
	Get(context.Context, string) ([]byte, error)
	Set(context.Context, string, []byte, time.Duration) error
	Delete(context.Context, string) error
	Eval(context.Context, string, []string, ...any) (any, error)
	Close() error
}

type Logger interface {
	Info(context.Context, string, ...any)
	Error(context.Context, string, ...any)
}
