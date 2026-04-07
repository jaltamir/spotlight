package output

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jaltamir/spotlight/internal/aggregator"
)

func TestJSONWriterName(t *testing.T) {
	w := NewJSONWriter()
	if w.Name() != "json" {
		t.Errorf("expected name=json, got %s", w.Name())
	}
}

func TestJSONWriterWrite(t *testing.T) {
	dir := t.TempDir()
	report := &aggregator.Report{
		GeneratedAt: "2026-04-05T12:00:00Z",
		TimeWindow:  "24h",
		TotalErrors: 42,
		Groups: []aggregator.Group{
			{Rank: 1, Source: "test", Service: "svc", Count: 42, Trend: "rising", SampleMessages: []string{"err"}},
		},
	}

	w := NewJSONWriter()
	if err := w.Write(context.Background(), report, dir, "2026-04-05T120000Z"); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "spotlight-2026-04-05T120000Z.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}

	var parsed aggregator.Report
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed.TotalErrors != 42 {
		t.Errorf("expected TotalErrors=42, got %d", parsed.TotalErrors)
	}
	if len(parsed.Groups) != 1 {
		t.Errorf("expected 1 group, got %d", len(parsed.Groups))
	}
}

func TestJSONWriterAnalysisField(t *testing.T) {
	dir := t.TempDir()
	analysis := "This is the analysis."
	report := &aggregator.Report{
		GeneratedAt: "2026-04-05T12:00:00Z",
		TimeWindow:  "1h",
		TotalErrors: 0,
		Groups:      nil,
		Analysis:    &analysis,
	}

	w := NewJSONWriter()
	if err := w.Write(context.Background(), report, dir, "test"); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "spotlight-test.json"))
	var parsed aggregator.Report
	json.Unmarshal(data, &parsed)

	if parsed.Analysis == nil {
		t.Fatal("analysis should not be nil")
	}
	if *parsed.Analysis != "This is the analysis." {
		t.Errorf("unexpected analysis: %s", *parsed.Analysis)
	}
}
