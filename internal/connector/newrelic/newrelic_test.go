package newrelic

import (
	"strings"
	"testing"
	"time"

	"github.com/jaltamir/spotlight/internal/config"
)

func TestParseResults(t *testing.T) {
	records, err := parseResults([]byte(`{
		"data": {
			"actor": {
				"account": {
					"nrql": {
						"results": [
							{"facet": ["svc-a", "/api/foo", "RuntimeError", "500", "broken"], "count": 10},
							{"facet": ["svc-a", "/api/foo", "RuntimeError", null, "also broken"], "count": 5},
							{"facet": ["svc-b", "/api/bar", "TimeoutError", "504", "timeout"], "count": 3}
						]
					}
				}
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}

	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}

	r := records[0]
	if r.Source != "newrelic" {
		t.Errorf("expected source=newrelic, got %s", r.Source)
	}
	if r.Service != "svc-a" {
		t.Errorf("expected service=svc-a, got %s", r.Service)
	}
	if r.Endpoint != "/api/foo" {
		t.Errorf("expected endpoint=/api/foo, got %s", r.Endpoint)
	}
	if r.ErrorType != "HTTP 500" {
		t.Errorf("expected error_type=HTTP 500, got %s", r.ErrorType)
	}
	if r.Message != "broken" {
		t.Errorf("expected message=broken, got %s", r.Message)
	}
	if r.Count != 10 {
		t.Errorf("expected count=10, got %d", r.Count)
	}

	// Null httpResponseCode should fall back to error.class.
	if records[1].ErrorType != "RuntimeError" {
		t.Errorf("expected error_type=RuntimeError for null httpCode, got %s", records[1].ErrorType)
	}
}

func TestParseResultsNerdGraphError(t *testing.T) {
	_, err := parseResults([]byte(`{
		"data": null,
		"errors": [{"message": "query failed"}]
	}`))
	if err == nil {
		t.Fatal("expected error for nerdgraph error response")
	}
	if !strings.Contains(err.Error(), "query failed") {
		t.Errorf("error should contain message, got: %v", err)
	}
}

func TestParseResultsInvalidJSON(t *testing.T) {
	_, err := parseResults([]byte(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseResultsSkipsShortFacets(t *testing.T) {
	records, err := parseResults([]byte(`{
		"data": {"actor": {"account": {"nrql": {"results": [
			{"facet": ["only", "three"], "count": 1},
			{"facet": ["a", "b", "c", "d", "e"], "count": 2}
		]}}}}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record (short facet skipped), got %d", len(records))
	}
}

func TestParseResultsEmpty(t *testing.T) {
	records, err := parseResults([]byte(`{
		"data": {"actor": {"account": {"nrql": {"results": []}}}}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("expected 0 records, got %d", len(records))
	}
}

func TestBuildNRQL(t *testing.T) {
	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	q := buildNRQL(since, until, []string{"app-a", "app-b"})
	if !strings.Contains(q, "appName IN ('app-a', 'app-b')") {
		t.Errorf("expected app filter, got: %s", q)
	}
	if !strings.Contains(q, "FACET") {
		t.Errorf("expected FACET clause, got: %s", q)
	}

	q2 := buildNRQL(since, until, nil)
	if strings.Contains(q2, "appName IN") {
		t.Error("expected no app filter when apps is nil")
	}
}

func TestNew(t *testing.T) {
	c := New(config.ConnectorConfig{
		APIKey:       "key",
		AccountID:    "123",
		Applications: []string{"app1"},
	})
	if c.Name() != "newrelic" {
		t.Errorf("expected name=newrelic, got %s", c.Name())
	}
	if c.apiKey != "key" || c.accountID != "123" {
		t.Error("config not applied")
	}
	if len(c.applications) != 1 || c.applications[0] != "app1" {
		t.Error("applications not set")
	}
}

func TestEscapeGraphQL(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{`plain`, `plain`},
		{`has "quotes"`, `has \"quotes\"`},
		{`has \backslash`, `has \\backslash`},
		{`"both\" kinds"`, `\"both\\\" kinds\"`},
	}
	for _, tt := range tests {
		got := escapeGraphQL(tt.input)
		if got != tt.want {
			t.Errorf("escapeGraphQL(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFacetString(t *testing.T) {
	tests := []struct {
		input any
		want  string
	}{
		{nil, ""},
		{"hello", "hello"},
		{42.0, "42"},
		{"", ""},
	}
	for _, tt := range tests {
		got := facetString(tt.input)
		if got != tt.want {
			t.Errorf("facetString(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if truncate("short", 10) != "short" {
		t.Error("short string should not be truncated")
	}
	if truncate("long string here", 4) != "long..." {
		t.Errorf("got %s", truncate("long string here", 4))
	}
}
