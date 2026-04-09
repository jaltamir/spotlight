package prompt

import (
	"fmt"
	"os"
)

const defaultPrompt = `You are a senior engineer analyzing error patterns from multiple monitoring sources.

You will receive two data sections:
1. An aggregated report (JSON) with grouped errors, counts, trends, and sample messages.
2. Raw error records (JSON) with individual errors from all connectors.

Analyze the errors and produce a markdown report covering:

## Executive Summary
A 2-3 sentence overview of the error landscape.

## Critical Issues (by priority)
For each significant issue:
- **What**: Description of the error pattern
- **Where**: Which services/endpoints are affected
- **Impact**: Business impact assessment
- **Root Cause Hypothesis**: What might be causing this
- **Investigation Steps**: Concrete next steps

## Cross-Source Correlations
Identify errors that appear related across different sources.

## Recommendations
Prioritized list of actions to take.

Respond in markdown.`

// Load reads a prompt from the filesystem with a fallback chain:
//  1. explicitPath (if non-empty) — error if file does not exist
//  2. spotlight-prompt.md (custom, gitignored)
//  3. spotlight-prompt.dist.md (versionado)
//  4. hardcoded default
func Load(explicitPath string) (string, error) {
	if explicitPath != "" {
		data, err := os.ReadFile(explicitPath)
		if err != nil {
			return "", fmt.Errorf("reading prompt file %q: %w", explicitPath, err)
		}
		return string(data), nil
	}

	if data, err := os.ReadFile("spotlight-prompt.md"); err == nil {
		return string(data), nil
	}

	if data, err := os.ReadFile("spotlight-prompt.dist.md"); err == nil {
		return string(data), nil
	}

	return defaultPrompt, nil
}
