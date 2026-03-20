package validation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeFakeKrakend writes a shell script to dir that acts as a fake krakend binary.
// When invoked with the "audit" subcommand it prints output to stdout and exits 1
// (non-zero with non-empty output is the success path in auditWithNativeKrakenD).
func makeFakeKrakend(t *testing.T, dir, output string) {
	t.Helper()
	script := "#!/bin/sh\necho '" + output + "'\nexit 1\n"
	path := filepath.Join(dir, "krakend")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake krakend: %v", err)
	}
}

// withFakeKrakend runs f with a fake krakend binary (that prints output) injected
// into PATH. It restores the original PATH after f returns.
func withFakeKrakend(t *testing.T, output string, f func()) {
	t.Helper()
	dir := t.TempDir()
	makeFakeKrakend(t, dir, output)

	origPath := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", origPath) })
	os.Setenv("PATH", dir+string(os.PathListSeparator)+origPath)

	f()
}

func TestAuditNativeKrakenD_ParsesSeverities(t *testing.T) {
	lines := []string{
		"[CRITICAL] TLS is disabled",
		"HIGH: no rate limiting configured",
		"MEDIUM: CORS policy too broad",
		"LOW: debug endpoint active",
		"informational line that should be ignored",
	}
	output := strings.Join(lines, "\n")

	withFakeKrakend(t, output, func() {
		result, err := auditWithNativeKrakenD(`{"version":3,"endpoints":[]}`, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Only the 4 non-info lines should produce issues.
		if len(result.Issues) != 4 {
			t.Fatalf("expected 4 issues, got %d: %v", len(result.Issues), result.Issues)
		}

		wantSeverities := []string{"critical", "high", "medium", "low"}
		for i, want := range wantSeverities {
			if result.Issues[i].Severity != want {
				t.Errorf("issue[%d].Severity = %q, want %q", i, result.Issues[i].Severity, want)
			}
		}
	})
}

func TestAuditNativeKrakenD_OnlyInfoLines_NoIssues(t *testing.T) {
	output := "Audit completed\nNo issues found\nAll checks passed"

	withFakeKrakend(t, output, func() {
		result, err := auditWithNativeKrakenD(`{"version":3,"endpoints":[]}`, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Issues) != 0 {
			t.Errorf("expected 0 issues for info-only output, got %d: %v", len(result.Issues), result.Issues)
		}
	})
}

func TestAuditNativeKrakenD_IssueFieldsPopulated(t *testing.T) {
	line := "HIGH: no authentication configured"
	withFakeKrakend(t, line, func() {
		result, err := auditWithNativeKrakenD(`{"version":3,"endpoints":[]}`, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Issues) != 1 {
			t.Fatalf("expected 1 issue, got %d", len(result.Issues))
		}
		issue := result.Issues[0]
		if issue.Category != "security" {
			t.Errorf("Category = %q, want %q", issue.Category, "security")
		}
		if issue.Title != line {
			t.Errorf("Title = %q, want %q", issue.Title, line)
		}
		if issue.Description != line {
			t.Errorf("Description = %q, want %q", issue.Description, line)
		}
		if issue.Remediation == "" {
			t.Error("Remediation must not be empty")
		}
	})
}

func TestAuditNativeKrakenD_ValidFlag_FalseWhenCriticalOrHigh(t *testing.T) {
	tests := []struct {
		name      string
		output    string
		wantValid bool
	}{
		{"critical present", "CRITICAL: TLS off", false},
		{"high present", "HIGH: no auth", false},
		{"only medium", "MEDIUM: rate limit", true},
		{"only low", "LOW: minor issue", true},
		{"no issues", "all good", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withFakeKrakend(t, tt.output, func() {
				result, err := auditWithNativeKrakenD(`{"version":3,"endpoints":[]}`, "")
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if result.Valid != tt.wantValid {
					t.Errorf("Valid = %v, want %v (output=%q)", result.Valid, tt.wantValid, tt.output)
				}
			})
		})
	}
}

func TestAuditNativeKrakenD_BlankLinesIgnored(t *testing.T) {
	output := "\n\nHIGH: finding one\n\nHIGH: finding two\n\n"

	withFakeKrakend(t, output, func() {
		result, err := auditWithNativeKrakenD(`{"version":3,"endpoints":[]}`, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Issues) != 2 {
			t.Errorf("expected 2 issues (blank lines must be ignored), got %d", len(result.Issues))
		}
	})
}

func TestAuditNativeKrakenD_CaseInsensitiveSeverity(t *testing.T) {
	// Keywords must be recognised regardless of case.
	output := "Critical issue\nhigh risk\nMedium concern\nlow priority"

	withFakeKrakend(t, output, func() {
		result, err := auditWithNativeKrakenD(`{"version":3,"endpoints":[]}`, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Issues) != 4 {
			t.Fatalf("expected 4 issues (case-insensitive), got %d: %v", len(result.Issues), result.Issues)
		}
		want := []string{"critical", "high", "medium", "low"}
		for i, w := range want {
			if result.Issues[i].Severity != w {
				t.Errorf("issue[%d].Severity = %q, want %q", i, result.Issues[i].Severity, w)
			}
		}
	})
}

