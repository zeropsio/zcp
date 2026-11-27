package ops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops/devservertest"
)

// non-parallel: recording a lease consults the process environment.
// witness: internal/ops/lease_mate_status.go | v.State, _ = mate.NextCopyState(v.State, mate.CopyEvent{Kind: mate.CopyLinkLost}) → v.State, _ = mate.NextCopyState(v.State, mate.CopyEvent{Kind: mate.CopyStop})
func TestDevServer_DSCopyBootRecord_NewAndExistingLeasesStartUnknown(t *testing.T) {
	t.Setenv("ZCP_MATE_ENABLED", "0")
	base := time.Date(2026, 11, 27, 7, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name     string
		previous bool
		at       time.Time
		want     mate.CopyState
	}{{"new", false, time.Time{}, mate.CopyUnknown}, {"existing", true, base, mate.CopyUnknown}, {"newer report", true, base.Add(2 * time.Minute), mate.CopyRunning}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "status.json")
			rec := leaseRecord{PoolHostname: "webpool", ContainerNumber: 1, Branch: "feature", Lease: "test", AcquiredAt: "2026-11-27T06:00:00Z"}
			if err := writeLeaseRecord(dir, rec); err != nil {
				t.Fatal(err)
			}
			if tc.previous {
				if err := mate.UpdateStatus(path, func(s *mate.Status) {
					s.Copies = &mate.CopyStatus{Leases: []mate.LeaseCopyStatus{{Alias: rec.alias(), Branch: rec.Branch, AcquiredAt: rec.AcquiredAt, State: mate.CopyRunning, StateAt: tc.at.Format(time.RFC3339Nano)}}}
				}); err != nil {
					t.Fatal(err)
				}
			}
			if err := PublishMateLeasesAtBoot(dir, path, base.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			status, err := mate.ReadStatus(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := status.Copies.Leases[0].State; got != tc.want {
				t.Fatalf("boot lease = %s, want %s", got, tc.want)
			}
		})
	}
}

// witness: internal/ops/mate_server_wait.go | next, _ = mate.NextCopyState(target.State, event) → next, _ = mate.NextCopyState(target.State, mate.CopyEvent{Kind: mate.CopyLinkLost})
func TestDevServer_DSCopyWrite_FailedPublicationKeepsTheAnsweredState(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		before mate.CopyState
		answer devsup.Status
		want   mate.CopyState
	}{{"start listened", mate.CopyStarting, devsup.StatusRunning, mate.CopyRunning}, {"running stopped", mate.CopyRunning, devsup.StatusStopped, mate.CopyStopped}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			block := filepath.Join(dir, "not-a-directory")
			if err := os.WriteFile(block, []byte("block"), 0o600); err != nil {
				t.Fatal(err)
			}
			c := devservertest.NewSupervised(`{"version":1,"servers":[{"name":"web","port":3000,"command":"run","preview":true}]}`)
			c.WaitFinds = map[string]devsup.ServerState{"web": {Status: tc.answer}}
			d := LeaseDeps{SSH: c}
			target := MateCopyTarget{Hostname: "appdev", State: tc.before, StateAt: "2026-11-27T07:00:00Z"}
			next, err := AwaitMateServer(t.Context(), d, filepath.Join(block, "status.json"), "", target)
			if err == nil || next != tc.want {
				t.Fatalf("write failure = %s, %v; want %s and an error", next, err, tc.want)
			}
		})
	}
}

// witness: internal/ops/lease_mate_status.go | next, ok := mate.NextCopyState(*state, mate.CopyEvent{Kind: mate.CopyLinkLost}) → next, ok := mate.NextCopyState(*state, mate.CopyEvent{Kind: mate.CopyStop})
func TestDevServer_DSCopyBoot_UnknownUntilTheSnapshot(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 11, 27, 7, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name          string
		initial       time.Time
		wantAfterBoot mate.CopyState
	}{
		{"older state", base, mate.CopyUnknown},
		{"newer report", base.Add(2 * time.Minute), mate.CopyRunning},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "status.json")
			if err := mate.UpdateStatus(path, func(s *mate.Status) {
				s.Copies = &mate.CopyStatus{DevServices: []mate.DevCopyStatus{{Hostname: "appdev", State: mate.CopyRunning, StateAt: tc.initial.Format(time.RFC3339Nano)}}}
			}); err != nil {
				t.Fatal(err)
			}
			if err := MarkMateCopiesUnknown(path, base.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			status, err := mate.ReadStatus(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := status.Copies.DevServices[0].State; got != tc.wantAfterBoot {
				t.Fatalf("boot = %s, want %s", got, tc.wantAfterBoot)
			}
			c := devservertest.NewSupervised(`{"version":1,"servers":[{"name":"web","port":3000,"command":"run","preview":true}]}`)
			d := LeaseDeps{SSH: c, Now: func() time.Time { return base.Add(3 * time.Minute) }}
			read := &DevServers{Supervisor: SupervisorVersion{Capabilities: []string{devsup.CapabilityCopyState}}}
			if err := ReportMateCopy(context.Background(), d, path, MateCopyTarget{Hostname: "appdev"}, read); err != nil {
				t.Fatal(err)
			}
			status, err = mate.ReadStatus(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := status.Copies.DevServices[0].State; got != mate.CopyRunning {
				t.Fatalf("snapshot = %s, want running", got)
			}
		})
	}
}

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

