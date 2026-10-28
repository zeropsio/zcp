package tools

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestGitPushBuildWatch_Decisions(t *testing.T) {
	// non-parallel: work-session persistence is keyed by the process PID.
	failedAt := "2026-10-28T12:00:00Z"
	for _, tc := range []struct {
		name    string
		status  string
		build   *platform.BuildInfo
		old     bool
		failure bool
	}{
		{name: "Decision: An integration build observed as BUILD_FAILED ends its watch immediately and reports that observed failure.", status: "BUILD_FAILED", build: &platform.BuildInfo{}, failure: true},
		{name: "Decision: A push whose build is not yet observed remains landed and unconfirmed when observation ends; it must not become a failed build.", status: "BUILD_FAILED", old: true},
		{name: "Decision: A pre-push appVersion must not establish this push's deployment outcome.", status: "ACTIVE", old: true},
		{name: "pipeline failure is authoritative even with ACTIVE status", status: "ACTIVE", build: &platform.BuildInfo{PipelineFailed: &failedAt}, failure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stateDir := t.TempDir()
			if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
				Hostname: "appdev", Mode: topology.PlanModeStandard,
				Stages:       []topology.StageRef{{Hostname: "appstage", SetupName: "prod"}},
				GitPushState: topology.GitPushConfigured, BuildIntegration: topology.BuildIntegrationActions,
				BootstrapSession: "test", BootstrappedAt: "2026-10-14", FirstDeployedAt: "2026-10-14",
			}); err != nil {
				t.Fatal(err)
			}
			ws := workflow.NewWorkSession("proj-1", string(workflow.EnvContainer), "ship it", []string{"appstage"})
			if err := workflow.SaveWorkSession(stateDir, ws); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = workflow.DeleteWorkSession(stateDir, os.Getpid()) })
			synctest.Test(t, func(t *testing.T) {
				pushedAt := time.Now()
				created := pushedAt
				if tc.old {
					created = created.Add(-time.Second)
				}
				client := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-stage", Name: "appstage"}}).
					WithAppVersionEvents([]platform.AppVersionEvent{{ID: "ours", ServiceStackID: "svc-stage", Status: tc.status, Created: created.Format(time.RFC3339Nano), Build: tc.build}}).
					WithLogAccess(&platform.LogAccess{})
				fetcher := platform.NewMockLogFetcher().WithEntries([]platform.LogEntry{
					{ID: "log-ours", Timestamp: created.Format(time.RFC3339Nano), Facility: "local0", Tag: "zbuilder@ours", Message: "/bin/sh: 1: thisbinaryisnotreal_xy42: not found"},
					{ID: "log-other", Timestamp: created.Format(time.RFC3339Nano), Facility: "local0", Tag: "zbuilder@other", Message: "unrelated build"},
				})
				result := &ops.GitPushResult{Status: "PUSHED"}
				progress := 0
				finishGitPushWithBuildWatch(context.Background(), client, "proj-1", fetcher, func(string, float64, float64) { progress++ },
					DeploySSHInput{TargetService: "appdev", Strategy: "git-push"}, stateDir, "zerops.yaml", pushedAt, result)
				loaded, err := workflow.LoadWorkSession(stateDir, os.Getpid())
				if err != nil {
					t.Fatal(err)
				}
				if result.Status != "PUSHED" || result.AutoRecorded || result.VerifyTarget != "" || result.BuildTarget != "appstage" {
					t.Fatalf("ASSERTION: push remains landed without claiming deployment success; got %+v", result)
				}
				if tc.failure {
					if !result.BuildObserved || result.BuildStatus != tc.status || result.FailureClassification == nil || result.FailureClassification.Category != topology.FailureClassBuild ||
						!slices.Equal(result.BuildLogs, []string{"/bin/sh: 1: thisbinaryisnotreal_xy42: not found"}) || !strings.Contains(result.NextActions, "finished "+tc.status) ||
						time.Since(pushedAt) != 0 || progress != 0 {
						t.Fatalf("ASSERTION: observed failure must reach Git-push classification and exact build logs immediately; got result=%+v progress=%d elapsed=%s", result, progress, time.Since(pushedAt))
					}
					attempts := loaded.Deploys["appstage"]
					if len(attempts) != 1 || attempts[0].Error != "integration build "+tc.status || attempts[0].SucceededAt != "" || attempts[0].FailureClass != topology.FailureClassBuild {
						t.Fatalf("ASSERTION: recorded outcome must be this observed build failure; got %+v", attempts)
					}
				} else if result.BuildObserved || result.BuildStatus != "NOT_OBSERVED" || result.FailureClassification != nil || len(result.BuildLogs) != 0 || len(loaded.Deploys["appstage"]) != 0 || !strings.Contains(result.NextActions, "Push landed but no integration build appeared") {
					t.Fatalf("ASSERTION: observation expiry must remain landed and unconfirmed with no failed build; got %+v attempts=%+v", result, loaded.Deploys)
				}
			})
		})
	}
}
