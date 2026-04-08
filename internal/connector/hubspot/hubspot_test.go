package hubspot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jaltamir/spotlight/internal/config"
)

// newTestConnector creates a Connector pointing at the given test server URL.
func newTestConnector(srv *httptest.Server) *Connector {
	return &Connector{
		apiKey:  "test-token",
		baseURL: srv.URL,
		client:  srv.Client(),
	}
}

func TestParseHubspotTime(t *testing.T) {
	tests := []struct {
		input string
		zero  bool
	}{
		{"2026-04-05T10:00:00Z", false},
		{"2026-04-05T10:00:00.123Z", false},
		{"", true},
		{"not-a-date", true},
	}
	for _, tt := range tests {
		got := parseHubspotTime(tt.input)
		if tt.zero && !got.IsZero() {
			t.Errorf("parseHubspotTime(%q) should be zero, got %v", tt.input, got)
		}
		if !tt.zero && got.IsZero() {
			t.Errorf("parseHubspotTime(%q) should not be zero", tt.input)
		}
	}

	// Verify UTC conversion.
	got := parseHubspotTime("2026-04-05T10:00:00Z")
	if got.Location() != time.UTC {
		t.Error("expected UTC timezone")
	}
}

func TestPropOr(t *testing.T) {
	r := crmResult{Properties: map[string]string{
		"email":    "test@example.com",
		"empty":    "",
		"has_data": "value",
	}}

	if propOr(r, "email", "fallback") != "test@example.com" {
		t.Error("should return existing value")
	}
	if propOr(r, "empty", "fallback") != "fallback" {
		t.Error("empty string should fall back")
	}
	if propOr(r, "missing", "fallback") != "fallback" {
		t.Error("missing key should fall back")
	}
}

func TestNew(t *testing.T) {
	c := New(config.ConnectorConfig{APIKey: "my-token"})
	if c.Name() != "hubspot" {
		t.Errorf("expected name=hubspot, got %s", c.Name())
	}
	if c.apiKey != "my-token" {
		t.Error("api key not set")
	}
	if c.baseURL != defaultBaseURL {
		t.Errorf("expected default baseURL %s, got %s", defaultBaseURL, c.baseURL)
	}
}

func TestSearchContactsCopiesFilters(t *testing.T) {
	original := []crmFilter{
		{Property: "hs_email_bounce", Operator: "GT", Value: "0"},
	}
	origLen := len(original)

	copied := make([]crmFilter, len(original), len(original)+1)
	copy(copied, original)
	copied = append(copied, crmFilter{Property: "lastmodifieddate", Operator: "GTE", Value: "123"})

	if len(original) != origLen {
		t.Error("original slice was mutated")
	}
	if len(copied) != origLen+1 {
		t.Error("copy should have extra element")
	}
}