// non-parallel: writing a lease record consults the process environment.
// witness: internal/ops/lease_mate_status.go | *state, *stateAt = next, at.UTC().Format(time.RFC3339Nano) → *state, *stateAt = mate.CopyState(string(next)+"!"), at.UTC().Format(time.RFC3339Nano)
func TestDevServer_DSCopyEvents_OnlyCurrentLeaseAndNewerReportAreApplied(t *testing.T) {
	t.Setenv("ZCP_MATE_ENABLED", "0")
	start := time.Date(2026, 11, 27, 7, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, acquired, expected string
		at                       time.Time
		want                     mate.CopyState
	}{
		{"current", "2026-11-27T07:00:00Z", "", start.Add(time.Minute), "running"},
		{"prior lease", "2026-11-27T06:00:00Z", "", start.Add(time.Minute), "starting"},
		{"older report", "2026-11-27T07:00:00Z", "", start.Add(-time.Minute), "starting"},
		{"superseded wait", "2026-11-27T07:00:00Z", "2026-11-27T06:59:00Z", start.Add(time.Minute), "starting"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "status.json")
			rec := leaseRecord{PoolHostname: "webpool", ContainerNumber: 1, Branch: "feature", AcquiredAt: "2026-11-27T07:00:00Z"}
			if err := writeLeaseRecord(dir, rec); err != nil {
				t.Fatal(err)
			}
			if err := mate.UpdateStatus(path, func(s *mate.Status) {
				s.Standup.Error = "kept"
				s.Copies = &mate.CopyStatus{Leases: []mate.LeaseCopyStatus{
					{Alias: "webpool-1", Pool: "webpool", Branch: "feature", AcquiredAt: rec.AcquiredAt, State: "starting", StateAt: start.Format(time.RFC3339Nano)},
					{Alias: "webpool-2", State: "stopped", StateAt: start.Format(time.RFC3339Nano)},
				}}
			}); err != nil {
				t.Fatal(err)
			}
			target := MateCopyTarget{Hostname: "webpool-1", Pool: "webpool", Branch: "feature", AcquiredAt: tc.acquired}
			if err := StoreMateCopyEvent(dir, path, target, mate.CopyEvent{Kind: mate.CopyReport, Report: "running"}, tc.at, tc.expected); err != nil {
				t.Fatal(err)
			}
			status, err := mate.ReadStatus(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := status.Copies.Leases[0].State; got != tc.want {
				t.Errorf("state = %s, want %s", got, tc.want)
			}
			if status.Standup.Error != "kept" || status.Copies.Leases[1].State != "stopped" {
				t.Error("another section or copy changed")
			}
		})
	}
}

// witness: internal/ops/mate_server_wait.go | mate.CopyEvent{Kind: mate.CopyLinkLost}, d.now().UTC(), target.StateAt → mate.CopyEvent{Kind: mate.CopyStop}, d.now().UTC(), target.StateAt
func TestDevServer_DSCopyLink_WaitFailureAndOlderSupervisorAreUnknown(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 11, 27, 7, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name         string
		capabilities []string
	}{
		{"older supervisor", nil}, {"lost wait", []string{devsup.CapabilityCopyState}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "status.json")
			if err := mate.UpdateStatus(path, func(s *mate.Status) {
				s.Copies = &mate.CopyStatus{DevServices: []mate.DevCopyStatus{{Hostname: "appdev", State: "running", StateAt: start.Format(time.RFC3339Nano)}}}
			}); err != nil {
				t.Fatal(err)
			}
			c := &devservertest.Container{Installed: true, Running: true, SupervisorVersion: "v1", Formats: []int{1}, Capabilities: tc.capabilities}
			d := LeaseDeps{SSH: c, Now: func() time.Time { return start.Add(time.Minute) }}
			target := MateCopyTarget{Hostname: "appdev", State: "running", StateAt: start.Format(time.RFC3339Nano)}
			if _, err := AwaitMateServer(t.Context(), d, path, "", target); err == nil {
				t.Fatal("lost link must be reported")
			}
			status, err := mate.ReadStatus(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := status.Copies.DevServices[0].State; got != "unknown" {
				t.Errorf("state = %s, want unknown", got)
			}
		})
	}
}
