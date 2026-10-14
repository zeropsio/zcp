package tools

import (
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestChangeReceipt_OnlyForAPairOfSeveralStages(t *testing.T) {
	t.Parallel()
	ws := &workflow.WorkSession{Deploys: map[string][]workflow.DeployAttempt{
		"appstage": {{AttemptedAt: "2026-10-14T21:05:00Z", SucceededAt: "2026-10-14T21:06:00Z", SHA: "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"}},
	}}
	for _, stages := range [][]topology.StageRef{nil, {{Hostname: "appstage", SetupName: "app"}}} {
		if got := changeReceipt(stages, ws, time.Now()); got != nil {
			t.Errorf("changeReceipt(%v) = %+v, want none", stages, got)
		}
	}
}

func TestChangeReceipt_EachPartsPreviewAsItsLastDeployLeftIt(t *testing.T) {
	t.Parallel()
	const commit = "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"
	ws := &workflow.WorkSession{
		Deploys: map[string][]workflow.DeployAttempt{
			// A failed deploy before the one that worked does not count.
			"webstage": {
				{AttemptedAt: "2026-10-14T21:01:00Z", Error: "build failed"},
				{AttemptedAt: "2026-10-14T21:05:00Z", SucceededAt: "2026-10-14T21:06:00Z", SHA: commit},
			},
			// A deploy of uncommitted work names no commit.
			"apistage": {{AttemptedAt: "2026-10-14T21:06:30Z", SucceededAt: "2026-10-14T21:07:00Z", SHA: commit, Dirty: true}},
			"workerstage": {
				{AttemptedAt: "2026-10-14T21:02:00Z", SucceededAt: "2026-10-14T21:03:00Z", SHA: commit},
				{AttemptedAt: "2026-10-14T21:08:00Z", Error: "deploy status BUILD_FAILED"},
			},
		},
		Verifies: map[string][]workflow.VerifyAttempt{
			"webstage": {{AttemptedAt: "2026-10-14T21:06:10Z", Passed: true}},
			// A verify that passed before the last deploy says nothing of it.
			"apistage":    {{AttemptedAt: "2026-10-14T21:04:00Z", Passed: true}},
			"workerstage": {{AttemptedAt: "2026-10-14T21:03:30Z", Passed: true}},
		},
	}
	stages := []topology.StageRef{
		{Hostname: "webstage", SetupName: "web"},
		{Hostname: "apistage", SetupName: "api"},
		{Hostname: "workerstage", SetupName: "worker"},
		{Hostname: "cronstage", SetupName: "cron"},
	}
	got := changeReceipt(stages, ws, time.Date(2026, 10, 14, 21, 10, 0, 0, time.UTC))
	want := &hq.ChangeReceipt{At: "2026-10-14T21:10:00Z", Parts: []hq.ChangeReceiptPart{
		{Setup: "web", PreviewHostname: "webstage", Deploy: "ok", DeployedAt: "2026-10-14T21:06:00Z", WorkspaceClean: true, DeployedCommit: commit, Verified: true},
		{Setup: "api", PreviewHostname: "apistage", Deploy: "ok", DeployedAt: "2026-10-14T21:07:00Z"},
		{Setup: "worker", PreviewHostname: "workerstage", Deploy: "failed", DeployedAt: "2026-10-14T21:08:00Z", WorkspaceClean: true},
		{Setup: "cron", PreviewHostname: "cronstage", Deploy: "none"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("changeReceipt =\n%+v\nwant\n%+v", got, want)
	}
}

// TestAChangesReceiptListsEachPartsPreviewAsFromACommitFromAnUncommittedTreeFailedOrNotDeployed:
// a stage deploy of a pair with several parts delivers its change, and the
// receipt HQ keeps on that change says of each part's preview what its last
// deploy in the work session left: from a commit, from an uncommitted tree,
// failed, or not deployed — one row per part, in the pair's order.
// Non-parallel: the lab redirects HOME and writes the enrollment there.
func TestAChangesReceiptListsEachPartsPreviewAsFromACommitFromAnUncommittedTreeFailedOrNotDeployed(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()
	startSession(t, lab.stateDir, "Split the app into parts")
	lab.write(map[string]string{"web/index.js": "the web\n", "api/main.go": "package main\n"})
	lab.git("add", "-A")
	lab.git("-c", "user.name=zcp", "-c", "user.email=zcp@example.invalid", "commit", "-q", "-m", "Split the app into parts")
	commit := lab.git("rev-parse", "HEAD")

	rows := []struct {
		name     string
		stage    topology.StageRef
		attempts []workflow.DeployAttempt
		want     hq.ChangeReceiptPart
	}{
		{
			name:     "from a commit",
			stage:    topology.StageRef{Hostname: "webstage", SetupName: "web"},
			attempts: []workflow.DeployAttempt{{AttemptedAt: "2026-10-15T01:00:00Z", SucceededAt: "2026-10-15T01:01:00Z", SHA: commit}},
			want:     hq.ChangeReceiptPart{Setup: "web", PreviewHostname: "webstage", Deploy: "ok", DeployedAt: "2026-10-15T01:01:00Z", WorkspaceClean: true, DeployedCommit: commit},
		},
		{
			name:     "from an uncommitted tree",
			stage:    topology.StageRef{Hostname: "apistage", SetupName: "api"},
			attempts: []workflow.DeployAttempt{{AttemptedAt: "2026-10-15T01:02:00Z", SucceededAt: "2026-10-15T01:03:00Z", SHA: commit, Dirty: true}},
			want:     hq.ChangeReceiptPart{Setup: "api", PreviewHostname: "apistage", Deploy: "ok", DeployedAt: "2026-10-15T01:03:00Z"},
		},
		{
			name:     "failed",
			stage:    topology.StageRef{Hostname: "workerstage", SetupName: "worker"},
			attempts: []workflow.DeployAttempt{{AttemptedAt: "2026-10-15T01:04:00Z", SHA: commit, Error: "deploy status BUILD_FAILED"}},
			want:     hq.ChangeReceiptPart{Setup: "worker", PreviewHostname: "workerstage", Deploy: "failed", DeployedAt: "2026-10-15T01:04:00Z", WorkspaceClean: true},
		},
		{
			name:  "not deployed",
			stage: topology.StageRef{Hostname: "cronstage", SetupName: "cron"},
			want:  hq.ChangeReceiptPart{Setup: "cron", PreviewHostname: "cronstage", Deploy: "none"},
		},
	}
	stages := make([]topology.StageRef, 0, len(rows))
	for _, row := range rows {
		stages = append(stages, row.stage)
	}
	if err := workflow.UpdateServiceMeta(lab.stateDir, "appdev", func(m *workflow.ServiceMeta) error {
		m.Stages = stages
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		for _, attempt := range row.attempts {
			if err := workflow.RecordDeployAttempt(lab.stateDir, row.stage.Hostname, attempt); err != nil {
				t.Fatalf("record %s: %v", row.stage.Hostname, err)
			}
		}
	}

	delivery := deliverHQPair(t.Context(), lab.mock, lab.hq.srv.Client(), lab.ssh, lab.rt, lab.stateDir, "webstage")
	if delivery == nil || delivery.Change == nil {
		t.Fatalf("want a delivery through a change, got %+v", delivery)
	}
	receipt := lab.hq.receipt(delivery.Change.Number)
	if receipt == nil || len(receipt.Parts) != len(rows) {
		t.Fatalf("receipt on change #%d = %+v, want one row per part", delivery.Change.Number, receipt)
	}
	for i, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			if got := receipt.Parts[i]; got != row.want {
				t.Errorf("part %d =\n%+v\nwant\n%+v", i, got, row.want)
			}
		})
	}
}

// TestAReceiptNamesACommitOnlyForADeployMadeWithACleanWorkspace: a preview's
// row in the receipt carries the dev HEAD only when the zerops_deploy that
// built it shipped exactly that commit — from a clean working tree, or a
// commit named by sha. A deploy that carried uncommitted changes, or one that
// failed, names none.
// Non-parallel: the work session is this process's, as the agent's is.
func TestAReceiptNamesACommitOnlyForADeployMadeWithACleanWorkspace(t *testing.T) {
	const head = "c0ffee0123456789abcdef0123456789abcdef01"
	tests := []struct {
		name       string
		ssh        *stubSSHSHA
		sha        string
		status     string
		wantDeploy string
		wantCommit string
	}{
		{name: "a clean working tree", ssh: &stubSSHSHA{headSHA: head}, status: statusActive, wantDeploy: "ok", wantCommit: head},
		{name: "an uncommitted tree", ssh: &stubSSHSHA{headSHA: head, headDirty: true}, status: statusActive, wantDeploy: "ok"},
		{name: "a commit named by sha", ssh: &stubSSHSHA{sha: head, tmp: "/tmp/zcp-extract-1", headSHA: head, headDirty: true}, sha: head[:7], status: statusActive, wantDeploy: "ok", wantCommit: head},
		{name: "a failed deploy from a clean tree", ssh: &stubSSHSHA{headSHA: head}, status: "BUILD_FAILED", wantDeploy: "failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateDir := t.TempDir()
			stages := []topology.StageRef{{Hostname: "webstage", SetupName: "web"}, {Hostname: "apistage", SetupName: "api"}}
			if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
				Hostname: "appdev", Mode: topology.PlanModeStandard, Stages: stages,
				BootstrapSession: "s1", BootstrappedAt: "2026-10-15T00:00:00Z",
			}); err != nil {
				t.Fatal(err)
			}
			startSession(t, stateDir, "Split the app into parts")
			mock := platform.NewMock().
				WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "appdev"}, {ID: "svc-2", Name: "webstage"}, {ID: "svc-3", Name: "apistage"}}).
				WithAppVersionEvents([]platform.AppVersionEvent{{ID: "av-1", ProjectID: "proj-1", ServiceStackID: "svc-2", Status: tt.status, Sequence: 1}})
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterDeploySSH(srv, mock, okHTTP, "proj-1", tt.ssh, &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"},
				nil, runtime.Info{}, stateDir, testDeployEngine(t), nil)

			args := map[string]any{"sourceService": "appdev", "targetService": "webstage", "setup": "web"}
			if tt.sha != "" {
				args["sha"] = tt.sha
			}
			callTool(t, srv, "zerops_deploy", args)

			ws, err := workflow.CurrentWorkSession(stateDir)
			if err != nil {
				t.Fatal(err)
			}
			receipt := changeReceipt(stages, ws, time.Now())
			if receipt == nil {
				t.Fatal("a pair of two parts carries a receipt")
			}
			web := receipt.Parts[0]
			if web.Deploy != tt.wantDeploy || web.DeployedCommit != tt.wantCommit {
				t.Errorf("webstage's row = deploy %q, commit %q; want deploy %q, commit %q (%+v)", web.Deploy, web.DeployedCommit, tt.wantDeploy, tt.wantCommit, web)
			}
		})
	}
}

