package mysql

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"depeg.local/platform/internal/domain"
	"depeg.local/platform/internal/domain/monitor"
	"depeg.local/platform/internal/identity"
	"depeg.local/platform/internal/port"
	"gorm.io/gorm/clause"
)

type monitorRepository struct{ database Database }

func NewMonitorRepository(db Database) port.MonitorRepository {
	return &monitorRepository{database: db}
}

type catalogRow struct {
	Asset           assetModel `gorm:"embedded"`
	Kind            string
	StreamSource    string
	IntervalSeconds int64
	FirstAt         time.Time
	LastAt          time.Time
	SampleCount     int64
	Price           string
	SourceTime      *time.Time
	MarketCapUSD    *string
	Volume24hUSD    *string
}

func (r *monitorRepository) Catalog(ctx context.Context) ([]monitor.Asset, error) {
	db, err := r.database.Session(ctx)
	if err != nil {
		return nil, err
	}
	// Select the asset page and its latest points from the same database snapshot.
	assets := db.Model(&assetModel{}).Order("symbol ASC, id ASC").Limit(1000)
	var rows []catalogRow
	err = db.Table("(?) AS a", assets).
		Select(`a.*, m.kind, COALESCE(c.source, s.source) AS stream_source,
			m.interval_seconds, m.first_at, m.last_at, m.sample_count,
			COALESCE(c.close, s.price_usd) AS price, c.source_time,
			s.market_cap_usd, s.volume24h_usd`).
		Joins("LEFT JOIN market_stream_summaries AS m ON m.asset_id = a.id").
		Joins(`LEFT JOIN candles AS c ON m.kind = 'ohlc' AND c.asset_id = m.asset_id
			AND c.source = m.source AND c.interval_seconds = m.interval_seconds AND c.open_time = m.last_at`).
		Joins(`LEFT JOIN market_snapshots AS s ON m.kind = 'daily' AND s.asset_id = m.asset_id
			AND s.source = m.source AND s.observed_at = m.last_at`).
		Order("a.symbol ASC, a.id ASC, m.kind ASC, stream_source ASC, m.interval_seconds ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, translate(err)
	}
	result := make([]monitor.Asset, 0, len(rows))
	for _, row := range rows {
		if len(result) == 0 || result[len(result)-1].Asset.ID != row.Asset.ID {
			result = append(result, monitor.Asset{Asset: asset(row.Asset), Streams: []monitor.Stream{}})
		}
		if row.Kind == "" {
			continue
		}
		last := row.LastAt.UTC()
		observed := last
		if row.Kind == "ohlc" {
			observed = last.Add(time.Duration(row.IntervalSeconds) * time.Second)
			if row.SourceTime != nil {
				observed = row.SourceTime.UTC()
			}
		}
		entry := &result[len(result)-1]
		entry.Streams = append(entry.Streams, monitor.Stream{
			Kind: row.Kind, Source: row.StreamSource, IntervalSeconds: row.IntervalSeconds,
			FirstAt: row.FirstAt.UTC(), LastAt: last, Count: row.SampleCount,
			Latest: monitor.Point{Time: last, ObservedAt: observed, Price: row.Price, MarketCapUSD: row.MarketCapUSD, Volume24hUSD: row.Volume24hUSD},
		})
	}
	return result, nil
}

func (r *monitorRepository) Rules(ctx context.Context, user string) ([]monitor.Rule, error) {
	db, err := r.database.Session(ctx)
	if err != nil {
		return nil, err
	}
	var rows []monitorRuleModel
	if err := db.Where("user_id = ?", user).Order("asset_id").Find(&rows).Error; err != nil {
		return nil, translate(err)
	}
	result := make([]monitor.Rule, 0, len(rows))
	for _, v := range rows {
		result = append(result, monitor.Rule(v))
	}
	return result, nil
}

func (r *monitorRepository) SaveRule(ctx context.Context, rule monitor.Rule) error {
	db, err := r.database.Session(ctx)
	if err != nil {
		return err
	}
	row := monitorRuleModel(rule)
	return translate(db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "asset_id"}}, DoUpdates: clause.AssignmentColumns([]string{"kind", "source", "interval_seconds", "anchor_price", "warning_bps", "critical_bps", "version", "updated_at"})}).Create(&row).Error)
}

func (r *monitorRepository) Record(ctx context.Context, rule monitor.Rule, events []monitor.Event) (int64, error) {
	var inserted int64
	err := r.database.Transaction(ctx, func(database Database) error {
		db, err := database.Session(ctx)
		if err != nil {
			return err
		}
		var current monitorRuleModel
		if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND asset_id = ?", rule.UserID, rule.AssetID).Take(&current).Error; err != nil {
			return translate(err)
		}
		if current.Version != rule.Version {
			return domain.ErrConflict
		}
		rows := make([]monitorEventModel, 0, len(events))
		for _, event := range events {
			if event.UserID != rule.UserID || event.AssetID != rule.AssetID || event.RuleVersion != rule.Version {
				return domain.ErrInvalid
			}
			rows = append(rows, monitorEventModel(event))
		}
		if len(rows) == 0 {
			return nil
		}
		write := db.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(rows, 200)
		inserted = write.RowsAffected
		return translate(write.Error)
	})
	if err != nil {
		return 0, err
	}
	return inserted, nil
}

type eventCursor struct {
	Time  time.Time `json:"time"`
	ID    string    `json:"id"`
	Scope string    `json:"scope"`
}

func (r *monitorRepository) Events(ctx context.Context, q monitor.EventQuery) (monitor.EventPage, error) {
	result := monitor.EventPage{Events: []monitor.Event{}}
	db, err := r.database.Session(ctx)
	if err != nil {
		return result, err
	}
	query := db.Model(&monitorEventModel{}).Where("user_id = ?", q.UserID)
	if q.AssetID != "" {
		query = query.Where("asset_id = ?", q.AssetID)
	}
	scope := identity.Digest(q.UserID + "|" + q.AssetID)
	if q.Cursor != "" {
		raw, e := base64.RawURLEncoding.DecodeString(q.Cursor)
		if e != nil {
			return result, domain.ErrInvalid
		}
		var c eventCursor
		if json.Unmarshal(raw, &c) != nil || c.Scope != scope || len(c.ID) != 64 || c.Time.IsZero() {
			return result, domain.ErrInvalid
		}
		query = query.Where("(observed_at < ? OR (observed_at = ? AND id < ?))", c.Time, c.Time, c.ID)
	}
	var rows []monitorEventModel
	if err := query.Order("observed_at DESC, id DESC").Limit(q.Limit + 1).Find(&rows).Error; err != nil {
		return result, translate(err)
	}
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		last := rows[len(rows)-1]
		raw, _ := json.Marshal(eventCursor{Time: last.ObservedAt.UTC(), ID: last.ID, Scope: scope})
		result.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	for _, row := range rows {
		e := monitor.Event(row)
		e.Time = e.Time.UTC()
		e.ObservedAt = e.ObservedAt.UTC()
		e.DetectedAt = e.DetectedAt.UTC()
		result.Events = append(result.Events, e)
	}
	return result, nil
}
