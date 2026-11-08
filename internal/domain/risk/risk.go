package risk

import "time"

// Horizon describes offsets from the observation time, not a price forecast.
// TODO: Initially allow 0-3h, 3-6h and 6-9h, matching the paper's experiment.
type Horizon struct {
	StartMinutes int
	EndMinutes   int
}

// Prediction estimates the fraction of significantly depegged future minutes.
// TODO: Validate [0,1]; nil means unavailable, while a non-nil zero is a result.
// TODO: Version the 5% deviation label and its anchor/quote normalization.
type Prediction struct {
	AssetID          string
	Horizon          Horizon
	DepegMinuteRatio *float64
	ModelVersion     string
	FeatureVersion   string
	LabelVersion     string
	Source           string
	ObservedAt       time.Time
	GeneratedAt      time.Time
	Quality          string
}

type Query struct {
	AssetID      string
	Horizon      Horizon
	ModelVersion string
	From         time.Time
	To           time.Time
	Cursor       string
	Limit        int
}

type Series struct {
	Predictions []Prediction
	NextCursor  string
	AsOf        time.Time
}
