package port

import (
	"context"
	"depeg.local/platform/internal/domain/monitor"
)

type MonitorRepository interface {
	Catalog(context.Context) ([]monitor.Asset, error)
	Rules(context.Context, string) ([]monitor.Rule, error)
	SaveRule(context.Context, monitor.Rule) error
	// Record verifies the rule version while holding its row lock, then writes atomically.
	Record(context.Context, monitor.Rule, []monitor.Event) (int64, error)
	Events(context.Context, monitor.EventQuery) (monitor.EventPage, error)
}

type MonitorService interface {
	Overview(context.Context, string) (monitor.Overview, error)
	SaveRule(context.Context, string, monitor.Rule) (monitor.Rule, error)
	Replay(context.Context, string, monitor.ReplayQuery) (monitor.ReplayResult, error)
	Events(context.Context, monitor.EventQuery) (monitor.EventPage, error)
}
