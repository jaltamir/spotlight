package output

import (
	"context"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"

	"github.com/jaltamir/spotlight/internal/aggregator"
)

const htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Spotlight Report — {{.GeneratedAt}}</title>
<style>
  :root {
    --bg: #0f1117;
    --surface: #1a1d27;
    --border: #2a2d3a;
    --text: #e1e4ed;
    --text-dim: #8b8fa3;
    --accent: #6c8cff;
    --rising: #ff5c5c;
    --stable: #ffc857;
    --falling: #4ecdc4;
    --badge-bg: #252836;
  }
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
    background: var(--bg);
    color: var(--text);
    line-height: 1.5;
    padding: 2rem;
  }
  .header {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    margin-bottom: 2rem;
    padding-bottom: 1rem;
    border-bottom: 1px solid var(--border);
  }
  .header h1 { font-size: 1.5rem; font-weight: 600; }
  .header .meta { color: var(--text-dim); font-size: 0.85rem; }
  .summary {
    display: flex;
    gap: 1.5rem;
    margin-bottom: 2rem;
  }
  .summary-card {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 1rem 1.5rem;
    min-width: 140px;
  }
  .summary-card .label { font-size: 0.75rem; color: var(--text-dim); text-transform: uppercase; letter-spacing: 0.05em; }
  .summary-card .value { font-size: 1.8rem; font-weight: 700; margin-top: 0.25rem; }
  .group {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 8px;
    margin-bottom: 1rem;
    overflow: hidden;
  }
  .group-header {
    display: flex;
    align-items: center;
    gap: 1rem;
    padding: 1rem 1.5rem;
    cursor: pointer;
    user-select: none;
  }
  .group-header:hover { background: rgba(255,255,255,0.02); }
  .rank {
    font-size: 0.75rem;
    font-weight: 700;
    color: var(--text-dim);
    background: var(--badge-bg);
    border-radius: 4px;
    padding: 0.2rem 0.5rem;
    min-width: 2rem;
    text-align: center;
  }
  .group-title { flex: 1; }
  .group-title .service { font-weight: 600; }
  .group-title .endpoint { color: var(--text-dim); font-size: 0.85rem; }
  .badge {
    font-size: 0.7rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    padding: 0.2rem 0.6rem;
    border-radius: 4px;
  }
  .badge-source {
    background: rgba(108,140,255,0.15);
    color: var(--accent);
  }
  .badge-rising { background: rgba(255,92,92,0.15); color: var(--rising); }
  .badge-stable { background: rgba(255,200,87,0.15); color: var(--stable); }
  .badge-falling { background: rgba(78,205,196,0.15); color: var(--falling); }
  .count {
    font-size: 1.3rem;
    font-weight: 700;
    min-width: 4rem;
    text-align: right;
  }
  .group-body {
    display: none;
    padding: 0 1.5rem 1rem;
    border-top: 1px solid var(--border);
  }
  .group.open .group-body { display: block; }
  .group-body table {
    width: 100%;
    font-size: 0.85rem;
    margin-top: 0.75rem;
  }
  .group-body td {
    padding: 0.3rem 0;
    vertical-align: top;
  }
  .group-body td:first-child {
    color: var(--text-dim);
    width: 120px;
    white-space: nowrap;
  }
  .messages {
    margin-top: 0.75rem;
  }
  .messages .msg {
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 4px;
    padding: 0.5rem 0.75rem;
    margin-bottom: 0.4rem;
    font-family: "SF Mono", Menlo, Consolas, monospace;
    font-size: 0.8rem;
    word-break: break-all;
  }
  .analysis {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 1.5rem;
    margin-top: 2rem;
    white-space: pre-wrap;
    font-size: 0.9rem;
    line-height: 1.7;
  }
  .analysis h2 { font-size: 1.1rem; margin-bottom: 1rem; }
  .chevron {
    color: var(--text-dim);
    transition: transform 0.2s;
    font-size: 0.8rem;
  }
  .group.open .chevron { transform: rotate(90deg); }
</style>
</head>
<body>

<div class="header">
  <h1>Spotlight Report</h1>
  <div class="meta">Generated {{.GeneratedAt}} &middot; Window {{.TimeWindow}}</div>
