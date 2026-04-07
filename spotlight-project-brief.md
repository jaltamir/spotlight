# Spotlight — Project Brief for Claude Code

## Overview

**Spotlight** is a CLI tool written in Go that connects to multiple data sources (APMs, CRMs, integration platforms), extracts errors and logs, groups them by patterns, and outputs a structured JSON report highlighting what needs attention. Optionally, it can send the grouped results to Claude's API for AI-powered interpretation.

The goal is to be an automated triage tool: connect, collect, group, prioritize — so the engineer knows where to look without manually digging through dashboards.

## Name & Repo

- **Name:** spotlight
- **Repo:** Public GitHub repo
- **License:** MIT

## Architecture

### Core Concepts

1. **Connectors** — Pluggable modules that know how to connect to a specific data source and extract error/log data. Each connector implements a common interface.
2. **Aggregator** — Takes raw error data from all connectors, groups it by patterns, and prioritizes by impact.
3. **Output** — Generates a structured JSON report. Optionally uploads to S3.
4. **Analyzer (optional)** — Sends the grouped JSON to Claude API for interpretation.

### Connector Interface

Each connector must implement:
- Connect to the source using credentials from config
- Extract errors/logs for a given time window
- Return normalized error records with: source, endpoint/service, error type/code, message, timestamp, count

### V1 Connectors

1. **New Relic** — Connects via New Relic API. Extracts error traces, groups by application/service, endpoint, error type, HTTP status code, and error message.
2. **HubSpot** — Connects via HubSpot API. Extracts errors from webhook logs and API call logs. Groups by endpoint, HTTP status code, and error message.

### Grouping Strategy (Deterministic)

The aggregator groups errors in layers:
1. **By source** (New Relic, HubSpot, etc.)
2. **By service/endpoint**
3. **By error type / HTTP status code**
4. **By volume and trend** — calculate frequency over the time window and detect spikes (e.g., an error going from 10 to 500 occurrences)

Each group includes:
- Source name
- Service/endpoint identifier
- Error type/code
- Total count in the time window
- Trend indicator (rising, stable, falling)
- Sample error messages (raw, not interpreted)
- First seen / last seen timestamps

Groups are sorted by impact: volume × trend weight (rising errors rank higher).

Do NOT attempt to group by message similarity — leave raw messages for the LLM to interpret.

### Output Format

The CLI generates a JSON file with this structure:

```json
{
  "generated_at": "2026-04-05T15:30:00Z",
  "time_window": "24h",
  "total_errors": 4523,
  "groups": [
    {
      "rank": 1,
      "source": "newrelic",
      "service": "ms-hubspot-integration",
      "endpoint": "/api/webhooks/hubspot",
      "error_type": "HTTP 500",
      "count": 1847,
      "trend": "rising",
      "trend_detail": "+340% vs previous window",
      "first_seen": "2026-04-04T16:00:00Z",
      "last_seen": "2026-04-05T15:28:00Z",
      "sample_messages": [
        "Contact not found in HubSpot search API",
        "Search API returned empty result for known contact",
        "Timeout waiting for HubSpot search response"
      ]
    }
  ],
  "analysis": null
}
```

When the `--analyze` flag is used, the `analysis` field is populated with Claude's interpretation.

### Claude API Integration

When invoked with `--analyze` or when `llm.enabled: true` in config:
1. Take the grouped JSON output
2. Build a prompt: "You are a senior engineer analyzing error patterns from multiple sources. Here are the grouped errors from the last [time_window]. For each group, explain what might be happening, identify possible root causes, and suggest investigation steps. Prioritize by business impact."
3. Send to Claude API (model: claude-sonnet-4-6 by default, configurable)
4. Include Claude's response in the `analysis` field of the JSON output

### S3 Upload

When S3 credentials are provided in config:
- Upload the JSON report to the specified S3 bucket/path
- Use a timestamped filename: `spotlight-report-2026-04-05T153000Z.json`
- Optionally keep the last N reports (configurable)

