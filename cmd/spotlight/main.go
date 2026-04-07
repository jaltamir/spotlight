package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"

	"github.com/jaltamir/spotlight/internal/aggregator"
	"github.com/jaltamir/spotlight/internal/analyzer"
	"github.com/jaltamir/spotlight/internal/config"
	"github.com/jaltamir/spotlight/internal/connector"
	"github.com/jaltamir/spotlight/internal/connector/hubspot"
	"github.com/jaltamir/spotlight/internal/connector/newrelic"
	"github.com/jaltamir/spotlight/internal/output"
)

func main() {
	var (
		configPath string
		window     string
		days       int
		analyze    bool
	)

	_ = godotenv.Load()

	rootCmd := &cobra.Command{
		Use:   "spotlight",
		Short: "CLI tool that connects to your APMs and integrations, groups errors by pattern, and tells you where to look.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
			defer cancel()

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			if days > 0 {
				cfg.TimeWindow = fmt.Sprintf("%dh", days*24)
			} else if window != "" {
				cfg.TimeWindow = window
			}

			duration, err := time.ParseDuration(cfg.TimeWindow)
			if err != nil {
				return fmt.Errorf("invalid time window %q: %w", cfg.TimeWindow, err)
			}

			now := time.Now().UTC()
			until := now
			since := now.Add(-duration)
			prevSince := since.Add(-duration)

			connectors := buildConnectors(cfg)
			if len(connectors) == 0 {
				return fmt.Errorf("no connectors enabled in config")
			}

			writers := buildWriters(cfg)
			if len(writers) == 0 {
				return fmt.Errorf("no outputs enabled in config")
			}

			// Clean and recreate output directory.
			outDir := cfg.OutputDir()
			if err := os.RemoveAll(outDir); err != nil {
				return fmt.Errorf("cleaning output dir: %w", err)
			}
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				return fmt.Errorf("creating output dir: %w", err)
			}

			// Collect from all connectors in parallel.
			fmt.Fprintf(os.Stderr, "Collecting errors from %d connector(s) for window %s...\n", len(connectors), cfg.TimeWindow)

			type result struct {
				name     string
				current  []connector.ErrorRecord
				previous []connector.ErrorRecord
				err      error
			}

			results := make(chan result, len(connectors))
			var wg sync.WaitGroup

			for _, c := range connectors {
				wg.Add(1)
				go func(c connector.Connector) {
					defer wg.Done()
					fmt.Fprintf(os.Stderr, "  → %s\n", c.Name())

					r := result{name: c.Name()}

					cur, err := c.Collect(ctx, since, until)
					if err != nil {
						r.err = err
						results <- r
						return
					}
					r.current = cur

					prev, err := c.Collect(ctx, prevSince, since)
					if err != nil {
						fmt.Fprintf(os.Stderr, "  ⚠ %s (previous window): %v\n", c.Name(), err)
					} else {
						r.previous = prev
					}

					results <- r
				}(c)
			}

			wg.Wait()
			close(results)

			var currentRecords, previousRecords []connector.ErrorRecord
			for r := range results {
				if r.err != nil {
					fmt.Fprintf(os.Stderr, "  ⚠ %s: %v\n", r.name, r.err)
					continue
				}
				currentRecords = append(currentRecords, r.current...)
				previousRecords = append(previousRecords, r.previous...)
			}

			report := aggregator.Aggregate(currentRecords, previousRecords, cfg.TimeWindow)
			fmt.Fprintf(os.Stderr, "Found %d errors in %d group(s)\n", report.TotalErrors, len(report.Groups))

			if analyze || cfg.LLM.Enabled {
				if cfg.LLM.APIKey == "" {
					return fmt.Errorf("--analyze requires llm.api_key in config or ANTHROPIC_API_KEY env var")
				}
				fmt.Fprintf(os.Stderr, "Running AI analysis with %s...\n", cfg.LLM.Model)
				text, err := analyzer.Analyze(ctx, report, cfg.LLM)
				if err != nil {
					fmt.Fprintf(os.Stderr, "  ⚠ analysis failed: %v\n", err)
				} else {
					report.Analysis = &text
				}
			}

			// Run all enabled output writers.
			ts := now.Format("2006-01-02T150405Z")
			for _, w := range writers {
				fmt.Fprintf(os.Stderr, "  ← %s\n", w.Name())
				if err := w.Write(ctx, report, outDir, ts); err != nil {
					fmt.Fprintf(os.Stderr, "  ⚠ %s: %v\n", w.Name(), err)
				}
			}

			fmt.Fprintf(os.Stderr, "Done. Output in %s/\n", outDir)
			return nil
		},
	}

	rootCmd.Flags().StringVarP(&configPath, "config", "c", "spotlight.yaml", "Path to config file")
	rootCmd.Flags().StringVarP(&window, "window", "w", "", "Override time window (e.g. 12h)")
	rootCmd.Flags().IntVarP(&days, "days", "d", 0, "Number of days to look back (overrides window)")
	rootCmd.Flags().BoolVar(&analyze, "analyze", false, "Run AI analysis on grouped errors")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func buildConnectors(cfg *config.Config) []connector.Connector {
	var connectors []connector.Connector
	for _, cc := range cfg.Connectors {
		if !cc.Enabled {
			continue
		}
		switch cc.Name {
		case "newrelic":
			connectors = append(connectors, newrelic.New(cc))
		case "hubspot":
			connectors = append(connectors, hubspot.New(cc))
		}
	}
	return connectors
}

func buildWriters(cfg *config.Config) []output.Writer {
	var writers []output.Writer
	for _, oc := range cfg.Outputs {
		if !oc.Enabled {
			continue
		}
		switch oc.Name {
		case "json":
			writers = append(writers, output.NewJSONWriter())
		case "html":
			writers = append(writers, output.NewHTMLWriter())
		case "s3":
			writers = append(writers, output.NewS3Writer(oc.S3))
		}
	}
	return writers
}
