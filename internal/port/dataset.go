package port

import (
	"context"
	"depeg.local/platform/internal/domain/dataset"
)

type DatasetReader interface {
	Scan(context.Context, string, dataset.Options, func(dataset.Batch) error) (dataset.Report, error)
}

type DatasetSink interface {
	Write(context.Context, dataset.Batch) error
}

type DatasetRepository interface {
	// Import commits the whole archive together; any callback error rolls it back.
	Import(context.Context, func(DatasetSink) error) error
}
