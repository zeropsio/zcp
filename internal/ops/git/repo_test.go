// Tests for: ops/git/repo.go — the repo-always guarantee run at adopt
// (init-if-missing + identity + HEAD guarantee when needed), driven
// through the Runner abstraction so it works identically over SSH
// (container) and locally (docs/spec-workflows.md's Git Lifecycle
// section, GLC-7).
package git

import (
	"context"
	"errors"
	"testing"
)

var errTest = errors.New("test failure")

// TestAdoptBaseline_NoRepo_InitsAndEnsures pins the probe=1 case (no repo
// at all): git init runs (probe=1 skips the tree check entirely — exists
// is already false), then identity ensure, then HEAD ensure — the same
// three steps bootstrap's InitServiceGit runs, minus any staging or
// commit of the files found on disk.
func TestAdoptBaseline_NoRepo_InitsAndEnsures(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{
		{err: errTest}, // test -d .git -> not a repo
		{},             // git init -b main
		{},             // identity ensure
		{},             // HEAD ensure
	}}
	result, err := AdoptBaseline(context.Background(), r, "/var/www")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Case != AdoptCaseInitialized {
		t.Errorf("Case = %q, want %q", result.Case, AdoptCaseInitialized)
	}
	if len(r.calls) != 4 {
		t.Fatalf("calls = %d, want 4: %+v", len(r.calls), r.calls)
	}
	if r.calls[1].script != "git init -q -b main" {
		t.Errorf("call[1] = %q, want git init", r.calls[1].script)
	}
	wantIdentity := `(test -n "$(git config user.email)" || git config user.email 'agent@zerops.io') && (test -n "$(git config user.name)" || git config user.name 'Zerops Agent')`
	if r.calls[2].script != wantIdentity {
		t.Errorf("call[2] = %q, want %q", r.calls[2].script, wantIdentity)
	}
	wantHead := `(git rev-parse -q --verify HEAD >/dev/null || git update-ref HEAD "$(git -c user.email='agent@zerops.io' -c user.name='Zerops Agent' commit-tree "$(git mktree </dev/null)" -m 'zcp init')")`
	if r.calls[3].script != wantHead {
		t.Errorf("call[3] = %q, want %q", r.calls[3].script, wantHead)
	}
}

// TestAdoptBaseline_EmptyTreeHEAD_SkipsInitButStillEnsures pins the
// probe=2 case (docs/spec-workflows.md's Git Lifecycle section, GLC-7): a
// repo that already exists (ops.InitServiceGit's GLC-1 marker commit ran
// first) but whose HEAD is over the empty tree must NOT be trusted as
// content — no git init (the repo already exists), but identity + HEAD
// ensure still run (both no-op in practice, since GLC-1 already filled
// them — this proves AdoptBaseline doesn't skip the guarantee just
// because SOME repo state is already present).
func TestAdoptBaseline_EmptyTreeHEAD_SkipsInitButStillEnsures(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{
		{}, // test -d .git -> is a repo
		{stdout: "4b825dc642cb6eb9a060e54bf8d69288fbee4904\n"}, // rev-parse HEAD^{tree} -> empty tree
		{}, // identity ensure
		{}, // HEAD ensure
	}}
	result, err := AdoptBaseline(context.Background(), r, "/var/www")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Case != AdoptCaseInitialized {
		t.Errorf("Case = %q, want %q", result.Case, AdoptCaseInitialized)
	}
	if len(r.calls) != 4 {
		t.Fatalf("calls = %d, want 4 (no git init call): %+v", len(r.calls), r.calls)
	}
	for _, c := range r.calls {
		if c.script == "git init -q -b main" {
			t.Errorf("calls = %+v, must not init — the repo already exists", r.calls)
		}
	}
	if r.calls[1].script != `git rev-parse --verify 'HEAD^{tree}'` {
		t.Errorf("call[1] = %q, want the tree probe", r.calls[1].script)
	}
}

// TestAdoptBaseline_ContentHEAD_PreservesHEAD pins the probe=3 case: a HEAD
// whose tree already carries content is trusted as-is — the HEAD is preserved, no commit, no history change.
func TestAdoptBaseline_ContentHEAD_PreservesHEAD(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{
		{},                          // test -d .git -> is a repo
		{stdout: "tree-sha-real\n"}, // rev-parse HEAD^{tree} -> real content
	}}
	result, err := AdoptBaseline(context.Background(), r, "/var/www")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Case != AdoptCaseExisting {
		t.Errorf("Case = %q, want %q", result.Case, AdoptCaseExisting)
	}
	if len(r.calls) != 2 {
		t.Fatalf("calls = %d, want 2 (probe + tree probe only, no init/identity/head): %+v", len(r.calls), r.calls)
	}
}

// TestAdoptBaseline_IdentityWriteFails_ReturnsError pins that a failure
// mid-chain (identity ensure) surfaces as an error rather than silently
// reporting AdoptCaseInitialized.
func TestAdoptBaseline_IdentityWriteFails_ReturnsError(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{
		{err: errTest}, // test -d .git -> not a repo
		{},             // git init -b main
		{err: errTest, stderr: "permission denied"}, // identity ensure fails
	}}
	_, err := AdoptBaseline(context.Background(), r, "/var/www")
	if err == nil {
		t.Fatal("expected error when identity ensure fails")
	}
}

// TestAdoptBaseline_HeadEnsureFails_ReturnsError mirrors the identity
// failure pin for the HEAD-ensure step.
func TestAdoptBaseline_HeadEnsureFails_ReturnsError(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{
		{err: errTest},                      // test -d .git -> not a repo
		{},                                  // git init -b main
		{},                                  // identity ensure
		{err: errTest, stderr: "disk full"}, // HEAD ensure fails
	}}
	_, err := AdoptBaseline(context.Background(), r, "/var/www")
	if err == nil {
		t.Fatal("expected error when HEAD ensure fails")
	}
}
