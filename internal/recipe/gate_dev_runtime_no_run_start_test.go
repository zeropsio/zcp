package recipe

import (
	"os"
	"path/filepath"
	"testing"
)

// Run-52 Fix 4 — scaffold lint: a dynamic-class dev runtime must omit
// run.start. The dynamic dev convention (run-49 issue 3) is SSHFS-mount
// + omitted-run.start + zerops_dev_server-owned process; a stale
// `start:` on a dynamic dev block (e.g. the run-51 `zsc noop --silent`
// regression) silently breaks that contract and no other gate caught
// it at authoring time. Implicit-webserver / static / managed classes
// auto-serve and are exempt; prod blocks legitimately carry start and
// are never inspected.

func writeYAML(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	return dir
}

func TestGateDevRuntimeNoRunStart_FlagsZscNoopOnDynamicDev(t *testing.T) {
	t.Parallel()
	// The run-51 reproducer: dynamic dev runtime with a stale
	// `start: zsc noop --silent`.
	src := writeYAML(t, "zerops:\n  - setup: dev\n    run:\n      base: nodejs@22\n      start: zsc noop --silent\n")
	plan := &Plan{Codebases: []Codebase{{Hostname: "worker", SourceRoot: src, IsWorker: true}}}
	ctx := GateContext{Plan: plan}
	v := gateDevRuntimeNoRunStart(ctx)
	if len(v) != 1 {
		t.Fatalf("want 1 violation, got %d (%+v)", len(v), v)
	}
	if v[0].Code != "dev-runtime-run-start-present" {
		t.Errorf("want code dev-runtime-run-start-present, got %s", v[0].Code)
	}
	if v[0].Path != "worker" {
		t.Errorf("want path worker, got %s", v[0].Path)
	}
	if v[0].Severity != SeverityBlocking {
		t.Errorf("want severity blocking, got %v", v[0].Severity)
	}
}

func TestGateDevRuntimeNoRunStart_FlagsAnyRunStartOnDynamicDev(t *testing.T) {
	t.Parallel()
	// Any non-empty run.start on a dynamic dev block is flagged — the rule
	// is absolute, not specific to zsc noop.
	src := writeYAML(t, "zerops:\n  - setup: dev\n    run:\n      base: nodejs@22\n      start: npm run dev\n")
	plan := &Plan{Codebases: []Codebase{{Hostname: "api", SourceRoot: src}}}
	if v := gateDevRuntimeNoRunStart(GateContext{Plan: plan}); len(v) != 1 {
		t.Fatalf("want 1 violation for npm run dev on dynamic dev, got %d (%+v)", len(v), v)
	}
}

func TestGateDevRuntimeNoRunStart_PassesWhenDevOmitsRunStart(t *testing.T) {
	t.Parallel()
	// Canonical post-run-49 dynamic dev shape — no run.start. Passes.
	src := writeYAML(t, "zerops:\n  - setup: dev\n    run:\n      base: nodejs@22\n")
	plan := &Plan{Codebases: []Codebase{{Hostname: "api", SourceRoot: src}}}
	if v := gateDevRuntimeNoRunStart(GateContext{Plan: plan}); len(v) != 0 {
		t.Errorf("want 0 violations when dev omits run.start, got %d (%+v)", len(v), v)
	}
}

func TestGateDevRuntimeNoRunStart_SkipsProdStart(t *testing.T) {
	t.Parallel()
	// Dev omits start; prod carries the real app start. Only the dev block
	// is inspected — prod start is legitimate and never flagged.
	src := writeYAML(t, "zerops:\n  - setup: dev\n    run:\n      base: nodejs@22\n  - setup: prod\n    build:\n      base: nodejs@22\n    run:\n      base: nodejs@22\n      start: node dist/main.js\n")
	plan := &Plan{Codebases: []Codebase{{Hostname: "api", SourceRoot: src}}}
	if v := gateDevRuntimeNoRunStart(GateContext{Plan: plan}); len(v) != 0 {
		t.Errorf("want 0 violations for prod-only start, got %d (%+v)", len(v), v)
	}
}

func TestGateDevRuntimeNoRunStart_SkipsStaticAndImplicitWebserverDev(t *testing.T) {
	t.Parallel()
	// Static + implicit-webserver dev runtimes are non-dynamic; the gate
	// scope is dynamic-only, so a run.start on them (uncommon but legal) is
	// not flagged by this gate.
	cases := []struct {
		name string
		body string
	}{
		{"static", "zerops:\n  - setup: dev\n    run:\n      base: static\n      start: serve .\n"},
		{"php-nginx", "zerops:\n  - setup: dev\n    run:\n      base: php-nginx@8.4\n      start: something\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := writeYAML(t, tc.body)
			plan := &Plan{Codebases: []Codebase{{Hostname: "app", SourceRoot: src}}}
			if v := gateDevRuntimeNoRunStart(GateContext{Plan: plan}); len(v) != 0 {
				t.Errorf("%s: want 0 violations (non-dynamic class out of scope), got %d (%+v)", tc.name, len(v), v)
			}
		})
	}
}

func TestExtractDevRunStart_Table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
		want string
	}{
		{"dev-with-start", "zerops:\n  - setup: dev\n    run:\n      base: nodejs@22\n      start: zsc noop --silent\n", "zsc noop --silent"},
		{"dev-no-start", "zerops:\n  - setup: dev\n    run:\n      base: nodejs@22\n", ""},
		{"quoted-start", "zerops:\n  - setup: dev\n    run:\n      base: nodejs@22\n      start: \"npm run dev\"\n", "npm run dev"},
		{"prod-start-not-dev", "zerops:\n  - setup: dev\n    run:\n      base: nodejs@22\n  - setup: prod\n    run:\n      base: nodejs@22\n      start: node dist/main.js\n", ""},
		{"legacy-setup-name", "zerops:\n  - setup: appdev\n    run:\n      base: nodejs@22\n      start: zsc noop\n", "zsc noop"},
		{"only-prod", "zerops:\n  - setup: prod\n    run:\n      base: nodejs@22\n      start: node dist/main.js\n", ""},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := extractDevRunStart(tc.body); got != tc.want {
				t.Errorf("extractDevRunStart(%q) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}