func TestAuditNativeKrakenD_MethodAndSummarySet(t *testing.T) {
	withFakeKrakend(t, "all good", func() {
		result, err := auditWithNativeKrakenD(`{"version":3,"endpoints":[]}`, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Method != "native" {
			t.Errorf("Method = %q, want %q", result.Method, "native")
		}
		if result.Summary == "" {
			t.Error("Summary must not be empty")
		}
	})
}

func TestAuditWithBasicChecks_InvalidJSON(t *testing.T) {
	_, err := auditWithBasicChecks(`{not json`)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestAuditWithBasicChecks_MissingCORS(t *testing.T) {
	config := `{"version": 3, "endpoints": []}`
	result, err := auditWithBasicChecks(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Category == "cors" {
			found = true
			if issue.Severity != "medium" {
				t.Errorf("CORS issue severity = %q, want %q", issue.Severity, "medium")
			}
		}
	}
	if !found {
		t.Error("expected a cors issue when extra_config is absent")
	}
}

func TestAuditWithBasicChecks_DebugEndpointEnabled(t *testing.T) {
	config := `{"version": 3, "debug_endpoint": true, "endpoints": []}`
	result, err := auditWithBasicChecks(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Category == "exposure" {
			found = true
			if issue.Severity != "high" {
				t.Errorf("exposure issue severity = %q, want %q", issue.Severity, "high")
			}
		}
	}
	if !found {
		t.Error("expected an exposure issue when debug_endpoint=true")
	}
	if result.Valid {
		t.Error("expected Valid=false when debug_endpoint is enabled")
	}
}

func TestAuditWithBasicChecks_NoRateLimit(t *testing.T) {
	config := `{"version": 3, "endpoints": []}`
	result, err := auditWithBasicChecks(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Category == "rate-limiting" {
			found = true
		}
	}
	if !found {
		t.Error("expected a rate-limiting issue when no rate limit is configured")
	}
}

func TestAuditWithBasicChecks_NonGETEndpointWithoutAuth(t *testing.T) {
	config := `{
		"version": 3,
		"endpoints": [
			{"endpoint": "/data", "method": "POST", "backend": [{"url_pattern": "/"}]}
		]
	}`
	result, err := auditWithBasicChecks(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Category == "authentication" {
			found = true
			if issue.Severity != "high" {
				t.Errorf("authentication issue severity = %q, want %q", issue.Severity, "high")
			}
		}
	}
	if !found {
		t.Error("expected an authentication issue for POST endpoint without auth")
	}
}

func TestAuditWithBasicChecks_GETEndpointNoAuth_NoFalsePositive(t *testing.T) {
	config := `{
		"version": 3,
		"endpoints": [
			{"endpoint": "/health", "method": "GET", "backend": [{"url_pattern": "/"}]}
		]
	}`
	result, err := auditWithBasicChecks(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, issue := range result.Issues {
		if issue.Category == "authentication" {
			t.Errorf("unexpected authentication issue for GET endpoint: %+v", issue)
		}
	}
}

func TestAuditWithBasicChecks_ServiceLevelRateLimit_NoIssue(t *testing.T) {
	config := `{
		"version": 3,
		"extra_config": {"qos/ratelimit/service": {"max_rate": 100}},
		"endpoints": []
	}`
	result, err := auditWithBasicChecks(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, issue := range result.Issues {
		if issue.Category == "rate-limiting" {
			t.Errorf("unexpected rate-limiting issue when service-level rate limit configured: %+v", issue)
		}
	}
}

func TestAuditWithBasicChecks_EndpointLevelRateLimit_NoIssue(t *testing.T) {
	config := `{
		"version": 3,
		"endpoints": [{
			"endpoint": "/api",
			"method": "GET",
			"extra_config": {"qos/ratelimit/router": {"max_rate": 10}},
			"backend": [{"url_pattern": "/"}]
		}]
	}`
	result, err := auditWithBasicChecks(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, issue := range result.Issues {
		if issue.Category == "rate-limiting" {
			t.Errorf("unexpected rate-limiting issue when endpoint rate limit configured: %+v", issue)
		}
	}
}

func TestAuditWithBasicChecks_MethodAndSummary(t *testing.T) {
	config := `{"version": 3, "endpoints": []}`
	result, err := auditWithBasicChecks(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Method != "basic" {
		t.Errorf("Method = %q, want %q", result.Method, "basic")
	}
	if result.Summary == "" {
		t.Error("Summary must not be empty")
	}
}
