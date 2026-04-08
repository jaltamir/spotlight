package hubspot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jaltamir/spotlight/internal/config"
	"github.com/jaltamir/spotlight/internal/connector"
)

const defaultBaseURL = "https://api.hubapi.com"

// maxPages caps pagination loops to avoid runaway requests.
const maxPages = 50

// Connector fetches errors from HubSpot via CRM search (email issues),
// audit logs (security events), and API usage (rate limit proximity).
type Connector struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

func New(cfg config.ConnectorConfig) *Connector {
	return &Connector{
		apiKey:  cfg.APIKey,
		baseURL: defaultBaseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Connector) Name() string { return "hubspot" }

func (c *Connector) Collect(ctx context.Context, since, until time.Time) ([]connector.ErrorRecord, error) {
	sinceMilli := since.UnixMilli()

	var all []connector.ErrorRecord

	// 1. Email bounces in the time window.
	bounces, err := c.searchContacts(ctx, sinceMilli, crmSearchRequest{
		Filters: []crmFilter{
			{Property: "hs_email_bounce", Operator: "GT", Value: "0"},
		},
		Properties: []string{"email", "hs_email_bounce", "hs_email_hard_bounce_reason_enum", "lastmodifieddate"},
	})
	if err != nil {
		return nil, fmt.Errorf("bounces: %w", err)
	}
	for _, r := range bounces {
		reason := propOr(r, "hs_email_hard_bounce_reason_enum", "unknown")
		all = append(all, connector.ErrorRecord{
			Source:    "hubspot",
			Service:  "email",
			Endpoint: "contacts/bounces",
			ErrorType: "email_bounce",
			Message:  fmt.Sprintf("bounce count=%s, reason=%s, email=%s", propOr(r, "hs_email_bounce", "?"), reason, propOr(r, "email", "")),
			Timestamp: parseHubspotTime(propOr(r, "lastmodifieddate", "")),
			Count:    1,
		})
	}

	// 2. Invalid email addresses flagged in the time window.
	badAddr, err := c.searchContacts(ctx, sinceMilli, crmSearchRequest{
		Filters: []crmFilter{
			{Property: "hs_email_bad_address", Operator: "EQ", Value: "true"},
		},
		Properties: []string{"email", "lastmodifieddate"},
	})
	if err != nil {
		return nil, fmt.Errorf("bad addresses: %w", err)
	}
	for _, r := range badAddr {
		all = append(all, connector.ErrorRecord{
			Source:    "hubspot",
			Service:  "email",
			Endpoint: "contacts/bad_address",
			ErrorType: "invalid_email",
			Message:  fmt.Sprintf("invalid email address: %s", propOr(r, "email", "")),
			Timestamp: parseHubspotTime(propOr(r, "lastmodifieddate", "")),
			Count:    1,
		})
	}

	// 3. Quarantined contacts in the time window.
	quarantined, err := c.searchContacts(ctx, sinceMilli, crmSearchRequest{
		Filters: []crmFilter{
			{Property: "hs_email_quarantined", Operator: "EQ", Value: "true"},
		},
		Properties: []string{"email", "hs_email_quarantined_reason", "lastmodifieddate"},
	})
	if err != nil {
		return nil, fmt.Errorf("quarantined: %w", err)
	}
	for _, r := range quarantined {
		reason := propOr(r, "hs_email_quarantined_reason", "unknown")
		all = append(all, connector.ErrorRecord{
			Source:    "hubspot",
			Service:  "email",
			Endpoint: "contacts/quarantined",
			ErrorType: "quarantined",
			Message:  fmt.Sprintf("quarantined: reason=%s, email=%s", reason, propOr(r, "email", "")),
			Timestamp: parseHubspotTime(propOr(r, "lastmodifieddate", "")),
			Count:    1,
		})
	}

	// 4. Audit logs — critical security events in the time window.
	auditRecords, err := c.collectAuditLogs(ctx, since, until)
	if err != nil {
		return nil, fmt.Errorf("audit logs: %w", err)
	}
	all = append(all, auditRecords...)

	// 5. API usage — flag if usage is above 80% of daily limit.
	usageRecords, err := c.collectAPIUsage(ctx)
	if err != nil {
		return nil, fmt.Errorf("api usage: %w", err)
	}
	all = append(all, usageRecords...)

	return all, nil
}

func (c *Connector) collectAuditLogs(ctx context.Context, since, until time.Time) ([]connector.ErrorRecord, error) {
	type auditResult struct {
		Category    string `json:"category"`
		SubCategory string `json:"subCategory"`
		Action      string `json:"action"`
		OccurredAt  string `json:"occurredAt"`
		ActingUser  struct {
			UserEmail string `json:"userEmail"`
		} `json:"actingUser"`
		TargetObjectID string `json:"targetObjectId"`
	}
	type auditResponse struct {
		Results []auditResult `json:"results"`
		Paging  *struct {
			Next *struct {
				After string `json:"after"`
			} `json:"next"`
		} `json:"paging"`
	}

	var records []connector.ErrorRecord
	after := ""

	for page := 0; page < maxPages; page++ {
		select {
		case <-ctx.Done():
			return records, ctx.Err()
		default:
		}

		u := fmt.Sprintf("%s/account-info/v3/activity/audit-logs?limit=100&occurredAfter=%s&occurredBefore=%s",
			c.baseURL, since.Format(time.RFC3339), until.Format(time.RFC3339))
		if after != "" {
			u += "&after=" + after
		}

		body, err := c.doGet(ctx, u)
		if err != nil {
			return nil, err
		}

		var resp auditResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("parsing audit logs: %w", err)
		}

		for _, r := range resp.Results {
			if r.Category != "CRITICAL_ACTION" && r.Category != "LOGIN" {
				continue
			}
			records = append(records, connector.ErrorRecord{
				Source:    "hubspot",
				Service:   "security",
				Endpoint:  fmt.Sprintf("audit/%s", r.SubCategory),
				ErrorType: r.Category,
				Message:   fmt.Sprintf("%s %s by %s (target: %s)", r.Action, r.SubCategory, r.ActingUser.UserEmail, r.TargetObjectID),
				Timestamp: parseHubspotTime(r.OccurredAt),
				Count:     1,
			})
		}

		if resp.Paging == nil || resp.Paging.Next == nil || resp.Paging.Next.After == "" {
			break
		}
		after = resp.Paging.Next.After
	}

	return records, nil
}

