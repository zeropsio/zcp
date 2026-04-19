package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
)

// writeYAMLAndScripts is a test helper: creates a tempdir with the given
// zerops.yaml content and the given relative scripts. Returns the tempdir
// absolute path, the parsed ZeropsYmlDoc, and the raw YAML text.
func writeYAMLAndScripts(t *testing.T, yamlText string, scripts map[string]string) (string, *ops.ZeropsYmlDoc, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte(yamlText), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	for rel, content := range scripts {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o755); err != nil {
			t.Fatalf("write script %s: %v", rel, err)
		}
	}
	doc, err := ops.ParseZeropsYml(dir)
	if err != nil {
		t.Fatalf("parse yaml: %v", err)
	}
	return dir, doc, yamlText
}

// findCheckByName returns a pointer to the first check with the given name,
// or nil when absent.
func findCheckByName(checks []workflowStepCheckShim, name string) *workflowStepCheckShim {
	for i := range checks {
		if checks[i].Name == name {
			return &checks[i]
		}
	}
	return nil
}

// workflowStepCheckShim mirrors workflow.StepCheck for test-local helpers.
// We convert to the shim to avoid leaking workflow-package field access
// into assertions — keeps the test file focused on the check's outputs.
type workflowStepCheckShim struct {
	Name   string
	Status string
	Detail string
}

// runScaffoldCheck invokes checkScaffoldArtifactLeak and converts its result
// to the test-local shim type.
func runScaffoldCheck(t *testing.T, dir string, doc *ops.ZeropsYmlDoc, rawYAML, hostname string) []workflowStepCheckShim {
	t.Helper()
	checks := checkScaffoldArtifactLeak(dir, doc, rawYAML, hostname)
	out := make([]workflowStepCheckShim, 0, len(checks))
	for _, c := range checks {
		out = append(out, workflowStepCheckShim{Name: c.Name, Status: c.Status, Detail: c.Detail})
	}
	return out
}

const cleanZeropsYAML = `zerops:
  - setup: dev
    build:
      base: nodejs@22
    run:
      base: nodejs@22
      start: zsc noop --silent
  - setup: prod
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
        - npm run build
      deployFiles: dist
    run:
      base: nodejs@22
      start: node dist/main.js
`

// TestScaffoldArtifactLeak_v29_PreshipLeak reproduces the v29 defect: an
// apidev/scripts/preship.sh shipped in the published tree because no check
// enforced the "no committed scaffold scripts" rule. The plant here mimics
// v29's file — 2.8 KB of pre-ship assertions — and the clean zerops.yaml has
// no reference to it.
func TestScaffoldArtifactLeak_v29_PreshipLeak(t *testing.T) {
	t.Parallel()
	dir, doc, raw := writeYAMLAndScripts(t, cleanZeropsYAML, map[string]string{
		"scripts/preship.sh": "#!/bin/bash\nset -e\nfail() { echo \"$1\"; exit 1; }\n# ... assertions ...\n",
	})
	got := runScaffoldCheck(t, dir, doc, raw, "apidev")
	check := findCheckByName(got, "apidev_scaffold_artifact_leak")
	if check == nil {
		t.Fatal("expected apidev_scaffold_artifact_leak check")
	}
	if check.Status != "fail" {
		t.Errorf("expected fail status, got %q (detail: %s)", check.Status, check.Detail)
	}
	if !strings.Contains(check.Detail, "scripts/preship.sh") {
		t.Errorf("expected detail to name scripts/preship.sh, got: %s", check.Detail)
	}
	// Detail must include the inline git-identity remediation hint so the
	// agent can amend the scaffold commit even on a container with no
	// git config.
	if !strings.Contains(check.Detail, "user.email=scaffold@zcp.local") {
		t.Errorf("expected remediation to include inline git identity override, got: %s", check.Detail)
	}
}

// TestScaffoldArtifactLeak_ReferencedScript_Passes: a committed script is
// allowed when zerops.yaml actually references it — this is legitimate
// platform code, not a scaffold-phase leak.
func TestScaffoldArtifactLeak_ReferencedScript_Passes(t *testing.T) {
	t.Parallel()
	yamlText := `zerops:
  - setup: dev
    build:
      base: nodejs@22
    run:
      base: nodejs@22
      start: ./scripts/healthcheck.sh
  - setup: prod
    build:
      base: nodejs@22
      buildCommands: ./scripts/healthcheck.sh
      deployFiles: dist
    run:
      base: nodejs@22
      start: ./scripts/healthcheck.sh
`
	dir, doc, raw := writeYAMLAndScripts(t, yamlText, map[string]string{
		"scripts/healthcheck.sh": "#!/bin/bash\ncurl -fs http://localhost:3000/health || exit 1\n",
	})
	got := runScaffoldCheck(t, dir, doc, raw, "api")
	check := findCheckByName(got, "api_scaffold_artifact_leak")
	if check == nil {
		t.Fatal("expected api_scaffold_artifact_leak check")
	}
	if check.Status != "pass" {
		t.Errorf("expected pass, got %q (detail: %s)", check.Status, check.Detail)
	}
}

