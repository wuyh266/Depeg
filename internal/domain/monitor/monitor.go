// Package monitor describes observed price deviations, not model predictions.
package monitor

import (
	"depeg.local/platform/internal/domain/market"
	"time"
)

type Point struct {
	Time         time.Time `json:"time"`
	ObservedAt   time.Time `json:"observed_at"`
	Price        string    `json:"price"`
	MarketCapUSD *string   `json:"market_cap_usd"`
	Volume24hUSD *string   `json:"volume_24h_usd"`
}

// Stream identifies a single source and cadence; sources are never averaged.
type Stream struct {
	Kind            string    `json:"kind"`
	Source          string    `json:"source"`
	IntervalSeconds int64     `json:"interval_seconds"`
	FirstAt         time.Time `json:"first_at"`
	LastAt          time.Time `json:"last_at"`
	Count           int64     `json:"count"`
	Latest          Point     `json:"latest"`
}

type Rule struct {
	UserID          string    `json:"-"`
	AssetID         string    `json:"asset_id"`
	Kind            string    `json:"kind"`
	Source          string    `json:"source"`
	IntervalSeconds int64     `json:"interval_seconds"`
	AnchorPrice     string    `json:"anchor_price"`
	WarningBPS      int       `json:"warning_bps"`
	CriticalBPS     int       `json:"critical_bps"`
	Version         string    `json:"version"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type Asset struct {
	Asset        market.Asset `json:"asset"`
	Streams      []Stream     `json:"streams"`
	Selected     *Stream      `json:"selected"`
	Rule         *Rule        `json:"rule"`
	AnchorPrice  *string      `json:"anchor_price"`
	AnchorOrigin string       `json:"anchor_origin"`
	DeviationBPS *string      `json:"deviation_bps"`
	Status       string       `json:"status"`
	Freshness    string       `json:"freshness"`
	WarningBPS   int          `json:"warning_bps"`
	CriticalBPS  int          `json:"critical_bps"`
}

type Overview struct {
	Assets []Asset   `json:"assets"`
	AsOf   time.Time `json:"as_of"`
}

// Event is one threshold-exceeding observation. It does not assert incident onset.
type Event struct {
	ID              string    `json:"id"`
	UserID          string    `json:"-"`
	AssetID         string    `json:"asset_id"`
	Kind            string    `json:"kind"`
	Source          string    `json:"source"`
	IntervalSeconds int64     `json:"interval_seconds"`
	Time            time.Time `json:"time"`
	ObservedAt      time.Time `json:"observed_at"`
	Price           string    `json:"price"`
	AnchorPrice     string    `json:"anchor_price"`
	DeviationBPS    string    `json:"deviation_bps"`
	Severity        string    `json:"severity"`
	Direction       string    `json:"direction"`
	WarningBPS      int       `json:"warning_bps"`
	CriticalBPS     int       `json:"critical_bps"`
	RuleVersion     string    `json:"rule_version"`
	DetectedAt      time.Time `json:"detected_at"`
}

type ReplayQuery struct {
	AssetID     string    `json:"asset_id"`
	From        time.Time `json:"from"`
	To          time.Time `json:"to"`
	RuleVersion string    `json:"rule_version"`
}

type ReplayResult struct {
	Scanned     int       `json:"scanned"`
	Matched     int       `json:"matched"`
	Inserted    int64     `json:"inserted"`
	RuleVersion string    `json:"rule_version"`
	From        time.Time `json:"from"`
	To          time.Time `json:"to"`
}

type EventQuery struct {
	UserID  string
	AssetID string
	Cursor  string
	Limit   int
}

type EventPage struct {
	Events     []Event `json:"events"`
	NextCursor string  `json:"next_cursor"`
}
