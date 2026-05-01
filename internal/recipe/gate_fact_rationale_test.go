package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGateFactRationaleCompleteness_Run20C5 — pins the producer-side
// facts-integrity gate. With a committed yaml carrying
// run.envVariables / run.healthCheck / run.start, the gate refuses
// when any of those directives lack an attesting `field_rationale`
// fact on the codebase scope. With facts present (or bypassed via
// the `intentionally skipped:` Why marker), the gate passes.
func TestGateFactRationaleCompleteness_Run20C5(t *testing.T) {
	t.Parallel()

	yaml := `zerops:
  - setup: api
    build:
      base: nodejs@22
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      envVariables:
        NODE_ENV: production
      initCommands:
        - command: zsc execOnce migrate npm run migrate
      healthCheck:
        httpGet:
          port: 3000
          path: /healthz
      start: node dist/main.js
`

	setup := func(t *testing.T) (sourceRoot string, log *FactsLog) {
		t.Helper()
		dir := t.TempDir()
		sourceRoot = filepath.Join(dir, "apidev")
		if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(sourceRoot, "zerops.yaml"), []byte(yaml), 0o600); err != nil {
			t.Fatalf("write yaml: %v", err)
		}
		log = OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
		return sourceRoot, log
	}

	t.Run("refuses_when_directive_groups_lack_facts", func(t *testing.T) {
		t.Parallel()
		sourceRoot, log := setup(t)
		plan := &Plan{
			Slug:      "synth",
			Codebases: []Codebase{{Hostname: "api", SourceRoot: sourceRoot}},
		}
		violations := gateFactRationaleCompleteness(GateContext{Plan: plan, FactsLog: log})
		if len(violations) == 0 {
			t.Fatal("expected refusals for all uncovered directive groups; got none")
		}
		// Spot-check that the canonical groups present in the yaml ARE
		// reported. yaml has: build, run.envVariables, run.initCommands,
		// run.ports, run.healthCheck, run.start.
		msgs := make([]string, 0, len(violations))
		for _, v := range violations {
			msgs = append(msgs, v.Message)
		}
		joined := strings.Join(msgs, " | ")
		for _, want := range []string{"run.healthCheck", "run.envVariables", "run.start"} {
			if !strings.Contains(joined, want) {
				t.Errorf("violations missing group %q; got %q", want, joined)
			}
		}
	})

	t.Run("passes_with_attesting_facts", func(t *testing.T) {
		t.Parallel()
		sourceRoot, log := setup(t)
		plan := &Plan{
			Slug:      "synth",
			Codebases: []Codebase{{Hostname: "api", SourceRoot: sourceRoot}},
		}
		// Record one field_rationale per directive group present in
		// the yaml. The exact FieldPath shape is `run.envVariables.<KEY>`
		// or just `run.envVariables` — both attest the group.
		appends := []FactRecord{
			{Kind: FactKindFieldRationale, Topic: "build-base", Scope: "api/zerops.yaml", FieldPath: "build", Why: "nodejs@22 matches the runtime base."},
			{Kind: FactKindFieldRationale, Topic: "run-start", Scope: "api/zerops.yaml", FieldPath: "run.start", Why: "node dist/main.js launches the compiled API."},
			{Kind: FactKindFieldRationale, Topic: "run-ports", Scope: "api/zerops.yaml", FieldPath: "run.ports", Why: "Port 3000 is the framework default."},
			{Kind: FactKindFieldRationale, Topic: "run-env", Scope: "api/zerops.yaml", FieldPath: "run.envVariables.NODE_ENV", Why: "production-mode at start."},
			{Kind: FactKindFieldRationale, Topic: "run-init", Scope: "api/zerops.yaml", FieldPath: "run.initCommands", Why: "execOnce migrate idempotent across replicas."},
			{Kind: FactKindFieldRationale, Topic: "run-health", Scope: "api/zerops.yaml", FieldPath: "run.healthCheck", Why: "L7 balancer needs /healthz to flip ready."},
		}
		for _, f := range appends {
			if err := log.Append(f); err != nil {
				t.Fatalf("append fact: %v", err)
			}
		}
		violations := gateFactRationaleCompleteness(GateContext{Plan: plan, FactsLog: log})
		if len(violations) != 0 {
			t.Errorf("expected no refusals when every group is attested; got %d:", len(violations))
			for _, v := range violations {
				t.Errorf("  - %s: %s", v.Code, v.Message)
			}
		}
	})

	t.Run("bypass_via_intentionally_skipped_why", func(t *testing.T) {
		t.Parallel()
		sourceRoot, log := setup(t)
		plan := &Plan{
			Slug:      "synth",
			Codebases: []Codebase{{Hostname: "api", SourceRoot: sourceRoot}},
		}
		// Attest most groups, bypass run.healthCheck explicitly.
		appends := []FactRecord{
			{Kind: FactKindFieldRationale, Topic: "build-base", Scope: "api/zerops.yaml", FieldPath: "build", Why: "matches runtime."},
			{Kind: FactKindFieldRationale, Topic: "run-start", Scope: "api/zerops.yaml", FieldPath: "run.start", Why: "compiled entry."},
			{Kind: FactKindFieldRationale, Topic: "run-ports", Scope: "api/zerops.yaml", FieldPath: "run.ports", Why: "framework default."},
			{Kind: FactKindFieldRationale, Topic: "run-env", Scope: "api/zerops.yaml", FieldPath: "run.envVariables", Why: "production-mode."},
			{Kind: FactKindFieldRationale, Topic: "run-init", Scope: "api/zerops.yaml", FieldPath: "run.initCommands", Why: "migrate."},
			{Kind: FactKindFieldRationale, Topic: "run-health-skip", Scope: "api/zerops.yaml", FieldPath: "run.healthCheck", Why: "intentionally skipped: caller already documents readinessCheck behavior verbatim."},
		}
		for _, f := range appends {
			if err := log.Append(f); err != nil {
				t.Fatalf("append fact: %v", err)
			}
		}
		violations := gateFactRationaleCompleteness(GateContext{Plan: plan, FactsLog: log})
		if len(violations) != 0 {
			t.Errorf("expected no refusals after bypass; got %d:", len(violations))
			for _, v := range violations {
				t.Errorf("  - %s: %s", v.Code, v.Message)
			}
		}
	})

	t.Run("missing_yaml_skips_silently", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
		plan := &Plan{
			Slug:      "synth",
			Codebases: []Codebase{{Hostname: "api", SourceRoot: dir}},
		}
		violations := gateFactRationaleCompleteness(GateContext{Plan: plan, FactsLog: log})
		if len(violations) != 0 {
			t.Errorf("missing yaml should not violate; got %d", len(violations))
		}
	})

	t.Run("nil_facts_log_is_no_op", func(t *testing.T) {
		t.Parallel()
		sourceRoot, _ := setup(t)
		plan := &Plan{
			Slug:      "synth",
			Codebases: []Codebase{{Hostname: "api", SourceRoot: sourceRoot}},
		}
		violations := gateFactRationaleCompleteness(GateContext{Plan: plan, FactsLog: nil})
		if len(violations) != 0 {
			t.Errorf("nil FactsLog should be a no-op; got %d violations", len(violations))
		}
	})
}

