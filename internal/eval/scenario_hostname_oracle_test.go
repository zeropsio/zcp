package eval

import (
	"os"
	"path/filepath"
	"regexp"
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
// TestScenario_ApiNodePostgresClassicDev_PromptPinsHostname pins the live
// gate9 finding: with only the oracle pinned, the agent named the dev
// service `appdev` in one of four runs (`apidev` in the others), so the
// prompt must name it for the oracle to be deterministic.
func TestScenario_ApiNodePostgresClassicDev_PromptPinsHostname(t *testing.T) {
	t.Parallel()

	repoRoot := findRepoRootForHostnameTest(t)
	path := filepath.Join(repoRoot, "eval", "behavioral", "scenarios", "api-node-postgres-classic-dev.md")
	sc, err := ParseScenario(path)
	if err != nil {
		t.Fatalf("ParseScenario(%q): %v", path, err)
	}

	const want = "Službu s API pojmenuj `apidev`."
	if !strings.Contains(sc.Prompt, want) {
		t.Errorf("prompt does not contain %q; got: %q", want, sc.Prompt)
	}
}

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

// TestScenario_ClassicCodeRunnerSandbox_ExpectsABoxSandbox pins A12
// (docs/spec-scenarios.md §9.3) to a Zerops Box: the farm's run projects are
// created through the prg1 API with no location, so they are in prg1 — the one
// region a Box exists in (docs/spec-workflows.md §8 PS-16). The function
// oracle's snippet prints the hostname and kernel release it ran under; its
// match accepts a Box sandbox's answer (hostname = the sandbox id, no dots;
// kernel 6.18.53, zerops-docs zbox/images.mdx — both live-verified
// 2026-10-24) and refuses what the API's own container or a pool container
// would print (node-id-<n>.runtime.<service>.zerops on the host's
// 7.1.8-zabbly+ kernel, live 2026-10-24) — namespaces in the API's container
// cannot change either. The credential check greps the Box key's reference.
func TestScenario_ClassicCodeRunnerSandbox_ExpectsABoxSandbox(t *testing.T) {
	t.Parallel()

	repoRoot := findRepoRootForHostnameTest(t)
	path := filepath.Join(repoRoot, "eval", "behavioral", "scenarios", "classic-code-runner-sandbox.md")
	sc, err := ParseScenario(path)
	if err != nil {
		t.Fatalf("ParseScenario(%q): %v", path, err)
	}

	boxExpected := false
	for _, exp := range sc.Verification.ExpectedServices {
		if exp.Hostname == "sandbox" {
			boxExpected = exp.Type == "zbox*"
		}
	}
	if !boxExpected {
		t.Errorf("expectedServices must name `sandbox` with type zbox*: %+v", sc.Verification.ExpectedServices)
	}

	var runCheck, keyCheck *ContainerCheckEntry
	for i := range sc.Verification.ContainerCheck {
		c := &sc.Verification.ContainerCheck[i]
		switch {
		case strings.Contains(c.Cmd, "/run"):
			runCheck = c
		case strings.Contains(c.Cmd, "ZBOX_WORKSPACE_API_KEY"):
			keyCheck = c
		}
	}
	if runCheck == nil || keyCheck == nil {
		t.Fatalf("want a POST /run check and a Box-key reference check, got %+v", sc.Verification.ContainerCheck)
	}
	if !strings.Contains(runCheck.Cmd, "platform.release()") || !strings.Contains(runCheck.Cmd, "socket.gethostname()") {
		t.Errorf("the snippet must print the hostname and kernel release: %s", runCheck.Cmd)
	}
	if !strings.Contains(keyCheck.Cmd, "${sandbox_ZBOX_WORKSPACE_API_KEY}") {
		t.Errorf("the key check must grep the reference ${sandbox_ZBOX_WORKSPACE_API_KEY}: %s", keyCheck.Cmd)
	}
	re, err := regexp.Compile(runCheck.Match)
	if err != nil {
		t.Fatalf("match %q: %v", runCheck.Match, err)
	}
	for answer, want := range map[string]bool{
		`{"output":"j1us09qz 6.18.53\n"}`:                       true,
		`{"output": "iuo80c7f 6.18.53"}`:                        true,
		`{"output":"node-id-1.runtime.api.zerops 7.1.8-zabbly+\n"}`:     false,
		`{"output":"node-id-2.runtime.sandbox.zerops 7.1.8-zabbly+\n"}`: false,
		`{"output":"api 7.1.8-zabbly+\n"}`:                      false,
	} {
		if got := re.MatchString(answer); got != want {
			t.Errorf("match(%s) = %v, want %v", answer, got, want)
		}
	}
}