// TestScaffoldArtifactLeak_EmptyTree_Passes: a codebase with no scripts/
// directory at all must not produce a leak fail (and should cleanly emit a
// pass entry).
func TestScaffoldArtifactLeak_EmptyTree_Passes(t *testing.T) {
	t.Parallel()
	dir, doc, raw := writeYAMLAndScripts(t, cleanZeropsYAML, nil)
	got := runScaffoldCheck(t, dir, doc, raw, "api")
	check := findCheckByName(got, "api_scaffold_artifact_leak")
	if check == nil {
		t.Fatal("expected api_scaffold_artifact_leak check")
	}
	if check.Status != "pass" {
		t.Errorf("expected pass, got %q", check.Status)
	}
}

// TestScaffoldArtifactLeak_InitCommandsReference_Passes: initCommands isn't
// modeled in the local zeropsYmlRun struct. The raw-YAML substring fallback
// pass MUST recognise a script path referenced from `run.initCommands` and
// let the script through.
func TestScaffoldArtifactLeak_InitCommandsReference_Passes(t *testing.T) {
	t.Parallel()
	yamlText := `zerops:
  - setup: prod
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
      deployFiles: dist
    run:
      base: nodejs@22
      start: node dist/main.js
      initCommands:
        - ./scripts/migrate.sh
`
	dir, doc, raw := writeYAMLAndScripts(t, yamlText, map[string]string{
		"scripts/migrate.sh": "#!/bin/bash\necho migrating\n",
	})
	got := runScaffoldCheck(t, dir, doc, raw, "api")
	check := findCheckByName(got, "api_scaffold_artifact_leak")
	if check == nil {
		t.Fatal("expected api_scaffold_artifact_leak check")
	}
	if check.Status != "pass" {
		t.Errorf("expected pass, got %q (detail: %s)", check.Status, check.Detail)
	}
}

// TestScaffoldArtifactLeak_BuildCommandsListReference_Passes: a
// buildCommands list (rather than string) that names the script must also
// count as a legitimate reference.
func TestScaffoldArtifactLeak_BuildCommandsListReference_Passes(t *testing.T) {
	t.Parallel()
	yamlText := `zerops:
  - setup: prod
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
        - ./scripts/build-helper.sh
      deployFiles: dist
    run:
      base: nodejs@22
      start: node dist/main.js
`
	dir, doc, raw := writeYAMLAndScripts(t, yamlText, map[string]string{
		"scripts/build-helper.sh": "#!/bin/bash\n# real build helper referenced from zerops.yaml\n",
	})
	got := runScaffoldCheck(t, dir, doc, raw, "api")
	check := findCheckByName(got, "api_scaffold_artifact_leak")
	if check == nil {
		t.Fatal("expected api_scaffold_artifact_leak check")
	}
	if check.Status != "pass" {
		t.Errorf("expected pass, got %q (detail: %s)", check.Status, check.Detail)
	}
}

// TestScaffoldArtifactLeak_MultipleLeaks lists every unreferenced artifact
// in the detail so the agent can clean them up in one remediation pass.
func TestScaffoldArtifactLeak_MultipleLeaks(t *testing.T) {
	t.Parallel()
	dir, doc, raw := writeYAMLAndScripts(t, cleanZeropsYAML, map[string]string{
		"scripts/preship.sh":     "#!/bin/bash\n",
		"verify/readme_check.sh": "#!/bin/bash\n",
		"scaffold-audit.sh":      "#!/bin/bash\n",
	})
	got := runScaffoldCheck(t, dir, doc, raw, "api")
	check := findCheckByName(got, "api_scaffold_artifact_leak")
	if check == nil {
		t.Fatal("expected api_scaffold_artifact_leak check")
	}
	if check.Status != "fail" {
		t.Errorf("expected fail, got %q", check.Status)
	}
	for _, want := range []string{"scripts/preship.sh", "verify/readme_check.sh", "scaffold-audit.sh"} {
		if !strings.Contains(check.Detail, want) {
			t.Errorf("expected detail to mention %q, got: %s", want, check.Detail)
		}
	}
}
