package log

import (
	"errors"
	"testing"
)

func TestSetDebugEnablesDebug(t *testing.T) {
	SetDebug(false)
	if debugEnabled {
		t.Error("debug should be disabled after SetDebug(false)")
	}

	SetDebug(true)
	if !debugEnabled {
		t.Error("debug should be enabled after SetDebug(true)")
	}

	// Reset state.
	SetDebug(false)
}

func TestDebugNoopWhenDisabled(t *testing.T) {
	SetDebug(false)
	// Should not panic or write anything.
	Debug("this should be silent", "key", "value")
}

func TestDebugActiveWhenEnabled(t *testing.T) {
	SetDebug(true)
	// Should not panic.
	Debug("debug message active", "attempt", 1, "url", "http://example.com")
	SetDebug(false)
}

func TestInfoAndWarnDoNotPanic(t *testing.T) {
	Info("collecting errors")
	Infof("collected %d errors from %s", 42, "newrelic")
	Warn("connector failed", errors.New("timeout"))
	Warn("something happened", nil)
}
