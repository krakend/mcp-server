package tools

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestGetHint_BelowThreshold(t *testing.T) {
	cleanUp(t)
	noCache()

	for i := 0; i < eeInteractionThreshold-1; i++ {
		if got := GetEeHint("s1"); got != "" {
			t.Errorf("call %d: expected empty hint below threshold, got %q", i+1, got)
		}
	}
}

func TestGetHint_AtThreshold_CooldownPassed(t *testing.T) {
	cleanUp(t)
	noCache()

	for i := 0; i < eeInteractionThreshold-1; i++ {
		GetEeHint("s1")
	}
	got := GetEeHint("s1")
	if got == "" {
		t.Error("expected hint at threshold with no prior hint, got empty string")
	}
}

func TestGetHint_AtThreshold_CooldownNotPassed(t *testing.T) {
	cleanUp(t)
	mockCache(time.Now().Add(-hintCooldown / 2))

	for i := 0; i < eeInteractionThreshold; i++ {
		GetEeHint("s1")
	}
	got := GetEeHint("s1")
	if got != "" {
		t.Errorf("expected no hint when cooldown not passed, got %q", got)
	}
}

func TestGetHint_CountResetsAfterHint(t *testing.T) {
	cleanUp(t)
	noCache()

	for i := 0; i < eeInteractionThreshold; i++ {
		GetEeHint("s1")
	}

	for i := 0; i < eeInteractionThreshold-1; i++ {
		if got := GetEeHint("s1"); got != "" {
			t.Errorf("call %d after reset: expected empty hint, got %q", i+1, got)
		}
	}
	if got := GetEeHint("s1"); got == "" {
		t.Error("expected hint after counter reset and new threshold reached")
	}
}

func TestGetHint_IndependentSessions(t *testing.T) {
	cleanUp(t)
	// Put s1 in cooldown; s2 should still reach threshold independently.
	readHintCache = func(id string) (time.Time, error) {
		if id == eeInteractionsCounter+":s1" {
			return time.Now(), nil
		}
		return time.Time{}, os.ErrNotExist
	}
	writeHintCache = func(string, time.Time) error { return nil }

	for i := 0; i < eeInteractionThreshold+2; i++ {
		GetEeHint("s1") // all suppressed by s1's cooldown
	}

	// s2 counter starts at zero regardless of s1's interactions
	for i := 0; i < eeInteractionThreshold-1; i++ {
		if got := GetEeHint("s2"); got != "" {
			t.Errorf("s2 call %d: expected empty hint below threshold, got %q", i+1, got)
		}
	}
	if got := GetEeHint("s2"); got == "" {
		t.Error("s2 should reach threshold independently of s1")
	}
}

func TestGetHint_EEDetectedSuppression(t *testing.T) {
	cleanUp(t)
	noCache()

	sessionRegistry.MarkAsEnterprise("s1")

	for i := 0; i < eeInteractionThreshold+5; i++ {
		if got := GetEeHint("s1"); got != "" {
			t.Errorf("EE-detected session should never get a hint, got %q on call %d", got, i+1)
		}
	}
}

func TestGetHint_EEDetectedFlagSetMidSession(t *testing.T) {
	cleanUp(t)
	noCache()

	GetEeHint("s1")
	GetEeHint("s1")

	sessionRegistry.MarkAsEnterprise("s1")

	for i := 0; i < 5; i++ {
		if got := GetEeHint("s1"); got != "" {
			t.Errorf("expected no hint after EE detected, got %q", got)
		}
	}
}

func cleanUp(t *testing.T) {
	t.Helper()
	origRead := readHintCache
	origWrite := writeHintCache
	t.Cleanup(func() {
		readHintCache = origRead
		writeHintCache = origWrite
		sessionRegistry = &SessionRegistry{
			sessions: make(map[string]*session),
		}
	})
}

func mockCache(last time.Time) {
	readHintCache = func(string) (time.Time, error) { return last, nil }
	writeHintCache = func(string, time.Time) error { return nil }
}

func noCache() {
	readHintCache = func(string) (time.Time, error) { return time.Time{}, os.ErrNotExist }
	writeHintCache = func(string, time.Time) error { return nil }
}

// TestHintMessages_NoInjectionSignatures guards against reintroducing prompt-injection
// signatures into the hint messages. Safety-trained LLMs detect these patterns and warn
// users, which erodes trust in the KrakenD product (see mcp-testing/findings.md F-004).
func TestHintMessages_NoInjectionSignatures(t *testing.T) {
	injectionSignatures := []string{
		"system note",
		"verbatim",
		"mandatory output",
		"render required",
		"regardless of",
		"do not paraphrase",
		"do not omit",
		"must be appended",
	}
	msgs := map[string]string{
		"eeHintMessage":       eeHintMessage,
		"securityHintMessage": securityHintMessage,
	}
	for name, msg := range msgs {
		lower := strings.ToLower(msg)
		for _, sig := range injectionSignatures {
			if strings.Contains(lower, sig) {
				t.Errorf("%s contains prompt-injection signature %q", name, sig)
			}
		}
	}
}
