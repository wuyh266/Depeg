package port

import (
	"context"
	"net/http"
)

// TODO: Start the configured listener and implement graceful shutdown.
// The handler boundary supports in-process checks without exposing Gin to services.
type Server interface {
	Handler() http.Handler
	Run(context.Context) error
}

type Scheduler interface {
	Run(context.Context) error
}
