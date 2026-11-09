package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"depeg.local/platform/internal/domain/market"
	"depeg.local/platform/internal/port"

	"github.com/redis/go-redis/v9"
)

// type AssetCache interface {
// 	GetAsset(ctx context.Context) (assets []market.Asset,found bool, err error)
// 	SetAsset(ctx context.Context, assets []market.Asset,TTL time.Duration) error
// 	InvalidateAsset(ctx context.Context) error
// }
type assetCache struct {
	client port.RedisClient
}

func NewAssetCache(client port.RedisClient) *assetCache {
	return &assetCache{client: client}
}
func assetCacheKey(version uint64) string {
	return fmt.Sprintf("depeg:dev:v1:assets:%d", version)
}
func (a *assetCache) GetAsset(ctx context.Context,version uint64) (assets []market.Asset, found bool, err error) {
	data, err := a.client.Get(ctx, assetCacheKey(version))
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("get asset cache: %w", err)
	}
	if err := json.Unmarshal(data, &assets); err != nil {
		return nil, false, fmt.Errorf("unmarshal asset cache: %w", err)
	}

	if assets == nil {
		return nil, false, fmt.Errorf("asset cache must contain an array")
	}

	return assets, true, nil
	
}
func (cache *assetCache) SetAsset(ctx context.Context,version uint64,assets []market.Asset,ttl time.Duration,) error {
	if ttl <= 0 {
		return fmt.Errorf("asset cache TTL must be positive")
	}

	if assets == nil {
		assets = []market.Asset{}
	}

	data, err := json.Marshal(assets)
	if err != nil {
		return fmt.Errorf("marshal asset cache: %w", err)
	}

	return cache.client.Set(ctx, assetCacheKey(version), data, ttl)
}
func (a *assetCache) InvalidateAsset(ctx context.Context,version uint64) error {
	return a.client.Delete(ctx, assetCacheKey(version))
}