// TestDirectiveGroupsPresent_Run20C5 — table test of yaml-AST directive
// enumeration. Confirms `run.envVariables` lands when the yaml has a
// `run.envVariables.<KEY>` map; absent groups don't false-positive.
func TestDirectiveGroupsPresent_Run20C5(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want []string // sorted-stable subset of directiveGroupsForGate
	}{
		{
			name: "minimal — just build + run.start",
			body: `zerops:
  - setup: api
    build:
      base: nodejs@22
    run:
      base: nodejs@22
      start: node main.js
`,
			want: []string{"build", "run.start"},
		},
		{
			name: "rich — build, run.envVariables, run.initCommands, run.ports, run.healthCheck, run.start",
			body: `zerops:
  - setup: api
    build:
      base: nodejs@22
    run:
      base: nodejs@22
      ports:
        - port: 3000
      envVariables:
        NODE_ENV: production
      initCommands:
        - command: migrate
      healthCheck:
        httpGet: {port: 3000, path: /healthz}
      start: node main.js
`,
			want: []string{"build", "run.start", "run.ports", "run.envVariables", "run.initCommands", "run.healthCheck"},
		},
		{
			name: "empty yaml — no groups",
			body: `zerops:
  - setup: api
`,
			want: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := directiveGroupsPresent([]byte(tc.body))
			gotSet := map[string]bool{}
			for _, g := range got {
				gotSet[g] = true
			}
			for _, w := range tc.want {
				if !gotSet[w] {
					t.Errorf("missing expected group %q in %v", w, got)
				}
			}
			if len(tc.want) == 0 && len(got) != 0 {
				t.Errorf("expected no groups; got %v", got)
			}
		})
	}
}
