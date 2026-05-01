package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteCodebaseYAML_WholeFragment_WritesVerbatim — Run-21-prep
// whole-yaml authoring contract. The codebase-content sub-agent records
// ONE fragment per codebase named `codebase/<host>/zerops-yaml` whose
// body is the entire commented zerops.yaml. Stitcher writes the body
// verbatim to `<SourceRoot>/zerops.yaml`, replacing the bare scaffold
// version. No per-block injection, no setup-scoped anchor matching, no
// comment stripping prior to write — the agent owns the whole document.
//
// This replaces the per-block fragment shape
// (`codebase/<h>/zerops-yaml-comments/<setup>.<path>.<leaf>`) that the
// run-21-prep audit found produces uneven coverage because the agent
// loses sight of the document as a whole.
func TestWriteCodebaseYAML_WholeFragment_WritesVerbatim(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	bare := `zerops:
  - setup: dev
    build:
      base: nodejs@22
      buildCommands:
        - npm install
      deployFiles: ./
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      start: zsc noop --silent
`
	yamlPath := filepath.Join(dir, "zerops.yaml")
	if err := os.WriteFile(yamlPath, []byte(bare), 0o600); err != nil {
		t.Fatalf("seed bare: %v", err)
	}
	commented := `zerops:
  - setup: dev
    build:
      base: nodejs@22
      buildCommands:
        - npm install
      # Whole-tree deploy ships source so SSHFS editing has files.
      deployFiles: ./
    run:
      base: nodejs@22
      # 0.0.0.0 binding, VXLAN routing rationale.
      ports:
        - port: 3000
          httpSupport: true
      # zsc noop keeps container alive for SSH workflow.
      start: zsc noop --silent
`
	plan := &Plan{
		Codebases: []Codebase{{Hostname: "api", SourceRoot: dir}},
		Fragments: map[string]string{
			"codebase/api/zerops-yaml": commented,
		},
	}

	if err := WriteCodebaseYAMLWithComments(plan, "api"); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != commented {
		t.Errorf("on-disk yaml != fragment body\n--- got\n%s\n--- want\n%s", got, commented)
	}
}

// TestWriteCodebaseYAML_NoFragment_LeavesBareYaml — when no whole-yaml
// fragment exists for the codebase (codebase-content phase hasn't run
// yet, or the agent hasn't authored it), the stitcher leaves the on-disk
// bare scaffold yaml untouched. Mirrors the pre-fragment scaffold-only
// state.
func TestWriteCodebaseYAML_NoFragment_LeavesBareYaml(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	bare := "zerops:\n  - setup: dev\n    run:\n      start: zsc noop --silent\n"
	yamlPath := filepath.Join(dir, "zerops.yaml")
	if err := os.WriteFile(yamlPath, []byte(bare), 0o600); err != nil {
		t.Fatal(err)
	}
	plan := &Plan{
		Codebases: []Codebase{{Hostname: "api", SourceRoot: dir}},
		Fragments: map[string]string{},
	}
	if err := WriteCodebaseYAMLWithComments(plan, "api"); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != bare {
		t.Errorf("on-disk yaml changed despite no fragment\n--- got\n%s\n--- want\n%s", got, bare)
	}
}

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

// TestWriteCodebaseYAMLWithComments_Idempotent — re-running stitch with
// the same fragment body produces byte-identical on-disk output. Run-21-
// prep contract: the agent owns the whole yaml; the stitcher just writes
// it. Idempotence here is `os.WriteFile(same body)`, but pin the property
// since refinement HOLDs that re-stitching after an unchanged fragment
// can't drift the on-disk yaml.
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
	commented := `zerops:
  - setup: prod
    run:
      base: nodejs@22
      # Cross-service refs aliased under stable own-keys.
      envVariables:
        DB_HOST: ${db_hostname}
`
	plan := &Plan{
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI, SourceRoot: srcRoot}},
		Fragments: map[string]string{
			"codebase/api/zerops-yaml": commented,
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
	if strings.Count(string(second), "# Cross-service refs") != 1 {
		t.Errorf("expected exactly 1 occurrence of comment, got %d:\n%s",
			strings.Count(string(second), "# Cross-service refs"), second)
	}
}
