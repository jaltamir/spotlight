package analyzer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jaltamir/spotlight/internal/aggregator"
	"github.com/jaltamir/spotlight/internal/config"
)

const anthropicAPI = "https://api.anthropic.com/v1/messages"

// Analyze sends the grouped report to Claude API and returns the analysis text.
func Analyze(ctx context.Context, report *aggregator.Report, cfg config.LLMConfig) (string, error) {
	reportJSON, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling report: %w", err)
	}

	prompt := fmt.Sprintf(
		"You are a senior engineer analyzing error patterns from multiple sources. "+
			"Here are the grouped errors from the last %s. "+
			"For each group, explain what might be happening, identify possible root causes, "+
			"and suggest investigation steps. Prioritize by business impact.\n\n%s",
		report.TimeWindow, string(reportJSON),
	)

	body, err := json.Marshal(map[string]any{
		"model":      cfg.Model,
		"max_tokens": 4096,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})
	if err != nil {
		return "", fmt.Errorf("building request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", anthropicAPI, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", cfg.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("User-Agent", "Spotlight/1.0")

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("calling claude api: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("claude api returned %d: %s", resp.StatusCode, truncate(string(respBody), 200))
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parsing response: %w", err)
	}

	if len(result.Content) == 0 {
		return "", fmt.Errorf("empty response from claude api")
	}

	return result.Content[0].Text, nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
