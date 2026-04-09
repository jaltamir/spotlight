package aggregator

import (
	"fmt"
	"sort"
	"time"

	"github.com/jaltamir/spotlight/internal/connector"
)

// Group represents a cluster of related errors.
type Group struct {
	Rank           int      `json:"rank"`
	Source         string   `json:"source"`
	Service        string   `json:"service"`
	Endpoint       string   `json:"endpoint"`
	ErrorType      string   `json:"error_type"`
	Count          int      `json:"count"`
	Trend          string   `json:"trend"`
	TrendDetail    string   `json:"trend_detail"`
	FirstSeen      string   `json:"first_seen"`
	LastSeen       string   `json:"last_seen"`
	SampleMessages []string `json:"sample_messages"`
}

// Report is the top-level output structure.
type Report struct {
	GeneratedAt string  `json:"generated_at"`
	TimeWindow  string  `json:"time_window"`
	TotalErrors int     `json:"total_errors"`
	Groups      []Group `json:"groups"`
	Analysis    *string `json:"analysis"`

	// RawRecords carries the original error records through the pipeline
	// for processors and writers that need per-record detail. Excluded from JSON output.
	RawRecords []connector.ErrorRecord `json:"-"`
}

type groupKey struct {
	Source    string
	Service  string
	Endpoint string
	ErrorType string
}

type groupAccum struct {
	key       groupKey
	count     int
	firstSeen time.Time
	lastSeen  time.Time
	messages  []string
}

const maxSampleMessages = 5

// Aggregate groups error records by source, service, endpoint, and error type,
// then sorts by impact score (count × trend weight).
func Aggregate(currentRecords, previousRecords []connector.ErrorRecord, timeWindow string) *Report {
	accums := make(map[groupKey]*groupAccum)

	for _, r := range currentRecords {
		k := groupKey{
			Source:    r.Source,
			Service:   r.Service,
			Endpoint:  r.Endpoint,
			ErrorType: r.ErrorType,
		}
		a, ok := accums[k]
		if !ok {
			a = &groupAccum{key: k, firstSeen: r.Timestamp, lastSeen: r.Timestamp}
			accums[k] = a
		}
		a.count += r.Count
		if r.Timestamp.Before(a.firstSeen) {
			a.firstSeen = r.Timestamp
		}
		if r.Timestamp.After(a.lastSeen) {
			a.lastSeen = r.Timestamp
		}
		if len(a.messages) < maxSampleMessages && r.Message != "" {
			if !containsMessage(a.messages, r.Message) {
				a.messages = append(a.messages, r.Message)
			}
		}
	}

	// Build previous window counts for trend calculation.
	prevCounts := make(map[groupKey]int)
	for _, r := range previousRecords {
		k := groupKey{
			Source:    r.Source,
			Service:   r.Service,
			Endpoint:  r.Endpoint,
			ErrorType: r.ErrorType,
		}
		prevCounts[k] += r.Count
	}

	var groups []Group
	totalErrors := 0
	for _, a := range accums {
		totalErrors += a.count
		trend, trendDetail := computeTrend(a.count, prevCounts[a.key])
		groups = append(groups, Group{
			Source:         a.key.Source,
			Service:        a.key.Service,
			Endpoint:       a.key.Endpoint,
			ErrorType:      a.key.ErrorType,
			Count:          a.count,
			Trend:          trend,
			TrendDetail:    trendDetail,
			FirstSeen:      a.firstSeen.Format(time.RFC3339),
			LastSeen:       a.lastSeen.Format(time.RFC3339),
			SampleMessages: a.messages,
		})
	}

	// Sort by impact: count × trend weight (rising = 3, stable = 1, falling = 0.5).
	sort.Slice(groups, func(i, j int) bool {
		return impactScore(groups[i]) > impactScore(groups[j])
	})

	for i := range groups {
		groups[i].Rank = i + 1
	}

	return &Report{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		TimeWindow:  timeWindow,
		TotalErrors: totalErrors,
		Groups:      groups,
		Analysis:    nil,
	}
}

func computeTrend(current, previous int) (string, string) {
	if previous == 0 {
		if current > 0 {
			return "rising", "new in this window"
		}
		return "stable", "no change"
	}

	pctChange := float64(current-previous) / float64(previous) * 100

	switch {
	case pctChange > 20:
		return "rising", fmt.Sprintf("+%.0f%% vs previous window", pctChange)
	case pctChange < -20:
		return "falling", fmt.Sprintf("%.0f%% vs previous window", pctChange)
	default:
		return "stable", fmt.Sprintf("%+.0f%% vs previous window", pctChange)
	}
}

func impactScore(g Group) float64 {
	weight := 1.0
	switch g.Trend {
	case "rising":
		weight = 3.0
	case "falling":
		weight = 0.5
	}
	return float64(g.Count) * weight
}

func containsMessage(msgs []string, msg string) bool {
	for _, m := range msgs {
		if m == msg {
			return true
		}
	}
	return false
}
