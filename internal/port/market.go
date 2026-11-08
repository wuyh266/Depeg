package port

import (
	"context"
	"depeg.local/platform/internal/domain/market"
	"time"
)

type MarketRepository interface {
	ListAssets(context.Context) ([]market.Asset, error)
	Trend(context.Context, market.Query) (market.Trend, error)
	Snapshots(context.Context, market.Query) (market.SnapshotSeries, error)
	UpsertCandles(context.Context, []market.Candle) error
	Import(context.Context, market.Asset, []market.Candle) error
}
type TrendCache interface {
	Get(context.Context, market.Query) (market.Trend, error)
	Set(context.Context, market.Query, market.Trend, time.Duration) error
}
type MarketService interface {
	ListAssets(context.Context) ([]market.Asset, error)
	Trend(context.Context, market.Query) (market.Trend, error)
	Snapshots(context.Context, market.Query) (market.SnapshotSeries, error)
}

type AssetCache interface {
	GetAsset(ctx context.Context) (assets []market.Asset,found bool, err error)
	SetAsset(ctx context.Context, assets []market.Asset,TTL time.Duration) error
	InvalidateAsset(ctx context.Context) error
}