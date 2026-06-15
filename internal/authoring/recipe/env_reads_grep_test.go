package recipe

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Run-40 B1 — sourceGrepEnvReads + parseDeclaredRunEnvKeys
// implementation pins.

// TestSourceGrepEnvReads_NodeProcessEnv covers the primary Node.js
// pattern: `process.env.<KEY>` dot-access. Pinned against the
// canonical run-39 case where the source carries
// `process.env.NATS_HOST` etc.
func TestSourceGrepEnvReads_NodeProcessEnv(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := `import { connect } from 'nats';
const nc = await connect({
  servers: ` + "`${process.env.NATS_HOST}:${process.env.NATS_PORT}`" + `,
  user: process.env.NATS_USER,
  pass: process.env.NATS_PASS,
});
`
	if err := os.WriteFile(filepath.Join(src, "main.ts"), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := sourceGrepEnvReads(dir)
	if err != nil {
		t.Fatalf("sourceGrepEnvReads: %v", err)
	}
	want := []string{"NATS_HOST", "NATS_PASS", "NATS_PORT", "NATS_USER"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("env reads = %v, want %v", got, want)
	}
}

// TestSourceGrepEnvReads_BracketForms — process.env["KEY"] and
// process.env['KEY'] are equivalent dot-access in Node.js and must
// be detected. Pinned so a regex regression doesn't silently miss
// codebases that prefer bracket-access style.
func TestSourceGrepEnvReads_BracketForms(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body := `const host = process.env["DB_HOST"];
const port = process.env['DB_PORT'];
const url = process.env.DB_URL;
`
	if err := os.WriteFile(filepath.Join(dir, "config.js"), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, _ := sourceGrepEnvReads(dir)
	want := []string{"DB_HOST", "DB_PORT", "DB_URL"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("env reads = %v, want %v", got, want)
	}
}

// TestSourceGrepEnvReads_NonEnvObjectKeysNotConfused — second-pass
// codex review caught a false-positive in the run-40 fix-up
// destructure regex: the bogus pattern `= process.env[^{]*{...}`
// matched `const env = process.env; const config = { DB_HOST: "x" }`
// and recorded `DB_HOST` as an env read even though the code never
// reads it from process.env. The pattern was deleted because no
// valid JS destructure shape places process.env BEFORE the braces.
// This test pins the false-positive scenario can't regress.
func TestSourceGrepEnvReads_NonEnvObjectKeysNotConfused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body := `// Bind process.env reference then build an unrelated config object.
const env = process.env;
const config = { DB_HOST: "literal-not-from-env", DB_PORT: 5432 };
const usedFromEnv = process.env.REAL_KEY;
`
	if err := os.WriteFile(filepath.Join(dir, "config.ts"), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := sourceGrepEnvReads(dir)
	if err != nil {
		t.Fatalf("sourceGrepEnvReads: %v", err)
	}
	want := []string{"REAL_KEY"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("non-env object keys leaked into reads.\nwant=%v\ngot=%v", want, got)
	}
}

// TestSourceGrepEnvReads_Destructuring — Run-40 fix-up #10. Captures
// `const { A, B } = process.env` and `const { VITE_X } = import.meta.env`
// shapes including aliasing (`{ DB_HOST: host }` → captures DB_HOST).
// Codex code review flagged this as an idiomatic JS pattern the
// original grep missed; the gate would false-positive on declared
// envs the source reads through destructuring.
func TestSourceGrepEnvReads_Destructuring(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body := `const { DB_HOST, DB_PORT } = process.env;
const { VITE_API_URL } = import.meta.env;
const { DB_USER: user, DB_PASSWORD: pass } = process.env;
`
	if err := os.WriteFile(filepath.Join(dir, "config.ts"), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := sourceGrepEnvReads(dir)
	if err != nil {
		t.Fatalf("sourceGrepEnvReads: %v", err)
	}
	want := []string{"DB_HOST", "DB_PASSWORD", "DB_PORT", "DB_USER", "VITE_API_URL"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("destructured env reads = %v, want %v", got, want)
	}
}

// TestSourceGrepEnvReads_ImportMetaEnv covers Vite/Astro
// `import.meta.env.<KEY>` reads — the build-time bake idiom.
func TestSourceGrepEnvReads_ImportMetaEnv(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body := `const apiUrl = import.meta.env.VITE_API_URL;
const buildId = import.meta.env["VITE_BUILD_ID"];
`
	if err := os.WriteFile(filepath.Join(dir, "api.ts"), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, _ := sourceGrepEnvReads(dir)
	want := []string{"VITE_API_URL", "VITE_BUILD_ID"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("env reads = %v, want %v", got, want)
	}
}

// TestSourceGrepEnvReads_SkipsNodeModulesAndDist — the walk must
// prune generated and dependency dirs so vendor code's env reads
// don't get attributed to the codebase.
func TestSourceGrepEnvReads_SkipsNodeModulesAndDist(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, sub := range []string{"node_modules/pkg", "dist", "build", ".next"} {
		full := filepath.Join(dir, sub)
		if err := os.MkdirAll(full, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
		if err := os.WriteFile(filepath.Join(full, "x.ts"), []byte("process.env.SHOULD_BE_SKIPPED"), 0o600); err != nil {
			t.Fatalf("write skip: %v", err)
		}
	}
	// Real source carries one legitimate read.
	if err := os.WriteFile(filepath.Join(dir, "real.ts"), []byte("const x = process.env.REAL_KEY;"), 0o600); err != nil {
		t.Fatalf("write real: %v", err)
	}
	got, _ := sourceGrepEnvReads(dir)
	want := []string{"REAL_KEY"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("env reads = %v, want %v (skip dirs must have been pruned)", got, want)
	}
}

// TestSourceGrepEnvReads_AbsentSourceRoot_ReturnsError — missing
// path bubbles up so the caller can decide whether to refuse close
// or skip. Distinguished from "scanned but empty".
func TestSourceGrepEnvReads_AbsentSourceRoot_ReturnsError(t *testing.T) {
	t.Parallel()
	_, err := sourceGrepEnvReads("/var/www/definitely-not-here")
	if err == nil {
		t.Errorf("expected error on absent sourceRoot; got nil")
	}
}

// TestParseDeclaredRunEnvKeys_ListOfSetupBlocks pins the canonical
// recipe yaml shape (list of `- setup: <host>` blocks each with their
// own run.envVariables map). Run-39's apidev/zerops.yaml uses this
// form.
func TestParseDeclaredRunEnvKeys_ListOfSetupBlocks(t *testing.T) {
	t.Parallel()
	yaml := `zerops:
  - setup: apidev
    run:
      envVariables:
        SEARCH_PUBLIC_HOST: ${search_zeropsSubdomain}
        SEARCH_SEARCH_KEY: ${search_defaultSearchKey}
        DB_HOST: ${db_hostname}
  - setup: apistage
    run:
      envVariables:
        APP_SECRET: ${APP_SECRET}
`
	got := parseDeclaredRunEnvKeys(yaml)
	want := []string{"APP_SECRET", "DB_HOST", "SEARCH_PUBLIC_HOST", "SEARCH_SEARCH_KEY"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseDeclaredRunEnvKeys = %v, want %v", got, want)
	}
}

// TestParseDeclaredRunEnvKeys_EmptyYaml — no envVariables blocks
// returns an empty (non-nil) slice. Callers can range without nil-
// check.
func TestParseDeclaredRunEnvKeys_EmptyYaml(t *testing.T) {
	t.Parallel()
	got := parseDeclaredRunEnvKeys("zerops: []\n")
	if got == nil {
		t.Fatal("expected non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice; got %d entries", len(got))
	}
}

// TestPopulateEnvReadsFromSource_HappyPath — feature-phase populate
// fills plan.ObservedFacts.EnvReads with the source-grep result per
// codebase. Pinned end-to-end via a session containing two codebases.
func TestPopulateEnvReadsFromSource_HappyPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cbDir := filepath.Join(dir, "apidev")
	if err := os.MkdirAll(filepath.Join(cbDir, "src"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := `const dbHost = process.env.DB_HOST;
const dbPort = process.env.DB_PORT;
`
	if err := os.WriteFile(filepath.Join(cbDir, "src", "config.ts"), []byte(body), 0o600); err != nil {
		t.Fatalf("write src: %v", err)
	}

	sess := &Session{
		Slug:       "synth-showcase",
		OutputRoot: dir,
		Plan: &Plan{
			Slug: "synth-showcase",
			Codebases: []Codebase{
				{Hostname: "api", SourceRoot: cbDir},
			},
		},
	}
	if err := populateEnvReadsFromSource(sess); err != nil {
		t.Fatalf("populateEnvReadsFromSource: %v", err)
	}
	got := sess.Plan.ObservedFacts.EnvReads["api"]
	want := []string{"DB_HOST", "DB_PORT"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("plan.ObservedFacts.EnvReads[api] = %v, want %v", got, want)
	}
}