## Configuration

Single YAML config file. Example:

```yaml
# spotlight.yaml

time_window: "24h"  # How far back to look

connectors:
  - name: newrelic
    enabled: true
    api_key: "${NEWRELIC_API_KEY}"  # Supports env var references
    account_id: "1234567"
    # Optional: filter by specific applications
    applications:
      - "ms-hubspot-integration"
      - "next-api"

  - name: hubspot
    enabled: true
    api_key: "${HUBSPOT_API_KEY}"
    # What to monitor
    monitor:
      - webhooks
      - api_calls

output:
  format: "json"
  path: "./reports"  # Local output directory

  s3:
    enabled: false
    bucket: "my-spotlight-reports"
    region: "eu-west-1"
    access_key: "${AWS_ACCESS_KEY_ID}"
    secret_key: "${AWS_SECRET_ACCESS_KEY}"
    prefix: "reports/"
    retain_last: 30  # Keep last 30 reports

llm:
  enabled: false
  provider: "claude"
  api_key: "${ANTHROPIC_API_KEY}"
  model: "claude-sonnet-4-6"  # Configurable
```

## CLI Usage

```bash
# Basic usage — generate report to stdout
spotlight --config spotlight.yaml

# Save to file
spotlight --config spotlight.yaml --output report.json

# With AI analysis
spotlight --config spotlight.yaml --analyze

# Override time window
spotlight --config spotlight.yaml --window 12h

# Upload to S3
spotlight --config spotlight.yaml --s3

# Full: analyze + save + upload
spotlight --config spotlight.yaml --analyze --s3 --output report.json
```

## Project Structure

```
spotlight/
├── cmd/
│   └── spotlight/
│       └── main.go           # CLI entry point (cobra or similar)
├── internal/
│   ├── config/
│   │   └── config.go         # YAML config parsing
│   ├── connector/
│   │   ├── connector.go      # Connector interface definition
│   │   ├── newrelic/
│   │   │   └── newrelic.go   # New Relic connector implementation
│   │   └── hubspot/
│   │       └── hubspot.go    # HubSpot connector implementation
│   ├── aggregator/
│   │   └── aggregator.go     # Error grouping and prioritization
│   ├── analyzer/
│   │   └── analyzer.go       # LLM integration (Claude API)
│   └── output/
│       ├── json.go           # JSON report generation
│       └── s3.go             # S3 upload
├── spotlight.yaml.example    # Example configuration
├── go.mod
├── go.sum
├── LICENSE                   # MIT
└── README.md
```

## README Guidelines

The README should include:
- Clear one-line description: "CLI tool that connects to your APMs and integrations, groups errors by pattern, and tells you where to look."
- Quick start with example config
- How to add new connectors (interface documentation)
- Example output JSON
- Note that LLM analysis is optional and requires an Anthropic API key

## Technical Decisions

- **Language:** Go — compiles to a single binary, no runtime dependencies
- **Config:** YAML with environment variable support for secrets
- **CLI framework:** Cobra (standard in Go CLI tools)
- **HTTP client:** Standard library net/http (no need for external deps for API calls)
- **No database:** Stateless tool, runs on demand
- **No frontend:** JSON output only. A separate dashboard project may come later.

## V1 Scope

Keep it focused:
- Two connectors: New Relic, HubSpot
- Deterministic grouping by source → service → error type → volume/trend
- JSON output to local file
- Optional S3 upload
- Optional Claude API analysis
- Solid README with example config

## Future Ideas (NOT v1)

- More connectors: Datadog, Sentry, Elasticsearch, CloudWatch
- Static HTML dashboard that reads the JSON from S3
- Scheduled execution (cron / Lambda)
- Slack/Teams notifications for new high-priority groups
- Diff between reports (what's new since last run)
- More LLM providers (OpenAI, etc.)
