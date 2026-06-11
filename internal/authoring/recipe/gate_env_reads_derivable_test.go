package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Run-40 B1 — gateEnvReadsDerivable refuses feature complete-phase
// when a codebase's zerops.yaml declares run.envVariables keys the
// source can't read. Pinned against run-39's S0-6 dead-env class.

// TestGateEnvReadsDerivable_RunS39_NatsQueueGroup pins the canonical
// dead-env case: workerdev/zerops.yaml declares NATS_QUEUE_GROUP but
// worker source hardcodes the literal `'workers'` (no
// process.env.NATS_QUEUE_GROUP read).
func TestGateEnvReadsDerivable_Run39_NatsQueueGroup(t *testing.T) {
	t.Parallel()
	dir := stageCodebaseWithYAMLAndSource(t,
		"worker",
		`zerops:
  - setup: worker
    run:
      envVariables:
        NATS_HOST: ${broker_hostname}
        NATS_QUEUE_GROUP: workers
`,
		map[string]string{
			"src/main.ts": `const host = process.env.NATS_HOST;
const sub = nc.subscribe(SUBJECT, { queue: 'workers' });
`,
		},
	)
	plan := &Plan{
		Codebases: []Codebase{
			{Hostname: "worker", SourceRoot: dir},
		},
		ObservedFacts: ObservedFacts{
			EnvReads: map[string][]string{
				"worker": {"NATS_HOST"}, // source reads NATS_HOST, NOT NATS_QUEUE_GROUP
			},
		},
	}

	v := gateEnvReadsDerivable(GateContext{Plan: plan})
	if len(v) != 1 {
		t.Fatalf("expected 1 dead-env violation; got %d: %+v", len(v), v)
	}
	if v[0].Code != "env-declared-not-read" {
		t.Errorf("code = %q, want env-declared-not-read", v[0].Code)
	}
	if v[0].Severity != SeverityBlocking {
		t.Errorf("severity = %v, want Blocking", v[0].Severity)
	}
	if !strings.Contains(v[0].Message, "NATS_QUEUE_GROUP") {
		t.Errorf("message should name the dead env; got %q", v[0].Message)
	}
}

// TestGateEnvReadsDerivable_AllDeclaredKeysRead — happy path: every
// declared key has a matching source-grep entry.
func TestGateEnvReadsDerivable_AllDeclaredKeysRead(t *testing.T) {
	t.Parallel()
	dir := stageCodebaseWithYAMLAndSource(t,
		"api",
		`zerops:
  - setup: api
    run:
      envVariables:
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
`,
		map[string]string{
			"src/db.ts": `const host = process.env.DB_HOST; const port = process.env.DB_PORT;`,
		},
	)
	plan := &Plan{
		Codebases: []Codebase{{Hostname: "api", SourceRoot: dir}},
		ObservedFacts: ObservedFacts{
			EnvReads: map[string][]string{"api": {"DB_HOST", "DB_PORT"}},
		},
	}
	v := gateEnvReadsDerivable(GateContext{Plan: plan})
	if len(v) != 0 {
		t.Errorf("expected no violations on happy path; got %+v", v)
	}
}

// TestGateEnvReadsDerivable_MultipleDeadEnvs_Run39_SearchPair pins
// run-39's apidev case: SEARCH_PUBLIC_HOST and SEARCH_SEARCH_KEY are
// both declared but neither is read. Both must surface in one
// round-trip.
func TestGateEnvReadsDerivable_MultipleDeadEnvs_Run39_SearchPair(t *testing.T) {
	t.Parallel()
	dir := stageCodebaseWithYAMLAndSource(t,
		"api",
		`zerops:
  - setup: api
    run:
      envVariables:
        DB_HOST: ${db_hostname}
        SEARCH_PUBLIC_HOST: ${search_zeropsSubdomain}
        SEARCH_SEARCH_KEY: ${search_defaultSearchKey}
`,
		map[string]string{
			"src/main.ts": `const dbHost = process.env.DB_HOST;`,
		},
	)
	plan := &Plan{
		Codebases: []Codebase{{Hostname: "api", SourceRoot: dir}},
		ObservedFacts: ObservedFacts{
			EnvReads: map[string][]string{"api": {"DB_HOST"}},
		},
	}
	v := gateEnvReadsDerivable(GateContext{Plan: plan})
	if len(v) != 2 {
		t.Fatalf("expected 2 dead-env violations (SEARCH_PUBLIC_HOST + SEARCH_SEARCH_KEY); got %d: %+v", len(v), v)
	}
	seen := map[string]bool{}
	for _, x := range v {
		for _, want := range []string{"SEARCH_PUBLIC_HOST", "SEARCH_SEARCH_KEY"} {
			if strings.Contains(x.Message, want) {
				seen[want] = true
			}
		}
	}
	for _, want := range []string{"SEARCH_PUBLIC_HOST", "SEARCH_SEARCH_KEY"} {
		if !seen[want] {
			t.Errorf("missing violation for dead env %q", want)
		}
	}
}

