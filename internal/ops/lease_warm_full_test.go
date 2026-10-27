// Tests for: ops/lease_warm.go's release decision in a full pool
// (docs/spec-workflows.md §8 DP-27, DP-28 "Preview route"): a container the
// orchestrator stops leaves the pool's public routes, and the platform puts
// it back only when the pool's containers change (live-verified 2026-10-25,
// plans/unpark-502). A reset stops and starts the container, so the one a
// release keeps as the pool's warm container is unrouted until the next
// acquire's top-up creates a container — which a pool holding its maximum
// cannot do (measured 2026-10-25 10:29 UTC: the acquire that took the
// reset-kept lvupool-1 in a full pool answered 502). So in a pool that serves
// an HTTP port and holds maxContainers containers, counted as the top-up
// counts them, a release removes its container rather than keep it warm; the
// pool's next acquire creates one, whose preview is routed.
package ops

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// servePorts gives the warm fixture's pool service ports (previewPorts serves
// lease previews; a database-only port serves none).
func (wf *warmFixture) servePorts(ports []platform.Port) {
	wf.deps.Client = platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "proj"}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-pool", Name: testPoolHost, Status: "ACTIVE", Ports: ports, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
		}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: testPoolToken, Sensitive: true}})
}

// TestLeaseRelease_FullPublicPool_RemovesRatherThanKeepsWarm: a pool serving
// previews that holds its maximum — another holder's container and the
// released one — removes the released container with its own lease instead
// of resetting it as the warm one: no reset release, no create. The release
// says why, and status says the pool has no warm container while it is full.
func TestLeaseRelease_FullPublicPool_RemovesRatherThanKeepsWarm(t *testing.T) {
	t.Parallel()
	orch := newWarmOrch(2)
	orch.add(4, leaseLockReserved, true, false, 1) // another holder's
	wf := newWarmFixture(t, orch)
	wf.servePorts(previewPorts)
	wf.hold(t, 5, true)

	view := wf.release(t, "devpool-5")

	releases, deletes, creates := orch.log()
	if len(deletes) != 1 || deletes[0] != (fakeRelease{id: "c-5", lease: warmLeaseOf("c-5")}) {
		t.Errorf("deletes = %+v, want c-5 removed with its own lease", deletes)
	}
	if len(releases) != 0 || len(creates) != 0 {
		t.Errorf("releases %+v, creates %v: want neither (a removed container is not reset; nothing fits beside the maximum)", releases, creates)
	}
	if view.State != LeaseStateReleased {
		t.Errorf("state = %q, want released", view.State)
	}
	for _, want := range []string{"removed with this lease rather than reset", "maximum of 2", "502", "cold start", "no warm container"} {
		if !strings.Contains(view.Note, want) {
			t.Errorf("note %q lacks %q", view.Note, want)
		}
	}
	if strings.Contains(view.Note, keptWarmNote) {
		t.Errorf("note %q says the container was kept warm", view.Note)
	}
	wf.recordGone(t, "devpool-5")

	status, err := LeaseStatus(context.Background(), wf.deps, testPoolHost)
	if err != nil {
		t.Fatal(err)
	}
	w := poolWarmOf(t, status)
	if w.Ready == nil || *w.Ready != 0 || w.Last == nil || w.Last.Outcome != LeaseWarmFull {
		t.Fatalf("warm = %+v (last %+v), want ready 0 and last full", w, w.Last)
	}
	for _, want := range []string{"no warm container", "removed", "maximum of 2", "cold start"} {
		if !strings.Contains(w.Last.Note, want) {
			t.Errorf("status's last note %q lacks %q", w.Last.Note, want)
		}
	}
}

// TestLeaseRelease_FullPoolCountsStuckContainers_Removes: a container no
// acquire takes still takes a place of the pool — one being removed, one a
// failed platform action left standing — so a pool full with it counts as
// full, as the top-up and a full pool's refusal count it.
func TestLeaseRelease_FullPoolCountsStuckContainers_Removes(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		stuck func(o *warmOrch)
	}{
		{"being removed", func(o *warmOrch) {
			b := o.add(6, "", true, false, 1)
			b.c.Status, b.c.LockState = warmStatusDeleting, swarmLockDeleting
		}},
		{"a failed platform action", func(o *warmOrch) { o.add(6, "", true, false, 1).c.Status = "ACTION_FAILED" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			orch := newWarmOrch(3)
			orch.add(4, leaseLockReserved, true, false, 1) // another holder's
			tt.stuck(orch)
			wf := newWarmFixture(t, orch)
			wf.servePorts(previewPorts)
			wf.hold(t, 5, true)

			view := wf.release(t, "devpool-5")

			releases, deletes, _ := orch.log()
			if len(deletes) != 1 || deletes[0] != (fakeRelease{id: "c-5", lease: warmLeaseOf("c-5")}) || len(releases) != 0 {
				t.Errorf("deletes %+v, releases %+v: want c-5 removed with its own lease, never reset", deletes, releases)
			}
			if !strings.Contains(view.Note, "maximum of 3") {
				t.Errorf("note %q: want the pool's maximum of 3 named", view.Note)
			}
		})
	}
}

