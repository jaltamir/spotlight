package output

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jaltamir/spotlight/internal/aggregator"
	"github.com/jaltamir/spotlight/internal/prompt"
)

const maxBriefRecords = 500

// BriefWriter generates a self-contained markdown file with prompt,
// aggregated report, and raw error records — ready to be consumed
// by an external AI agent.
type BriefWriter struct{}

func NewBriefWriter() *BriefWriter {
	return &BriefWriter{}
}

func (w *BriefWriter) Name() string { return "brief" }

func (w *BriefWriter) Write(_ context.Context, report *aggregator.Report, outDir, timestamp string) error {
	promptText, err := prompt.Load("")
	if err != nil {
		return fmt.Errorf("loading prompt: %w", err)
	}

	reportJSON, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling report: %w", err)
	}

	records := report.RawRecords
	totalRecords := len(records)
	if len(records) > maxBriefRecords {
		records = records[:maxBriefRecords]
	}

	recordsJSON, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling raw records: %w", err)
	}

	content := fmt.Sprintf(
		"# Spotlight Action Brief — %s\n\n"+
			"## Instructions\n\n%s\n\n"+
			"---\n\n"+
			"# Data\n\n"+
			"## Aggregated Report\n\n```json\n%s\n```\n\n"+
			"## Raw Error Records (%d of %d)\n\n```json\n%s\n```\n",
		timestamp,
		promptText,
		string(reportJSON),
		len(records), totalRecords,
		string(recordsJSON),
	)

	path := filepath.Join(outDir, fmt.Sprintf("spotlight-brief-%s.md", timestamp))
	return os.WriteFile(path, []byte(content), 0o644)
}
