// Tests for: A3 — the workflow that ships a Mate's code to the group's stage
// has to be IN the repository the Mate pushes.
//
// `.gitea/workflows/zerops.yml` was only ever emitted as text by
// `zerops_workflow action="build-integration" integration="actions"`, which
// nothing in A1 or A2 calls. So a real Mate's repository never carried it and
// the runner path never ran (measured 2026-09-16). A1 now writes it into the
// pair's working tree when it wires the repository, before anything is
// pushed.
package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// runContainerCommandsIn replays the commands an SSH stub recorded against a
// real directory, standing in for the pair's working tree: the container's
// /var/www is rewritten to dir so the file the command writes can be read
// back. Commands that are not writes are skipped — this is about what landed
// in the tree, not about re-running git.
func runContainerCommandsIn(t *testing.T, dir string, commands []string) {
	t.Helper()
	for _, cmd := range commands {
		if !strings.Contains(cmd, giteaWorkflowFilePath) {
			continue
		}
		runShellIn(t, strings.Replace(cmd, "cd '"+giteaPairWorkingDir+"'", "cd '"+dir+"'", 1))
	}
}

func runShellIn(t *testing.T, command string) {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "sh", "-c", command).CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\ncommand: %s\noutput:\n%s", err, command, out)
	}
}

// TestReconcileGiteaRepositories_EmitsTheWorkflow pins A3: after A1 wires the
// repository the workflow file is IN the tree, and it carries no credential
// of any kind — the job asks the account's broker with its own token, which
// is the entire reason this path exists.
func TestReconcileGiteaRepositories_EmitsTheWorkflow(t *testing.T) {
	stateDir := t.TempDir()
	writeGiteaPairMeta(t, stateDir)
	fake := newFakeGitea()
	srv := fake.start(t)
	ssh := giteaReconcileSSH()
	client := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})

	reconcileGiteaRepositories(
		context.Background(), client, srv.Client(), ssh,
		runtime.Info{InContainer: true, ProjectID: "p1"}, stateDir,
		writeLiveEnvFile(t, map[string]string{
			"GITEA_URL": srv.URL, "MATE_BROKER_URL": srv.URL, "GITEA_TOKEN": giteaBotToken,
		}),
	)

	tree := t.TempDir()
	runContainerCommandsIn(t, tree, ssh.commands)
	raw, err := os.ReadFile(filepath.Join(tree, giteaWorkflowFilePath))
	if err != nil {
		t.Fatalf("A1 must leave the workflow in the pair's tree: %v\ncommands:\n%s",
			err, strings.Join(ssh.commands, "\n"))
	}
	body := string(raw)

	for _, want := range []string{
		"on:", "push:", "branches: [main]",
		"actions/checkout@v4",
		"uses: zeropsio/gitea-mate/actions/deploy@v1",
		"environment: stage",
		"service: appdev",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the workflow is missing %q:\n%s", want, body)
		}
	}
	// The whole point of the Gitea track: no repository secret, no Zerops
	// token, no CLI that would need one.
	for _, forbidden := range []string{"secrets.", "ZEROPS_TOKEN", "zcli"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the workflow must carry no %q:\n%s", forbidden, body)
		}
	}
}

// TestReconcileGiteaRepositories_WorkflowIsIdempotent — the emit is content
// idempotent. A rewrite of identical bytes would show up as a modified file
// in the pair's `git status` and in the deploy's dirty-tree warning, telling
// the Mate it has work to commit that it does not.
func TestReconcileGiteaRepositories_WorkflowIsIdempotent(t *testing.T) {
	stateDir := t.TempDir()
	writeGiteaPairMeta(t, stateDir)
	fake := newFakeGitea()
	srv := fake.start(t)
	ssh := giteaReconcileSSH()
	client := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})

	reconcileGiteaRepositories(
		context.Background(), client, srv.Client(), ssh,
		runtime.Info{InContainer: true, ProjectID: "p1"}, stateDir,
		writeLiveEnvFile(t, map[string]string{
			"GITEA_URL": srv.URL, "MATE_BROKER_URL": srv.URL, "GITEA_TOKEN": giteaBotToken,
		}),
	)

	tree := t.TempDir()
	runContainerCommandsIn(t, tree, ssh.commands)
	path := filepath.Join(tree, giteaWorkflowFilePath)
	first, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	stale := first.ModTime().Add(-time.Hour)
	if err := os.Chtimes(path, stale, stale); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	runContainerCommandsIn(t, tree, ssh.commands)
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}
	if !after.ModTime().Equal(stale) {
		t.Errorf("an unchanged workflow was rewritten, dirtying the pair's tree")
	}
}
