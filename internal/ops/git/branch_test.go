// Tests for: ops/git/branch.go — resolving the tracked ref a checkout is
// on (GF-7, docs/spec-workflows.md §12.6), via a fake Runner recording the
// exact command sequence. Mirrors sha_test.go's fakeRunner harness.
package git

import (
	"context"
	"strings"
	"testing"
)

func TestCurrentBranch_AttachedHEAD_ReturnsBranchName(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{{stdout: "release\n"}}}
	got, err := CurrentBranch(context.Background(), r, "/var/www", "https://github.com/example/app.git")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "release" {
		t.Errorf("branch = %q, want %q", got, "release")
	}
	if len(r.calls) != 1 {
		t.Fatalf("calls = %d, want 1 (symbolic-ref only — HEAD is attached, no remote lookup needed)", len(r.calls))
	}
	if !strings.Contains(r.calls[0].script, "git symbolic-ref --short HEAD") {
		t.Errorf("script = %q, want git symbolic-ref --short HEAD", r.calls[0].script)
	}
}

func TestCurrentBranch_DetachedHEAD_FallsBackToRemoteDefaultBranch(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{
		{stdout: ""}, // symbolic-ref: detached, no output
		{stdout: "ref: refs/heads/develop\tHEAD\nabc123\tHEAD\n"},
	}}
	got, err := CurrentBranch(context.Background(), r, "/var/www", "https://github.com/example/app.git")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "develop" {
		t.Errorf("branch = %q, want %q (remote default branch)", got, "develop")
	}
	if len(r.calls) != 2 {
		t.Fatalf("calls = %d, want 2 (symbolic-ref, then ls-remote --symref)", len(r.calls))
	}
	if !strings.Contains(r.calls[1].script, "git ls-remote --symref") || !strings.Contains(r.calls[1].script, "example/app.git") {
		t.Errorf("second script = %q, want ls-remote --symref against the remote URL", r.calls[1].script)
	}
}

func TestCurrentBranch_UnbornNoRemote_ReturnsEmpty(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{{stdout: ""}}}
	got, err := CurrentBranch(context.Background(), r, "/var/www", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("branch = %q, want empty (unborn HEAD, no remote to fall back to)", got)
	}
	if len(r.calls) != 1 {
		t.Fatalf("calls = %d, want 1 (no remote URL ⇒ no ls-remote attempt)", len(r.calls))
	}
}

func TestCurrentBranch_DetachedHEAD_RemoteLookupFails_ReturnsEmptyNoError(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{
		{stdout: ""},
		{err: errTestLsRemote},
	}}
	got, err := CurrentBranch(context.Background(), r, "/var/www", "https://github.com/example/app.git")
	if err != nil {
		t.Fatalf("remote lookup failure must be swallowed (best-effort; caller falls back to \"main\"), got error: %v", err)
	}
	if got != "" {
		t.Errorf("branch = %q, want empty", got)
	}
}

var errTestLsRemote = &fakeRunnerError{"ls-remote: network unreachable"}

type fakeRunnerError struct{ msg string }

func (e *fakeRunnerError) Error() string { return e.msg }
