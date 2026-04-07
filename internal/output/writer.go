package output

import (
	"context"

	"github.com/jaltamir/spotlight/internal/aggregator"
)

// Writer is the interface that all output connectors must implement.
type Writer interface {
	// Name returns the output connector identifier (e.g. "json", "html", "s3").
	Name() string

	// Write outputs the report. The outDir is the base output directory
	// and timestamp is used for filenames.
	Write(ctx context.Context, report *aggregator.Report, outDir string, timestamp string) error
}
