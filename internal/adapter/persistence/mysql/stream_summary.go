package mysql

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"depeg.local/platform/internal/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type streamKey struct {
	AssetID         string `gorm:"size:96;primaryKey"`
	Kind            string `gorm:"size:8;primaryKey"`
	Source          string `gorm:"size:64;primaryKey"`
	IntervalSeconds int64  `gorm:"primaryKey;autoIncrement:false"`
}

type streamSummaryModel struct {
	Key         streamKey `gorm:"embedded"`
	FirstAt     time.Time `gorm:"not null"`
	LastAt      time.Time `gorm:"not null"`
	SampleCount int64     `gorm:"not null"`
}

func (streamSummaryModel) TableName() string { return "market_stream_summaries" }

// writeMarketStreams runs inside the import transaction. The asset row serializes
// writes even for streams that do not have a summary yet.
func writeMarketStreams[T any](db *gorm.DB, rows []T, batchSize int,
	identity func(T) (streamKey, time.Time), write func([]T) error,
) error {
	keys := make([]streamKey, 0)
	groups := make(map[streamKey][]T)
	isMySQL := db.Dialector.Name() == "mysql"
	for _, row := range rows {
		key, _ := identity(row)
		if isMySQL {
			key.AssetID = strings.ToLower(key.AssetID)
			key.Source = strings.ToLower(key.Source)
		}
		if len(groups[key]) == 0 {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], row)
	}
	slices.SortFunc(keys, func(a, b streamKey) int {
		return cmp.Or(cmp.Compare(a.AssetID, b.AssetID), cmp.Compare(a.Kind, b.Kind),
			cmp.Compare(a.Source, b.Source), cmp.Compare(a.IntervalSeconds, b.IntervalSeconds))
	})
	for _, key := range keys {
		var locked assetModel
		if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id").Where("id = ?", key.AssetID).Take(&locked).Error; err != nil {
			return translate(err)
		}
		summary := streamSummaryModel{Key: key}
		for batch := range slices.Chunk(groups[key], batchSize) {
			values := make([]any, 0, len(batch))
			for _, row := range batch {
				_, stamp := identity(row)
				if isMySQL {
					// Compare using the precision of the persisted natural key.
					values = append(values, gorm.Expr("CAST(? AS DATETIME(3))", stamp))
				} else {
					values = append(values, stamp)
				}
			}
			before, err := storedStreamTimes(db, key, values)
			if err != nil {
				return err
			}
			if err := write(batch); err != nil {
				return err
			}
			after, err := storedStreamTimes(db, key, values)
			if err != nil {
				return err
			}
			if len(after) == 0 || len(after) < len(before) {
				return domain.ErrUnavailable
			}
			summary.SampleCount += int64(len(after) - len(before))
			for _, stamp := range after {
				if summary.FirstAt.IsZero() || stamp.Before(summary.FirstAt) {
					summary.FirstAt = stamp
				}
				if stamp.After(summary.LastAt) {
					summary.LastAt = stamp
				}
			}
		}
		updates := clause.Assignments(map[string]any{
			"first_at":     gorm.Expr("CASE WHEN first_at > ? THEN ? ELSE first_at END", summary.FirstAt, summary.FirstAt),
			"last_at":      gorm.Expr("CASE WHEN last_at < ? THEN ? ELSE last_at END", summary.LastAt, summary.LastAt),
			"sample_count": gorm.Expr("sample_count + ?", summary.SampleCount),
		})
		conflict := clause.OnConflict{
			Columns:   []clause.Column{{Name: "asset_id"}, {Name: "kind"}, {Name: "source"}, {Name: "interval_seconds"}},
			DoUpdates: updates,
		}
		if err := db.Clauses(conflict).Create(&summary).Error; err != nil {
			return translate(err)
		}
	}
	return nil
}

func storedStreamTimes(db *gorm.DB, key streamKey, times []any) ([]time.Time, error) {
	query := db.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("asset_id = ? AND source = ?", key.AssetID, key.Source)
	column := "observed_at"
	if key.Kind == "ohlc" {
		column = "open_time"
		query = query.Model(&candleModel{}).Where("interval_seconds = ?", key.IntervalSeconds)
	} else {
		query = query.Model(&snapshotModel{})
	}
	var stored []time.Time
	err := query.Where(clause.IN{Column: clause.Column{Name: column}, Values: times}).
		Pluck(column, &stored).Error
	return stored, translate(err)
}
