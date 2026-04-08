package analyzer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jaltamir/spotlight/internal/aggregator"
	"github.com/jaltamir/spotlight/internal/config"
)

func newTestAnalyzer(srv *httptest.Server, provider string) *Analyzer {
	return &Analyzer{
		baseURL: srv.URL,
		client:  srv.Client(),
		cfg:     config.LLMConfig{Provider: provider, APIKey: "test-key", Model: "test-model"},
	}
}

func TestName(t *testing.T) {
	a := New(config.LLMConfig{})
	if a.Name() != "llm" {
		t.Errorf("expected name=llm, got %s", a.Name())
	}
}

func TestNewDefaultsAnthropic(t *testing.T) {
	a := New(config.LLMConfig{Provider: "anthropic"})
	if a.baseURL != defaultAnthropicURL {
		t.Errorf("expected anthropic URL, got %s", a.baseURL)
	}
}

func TestNewDefaultsOpenAI(t *testing.T) {
	a := New(config.LLMConfig{Provider: "openai"})
	if a.baseURL != defaultOpenAIURL {
		t.Errorf("expected openai URL, got %s", a.baseURL)
	}
}

func TestNewCustomBaseURL(t *testing.T) {
	a := New(config.LLMConfig{Provider: "openai", BaseURL: "http://localhost:8080"})
	if a.baseURL != "http://localhost:8080" {
		t.Errorf("expected custom URL, got %s", a.baseURL)
	}
}

func TestEnrichAnthropic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("expected x-api-key=test-key, got %q", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("missing anthropic-version header")
		}
		w.Write([]byte(`{"content": [{"type": "text", "text": "Analysis: database overload."}]}`))
	}))
	defer srv.Close()

	a := newTestAnalyzer(srv, "anthropic")
	report := &aggregator.Report{TimeWindow: "24h", TotalErrors: 10}

	if err := a.Enrich(context.Background(), report); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Analysis == nil || *report.Analysis != "Analysis: database overload." {
		t.Errorf("unexpected analysis: %v", report.Analysis)
	}
}

func TestEnrichOpenAI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-key" {
			t.Errorf("expected Bearer auth, got %q", auth)
		}
		if r.Header.Get("x-api-key") != "" {
			t.Error("should not have x-api-key for openai")
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
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected error to mention status code, got: %v", err)
	}
	if report.Analysis != nil {
		t.Error("analysis should remain nil on error")
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
	if !strings.Contains(err.Error(), "empty response") {
		t.Errorf("expected 'empty response' error, got: %v", err)
	}
}

func TestEnrichCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	a := New(config.LLMConfig{Provider: "anthropic", APIKey: "key", Model: "model"})
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
