// Tests for: ops/git/ledger.go — the zcp deploy ledger (docs/spec-
// workflows.md §4.9): WriteLedger creates/overwrites one annotated tag per
// (project, target, appVersionId); LastDeployOnRecord reads the newest tag
// for a (project, target) pair back.
package git

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

func TestWriteLedger_Success_CreatesForcedAnnotatedTag(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{
		{}, // tag -a -f -m ... <name> <sha>
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
	if len(r.calls) != 1 {
		t.Fatalf("calls = %d, want 1: %+v", len(r.calls), r.calls)
	}
	script := r.calls[0].script
	if !strings.Contains(script, "tag -a -f -m") {
		t.Errorf("script = %q, want tag -a -f -m (force: a retried deploy of the same appVersion must not fail on an existing tag)", script)
	}
	if !strings.Contains(script, "'zcp/deploy/proj-1/appstage/av-1'") {
		t.Errorf("script = %q, want the tag name zcp/deploy/proj-1/appstage/av-1", script)
	}
	if !strings.Contains(script, "'sha1'") {
		t.Errorf("script = %q, want the tag pointed at sha1", script)
	}
	// commit-tree-free: tagging an existing commit needs an identity only
	// because `git tag -a` creates a tag OBJECT (tagger line) — same
	// rationale as the old commit-tree identity requirement.
	if !strings.Contains(script, "-c user.name=") || !strings.Contains(script, "-c user.email=") {
		t.Errorf("script = %q, want explicit -c user.name= and -c user.email= before tag", script)
	}
	if !strings.Contains(script, `"appVersionId":"av-1"`) {
		t.Errorf("script = %q, want the JSON ledger entry as -m", script)
	}
}

func TestWriteLedger_RunFails_ReturnsError(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{
		{stderr: "fatal: failed to write tag", err: errTest},
	}}
	entry := topology.LedgerEntry{SHA: "sha1", Target: "appstage", Project: "proj-1", AppVersionID: "av-1"}
	if err := WriteLedger(context.Background(), r, "/repo", entry); err == nil {
		t.Fatal("expected error")
	}
}

func TestLastDeployOnRecord_NothingOnRecord_ReturnsNotOkNoError(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{{stdout: ""}}}
	_, ok, err := LastDeployOnRecord(context.Background(), r, "/repo", "proj-1", "appstage")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("ok = true, want false (nothing on record is not a failure)")
	}
	if !strings.Contains(r.calls[0].script, "refs/tags/zcp/deploy/proj-1/appstage") {
		t.Errorf("script = %q, want it to read refs/tags/zcp/deploy/proj-1/appstage", r.calls[0].script)
	}
	if !strings.Contains(r.calls[0].script, "--sort=-taggerdate") || !strings.Contains(r.calls[0].script, "--count=1") {
		t.Errorf("script = %q, want --sort=-taggerdate --count=1 (newest wins)", r.calls[0].script)
	}
}

func TestLastDeployOnRecord_TagExists_DecodesJSONMessage(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{
		{stdout: "commitsha1\t{\"sha\":\"commitsha1\",\"appVersionId\":\"av-1\",\"target\":\"appstage\",\"project\":\"proj-1\",\"at\":\"2026-09-14T12:00:00Z\"}\n"},
	}}
	entry, ok, err := LastDeployOnRecord(context.Background(), r, "/repo", "proj-1", "appstage")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	want := topology.LedgerEntry{SHA: "commitsha1", AppVersionID: "av-1", Target: "appstage", Project: "proj-1", At: "2026-09-14T12:00:00Z"}
	if entry != want {
		t.Errorf("entry = %+v, want %+v", entry, want)
	}
}

func TestLastDeployOnRecord_RunFails_ReturnsError(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{{stderr: "fatal: not a git repository", err: errTest}}}
	_, ok, err := LastDeployOnRecord(context.Background(), r, "/repo", "proj-1", "appstage")
	if err == nil {
		t.Fatal("expected error")
	}
	if ok {
		t.Error("ok = true, want false on error")
	}
}

var errTest = &testError{"boom"}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }
