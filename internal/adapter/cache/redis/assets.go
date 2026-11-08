package redis
import (
	"context"
	"errors"
	"encoding/json"
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
const assetCacheKey = "depeg:dev:v1:assets"
func (a *assetCache) GetAsset(ctx context.Context) (assets []market.Asset, found bool, err error) {
	data, err := a.client.Get(ctx, assetCacheKey)
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("get asset cache: %w", err)
	}
	if err := json.Unmarshal(data, &assets); err != nil {
		return nil, false, fmt.Errorf("unmarshal asset cache: %w", err)
	}
	
	return assets, true, err
}
func (a *assetCache) SetAsset(ctx context.Context, assets []market.Asset, TTL time.Duration) error {
	data, err := json.Marshal(assets)
	if err != nil {
		return fmt.Errorf("marshal asset cache: %w", err)
	}
	return a.client.Set(ctx, assetCacheKey, data, TTL)
}
func (a *assetCache) InvalidateAsset(ctx context.Context) error {
	return a.client.Delete(ctx, assetCacheKey)
}