// TestGateEnvReadsDerivable_ViteCarveOut — when a SPA codebase reads
// import.meta.env idiomatically, the gate exempts declared VITE_*
// keys whose specific reads may live in build-time config files the
// regex didn't scan. Conservative carve-out so the Vite-baked
// VITE_API_URL pattern (legitimate) doesn't false-positive while the
// non-VITE_ dead envs (NATS_QUEUE_GROUP) still fire.
func TestGateEnvReadsDerivable_ViteCarveOut(t *testing.T) {
	t.Parallel()
	dir := stageCodebaseWithYAMLAndSource(t,
		"app",
		`zerops:
  - setup: app
    build:
      envVariables:
        VITE_API_URL: ${API_URL}
    run:
      envVariables:
        VITE_API_URL: ${API_URL}
`,
		map[string]string{
			"src/api.ts": `const url = import.meta.env.VITE_OTHER_KEY;`,
		},
	)
	plan := &Plan{
		Codebases: []Codebase{{Hostname: "app", SourceRoot: dir}},
		ObservedFacts: ObservedFacts{
			EnvReads: map[string][]string{"app": {"VITE_OTHER_KEY"}}, // VITE_API_URL not in reads
		},
	}
	v := gateEnvReadsDerivable(GateContext{Plan: plan})
	for _, x := range v {
		if strings.Contains(x.Message, "VITE_API_URL") {
			t.Errorf("Vite carve-out should exempt declared VITE_* keys; got %+v", x)
		}
	}
}

// TestGateEnvReadsDerivable_NoEnvReadsEntry_Blocks — Run-40 fix-up #6.
// When the plan has declared envs but no ObservedFacts.EnvReads
// entry, the gate now BLOCKS (not Notice). Without this severity
// upgrade the no-entry branch is a silent bypass of the dead-env
// refusal: if populate skipped a codebase the gate would soft-notice
// instead of refusing, and dead-env declarations would ship.
func TestGateEnvReadsDerivable_NoEnvReadsEntry_Blocks(t *testing.T) {
	t.Parallel()
	dir := stageCodebaseWithYAMLAndSource(t,
		"worker",
		`zerops:
  - setup: worker
    run:
      envVariables:
        NATS_HOST: ${broker_hostname}
`,
		map[string]string{},
	)
	plan := &Plan{
		Codebases: []Codebase{{Hostname: "worker", SourceRoot: dir}},
		// ObservedFacts.EnvReads intentionally empty.
	}
	v := gateEnvReadsDerivable(GateContext{Plan: plan})
	if len(v) != 1 {
		t.Fatalf("expected 1 violation; got %d: %+v", len(v), v)
	}
	if v[0].Severity != SeverityBlocking {
		t.Errorf("severity should be Blocking when env-reads aren't populated (fix-up #6); got %v", v[0].Severity)
	}
	if v[0].Code != "env-reads-not-populated" {
		t.Errorf("code should be env-reads-not-populated; got %q", v[0].Code)
	}
}

// TestGateEnvReadsDerivable_NoDeclaredEnvs_NoViolation — a codebase
// whose yaml declares zero run.envVariables passes the gate silently.
func TestGateEnvReadsDerivable_NoDeclaredEnvs_NoViolation(t *testing.T) {
	t.Parallel()
	dir := stageCodebaseWithYAMLAndSource(t,
		"app",
		"zerops:\n  - setup: app\n    run:\n      base: static\n",
		map[string]string{},
	)
	plan := &Plan{
		Codebases: []Codebase{{Hostname: "app", SourceRoot: dir}},
	}
	v := gateEnvReadsDerivable(GateContext{Plan: plan})
	if len(v) != 0 {
		t.Errorf("expected no violations when no envs declared; got %+v", v)
	}
}

// TestFeatureGates_RegistersEnvReadsDerivable pins the gate wiring:
// feature complete-phase MUST include the env-reads-derivable check.
func TestFeatureGates_RegistersEnvReadsDerivable(t *testing.T) {
	t.Parallel()
	gates := FeatureGates()
	mustHaveGate(t, gates, "env-reads-derivable")
}

// stageCodebaseWithYAMLAndSource writes zerops.yaml and named source
// files into a temp dir; returns the temp dir for use as SourceRoot.
func stageCodebaseWithYAMLAndSource(t *testing.T, _ string, yamlBody string, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte(yamlBody), 0o600); err != nil {
		t.Fatalf("write zerops.yaml: %v", err)
	}
	for rel, body := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", full, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
	return dir
}
