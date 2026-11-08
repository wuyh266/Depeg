package dataset

import (
	"depeg.local/platform/internal/domain/market"
	"time"
)

type Options struct {
	Kind    string
	CoinIDs []string
	// OHLC metadata omits vs_currency, so USD must be explicitly confirmed.
	QuoteCurrency  string
	ExpectedSHA256 string
}

type News struct {
	ID          string
	URL         string
	Title       string
	PublishedAt time.Time
	FetchedAt   time.Time
	Type        string
	SourceName  string
	ImageURL    string
	Authors     []string
	CoinIDs     []string
	RawJSON     string
}

// Batch bounds memory to one asset table, or a small group of news records.
type Batch struct {
	Asset     *market.Asset
	Candles   []market.Candle
	Snapshots []market.Snapshot
	News      []News
}

type AssetReport struct {
	CoinID         string    `json:"coin_id"`
	AssetID        string    `json:"asset_id"`
	Kind           string    `json:"kind"`
	Rows           int64     `json:"rows"`
	First          time.Time `json:"first_source_time"`
	Last           time.Time `json:"last_source_time"`
	MissingPeriods int64     `json:"missing_periods"`
}

type Report struct {
	Mode              string        `json:"mode"`
	SHA256            string        `json:"sha256"`
	Assets            []AssetReport `json:"assets"`
	Candles           int64         `json:"candles"`
	Snapshots         int64         `json:"snapshots"`
	News              int64         `json:"news"`
	MissingVolumes    int64         `json:"missing_volumes"`
	InvalidMarketCaps int64         `json:"invalid_market_caps"`
	RoundedValues     int64         `json:"rounded_values"`
}
