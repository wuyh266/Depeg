package redis

import (
	"context"
	"depeg.local/platform/internal/domain/market"
	"depeg.local/platform/internal/placeholder"
	"depeg.local/platform/internal/port"
	"time"
)

// TODO: Include asset, source, interval, time range, cursor and limit in cache keys.
type trends struct{ client port.RedisClient }

func NewTrendCache(client port.RedisClient) port.TrendCache { return &trends{client: client} }
func (*trends) Get(context.Context, market.Query) (market.Trend, error) {
	// TODO: Decode a versioned cache entry and expose missing/stale data explicitly.
	return market.Trend{}, placeholder.ErrNotImplemented
}
func (*trends) Set(context.Context, market.Query, market.Trend, time.Duration) error {
	// TODO: Cache only validated data with explicit TTL.
	return placeholder.ErrNotImplemented
}
