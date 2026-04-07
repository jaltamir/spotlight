package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	TimeWindow string            `yaml:"time_window"`
	Connectors []ConnectorConfig `yaml:"connectors"`
	Outputs    []OutputConfig    `yaml:"outputs"`
	LLM        LLMConfig         `yaml:"llm"`
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
	Enabled  bool   `yaml:"enabled"`
	Provider string `yaml:"provider"`
	APIKey   string `yaml:"api_key"`
	Model    string `yaml:"model"`
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
