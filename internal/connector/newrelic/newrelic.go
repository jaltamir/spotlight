package newrelic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jaltamir/spotlight/internal/config"
	"github.com/jaltamir/spotlight/internal/connector"
	"github.com/jaltamir/spotlight/internal/httpclient"
	"github.com/jaltamir/spotlight/internal/version"
)

const defaultEndpoint = "https://api.newrelic.com/graphql"

// Connector fetches error traces from New Relic via NRQL/NerdGraph.
type Connector struct {
	apiKey       string
	accountID    string
	applications []string
	endpoint     string
	client       *http.Client
}

func New(cfg config.ConnectorConfig) *Connector {
	return &Connector{
		apiKey:       cfg.APIKey,
		accountID:    cfg.AccountID,
		applications: cfg.Applications,
		endpoint:     defaultEndpoint,
		client:       httpclient.NewClient(30 * time.Second),
	}
}

func (c *Connector) Name() string { return "newrelic" }

func (c *Connector) Collect(ctx context.Context, since, until time.Time) ([]connector.ErrorRecord, error) {
	nrql := buildNRQL(since, until, c.applications)
	body, err := c.queryNerdGraph(ctx, nrql)
	if err != nil {
		return nil, fmt.Errorf("newrelic query: %w", err)
	}
	return parseResults(body)
}

func buildNRQL(since, until time.Time, apps []string) string {
	q := fmt.Sprintf(
		"SELECT count(*) FROM TransactionError "+
			"WHERE timestamp >= %d AND timestamp <= %d ",
		since.UnixMilli(), until.UnixMilli(),
	)
	if len(apps) > 0 {
		quoted := make([]string, len(apps))
		for i, a := range apps {
			quoted[i] = fmt.Sprintf("'%s'", a)
		}
		q += fmt.Sprintf("AND appName IN (%s) ", strings.Join(quoted, ", "))
	}
	q += "FACET appName, transactionName, error.class, httpResponseCode, error.message LIMIT MAX SINCE 1 day ago"
	return q
}

type nerdGraphResponse struct {
	Data struct {
		Actor struct {
			Account struct {
				NRQL struct {
					Results []nrqlResult `json:"results"`
				} `json:"nrql"`
			} `json:"account"`
		} `json:"actor"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// nrqlResult maps the NerdGraph FACET response format.
// FACET queries return results as: {"facet": ["val1","val2",...], "count": N}
// Facet order matches the FACET clause: appName, transactionName, error.class, httpResponseCode, error.message
type nrqlResult struct {
	Facet []any `json:"facet"`
	Count int   `json:"count"`
}

func (c *Connector) queryNerdGraph(ctx context.Context, nrql string) ([]byte, error) {
	gql := fmt.Sprintf(`{
		actor {
			account(id: %s) {
				nrql(query: "%s") {
					results
				}
			}
		}
	}`, c.accountID, escapeGraphQL(nrql))

	payload := fmt.Sprintf(`{"query":%q}`, gql)
	req, err := http.NewRequestWithContext(ctx, "POST", c.endpoint, strings.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("API-Key", c.apiKey)
	req.Header.Set("User-Agent", version.UserAgent())

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
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	return body, nil
}

func parseResults(data []byte) ([]connector.ErrorRecord, error) {
	var resp nerdGraphResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}
	if len(resp.Errors) > 0 {
		return nil, fmt.Errorf("nerdgraph error: %s", resp.Errors[0].Message)
	}

	var records []connector.ErrorRecord
	now := time.Now().UTC()
	for _, r := range resp.Data.Actor.Account.NRQL.Results {
		// Facet order: appName, transactionName, error.class, httpResponseCode, error.message
		if len(r.Facet) < 5 {
			continue
		}
		appName := facetString(r.Facet[0])
		txnName := facetString(r.Facet[1])
		errClass := facetString(r.Facet[2])
		httpCode := facetString(r.Facet[3])
		errMsg := facetString(r.Facet[4])

		errType := errClass
		if httpCode != "" {
			errType = fmt.Sprintf("HTTP %s", httpCode)
		}

		records = append(records, connector.ErrorRecord{
			Source:    "newrelic",
			Service:  appName,
			Endpoint: txnName,
			ErrorType: errType,
			Message:  errMsg,
			Timestamp: now,
			Count:    r.Count,
		})
	}
	return records, nil
}

func facetString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func escapeGraphQL(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
