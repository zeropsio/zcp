package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestScanBareYAMLViolations_Run20C3 — pure-function table test of the
// `^\s+# ` scan. Carve-outs (shebang on line 1, trailing data-line
// comments) pass. Indented bare-comment lines fail with their 1-indexed
// line number reported.
func TestScanBareYAMLViolations_Run20C3(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		body      string
		wantLines []int
	}{
		{
			name: "clean bare yaml — no violations",
			body: `zerops:
  - setup: api
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
`,
			wantLines: nil,
		},
		{
			name: "shebang at line 1 is preserved (carve-out)",
			body: `#zeropsPreprocessor=on
zerops:
  - setup: api
    run:
      base: nodejs@22
`,
			wantLines: nil,
		},
		{
			name: "trailing comment on data line passes (no leading-whitespace `#`)",
			body: `zerops:
  - setup: api
    run:
      ports:
        - port: 3000  # framework default
          httpSupport: true
`,
			wantLines: nil,
		},
		{
			name: "indented causal comment above directive — refused",
			body: `zerops:
  - setup: api
    run:
      # NODE_ENV=production: framework needs production-mode at start.
      base: nodejs@22
`,
			wantLines: []int{4},
		},
		{
			name: "multiple indented comments — all reported",
			body: `zerops:
  - setup: api
    run:
      # API runs on Node 22 because the build container is also Node 22.
      base: nodejs@22
      ports:
        # Port 3000 is the framework default; httpSupport publishes it.
        - port: 3000
          httpSupport: true
`,
			wantLines: []int{4, 7},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vs := scanBareYAMLViolations(tc.body)
			gotLines := make([]int, len(vs))
			for i, v := range vs {
				gotLines[i] = v.line
			}
			if len(gotLines) != len(tc.wantLines) {
				t.Fatalf("violation count: got %d, want %d (lines=%v)",
					len(gotLines), len(tc.wantLines), gotLines)
			}
			for i, want := range tc.wantLines {
				if gotLines[i] != want {
					t.Errorf("violation %d: got line %d, want %d", i, gotLines[i], want)
				}
			}
		})
	}
}

// TestGateScaffoldBareYAML_Run20C3 — gate-level integration test. With
// a committed zerops.yaml carrying scaffold-leaked indented `#` lines,
// the gate refuses with the exact violating line numbers named in the
// message. Clean bare yaml passes.
func TestGateScaffoldBareYAML_Run20C3(t *testing.T) {
	t.Parallel()

	t.Run("refuses_with_line_numbers_named", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		sourceRoot := filepath.Join(dir, "apidev")
		if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		yaml := `zerops:
  - setup: api
    run:
      # Causal comment that scaffold should not have written.
      base: nodejs@22
`
		if err := os.WriteFile(filepath.Join(sourceRoot, "zerops.yaml"), []byte(yaml), 0o600); err != nil {
			t.Fatalf("write yaml: %v", err)
		}
		plan := &Plan{
			Slug:      "synth",
			Codebases: []Codebase{{Hostname: "api", SourceRoot: sourceRoot}},
		}
		violations := gateScaffoldBareYAML(GateContext{Plan: plan})
		if len(violations) != 1 {
			t.Fatalf("violation count: got %d, want 1", len(violations))
		}
		v := violations[0]
		if v.Code != "scaffold-yaml-leaked-comment" {
			t.Errorf("code: got %q, want scaffold-yaml-leaked-comment", v.Code)
		}
		if !strings.Contains(v.Message, "L4:") {
			t.Errorf("message must name violating line number; got %q", v.Message)
		}
		if !strings.Contains(v.Message, "bare-yaml-prohibition.md") {
			t.Errorf("message must cite the principle; got %q", v.Message)
		}
		if !strings.Contains(v.Message, "codebase/api/zerops-yaml") {
			t.Errorf("message must redirect to fragment-recording path; got %q", v.Message)
		}
	})

	t.Run("clean_yaml_passes", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		sourceRoot := filepath.Join(dir, "apidev")
		if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		yaml := `#zeropsPreprocessor=on
zerops:
  - setup: api
    run:
      base: nodejs@22
      ports:
        - port: 3000  # trailing data-line comment
          httpSupport: true
`
		if err := os.WriteFile(filepath.Join(sourceRoot, "zerops.yaml"), []byte(yaml), 0o600); err != nil {
			t.Fatalf("write yaml: %v", err)
		}
		plan := &Plan{
			Slug:      "synth",
			Codebases: []Codebase{{Hostname: "api", SourceRoot: sourceRoot}},
		}
		violations := gateScaffoldBareYAML(GateContext{Plan: plan})
		if len(violations) != 0 {
			t.Errorf("expected no violations on clean yaml; got %d: %v",
				len(violations), violations)
		}
	})

	t.Run("missing_yaml_skips_silently", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		plan := &Plan{
			Slug:      "synth",
			Codebases: []Codebase{{Hostname: "api", SourceRoot: dir}},
		}
		violations := gateScaffoldBareYAML(GateContext{Plan: plan})
		if len(violations) != 0 {
			t.Errorf("missing yaml should not violate; got %d: %v",
				len(violations), violations)
		}
	})
}

// TestScaffoldBrief_Run20C3_LoadsBareYamlPrinciple — pins that the
// scaffold brief composer pulls in `principles/bare-yaml-prohibition.md`
// so the agent reads the rule before authoring scaffold yaml.
func TestScaffoldBrief_Run20C3_LoadsBareYamlPrinciple(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	cb := plan.Codebases[0]
	brief, err := BuildScaffoldBrief(plan, cb, nil)
	if err != nil {
		t.Fatalf("BuildScaffoldBrief: %v", err)
	}
	if !strings.Contains(brief.Body, "Bare zerops.yaml during scaffold") {
		t.Errorf("scaffold brief missing bare-yaml-prohibition principle heading")
	}
	if !strings.Contains(brief.Body, "without inline causal comments") {
		t.Errorf("scaffold brief missing the bare-yaml rule")
	}
}
