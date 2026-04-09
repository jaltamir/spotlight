package output

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jaltamir/spotlight/internal/aggregator"
	"github.com/jaltamir/spotlight/internal/connector"
)

func TestBriefWriterName(t *testing.T) {
	w := NewBriefWriter()
	if w.Name() != "brief" {
		t.Errorf("expected name=brief, got %s", w.Name())
	}
}

func TestBriefWriterWrite(t *testing.T) {
	dir := t.TempDir()
	report := &aggregator.Report{
		GeneratedAt: "2026-04-09T12:00:00Z",
		TimeWindow:  "24h",
		TotalErrors: 10,
		Groups: []aggregator.Group{
			{Rank: 1, Source: "test", Service: "svc", ErrorType: "error", Count: 10},
		},
	}

	w := NewBriefWriter()
	if err := w.Write(context.Background(), report, dir, "2026-04-09T120000Z"); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "spotlight-brief-2026-04-09T120000Z.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}

	md := string(data)
	if !strings.Contains(md, "# Spotlight Action Brief") {
		t.Error("missing brief header")
	}
	if !strings.Contains(md, "## Instructions") {
		t.Error("missing instructions section")
	}
	if !strings.Contains(md, "# Data") {
		t.Error("missing data separator")
	}
	if !strings.Contains(md, "## Aggregated Report") {
		t.Error("missing aggregated report section")
	}
	if !strings.Contains(md, `"total_errors": 10`) {
		t.Error("missing report data")
	}
}

func TestBriefWriterWithRawRecords(t *testing.T) {
	dir := t.TempDir()
	report := &aggregator.Report{
		TimeWindow:  "1h",
		TotalErrors: 1,
		RawRecords: []connector.ErrorRecord{
			{Source: "test", Message: "raw-error-msg", Timestamp: time.Now(), Count: 1},
		},
	}

	w := NewBriefWriter()
	w.Write(context.Background(), report, dir, "ts")

	data, _ := os.ReadFile(filepath.Join(dir, "spotlight-brief-ts.md"))
	md := string(data)

	if !strings.Contains(md, "## Raw Error Records (1 of 1)") {
		t.Error("missing raw records section with correct count")
	}
	if !strings.Contains(md, "raw-error-msg") {
		t.Error("missing raw record data")
	}
}

func TestBriefWriterTruncatesRecords(t *testing.T) {
	dir := t.TempDir()
	records := make([]connector.ErrorRecord, 600)
	for i := range records {
		records[i] = connector.ErrorRecord{Source: "test", Count: 1}
	}
	report := &aggregator.Report{
		TimeWindow: "1h",
		RawRecords: records,
	}

	w := NewBriefWriter()
	w.Write(context.Background(), report, dir, "ts")

	data, _ := os.ReadFile(filepath.Join(dir, "spotlight-brief-ts.md"))
	md := string(data)

	if !strings.Contains(md, "500 of 600") {
		t.Errorf("expected truncation indicator '500 of 600', got: %s", md[:200])
	}
}

func TestBriefWriterWithoutRawRecords(t *testing.T) {
	dir := t.TempDir()
	report := &aggregator.Report{TimeWindow: "1h"}

	w := NewBriefWriter()
	w.Write(context.Background(), report, dir, "ts")

	data, _ := os.ReadFile(filepath.Join(dir, "spotlight-brief-ts.md"))
	md := string(data)

	if !strings.Contains(md, "0 of 0") {
		t.Error("expected empty raw records section")
	}
}
