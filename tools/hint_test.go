package tools

import (
	"os"
	"testing"
	"time"
)

var eeFeatures = []string{"ee/feature-a"}

func TestGetHint_EmptyFeatures(t *testing.T) {
	cleanUp(t)
	noCache()

	for i := 0; i < 5; i++ {
		if got := GetHint("s1", nil); got != "" {
			t.Errorf("expected empty hint for nil eeFeatures, got %q", got)
		}
	}
}

func TestGetHint_BelowThreshold(t *testing.T) {
	cleanUp(t)
	noCache()

	for i := 0; i < eeInteractionThreshold-1; i++ {
		if got := GetHint("s1", eeFeatures); got != "" {
			t.Errorf("call %d: expected empty hint below threshold, got %q", i+1, got)
		}
	}
}

func TestGetHint_AtThreshold_CooldownPassed(t *testing.T) {
	cleanUp(t)
	noCache()

	for i := 0; i < eeInteractionThreshold-1; i++ {
		GetHint("s1", eeFeatures)
	}
	got := GetHint("s1", eeFeatures)
	if got == "" {
		t.Error("expected hint at threshold with no prior hint, got empty string")
	}
}

func TestGetHint_AtThreshold_CooldownNotPassed(t *testing.T) {
	cleanUp(t)
	mockCache(time.Now().Add(-hintCooldown / 2))

	for i := 0; i < eeInteractionThreshold; i++ {
		GetHint("s1", eeFeatures)
	}
	got := GetHint("s1", eeFeatures)
	if got != "" {
		t.Errorf("expected no hint when cooldown not passed, got %q", got)
	}
}

func TestGetHint_CountResetsAfterHint(t *testing.T) {
	cleanUp(t)
	noCache()

	for i := 0; i < eeInteractionThreshold; i++ {
		GetHint("s1", eeFeatures)
	}

	for i := 0; i < eeInteractionThreshold-1; i++ {
		if got := GetHint("s1", eeFeatures); got != "" {
			t.Errorf("call %d after reset: expected empty hint, got %q", i+1, got)
		}
	}
	if got := GetHint("s1", eeFeatures); got == "" {
		t.Error("expected hint after counter reset and new threshold reached")
	}
}

func TestGetHint_IndependentSessions(t *testing.T) {
	cleanUp(t)
	noCache()

	for i := 0; i < eeInteractionThreshold; i++ {
		GetHint("s1", eeFeatures)
	}

	if got := GetHint("s2", eeFeatures); got != "" {
		t.Errorf("s2 should not get hint on first call, got %q", got)
	}
}

func TestGetHint_EEDetectedSuppression(t *testing.T) {
	cleanUp(t)
	noCache()

	sessionRegistry.MarkAsEnterprise("s1")

	for i := 0; i < eeInteractionThreshold+5; i++ {
		if got := GetHint("s1", eeFeatures); got != "" {
			t.Errorf("EE-detected session should never get a hint, got %q on call %d", got, i+1)
		}
	}
}

func TestGetHint_EEDetectedFlagSetMidSession(t *testing.T) {
	cleanUp(t)
	noCache()

	GetHint("s1", eeFeatures)
	GetHint("s1", eeFeatures)

	sessionRegistry.MarkAsEnterprise("s1")

	for i := 0; i < 5; i++ {
		if got := GetHint("s1", eeFeatures); got != "" {
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
