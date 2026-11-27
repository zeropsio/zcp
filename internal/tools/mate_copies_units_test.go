package tools

import (
	"context"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// witness: internal/tools/mate_copies.go | dev.Branch, dev.BranchReadAt = branch, branchAt.UTC().Format(time.RFC3339Nano) → dev.BranchReadAt = branchAt.UTC().Format(time.RFC3339Nano)
func TestMateCopies_MCClassic_RecordsTheCheckedOutBranch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, branch string }{{"work branch", "feature/card"}, {"detached checkout", ""}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "status.json")
			synctest.Test(t, func(t *testing.T) {
				metas := []*workflow.ServiceMeta{
					{Hostname: "appdev", Stages: []topology.StageRef{{Hostname: "appstage"}}},
					{Hostname: "webpool", Stages: []topology.StageRef{{Hostname: "webstage"}}, DevPool: &workflow.DevPoolRef{}},
				}
				for _, meta := range metas {
					if err := workflow.WriteServiceMeta(dir, meta); err != nil {
						t.Fatal(err)
					}
				}
				if err := reconcileMateDevServices(dir, path); err != nil {
					t.Fatal(err)
				}
				at := time.Date(2026, 11, 27, 7, 0, 0, 0, time.UTC)
				if err := storeClassicLook(path, "appdev", &tc.branch, at, nil, time.Time{}); err != nil {
					t.Fatal(err)
				}
				status, err := mate.ReadStatus(path)
				if err != nil {
					t.Fatal(err)
				}
				if status.Copies == nil || len(status.Copies.DevServices) != 1 {
					t.Fatalf("dev services = %+v, want only appdev", status.Copies)
				}
				if status.Copies.Leases != nil {
					t.Fatal("unread leases must not be an empty observation")
				}
				dev := status.Copies.DevServices[0]
				if dev.Hostname != "appdev" || dev.Branch == nil || *dev.Branch != tc.branch || dev.BranchReadAt != "2026-11-27T07:00:00Z" {
					t.Errorf("dev service = %+v, want its checked-out branch at the read time", dev)
				}
			})
		})
	}
}

type mateCopySwarm struct{ ops.SwarmAPI }

func (mateCopySwarm) Renew(_ context.Context, id, _ string) (platform.SwarmContainer, error) {
	return platform.SwarmContainer{ID: id, Status: platform.SwarmContainerActive, LockState: "reserved"}, nil
}

func (mateCopySwarm) Release(_ context.Context, id, _ string, _ bool) (platform.SwarmContainer, error) {
	return platform.SwarmContainer{ID: id}, nil
}

// non-parallel: the lease change publishes through the process environment.
// witness: internal/ops/lease.go | publishMateLeases(stateDir, "release") →
func TestMateCopies_MCMerge_ReleaseReplacesTheLeaseSet(t *testing.T) {
	for _, tc := range []struct{ name, branch string }{{"merged feature", "feature/card"}, {"merged fix", "fix/login"}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "status.json")
			t.Setenv("ZCP_MATE_ENABLED", "1")
			t.Setenv(mate.EnvStatusFile, path)
			synctest.Test(t, func(t *testing.T) {
				writeHQLeaseRecord(t, dir, "webpool", 1, completionRepo, tc.branch)
				writeHQLeaseRecord(t, dir, "webpool", 2, completionRepo, "other")
				if err := ops.PublishMateLeases(dir, path); err != nil {
					t.Fatal(err)
				}
				client := platform.NewMock().
					WithProject(&platform.Project{ID: "p1"}).
					WithServices([]platform.ServiceStack{{ID: "svc-webpool", Name: "webpool", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1"}}}).
					WithServiceEnv("svc-webpool", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: "test-bearer"}})
				deps := ops.LeaseDeps{Client: client, ProjectID: "p1", StateDir: dir, SSH: &guardSSH{},
					Swarm: func(string, string) ops.SwarmAPI { return mateCopySwarm{} }, Env: func(string) (string, bool) { return "", false }}
				app := "app-1"
				state := hq.MateState{AppID: &app, OpenPerBranch: true, Changes: []hq.MateChange{{Repo: "appdev", Number: 4, State: hq.ChangeMerged, Branch: tc.branch, UpdatedAt: "2026-11-27T07:00:00Z"}}}
				lines := releaseCompletedLeases(t.Context(), dir, completionHQ, &app, state.Changes, completionState(state), func(ctx context.Context, repo, branch string, confirm func([]ops.LeaseTargetInfo) bool) (ops.LeaseChange, error) {
					return ops.LeaseReleaseGroupConfirmed(ctx, deps, repo, branch, confirm)
				})
				if len(lines) != 1 {
					t.Fatalf("completion = %v, want one released change", lines)
				}
				status, err := mate.ReadStatus(path)
				if err != nil {
					t.Fatal(err)
				}
				if status.Copies == nil || len(status.Copies.Leases) != 1 || status.Copies.Leases[0].Alias != "webpool-2" || status.Copies.Leases[0].Branch != "other" {
					t.Errorf("leases after merge = %+v, want only webpool-2 on other", status.Copies)
				}
			})
		})
	}
}
