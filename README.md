# Spotlight

CLI tool that connects to your APMs and integrations, groups errors by pattern, and tells you where to look.

## What it does

Spotlight collects errors from multiple data sources in parallel, groups them by pattern (source, service, endpoint, error type), calculates trends against the previous time window, and outputs structured reports. Optionally, it can send the grouped results to Claude's API for AI-powered root cause analysis.

## Architecture

```
Input connectors            Aggregator            Output writers
┌──────────┐                                      ┌──────┐
│ newrelic  │──┐                              ┌──▶ │ json │
└──────────┘  │    ┌─────────────────┐        │    └──────┘
              ├──▶ │ group + rank by │ ──────▶ ├──▶ ┌──────┐
┌──────────┐  │    │ impact score    │        │    │ html │
│ hubspot  │──┘    └─────────────────┘        │    └──────┘
└──────────┘                                  ├──▶ ┌──────┐
                                              │    │  s3  │
                                              │    └──────┘
                                              └──▶ (slack, llm, ...)
```

Both input connectors and output writers implement a simple interface and are configured in `spotlight.yaml`.

## Quick start

```bash
# Build
go build -o spotlight ./cmd/spotlight/

# Create config from the dist file
cp spotlight.yaml.dist spotlight.yaml

# Create a .env with your API keys
cat > .env <<EOF
NEWRELIC_API_KEY=NRAK-...
NEWRELIC_ACCOUNT_ID=1234567
NEWRELIC_APMS=my-service-a, my-service-b
HUBSPOT_API_KEY=pat-eu1-...
EOF

# Run (default: last 24h)
./spotlight

# Last 3 days, HTML report
./spotlight -d 3
```

## CLI flags

```
-c, --config string   Path to config file (default "spotlight.yaml")
-d, --days int        Number of days to look back (overrides window)
-w, --window string   Override time window (e.g. 12h)
    --analyze         Run AI analysis on grouped errors
```

## Configuration

Single YAML file with `${ENV_VAR}` expansion for secrets.

```yaml
time_window: "24h"

# --- Input connectors ---
connectors:
  - name: newrelic
    enabled: true
    api_key: "${NEWRELIC_API_KEY}"
    account_id: "${NEWRELIC_ACCOUNT_ID}"
    applications:
      - "${NEWRELIC_APMS}"   # comma-separated from env var

  - name: hubspot
    enabled: true
    api_key: "${HUBSPOT_API_KEY}"

# --- Output writers ---
outputs:
  - name: json
    enabled: true
    path: "./reports"

  - name: html
    enabled: true
    path: "./reports"

  - name: s3
    enabled: false
    s3:
      bucket: "my-bucket"
      region: "eu-west-1"
      access_key: "${AWS_ACCESS_KEY_ID}"
      secret_key: "${AWS_SECRET_ACCESS_KEY}"
      prefix: "reports/"
      retain_last: 30

# --- Optional AI analysis ---
llm:
  enabled: false
  api_key: "${ANTHROPIC_API_KEY}"
  model: "claude-sonnet-4-6"
```

The `.env` file is loaded automatically via [godotenv](https://github.com/joho/godotenv). Existing env vars take precedence.

## Input connectors

| Connector | Source | What it collects |
|-----------|--------|-----------------|
| **newrelic** | NerdGraph API | Transaction errors faceted by app, endpoint, error class, HTTP status, and message |
| **hubspot** | CRM Search API | Email bounces, invalid addresses, quarantined contacts |
| | Audit Logs API | Critical security events (scope changes, key creation, logins) |
| | API Usage API | Rate limit warnings (fires when usage > 80% of daily limit) |

### HubSpot scopes required

The HubSpot Private App token needs these scopes:

| Scope | What it unlocks |
|-------|----------------|
| `crm.objects.contacts.read` | CRM search for bounces/invalid/quarantined |
| `account-info.security.read` | Audit logs (read-only) |
| `oauth` | API usage endpoint, account details |

## Output writers

| Writer | What it produces |
|--------|-----------------|
| **json** | `reports/spotlight-{timestamp}.json` |
| **html** | `reports/spotlight-{timestamp}.html` — dark theme, expandable groups, trend badges |
| **s3** | Uploads JSON to S3 with timestamped key and optional retention pruning |

The output directory is cleaned at the start of each run.

## Adding a new input connector

Implement the `Connector` interface:

```go
type Connector interface {
    Name() string
    Collect(ctx context.Context, since, until time.Time) ([]ErrorRecord, error)
}
```

Register it in `buildConnectors()` in `cmd/spotlight/main.go`.

## Adding a new output writer

Implement the `Writer` interface:

```go
type Writer interface {
    Name() string
    Write(ctx context.Context, report *aggregator.Report, outDir, timestamp string) error
}
```

Register it in `buildWriters()` in `cmd/spotlight/main.go`.

## Output JSON format

```json
{
  "generated_at": "2026-04-05T15:30:00Z",
  "time_window": "24h",
  "total_errors": 231,
  "groups": [
    {
      "rank": 1,
      "source": "newrelic",
      "service": "my-api-service",
      "endpoint": "WebTransaction/Expressjs/POST//users/:id/sync",
      "error_type": "HTTP 500",
      "count": 90,
      "trend": "rising",
      "trend_detail": "+340% vs previous window",
      "first_seen": "2026-04-05T16:00:00Z",
      "last_seen": "2026-04-05T18:32:26Z",
      "sample_messages": [
        "Connection refused",
        "Request timeout after 30s"
      ]
    }
  ],
  "analysis": null
}
```

Groups are sorted by impact score: `count * trend_weight` (rising=3, stable=1, falling=0.5).

## AI analysis

The `--analyze` flag (or `llm.enabled: true`) sends the grouped report to Claude's API for interpretation. Requires `ANTHROPIC_API_KEY`. The model defaults to `claude-sonnet-4-6`.

## Project structure

```
spotlight/
├── cmd/spotlight/
│   └── main.go              # CLI, connector/writer orchestration
├── internal/
│   ├── config/              # YAML parsing, env expansion
│   ├── connector/
│   │   ├── connector.go     # Connector interface + ErrorRecord
│   │   ├── newrelic/        # New Relic NerdGraph connector
│   │   └── hubspot/         # HubSpot CRM/audit/usage connector
│   ├── aggregator/          # Grouping, trend calculation, ranking
│   ├── analyzer/            # Claude API integration
│   └── output/
│       ├── writer.go        # Writer interface
│       ├── json.go          # JSON output
│       ├── html.go          # HTML report
│       └── s3.go            # S3 upload
├── spotlight.yaml.dist      # Example config (versioned)
├── .env                     # API keys (gitignored)
└── spotlight.yaml           # Local config (gitignored)
```

## License

MIT
