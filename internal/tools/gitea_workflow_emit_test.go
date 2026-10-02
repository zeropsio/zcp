// Tests for: A3 — the workflow that ships a Mate's code has to be IN the
// repository the Mate pushes.
//
// `.gitea/workflows/zerops.yml` was only ever emitted as text by
// `zerops_workflow action="build-integration" integration="actions"`, which
// nothing in A1 or A2 calls. So a real Mate's repository never carried it and
// the runner path never ran (measured 2026-09-16). The wiring now writes it
// into the pair's working tree when it wires the repository, before anything
// is pushed.
package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestReconcileHQRepositories_EmitsTheWorkflow pins A3: after the wiring the
// workflow file is IN the tree, and it carries no credential of any kind.
func TestReconcileHQRepositories_EmitsTheWorkflow(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()

	raw, err := os.ReadFile(filepath.Join(lab.pair, giteaWorkflowFilePath))
	if err != nil {
		t.Fatalf("the wiring must leave the workflow in the pair's tree: %v\ncommands:\n%s",
			err, strings.Join(lab.ssh.commands, "\n"))
	}
	body := string(raw)
	for _, want := range []string{
		"on:", "push:", "branches: [main]",
		"actions/checkout@v4",
		"uses: zeropsio/gitea-mate/actions/deploy@v4",
		"workflow_dispatch:",
		"ref: ${{ inputs.sha || github.sha }}",
		"environment: ${{ inputs.environment }}",
		"service: ${{ inputs.service }}",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the workflow is missing %q:\n%s", want, body)
		}
	}
	for _, forbidden := range []string{"secrets.", "ZEROPS_TOKEN", "actions/deploy@v1"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the workflow must carry no %q:\n%s", forbidden, body)
		}
	}
}

// TestReconcileHQRepositories_WorkflowIsIdempotent — the emit is content
// idempotent. A rewrite of identical bytes would show up as a modified file in
// the pair's `git status` and in the deploy's dirty-tree warning, telling the
// Mate it has work to commit that it does not.
func TestReconcileHQRepositories_WorkflowIsIdempotent(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()

	path := filepath.Join(lab.pair, giteaWorkflowFilePath)
	first, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	stale := first.ModTime().Add(-time.Hour)
	if err := os.Chtimes(path, stale, stale); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	var write string
	for _, cmd := range lab.ssh.commands {
		if strings.Contains(cmd, giteaWorkflowFilePath) && !strings.Contains(cmd, "cat ") {
			write = cmd
		}
	}
	if write == "" {
		t.Fatal("the wiring wrote no workflow")
	}
	if out, err := lab.ssh.ExecSSH(context.Background(), "appdev", write); err != nil {
		t.Fatalf("write again: %v\n%s", err, out)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}
	if !after.ModTime().Equal(stale) {
		t.Errorf("an unchanged workflow was rewritten, dirtying the pair's tree")
	}
}
