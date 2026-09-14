// Tests for: ops/git/ledger.go — the refs/zcp/* deploy ledger
// (docs/spec-workflows.md §4.5): WriteLedger moves refs/zcp/env/<target> and
// appends a refs/zcp/deploy/<n> commit; ReadEnvRef reads the former back.
// The latter is read directly via `git log refs/zcp/deploy/*` (over SSH or
// locally) — no Go-side ListDeploys; nothing called it.
package git

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

func TestWriteLedger_Success_ResolvesTreeCommitsAndMovesBothRefs(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{
		{stdout: "tree-abc\n"},   // rev-parse <sha>^{tree}
		{stdout: "commit-xyz\n"}, // commit-tree
		{},                       // update-ref deploy/<n>
		{},                       // update-ref env/<target>
	}}
	entry := topology.LedgerEntry{
		SHA:          "sha1",
		AppVersionID: "av-1",
		Target:       "appstage",
		Project:      "proj-1",
		At:           "2026-09-14T12:00:00Z",
	}
	if err := WriteLedger(context.Background(), r, "/repo", entry); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(r.calls) != 4 {
		t.Fatalf("calls = %d, want 4: %+v", len(r.calls), r.calls)
	}
	if !strings.Contains(r.calls[0].script, "rev-parse") || !strings.Contains(r.calls[0].script, "^{tree}") {
		t.Errorf("call[0] = %q, want rev-parse ...^{tree}", r.calls[0].script)
	}
	if !strings.Contains(r.calls[1].script, "commit-tree 'tree-abc' -p 'sha1'") {
		t.Errorf("call[1] = %q, want commit-tree 'tree-abc' -p 'sha1'", r.calls[1].script)
	}
	// commit-tree creates a commit object, which needs an author/committer
	// identity — a buildFromGit-provisioned container has no ~/.gitconfig
	// and no GIT_AUTHOR_*/GIT_COMMITTER_* env (unlike a self-deploy
	// container, which InitServiceGit seeds), so without an explicit
	// identity here the write fails "unable to auto-detect email address"
	// and is silently lost (WriteLedger's caller only sees a response
	// warning, easy to miss).
	if !strings.Contains(r.calls[1].script, "-c user.name=") || !strings.Contains(r.calls[1].script, "-c user.email=") {
		t.Errorf("call[1] = %q, want explicit -c user.name= and -c user.email= before commit-tree", r.calls[1].script)
	}
	if idx := strings.Index(r.calls[1].script, "-c user.name="); idx < 0 || idx > strings.Index(r.calls[1].script, "commit-tree") {
		t.Errorf("call[1] = %q, want the -c identity flags BEFORE commit-tree in the git invocation", r.calls[1].script)
	}
	// The commit message is the entry's JSON, single line.
	if !strings.Contains(r.calls[1].script, `"appVersionId":"av-1"`) {
		t.Errorf("call[1] = %q, want the JSON ledger entry as -m", r.calls[1].script)
	}
	if !strings.Contains(r.calls[2].script, "update-ref 'refs/zcp/deploy/") {
		t.Errorf("call[2] = %q, want update-ref 'refs/zcp/deploy/<n>' 'commit-xyz'", r.calls[2].script)
	}
	if !strings.Contains(r.calls[2].script, "'commit-xyz'") {
		t.Errorf("call[2] = %q, want the new commit sha", r.calls[2].script)
	}
	if !strings.Contains(r.calls[3].script, "update-ref 'refs/zcp/env/appstage' 'sha1'") {
		t.Errorf("call[3] = %q, want update-ref 'refs/zcp/env/appstage' 'sha1'", r.calls[3].script)
	}
}

func TestWriteLedger_TreeResolutionFails_ReturnsErrorWithoutMovingRefs(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{
		{stderr: "fatal: bad object", err: errTest},
	}}
	entry := topology.LedgerEntry{SHA: "sha1", Target: "appstage"}
	if err := WriteLedger(context.Background(), r, "/repo", entry); err == nil {
		t.Fatal("expected error")
	}
	if len(r.calls) != 1 {
		t.Fatalf("calls = %d, want 1 (must not move refs after a failed tree resolution)", len(r.calls))
	}
}

func TestReadEnvRef_NoRefYet_ReturnsEmptyNoError(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{{err: errTest}}}
	got, err := ReadEnvRef(context.Background(), r, "/repo", "appstage")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("got = %q, want empty (no ledger yet is not an error)", got)
	}
}

func TestReadEnvRef_RefExists_ReturnsSHA(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{{stdout: "sha1\n"}}}
	got, err := ReadEnvRef(context.Background(), r, "/repo", "appstage")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "sha1" {
		t.Errorf("got = %q, want sha1", got)
	}
	if !strings.Contains(r.calls[0].script, "refs/zcp/env/appstage") {
		t.Errorf("script = %q, want it to read refs/zcp/env/appstage", r.calls[0].script)
	}
}

var errTest = &testError{"boom"}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }
