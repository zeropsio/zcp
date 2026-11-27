package tools

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/devservertest"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// witness: internal/tools/mate_copies.go | case _, ok := <-watcher.Errors: → case _, ok := <-watcher.Events:
func TestMateCopies_MCWatchLoss_RefreshesCreatedAndRemovedServices(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		present bool
		before  []mate.DevCopyStatus
		want    []string
	}{
		{name: "created", present: true, want: []string{"appdev"}},
		{name: "removed", before: []mate.DevCopyStatus{{Hostname: "appdev"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				dir := t.TempDir()
				path := filepath.Join(dir, "status.json")
				if tc.present {
					if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{Hostname: "appdev", Stages: []topology.StageRef{{Hostname: "appstage"}}}); err != nil {
						t.Fatal(err)
					}
				}
				if err := mate.UpdateStatus(path, func(s *mate.Status) { s.Copies = &mate.CopyStatus{DevServices: tc.before} }); err != nil {
					t.Fatal(err)
				}
				ssh := &leaseStubSSH{fn: func(string) ([]byte, error) { return nil, errors.New("link unavailable") }}
				o := &mateCopyObserver{stateDir: dir, statusPath: path, log: func(string) {}, ssh: ssh,
					deps: func(context.Context) (devGitDeps, bool) { return devGitDeps{}, false },
					done: make(chan mateCopyWaited, 1), scanDone: make(chan struct{}, 1),
					active: map[mateCopyWaitKey]mateCopyActive{}, seen: map[mateCopyWaitKey]string{}}
				watcher := &fsnotify.Watcher{Events: make(chan fsnotify.Event), Errors: make(chan error, 1)}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				go o.run(ctx, watcher, make(chan struct{}))
				watcher.Errors <- errors.New("events lost")
				synctest.Wait()
				status, err := mate.ReadStatus(path)
				if err != nil {
					t.Fatal(err)
				}
				hosts := make([]string, 0, len(status.Copies.DevServices))
				for _, dev := range status.Copies.DevServices {
					hosts = append(hosts, dev.Hostname)
				}
				if !slices.Equal(hosts, tc.want) {
					t.Fatalf("dev services = %v, want %v", hosts, tc.want)
				}
			})
		})
	}
}

// witness: internal/tools/mate_copies.go | dev.State, _ = mate.NextCopyState(dev.State, mate.CopyEvent{Kind: mate.CopyLinkLost}) → dev.State, _ = mate.NextCopyState(dev.State, mate.CopyEvent{Kind: mate.CopyStop})
func TestMateCopies_MCBoot_ClassicCopiesStartUnknown(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 11, 27, 7, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name     string
		previous bool
		at       time.Time
		want     mate.CopyState
	}{{"new", false, time.Time{}, mate.CopyUnknown}, {"existing", true, base, mate.CopyUnknown}, {"newer report", true, base.Add(2 * time.Minute), mate.CopyRunning}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "status.json")
			if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{Hostname: "appdev", Stages: []topology.StageRef{{Hostname: "appstage"}}}); err != nil {
				t.Fatal(err)
			}
			if tc.previous {
				if err := mate.UpdateStatus(path, func(s *mate.Status) {
					s.Copies = &mate.CopyStatus{DevServices: []mate.DevCopyStatus{{Hostname: "appdev", State: mate.CopyRunning, StateAt: tc.at.Format(time.RFC3339Nano)}}}
				}); err != nil {
					t.Fatal(err)
				}
			}
			if err := reconcileMateDevServicesAtBoot(dir, path, base.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			status, err := mate.ReadStatus(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := status.Copies.DevServices[0].State; got != tc.want {
				t.Fatalf("boot copy = %s, want %s", got, tc.want)
			}
		})
	}
}

// witness: internal/tools/mate_copies.go | o.startWait(ctx, result.key, result.target, d) → _ = d
func TestMateCopies_MCWrite_WaitsForTheNextEventAfterAWriteFailure(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ host string }{{"appdev"}, {"anotherdev"}} {
		t.Run(tc.host, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "status.json")
				const at = "2026-11-27T07:00:00Z"
				if err := mate.UpdateStatus(path, func(s *mate.Status) {
					s.Copies = &mate.CopyStatus{DevServices: []mate.DevCopyStatus{{Hostname: tc.host, State: mate.CopyStopped, StateAt: at}}}
				}); err != nil {
					t.Fatal(err)
				}
				started, release := make(chan struct{}, 2), make(chan struct{})
				ssh := &leaseStubSSH{fn: func(string) ([]byte, error) { started <- struct{}{}; <-release; return nil, errors.New("closed") }}
				key := mateCopyWaitKey{host: tc.host}
				o := &mateCopyObserver{statusPath: path, log: func(string) {}, done: make(chan mateCopyWaited, 2),
					active: map[mateCopyWaitKey]mateCopyActive{key: {at: at, cancel: func() {}}}, seen: map[mateCopyWaitKey]string{key: at},
					deps: func(context.Context) (devGitDeps, bool) { return devGitDeps{ssh: ssh}, true }}
				ctx, cancel := context.WithCancel(t.Context())
				defer close(release)
				defer cancel()
				o.finishWait(ctx, mateCopyWaited{key: key, target: ops.MateCopyTarget{Hostname: tc.host, State: mate.CopyStarting, StateAt: at}})
				synctest.Wait()
				if len(o.active) != 1 || len(started) != 1 {
					t.Fatalf("next wait: active=%d started=%d, want one", len(o.active), len(started))
				}
			})
		})
	}
}

// witness: internal/tools/mate_copies.go | if active.at != target.StateAt { → if true {
func TestMateCopies_MCWait_UnchangedStateKeepsOneWait(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ host string }{{"appdev"}, {"anotherdev"}} {
		t.Run(tc.host, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "status.json")
				if err := mate.UpdateStatus(path, func(s *mate.Status) {
					s.Copies = &mate.CopyStatus{DevServices: []mate.DevCopyStatus{{Hostname: tc.host, State: mate.CopyRunning, StateAt: "2026-11-27T07:00:00Z"}}}
				}); err != nil {
					t.Fatal(err)
				}
				started, release := make(chan struct{}, 2), make(chan struct{})
				ssh := &leaseStubSSH{fn: func(string) ([]byte, error) { started <- struct{}{}; <-release; return nil, errors.New("closed") }}
				o := &mateCopyObserver{statusPath: path, log: func(string) {}, done: make(chan mateCopyWaited, 2),
					active: map[mateCopyWaitKey]mateCopyActive{}, seen: map[mateCopyWaitKey]string{},
					deps: func(context.Context) (devGitDeps, bool) { return devGitDeps{ssh: ssh}, true }}
				ctx, cancel := context.WithCancel(t.Context())
				defer close(release)
				defer cancel()
				o.observe(ctx)
				synctest.Wait()
				if len(o.active) != 1 || len(started) != 1 {
					t.Fatalf("active = %d, started = %d, want one", len(o.active), len(started))
				}
				key := mateCopyWaitKey{host: tc.host}
				a := o.active[key]
				originalCancel := a.cancel
				cancelled := false
				a.cancel = func() { cancelled = true; originalCancel() }
				o.active[key] = a
				o.observe(ctx)
				synctest.Wait()
				if len(o.active) != 1 || len(started) != 1 || cancelled {
					t.Fatalf("unchanged copy replaced its wait: active=%d started=%d cancelled=%v", len(o.active), len(started), cancelled)
				}
			})
		})
	}
}

// non-parallel: the preview reporting entry point reads the process environment.
// witness: internal/tools/mate_copies.go | reportPreviewCopy(ctx, d, req, lease, host, ds) →
func TestMateCopies_MCPreviewState_PublishesThePreviewReport(t *testing.T) {
	for _, tc := range []struct {
		name   string
		legacy bool
		want   mate.CopyState
	}{{"supported", false, mate.CopyRunning}, {"older supervisor", true, mate.CopyUnknown}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "status.json")
			t.Setenv("ZCP_MATE_ENABLED", "1")
			t.Setenv(mate.EnvStatusFile, path)
			synctest.Test(t, func(t *testing.T) {
				writeDevpoolLeaseFile(t, dir)
				if err := ops.PublishMateLeases(dir, path); err != nil {
					t.Fatal(err)
				}
				if err := ops.MarkMateCopiesUnknown(path, time.Now().Add(time.Second)); err != nil {
					t.Fatal(err)
				}
				pool := platform.ServiceStack{ID: "svc-pool", Name: "devpool", ProjectID: "proj-1", Status: "ACTIVE", SubdomainAccess: true,
					Ports: []platform.Port{{Port: 9003, Protocol: "tcp", HTTPSupport: true}}, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1"}}
				client := platform.NewMock().WithServices([]platform.ServiceStack{pool}).WithService(&pool).
					WithProject(&platform.Project{ID: "proj-1", SubdomainHost: "abc1.prg1.zerops.app"}).
					WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: "test-bearer"}})
				c := devservertest.NewSupervised(`{"version":1,"previewPortBase":9000,"servers":[{"name":"web","port":3000,"command":"run web","preview":true}]}`)
				c.StableNumber = 3
				if tc.legacy {
					c.Capabilities = nil
				}
				d := ops.LeaseDeps{Client: client, ProjectID: "proj-1", StateDir: dir, SSH: c,
					Swarm: func(string, string) ops.SwarmAPI { return mateCopySwarm{} }, Env: func(string) (string, bool) { return "", false },
					Now: func() time.Time { return time.Now().Add(2 * time.Second) }}
				answer := EnsureMatePreview(t.Context(), d, okHTTP, MatePreviewRequest{Lease: "devpool-3", AcquiredAt: "2026-10-23T12:00:00Z", Branch: "feat-x"})
				if answer.Status != "running" {
					t.Fatalf("preview = %+v, want running", answer)
				}
				status, err := mate.ReadStatus(path)
				if err != nil {
					t.Fatal(err)
				}
				if got := status.Copies.Leases[0].State; got != tc.want {
					t.Fatalf("copy state = %s, want %s", got, tc.want)
				}
			})
		})
	}
}

// witness: internal/tools/mate_copies.go | if state.Status == devsup.StatusStarting && ds.Supervisor.Supports(devsup.CapabilityWait) { → if true {
func TestMateCopies_MCPreview_EnsuresTheSelectedServerAndReportsItsAddress(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, initial string
		legacy        bool
		wantStatus    string
		wantReason    string
	}{
		{"already running", "running", false, "running", ""},
		{"already starting", "starting", false, "running", ""},
		{"stopped", "stopped", false, "running", ""},
		{"older supervisor", "stopped", true, "running", ""},
		{"start fails", "crashed", false, "failed", "preview server: crashed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				dir := t.TempDir()
				writeDevpoolLeaseFile(t, dir)
				pool := platform.ServiceStack{ID: "svc-pool", Name: "devpool", ProjectID: "proj-1", Status: "ACTIVE", SubdomainAccess: true,
					Ports:                []platform.Port{{Port: 9003, Protocol: "tcp", HTTPSupport: true}},
					ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1"}}
				client := platform.NewMock().WithServices([]platform.ServiceStack{pool}).WithService(&pool).
					WithProject(&platform.Project{ID: "proj-1", SubdomainHost: "abc1.prg1.zerops.app"}).
					WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: "test-bearer"}})
				c := devservertest.NewSupervised(`{"version":1,"previewPortBase":9000,"servers":[{"name":"api","port":3001,"command":"run api"},{"name":"web","port":3000,"command":"run web","preview":true}]}`)
				c.StableNumber = 3
				if tc.initial == "starting" {
					c.Servers["web"].Status = devsup.StatusStarting
					c.WaitFinds = map[string]devsup.ServerState{"web": {Status: devsup.StatusRunning}}
				} else if tc.initial != "running" {
					c.Servers["web"].Status = devsup.StatusStopped
				}
				if tc.initial == "crashed" {
					c.Crash = map[string]bool{"web": true}
				}
				if tc.legacy {
					c.Capabilities = nil
				}
				d := ops.LeaseDeps{Client: client, ProjectID: "proj-1", StateDir: dir, SSH: c,
					Swarm: func(string, string) ops.SwarmAPI { return mateCopySwarm{} }, Env: func(string) (string, bool) { return "", false }}
				got := EnsureMatePreview(t.Context(), d, okHTTP, MatePreviewRequest{Lease: "devpool-3", AcquiredAt: "2026-10-23T12:00:00Z", Branch: "feat-x"})
				if got.Status != tc.wantStatus || got.Reason != tc.wantReason {
					t.Fatalf("answer = %+v, want %s / %s", got, tc.wantStatus, tc.wantReason)
				}
				if tc.wantStatus == "running" && got.URL != "https://devpool-abc1-9003.prg1.zerops.app" {
					t.Errorf("address = %q, want the selected web server's preview address", got.URL)
				}
				if tc.wantStatus == "running" && c.Status("web") != devsup.StatusRunning {
					t.Errorf("selected server = %s, want running", c.Status("web"))
				}
				if tc.wantStatus == "failed" && got.URL != "" {
					t.Errorf("failed answer carries address %q", got.URL)
				}
			})
		})
	}
}

// witness: internal/tools/mate_copies.go | if branch != req.Branch { → if branch == req.Branch {
func TestMateCopies_MCBranch_RefusesAClassicCopyOnAnotherBranch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ branch, want string }{{"feat-x", ""}, {"other", "draft ended"}} {
		t.Run(tc.branch, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{Hostname: "appdev", Stages: []topology.StageRef{{Hostname: "appstage"}}}); err != nil {
				t.Fatal(err)
			}
			ssh := &stubSSH{output: []byte("checkout-head:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\ncheckout-branch:" + tc.branch + "\ncheckout-clean\n")}
			_, reason := previewCopy(t.Context(), ops.LeaseDeps{StateDir: dir, SSH: ssh}, MatePreviewRequest{Service: "appdev", Branch: "feat-x"}, "appdev", ops.DevRepoRoot)
			if reason != tc.want {
				t.Errorf("reason = %q, want %q", reason, tc.want)
			}
		})
	}
}

// witness: internal/tools/mate_copies.go | return lease.AcquiredAt == req.AcquiredAt && lease.Branch == req.Branch → return true
func TestMateCopies_MCEnded_RefusesAChangedOrMissingLease(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, acquired, branch string
		record                 bool
	}{
		{"missing", "2026-10-23T12:00:00Z", "feat-x", false},
		{"reacquired", "2026-10-22T12:00:00Z", "feat-x", true},
		{"another branch", "2026-10-23T12:00:00Z", "other", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if tc.record {
				writeDevpoolLeaseFile(t, dir)
			}
			d := ops.LeaseDeps{StateDir: dir}
			got := EnsureMatePreview(t.Context(), d, okHTTP, MatePreviewRequest{Lease: "devpool-3", AcquiredAt: tc.acquired, Branch: tc.branch})
			if got.Status != "failed" || got.Reason != "draft ended" || got.URL != "" {
				t.Errorf("answer = %+v, want draft ended without an address", got)
			}
		})
	}
}

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
