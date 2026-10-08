//go:build linux

package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Run on a Linux systemd container explicitly selected by the operator.
// Non-parallel: PATH/HOME are process-wide and the managed browser is exclusive.
func TestBrowserRun_ExitPaths_NoDetachedProcesses(t *testing.T) {
	if os.Getenv("ZCP_BROWSER_LIFECYCLE_TEST") != "1" {
		t.Skip("requires an explicitly selected systemd container")
	}
	for _, outcome := range []string{"success", "error", "deadline", "cancel"} {
		t.Run(outcome, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("HOME", dir)
			pids := filepath.Join(dir, "pids")
			script := fmt.Sprintf("#!/bin/sh\nsetsid sleep 600 </dev/null >/dev/null 2>&1 &\na=$!\nsetsid sleep 600 </dev/null >/dev/null 2>&1 &\nb=$!\nprintf '%%s %%s' \"$a\" \"$b\" > %s\n", shellQuote(pids))
			switch outcome {
			case "success":
				script += "echo '[]'\n"
			case "error":
				script += "exit 1\n"
			default:
				script += "exec sleep 600\n"
			}
			if err := os.WriteFile(filepath.Join(dir, "agent-browser"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			timeout := 10 * time.Second
			if outcome == "deadline" {
				timeout = time.Second
			}
			if outcome == "cancel" {
				go func() {
					for {
						if _, err := os.Stat(pids); err == nil {
							cancel()
							return
						}
						select {
						case <-ctx.Done():
							return
						case <-time.After(10 * time.Millisecond):
						}
					}
				}()
			}
			_, stderr, _, err := (execBrowserRunner{}).Run(ctx, "[]", timeout)
			data, readErr := os.ReadFile(pids)
			if readErr != nil {
				t.Fatalf("fixture did not launch: %v, run=%v stderr=%s", readErr, err, stderr)
			}
			for value := range strings.FieldsSeq(string(data)) {
				pid, convErr := strconv.Atoi(value)
				if convErr != nil {
					t.Fatal(convErr)
				}
				defer func() { _ = unix.Kill(pid, unix.SIGKILL) }()
				if _, statErr := os.Stat(fmt.Sprintf("/proc/%d", pid)); statErr == nil {
					t.Errorf("managed batch left detached browser process %d alive after %s", pid, outcome)
				}
			}
		})
	}
}

func TestBrowserRun_ConcurrentOwner_RefusesAndPreservesOwner(t *testing.T) {
	if os.Getenv("ZCP_BROWSER_LIFECYCLE_TEST") != "1" {
		t.Skip("requires an explicitly selected systemd container")
	}
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	if err := os.WriteFile(filepath.Join(dir, "agent-browser"), []byte("#!/bin/sh\nexec sleep 600\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd, cleanup, err := managedBrowserCommand(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := cleanup(); err != nil {
			t.Error(err)
		}
	}()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	out, stderr, _, err := (execBrowserRunner{}).Run(context.Background(), "[]", time.Second)
	if err == nil || !strings.Contains(err.Error(), "already owned") {
		t.Errorf("concurrent browser must refuse admission, got %v stdout=%s stderr=%s", err, out, stderr)
	}
	cancel()
	_ = cmd.Wait()
}

// This opt-in smoke test exercises real Chrome without provider turns.
func TestBrowserRun_RealChrome_LeavesNoOwnedTasks(t *testing.T) {
	if os.Getenv("ZCP_BROWSER_REAL_TEST") != "1" {
		t.Skip("requires an explicitly selected Mate with agent-browser")
	}
	for _, outcome := range []string{"success", "deadline"} {
		t.Run(outcome, func(t *testing.T) {
			commands := `[["open","data:text/html,<h1>Owned browser</h1>"],["snapshot"],["close"]]`
			timeout := 30 * time.Second
			if outcome == "deadline" {
				commands = `[["open","data:text/html,<h1>Owned browser</h1>"],["wait","30000"],["close"]]`
				timeout = 5 * time.Second
			}
			out, stderr, _, err := (execBrowserRunner{}).Run(context.Background(), commands, timeout)
			if outcome == "success" && err != nil {
				t.Fatalf("real browser batch: %v stderr=%s stdout=%s", err, stderr, out)
			}
			if outcome == "deadline" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("wanted deadline, got %v stderr=%s stdout=%s", err, stderr, out)
			}
			t.Logf("%s: batch error=%v, owned cgroup stopped and runtime removed", outcome, err)
		})
	}
}

func TestBrowserRun_OwnerCrash_CleansOnlyOwnedResidue(t *testing.T) {
	if os.Getenv("ZCP_BROWSER_LIFECYCLE_TEST") != "1" {
		t.Skip("requires an explicitly selected systemd container")
	}
	if os.Getenv("ZCP_BROWSER_CRASH_HELPER") == "1" {
		out, stderr, _, err := (execBrowserRunner{}).Run(context.Background(), "[]", 10*time.Minute)
		t.Fatalf("crash helper unexpectedly returned: %v stdout=%s stderr=%s", err, out, stderr)
	}
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	pidfile := filepath.Join(dir, "pid")
	script := "#!/bin/sh\ntouch \"$TMPDIR/owned-profile\"\nsetsid sleep 600 </dev/null >/dev/null 2>&1 &\necho $! > " + shellQuote(pidfile) + "\nexec sleep 600\n"
	binary := filepath.Join(dir, "agent-browser")
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	helper := exec.CommandContext(context.Background(), executable, "-test.run=^TestBrowserRun_OwnerCrash_CleansOnlyOwnedResidue$")
	helper.Env = append(os.Environ(), "ZCP_BROWSER_CRASH_HELPER=1")
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = helper.Process.Kill() }()
	wait, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var data []byte
	for {
		data, err = os.ReadFile(pidfile)
		if err == nil && len(data) > 0 {
			break
		}
		select {
		case <-wait.Done():
			t.Fatal("crash helper did not launch")
		case <-time.After(10 * time.Millisecond):
		}
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unix.Kill(pid, unix.SIGKILL) }()
	if err := helper.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = helper.Wait()
	outside := filepath.Join(dir, "persistent-profile")
	if err := os.WriteFile(outside, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte("#!/bin/sh\ntest ! -e \"$TMPDIR/owned-profile\" || exit 2\necho '[]'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	_, stderr, _, err := (execBrowserRunner{}).Run(context.Background(), "[]", 10*time.Second)
	if err != nil {
		t.Fatalf("restart after owner crash: %v stderr=%s", err, stderr)
	}
	if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); err == nil {
		t.Error("crashed owner's detached browser survived next managed start")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Error("unowned persistent profile was removed")
	}
}
