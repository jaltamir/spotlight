package processor

import (
	"context"

	"github.com/jaltamir/spotlight/internal/aggregator"
)

// Processor transforms or augments a report in place between
// aggregation and output.
type Processor interface {
	// Name returns the processor identifier (e.g. "llm").
	Name() string

	// Process mutates the report in place.
	Process(ctx context.Context, report *aggregator.Report) error
}
