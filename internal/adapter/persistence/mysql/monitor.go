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

// Aggregates are read as strings because SQLite and MySQL return different driver types.
func catalogTime(v string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05.999999999Z07:00", "2006-01-02 15:04:05.999999999"} {
		if t, e := time.Parse(layout, v); e == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, domain.ErrUnavailable
}

func (r *monitorRepository) Catalog(ctx context.Context) ([]monitor.Asset, error) {
	db, err := r.database.Session(ctx)
	if err != nil {
		return nil, err
	}
	var assets []assetModel
	if err := db.Order("symbol ASC, id ASC").Limit(1000).Find(&assets).Error; err != nil {
		return nil, translate(err)
	}
	result := make([]monitor.Asset, 0, len(assets))
	index := map[string]int{}
	for _, a := range assets {
		index[a.ID] = len(result)
		result = append(result, monitor.Asset{Asset: asset(a), Streams: []monitor.Stream{}})
	}
	// One indexed aggregation per dataset, not one history query per asset.
	dailyGroup := db.Model(&snapshotModel{}).Select("asset_id, source, MIN(observed_at) AS first_at, MAX(observed_at) AS last_at, COUNT(*) AS sample_count").Group("asset_id, source")
	var daily []struct {
		Snapshot    snapshotModel `gorm:"embedded"`
		FirstAt     string
		SampleCount int64
	}
	if err := db.Table("market_snapshots AS s").Select("s.*, g.first_at, g.sample_count").Joins("JOIN (?) AS g ON s.asset_id = g.asset_id AND s.source = g.source AND s.observed_at = g.last_at", dailyGroup).Order("s.asset_id, s.source").Scan(&daily).Error; err != nil {
		return nil, translate(err)
	}
	for _, entry := range daily {
		row := entry.Snapshot
		i, ok := index[row.AssetID]
		if !ok {
			continue
		}
		first, e := catalogTime(entry.FirstAt)
		if e != nil {
			return nil, e
		}
		result[i].Streams = append(result[i].Streams, monitor.Stream{Kind: "daily", Source: row.Source, IntervalSeconds: 86400, FirstAt: first, LastAt: row.ObservedAt.UTC(), Count: entry.SampleCount, Latest: monitor.Point{Time: row.ObservedAt.UTC(), ObservedAt: row.ObservedAt.UTC(), Price: row.PriceUSD, MarketCapUSD: row.MarketCapUSD, Volume24hUSD: row.Volume24hUSD}})
	}
	hourlyGroup := db.Model(&candleModel{}).Select("asset_id, source, interval_seconds, MIN(open_time) AS first_at, MAX(open_time) AS last_at, COUNT(*) AS sample_count").Group("asset_id, source, interval_seconds")
	var hourly []struct {
		Candle      candleModel `gorm:"embedded"`
		FirstAt     string
		SampleCount int64
	}
	if err := db.Table("candles AS c").Select("c.*, g.first_at, g.sample_count").Joins("JOIN (?) AS g ON c.asset_id = g.asset_id AND c.source = g.source AND c.interval_seconds = g.interval_seconds AND c.open_time = g.last_at", hourlyGroup).Order("c.asset_id, c.source, c.interval_seconds").Scan(&hourly).Error; err != nil {
		return nil, translate(err)
	}
	for _, entry := range hourly {
		row := entry.Candle
		i, ok := index[row.AssetID]
		if !ok {
			continue
		}
		first, e := catalogTime(entry.FirstAt)
		if e != nil {
			return nil, e
		}
		observed := row.OpenTime.Add(time.Duration(row.IntervalSeconds) * time.Second)
		if row.SourceTime != nil {
			observed = *row.SourceTime
		}
		result[i].Streams = append(result[i].Streams, monitor.Stream{Kind: "ohlc", Source: row.Source, IntervalSeconds: row.IntervalSeconds, FirstAt: first, LastAt: row.OpenTime.UTC(), Count: entry.SampleCount, Latest: monitor.Point{Time: row.OpenTime.UTC(), ObservedAt: observed.UTC(), Price: row.Close}})
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
