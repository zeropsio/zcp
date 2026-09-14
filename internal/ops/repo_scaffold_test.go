// Tests for: ops/repo_scaffold.go — the SSH-side wiring of ops/git's
// repo-always primitives (docs/spec-workflows.md §4.10).
package ops

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

func TestEnsureScaffoldRepo_EmptyHostname_ReturnsError(t *testing.T) {
	if err := EnsureScaffoldRepo(context.Background(), &mockSSHDeployer{}, "", topology.RuntimeDynamic); err == nil {
		t.Fatal("expected error for empty hostname")
	}
}

func TestEnsureScaffoldRepo_RunsOverSSHAgainstVarWww(t *testing.T) {
	m := &mockSSHDeployer{output: []byte("")}
	if err := EnsureScaffoldRepo(context.Background(), m, "appdev", topology.RuntimeDynamic); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.calls) == 0 {
		t.Fatal("expected at least one SSH call")
	}
	for _, c := range m.calls {
		if c.hostname != "appdev" {
			t.Errorf("call hostname = %q, want appdev", c.hostname)
		}
		if !strings.Contains(c.command, "cd '/var/www'") {
			t.Errorf("command = %q, want it to cd into /var/www", c.command)
		}
	}
}

func TestAdoptRepoBaseline_EmptyHostname_ReturnsError(t *testing.T) {
	if _, err := AdoptRepoBaseline(context.Background(), &mockSSHDeployer{}, "", "av-1", topology.RuntimeDynamic); err == nil {
		t.Fatal("expected error for empty hostname")
	}
}

func TestAdoptRepoBaseline_RunsOverSSH(t *testing.T) {
	m := &mockSSHDeployer{output: []byte("")}
	if _, err := AdoptRepoBaseline(context.Background(), m, "appstage", "av-1", topology.RuntimeDynamic); err != nil {
		t.Fatalf("unexpected error: %v", err)
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

func TestReadRepoStatus_HeadResolves_ReturnsPresentWithHeadAndBaseline(t *testing.T) {
	m := &mockSSHDeployer{results: []sshResult{
		{output: []byte("sha-abc123\n")},         // rev-parse --verify HEAD^{commit}
		{output: []byte("zcp/baseline/av-42\n")}, // tag --points-at HEAD --list
	}}
	got, err := ReadRepoStatus(context.Background(), m, "appdev")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Present {
		t.Error("Present = false, want true")
	}
	if got.Head != "sha-abc123" {
		t.Errorf("Head = %q, want sha-abc123", got.Head)
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

var errRepoScaffoldTest = &repoScaffoldTestError{"no such repo"}

type repoScaffoldTestError struct{ msg string }

func (e *repoScaffoldTestError) Error() string { return e.msg }
