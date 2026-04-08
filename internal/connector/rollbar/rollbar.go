package rollbar

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jaltamir/spotlight/internal/config"
	"github.com/jaltamir/spotlight/internal/connector"
	"github.com/jaltamir/spotlight/internal/httpclient"
	"github.com/jaltamir/spotlight/internal/log"
	"github.com/jaltamir/spotlight/internal/version"
)

const defaultBaseURL = "https://api.rollbar.com/api/1"

// maxPages caps pagination loops (100 items/page → 2000 items max).
const maxPages = 20

// Connector fetches active error items from a Rollbar project.
type Connector struct {
	apiKey       string
	projectSlug  string
	environments []string
	baseURL      string
	client       *http.Client
}

func New(cfg config.ConnectorConfig) *Connector {
	return &Connector{
		apiKey:       cfg.APIKey,
		projectSlug:  cfg.AccountID,
		environments: cfg.Applications,
		baseURL:      defaultBaseURL,
		client:       httpclient.NewClient(30 * time.Second),
	}
}

func (c *Connector) Name() string { return "rollbar" }

func (c *Connector) Collect(ctx context.Context, since, until time.Time) ([]connector.ErrorRecord, error) {
	var records []connector.ErrorRecord

	for page := 1; page <= maxPages; page++ {
		select {
		case <-ctx.Done():
			return records, ctx.Err()
		default:
		}

		items, hasMore, err := c.fetchPage(ctx, page)
		if err != nil {
			return nil, fmt.Errorf("rollbar page %d: %w", page, err)
		}

		for _, it := range items {
			ts := time.Unix(it.LastOccurrenceTimestamp, 0).UTC()

			// Items are sorted by last_occurrence descending.
			// If this item is older than our window, all subsequent are too.
			if ts.Before(since) {
				return records, nil
			}

			// Skip items newer than the window (shouldn't happen, but be safe).
			if ts.After(until) {
				continue
			}

			// Apply environment filter if configured.
			if len(c.environments) > 0 && !contains(c.environments, it.Environment) {
				continue
			}

			records = append(records, connector.ErrorRecord{
				Source:    "rollbar",
				Service:  c.projectSlug,
				Endpoint: it.Framework,
				ErrorType: it.Level,
				Message:  it.Title,
				Timestamp: ts,
				Count:    1,
			})
		}

		if !hasMore {
			break
		}
	}

	return records, nil
}

type itemsResponse struct {
	Err    int    `json:"err"`
	ErrMsg string `json:"message"`
	Result struct {
		Items []item `json:"items"`
	} `json:"result"`
}

type item struct {
	Title                   string `json:"title"`
	Level                   string `json:"level"`
	Environment             string `json:"environment"`
	Framework               string `json:"framework"`
	ProjectSlug             string `json:"project_slug"`
	TotalOccurrences        int    `json:"total_occurrences"`
	LastOccurrenceTimestamp  int64  `json:"last_occurrence_timestamp"`
	FirstOccurrenceTimestamp int64  `json:"first_occurrence_timestamp"`
	Counter                 int    `json:"counter"`
}

func (c *Connector) fetchPage(ctx context.Context, page int) ([]item, bool, error) {
	u := fmt.Sprintf("%s/items/?status=active&level=error&page=%d", c.baseURL, page)

	log.Debug("rollbar fetch", "page", page, "url", u)

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("X-Rollbar-Access-Token", c.apiKey)
	req.Header.Set("User-Agent", version.UserAgent())

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	var parsed itemsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, false, fmt.Errorf("parsing response: %w", err)
	}

	if parsed.Err != 0 {
		return nil, false, fmt.Errorf("rollbar API error: %s", parsed.ErrMsg)
	}

	// Rollbar returns 100 items per page; fewer means last page.
	hasMore := len(parsed.Result.Items) == 100
	return parsed.Result.Items, hasMore, nil
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
