package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStripYAMLComments_RemovesIndentedHashLines pins the helper that
// makes WriteCodebaseYAMLWithComments idempotent. Stitch must be safe
// to re-run after a fragment edit without duplicating prior comments.
//
// Strips lines whose first non-whitespace character is `#` AND that sit
// above (or inline alongside) a yaml block. Preserves the
// `#zeropsPreprocessor=on` shebang at line 0 (no leading whitespace) so
// import.yaml semantics aren't broken. Preserves data lines unchanged.
func TestStripYAMLComments_RemovesIndentedHashLines(t *testing.T) {
	t.Parallel()

	in := `zerops:
  # This is a comment block.
  # Spans multiple lines.
  - setup: prod
    run:
      # Inline comment above key.
      base: nodejs@22
      ports:
        - port: 3000  # Trailing comment kept (not on its own line).
      start: zsc noop --silent
`
	want := `zerops:
  - setup: prod
    run:
      base: nodejs@22
      ports:
        - port: 3000  # Trailing comment kept (not on its own line).
      start: zsc noop --silent
`
	got := stripYAMLComments(in)
	if got != want {
		t.Errorf("stripYAMLComments mismatch:\n--- got\n%s\n--- want\n%s", got, want)
	}
}

// TestStripYAMLComments_PreservesShebang ensures the
// `#zeropsPreprocessor=on` line at file-start is NOT stripped — it's
// the import-yaml preprocessor activation pragma, not a causal comment.
func TestStripYAMLComments_PreservesShebang(t *testing.T) {
	t.Parallel()

	in := `#zeropsPreprocessor=on

# Project-level comment ABOVE the project key (gets stripped).
project:
  name: test
`
	want := `#zeropsPreprocessor=on

project:
  name: test
`
	got := stripYAMLComments(in)
	if got != want {
		t.Errorf("shebang must survive strip:\n--- got\n%s\n--- want\n%s", got, want)
	}
}

// TestWriteCodebaseYAMLWithComments_StripsThenInjects pins the canonical
// stitch path the run-19 prep added: bare `<SourceRoot>/zerops.yaml`
// (scaffold output) gets recorded comment fragments injected by
// `injectZeropsYamlComments`, and the result is written BACK to disk.
//
// Run-18 surfaced the gap: codebase-content recorded
// `zerops-yaml-comments/<block>` fragments correctly but the stitch
// path only injected them into IG #1's README inline yaml. The on-disk
// `<SourceRoot>/zerops.yaml` stayed bare for apidev + workerdev. This
// test pins that the new stitch step closes the gap.
func TestWriteCodebaseYAMLWithComments_StripsThenInjects(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	srcRoot := filepath.Join(dir, "apidev")
	if err := os.MkdirAll(srcRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Scaffold output: bare yaml (no causal comments).
	bare := `zerops:
  - setup: prod
    run:
      base: nodejs@22
      envVariables:
        DB_HOST: ${db_hostname}
        S3_REGION: us-east-1
      initCommands:
        - zsc execOnce ${appVersionId}-migrate
`
	yamlPath := filepath.Join(srcRoot, "zerops.yaml")
	if err := os.WriteFile(yamlPath, []byte(bare), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	plan := &Plan{
		Slug: "test",
		Codebases: []Codebase{
			{Hostname: "api", Role: RoleAPI, SourceRoot: srcRoot},
		},
		Fragments: map[string]string{
			"codebase/api/zerops-yaml-comments/run.envVariables": "Aliasing cross-service refs to own keys.",
			"codebase/api/zerops-yaml-comments/run.initCommands": "Two execOnce keys: per-deploy + first-time-only.",
		},
	}

	if err := WriteCodebaseYAMLWithComments(plan, "api"); err != nil {
		t.Fatalf("WriteCodebaseYAMLWithComments: %v", err)
	}

	got, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("read post-stitch: %v", err)
	}
	body := string(got)
	if !strings.Contains(body, "# Aliasing cross-service refs to own keys.") {
		t.Errorf("envVariables comment not injected:\n%s", body)
	}
	if !strings.Contains(body, "# Two execOnce keys: per-deploy + first-time-only.") {
		t.Errorf("initCommands comment not injected:\n%s", body)
	}
	if !strings.Contains(body, "envVariables:") {
		t.Errorf("yaml body lost envVariables key:\n%s", body)
	}
}

// TestWriteCodebaseYAMLWithComments_Idempotent — second run with
// identical fragments produces byte-identical output. Re-running stitch
// after a re-record must not duplicate prior comments. Idempotence is
// the load-bearing safety property: refinement HOLDs that edit a
// single fragment then re-stitch can't drift the on-disk yaml.
func TestWriteCodebaseYAMLWithComments_Idempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	srcRoot := filepath.Join(dir, "apidev")
	if err := os.MkdirAll(srcRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	bare := `zerops:
  - setup: prod
    run:
      base: nodejs@22
      envVariables:
        DB_HOST: ${db_hostname}
`
	yamlPath := filepath.Join(srcRoot, "zerops.yaml")
	if err := os.WriteFile(yamlPath, []byte(bare), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	plan := &Plan{
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI, SourceRoot: srcRoot}},
		Fragments: map[string]string{
			"codebase/api/zerops-yaml-comments/run.envVariables": "Alias rationale.",
		},
	}
	if err := WriteCodebaseYAMLWithComments(plan, "api"); err != nil {
		t.Fatalf("first stitch: %v", err)
	}
	first, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("read first: %v", err)
	}
	if err := WriteCodebaseYAMLWithComments(plan, "api"); err != nil {
		t.Fatalf("second stitch: %v", err)
	}
	second, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("read second: %v", err)
	}
	if string(first) != string(second) {
		t.Errorf("stitch is not idempotent:\n--- first\n%s\n--- second\n%s", first, second)
	}
	// Sanity: only one occurrence of the comment, not two.
	if strings.Count(string(second), "# Alias rationale.") != 1 {
		t.Errorf("expected exactly 1 occurrence of comment, got %d:\n%s",
			strings.Count(string(second), "# Alias rationale."), second)
	}
}
