package rollbar

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jaltamir/spotlight/internal/config"
)

func newTestConnector(srv *httptest.Server, slug string, envs []string) *Connector {
	return &Connector{
		apiKey:       "test-token",
		projectSlug:  slug,
		environments: envs,
		baseURL:      srv.URL,
		client:       srv.Client(),
	}
}

func makeItems(n int, baseTsUnix int64, env string) []item {
	items := make([]item, n)
	for i := range items {
		items[i] = item{
			Title:                   fmt.Sprintf("Error #%d", i),
			Level:                   "error",
			Environment:             env,
			Framework:               "php",
			ProjectSlug:             "test-project",
			TotalOccurrences:        100 + i,
			LastOccurrenceTimestamp:  baseTsUnix - int64(i*3600), // 1 hour apart, descending
			FirstOccurrenceTimestamp: baseTsUnix - 86400,
			Counter:                 i + 1,
		}
	}
	return items
}

func TestNew(t *testing.T) {
	c := New(config.ConnectorConfig{
		APIKey:       "my-token",
		AccountID:    "my-project",
		Applications: []string{"pro"},
	})
	if c.Name() != "rollbar" {
		t.Errorf("expected name=rollbar, got %s", c.Name())
	}
	if c.apiKey != "my-token" || c.projectSlug != "my-project" {
		t.Error("config not applied")
	}
	if c.baseURL != defaultBaseURL {
		t.Errorf("expected default baseURL, got %s", c.baseURL)
	}
}

func TestCollectFullFlow(t *testing.T) {
	now := time.Now().UTC()
	since := now.Add(-24 * time.Hour)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Rollbar-Access-Token") != "test-token" {
			t.Errorf("missing auth header")
		}
		json.NewEncoder(w).Encode(itemsResponse{
			Result: struct {
				Items []item `json:"items"`
			}{
				Items: []item{
					{Title: "In window", Level: "error", Environment: "pro", Framework: "php",
						LastOccurrenceTimestamp: now.Add(-1 * time.Hour).Unix()},
					{Title: "Out of window", Level: "error", Environment: "pro", Framework: "php",
						LastOccurrenceTimestamp: now.Add(-48 * time.Hour).Unix()},
				},
			},
		})
	}))
	defer srv.Close()

	c := newTestConnector(srv, "my-project", nil)
	records, err := c.Collect(context.Background(), since, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record (in-window only), got %d", len(records))
	}
	r := records[0]
	if r.Source != "rollbar" || r.Service != "my-project" || r.Message != "In window" {
		t.Errorf("unexpected record: %+v", r)
	}
	if r.Count != 1 {
		t.Errorf("expected count=1, got %d", r.Count)
	}
}

func TestCollectEnvironmentFilter(t *testing.T) {
	now := time.Now().UTC()
	since := now.Add(-24 * time.Hour)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(itemsResponse{
			Result: struct {
				Items []item `json:"items"`
			}{
				Items: []item{
					{Title: "Pro error", Level: "error", Environment: "pro",
						LastOccurrenceTimestamp: now.Add(-1 * time.Hour).Unix()},
					{Title: "Staging error", Level: "error", Environment: "staging",
						LastOccurrenceTimestamp: now.Add(-2 * time.Hour).Unix()},
				},
			},
		})
	}))
	defer srv.Close()

	c := newTestConnector(srv, "proj", []string{"pro"})
	records, err := c.Collect(context.Background(), since, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("expected 1 record (pro only), got %d", len(records))
	}
	if len(records) > 0 && records[0].Message != "Pro error" {
		t.Errorf("expected Pro error, got %s", records[0].Message)
	}
}

func TestCollectPagination(t *testing.T) {
	now := time.Now().UTC()
	since := now.Add(-200 * time.Hour)
	callCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		page := r.URL.Query().Get("page")
		if page == "1" {
			// Return exactly 100 items to trigger page 2.
			json.NewEncoder(w).Encode(itemsResponse{
				Result: struct {
					Items []item `json:"items"`
				}{Items: makeItems(100, now.Unix(), "pro")},
			})
		} else {
			// Page 2: fewer items, signals end.
			json.NewEncoder(w).Encode(itemsResponse{
				Result: struct {
					Items []item `json:"items"`
				}{Items: makeItems(5, now.Add(-101*time.Hour).Unix(), "pro")},
			})
		}
	}))
	defer srv.Close()

	c := newTestConnector(srv, "proj", nil)
	records, err := c.Collect(context.Background(), since, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 HTTP calls, got %d", callCount)
	}
	if len(records) != 105 {
		t.Errorf("expected 105 records, got %d", len(records))
	}
}

func TestCollectEarlyExit(t *testing.T) {
	now := time.Now().UTC()
	since := now.Add(-24 * time.Hour)
	callCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		// First page has an item before `since` → early exit, no page 2.
		json.NewEncoder(w).Encode(itemsResponse{
			Result: struct {
				Items []item `json:"items"`
			}{
				Items: []item{
					{Title: "Recent", Level: "error", Environment: "pro",
						LastOccurrenceTimestamp: now.Add(-1 * time.Hour).Unix()},
					{Title: "Old", Level: "error", Environment: "pro",
						LastOccurrenceTimestamp: now.Add(-48 * time.Hour).Unix()},
				},
			},
		})
	}))
	defer srv.Close()

	c := newTestConnector(srv, "proj", nil)
	records, err := c.Collect(context.Background(), since, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 call (early exit), got %d", callCount)
	}
	if len(records) != 1 {
		t.Errorf("expected 1 record, got %d", len(records))
	}
}

func TestCollectHTTP500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer srv.Close()

	c := newTestConnector(srv, "proj", nil)
	_, err := c.Collect(context.Background(), time.Now().Add(-time.Hour), time.Now())
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
}

func TestCollectAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(itemsResponse{Err: 1, ErrMsg: "invalid token"})
	}))
	defer srv.Close()

	c := newTestConnector(srv, "proj", nil)
	_, err := c.Collect(context.Background(), time.Now().Add(-time.Hour), time.Now())
	if err == nil {
		t.Fatal("expected error for API error response")
	}
}

func TestCollectContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return 100 items to trigger pagination.
		json.NewEncoder(w).Encode(itemsResponse{
			Result: struct {
				Items []item `json:"items"`
			}{Items: makeItems(100, time.Now().Unix(), "pro")},
		})
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	c := newTestConnector(srv, "proj", nil)
	_, err := c.Collect(ctx, time.Now().Add(-time.Hour), time.Now())
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}

func TestCollectEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(itemsResponse{
			Result: struct {
				Items []item `json:"items"`
			}{Items: []item{}},
		})
	}))
	defer srv.Close()

	c := newTestConnector(srv, "proj", nil)
	records, err := c.Collect(context.Background(), time.Now().Add(-time.Hour), time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records, got %d", len(records))
	}
}

func TestContains(t *testing.T) {
	if !contains([]string{"a", "b"}, "b") {
		t.Error("should find 'b'")
	}
	if contains([]string{"a", "b"}, "c") {
		t.Error("should not find 'c'")
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
