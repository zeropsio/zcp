package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops/devservertest"
	"github.com/zeropsio/zcp/internal/topology"
)

// non-parallel: the publishing entry point reads the process environment.
// witness: internal/ops/lease.go | publishMateLeases(stateDir, "record") →
func TestLease_LPMateAcquire_RecordsTheBranch(t *testing.T) {
	for _, tc := range []struct {
		name, branch string
	}{{"feature branch", "feature/card"}, {"another branch", "fix/login"}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "status.json")
			t.Setenv("ZCP_MATE_ENABLED", "1")
			t.Setenv(mate.EnvStatusFile, path)
			synctest.Test(t, func(t *testing.T) {
				for _, rec := range []leaseRecord{
					{PoolHostname: "webpool", ContainerNumber: 1, Branch: "earlier", Lease: "test-secret-1", AcquiredAt: "2026-11-27T07:00:00Z"},
					{PoolHostname: "webpool", ContainerNumber: 2, Branch: tc.branch, Lease: "test-secret-2", AcquiredAt: "2026-11-27T07:01:00Z"},
				} {
					if err := writeLeaseRecord(dir, rec); err != nil {
						t.Fatal(err)
					}
				}
				status, err := mate.ReadStatus(path)
				if err != nil {
					t.Fatal(err)
				}
				if status.Copies == nil || len(status.Copies.Leases) != 2 || status.Copies.Leases[0].Branch != "earlier" || status.Copies.Leases[1].Branch != tc.branch {
					t.Fatalf("lease projection = %+v, want both branches", status.Copies)
				}
				if status.Copies.DevServices != nil {
					t.Fatal("unread development services must not be an empty observation")
				}
				raw, err := os.ReadFile(path)
				if err != nil || strings.Contains(string(raw), "test-secret") {
					t.Fatal("status file must be readable and exclude the lease credential")
				}
			})
		})
	}
}

// non-parallel: the publishing entry point reads the process environment.
// witness: internal/ops/lease.go | publishMateLeases(stateDir, "release") →
func TestLease_LPMateRelease_DropsOnlyTheReleasedLease(t *testing.T) {
	for _, tc := range []struct{ name, released string }{{"first lease", "webpool-1"}, {"second lease", "webpool-2"}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "status.json")
			t.Setenv("ZCP_MATE_ENABLED", "1")
			t.Setenv(mate.EnvStatusFile, path)
			synctest.Test(t, func(t *testing.T) {
				records := []leaseRecord{
					{PoolHostname: "webpool", ContainerNumber: 1, ContainerID: "c1", Branch: "merged", Lease: "test-1"},
					{PoolHostname: "webpool", ContainerNumber: 2, ContainerID: "c2", Branch: "other", Lease: "test-2"},
				}
				for _, rec := range records {
					if err := writeLeaseRecord(dir, rec); err != nil {
						t.Fatal(err)
					}
				}
				for _, rec := range records {
					if rec.alias() == tc.released {
						if _, err := dropLeaseRecord(dir, rec, LeaseStateReleased, "merged"); err != nil {
							t.Fatal(err)
						}
					}
				}
				status, err := mate.ReadStatus(path)
				if err != nil {
					t.Fatal(err)
				}
				if status.Copies == nil || len(status.Copies.Leases) != 1 || status.Copies.Leases[0].Alias == tc.released {
					t.Fatalf("lease projection = %+v, want only the other lease", status.Copies)
				}
			})
		})
	}
}

// witness: internal/ops/mate_server_wait.go | *old = observed → observed = *old
func TestDevServer_DSMateWait_SupervisorAnswerIsRecorded(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 11, 27, 7, 0, 0, 0, time.UTC)
	finish := start.Add(time.Minute)
	for _, tc := range []struct {
		name, status, want      string
		readAt, wantAt, laterAt time.Time
		wait                    bool
	}{
		{name: "running", status: "running", readAt: start, wait: true, want: "running", wantAt: finish},
		{name: "crashed", status: "crashed", readAt: start, wait: true, want: "crashed", wantAt: finish},
		{name: "newer observation", status: "running", readAt: finish.Add(time.Minute), wait: true, want: "starting", wantAt: finish.Add(time.Minute)},
		{name: "older supervisor", status: "running", readAt: start, want: "starting", wantAt: start},
		{name: "another server read later", status: "running", readAt: start, wait: true, want: "running", wantAt: finish, laterAt: finish.Add(time.Minute)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "status.json")
			synctest.Test(t, func(t *testing.T) {
				servers := []topology.DevServerState{{Name: "web", Status: "starting", ReadAt: tc.readAt}}
				var listAt string
				if !tc.laterAt.IsZero() {
					servers = append(servers, topology.DevServerState{Name: "api", Status: "running", ReadAt: tc.laterAt})
					listAt = tc.laterAt.Format(time.RFC3339Nano)
				}
				if err := mate.UpdateStatus(path, func(s *mate.Status) {
					s.Copies = &mate.CopyStatus{DevServices: []mate.DevCopyStatus{{Hostname: "appdev", DevServers: &servers, DevServersReadAt: listAt}}}
				}); err != nil {
					t.Fatal(err)
				}
				c := &devservertest.Container{Installed: true, Running: true, SupervisorVersion: "v1", Formats: []int{1},
					Servers:   map[string]*devsup.ServerState{"web": {Status: devsup.StatusStarting, Desired: devsup.DesiredRunning}},
					WaitFinds: map[string]devsup.ServerState{"web": {Status: devsup.Status(tc.status), Desired: devsup.DesiredRunning}}}
				if tc.wait {
					c.Capabilities = []string{devsup.CapabilityWait}
				}
				d := LeaseDeps{SSH: c, Now: func() time.Time { return finish }}
				_, err := AwaitMateServer(t.Context(), d, path, "", MateServerWait{Hostname: "appdev", Name: "web", ReadAt: start})
				if err != nil {
					t.Fatal(err)
				}
				status, err := mate.ReadStatus(path)
				if err != nil {
					t.Fatal(err)
				}
				got := (*status.Copies.DevServices[0].DevServers)[0]
				if got.Status != tc.want || !got.ReadAt.Equal(tc.wantAt) {
					t.Errorf("server = %+v, want %s observed at %s", got, tc.want, tc.wantAt)
				}
				if !tc.laterAt.IsZero() && status.Copies.DevServices[0].DevServersReadAt != listAt {
					t.Error("an earlier server wait moved the list observation time backwards")
				}
			})
		})
	}
}
