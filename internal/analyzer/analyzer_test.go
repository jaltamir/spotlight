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

func newTestAnalyzer(srv *httptest.Server) *Analyzer {
	return &Analyzer{
		baseURL: srv.URL,
		client:  srv.Client(),
	}
}

func TestAnalyzeFullFlow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("expected x-api-key=test-key, got %q", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("unexpected anthropic-version: %s", r.Header.Get("anthropic-version"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"content": [{"type": "text", "text": "Analysis: database overload."}]}`))
	}))
	defer srv.Close()

	a := newTestAnalyzer(srv)
	report := &aggregator.Report{TimeWindow: "24h", TotalErrors: 10}
	cfg := config.LLMConfig{APIKey: "test-key", Model: "claude-sonnet-4-6"}

	text, err := a.Analyze(context.Background(), report, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "Analysis: database overload." {
		t.Errorf("unexpected analysis text: %q", text)
	}
}

func TestAnalyzeHTTP500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer srv.Close()

	a := newTestAnalyzer(srv)
	report := &aggregator.Report{TimeWindow: "1h"}
	cfg := config.LLMConfig{APIKey: "key", Model: "model"}

	_, err := a.Analyze(context.Background(), report, cfg)
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected error to mention status code, got: %v", err)
	}
}

func TestAnalyzeEmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"content": []}`))
	}))
	defer srv.Close()

	a := newTestAnalyzer(srv)
	report := &aggregator.Report{TimeWindow: "1h"}
	cfg := config.LLMConfig{APIKey: "key", Model: "model"}

	_, err := a.Analyze(context.Background(), report, cfg)
	if err == nil {
		t.Fatal("expected error for empty content array")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Errorf("expected 'empty response' error, got: %v", err)
	}
}

func TestAnalyzeCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	report := &aggregator.Report{TimeWindow: "1h"}
	cfg := config.LLMConfig{APIKey: "key", Model: "model"}

	_, err := Analyze(ctx, report, cfg)
	if err == nil {
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
