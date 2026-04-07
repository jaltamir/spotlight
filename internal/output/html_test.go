package output

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jaltamir/spotlight/internal/aggregator"
)

func TestHTMLWriterName(t *testing.T) {
	w := NewHTMLWriter()
	if w.Name() != "html" {
		t.Errorf("expected name=html, got %s", w.Name())
	}
}

func TestHTMLWriterWrite(t *testing.T) {
	dir := t.TempDir()
	report := &aggregator.Report{
		GeneratedAt: "2026-04-05T12:00:00Z",
		TimeWindow:  "24h",
		TotalErrors: 99,
		Groups: []aggregator.Group{
			{Rank: 1, Source: "newrelic", Service: "my-svc", Endpoint: "/api/test", ErrorType: "HTTP 500", Count: 50, Trend: "rising", TrendDetail: "+200%", FirstSeen: "2026-04-05T10:00:00Z", LastSeen: "2026-04-05T12:00:00Z", SampleMessages: []string{"error one", "error two"}},
			{Rank: 2, Source: "hubspot", Service: "email", Endpoint: "contacts/bounces", ErrorType: "email_bounce", Count: 49, Trend: "stable", TrendDetail: "+5%", FirstSeen: "2026-04-05T11:00:00Z", LastSeen: "2026-04-05T12:00:00Z"},
		},
	}

	w := NewHTMLWriter()
	if err := w.Write(context.Background(), report, dir, "2026-04-05T120000Z"); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "spotlight-2026-04-05T120000Z.html")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}

	html := string(data)

	// Check basic structure.
	if !strings.Contains(html, "<!DOCTYPE html>") {
		t.Error("missing DOCTYPE")
	}
	if !strings.Contains(html, "Spotlight Report") {
		t.Error("missing title")
	}
	if !strings.Contains(html, "99") {
		t.Error("missing total errors count")
	}
	if !strings.Contains(html, "my-svc") {
		t.Error("missing service name")
	}
	if !strings.Contains(html, "/api/test") {
		t.Error("missing endpoint")
	}
	if !strings.Contains(html, "error one") {
		t.Error("missing sample message")
	}
	if !strings.Contains(html, "badge-rising") {
		t.Error("missing rising badge")
	}
	if !strings.Contains(html, "badge-stable") {
		t.Error("missing stable badge")
	}
}

func TestHTMLWriterWithAnalysis(t *testing.T) {
	dir := t.TempDir()
	analysis := "Root cause: database connection pool exhausted.\nAction: increase pool size."
	report := &aggregator.Report{
		GeneratedAt: "2026-04-05T12:00:00Z",
		TimeWindow:  "1h",
		TotalErrors: 0,
		Groups:      nil,
		Analysis:    &analysis,
	}

	w := NewHTMLWriter()
	if err := w.Write(context.Background(), report, dir, "test"); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "spotlight-test.html"))
	html := string(data)

	if !strings.Contains(html, "AI Analysis") {
		t.Error("missing analysis section")
	}
	if !strings.Contains(html, "database connection pool") {
		t.Error("missing analysis content")
	}
	// Newlines should be converted to <br>.
	if !strings.Contains(html, "<br>") {
		t.Error("newlines should be converted to <br>")
	}
}

func TestHTMLWriterEmptyReport(t *testing.T) {
	dir := t.TempDir()
	report := &aggregator.Report{
		GeneratedAt: "2026-04-05T12:00:00Z",
		TimeWindow:  "1h",
		TotalErrors: 0,
		Groups:      nil,
	}

	w := NewHTMLWriter()
	if err := w.Write(context.Background(), report, dir, "empty"); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "spotlight-empty.html"))
	if !strings.Contains(string(data), "<!DOCTYPE html>") {
		t.Error("empty report should still produce valid HTML")
	}
}

func TestHTMLWriterEscapesXSS(t *testing.T) {
	dir := t.TempDir()
	report := &aggregator.Report{
		GeneratedAt: "2026-04-05T12:00:00Z",
		TimeWindow:  "1h",
		TotalErrors: 1,
		Groups: []aggregator.Group{
			{Rank: 1, Source: "test", Service: "<script>alert(1)</script>", Endpoint: "/xss", ErrorType: "XSS", Count: 1, Trend: "rising", SampleMessages: []string{"<img onerror=alert(1)>"}},
		},
	}

	w := NewHTMLWriter()
	w.Write(context.Background(), report, dir, "xss")

	data, _ := os.ReadFile(filepath.Join(dir, "spotlight-xss.html"))
	html := string(data)

	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Error("XSS in service name not escaped")
	}
	if strings.Contains(html, "<img onerror") {
		t.Error("XSS in message not escaped")
	}
}
