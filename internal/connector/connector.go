package connector

import (
	"context"
	"time"
)

// ErrorRecord represents a single normalized error from any source.
type ErrorRecord struct {
	Source    string    `json:"source"`
	Service  string    `json:"service"`
	Endpoint string    `json:"endpoint"`
	ErrorType string   `json:"error_type"`
	Message  string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
	Count    int       `json:"count"`
}

// Connector is the interface that all data source connectors must implement.
type Connector interface {
	// Name returns the connector identifier (e.g. "newrelic", "hubspot").
	Name() string

	// Collect extracts error records for the given time window.
	Collect(ctx context.Context, since, until time.Time) ([]ErrorRecord, error)
}
