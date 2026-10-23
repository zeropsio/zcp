//go:build linux || darwin

package devsup

import (
	"context"
	"errors"
	"net"
	"os"
	"syscall"
	"testing"
	"time"
)

// A supervisor stalled by a thrashing container still holds its socket: ctl
// reached it and got no answer in time. That is a supervisor not answering,
// never "no supervisor" — which would send zcp to set one up with a redeploy.
func TestControl_ASupervisorThatDoesNotAnswerInTimeIsReportedUnresponsiveNotMissing(t *testing.T) {
	t.Parallel()
	dir, err := os.MkdirTemp("/tmp", "devsupctl") //nolint:usetesting // the unix socket path must stay under 104 bytes on macOS; t.TempDir() is longer
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "unix", SocketPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	held := make(chan net.Conn, 1)
	go func() {
		if c, err := ln.Accept(); err == nil {
			held <- c // accepted, never answered
		}
	}()
	t.Cleanup(func() {
		select {
		case c := <-held:
			_ = c.Close()
		default:
		}
	})

	resp, err := Control(dir, Request{Verb: VerbStatus}, 200*time.Millisecond)
	if !errors.Is(err, ErrUnresponsive) || errors.Is(err, ErrNoSupervisor) || resp.Code != CodeUnresponsive {
		t.Fatalf("control of a stalled supervisor: code %d err %v, want %d and ErrUnresponsive", resp.Code, err, CodeUnresponsive)
	}
}

// A dial that times out or finds the socket's backlog full reached a live
// supervisor that does not keep up; only an absent socket or a refused
// connection means no supervisor runs there.
func TestControlDialFailure_OnlyAnAbsentOrRefusedSocketIsNoSupervisor(t *testing.T) {
	t.Parallel()
	op := func(err error) error {
		return &net.OpError{Op: "dial", Net: "unix", Err: os.NewSyscallError("connect", err)}
	}
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{"dial timed out", &net.OpError{Op: "dial", Net: "unix", Err: timeoutError{}}, CodeUnresponsive},
		{"context deadline", context.DeadlineExceeded, CodeUnresponsive},
		{"backlog full", op(syscall.EAGAIN), CodeUnresponsive},
		{"socket absent", op(syscall.ENOENT), CodeNoSupervisor},
		{"connection refused", op(syscall.ECONNREFUSED), CodeNoSupervisor},
	} {
		resp, err := dialFailure(tc.err)
		wantErr := ErrNoSupervisor
		if tc.want == CodeUnresponsive {
			wantErr = ErrUnresponsive
		}
		if resp.Code != tc.want || !errors.Is(err, wantErr) || !errors.Is(err, tc.err) {
			t.Errorf("%s: code %d err %v, want %d wrapping %v", tc.name, resp.Code, err, tc.want, wantErr)
		}
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }
