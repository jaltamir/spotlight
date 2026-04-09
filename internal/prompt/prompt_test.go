package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadExplicitPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom.md")
	os.WriteFile(path, []byte("custom prompt"), 0o644)

	text, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "custom prompt" {
		t.Errorf("expected 'custom prompt', got %q", text)
	}
}

func TestLoadExplicitPathNotFound(t *testing.T) {
	_, err := Load("/nonexistent/prompt.md")
	if err == nil {
		t.Fatal("expected error for missing explicit path")
	}
}

func TestLoadCustomFile(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("spotlight-prompt.md", []byte("custom file"), 0o644)

	text, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "custom file" {
		t.Errorf("expected 'custom file', got %q", text)
	}
}

func TestLoadDistFile(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("spotlight-prompt.dist.md", []byte("dist prompt"), 0o644)

	text, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "dist prompt" {
		t.Errorf("expected 'dist prompt', got %q", text)
	}
}

func TestLoadCustomTakesPrecedenceOverDist(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("spotlight-prompt.md", []byte("custom wins"), 0o644)
	os.WriteFile("spotlight-prompt.dist.md", []byte("dist loses"), 0o644)

	text, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "custom wins" {
		t.Errorf("expected 'custom wins', got %q", text)
	}
}

func TestLoadFallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	text, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(text, "senior engineer") {
		t.Error("expected hardcoded default prompt")
	}
}
