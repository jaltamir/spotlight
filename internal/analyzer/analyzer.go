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
	"github.com/jaltamir/spotlight/internal/httpclient"
	"github.com/jaltamir/spotlight/internal/version"
)

const (
	defaultAnthropicURL = "https://api.anthropic.com/v1/messages"
	defaultOpenAIURL    = "https://api.openai.com/v1/chat/completions"
	maxRawRecords       = 500
)

// Analyzer sends grouped reports to an LLM API for analysis.
// It implements the processor.Processor interface.
type Analyzer struct {
	baseURL      string
	client       *http.Client
	cfg          config.LLMConfig
	systemPrompt string
}

// New returns an Analyzer configured with the given LLM settings and prompt.
func New(cfg config.LLMConfig, systemPrompt string) *Analyzer {
	base := cfg.BaseURL
	if base == "" {
		switch cfg.Provider {
		case "openai":
			base = defaultOpenAIURL
		default:
			base = defaultAnthropicURL
		}
	}
	return &Analyzer{
		baseURL:      base,
		client:       httpclient.NewClient(120 * time.Second),
		cfg:          cfg,
		systemPrompt: systemPrompt,
	}
}

// Name implements processor.Processor.
func (a *Analyzer) Name() string { return "llm" }

// Process implements processor.Processor. It calls the LLM API and
// sets report.Analysis.
func (a *Analyzer) Process(ctx context.Context, report *aggregator.Report) error {
	text, err := a.analyze(ctx, report)
	if err != nil {
		return err
	}
	report.Analysis = &text
	return nil
}

func (a *Analyzer) analyze(ctx context.Context, report *aggregator.Report) (string, error) {
	reportJSON, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling report: %w", err)
	}

	// Serialize raw records (capped to avoid exceeding token limits).
	records := report.RawRecords
	if len(records) > maxRawRecords {
		records = records[:maxRawRecords]
	}
	recordsJSON, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling raw records: %w", err)
	}

	userMessage := fmt.Sprintf(
		"Time window: %s\n\n## Aggregated Report\n```json\n%s\n```\n\n## Raw Error Records (%d of %d)\n```json\n%s\n```",
		report.TimeWindow,
		string(reportJSON),
		len(records), len(report.RawRecords),
		string(recordsJSON),
	)

	body, err := a.buildRequestBody(a.systemPrompt, userMessage)
	if err != nil {
		return "", fmt.Errorf("building request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", a.baseURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	a.setHeaders(req)

	resp, err := a.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("calling llm api: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("llm api returned %d: %s", resp.StatusCode, truncate(string(respBody), 200))
	}

	return a.parseResponse(respBody)
}

func (a *Analyzer) buildRequestBody(systemPrompt, userMessage string) ([]byte, error) {
	switch a.cfg.Provider {
	case "openai":
		return json.Marshal(map[string]any{
			"model":      a.cfg.Model,
			"max_tokens": 4096,
			"messages": []map[string]string{
				{"role": "system", "content": systemPrompt},
				{"role": "user", "content": userMessage},
			},
		})
	default: // anthropic
		return json.Marshal(map[string]any{
			"model":      a.cfg.Model,
			"max_tokens": 4096,
			"system":     systemPrompt,
			"messages": []map[string]string{
				{"role": "user", "content": userMessage},
			},
		})
	}
}

func (a *Analyzer) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", version.UserAgent())

	switch a.cfg.Provider {
	case "openai":
		req.Header.Set("Authorization", "Bearer "+a.cfg.APIKey)
	default: // anthropic
		req.Header.Set("x-api-key", a.cfg.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	}
}

func (a *Analyzer) parseResponse(body []byte) (string, error) {
	switch a.cfg.Provider {
	case "openai":
		return parseOpenAIResponse(body)
	default:
		return parseAnthropicResponse(body)
	}
}

func parseAnthropicResponse(body []byte) (string, error) {
	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parsing response: %w", err)
	}
	if len(result.Content) == 0 {
		return "", fmt.Errorf("empty response from llm api")
	}
	return result.Content[0].Text, nil
}

func parseOpenAIResponse(body []byte) (string, error) {
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parsing response: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("empty response from llm api")
	}
	return result.Choices[0].Message.Content, nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
