package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	TimeWindow string            `yaml:"time_window"`
	Connectors []ConnectorConfig `yaml:"connectors"`
	Enrichers  []EnricherConfig  `yaml:"enrichers"`
	Outputs    []OutputConfig    `yaml:"outputs"`
	LLM        LLMConfig         `yaml:"llm"`
}

type EnricherConfig struct {
	Name    string `yaml:"name"`
	Enabled bool   `yaml:"enabled"`
}

type ConnectorConfig struct {
	Name         string   `yaml:"name"`
	Enabled      bool     `yaml:"enabled"`
	APIKey       string   `yaml:"api_key"`
	AccountID    string   `yaml:"account_id"`
	Applications []string `yaml:"applications"`
	Monitor      []string `yaml:"monitor"`
}

type OutputConfig struct {
	Name    string   `yaml:"name"`
	Enabled bool     `yaml:"enabled"`
	Path    string   `yaml:"path"`
	S3      S3Config `yaml:"s3"`
}

type S3Config struct {
	Bucket     string `yaml:"bucket"`
	Region     string `yaml:"region"`
	AccessKey  string `yaml:"access_key"`
	SecretKey  string `yaml:"secret_key"`
	Prefix     string `yaml:"prefix"`
	RetainLast int    `yaml:"retain_last"`
}

type LLMConfig struct {
	Provider string `yaml:"provider"`
	APIKey   string `yaml:"api_key"`
	Model    string `yaml:"model"`
	BaseURL  string `yaml:"base_url"`
}

var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// Load reads and parses a YAML config file, expanding environment variable references.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	expanded := expandEnvVars(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if cfg.TimeWindow == "" {
		cfg.TimeWindow = "24h"
	}
	if cfg.LLM.Provider == "" {
		cfg.LLM.Provider = "anthropic"
	}
	if cfg.LLM.Model == "" {
		cfg.LLM.Model = "claude-sonnet-4-6"
	}

	for i := range cfg.Connectors {
		cfg.Connectors[i].Applications = expandCommaSeparated(cfg.Connectors[i].Applications)
		cfg.Connectors[i].Monitor = expandCommaSeparated(cfg.Connectors[i].Monitor)
	}

	// Default output path for file-based outputs.
	for i := range cfg.Outputs {
		if cfg.Outputs[i].Path == "" {
			cfg.Outputs[i].Path = "./reports"
		}
	}

	return &cfg, nil
}

// Validate checks that the config is semantically valid after all overrides are applied.
// It is a separate method (not called from Load) so CLI overrides can happen first.
func (c *Config) Validate() error {
	if _, err := time.ParseDuration(c.TimeWindow); err != nil {
		return fmt.Errorf("invalid time_window %q: %w", c.TimeWindow, err)
	}

	var enabledConnectors int
	for _, cc := range c.Connectors {
		if !cc.Enabled {
			continue
		}
		enabledConnectors++
		switch cc.Name {
		case "newrelic":
			if cc.APIKey == "" {
				return fmt.Errorf("connector newrelic: api_key is required")
			}
			if cc.AccountID == "" {
				return fmt.Errorf("connector newrelic: account_id is required")
			}
		case "hubspot":
			if cc.APIKey == "" {
				return fmt.Errorf("connector hubspot: api_key is required")
			}
		case "rollbar":
			if cc.APIKey == "" {
				return fmt.Errorf("connector rollbar: api_key is required")
			}
			if cc.AccountID == "" {
				return fmt.Errorf("connector rollbar: account_id is required (project slug)")
			}
		}
		// Unknown connector names are allowed (forward compatibility).
	}
	if enabledConnectors == 0 {
		return fmt.Errorf("at least one connector must be enabled")
	}

	var enabledOutputs int
	for _, oc := range c.Outputs {
		if !oc.Enabled {
			continue
		}
		enabledOutputs++
		if oc.Name == "s3" {
			if oc.S3.Bucket == "" {
				return fmt.Errorf("output s3: bucket is required")
			}
			if oc.S3.Region == "" {
				return fmt.Errorf("output s3: region is required")
			}
		}
		// Unknown output names are allowed (forward compatibility).
	}
	if enabledOutputs == 0 {
		return fmt.Errorf("at least one output must be enabled")
	}

	for _, ec := range c.Enrichers {
		if !ec.Enabled {
			continue
		}
		if ec.Name == "llm" {
			if c.LLM.APIKey == "" {
				return fmt.Errorf("enricher llm: api_key is required in llm config")
			}
			if c.LLM.Provider != "anthropic" && c.LLM.Provider != "openai" {
				return fmt.Errorf("enricher llm: unknown provider %q (expected \"anthropic\" or \"openai\")", c.LLM.Provider)
			}
		}
	}

	return nil
}

// OutputDir returns the output path from the first file-based output,
// or "./reports" as default.
func (c *Config) OutputDir() string {
	for _, o := range c.Outputs {
		if o.Enabled && o.Path != "" && (o.Name == "json" || o.Name == "html") {
			return o.Path
		}
	}
	return "./reports"
}

// expandCommaSeparated handles the case where an env var expands to a
// comma-separated string inside a YAML array field. YAML will parse it as
// a single-element slice like ["app1,app2,app3"], so we split on commas.
func expandCommaSeparated(vals []string) []string {
	var result []string
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if strings.Contains(v, ",") {
			for _, part := range strings.Split(v, ",") {
				part = strings.TrimSpace(part)
				if part != "" {
					result = append(result, part)
				}
			}
		} else {
			result = append(result, v)
		}
	}
	return result
}

func expandEnvVars(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		varName := strings.TrimSuffix(strings.TrimPrefix(match, "${"), "}")
		if val, ok := os.LookupEnv(varName); ok {
			return val
		}
		return match
	})
}