// TestLeaseRelease_WarmKeptWhereTheRouteNeedsNoRemoval: today's reset-keep
// stands where the warm container's route needs no removal — a pool with room
// (the next acquire's top-up create re-renders the routes) and a full pool
// with no HTTP port (no public route to lose).
func TestLeaseRelease_WarmKeptWhereTheRouteNeedsNoRemoval(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		max   int
		ports []platform.Port
	}{
		{"a pool serving previews, with room", 3, previewPorts},
		{"a full pool with no HTTP port", 2, []platform.Port{{Port: 5432, Protocol: "tcp", Scheme: "tcp"}}},
		{"a full pool with no port", 2, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			orch := newWarmOrch(tt.max)
			orch.add(4, leaseLockReserved, true, false, 1) // another holder's
			wf := newWarmFixture(t, orch)
			wf.servePorts(tt.ports)
			wf.hold(t, 5, true)

			view := wf.release(t, "devpool-5")

			releases, deletes, creates := orch.log()
			if len(releases) != 1 || releases[0] != (fakeRelease{id: "c-5", lease: warmLeaseOf("c-5"), reset: true}) {
				t.Errorf("releases = %+v, want c-5 released with its lease and a reset", releases)
			}
			if len(deletes) != 0 || len(creates) != 0 {
				t.Errorf("deletes %+v, creates %v: want neither (the kept container is the warm one)", deletes, creates)
			}
			if !strings.Contains(view.Note, keptWarmNote) {
				t.Errorf("note %q: want kept as the pool's warm container", view.Note)
			}
		})
	}
}

