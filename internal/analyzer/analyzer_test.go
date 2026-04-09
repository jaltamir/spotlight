package analyzer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jaltamir/spotlight/internal/aggregator"
	"github.com/jaltamir/spotlight/internal/config"
	"github.com/jaltamir/spotlight/internal/connector"
)

func newTestAnalyzer(srv *httptest.Server, provider string) *Analyzer {
	return &Analyzer{
		baseURL:      srv.URL,
		client:       srv.Client(),
		cfg:          config.LLMConfig{Provider: provider, APIKey: "test-key", Model: "test-model"},
		systemPrompt: "You are a test assistant.",
	}
}

func TestName(t *testing.T) {
	a := New(config.LLMConfig{}, "prompt")
	if a.Name() != "llm" {
		t.Errorf("expected name=llm, got %s", a.Name())
	}
}

func TestNewDefaultsAnthropic(t *testing.T) {
	a := New(config.LLMConfig{Provider: "anthropic"}, "p")
	if a.baseURL != defaultAnthropicURL {
		t.Errorf("expected anthropic URL, got %s", a.baseURL)
	}
}

func TestNewDefaultsOpenAI(t *testing.T) {
	a := New(config.LLMConfig{Provider: "openai"}, "p")
	if a.baseURL != defaultOpenAIURL {
		t.Errorf("expected openai URL, got %s", a.baseURL)
	}
}

func TestNewCustomBaseURL(t *testing.T) {
	a := New(config.LLMConfig{Provider: "openai", BaseURL: "http://localhost:8080"}, "p")
	if a.baseURL != "http://localhost:8080" {
		t.Errorf("expected custom URL, got %s", a.baseURL)
	}
}

func TestNewStoresPrompt(t *testing.T) {
	a := New(config.LLMConfig{}, "my custom prompt")
	if a.systemPrompt != "my custom prompt" {
		t.Errorf("expected stored prompt, got %q", a.systemPrompt)
	}
}

func TestEnrichAnthropic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("expected x-api-key=test-key, got %q", r.Header.Get("x-api-key"))
		}

		// Verify system prompt is in the request body.
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["system"] != "You are a test assistant." {
			t.Errorf("expected system prompt in body, got %v", body["system"])
		}

		w.Write([]byte(`{"content": [{"type": "text", "text": "## Analysis\n\nDatabase overload."}]}`))
	}))
	defer srv.Close()

	a := newTestAnalyzer(srv, "anthropic")
	report := &aggregator.Report{TimeWindow: "24h", TotalErrors: 10}

	if err := a.Enrich(context.Background(), report); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Analysis == nil || !strings.Contains(*report.Analysis, "Database overload") {
		t.Errorf("unexpected analysis: %v", report.Analysis)
	}
}

func TestEnrichOpenAI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-key" {
			t.Errorf("expected Bearer auth, got %q", auth)
		}

		// Verify system message in messages array.
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		msgs := body["messages"].([]any)
		firstMsg := msgs[0].(map[string]any)
		if firstMsg["role"] != "system" {
			t.Errorf("expected system role, got %v", firstMsg["role"])
		}

		w.Write([]byte(`{"choices": [{"message": {"content": "OpenAI analysis."}}]}`))
	}))
	defer srv.Close()

	a := newTestAnalyzer(srv, "openai")
	report := &aggregator.Report{TimeWindow: "1h"}

	if err := a.Enrich(context.Background(), report); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Analysis == nil || *report.Analysis != "OpenAI analysis." {
		t.Errorf("unexpected analysis: %v", report.Analysis)
	}
}

func TestEnrichIncludesRawRecords(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		msgs := body["messages"].([]any)
		userMsg := msgs[0].(map[string]any)["content"].(string)
		if !strings.Contains(userMsg, "Raw Error Records") {
			t.Error("user message should contain raw records section")
		}
		if !strings.Contains(userMsg, "test-error") {
			t.Error("user message should contain raw record data")
		}
		w.Write([]byte(`{"content": [{"text": "done"}]}`))
	}))
	defer srv.Close()

	a := newTestAnalyzer(srv, "anthropic")
	report := &aggregator.Report{
		TimeWindow: "1h",
		RawRecords: []connector.ErrorRecord{
			{Source: "test", Message: "test-error", Timestamp: time.Now(), Count: 1},
		},
	}

	if err := a.Enrich(context.Background(), report); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnrichTruncatesRawRecords(t *testing.T) {
	recordCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		msgs := body["messages"].([]any)
		userMsg := msgs[0].(map[string]any)["content"].(string)
		// Check the "(500 of 1000)" indicator.
		if !strings.Contains(userMsg, "500 of 1000") {
			t.Errorf("expected truncation indicator '500 of 1000' in message")
		}
		recordCount++
		w.Write([]byte(`{"content": [{"text": "done"}]}`))
	}))
	defer srv.Close()

	a := newTestAnalyzer(srv, "anthropic")
	records := make([]connector.ErrorRecord, 1000)
	for i := range records {
		records[i] = connector.ErrorRecord{Source: "test", Count: 1}
	}
	report := &aggregator.Report{TimeWindow: "1h", RawRecords: records}

	a.Enrich(context.Background(), report)
	if recordCount != 1 {
		t.Error("expected exactly 1 API call")
	}
}

func TestEnrichHTTP500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer srv.Close()

	a := newTestAnalyzer(srv, "anthropic")
	report := &aggregator.Report{TimeWindow: "1h"}

	err := a.Enrich(context.Background(), report)
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
}

func TestEnrichEmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"content": []}`))
	}))
	defer srv.Close()

	a := newTestAnalyzer(srv, "anthropic")
	report := &aggregator.Report{TimeWindow: "1h"}

	err := a.Enrich(context.Background(), report)
	if err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestEnrichCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	a := New(config.LLMConfig{Provider: "anthropic", APIKey: "key", Model: "model"}, "prompt")
	report := &aggregator.Report{TimeWindow: "1h"}

	if err := a.Enrich(ctx, report); err == nil {
		t.Fatal("expected error with cancelled context")
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is too long", 7, "this is..."},
		{"", 5, ""},
	}
	for _, tt := range tests {
		got := truncate(tt.input, tt.max)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
		}
	}
}

func TestParseAnthropicResponse(t *testing.T) {
	text, err := parseAnthropicResponse([]byte(`{"content": [{"text": "hello"}]}`))
	if err != nil || text != "hello" {
		t.Errorf("expected 'hello', got %q, err=%v", text, err)
	}
}

func TestParseOpenAIResponse(t *testing.T) {
	text, err := parseOpenAIResponse([]byte(`{"choices": [{"message": {"content": "hello"}}]}`))
	if err != nil || text != "hello" {
		t.Errorf("expected 'hello', got %q, err=%v", text, err)
	}
}

func TestParseOpenAIResponseEmpty(t *testing.T) {
	_, err := parseOpenAIResponse([]byte(`{"choices": []}`))
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}