</div>

<div class="summary">
  <div class="summary-card">
    <div class="label">Total Errors</div>
    <div class="value">{{.TotalErrors}}</div>
  </div>
  <div class="summary-card">
    <div class="label">Groups</div>
    <div class="value">{{len .Groups}}</div>
  </div>
  <div class="summary-card">
    <div class="label">Rising</div>
    <div class="value" style="color:var(--rising)">{{countTrend .Groups "rising"}}</div>
  </div>
  <div class="summary-card">
    <div class="label">Stable</div>
    <div class="value" style="color:var(--stable)">{{countTrend .Groups "stable"}}</div>
  </div>
  <div class="summary-card">
    <div class="label">Falling</div>
    <div class="value" style="color:var(--falling)">{{countTrend .Groups "falling"}}</div>
  </div>
</div>

{{range .Groups}}
<div class="group" onclick="this.classList.toggle('open')">
  <div class="group-header">
    <span class="chevron">&#9654;</span>
    <span class="rank">#{{.Rank}}</span>
    <span class="badge badge-source">{{.Source}}</span>
    <div class="group-title">
      <div class="service">{{.Service}}</div>
      <div class="endpoint">{{.Endpoint}}</div>
    </div>
    <span class="badge badge-{{.Trend}}">{{.Trend}}</span>
    <span class="count">{{.Count}}</span>
  </div>
  <div class="group-body">
    <table>
      <tr><td>Error type</td><td>{{.ErrorType}}</td></tr>
      <tr><td>Trend</td><td>{{.TrendDetail}}</td></tr>
      <tr><td>First seen</td><td>{{.FirstSeen}}</td></tr>
      <tr><td>Last seen</td><td>{{.LastSeen}}</td></tr>
    </table>
    {{if .SampleMessages}}
    <div class="messages">
      {{range .SampleMessages}}<div class="msg">{{.}}</div>{{end}}
    </div>
    {{end}}
  </div>
</div>
{{end}}

{{if .Analysis}}
<div class="analysis">
  <h2>AI Analysis</h2>
  {{.Analysis}}
</div>
{{end}}

</body>
</html>`

// HTMLWriter renders the report as a self-contained HTML file.
type HTMLWriter struct{}

func NewHTMLWriter() *HTMLWriter { return &HTMLWriter{} }

func (w *HTMLWriter) Name() string { return "html" }

func (w *HTMLWriter) Write(_ context.Context, report *aggregator.Report, outDir string, timestamp string) error {
	path := filepath.Join(outDir, fmt.Sprintf("spotlight-%s.html", timestamp))
	return writeHTML(report, path)
}

func writeHTML(report *aggregator.Report, path string) error {
	funcMap := template.FuncMap{
		"countTrend": func(groups []aggregator.Group, trend string) int {
			n := 0
			for _, g := range groups {
				if g.Trend == trend {
					n++
				}
			}
			return n
		},
		"deref": func(s *string) string {
			if s == nil {
				return ""
			}
			return *s
		},
	}

	tmpl, err := template.New("report").Funcs(funcMap).Parse(htmlTemplate)
	if err != nil {
		return fmt.Errorf("parsing template: %w", err)
	}

	// Prepare a view with Analysis as template.HTML to render safely.
	type view struct {
		GeneratedAt string
		TimeWindow  string
		TotalErrors int
		Groups      []aggregator.Group
		Analysis    template.HTML
	}

	var analysisHTML template.HTML
	if report.Analysis != nil {
		// Escape but preserve newlines as <br>.
		escaped := template.HTMLEscapeString(*report.Analysis)
		escaped = strings.ReplaceAll(escaped, "\n", "<br>")
		analysisHTML = template.HTML(escaped)
	}

	v := view{
		GeneratedAt: report.GeneratedAt,
		TimeWindow:  report.TimeWindow,
		TotalErrors: report.TotalErrors,
		Groups:      report.Groups,
		Analysis:    analysisHTML,
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating file: %w", err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, v); err != nil {
		return fmt.Errorf("rendering template: %w", err)
	}

	return nil
}
