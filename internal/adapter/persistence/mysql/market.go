package mysql

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"gorm.io/gorm/clause"
	"time"

	"depeg.local/platform/internal/domain"
	"depeg.local/platform/internal/domain/market"
	"depeg.local/platform/internal/identity"
	"depeg.local/platform/internal/port"
)

type marketRepository struct{ database Database }

func NewMarketRepository(database Database) port.MarketRepository {
	return &marketRepository{database: database}
}
func asset(row assetModel) market.Asset {
	return market.Asset{ID: row.ID, Symbol: row.Symbol, Chain: row.Chain, TokenAddress: row.TokenAddress, QuoteCurrency: row.QuoteCurrency, AnchorPrice: row.AnchorPrice}
}
func (r *marketRepository) ListAssets(ctx context.Context) ([]market.Asset, error) {
	db, err := r.database.Session(ctx)
	if err != nil {
		return nil, err
	}
	var rows []assetModel
	if err := db.Order("symbol ASC").Order("id ASC").Limit(1000).Find(&rows).Error; err != nil {
		return nil, translate(err)
	}
	result := make([]market.Asset, 0, len(rows))
	for _, row := range rows {
		result = append(result, asset(row))
	}
	return result, nil
}

func (r *marketRepository) LatestOpenTime(ctx context.Context, assetID, source string, interval time.Duration) (time.Time, bool, error) {
	db, err := r.database.Session(ctx)
	if err != nil {
		return time.Time{}, false, err
	}
	var row candleModel
	result := db.Select("open_time").
		Where("asset_id = ? AND source = ? AND interval_seconds = ?", assetID, source, int64(interval/time.Second)).
		Order("open_time DESC").Limit(1).Find(&row)
	if result.Error != nil {
		return time.Time{}, false, translate(result.Error)
	}
	return row.OpenTime.UTC(), result.RowsAffected != 0, nil
}

type marketCursor struct {
	Time   time.Time `json:"time"`
	Source string    `json:"source"`
	Scope  string    `json:"scope"`
}

func scope(q market.Query) string {
	return identity.Digest(fmt.Sprintf("%s|%s|%d|%s|%s", q.AssetID, q.Source, q.Interval, q.From.UTC().Format(time.RFC3339Nano), q.To.UTC().Format(time.RFC3339Nano)))
}
func (r *marketRepository) Trend(ctx context.Context, q market.Query) (market.Trend, error) {
	db, err := r.database.Session(ctx)
	if err != nil {
		return market.Trend{}, err
	}
	var row assetModel
	if err := db.Where("id = ?", q.AssetID).Take(&row).Error; err != nil {
		return market.Trend{}, translate(err)
	}
	query := db.Model(&candleModel{}).Where("asset_id = ? AND interval_seconds = ? AND open_time >= ? AND open_time < ?", q.AssetID, int64(q.Interval/time.Second), q.From, q.To)
	if q.Source != "" {
		query = query.Where("source = ?", q.Source)
	}
	if q.Cursor != "" {
		if len(q.Cursor) > 1024 {
			return market.Trend{}, domain.ErrInvalid
		}
		decoded, err := base64.RawURLEncoding.DecodeString(q.Cursor)
		if err != nil {
			return market.Trend{}, domain.ErrInvalid
		}
		var cursor marketCursor
		if json.Unmarshal(decoded, &cursor) != nil || cursor.Scope != scope(q) || cursor.Time.Before(q.From) || !cursor.Time.Before(q.To) || len(cursor.Source) > 64 {
			return market.Trend{}, domain.ErrInvalid
		}
		query = query.Where("(open_time > ? OR (open_time = ? AND source > ?))", cursor.Time, cursor.Time, cursor.Source)
	}
	var rows []candleModel
	if err := query.Order("open_time ASC").Order("source ASC").Limit(q.Limit + 1).Find(&rows).Error; err != nil {
		return market.Trend{}, translate(err)
	}
	result := market.Trend{Asset: asset(row), Candles: make([]market.Candle, 0), Interval: q.Interval.String(), AsOf: time.Now().UTC(), From: q.From, To: q.To}
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		last := rows[len(rows)-1]
		value, _ := json.Marshal(marketCursor{Time: last.OpenTime.UTC(), Source: last.Source, Scope: scope(q)})
		result.NextCursor = base64.RawURLEncoding.EncodeToString(value)
	}
	for _, row := range rows {
		result.Candles = append(result.Candles, market.Candle{AssetID: row.AssetID, Source: row.Source, Time: row.OpenTime.UTC(), Interval: time.Duration(row.IntervalSeconds) * time.Second, Open: row.Open, High: row.High, Low: row.Low, Close: row.Close, Volume: row.Volume, Quality: row.Quality, SourceTime: row.SourceTime, RawValues: row.RawValues})
	}
	return result, nil
}
func (r *marketRepository) UpsertCandles(ctx context.Context, candles []market.Candle) error {
	if len(candles) == 0 {
		return nil
	}
	return r.database.Transaction(ctx, func(database Database) error {
		db, err := database.Session(ctx)
		if err != nil {
			return err
		}
		rows := make([]candleModel, 0, len(candles))
		for _, c := range candles {
			rows = append(rows, candleModel{AssetID: c.AssetID, Source: c.Source, IntervalSeconds: int64(c.Interval / time.Second), OpenTime: c.Time.UTC(), Open: c.Open, High: c.High, Low: c.Low, Close: c.Close, Volume: c.Volume, Quality: c.Quality, SourceTime: c.SourceTime, RawValues: c.RawValues})
		}
		conflict := clause.OnConflict{
			Columns:   []clause.Column{{Name: "asset_id"}, {Name: "source"}, {Name: "interval_seconds"}, {Name: "open_time"}},
			DoUpdates: clause.AssignmentColumns([]string{"open", "high", "low", "close", "volume", "quality", "source_time", "raw_values", "updated_at"}),
		}
		return writeMarketStreams(db, rows, 200, func(row candleModel) (streamKey, time.Time) {
			return streamKey{AssetID: row.AssetID, Kind: "ohlc", Source: row.Source, IntervalSeconds: row.IntervalSeconds}, row.OpenTime
		}, func(batch []candleModel) error {
			return translate(db.Clauses(conflict).Create(&batch).Error)
		})
	})
}
func (r *marketRepository) Import(ctx context.Context, a market.Asset, candles []market.Candle) error {
	return r.database.Transaction(ctx, func(database Database) error {
		db, err := database.Session(ctx)
		if err != nil {
			return err
		}
		row := assetModel{ID: a.ID, Symbol: a.Symbol, Chain: a.Chain, TokenAddress: a.TokenAddress, QuoteCurrency: a.QuoteCurrency, AnchorPrice: a.AnchorPrice}
		if err := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"symbol", "chain", "token_address", "quote_currency", "anchor_price", "updated_at"})}).Create(&row).Error; err != nil {
			return translate(err)
		}
		if err := NewMarketRepository(database).UpsertCandles(ctx, candles); err != nil {
			return err
		}

		return bumpAssetVersion(ctx, database)
	})
}
