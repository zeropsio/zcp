//go:build darwin || linux

package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"
)

// brokenPipe is the write end of a pipe whose read end is already closed: a
// stdout whose reader went away.
func brokenPipe(t *testing.T) *os.File {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}

// TestHandleSIGPIPE_ChildWritingToGoneReader_DiesOfSIGPIPE: the processes zcp
// git starts — ssh — keep SIGPIPE's default, so ssh dies of it when the
// caller's reader went away (`zcp git appdev log | head -1`), as it would
// started from a shell, and the remote git with it; it does not keep the
// session open, discarding what the remote git goes on producing.
//
// non-parallel: changes the process's signal disposition.
func TestHandleSIGPIPE_ChildWritingToGoneReader_DiesOfSIGPIPE(t *testing.T) {
	t.Cleanup(func() { signal.Reset(syscall.SIGPIPE) })
	handleSIGPIPE()
	cmd := exec.CommandContext(t.Context(), "sh", "-c", "exec yes")
	cmd.Stdout, cmd.Stderr = brokenPipe(t), io.Discard
	err := cmd.Run()
	ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
	if !ok || !ws.Signaled() || ws.Signal() != syscall.SIGPIPE {
		t.Errorf("a child writing to a gone reader ended %v (signalled %v), want killed by SIGPIPE", err, ok && ws.Signaled())
	}
}

// sigpipeHelperEnv starts TestHandleSIGPIPE_HelperProcess as the helper.
const sigpipeHelperEnv = "ZCP_TEST_SIGPIPE_HELPER"

// TestHandleSIGPIPE_OwnStdoutToGoneReader_WriteFailsWithEPIPE: zcp git's own
// write to a stdout whose reader went away fails with EPIPE — the relayed
// call's copy ends, revokes and returns — instead of killing zcp, which Go
// does by default for a broken stdout.
func TestHandleSIGPIPE_OwnStdoutToGoneReader_WriteFailsWithEPIPE(t *testing.T) {
	t.Parallel()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestHandleSIGPIPE_HelperProcess$") //nolint:gosec // re-runs this test binary with a fixed test filter
	cmd.Env = append(os.Environ(), sigpipeHelperEnv+"=1")
	cmd.Stdout, cmd.Stderr = brokenPipe(t), io.Discard
	err := cmd.Run()
	if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); !ok || ws.Signaled() || ws.ExitStatus() != 0 {
		t.Errorf("zcp writing to a gone reader ended %v, want its write to fail with EPIPE (exit 0 from the helper)", err)
	}
}

// TestHandleSIGPIPE_HelperProcess is the helper of
// TestHandleSIGPIPE_OwnStdoutToGoneReader_WriteFailsWithEPIPE: with SIGPIPE
// handled as zcp git handles it, it writes to its stdout and exits 0 when the
// write fails with EPIPE, 3 when it fails otherwise, 4 when it never fails.
func TestHandleSIGPIPE_HelperProcess(t *testing.T) {
	if os.Getenv(sigpipeHelperEnv) != "1" {
		t.Skip("runs only as the helper process")
	}
	handleSIGPIPE()
	for range 1000 {
		if _, err := os.Stdout.WriteString("line\n"); err != nil {
			if errors.Is(err, syscall.EPIPE) {
				os.Exit(0)
			}
			os.Exit(3)
		}
	}
	os.Exit(4)
}