func (c *Connector) collectAPIUsage(ctx context.Context) ([]connector.ErrorRecord, error) {
	body, err := c.doGet(ctx, c.baseURL+"/account-info/v3/api-usage/daily/private-apps")
	if err != nil {
		return nil, err
	}

	var resp struct {
		Results []struct {
			Name         string `json:"name"`
			UsageLimit   int    `json:"usageLimit"`
			CurrentUsage int    `json:"currentUsage"`
			CollectedAt  string `json:"collectedAt"`
			ResetsAt     string `json:"resetsAt"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing api usage: %w", err)
	}

	var records []connector.ErrorRecord
	for _, r := range resp.Results {
		if r.UsageLimit == 0 {
			continue
		}
		pct := float64(r.CurrentUsage) / float64(r.UsageLimit) * 100
		if pct >= 80 {
			records = append(records, connector.ErrorRecord{
				Source:    "hubspot",
				Service:  "api_usage",
				Endpoint: "account/rate_limit",
				ErrorType: "rate_limit_warning",
				Message:  fmt.Sprintf("%s: %d/%d (%.0f%%) — resets at %s", r.Name, r.CurrentUsage, r.UsageLimit, pct, r.ResetsAt),
				Timestamp: parseHubspotTime(r.CollectedAt),
				Count:    1,
			})
		}
	}
	return records, nil
}

type crmFilter struct {
	Property string `json:"propertyName"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}

type crmSearchRequest struct {
	Filters    []crmFilter
	Properties []string
}

type crmPaging struct {
	Next *struct {
		After string `json:"after"`
	} `json:"next"`
}

type crmSearchResponse struct {
	Total   int         `json:"total"`
	Results []crmResult `json:"results"`
	Paging  *crmPaging  `json:"paging"`
}

type crmResult struct {
	Properties map[string]string `json:"properties"`
}

func (c *Connector) searchContacts(ctx context.Context, sinceMilli int64, req crmSearchRequest) ([]crmResult, error) {
	// Copy filters to avoid mutating the caller's slice.
	filters := make([]crmFilter, len(req.Filters), len(req.Filters)+1)
	copy(filters, req.Filters)
	filters = append(filters, crmFilter{
		Property: "lastmodifieddate",
		Operator: "GTE",
		Value:    fmt.Sprintf("%d", sinceMilli),
	})

	var all []crmResult
	after := ""

	for page := 0; page < maxPages; page++ {
		select {
		case <-ctx.Done():
			return all, ctx.Err()
		default:
		}

		payload := map[string]any{
			"filterGroups": []map[string]any{
				{"filters": filters},
			},
			"properties": req.Properties,
			"sorts":      []map[string]string{{"propertyName": "lastmodifieddate", "direction": "DESCENDING"}},
			"limit":      100,
		}
		if after != "" {
			payload["after"] = after
		}

		body, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}

		respBody, err := c.doPost(ctx, c.baseURL+"/crm/v3/objects/contacts/search", body)
		if err != nil {
			return nil, err
		}

		var resp crmSearchResponse
		if err := json.Unmarshal(respBody, &resp); err != nil {
			return nil, fmt.Errorf("parsing response: %w", err)
		}

		all = append(all, resp.Results...)

		if resp.Paging == nil || resp.Paging.Next == nil || resp.Paging.Next.After == "" {
			break
		}
		after = resp.Paging.Next.After
	}

	return all, nil
}

func (c *Connector) doGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("User-Agent", "Spotlight/1.0")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from HubSpot API", resp.StatusCode)
	}

	return body, nil
}

func (c *Connector) doPost(ctx context.Context, url string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Spotlight/1.0")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from HubSpot API", resp.StatusCode)
	}

	return respBody, nil
}

func propOr(r crmResult, key, fallback string) string {
	if v, ok := r.Properties[key]; ok && v != "" {
		return v
	}
	return fallback
}

func parseHubspotTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}
