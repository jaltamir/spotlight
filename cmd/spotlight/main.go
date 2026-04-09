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
	"github.com/jaltamir/spotlight/internal/connector/rollbar"
	"github.com/jaltamir/spotlight/internal/log"
	"github.com/jaltamir/spotlight/internal/output"
	"github.com/jaltamir/spotlight/internal/processor"
	"github.com/jaltamir/spotlight/internal/prompt"
	"github.com/jaltamir/spotlight/internal/version"
)

func main() {
	var (
		configPath string
		window     string
		days       int
		debug      bool
	)

	_ = godotenv.Load()

	rootCmd := &cobra.Command{
		Use:   "spotlight",
		Short: "CLI tool that connects to your APMs and integrations, groups errors by pattern, and tells you where to look.",
		RunE: func(cmd *cobra.Command, args []string) error {
			log.SetDebug(debug)

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

			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("invalid config: %w", err)
			}

			duration, _ := time.ParseDuration(cfg.TimeWindow) // already validated above

			now := time.Now().UTC()
			until := now
			since := now.Add(-duration)
			prevSince := since.Add(-duration)

			connectors := buildConnectors(cfg)
			processors := buildProcessors(cfg)
			writers := buildWriters(cfg)

			// Clean and recreate output directory.
			outDir := cfg.OutputDir()
			if err := os.RemoveAll(outDir); err != nil {
				return fmt.Errorf("cleaning output dir: %w", err)
			}
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				return fmt.Errorf("creating output dir: %w", err)
			}

			log.Infof("Collecting errors from %d connector(s) for window %s...", len(connectors), cfg.TimeWindow)

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
					log.Infof("  collecting from %s", c.Name())
					log.Debug("connector started", "connector", c.Name(), "since", since, "until", until)

					r := result{name: c.Name()}

					cur, err := c.Collect(ctx, since, until)
					if err != nil {
						r.err = err
						results <- r
						return
					}
					r.current = cur
					log.Debug("connector current window done", "connector", c.Name(), "records", len(cur))

					prev, err := c.Collect(ctx, prevSince, since)
					if err != nil {
						log.Warn(c.Name()+" (previous window)", err)
					} else {
						r.previous = prev
						log.Debug("connector previous window done", "connector", c.Name(), "records", len(prev))
					}

					results <- r
				}(c)
			}

			wg.Wait()
			close(results)

			var currentRecords, previousRecords []connector.ErrorRecord
			for r := range results {
				if r.err != nil {
					log.Warn(r.name, r.err)
					continue
				}
				currentRecords = append(currentRecords, r.current...)
				previousRecords = append(previousRecords, r.previous...)
			}

			report := aggregator.Aggregate(currentRecords, previousRecords, cfg.TimeWindow)
			report.RawRecords = currentRecords
			log.Infof("Found %d errors in %d group(s)", report.TotalErrors, len(report.Groups))

			// Run processors (e.g. LLM analysis).
			for _, p := range processors {
				log.Infof("  processing with %s", p.Name())
				log.Debug("processor started", "processor", p.Name())
				if err := p.Process(ctx, report); err != nil {
					log.Warn(p.Name(), err)
				}
			}

			// Run all enabled output writers.
			ts := now.Format("2006-01-02T150405Z")
			for _, w := range writers {
				log.Infof("  writing %s", w.Name())
				log.Debug("writer started", "writer", w.Name(), "outDir", outDir)
				if err := w.Write(ctx, report, outDir, ts); err != nil {
					log.Warn(w.Name(), err)
				}
			}

			log.Infof("Done. Output in %s/", outDir)
			return nil
		},
	}

	rootCmd.Flags().StringVarP(&configPath, "config", "c", "spotlight.yaml", "Path to config file")
	rootCmd.Flags().StringVarP(&window, "window", "w", "", "Override time window (e.g. 12h)")
	rootCmd.Flags().IntVarP(&days, "days", "d", 0, "Number of days to look back (overrides window)")
	rootCmd.Flags().BoolVar(&debug, "debug", false, "Enable structured debug logging")

	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version.String())
		},
	})

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
		case "rollbar":
			connectors = append(connectors, rollbar.New(cc))
		}
	}
	return connectors
}

func buildProcessors(cfg *config.Config) []processor.Processor {
	var processors []processor.Processor
	for _, pc := range cfg.Processors {
		if !pc.Enabled {
			continue
		}
		switch pc.Name {
		case "llm":
			promptText, err := prompt.Load(cfg.LLM.PromptFile)
			if err != nil {
				log.Warn("llm prompt", err)
				continue
			}
			processors = append(processors, analyzer.New(cfg.LLM, promptText))
		}
	}
	return processors
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
		case "brief":
			writers = append(writers, output.NewBriefWriter())
		}
	}
	return writers
}
