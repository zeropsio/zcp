// Tests for: ops/repo_scaffold.go — the SSH-side wiring of ops/git's
// repo-always primitives (docs/spec-workflows.md's Git Lifecycle section).
package ops

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

func TestAdoptRepoBaseline_EmptyHostname_ReturnsError(t *testing.T) {
	if _, err := AdoptRepoBaseline(context.Background(), &mockSSHDeployer{}, "", "av-1", topology.RuntimeDynamic); err == nil {
		t.Fatal("expected error for empty hostname")
	}
}

func TestAdoptRepoBaseline_RunsOverSSH(t *testing.T) {
	m := &mockSSHDeployer{output: []byte("")}
	provenance, err := AdoptRepoBaseline(context.Background(), m, "appstage", "av-1", topology.RuntimeDynamic)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provenance != topology.RepoProvenanceExisting {
		t.Errorf("provenance = %q, want %q (a non-empty-tree HEAD^{tree} probe result — the mock's static empty-string output isn't the empty-tree sha)", provenance, topology.RepoProvenanceExisting)
	}
	found := false
	for _, c := range m.calls {
		if strings.Contains(c.command, "zcp/baseline/av-1") {
			found = true
		}
	}
	if !found {
		t.Errorf("calls = %+v, want one containing the baseline tag", m.calls)
	}
}

// TestAdoptRepoBaseline_EmptyTreeHEAD_ReturnsSnapshotProvenance proves the
// SSH-side wiring converts git.AdoptCaseSnapshot to
// topology.RepoProvenanceSnapshot — the case docs/spec-workflows.md §8
// GLC-7 describes for a service adopted with no prior git (GLC-1's marker
// commit leaves a HEAD over the empty tree).
func TestAdoptRepoBaseline_EmptyTreeHEAD_ReturnsSnapshotProvenance(t *testing.T) {
	m := &mockSSHDeployer{results: []sshResult{
		{output: []byte("")}, // test -d .git -> is a repo
		{output: []byte("4b825dc642cb6eb9a060e54bf8d69288fbee4904\n")}, // rev-parse HEAD^{tree} -> empty tree
		{output: []byte("")}, // seed exclude
		{output: []byte("")}, // git add -A
		{output: []byte("")}, // commit
		{output: []byte("")}, // tag
	}}
	provenance, err := AdoptRepoBaseline(context.Background(), m, "appdev", "av-2", topology.RuntimeDynamic)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provenance != topology.RepoProvenanceSnapshot {
		t.Errorf("provenance = %q, want %q", provenance, topology.RepoProvenanceSnapshot)
	}
}

func TestReadRepoStatus_HeadResolves_ReturnsPresentWithHeadAndBaseline(t *testing.T) {
	m := &mockSSHDeployer{results: []sshResult{
		{output: []byte("5ba0abc123\n")},         // rev-parse --verify HEAD^{commit}
		{output: []byte("zcp/baseline/av-42\n")}, // tag --points-at HEAD --list
	}}
	got, err := ReadRepoStatus(context.Background(), m, "appdev")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Present {
		t.Error("Present = false, want true")
	}
	if got.Head != "5ba0abc123" {
		t.Errorf("Head = %q, want 5ba0abc123", got.Head)
	}
	if got.Baseline != "av-42" {
		t.Errorf("Baseline = %q, want av-42", got.Baseline)
	}
}

func TestReadRepoStatus_NoRepo_ReturnsNotPresentNoError(t *testing.T) {
	m := &mockSSHDeployer{err: errRepoScaffoldTest}
	got, err := ReadRepoStatus(context.Background(), m, "appdev")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Present {
		t.Error("Present = true, want false (no repo)")
	}
}

func TestLocalRepoHead_RealRepo_ReturnsPresentAndHead(t *testing.T) {
	dir := t.TempDir()
	runGit := func(args ...string) {
		cmd := exec.CommandContext(context.Background(), "git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	runGit("init", "-q", "-b", "main")
	runGit("commit", "-q", "--allow-empty", "-m", "init")

	present, head := LocalRepoHead(context.Background(), dir)
	if !present {
		t.Fatal("present = false, want true")
	}
	if head == "" {
		t.Error("head is empty, want a resolved sha")
	}
}

func TestLocalRepoHead_NotARepo_ReturnsNotPresent(t *testing.T) {
	dir := t.TempDir()
	present, head := LocalRepoHead(context.Background(), dir)
	if present {
		t.Errorf("present = true, want false (not a repo): head=%q", head)
	}
}

var errRepoScaffoldTest = &repoScaffoldTestError{"no such repo"}

type repoScaffoldTestError struct{ msg string }

func (e *repoScaffoldTestError) Error() string { return e.msg }
