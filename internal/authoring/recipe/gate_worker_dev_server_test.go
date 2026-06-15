package recipe

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGateWorkerDevServerStarted_RefusesWhenAttestationMissing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	srcRoot := filepath.Join(root, "workerdev")
	if err := os.MkdirAll(srcRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Dev setup with dynamic-runtime base and no run.start — the canonical
	// post-run-49 shape. Gate fires because no worker_dev_server_started
	// fact was recorded.
	yamlBody := "zerops:\n  - setup: dev\n    run:\n      base: nodejs@22\n"
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
	yamlBody := "zerops:\n  - setup: dev\n    run:\n      base: nodejs@22\n"
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
	yamlBody := "zerops:\n  - setup: dev\n    run:\n      base: nodejs@22\n"
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

func TestGateWorkerDevServerStarted_SkipsCodebasesWithOnlyProdSetup(t *testing.T) {
	t.Parallel()
	// Codebase with a compiled prod setup only (no `setup: dev`) — gate
	// doesn't apply, no attestation required, no violation. Mirrors the
	// pre-run-49 "compiled-start" case where the gate didn't fire on
	// platform-managed processes.
	root := t.TempDir()
	srcRoot := filepath.Join(root, "apidev")
	if err := os.MkdirAll(srcRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	yamlBody := "zerops:\n  - setup: prod\n    run:\n      base: nodejs@22\n      start: node dist/main.js\n"
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
		t.Errorf("want 0 violations for compiled-only codebase, got %d", len(v))
	}
}

func TestGateWorkerDevServerStarted_SkipsImplicitWebserverDev(t *testing.T) {
	t.Parallel()
	// Implicit-webserver dev setup (php-nginx) — no `zerops_dev_server`
	// needed because the bundled webserver auto-serves on container boot.
	// Gate must NOT fire even without an attestation fact.
	root := t.TempDir()
	srcRoot := filepath.Join(root, "phpdev")
	if err := os.MkdirAll(srcRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	yamlBody := "zerops:\n  - setup: dev\n    run:\n      base: php-nginx@8.4\n      documentRoot: public\n"
	if err := os.WriteFile(filepath.Join(srcRoot, "zerops.yaml"), []byte(yamlBody), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	plan := &Plan{
		Codebases: []Codebase{
			{Hostname: "php", SourceRoot: srcRoot},
		},
	}
	ctx := GateContext{Plan: plan, OutputRoot: root, FactsLog: newMemFactsLog(t, root)}
	if v := gateWorkerDevServerStarted(ctx); len(v) != 0 {
		t.Errorf("want 0 violations for implicit-webserver dev codebase, got %d (%+v)", len(v), v)
	}
}

func TestGateWorkerDevServerStarted_SkipsStaticDev(t *testing.T) {
	t.Parallel()
	// Static dev setup — same exemption as implicit-webserver. Static
	// runtimes auto-serve deployFiles and need no dev-server.
	root := t.TempDir()
	srcRoot := filepath.Join(root, "appdev")
	if err := os.MkdirAll(srcRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	yamlBody := "zerops:\n  - setup: dev\n    run:\n      base: static\n"
	if err := os.WriteFile(filepath.Join(srcRoot, "zerops.yaml"), []byte(yamlBody), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	plan := &Plan{
		Codebases: []Codebase{
			{Hostname: "app", SourceRoot: srcRoot},
		},
	}
	ctx := GateContext{Plan: plan, OutputRoot: root, FactsLog: newMemFactsLog(t, root)}
	if v := gateWorkerDevServerStarted(ctx); len(v) != 0 {
		t.Errorf("want 0 violations for static dev codebase, got %d (%+v)", len(v), v)
	}
}

func TestCodebaseDevSetupIsDynamic_DetectsRuntimeClasses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"dev-nodejs", "zerops:\n  - setup: dev\n    run:\n      base: nodejs@22\n", true},
		{"dev-go", "zerops:\n  - setup: dev\n    run:\n      base: go@1\n", true},
		{"dev-python", "zerops:\n  - setup: dev\n    run:\n      base: python@3.12\n", true},
		{"dev-bun", "zerops:\n  - setup: dev\n    run:\n      base: bun@1.1\n", true},
		{"dev-php-nginx", "zerops:\n  - setup: dev\n    run:\n      base: php-nginx@8.4\n", false},
		{"dev-php-apache", "zerops:\n  - setup: dev\n    run:\n      base: php-apache@8.3\n", false},
		{"dev-static", "zerops:\n  - setup: dev\n    run:\n      base: static\n", false},
		{"dev-nginx", "zerops:\n  - setup: dev\n    run:\n      base: nginx@1.22\n", false},
		{"only-prod", "zerops:\n  - setup: prod\n    run:\n      base: nodejs@22\n      start: node dist/main.js\n", false},
		{"legacy-setup-name", "zerops:\n  - setup: appdev\n    run:\n      base: nodejs@22\n", true},
		{"quoted-base", "zerops:\n  - setup: dev\n    run:\n      base: \"nodejs@22\"\n", true},
		{"composite-base", "zerops:\n  - setup: dev\n    run:\n      base: alpine/nodejs@22\n", true},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte(tc.body), 0o644); err != nil {
				t.Fatalf("write yaml: %v", err)
			}
			got := codebaseDevSetupIsDynamic(dir)
			if got != tc.want {
				t.Errorf("codebaseDevSetupIsDynamic(%q) = %v, want %v", tc.name, got, tc.want)
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
