package validation

import (
	"path/filepath"
	"strings"
	"testing"
)

func argsContainsPair(args []string, key, val string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == key && args[i+1] == val {
			return true
		}
	}
	return false
}

func argsContainsStr(args []string, s string) bool {
	for _, a := range args {
		if a == s {
			return true
		}
	}
	return false
}

func dockerArgsFrom(t *testing.T, env *ValidationEnvironment, command, configFile, image string) []string {
	t.Helper()
	cmd := buildDockerKrakenDCommand(env, command, configFile, image)
	if filepath.Base(cmd.Path) != "docker" {
		t.Fatalf("expected docker binary, got %s", cmd.Path)
	}
	return cmd.Args[1:] // strip argv[0]
}

func TestBuildDockerKrakenDCommandWithImage_NoFC(t *testing.T) {
	env := &ValidationEnvironment{FlexibleConfig: nil}
	args := dockerArgsFrom(t, env, "check", "/project/krakend.json", "krakend:2.0")

	if args[0] != "run" {
		t.Errorf("first arg must be 'run', got %q", args[0])
	}
	if !argsContainsPair(args, "-v", "/project:/etc/krakend") {
		t.Errorf("expected volume mount /project:/etc/krakend, args=%v", args)
	}
	if !argsContainsStr(args, "krakend:2.0") {
		t.Errorf("expected image krakend:2.0 in args, args=%v", args)
	}
	if !argsContainsStr(args, "/etc/krakend/krakend.json") {
		t.Errorf("expected -c /etc/krakend/krakend.json, args=%v", args)
	}
	// check command gets -l for lint
	if !argsContainsStr(args, "-l") {
		t.Errorf("expected -l for check command, args=%v", args)
	}
	// no FC_ env vars
	for _, a := range args {
		if strings.HasPrefix(a, "FC_") {
			t.Errorf("unexpected FC_ env var with no FC: %q", a)
		}
	}
}

func TestBuildDockerKrakenDCommandWithImage_FCNotDetected(t *testing.T) {
	env := &ValidationEnvironment{
		FlexibleConfig: &FlexibleConfigInfo{Detected: false, Type: "ce"},
	}
	args := dockerArgsFrom(t, env, "audit", "/project/krakend.json", "krakend:latest")

	// audit must not get -l
	if argsContainsStr(args, "-l") {
		t.Errorf("audit command must not include -l, args=%v", args)
	}
	for _, a := range args {
		if strings.HasPrefix(a, "FC_") {
			t.Errorf("unexpected FC_ env var when FC not detected: %q", a)
		}
	}
}

func TestBuildDockerKrakenDCommandWithImage_EEType(t *testing.T) {
	env := &ValidationEnvironment{
		FlexibleConfig: &FlexibleConfigInfo{
			Detected:    true,
			Type:        "ee",
			SettingsDir: "settings/",
		},
	}
	args := dockerArgsFrom(t, env, "check", "/project/krakend.tmpl", "krakend/krakend-ee:latest")

	if !argsContainsStr(args, "krakend/krakend-ee:latest") {
		t.Errorf("expected EE image in args, args=%v", args)
	}
	// EE type is treated like no-FC: no FC_ env vars
	for _, a := range args {
		if strings.HasPrefix(a, "FC_") {
			t.Errorf("unexpected FC_ env var for EE type: %q", a)
		}
	}
}

func TestBuildDockerKrakenDCommandWithImage_CEFC_WithSettingsDir(t *testing.T) {
	env := &ValidationEnvironment{
		FlexibleConfig: &FlexibleConfigInfo{
			Detected:    true,
			Type:        "ce",
			SettingsDir: "settings/",
		},
	}
	args := dockerArgsFrom(t, env, "check", "/project/krakend.tmpl", "krakend:latest")

	if !argsContainsPair(args, "-e", "FC_ENABLE=1") {
		t.Errorf("expected FC_ENABLE=1 when SettingsDir is set, args=%v", args)
	}
	if !argsContainsPair(args, "-e", "FC_SETTINGS=/etc/krakend/settings/") {
		t.Errorf("expected FC_SETTINGS=/etc/krakend/settings/, args=%v", args)
	}
	if !argsContainsPair(args, "-e", "FC_OUT=/etc/krakend/out.json") {
		t.Errorf("expected FC_OUT, args=%v", args)
	}
}

