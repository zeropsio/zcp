// Tests for: Store.CoversHost — Fix 1 of run-25 unblock work.
// Background: deploy gate (`requireAdoption`) blocks cross-deploy of
// `apistage`/`appstage` while a recipe session has `api`/`app` codebases
// in its Plan. CoversHost answers "does any open recipe session own this
// host" so the deploy gate can skip the bootstrap-adoption check during
// recipe authoring.
package recipe

import (
	"path/filepath"
	"testing"
)

func TestStoreCoversHost_BareCodebaseMatches(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	sess, err := store.OpenOrCreate("synth-showcase", filepath.Join(dir, "run"))
	if err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	sess.Plan = &Plan{
		Slug:      "synth-showcase",
		Codebases: []Codebase{{Hostname: "api"}},
	}
	if !store.CoversHost("api") {
		t.Error("bare codebase hostname `api` should match")
	}
}

func TestStoreCoversHost_DevSlotMatches(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	sess, err := store.OpenOrCreate("synth-showcase", filepath.Join(dir, "run"))
	if err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	sess.Plan = &Plan{
		Slug:      "synth-showcase",
		Codebases: []Codebase{{Hostname: "api"}},
	}
	if !store.CoversHost("apidev") {
		t.Error("`apidev` (dev slot for codebase `api`) should match")
	}
}

func TestStoreCoversHost_StageSlotMatches(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	sess, err := store.OpenOrCreate("synth-showcase", filepath.Join(dir, "run"))
	if err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	sess.Plan = &Plan{
		Slug:      "synth-showcase",
		Codebases: []Codebase{{Hostname: "api"}},
	}
	if !store.CoversHost("apistage") {
		t.Error("`apistage` (stage slot for codebase `api`) should match")
	}
}

func TestStoreCoversHost_ServiceMatches(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	sess, err := store.OpenOrCreate("synth-showcase", filepath.Join(dir, "run"))
	if err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	sess.Plan = &Plan{
		Slug:     "synth-showcase",
		Services: []Service{{Hostname: "db", Kind: ServiceKindManaged, Type: "postgresql@18"}},
	}
	if !store.CoversHost("db") {
		t.Error("managed service hostname `db` should match")
	}
}

func TestStoreCoversHost_EmptyHost_ReturnsFalse(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	sess, err := store.OpenOrCreate("synth-showcase", filepath.Join(dir, "run"))
	if err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	sess.Plan = &Plan{
		Slug:      "synth-showcase",
		Codebases: []Codebase{{Hostname: "api"}},
	}
	if store.CoversHost("") {
		t.Error("empty host must not match")
	}
}

func TestStoreCoversHost_NoOpenSessions_ReturnsFalse(t *testing.T) {
	t.Parallel()

	store := NewStore(t.TempDir())
	if store.CoversHost("api") {
		t.Error("no open sessions → CoversHost must return false")
	}
}

func TestStoreCoversHost_UnrelatedHost_ReturnsFalse(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	sess, err := store.OpenOrCreate("synth-showcase", filepath.Join(dir, "run"))
	if err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	sess.Plan = &Plan{
		Slug:      "synth-showcase",
		Codebases: []Codebase{{Hostname: "api"}},
	}
	if store.CoversHost("unrelated-host") {
		t.Error("unrelated host must not match")
	}
}

// TestStoreCoversHost_EmptyCodebases_ReturnsFalse pins the strict-match
// rule from the codex review: empty Plan.Codebases must NOT fall back to
// "true" the way refinement_suspects.FactBelongsToCodebases does. The
// adoption-exemption use case requires a positive match, not a permissive
// fallback.
func TestStoreCoversHost_EmptyCodebases_ReturnsFalse(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	sess, err := store.OpenOrCreate("synth-showcase", filepath.Join(dir, "run"))
	if err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	// Open session with empty Plan (Codebases + Services both empty).
	sess.Plan = &Plan{Slug: "synth-showcase"}
	if store.CoversHost("api") {
		t.Error("empty codebases + services must NOT cover any host (no permissive fallback)")
	}
}

// TestStoreCoversHost_MultipleSessionsOneCovers verifies that if one of
// several open sessions covers the host, CoversHost returns true.
func TestStoreCoversHost_MultipleSessionsOneCovers(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	sessA, err := store.OpenOrCreate("alpha", filepath.Join(dir, "a"))
	if err != nil {
		t.Fatalf("OpenOrCreate alpha: %v", err)
	}
	sessA.Plan = &Plan{Slug: "alpha", Codebases: []Codebase{{Hostname: "api"}}}

	sessB, err := store.OpenOrCreate("beta", filepath.Join(dir, "b"))
	if err != nil {
		t.Fatalf("OpenOrCreate beta: %v", err)
	}
	sessB.Plan = &Plan{Slug: "beta", Codebases: []Codebase{{Hostname: "web"}}}

	if !store.CoversHost("apistage") {
		t.Error("apistage covered by alpha's `api` codebase")
	}
	if !store.CoversHost("webdev") {
		t.Error("webdev covered by beta's `web` codebase")
	}
	if store.CoversHost("workerdev") {
		t.Error("workerdev not in any plan — must not match")
	}
}
