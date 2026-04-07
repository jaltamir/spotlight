package aggregator

import (
	"testing"
	"time"

	"github.com/jaltamir/spotlight/internal/connector"
)

func TestAggregateGroupsByKey(t *testing.T) {
	now := time.Now().UTC()
	records := []connector.ErrorRecord{
		{Source: "newrelic", Service: "svc-a", Endpoint: "/api/foo", ErrorType: "HTTP 500", Message: "err1", Timestamp: now, Count: 10},
		{Source: "newrelic", Service: "svc-a", Endpoint: "/api/foo", ErrorType: "HTTP 500", Message: "err2", Timestamp: now.Add(-time.Hour), Count: 5},
		{Source: "newrelic", Service: "svc-b", Endpoint: "/api/bar", ErrorType: "HTTP 404", Message: "not found", Timestamp: now, Count: 3},
	}

	report := Aggregate(records, nil, "24h")

	if report.TotalErrors != 18 {
		t.Errorf("expected TotalErrors=18, got %d", report.TotalErrors)
	}
	if len(report.Groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(report.Groups))
	}
	if report.Groups[0].Rank != 1 || report.Groups[1].Rank != 2 {
		t.Error("ranks not set correctly")
	}
}

func TestAggregateDeduplicatesMessages(t *testing.T) {
	now := time.Now().UTC()
	records := []connector.ErrorRecord{
		{Source: "newrelic", Service: "svc", Endpoint: "/ep", ErrorType: "HTTP 500", Message: "same error", Timestamp: now, Count: 1},
		{Source: "newrelic", Service: "svc", Endpoint: "/ep", ErrorType: "HTTP 500", Message: "same error", Timestamp: now, Count: 1},
		{Source: "newrelic", Service: "svc", Endpoint: "/ep", ErrorType: "HTTP 500", Message: "different error", Timestamp: now, Count: 1},
	}

	report := Aggregate(records, nil, "1h")
	if len(report.Groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(report.Groups))
	}
	if len(report.Groups[0].SampleMessages) != 2 {
		t.Errorf("expected 2 unique messages, got %d", len(report.Groups[0].SampleMessages))
	}
}

func TestAggregateWithTrend(t *testing.T) {
	now := time.Now().UTC()
	current := []connector.ErrorRecord{
		{Source: "newrelic", Service: "svc", Endpoint: "/ep", ErrorType: "HTTP 500", Message: "err", Timestamp: now, Count: 100},
	}
	previous := []connector.ErrorRecord{
		{Source: "newrelic", Service: "svc", Endpoint: "/ep", ErrorType: "HTTP 500", Message: "err", Timestamp: now.Add(-24 * time.Hour), Count: 10},
	}

	report := Aggregate(current, previous, "24h")
	if len(report.Groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(report.Groups))
	}
	g := report.Groups[0]
	if g.Trend != "rising" {
		t.Errorf("expected trend=rising, got %s", g.Trend)
	}
	if g.Count != 100 {
		t.Errorf("expected count=100, got %d", g.Count)
	}
}

func TestAggregateRankingByImpact(t *testing.T) {
	now := time.Now().UTC()
	current := []connector.ErrorRecord{
		// Low count but rising (new) → impact = 5 * 3 = 15
		{Source: "src", Service: "svc", Endpoint: "/new", ErrorType: "HTTP 500", Message: "new err", Timestamp: now, Count: 5},
		// High count but falling → impact = 100 * 0.5 = 50
		{Source: "src", Service: "svc", Endpoint: "/old", ErrorType: "HTTP 500", Message: "old err", Timestamp: now, Count: 100},
	}
	previous := []connector.ErrorRecord{
		{Source: "src", Service: "svc", Endpoint: "/old", ErrorType: "HTTP 500", Message: "old err", Timestamp: now.Add(-24 * time.Hour), Count: 200},
	}

	report := Aggregate(current, previous, "24h")
	if len(report.Groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(report.Groups))
	}
	// /old has higher impact score (50) than /new (15)
	if report.Groups[0].Endpoint != "/old" {
		t.Errorf("expected /old ranked first, got %s", report.Groups[0].Endpoint)
	}
}

func TestComputeTrend(t *testing.T) {
	tests := []struct {
		current, previous int
		wantTrend         string
	}{
		{100, 0, "rising"},
		{100, 10, "rising"},
		{100, 90, "stable"},
		{100, 100, "stable"},
		{10, 100, "falling"},
		{0, 0, "stable"},
	}

	for _, tt := range tests {
		trend, _ := computeTrend(tt.current, tt.previous)
		if trend != tt.wantTrend {
			t.Errorf("computeTrend(%d, %d) = %s, want %s", tt.current, tt.previous, trend, tt.wantTrend)
		}
	}
}
