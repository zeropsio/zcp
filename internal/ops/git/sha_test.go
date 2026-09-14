// Tests for: ops/git/sha.go — resolving a sha and materializing its tree
// into a temp directory, both via a fake Runner recording the exact command
// sequence (docs/spec-workflows.md §4.5, P3: `git archive <sha> | tar -x`
// reproduces exactly what `git ls-files` would push, without a .tgz
// round-trip through zcli's own archive-output flag).
package git

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeRunner is a scripted Runner: each call consumes the next queued
// result, recording (dir, script) for assertions.
type fakeRunner struct {
	results []fakeResult
	calls   []fakeCall
	idx     int
}

type fakeResult struct {
	stdout, stderr string
	err            error
}

type fakeCall struct {
	dir, script string
}

func (f *fakeRunner) Run(_ context.Context, dir, script string) (string, string, error) {
	f.calls = append(f.calls, fakeCall{dir: dir, script: script})
	if f.idx >= len(f.results) {
		return "", "", nil
	}
	r := f.results[f.idx]
	f.idx++
	return r.stdout, r.stderr, r.err
}

func TestResolveSHA_ValidSHA_RunsRevParseVerifyCommit(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{{stdout: "abc123def456\n"}}}
	got, err := ResolveSHA(context.Background(), r, "/repo", "abc123d")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "abc123def456" {
		t.Errorf("resolved = %q, want abc123def456", got)
	}
	if len(r.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(r.calls))
	}
	if r.calls[0].dir != "/repo" {
		t.Errorf("dir = %q, want /repo", r.calls[0].dir)
	}
	if !strings.Contains(r.calls[0].script, "git rev-parse --verify") {
		t.Errorf("script = %q, want git rev-parse --verify", r.calls[0].script)
	}
	if !strings.Contains(r.calls[0].script, "^{commit}") {
		t.Errorf("script = %q, want the ^{commit} suffix (commit-only resolution)", r.calls[0].script)
	}
}

func TestResolveSHA_InvalidSHA_ReturnsError(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{{stderr: "fatal: bad revision", err: errors.New("exit 128")}}}
	_, err := ResolveSHA(context.Background(), r, "/repo", "nope")
	if err == nil {
		t.Fatal("expected error for unresolvable sha")
	}
	if !strings.Contains(err.Error(), "bad revision") {
		t.Errorf("error = %v, want it to surface the git stderr", err)
	}
}

func TestExtractCommitToTemp_RunsArchivePipedToTar(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{{}}}
	err := ExtractCommitToTemp(context.Background(), r, "/repo", "abc123", "/tmp/zcp-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(r.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(r.calls))
	}
	if r.calls[0].dir != "/repo" {
		t.Errorf("dir = %q, want /repo", r.calls[0].dir)
	}
	script := r.calls[0].script
	if !strings.Contains(script, "git archive --format=tar") {
		t.Errorf("script = %q, want git archive --format=tar", script)
	}
	if !strings.Contains(script, "abc123") {
		t.Errorf("script = %q, want the sha", script)
	}
	if !strings.Contains(script, "| tar -x -C") {
		t.Errorf("script = %q, want a pipe into tar -x -C", script)
	}
	if !strings.Contains(script, "/tmp/zcp-1") {
		t.Errorf("script = %q, want the tmp dir as the tar extract target", script)
	}
}

func TestExtractCommitToTemp_ArchiveFails_ReturnsError(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{{stderr: "fatal: not a valid object name", err: errors.New("exit 128")}}}
	err := ExtractCommitToTemp(context.Background(), r, "/repo", "deadbeef", "/tmp/zcp-1")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not a valid object name") {
		t.Errorf("error = %v, want it to surface git stderr", err)
	}
}
