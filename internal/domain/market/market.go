package market

import "time"

type Asset struct {
	ID            string  `json:"id"`
	Symbol        string  `json:"symbol"`
	Chain         string  `json:"chain"`
	TokenAddress  string  `json:"token_address"`
	QuoteCurrency string  `json:"quote_currency"`
	AnchorPrice   *string `json:"anchor_price"`
}
type Candle struct {
	AssetID    string        `json:"asset_id"`
	Source     string        `json:"source"`
	Time       time.Time     `json:"time"`
	Interval   time.Duration `json:"-"`
	Open       string        `json:"open"`
	High       string        `json:"high"`
	Low        string        `json:"low"`
	Close      string        `json:"close"`
	Volume     *string       `json:"volume"`
	Quality    string        `json:"quality"`
	SourceTime *time.Time    `json:"source_time,omitempty"`
	RawValues  string        `json:"-"`
}
type Query struct {
	AssetID  string
	Source   string
	From     time.Time
	To       time.Time
	Interval time.Duration
	Cursor   string
	Limit    int
}
type Trend struct {
	Asset      Asset     `json:"asset"`
	Candles    []Candle  `json:"candles"`
	Interval   string    `json:"interval"`
	NextCursor string    `json:"next_cursor"`
	AsOf       time.Time `json:"as_of"`
	From       time.Time `json:"from"`
	To         time.Time `json:"to"`
}

// Snapshot is a point observation, not a synthetic OHLC candle.
type Snapshot struct {
	AssetID      string    `json:"asset_id"`
	Source       string    `json:"source"`
	Time         time.Time `json:"time"`
	PriceUSD     string    `json:"price_usd"`
	MarketCapUSD *string   `json:"market_cap_usd"`
	Volume24hUSD *string   `json:"volume_24h_usd"`
	QualityFlags []string  `json:"quality_flags"`
	RawValues    string    `json:"-"`
}

type SnapshotSeries struct {
	Asset      Asset      `json:"asset"`
	Points     []Snapshot `json:"points"`
	From       time.Time  `json:"from"`
	To         time.Time  `json:"to"`
	NextCursor string     `json:"next_cursor"`
}
