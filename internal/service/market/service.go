package market

import (
	"context"
	"depeg.local/platform/internal/domain"
	entity "depeg.local/platform/internal/domain/market"
	"depeg.local/platform/internal/port"
	"regexp"
	"time"
)

type service struct{ 
	repository port.MarketRepository
	assetCache port.AssetCache
	logger port.Logger
}

var AssetIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,95}$`)
var SourcePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,63}$`)

func New(r port.MarketRepository,a port.AssetCache,l port.Logger) port.MarketService { 
	return &service{
		repository: r,
		assetCache: a,
		logger: l,
	} 
}
func (s *service) ListAssets(ctx context.Context) ([]entity.Asset, error) {
	if err:=ctx.Err(); err != nil {
		return nil, err
	}
	if s.assetCache != nil {
		readctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		assets, found, err := s.assetCache.GetAsset(readctx)
		cancel()
		if err != nil {
			s.logger.Error(ctx,"failed to get asset cache", "error", err)
		} else if found {
			if s.logger != nil {
				s.logger.Info(ctx,"asset cache hit")
			}
			return assets, nil
		}
	}
	if s.logger != nil {
		s.logger.Info(ctx, "loading assets from MySQL")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	assets, err := s.repository.ListAssets(ctx)
	if err != nil {
		return nil, err
	}
	if s.assetCache != nil {
		writectx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		err := s.assetCache.SetAsset(writectx, assets, 60*time.Second); 
		cancel()
		if err != nil {
			s.logger.Error(ctx,"failed to set asset cache", "error", err)
		}
	}
	return assets, nil
}
func (s *service) Trend(ctx context.Context, q entity.Query) (entity.Trend, error) {
	if !AssetIDPattern.MatchString(q.AssetID) || (q.Source != "" && !SourcePattern.MatchString(q.Source)) {
		return entity.Trend{}, domain.ErrInvalid
	}
	if q.From.IsZero() || q.To.IsZero() || !q.To.After(q.From) || q.To.Sub(q.From) > 31*24*time.Hour || q.From.Year() < 1970 || q.To.Year() > 9999 {
		return entity.Trend{}, domain.ErrInvalid
	}
	switch q.Interval {
	case time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, 24 * time.Hour:
	default:
		return entity.Trend{}, domain.ErrInvalid
	}
	if q.Limit == 0 {
		q.Limit = 500
	}
	if q.Limit < 1 || q.Limit > 2000 || len(q.Cursor) > 1024 {
		return entity.Trend{}, domain.ErrInvalid
	}
	q.From = q.From.UTC()
	q.To = q.To.UTC()
	return s.repository.Trend(ctx, q)
}

func (s *service) Snapshots(ctx context.Context, q entity.Query) (entity.SnapshotSeries, error) {
	if !AssetIDPattern.MatchString(q.AssetID) || (q.Source != "" && !SourcePattern.MatchString(q.Source)) || q.From.IsZero() || q.To.IsZero() || !q.To.After(q.From) || q.To.Sub(q.From) > 3660*24*time.Hour || q.From.Year() < 1970 || q.To.Year() > 9999 || len(q.Cursor) > 1024 {
		return entity.SnapshotSeries{}, domain.ErrInvalid
	}
	if q.Limit == 0 {
		q.Limit = 500
	}
	if q.Limit < 1 || q.Limit > 2000 {
		return entity.SnapshotSeries{}, domain.ErrInvalid
	}
	q.From = q.From.UTC()
	q.To = q.To.UTC()
	q.Interval = 24 * time.Hour
	return s.repository.Snapshots(ctx, q)
}
