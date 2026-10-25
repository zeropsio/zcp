//go:build unix

// Tests for: the per-alias record lock (docs/spec-workflows.md §8 DP-2) — a
// record is dropped only while it is still the lease's own, checked and
// removed with no write of another zcp process in between.
package ops

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// TestDropLeaseRecord_WaitsForAWriterOfTheAlias: another zcp process sharing
// the state directory writes a lease's record under the alias's record lock
// (<stateDir>/locks/record-<alias>.lock). A drop of the old lease's record
// reads the record and removes it under the same lock, so a write landing
// between the read and the remove — the new lease's record deleted with its
// secret — cannot happen: the drop waits for the writer, then finds the new
// lease's record and keeps it.
func TestDropLeaseRecord_WaitsForAWriterOfTheAlias(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	recordLease(t, stateDir, testPoolHost, "svc-pool", 3, "feat")
	old, err := loadLeaseRecord(stateDir, "devpool-3")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(stateDir, "locks"), 0o700); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(stateDir, "locks", "record-devpool-3.lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}

	type dropped struct {
		v   LeaseView
		err error
	}
	done := make(chan dropped, 1)
	go func() {
		v, err := dropLeaseRecord(stateDir, old, LeaseStateReleased, "")
		done <- dropped{v, err}
	}()
	select {
	case d := <-done:
		t.Fatalf("the drop returned (%+v, %v) while another process held the alias's record lock mid-write", d.v, d.err)
	case <-time.After(300 * time.Millisecond):
	}
	// The other process's write, still under its lock: its own lease.
	next := old
	next.Lease = "devpool-lease-of-another-zcp-process"
	raw, err := json.Marshal(next)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "leases", "devpool-3.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}

	var d dropped
	select {
	case d = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the drop did not return once the lock was free")
	}
	if d.err != nil {
		t.Fatalf("drop: %v", d.err)
	}
	if got := recordLeaseOf(t, stateDir, "devpool-3"); got != next.Lease {
		t.Errorf("devpool-3's record holds lease %q, want the other process's, kept", got)
	}
	if d.v.recordKept == "" {
		t.Errorf("view = %+v, want it to say the alias names another lease now", d.v)
	}
}