func TestBuildDockerKrakenDCommandWithImage_CEFC_WithAllDirs(t *testing.T) {
	env := &ValidationEnvironment{
		FlexibleConfig: &FlexibleConfigInfo{
			Detected:     true,
			Type:         "ce",
			SettingsDir:  "settings/",
			TemplatesDir: "templates/",
			PartialsDir:  "partials/",
		},
	}
	args := dockerArgsFrom(t, env, "audit", "/project/krakend.tmpl", "krakend:latest")

	if !argsContainsPair(args, "-e", "FC_ENABLE=1") {
		t.Errorf("expected FC_ENABLE=1, args=%v", args)
	}
	if !argsContainsPair(args, "-e", "FC_SETTINGS=/etc/krakend/settings/") {
		t.Errorf("expected FC_SETTINGS, args=%v", args)
	}
	if !argsContainsPair(args, "-e", "FC_TEMPLATES=/etc/krakend/templates/") {
		t.Errorf("expected FC_TEMPLATES, args=%v", args)
	}
	if !argsContainsPair(args, "-e", "FC_PARTIALS=/etc/krakend/partials/") {
		t.Errorf("expected FC_PARTIALS, args=%v", args)
	}
	if !argsContainsPair(args, "-e", "FC_OUT=/etc/krakend/out.json") {
		t.Errorf("expected FC_OUT, args=%v", args)
	}
}

func TestBuildDockerKrakenDCommandWithImage_CEFC_TemplatesOnly(t *testing.T) {
	env := &ValidationEnvironment{
		FlexibleConfig: &FlexibleConfigInfo{
			Detected:     true,
			Type:         "ce",
			SettingsDir:  "",
			TemplatesDir: "templates/",
			PartialsDir:  "",
		},
	}
	args := dockerArgsFrom(t, env, "audit", "/project/krakend.tmpl", "krakend:latest")

	if !argsContainsPair(args, "-e", "FC_TEMPLATES=/etc/krakend/templates/") {
		t.Errorf("expected FC_TEMPLATES, args=%v", args)
	}
	// no SettingsDir or PartialsDir — those entries must be absent
	for _, a := range args {
		if strings.HasPrefix(a, "FC_SETTINGS") || strings.HasPrefix(a, "FC_PARTIALS") {
			t.Errorf("unexpected env var %q when SettingsDir/PartialsDir are empty", a)
		}
	}
}

func TestBuildDockerKrakenDCommandWithImage_VolumeMountsParentDirectory(t *testing.T) {
	env := &ValidationEnvironment{FlexibleConfig: nil}
	args := dockerArgsFrom(t, env, "check", "/some/nested/dir/krakend.json", "krakend:latest")

	// Volume must mount the parent directory, not the file itself.
	if !argsContainsPair(args, "-v", "/some/nested/dir:/etc/krakend") {
		t.Errorf("expected parent-directory volume mount, args=%v", args)
	}
	// Config path inside container must use the base filename.
	if !argsContainsStr(args, "/etc/krakend/krakend.json") {
		t.Errorf("expected /etc/krakend/krakend.json in args, args=%v", args)
	}
}

func TestBuildDockerKrakenDCommandWithImage_ImagePassThrough(t *testing.T) {
	env := &ValidationEnvironment{FlexibleConfig: nil}
	image := "myregistry.example.com/krakend:3.0-custom"
	args := dockerArgsFrom(t, env, "check", "/project/krakend.json", image)

	if !argsContainsStr(args, image) {
		t.Errorf("expected custom image %q to be present in args, args=%v", image, args)
	}
}

// TestBuildDockerKrakenDCommandWithImage_CEFC_NoSettingsDir_FCEnableBug documents
// a known bug in the old code: FC_ENABLE=1 is missing when SettingsDir is empty.
// This test is expected to FAIL on the old code and PASS after the fix.
func TestBuildDockerKrakenDCommandWithImage_CEFC_NoSettingsDir_FCEnableBug(t *testing.T) {
	env := &ValidationEnvironment{
		FlexibleConfig: &FlexibleConfigInfo{
			Detected:    true,
			Type:        "ce",
			SettingsDir: "", // intentionally empty
		},
	}
	args := dockerArgsFrom(t, env, "audit", "/project/krakend.tmpl", "krakend:latest")

	if !argsContainsPair(args, "-e", "FC_ENABLE=1") {
		t.Errorf("FC_ENABLE=1 must be present for CE FC regardless of SettingsDir; args=%v", args)
	}
}