// TestSearchContactsPagination verifies that searchContacts fetches multiple pages.
func TestSearchContactsPagination(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)

		if req["after"] == nil {
			// First page — return 1 result and a paging cursor.
			json.NewEncoder(w).Encode(crmSearchResponse{
				Total: 2,
				Results: []crmResult{
					{Properties: map[string]string{"email": "p1@test.com", "lastmodifieddate": "2026-04-05T10:00:00Z"}},
				},
				Paging: &crmPaging{Next: &struct {
					After string `json:"after"`
				}{After: "cursor-abc"}},
			})
		} else {
			// Second page — return 1 result, no next cursor.
			json.NewEncoder(w).Encode(crmSearchResponse{
				Total: 2,
				Results: []crmResult{
					{Properties: map[string]string{"email": "p2@test.com", "lastmodifieddate": "2026-04-05T11:00:00Z"}},
				},
			})
		}
	}))
	defer srv.Close()

	c := newTestConnector(srv)
	results, err := c.searchContacts(context.Background(), 0, crmSearchRequest{
		Filters:    []crmFilter{{Property: "hs_email_bounce", Operator: "GT", Value: "0"}},
		Properties: []string{"email", "lastmodifieddate"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results across 2 pages, got %d", len(results))
	}
	if callCount != 2 {
		t.Errorf("expected 2 HTTP calls, got %d", callCount)
	}
}

// TestSearchContactsMaxPages verifies the max-pages safety cap.
func TestSearchContactsMaxPages(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		// Always return a next cursor to simulate infinite pagination.
		json.NewEncoder(w).Encode(crmSearchResponse{
			Results: []crmResult{
				{Properties: map[string]string{"email": "x@test.com"}},
			},
			Paging: &crmPaging{Next: &struct {
				After string `json:"after"`
			}{After: "next"}},
		})
	}))
	defer srv.Close()

	c := newTestConnector(srv)
	results, err := c.searchContacts(context.Background(), 0, crmSearchRequest{
		Filters:    []crmFilter{{Property: "hs_email_bounce", Operator: "GT", Value: "0"}},
		Properties: []string{"email"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != maxPages { //nolint:staticcheck
		t.Errorf("expected maxPages=%d calls, got %d", maxPages, callCount)
	}
	if len(results) != maxPages {
		t.Errorf("expected %d results, got %d", maxPages, len(results))
	}
}

// TestSearchContactsContextCancel verifies context cancellation mid-pagination.
func TestSearchContactsContextCancel(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		json.NewEncoder(w).Encode(crmSearchResponse{
			Results: []crmResult{{Properties: map[string]string{"email": "x@test.com"}}},
			Paging: &crmPaging{Next: &struct {
				After string `json:"after"`
			}{After: "next"}},
		})
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after the first call completes by using a custom handler that cancels.
	cancelOnce := false
	srvCancel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !cancelOnce {
			cancelOnce = true
			json.NewEncoder(w).Encode(crmSearchResponse{
				Results: []crmResult{{Properties: map[string]string{"email": "first@test.com"}}},
				Paging: &crmPaging{Next: &struct {
					After string `json:"after"`
				}{After: "cursor"}}})
			cancel() // cancel after first page is sent
		} else {
			json.NewEncoder(w).Encode(crmSearchResponse{
				Results: []crmResult{{Properties: map[string]string{"email": "second@test.com"}}},
			})
		}
	}))
	defer srvCancel.Close()

	c2 := &Connector{apiKey: "token", baseURL: srvCancel.URL, client: srvCancel.Client()}
	_, err := c2.searchContacts(ctx, 0, crmSearchRequest{
		Filters:    []crmFilter{{Property: "hs_email_bounce", Operator: "GT", Value: "0"}},
		Properties: []string{"email"},
	})

	if err == nil {
		t.Error("expected context cancellation error")
	}
	srv.Close() // close unused server
}

// TestAuditLogsPagination verifies that collectAuditLogs fetches multiple pages.
func TestAuditLogsPagination(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		hasAfter := r.URL.Query().Get("after") != ""

		type auditEntry struct {
			Category    string            `json:"category"`
			SubCategory string            `json:"subCategory"`
			Action      string            `json:"action"`
			OccurredAt  string            `json:"occurredAt"`
			ActingUser  map[string]string `json:"actingUser"`
			TargetID    string            `json:"targetObjectId"`
		}

		entry := auditEntry{
			Category: "CRITICAL_ACTION", SubCategory: "HAPIKEY_CREATE",
			Action: "CREATE", OccurredAt: "2026-04-05T10:00:00Z",
			ActingUser: map[string]string{"userEmail": "admin@test.com"},
		}

		if !hasAfter {
			json.NewEncoder(w).Encode(map[string]any{
				"results": []auditEntry{entry},
				"paging":  map[string]any{"next": map[string]string{"after": "cursor-xyz"}},
			})
		} else {
			json.NewEncoder(w).Encode(map[string]any{
				"results": []auditEntry{entry},
			})
		}
	}))
	defer srv.Close()

	c := newTestConnector(srv)
	records, err := c.collectAuditLogs(context.Background(),
		time.Date(2026, 4, 5, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 5, 23, 59, 59, 0, time.UTC),
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 2 {
		t.Errorf("expected 2 audit records across 2 pages, got %d", len(records))
	}
	if callCount != 2 {
		t.Errorf("expected 2 HTTP calls, got %d", callCount)
	}
}
