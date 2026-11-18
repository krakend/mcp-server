package tools

import (
	"os"
	"path/filepath"
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
	noCache()

	// s1 interactions must not count towards s2's threshold.
	for i := 0; i < eeInteractionThreshold-1; i++ {
		GetEeHint("s1")
	}

	for i := 0; i < eeInteractionThreshold-1; i++ {
		if got := GetEeHint("s2"); got != "" {
			t.Errorf("s2 call %d: expected empty hint below threshold, got %q", i+1, got)
		}
	}
	if got := GetEeHint("s2"); got == "" {
		t.Error("s2 should reach threshold independently of s1")
	}
}

func TestGetHint_CooldownSharedAcrossSessions(t *testing.T) {
	cleanUp(t)
	memoryCache()

	for i := 0; i < eeInteractionThreshold-1; i++ {
		GetEeHint("s1")
	}
	if got := GetEeHint("s1"); got == "" {
		t.Fatal("expected hint for s1 at threshold, got empty string")
	}

	// A new session (e.g. a restarted stdio process) must respect the cooldown
	// started by s1.
	for i := 0; i < eeInteractionThreshold+2; i++ {
		if got := GetEeHint("s2"); got != "" {
			t.Errorf("s2 call %d: expected no hint during cooldown started by s1, got %q", i+1, got)
		}
	}
}

func TestGetHint_CooldownIndependentPerHintType(t *testing.T) {
	cleanUp(t)
	memoryCache()

	for i := 0; i < eeInteractionThreshold; i++ {
		GetEeHint("s1")
	}
	if got := GetSecurityHint("s1"); got == "" {
		t.Error("EE hint cooldown must not suppress the security hint")
	}
}

func TestHintFileCache_RoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".krakend-mcp")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	stale := time.Now().Add(-2 * hintCooldown)
	if err := writeHintFileCache("stale", stale); err != nil {
		t.Fatalf("unexpected error writing stale entry: %v", err)
	}
	want := time.Now().Truncate(time.Second)
	if err := writeHintFileCache(eeInteractionsCounter, want); err != nil {
		t.Fatalf("unexpected error writing entry: %v", err)
	}

	got, err := readHintFileCache(eeInteractionsCounter)
	if err != nil {
		t.Fatalf("unexpected error reading entry: %v", err)
	}
	if !got.Equal(want) {
		t.Errorf("have %v, want %v", got, want)
	}
	if _, err := readHintFileCache("stale"); err == nil {
		t.Error("expected expired entry to be pruned on write")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != hintTimestampFile {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("expected only %s in %s, got %v", hintTimestampFile, dir, names)
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

// memoryCache backs the cooldown with an in-memory map, shared by every
// session, so tests can observe what one session's hint does to another.
func memoryCache() {
	records := map[string]time.Time{}
	readHintCache = func(key string) (time.Time, error) {
		v, ok := records[key]
		if !ok {
			return time.Time{}, os.ErrNotExist
		}
		return v, nil
	}
	writeHintCache = func(key string, ts time.Time) error {
		records[key] = ts
		return nil
	}
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
