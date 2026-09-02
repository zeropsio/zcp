// White-box tests for the mate environment merge — internal to package service
// because the store path (internal/mate.LiveEnvStorePath) is a fixed absolute
// constant, and these tests need a temp path in its place. The external
// service_test.go exercises the same merge end to end through
// service.Start("mate"), but only for the "store absent" case: writing to the
// real, root-owned /etc/zerops-zembed/env.json from a test is not an option.
package service

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestMergeMateEnv_OrderAndPrecedence locks the precedence contract: the
// process environment = os.Environ() (applied by the caller, runCommand) ←
// the live env store (every key) ← the T3CODE_* env file (wins on collision).
// mergeMateEnv resolves the store-vs-file half of that chain; runCommand's
// append(os.Environ(), ...) resolves the rest.
func TestMergeMateEnv_OrderAndPrecedence(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	storePath := filepath.Join(dir, "env.json")
	storeBody := `{"PATH":"/opt/zerops/bin:/usr/local/bin:/usr/bin","ZCP_API_KEY":"key-1","VSCODE_PASSWORD":"pw-1","T3CODE_ZEROPS_PROJECT_ID":"stale-project"}`
	if err := os.WriteFile(storePath, []byte(storeBody), 0o644); err != nil {
		t.Fatalf("write store: %v", err)
	}

	envPath := filepath.Join(dir, "mate.env")
	envBody := "T3CODE_ZEROPS_PROJECT_ID=nTV3oMB2SS634ImDJnQckg\nT3CODE_ZEROPS_API_HOST=api.app-prg1.zerops.io\n"
	if err := os.WriteFile(envPath, []byte(envBody), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	got := mergeMateEnv(storePath, envPath)

	// A key present in both the store and the env file takes the env file's
	// value — a stale store must never win over what zcp init just wrote.
	if slices.Contains(got, "T3CODE_ZEROPS_PROJECT_ID=stale-project") {
		t.Errorf("store's stale value must not survive the merge: %q", got)
	}
	if !slices.Contains(got, "T3CODE_ZEROPS_PROJECT_ID=nTV3oMB2SS634ImDJnQckg") {
		t.Errorf("env file's value must win: %q", got)
	}

	// A key present only in the store is passed through — this is also what
	// makes PATH from the store replace the unit's own (near-empty) PATH once
	// runCommand appends this list over os.Environ().
	for _, want := range []string{
		"PATH=/opt/zerops/bin:/usr/local/bin:/usr/bin",
		"ZCP_API_KEY=key-1",
		"VSCODE_PASSWORD=pw-1",
		"T3CODE_ZEROPS_API_HOST=api.app-prg1.zerops.io",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("merged env must contain %q, got %q", want, got)
		}
	}

	// Exactly one line per key: the store's stale T3CODE_ZEROPS_PROJECT_ID is
	// dropped, not merely shadowed. 3 store-only keys + 2 file keys = 5.
	const wantCount = 5
	if len(got) != wantCount {
		t.Errorf("merged env length = %d, want %d (got %q)", len(got), wantCount, got)
	}
}

// TestMergeMateEnv_StoreOnly covers a store with no corresponding env file
// entries: every store key passes through unchanged.
func TestMergeMateEnv_StoreOnly(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	storePath := filepath.Join(dir, "env.json")
	if err := os.WriteFile(storePath, []byte(`{"HOME":"/home/zerops","PATH":"/opt/zerops/bin"}`), 0o644); err != nil {
		t.Fatalf("write store: %v", err)
	}
	envPath := filepath.Join(dir, "mate.env") // never created — the file is absent

	got := mergeMateEnv(storePath, envPath)
	want := []string{"HOME=/home/zerops", "PATH=/opt/zerops/bin"}
	if !slices.Equal(got, want) {
		t.Errorf("mergeMateEnv with no env file:\n got %q\nwant %q", got, want)
	}
}

// TestMergeMateEnv_EnvFileOnly covers the store being absent (the sandbox/CI
// reality: /etc/zerops-zembed/env.json does not exist on a dev machine) — the
// merge degrades to exactly the env file's lines, matching the pre-existing
// behavior TestStart_Mate_MergesEnvFile (service_test.go) still pins end to end.
func TestMergeMateEnv_EnvFileOnly(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	storePath := filepath.Join(dir, "does-not-exist.json")
	envPath := filepath.Join(dir, "mate.env")
	envBody := "T3CODE_ZEROPS_PROJECT_ID=p1\nT3CODE_ZEROPS_API_HOST=api.app-prg1.zerops.io\n"
	if err := os.WriteFile(envPath, []byte(envBody), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	got := mergeMateEnv(storePath, envPath)
	want := []string{"T3CODE_ZEROPS_PROJECT_ID=p1", "T3CODE_ZEROPS_API_HOST=api.app-prg1.zerops.io"}
	if !slices.Equal(got, want) {
		t.Errorf("mergeMateEnv with no store:\n got %q\nwant %q", got, want)
	}
}
