package mysql

import (
	"context"
	"depeg.local/platform/internal/domain"
	"depeg.local/platform/internal/domain/market"
	"depeg.local/platform/internal/identity"
	"encoding/base64"
	"encoding/json"
)

func (r *marketRepository) Snapshots(ctx context.Context, q market.Query) (market.SnapshotSeries, error) {
	db, err := r.database.Session(ctx)
	if err != nil {
		return market.SnapshotSeries{}, err
	}
	var a assetModel
	if err := db.Where("id = ?", q.AssetID).Take(&a).Error; err != nil {
		return market.SnapshotSeries{}, translate(err)
	}
	query := db.Model(&snapshotModel{}).Omit("raw_values").Where("asset_id = ? AND observed_at >= ? AND observed_at < ?", q.AssetID, q.From, q.To)
	if q.Source != "" {
		query = query.Where("source = ?", q.Source)
	}
	cursorScope := identity.Digest("snapshots|" + scope(q))
	if q.Cursor != "" {
		if len(q.Cursor) > 1024 {
			return market.SnapshotSeries{}, domain.ErrInvalid
		}
		data, err := base64.RawURLEncoding.DecodeString(q.Cursor)
		if err != nil {
			return market.SnapshotSeries{}, domain.ErrInvalid
		}
		var cursor marketCursor
		if json.Unmarshal(data, &cursor) != nil || cursor.Scope != cursorScope || cursor.Time.Before(q.From) || !cursor.Time.Before(q.To) || len(cursor.Source) > 64 {
			return market.SnapshotSeries{}, domain.ErrInvalid
		}
		query = query.Where("(observed_at > ? OR (observed_at = ? AND source > ?))", cursor.Time, cursor.Time, cursor.Source)
	}
	var rows []snapshotModel
	if err := query.Order("observed_at ASC").Order("source ASC").Limit(q.Limit + 1).Find(&rows).Error; err != nil {
		return market.SnapshotSeries{}, translate(err)
	}
	result := market.SnapshotSeries{Asset: asset(a), Points: make([]market.Snapshot, 0), From: q.From, To: q.To}
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		last := rows[len(rows)-1]
		value, _ := json.Marshal(marketCursor{Time: last.ObservedAt.UTC(), Source: last.Source, Scope: cursorScope})
		result.NextCursor = base64.RawURLEncoding.EncodeToString(value)
	}
	for _, row := range rows {
		flags := []string{}
		if json.Unmarshal([]byte(row.QualityFlags), &flags) != nil {
			return market.SnapshotSeries{}, domain.ErrUnavailable
		}
		result.Points = append(result.Points, market.Snapshot{AssetID: row.AssetID, Source: row.Source, Time: row.ObservedAt.UTC(), PriceUSD: row.PriceUSD, MarketCapUSD: row.MarketCapUSD, Volume24hUSD: row.Volume24hUSD, QualityFlags: flags})
	}
	return result, nil
}