func TestChangeReceipt_APreviewStillBuildingWhenThePollTimesOutKeepsItsCommitOnceItFinishes(t *testing.T) {
	t.Parallel()
	const commit = "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"
	stages := []topology.StageRef{{Hostname: "webstage", SetupName: "web"}, {Hostname: "apistage", SetupName: "api"}}
	rows := []struct {
		name   string
		settle bool
		dirty  bool
		want   hq.ChangeReceiptPart
	}{
		{name: "while it builds", want: hq.ChangeReceiptPart{Setup: "web", PreviewHostname: "webstage", Deploy: "none"}},
		{name: "once it finishes from a clean tree", settle: true, want: hq.ChangeReceiptPart{Setup: "web", PreviewHostname: "webstage", Deploy: "ok", DeployedAt: "2026-10-15T01:05:00Z", WorkspaceClean: true, DeployedCommit: commit}},
		{name: "once it finishes from a dirty tree", settle: true, dirty: true, want: hq.ChangeReceiptPart{Setup: "web", PreviewHostname: "webstage", Deploy: "ok", DeployedAt: "2026-10-15T01:05:00Z"}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := workflow.SaveWorkSession(dir, workflow.NewWorkSession(labMate, string(workflow.EnvContainer), "Split the app into parts", []string{"webstage"})); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = workflow.DeleteWorkSession(dir, os.Getpid()) })
			inFlight := workflow.DeployAttempt{AttemptedAt: "2026-10-15T01:00:00Z", Error: workflow.DeployBuildInFlightError, SHA: commit, Dirty: row.dirty}
			if err := workflow.RecordDeployAttempt(dir, "webstage", inFlight); err != nil {
				t.Fatal(err)
			}
			if row.settle {
				if err := workflow.RecordDeployAttempt(dir, "webstage", workflow.DeployAttempt{AttemptedAt: "2026-10-15T01:05:00Z", SucceededAt: "2026-10-15T01:05:00Z", Strategy: "record-deploy"}); err != nil {
					t.Fatal(err)
				}
			}
			ws, err := workflow.CurrentWorkSession(dir)
			if err != nil || ws == nil {
				t.Fatalf("session: %v %v", ws, err)
			}
			got := changeReceipt(stages, ws, time.Now()).Parts[0]
			if got != row.want {
				t.Errorf("part =\n%+v\nwant\n%+v", got, row.want)
			}
		})
	}
}
