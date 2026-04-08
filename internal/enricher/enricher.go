package enricher

import (
	"context"

	"github.com/jaltamir/spotlight/internal/aggregator"
)

// Enricher transforms or augments a report in place between
// aggregation and output.
type Enricher interface {
	// Name returns the enricher identifier (e.g. "llm").
	Name() string

	// Enrich mutates the report in place.
	Enrich(ctx context.Context, report *aggregator.Report) error
}
