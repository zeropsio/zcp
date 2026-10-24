// Tests for: tools/deploy_ssh.go + tools/deploy_batch.go — a stage tracks
// main: zerops_deploy and zerops_deploy_batch from a lease to the part's stage
// fetch origin/main first, never refuse the commit they ship, and say so when
// it is not on main (docs/spec-workflows.md §8 DP-8).
package tools

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// mainCheckSSH is a leased container whose checkout is on feat-x at a commit
// that is not on origin/main.
type mainCheckSSH struct {
	mu    sync.Mutex
	calls []leaseSSHCall
}

const (
	mainCheckHead = "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"
	mainCheckMain = "9f8e7d6c5b4a39281706f5e4d3c2b1a098765432"
)

func (s *mainCheckSSH) ExecSSH(_ context.Context, host, command string) ([]byte, error) {
	s.mu.Lock()
	s.calls = append(s.calls, leaseSSHCall{host: host, command: command})
	s.mu.Unlock()
	switch {
	case strings.Contains(command, "merge-base --is-ancestor"):
		return []byte("ZCP:MAIN:off " + mainCheckMain + "\n"), nil
	case strings.Contains(command, "rev-parse --verify HEAD") && strings.Contains(command, "git status --porcelain"):
		return []byte(mainCheckHead + "\nZCP:BRANCH:feat-x\n"), nil
	}
	return []byte("ok"), nil
}

// ExecSSHStdin answers one command as ExecSSH does; the input is not read.
func (s *mainCheckSSH) ExecSSHStdin(ctx context.Context, host, command string, _ []byte) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

func (s *mainCheckSSH) ExecSSHBackground(ctx context.Context, host, command string, _ time.Duration) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

func (s *mainCheckSSH) ran(sub string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.calls {
		if strings.Contains(c.command, sub) {
			return true
		}
	}
	return false
}

// stageTracksMainNote is the result's stage-tracks-main warning, "" when it
// has none.
func stageTracksMainNote(r ops.DeployResult) string {
	for _, w := range r.Warnings {
		if strings.Contains(w, "Stage tracks main") {
			return w
		}
	}
	return ""
}

func TestDeployTool_LeaseToStage_StageTracksMain(t *testing.T) {
	t.Parallel()

	wantNote := []string{
		"Stage tracks main", "ships feat-x a1b2c3d", "not on origin/main (at 9f8e7d6)",
		`zerops_dev_server action=status hostname="webpool-3"`, "previewUrl",
		`zerops_deploy sourceService="webpool-3" targetService="appstage" sha="origin/main"`,
	}
	tests := []struct {
		name string
		// failedStage: the stage's last build failed and it waits for a
		// deploy — a corrective deploy from the lease must still run.
		failedStage bool
		batch       bool
	}{
		{name: "single deploy"},
		{name: "single deploy to a failed stage", failedStage: true},
		{name: "batch entry", batch: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rig := newLeaseDeployRig(t)
			if tt.failedStage {
				rig.mock.WithServices([]platform.ServiceStack{
					{ID: "svc-stage", Name: "appstage", Status: "READY_TO_DEPLOY", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
					{ID: "svc-pool", Name: "webpool", Status: serviceStatusRunning, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1"}},
				})
			}
			ssh := &mainCheckSSH{}

			var parsed ops.DeployResult
			if tt.batch {
				result, batch, text := runPoolBatch(t, rig.mock, ssh, rig.stateDir, []map[string]any{
					{"sourceService": rig.alias, "targetService": "appstage"},
				})
				if result.IsError {
					t.Fatalf("a batch from a lease to the stage was refused: %s", text)
				}
				parsed = batchEntryResult(t, batch, rig.alias, "appstage")
			} else {
				result, text := rig.deploy(t, ssh, map[string]any{"sourceService": rig.alias, "targetService": "appstage"})
				if result.IsError {
					t.Fatalf("a deploy from a lease to the stage is never refused for its commit: %s", text)
				}
				if err := json.Unmarshal([]byte(text), &parsed); err != nil {
					t.Fatalf("parse result: %v", err)
				}
			}

			if !ssh.ran("zcli push") {
				t.Fatal("the push did not run")
			}
			if !ssh.ran("git fetch") {
				t.Error("origin/main was not fetched before the deploy")
			}
			if parsed.Status != statusDeployed {
				t.Errorf("status = %q, want %s", parsed.Status, statusDeployed)
			}
			note := stageTracksMainNote(parsed)
			if note == "" {
				t.Fatalf("no stage note in warnings %q", parsed.Warnings)
			}
			for _, want := range wantNote {
				if !strings.Contains(note, want) {
					t.Errorf("note %q lacks %q", note, want)
				}
			}
		})
	}
}

// TestDeployTool_LeaseGitPushRefusal_SteersStageToMain: the git-push refusal
// from a lease no longer sends the branch to the stage; it points at the
// lease's preview URL for review and at the call that puts main on the stage.
func TestDeployTool_LeaseGitPushRefusal_SteersStageToMain(t *testing.T) {
	t.Parallel()

	rig := newLeaseDeployRig(t)
	ssh := &leaseSSH{}
	result, text := rig.deploy(t, ssh, map[string]any{"sourceService": rig.alias, "targetService": "appstage", "strategy": "git-push"})
	if !result.IsError {
		t.Fatalf("a git-push from a lease ran: %s", text)
	}
	for _, want := range []string{"git push", "preview URL", "stage tracks main", `sha=\"origin/main\"`} {
		if !strings.Contains(text, want) {
			t.Errorf("refusal %s lacks %q", text, want)
		}
	}
	if strings.Contains(text, "to run the code on a stage") {
		t.Errorf("refusal %s still sends the branch to the stage", text)
	}
	if len(ssh.hosts()) != 0 {
		t.Errorf("ssh calls ran before the refusal: %v", ssh.hosts())
	}
}
