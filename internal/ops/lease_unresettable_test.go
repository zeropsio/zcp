// Tests for: ops/lease_unresettable.go, the release path of lease.go — a
// container the platform created for the pool's minContainers has no primary
// snapshot, so the orchestrator refuses its reset (no-primary-snapshot). A
// release without the reset would hand the next holder this one's files, so
// zcp deletes the container instead, with the lease, once the release guard
// passed (docs/spec-workflows.md §8 DP-15).
package ops

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// noPrimarySnapshot is the orchestrator's refusal of a reset of a container
// the platform created (live shape).
func noPrimarySnapshot() error {
	return &platform.SwarmError{
		Status: http.StatusBadRequest, Code: "containerAction",
		Message: "container has no primary snapshot, so it cannot be reset", Reason: "no-primary-snapshot",
	}
}

func TestLeaseRelease_Unresettable_DeletesTheContainer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		status      string // the container's git status
		unpushed    int
		force       bool
		wantRefused bool
	}{
		{name: "clean: the guard passes, the container is deleted"},
		{name: "unsaved work with force: deleted", status: "?? notes.txt\n", unpushed: 2, force: true},
		{name: "unsaved work without force: refused before anything", status: "?? notes.txt\n", unpushed: 2, wantRefused: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hf, _ := unsavedFixture(t, tt.status, tt.unpushed)
			hf.swarm.releaseErr = noPrimarySnapshot()

			view, err := LeaseRelease(context.Background(), hf.deps, "devpool-3", tt.force)

			_, recErr := os.Stat(filepath.Join(hf.stateDir, "leases", "devpool-3.json"))
			if tt.wantRefused {
				if err == nil {
					t.Fatalf("view = %+v, want the guard's refusal", view)
				}
				if len(hf.swarm.releases) != 0 || len(hf.swarm.deletes) != 0 || recErr != nil {
					t.Errorf("a refused release reached the orchestrator (releases %+v, deletes %+v) or dropped the record (%v)", hf.swarm.releases, hf.swarm.deletes, recErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("release: %v", err)
			}
			for _, r := range hf.swarm.releases {
				if !r.reset {
					t.Errorf("released without a reset (%+v): the next holder would get this one's files", r)
				}
			}
			if len(hf.swarm.deletes) != 1 || hf.swarm.deletes[0] != (fakeRelease{id: "c-abc", lease: testLease}) {
				t.Errorf("deletes = %+v, want c-abc deleted with its lease", hf.swarm.deletes)
			}
			if view.State != LeaseStateReleased {
				t.Errorf("state = %q, want released", view.State)
			}
			for _, want := range []string{"could not be reset", "deleted", "minContainers", "0"} {
				if !strings.Contains(view.Note, want) {
					t.Errorf("note %q lacks %q", view.Note, want)
				}
			}
			if strings.Contains(view.Note, testLease) {
				t.Errorf("note leaks the lease: %q", view.Note)
			}
			if !errors.Is(recErr, os.ErrNotExist) {
				t.Errorf("the record survives the deleted container: %v", recErr)
			}
		})
	}
}

func TestLeaseRelease_UnresettableAndTheDeleteFails_KeepsTheLease(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		deleteErr error
		wantLost  bool
	}{
		{name: "a removal is already running", deleteErr: &platform.SwarmError{Status: http.StatusConflict, Code: "containerAction", Message: "another removal is running", Reason: "removal-in-progress", Retryable: true}},
		// A lease the orchestrator no longer knows has nothing left to delete.
		{name: "the lease is gone", deleteErr: &platform.SwarmError{Status: http.StatusConflict, Code: "containerAction", Message: "unknown lease", Reason: "unknown-lease"}, wantLost: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hf, _ := unsavedFixture(t, "", 0)
			hf.swarm.releaseErr = noPrimarySnapshot()
			hf.swarm.deleteErr = tt.deleteErr

			view, err := LeaseRelease(context.Background(), hf.deps, "devpool-3", false)

			_, recErr := os.Stat(filepath.Join(hf.stateDir, "leases", "devpool-3.json"))
			if tt.wantLost {
				if err != nil || view.State != LeaseStateLost || !errors.Is(recErr, os.ErrNotExist) {
					t.Errorf("view = %+v, err %v, record %v; want lost and the record dropped", view, err, recErr)
				}
				return
			}
			var pe *platform.PlatformError
			if !errors.As(err, &pe) {
				t.Fatalf("err = %v, want a refusal", err)
			}
			for _, want := range []string{"could not be reset", "delet"} {
				if !strings.Contains(pe.Message, want) {
					t.Errorf("message %q lacks %q", pe.Message, want)
				}
			}
			if recErr != nil {
				t.Errorf("the record is gone though the container stays reserved: %v", recErr)
			}
			for _, r := range hf.swarm.releases {
				if !r.reset {
					t.Errorf("fell back to a release without reset: %+v", r)
				}
			}
		})
	}
}

func TestLeaseCheckout_FailureOnAnUnresettableContainer_DeletesIt(t *testing.T) {
	t.Parallel()
	hf := newHostFixture(t)
	hf.swarm.acquired = acquiredAs(3)
	hf.swarm.releaseErr = noPrimarySnapshot()
	out := "fatal: repository 'https://github.com/acme/app/' not found"
	hf.deps.SSH = &leaseSSH{fn: func(string, string) ([]byte, error) {
		return []byte(out), sshFailure(out, exitError(t, 128))
	}}

	_, err := LeaseAcquire(context.Background(), hf.deps, testPoolHost, testRepo, "feature")

	var pe *platform.PlatformError
	if !errors.As(err, &pe) || !strings.Contains(pe.Message, "released") {
		t.Fatalf("err = %v, want the checkout failure with the container handed back", err)
	}
	if len(hf.swarm.deletes) != 1 || hf.swarm.deletes[0].id != "c-abc" {
		t.Errorf("deletes = %+v, want the unresettable container deleted", hf.swarm.deletes)
	}
	if _, statErr := os.Stat(filepath.Join(hf.stateDir, "leases", "devpool-3.json")); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("the record survives: %v", statErr)
	}
}
