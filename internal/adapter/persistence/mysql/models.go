package mysql

import "time"

type userModel struct {
	ID              string  `gorm:"size:32;primaryKey"`
	Username        string  `gorm:"size:32;uniqueIndex:uq_users_username;not null"`
	Email           *string `gorm:"size:254;uniqueIndex:uq_users_email"`
	EmailVerifiedAt *time.Time
	PasswordHash    string `gorm:"size:100;not null"`
	Disabled        bool   `gorm:"not null;default:false"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (userModel) TableName() string { return "users" }

type sessionModel struct {
	ID          string    `gorm:"size:32;primaryKey"`
	UserID      string    `gorm:"size:32;index;not null"`
	RefreshHash string    `gorm:"size:64;uniqueIndex;not null"`
	ExpiresAt   time.Time `gorm:"index;not null"`
	RevokedAt   *time.Time
	CreatedAt   time.Time
}

func (sessionModel) TableName() string { return "auth_sessions" }

type assetModel struct {
	ID            string  `gorm:"size:96;primaryKey"`
	Symbol        string  `gorm:"size:32;not null"`
	Chain         string  `gorm:"size:64;not null"`
	TokenAddress  string  `gorm:"size:128;not null"`
	QuoteCurrency string  `gorm:"size:16;not null"`
	AnchorPrice   *string `gorm:"type:decimal(38,18)"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (assetModel) TableName() string { return "assets" }

type candleModel struct {
	ID              uint64    `gorm:"primaryKey;autoIncrement"`
	AssetID         string    `gorm:"size:96;uniqueIndex:uq_candles,priority:1;not null"`
	Source          string    `gorm:"size:64;uniqueIndex:uq_candles,priority:2;not null"`
	IntervalSeconds int64     `gorm:"uniqueIndex:uq_candles,priority:3;not null"`
	OpenTime        time.Time `gorm:"uniqueIndex:uq_candles,priority:4;not null"`
	Open            string    `gorm:"type:decimal(38,18);not null"`
	High            string    `gorm:"type:decimal(38,18);not null"`
	Low             string    `gorm:"type:decimal(38,18);not null"`
	Close           string    `gorm:"type:decimal(38,18);not null"`
	Volume          *string   `gorm:"type:decimal(38,18)"`
	Quality         string    `gorm:"size:32;not null"`
	SourceTime      *time.Time
	RawValues       string `gorm:"type:longtext"`
	UpdatedAt       time.Time
}

func (candleModel) TableName() string { return "candles" }

// SchemaModels supports isolated schema fixtures. Production uses reviewed SQL migrations.
func SchemaModels() []any {
	return []any{&userModel{}, &sessionModel{}, &emailCodeModel{}, &passwordResetCodeModel{}, &emailCodeSendModel{}, &assetModel{}, &candleModel{}, &snapshotModel{}, &newsModel{}, &monitorRuleModel{}, &monitorEventModel{}, &streamSummaryModel{}}
}

type snapshotModel struct {
	ID           uint64    `gorm:"primaryKey;autoIncrement"`
	AssetID      string    `gorm:"size:96;not null;uniqueIndex:uq_snapshots,priority:1"`
	Source       string    `gorm:"size:64;not null;uniqueIndex:uq_snapshots,priority:2"`
	ObservedAt   time.Time `gorm:"not null;uniqueIndex:uq_snapshots,priority:3"`
	PriceUSD     string    `gorm:"type:decimal(38,18);not null"`
	MarketCapUSD *string   `gorm:"type:decimal(38,18)"`
	Volume24hUSD *string   `gorm:"type:decimal(38,18)"`
	QualityFlags string    `gorm:"type:longtext;not null"`
	RawValues    string    `gorm:"type:longtext;not null"`
	UpdatedAt    time.Time
}

func (snapshotModel) TableName() string { return "market_snapshots" }

type newsModel struct {
	ID          string    `gorm:"size:64;primaryKey"`
	URL         string    `gorm:"type:text;not null"`
	Title       string    `gorm:"type:text;not null"`
	PublishedAt time.Time `gorm:"not null;index:idx_news_time"`
	FetchedAt   time.Time `gorm:"not null"`
	Type        string    `gorm:"size:32;not null"`
	SourceName  string    `gorm:"size:256;not null"`
	ImageURL    string    `gorm:"type:text;not null"`
	Authors     string    `gorm:"type:longtext;not null"`
	CoinIDs     string    `gorm:"type:longtext;not null"`
	RawJSON     string    `gorm:"type:longtext;not null"`
	UpdatedAt   time.Time
}

func (newsModel) TableName() string { return "news_articles" }

type monitorRuleModel struct {
	UserID          string `gorm:"size:32;primaryKey"`
	AssetID         string `gorm:"size:96;primaryKey"`
	Kind            string `gorm:"size:16;not null"`
	Source          string `gorm:"size:64;not null"`
	IntervalSeconds int64  `gorm:"not null"`
	AnchorPrice     string `gorm:"type:decimal(38,18);not null"`
	WarningBPS      int    `gorm:"not null"`
	CriticalBPS     int    `gorm:"not null"`
	Version         string `gorm:"size:64;not null"`
	UpdatedAt       time.Time
}

func (monitorRuleModel) TableName() string { return "monitor_rules" }

type monitorEventModel struct {
	ID              string    `gorm:"size:64;primaryKey"`
	UserID          string    `gorm:"size:32;not null;index:idx_monitor_event_user,priority:1;index:idx_monitor_event_asset,priority:1"`
	AssetID         string    `gorm:"size:96;not null;index:idx_monitor_event_asset,priority:2"`
	Kind            string    `gorm:"size:16;not null"`
	Source          string    `gorm:"size:64;not null"`
	IntervalSeconds int64     `gorm:"not null"`
	Time            time.Time `gorm:"not null"`
	ObservedAt      time.Time `gorm:"not null;index:idx_monitor_event_user,priority:2;index:idx_monitor_event_asset,priority:3"`
	Price           string    `gorm:"type:decimal(38,18);not null"`
	AnchorPrice     string    `gorm:"type:decimal(38,18);not null"`
	DeviationBPS    string    `gorm:"size:96;not null"`
	Severity        string    `gorm:"size:16;not null"`
	Direction       string    `gorm:"size:16;not null"`
	WarningBPS      int       `gorm:"not null"`
	CriticalBPS     int       `gorm:"not null"`
	RuleVersion     string    `gorm:"size:64;not null"`
	DetectedAt      time.Time `gorm:"not null"`
}

func (monitorEventModel) TableName() string { return "monitor_events" }
