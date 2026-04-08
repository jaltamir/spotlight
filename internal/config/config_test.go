package config

import (
	"os"
	"path/filepath"
	"testing"
)

// validConfig returns a Config that passes Validate().
func validConfig() *Config {
	return &Config{
		TimeWindow: "24h",
		Connectors: []ConnectorConfig{
			{Name: "newrelic", Enabled: true, APIKey: "key", AccountID: "123"},
		},
		Outputs: []OutputConfig{
			{Name: "json", Enabled: true, Path: "./reports"},
		},
	}
}

func TestValidateHappyPath(t *testing.T) {
	if err := validConfig().Validate(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateNoEnabledConnectors(t *testing.T) {
	cfg := validConfig()
	cfg.Connectors[0].Enabled = false
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for no enabled connectors")
	}
}

func TestValidateNoEnabledOutputs(t *testing.T) {
	cfg := validConfig()
	cfg.Outputs[0].Enabled = false
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for no enabled outputs")
	}
}

func TestValidateNewRelicMissingAPIKey(t *testing.T) {
	cfg := validConfig()
	cfg.Connectors[0].APIKey = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for newrelic missing api_key")
	}
}

func TestValidateNewRelicMissingAccountID(t *testing.T) {
	cfg := validConfig()
	cfg.Connectors[0].AccountID = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for newrelic missing account_id")
	}
}

func TestValidateHubSpotMissingAPIKey(t *testing.T) {
	cfg := validConfig()
	cfg.Connectors = []ConnectorConfig{
		{Name: "hubspot", Enabled: true, APIKey: ""},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for hubspot missing api_key")
	}
}

func TestValidateS3MissingFields(t *testing.T) {
	cfg := validConfig()
	cfg.Outputs = append(cfg.Outputs, OutputConfig{
		Name:    "s3",
		Enabled: true,
		S3:      S3Config{Bucket: "", Region: ""},
	})
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for s3 missing bucket/region")
	}
}

func TestValidateInvalidTimeWindow(t *testing.T) {
	cfg := validConfig()
	cfg.TimeWindow = "notaduration"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for invalid time_window")
	}
}

func TestValidateLLMEnricherWithoutKey(t *testing.T) {
	cfg := validConfig()
	cfg.Enrichers = []EnricherConfig{{Name: "llm", Enabled: true}}
	cfg.LLM = LLMConfig{Provider: "anthropic", APIKey: ""}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for llm enricher without api_key")
	}
}

func TestValidateLLMEnricherUnknownProvider(t *testing.T) {
	cfg := validConfig()
	cfg.Enrichers = []EnricherConfig{{Name: "llm", Enabled: true}}
	cfg.LLM = LLMConfig{Provider: "gemini", APIKey: "key"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestValidateLLMEnricherValid(t *testing.T) {
	cfg := validConfig()
	cfg.Enrichers = []EnricherConfig{{Name: "llm", Enabled: true}}
	cfg.LLM = LLMConfig{Provider: "openai", APIKey: "key", Model: "gpt-4o"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")

	content := `
time_window: "12h"
connectors:
  - name: newrelic
    enabled: true
    api_key: "test-key"
    account_id: "123"
    applications:
      - "svc-a"
  - name: hubspot
    enabled: false
    api_key: "hs-key"
enrichers:
  - name: llm
    enabled: true
outputs:
  - name: json
    enabled: true
    path: "./out"
  - name: html
    enabled: false
llm:
  api_key: "llm-key"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.TimeWindow != "12h" {
		t.Errorf("expected TimeWindow=12h, got %s", cfg.TimeWindow)
	}
	if len(cfg.Connectors) != 2 {
		t.Fatalf("expected 2 connectors, got %d", len(cfg.Connectors))
	}
	if cfg.Connectors[0].Name != "newrelic" || !cfg.Connectors[0].Enabled {
		t.Error("first connector should be enabled newrelic")
	}
	if cfg.Connectors[1].Enabled {
		t.Error("second connector should be disabled")
	}
	if cfg.LLM.Model != "claude-sonnet-4-6" {
		t.Errorf("expected default model claude-sonnet-4-6, got %s", cfg.LLM.Model)
	}
	if cfg.LLM.Provider != "anthropic" {
		t.Errorf("expected default provider=anthropic, got %s", cfg.LLM.Provider)
	}
	if len(cfg.Enrichers) != 1 || cfg.Enrichers[0].Name != "llm" || !cfg.Enrichers[0].Enabled {
		t.Errorf("expected 1 enabled llm enricher, got %v", cfg.Enrichers)
	}
}

func TestLoadConfigEnvExpansion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")

	t.Setenv("TEST_SPOTLIGHT_KEY", "expanded-value")
	content := `
connectors:
  - name: newrelic
    enabled: true
    api_key: "${TEST_SPOTLIGHT_KEY}"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Connectors[0].APIKey != "expanded-value" {
		t.Errorf("expected expanded-value, got %s", cfg.Connectors[0].APIKey)
	}
}

func TestLoadConfigCommaSeparatedEnvApps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")

	t.Setenv("NEWRELIC_APMS", "svc-a, svc-b, svc-c")
	content := `
connectors:
  - name: newrelic
    enabled: true
    api_key: "key"
    account_id: "123"
    applications:
      - "${NEWRELIC_APMS}"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	apps := cfg.Connectors[0].Applications
	if len(apps) != 3 {
		t.Fatalf("expected 3 applications, got %d: %v", len(apps), apps)
	}
	if apps[0] != "svc-a" || apps[1] != "svc-b" || apps[2] != "svc-c" {
		t.Errorf("unexpected applications: %v", apps)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")

	if err := os.WriteFile(path, []byte("connectors: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.TimeWindow != "24h" {
		t.Errorf("expected default TimeWindow=24h, got %s", cfg.TimeWindow)
	}
}
