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
)

// Analyzer sends grouped reports to an LLM API for analysis.
// It implements the enricher.Enricher interface.
type Analyzer struct {
	baseURL string
	client  *http.Client
	cfg     config.LLMConfig
}

// New returns an Analyzer configured with the given LLM settings.
func New(cfg config.LLMConfig) *Analyzer {
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
		baseURL: base,
		client:  httpclient.NewClient(120 * time.Second),
		cfg:     cfg,
	}
}

// Name implements enricher.Enricher.
func (a *Analyzer) Name() string { return "llm" }

// Enrich implements enricher.Enricher. It calls the LLM API and
// sets report.Analysis.
func (a *Analyzer) Enrich(ctx context.Context, report *aggregator.Report) error {
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

	prompt := fmt.Sprintf(
		"You are a senior engineer analyzing error patterns from multiple sources. "+
			"Here are the grouped errors from the last %s. "+
			"For each group, explain what might be happening, identify possible root causes, "+
			"and suggest investigation steps. Prioritize by business impact.\n\n%s",
		report.TimeWindow, string(reportJSON),
	)

	body, err := a.buildRequestBody(prompt)
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

func (a *Analyzer) buildRequestBody(prompt string) ([]byte, error) {
	payload := map[string]any{
		"model":      a.cfg.Model,
		"max_tokens": 4096,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}
	return json.Marshal(payload)
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
