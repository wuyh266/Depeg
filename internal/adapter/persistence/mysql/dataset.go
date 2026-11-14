package mysql

import (
	"context"
	"depeg.local/platform/internal/domain"
	"depeg.local/platform/internal/domain/dataset"
	"depeg.local/platform/internal/port"
	"encoding/json"
	"time"

	"gorm.io/gorm/clause"
)

type datasetRepository struct{ database Database }
type datasetSink struct{ database Database }

func NewDatasetRepository(database Database) port.DatasetRepository {
	return &datasetRepository{database: database}
}

func (r *datasetRepository) Import(ctx context.Context, write func(port.DatasetSink) error) error {
	if write == nil {
		return domain.ErrInvalid
	}
	return r.database.Transaction(ctx, func(database Database) error {
		if err := write(&datasetSink{database: database}); err != nil {
			return err
		}

		return bumpAssetVersion(ctx, database)
	})
}

func (s *datasetSink) Write(ctx context.Context, batch dataset.Batch) error {
	db, err := s.database.Session(ctx)
	if err != nil {
		return err
	}
	if batch.Asset != nil {
		a := batch.Asset
		row := assetModel{ID: a.ID, Symbol: a.Symbol, Chain: a.Chain, TokenAddress: a.TokenAddress, QuoteCurrency: a.QuoteCurrency, AnchorPrice: a.AnchorPrice}
		// Create missing assets only. A duplicate ID keeps every existing setting.
		if err := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoNothing: true}).Create(&row).Error; err != nil {
			return translate(err)
		}
		var existing assetModel
		// Read the current row, including a concurrent insert, in this transaction.
		if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("quote_currency").Where("id = ?", a.ID).Take(&existing).Error; err != nil {
			return translate(err)
		}
		if existing.QuoteCurrency != a.QuoteCurrency {
			return domain.ErrConflict
		}
	}
	if err := NewMarketRepository(s.database).UpsertCandles(ctx, batch.Candles); err != nil {
		return err
	}
	if len(batch.Snapshots) > 0 {
		rows := make([]snapshotModel, 0, len(batch.Snapshots))
		for _, point := range batch.Snapshots {
			flags := point.QualityFlags
			if flags == nil {
				flags = []string{}
			}
			encoded, err := json.Marshal(flags)
			if err != nil {
				return domain.ErrInvalid
			}
			rows = append(rows, snapshotModel{AssetID: point.AssetID, Source: point.Source, ObservedAt: point.Time.UTC(), PriceUSD: point.PriceUSD, MarketCapUSD: point.MarketCapUSD, Volume24hUSD: point.Volume24hUSD, QualityFlags: string(encoded), RawValues: point.RawValues})
		}
		conflict := clause.OnConflict{
			Columns:   []clause.Column{{Name: "asset_id"}, {Name: "source"}, {Name: "observed_at"}},
			DoUpdates: clause.AssignmentColumns([]string{"price_usd", "market_cap_usd", "volume24h_usd", "quality_flags", "raw_values", "updated_at"}),
		}
		if err := writeMarketStreams(db, rows, 100, func(row snapshotModel) (streamKey, time.Time) {
			return streamKey{AssetID: row.AssetID, Kind: "daily", Source: row.Source, IntervalSeconds: 86400}, row.ObservedAt
		}, func(batch []snapshotModel) error {
			return translate(db.Clauses(conflict).Create(&batch).Error)
		}); err != nil {
			return err
		}
	}
	if len(batch.News) > 0 {
		rows := make([]newsModel, 0, len(batch.News))
		for _, item := range batch.News {
			authors, err := json.Marshal(item.Authors)
			if err != nil {
				return domain.ErrInvalid
			}
			coins, err := json.Marshal(item.CoinIDs)
			if err != nil {
				return domain.ErrInvalid
			}
			rows = append(rows, newsModel{ID: item.ID, URL: item.URL, Title: item.Title, PublishedAt: item.PublishedAt.UTC(), FetchedAt: item.FetchedAt.UTC(), Type: item.Type, SourceName: item.SourceName, ImageURL: item.ImageURL, Authors: string(authors), CoinIDs: string(coins), RawJSON: item.RawJSON})
		}
		if err := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"url", "title", "published_at", "fetched_at", "type", "source_name", "image_url", "authors", "coin_ids", "raw_json", "updated_at"})}).CreateInBatches(rows, 100).Error; err != nil {
			return translate(err)
		}
	}
	return nil
}
