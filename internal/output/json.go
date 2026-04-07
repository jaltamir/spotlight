package output

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jaltamir/spotlight/internal/aggregator"
)

// JSONWriter writes the report as formatted JSON.
type JSONWriter struct{}

func NewJSONWriter() *JSONWriter { return &JSONWriter{} }

func (w *JSONWriter) Name() string { return "json" }

func (w *JSONWriter) Write(_ context.Context, report *aggregator.Report, outDir string, timestamp string) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling report: %w", err)
	}
	data = append(data, '\n')

	path := filepath.Join(outDir, fmt.Sprintf("spotlight-%s.json", timestamp))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing report: %w", err)
	}

	return nil
}
