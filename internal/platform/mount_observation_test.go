package platform

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCheckMount_Observation(t *testing.T) {
	t.Parallel()
	// Decision: A mount read that cannot answer is not reported as an absent mount.
	t.Run("A mount read that cannot answer is not reported as an absent mount.", func(t *testing.T) {
		t.Parallel()
		unreadable := errors.New("mount table unreadable")
		m := &SystemMounter{mounted: func(string) (bool, error) { return false, unreadable }}
		_, err := m.CheckMount(context.Background(), t.TempDir())
		if !errors.Is(err, unreadable) {
			t.Fatalf("ASSERTION: unreadable mount table must preserve its error, got %v", err)
		}
	})
	t.Run("kernel mount absent", func(t *testing.T) {
		t.Parallel()
		m := &SystemMounter{mounted: func(string) (bool, error) { return false, nil }}
		state, err := m.CheckMount(context.Background(), t.TempDir())
		if err != nil || state != MountStateNotMounted {
			t.Fatalf("ASSERTION: known absent kernel mount = %v, %v", state, err)
		}
	})
	t.Run("kernel mount present and usable", func(t *testing.T) {
		t.Parallel()
		m := &SystemMounter{mounted: func(string) (bool, error) { return true, nil }}
		state, err := m.CheckMount(context.Background(), t.TempDir())
		if err != nil || state != MountStateActive {
			t.Fatalf("ASSERTION: usable kernel mount = %v, %v", state, err)
		}
	})
}

func TestCheckMount_ProbeErrorsRemainUnknown(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		message string
		stale   bool
	}{
		{"Permission denied", false}, {"Input/output error", false},
		{"Transport endpoint is not connected", true}, {"Stale file handle", true},
	} {
		t.Run(tc.message, func(t *testing.T) {
			t.Parallel()
			m := &SystemMounter{mounted: func(string) (bool, error) { return true, nil },
				probeCommand: func(string, ...string) *exec.Cmd {
					cmd := exec.CommandContext(context.Background(), "/bin/sh", "-c", "read diagnostic; printf '%s' \"$diagnostic\" >&2; exit 1")
					cmd.Stdin = strings.NewReader(tc.message + "\n")
					return cmd
				},
			}
			state, err := m.CheckMount(context.Background(), t.TempDir())
			if tc.stale {
				if err != nil || state != MountStateStale {
					t.Fatalf("state=%v err=%v; want observed stale", state, err)
				}
			} else if err == nil {
				t.Fatalf("ASSERTION: unreadable path reported as state %v without error", state)
			}
		})
	}
}

func TestHasUnit_UnreadableIsNotAbsent(t *testing.T) {
	t.Parallel()
	for _, err := range []error{context.Canceled, errors.New("exit status 1: Failed to connect to bus: No such file or directory"), errors.New("exit status 1")} {
		m := &SystemMounter{run: func(context.Context, time.Duration, string, ...string) error { return err }}
		has, got := m.HasUnit(context.Background(), "apppool-3")
		if has || !errors.Is(got, err) {
			t.Fatalf("ASSERTION: unreadable unit reported as absent: has=%v err=%v", has, got)
		}
	}
}

func TestCheckMount_BlockedProbe_CancelReturnsAndReaps(t *testing.T) {
	t.Parallel()
	inputR, inputW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer inputR.Close()
	defer inputW.Close()
	receiptR, receiptW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer receiptR.Close()
	defer receiptW.Close()
	var starts atomic.Int32
	m := &SystemMounter{mounted: func(string) (bool, error) { return true, nil }, probeCommand: func(string, ...string) *exec.Cmd {
		starts.Add(1)
		// The shell itself blocks in read with an open, empty input pipe; no
		// descendant or wall-clock sleep stands in for the blocked observer.
		cmd := exec.CommandContext(context.Background(), "/bin/sh", "-c", "printf R >&3; read answer")
		cmd.Stdin = inputR
		cmd.ExtraFiles = []*os.File{receiptW}
		return cmd
	}}
	path := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := m.CheckMount(ctx, path); result <- err }()
	receipt := make([]byte, 1)
	if _, err := io.ReadFull(receiptR, receipt); err != nil {
		t.Fatal(err)
	}
	m.probeMu.Lock()
	p := m.probes[path]
	m.probeMu.Unlock()
	if p == nil || receipt[0] != 'R' {
		t.Fatal("missing actual child/start receipt")
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("ASSERTION: blocked observer did not return unknown cancellation: %v", err)
	}
	<-p.done // Kill+Wait receipt: do not leave a child running after the test.
	m.probeMu.Lock()
	retained := m.probes[path] == p
	reaped := p.cmd == nil
	m.probeMu.Unlock()
	if !retained || !reaped || starts.Load() != 1 {
		t.Fatalf("retained=%v reaped=%v starts=%d", retained, reaped, starts.Load())
	}
	// A status arriving before reconciliation consumes the same cancellation,
	// rather than submitting another probe into the mount.
	if _, err := m.CheckMount(context.Background(), path); err == nil {
		t.Fatal("killed observation became a successful read")
	}
	if starts.Load() != 1 {
		t.Fatalf("repeated probe before reconciliation: %d", starts.Load())
	}
}

func TestMountProbe_UnreapedProcess_NeverStartsAnotherReadOrWrite(t *testing.T) {
	t.Parallel()
	path := t.TempDir()
	p := &mountProbe{done: make(chan struct{})}
	m := &SystemMounter{probes: map[string]*mountProbe{path: p}, probeCommand: func(string, ...string) *exec.Cmd {
		t.Error("started a probe beside an unreaped one")
		return exec.CommandContext(context.Background(), "false")
	}}
	for _, writable := range []bool{false, true} {
		if got := m.pathProbe(context.Background(), path, writable); got != p {
			t.Fatalf("unreaped process lost its tracking slot for writable=%v", writable)
		}
	}
	// Inject expiry after joining the retained process. No clock window.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.observePath(ctx, path, true); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled pending observer: %v", err)
	}
	if m.probes[path] != p {
		t.Fatal("cancellation lost unreaped process tracking")
	}
}

func TestForceUnmount_UnitUnreadable_NoKernelUnmount(t *testing.T) {
	t.Parallel()
	var commands []recordedCommand
	m := &SystemMounter{run: func(_ context.Context, _ time.Duration, name string, args ...string) error {
		commands = append(commands, append(recordedCommand{name}, args...))
		if name == "sudo" && slices.Contains(args, "remove") {
			return errors.New("unit removal unanswered")
		}
		if name == "systemctl" {
			return errors.New("unit read unanswered")
		}
		return nil
	}}
	if err := m.ForceUnmount(context.Background(), "apppool-3", t.TempDir()); err == nil {
		t.Fatal("ASSERTION: unreadable unit after unanswered removal must stop before kernel unmount")
	}
	for _, cmd := range commands {
		if cmd[0] == "fusermount" {
			t.Fatalf("ASSERTION: unreadable unit authorized kernel unmount: %v", commands)
		}
	}
}
