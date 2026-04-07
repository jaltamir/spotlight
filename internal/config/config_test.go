package config

import (
	"os"
	"path/filepath"
	"testing"
)

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
outputs:
  - name: json
    enabled: true
    path: "./out"
  - name: html
    enabled: false
llm:
  enabled: true
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
