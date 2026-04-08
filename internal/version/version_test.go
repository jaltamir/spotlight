package version

import (
	"strings"
	"testing"
)

func TestString(t *testing.T) {
	Version = "1.2.3"
	Commit = "abc1234"
	Date = "2026-04-05"

	s := String()
	if !strings.Contains(s, "1.2.3") {
		t.Errorf("String() should contain version, got: %s", s)
	}
	if !strings.Contains(s, "abc1234") {
		t.Errorf("String() should contain commit, got: %s", s)
	}
	if !strings.Contains(s, "2026-04-05") {
		t.Errorf("String() should contain date, got: %s", s)
	}
}

func TestUserAgent(t *testing.T) {
	Version = "1.2.3"
	ua := UserAgent()
	if !strings.HasPrefix(ua, "Spotlight/") {
		t.Errorf("UserAgent() should start with 'Spotlight/', got: %s", ua)
	}
	if !strings.Contains(ua, "1.2.3") {
		t.Errorf("UserAgent() should contain version, got: %s", ua)
	}
}