// TestLeaseReleaseGroup_FullPublicPoolMemberRemoved_OtherKeptWarm: a branch's
// release decides each member under its own pool's warm lock from its own
// read: the member in a full pool serving previews is removed with its own
// lease, the member in a pool with room is kept warm — with the guard (ssh
// reads each lease) or forced.
func TestLeaseReleaseGroup_FullPublicPoolMemberRemoved_OtherKeptWarm(t *testing.T) {
	t.Parallel()
	for _, force := range []bool{false, true} {
		t.Run("force="+strconv.FormatBool(force), func(t *testing.T) {
			t.Parallel()
			hq, web := newWarmOrch(3), newWarmOrch(2) // hqpool has room; webpool will be full
			for _, o := range []struct {
				orch       *warmOrch
				svc        string
				mine, held int64
			}{{hq, "svc-hq", 3, 4}, {web, "svc-web", 7, 8}} {
				o.orch.add(o.held, leaseLockReserved, true, false, 1).c.ServiceID = o.svc // another holder's
				o.orch.add(o.mine, leaseLockReserved, true, false, 1).c.ServiceID = o.svc
			}
			stateDir := t.TempDir()
			for _, r := range []struct {
				pool, svc string
				number    int64
			}{{groupHQPool, "svc-hq", 3}, {groupWebPool, "svc-web", 7}} {
				id := "c-" + strconv.FormatInt(r.number, 10)
				if err := writeLeaseRecord(stateDir, leaseRecord{PoolHostname: r.pool, PoolServiceID: r.svc, ContainerID: id,
					ContainerNumber: r.number, ContainerHostname: "node-id-" + strconv.FormatInt(r.number, 10) + ".runtime." + r.pool + ".zerops",
					Lease: warmLeaseOf(id), Branch: "feat", Repo: testRepo, AcquiredAt: "2026-10-25T09:00:00Z"}); err != nil {
					t.Fatal(err)
				}
			}
			ssh := &leaseSSH{}
			ssh.setHandler(func(_, command string) ([]byte, error) {
				if strings.Contains(command, "rev-list --count") {
					return []byte("0\n"), nil
				}
				return nil, nil // a clean checkout
			})
			hqSvc, webSvc := poolService("svc-hq", groupHQPool), poolService("svc-web", groupWebPool)
			hqSvc.Ports, webSvc.Ports = previewPorts, previewPorts
			mock := platform.NewMock().
				WithProject(&platform.Project{ID: "p1", Name: "proj"}).
				WithServices([]platform.ServiceStack{hqSvc, webSvc}).
				WithServiceEnv("svc-hq", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: "hq-token", Sensitive: true}}).
				WithServiceEnv("svc-web", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: "web-token", Sensitive: true}})
			orchs := map[string]SwarmAPI{orchestratorURL(groupHQPool): hq, orchestratorURL(groupWebPool): web}
			deps := LeaseDeps{
				Client: mock, ProjectID: "p1", StateDir: stateDir, SSH: ssh,
				Swarm: func(baseURL, _ string) SwarmAPI { return orchs[baseURL] },
				Env:   func(string) (string, bool) { return "", false },
				Warm:  &LeaseWarm{Target: 1, Go: func(fn func()) { fn() }},
			}

			views, err := LeaseReleaseGroup(context.Background(), deps, "", "feat", force)
			if err != nil {
				t.Fatalf("LeaseReleaseGroup: %v", err)
			}
			if len(views) != 2 {
				t.Fatalf("views = %+v, want both leases", views)
			}
			notes := map[string]string{}
			for _, v := range views {
				if v.State != LeaseStateReleased || v.Error != "" {
					t.Errorf("view = %+v, want released", v)
				}
				notes[v.Alias] = v.Note
			}
			if hqRel, hqDel, _ := hq.log(); len(hqRel) != 1 || !hqRel[0].reset || len(hqDel) != 0 {
				t.Errorf("hqpool releases %+v, deletes %+v: want hqpool-3 reset and kept (the pool has room)", hqRel, hqDel)
			}
			if !strings.Contains(notes["hqpool-3"], keptWarmNote) {
				t.Errorf("hqpool-3 note %q: want kept as the pool's warm container", notes["hqpool-3"])
			}
			if webRel, webDel, _ := web.log(); len(webRel) != 0 || len(webDel) != 1 || webDel[0] != (fakeRelease{id: "c-7", lease: warmLeaseOf("c-7")}) {
				t.Errorf("webpool releases %+v, deletes %+v: want webpool-7 removed with its own lease (the pool is full)", webRel, webDel)
			}
			if n := notes["webpool-7"]; !strings.Contains(n, "removed") || !strings.Contains(n, "maximum of 2") {
				t.Errorf("webpool-7 note %q: want removed because the pool holds its maximum of 2", n)
			}
			if recs, err := readLeaseRecords(stateDir); err != nil || len(recs) != 0 {
				t.Errorf("records left: %+v (%v)", recs, err)
			}
		})
	}
}

// TestAbandonLease_FullPublicPool_HandBackSaysRemovedFull: a failed
// checkout's hand-back goes through the same release, so in a full pool
// serving previews the container is removed, and the refusal says so and why.
func TestAbandonLease_FullPublicPool_HandBackSaysRemovedFull(t *testing.T) {
	t.Parallel()
	orch := newWarmOrch(2)
	orch.add(4, leaseLockReserved, true, false, 1) // another holder's
	wf := newWarmFixture(t, orch)
	wf.servePorts(previewPorts)
	wf.hold(t, 5, true)
	rec, err := loadLeaseRecord(wf.stateDir, "devpool-5")
	if err != nil {
		t.Fatal(err)
	}

	got := abandonLease(context.Background(), wf.deps, rec, platform.NewPlatformError(platform.ErrAPIError, "checkout failed", "Fix the branch"))

	var pe *platform.PlatformError
	if !errors.As(got, &pe) {
		t.Fatalf("abandonLease = %v, want a platform error", got)
	}
	if !strings.Contains(pe.Message, "removed") || !strings.Contains(pe.Message, "full") {
		t.Errorf("message %q: want the container removed because the pool is full", pe.Message)
	}
	if strings.Contains(pe.Message, "kept as the pool's warm container") || strings.Contains(pe.Message, "already had its warm container") {
		t.Errorf("message %q names another outcome", pe.Message)
	}
	if _, deletes, _ := orch.log(); len(deletes) != 1 || deletes[0].id != "c-5" {
		t.Errorf("deletes = %+v, want c-5 removed", deletes)
	}
}
