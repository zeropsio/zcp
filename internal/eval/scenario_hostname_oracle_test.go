package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// findRepoRootForHostnameTest walks up from the test's working directory
// until it finds go.mod — same technique as internal/content's
// findRepoRoot, duplicated locally since these tests parse real scenario
// files under eval/behavioral/scenarios, not package testdata fixtures.
func findRepoRootForHostnameTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for range 8 {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("repo root (go.mod) not found")
	return ""
}

// TestScenario_ApiNodePostgresClassicDev_ExpectsDevSuffixedHostname pins the
// oracle to the hostname ZCP's own Discover convention actually produces for
// a dev-only bootstrap: mode is derived from the hostname pattern, and
// dev-only ⇒ "<name>dev" (docs/spec-workflows.md, Discover hostname
// patterns, ~line 585). The scenario's persona says "if asked, api
// suffices" (the agent supplies the base name); the agent then applies the
// dev suffix itself, so the oracle must expect "apidev", not "api".
func TestScenario_ApiNodePostgresClassicDev_ExpectsDevSuffixedHostname(t *testing.T) {
	t.Parallel()

	repoRoot := findRepoRootForHostnameTest(t)
	path := filepath.Join(repoRoot, "eval", "behavioral", "scenarios", "api-node-postgres-classic-dev.md")
	sc, err := ParseScenario(path)
	if err != nil {
		t.Fatalf("ParseScenario(%q): %v", path, err)
	}

	found := false
	for _, exp := range sc.Verification.ExpectedServices {
		if exp.Type == "" || !strings.Contains(exp.Type, "nodejs") {
			continue
		}
		found = true
		if exp.Hostname != "apidev" {
			t.Errorf("expectedServices nodejs hostname: got %q, want %q", exp.Hostname, "apidev")
		}
	}
	if !found {
		t.Fatal("expectedServices: no nodejs entry found")
	}

	if sc.Verification.Liveness == nil {
		t.Fatal("verification.liveness: missing")
	}
	if sc.Verification.Liveness.Service != "apidev" {
		t.Errorf("liveness.service: got %q, want %q", sc.Verification.Liveness.Service, "apidev")
	}

	if sc.Verification.NodePostgresRecord == nil {
		t.Fatal("verification.nodePostgresRecord: missing")
	}
	if sc.Verification.NodePostgresRecord.Stage != "apidev" {
		t.Errorf("nodePostgresRecord.stage: got %q, want %q", sc.Verification.NodePostgresRecord.Stage, "apidev")
	}
}

// TestScenario_ClassicStaticNginxSimple_PromptPinsHostname pins the
// deterministic fix for the classic-static-nginx-simple scenario: the
// prompt never named a hostname, so a default-picking agent lands on ZCP's
// simple-mode default ("app") rather than the oracle's "web". The fix pins
// the hostname in the PROMPT (product unchanged) by appending the sentence
// `Call the service "web".`.
func TestScenario_ClassicStaticNginxSimple_PromptPinsHostname(t *testing.T) {
	t.Parallel()

	repoRoot := findRepoRootForHostnameTest(t)
	path := filepath.Join(repoRoot, "eval", "behavioral", "scenarios", "classic-static-nginx-simple.md")
	sc, err := ParseScenario(path)
	if err != nil {
		t.Fatalf("ParseScenario(%q): %v", path, err)
	}

	const want = `Call the service "web".`
	if !strings.Contains(sc.Prompt, want) {
		t.Errorf("prompt does not contain %q; got: %q", want, sc.Prompt)
	}
}
