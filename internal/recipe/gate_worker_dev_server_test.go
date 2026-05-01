package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGateWorkerDevServerStarted_RefusesWhenAttestationMissing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	srcRoot := filepath.Join(root, "workerdev")
	if err := os.MkdirAll(srcRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	yamlBody := strings.Join([]string{
		"zerops:",
		"  - setup: dev",
		"    run:",
		"      base: nodejs@22",
		"      start: zsc noop --silent",
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(srcRoot, "zerops.yaml"), []byte(yamlBody), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	plan := &Plan{
		Codebases: []Codebase{
			{Hostname: "worker", SourceRoot: srcRoot, IsWorker: true},
		},
	}
	log := newMemFactsLog(t, root)
	ctx := GateContext{Plan: plan, OutputRoot: root, FactsLog: log}
	violations := gateWorkerDevServerStarted(ctx)
	if len(violations) != 1 {
		t.Fatalf("want 1 violation, got %d (%+v)", len(violations), violations)
	}
	if violations[0].Code != "worker-dev-server-not-started" {
		t.Errorf("want code worker-dev-server-not-started, got %s", violations[0].Code)
	}
	if violations[0].Path != "worker" {
		t.Errorf("want path worker, got %s", violations[0].Path)
	}
	if violations[0].Severity != SeverityBlocking {
		t.Errorf("want severity blocking, got %v", violations[0].Severity)
	}
}

func TestGateWorkerDevServerStarted_PassesWithAttestationFact(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	srcRoot := filepath.Join(root, "workerdev")
	if err := os.MkdirAll(srcRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	yamlBody := "zerops:\n  - setup: dev\n    run:\n      start: zsc noop --silent\n"
	if err := os.WriteFile(filepath.Join(srcRoot, "zerops.yaml"), []byte(yamlBody), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	plan := &Plan{
		Codebases: []Codebase{
			{Hostname: "worker", SourceRoot: srcRoot, IsWorker: true},
		},
	}
	log := newMemFactsLog(t, root)
	if err := log.Append(FactRecord{
		Topic:            "worker_dev_server_started",
		Kind:             FactKindPorterChange,
		Scope:            "worker/runtime",
		Why:              "Worker process owned by zerops_dev_server.",
		CandidateClass:   "scaffold-decision",
		CandidateSurface: "CODEBASE_ZEROPS_COMMENTS",
	}); err != nil {
		t.Fatalf("append fact: %v", err)
	}
	ctx := GateContext{Plan: plan, OutputRoot: root, FactsLog: log}
	if v := gateWorkerDevServerStarted(ctx); len(v) != 0 {
		t.Errorf("want 0 violations, got %d (%+v)", len(v), v)
	}
}

func TestGateWorkerDevServerStarted_PassesWithBypassFact(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	srcRoot := filepath.Join(root, "batchdev")
	if err := os.MkdirAll(srcRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	yamlBody := "zerops:\n  - setup: dev\n    run:\n      start: zsc noop --silent\n"
	if err := os.WriteFile(filepath.Join(srcRoot, "zerops.yaml"), []byte(yamlBody), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	plan := &Plan{
		Codebases: []Codebase{
			{Hostname: "batch", SourceRoot: srcRoot, IsWorker: true},
		},
	}
	log := newMemFactsLog(t, root)
	if err := log.Append(FactRecord{
		Topic:            "worker_no_dev_server",
		Kind:             FactKindPorterChange,
		Scope:            "batch/runtime",
		Why:              "One-shot batch codebase; no watcher loop needed.",
		CandidateClass:   "scaffold-decision",
		CandidateSurface: "CODEBASE_ZEROPS_COMMENTS",
	}); err != nil {
		t.Fatalf("append fact: %v", err)
	}
	ctx := GateContext{Plan: plan, OutputRoot: root, FactsLog: log}
	if v := gateWorkerDevServerStarted(ctx); len(v) != 0 {
		t.Errorf("want 0 violations, got %d (%+v)", len(v), v)
	}
}

func TestGateWorkerDevServerStarted_SkipsCodebasesWithCompiledStart(t *testing.T) {
	t.Parallel()
	// api codebase with compiled start (no zsc noop) — gate doesn't apply,
	// no attestation required, no violation.
	root := t.TempDir()
	srcRoot := filepath.Join(root, "apidev")
	if err := os.MkdirAll(srcRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	yamlBody := "zerops:\n  - setup: prod\n    run:\n      start: node dist/main.js\n"
	if err := os.WriteFile(filepath.Join(srcRoot, "zerops.yaml"), []byte(yamlBody), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	plan := &Plan{
		Codebases: []Codebase{
			{Hostname: "api", SourceRoot: srcRoot},
		},
	}
	ctx := GateContext{Plan: plan, OutputRoot: root, FactsLog: newMemFactsLog(t, root)}
	if v := gateWorkerDevServerStarted(ctx); len(v) != 0 {
		t.Errorf("want 0 violations for compiled-start codebase, got %d", len(v))
	}
}

func TestCodebaseHasNoopDevStart_DetectsCanonicalShape(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"canonical", "zerops:\n  - setup: dev\n    run:\n      start: zsc noop --silent\n", true},
		{"single-quoted", "run:\n  start: 'zsc noop --silent'\n", true},
		{"double-quoted", "run:\n  start: \"zsc noop --silent\"\n", true},
		{"compiled-entry", "run:\n  start: node dist/main.js\n", false},
		{"npx-serve", "run:\n  start: npx --no-install serve -s dist -l 3000\n", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte(tc.body), 0o644); err != nil {
				t.Fatalf("write yaml: %v", err)
			}
			got := codebaseHasNoopDevStart(dir)
			if got != tc.want {
				t.Errorf("codebaseHasNoopDevStart(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// newMemFactsLog returns a fresh FactsLog backed by a tempdir file.
// OpenFactsLog accepts a path that does not yet exist; Append creates
// it on first write.
func newMemFactsLog(t *testing.T, dir string) *FactsLog {
	t.Helper()
	return OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
}
