//go:build linux

package eval

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"strconv"
	"testing"
)

// TestProcessIdentity_LinuxReadsProcOfCapturedChild pins the Linux /proc
// reader half of docs/spec-testing-architecture.md §10.4 "Observed process
// identity": a child process carrying this window's ZCP_CAPTURE_SESSION_ID
// is counted (digest of its /proc/<pid>/exe, project id read back from its
// environment); a child without that env var is unobservable. The test
// process's own /proc/self/environ cannot be made to carry the var via
// os.Setenv (it does not rewrite /proc/self/environ), so both cases spawn a
// short-lived child with an explicit Env.
func TestProcessIdentity_LinuxReadsProcOfCapturedChild(t *testing.T) {
	const windowID = "window-under-test"
	// The trailing builtin keeps the shell from exec'ing sleep in its own
	// place (dash does that for a script's last command), so the pid the test
	// reads stays the shell: its /proc/<pid>/exe is the candidate binary and
	// no exec runs while /proc/<pid>/environ is read. The builtin echo first
	// says the shell runs: cmd.Start returns when the vfork'd child releases
	// its parent, which the kernel does inside execve, before the new image's
	// environment is in place — /proc/<pid>/environ then reads empty and the
	// child unobservable (Release v9.188.0, attempt 1). A zcp the reader
	// observes in production has written its mcp/zcp-<pid>.jsonl, so it is
	// long past its exec; the test waits for the same.
	const childScript = "echo ready; sleep 30; :"

	shPath, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("no sh on PATH: %v", err)
	}
	shDigest, err := sha256File(shPath)
	if err != nil {
		t.Fatalf("sha256File(%s): %v", shPath, err)
	}

	t.Run("child with the window env is counted", func(t *testing.T) {
		cmd := startRunningChild(t, shPath, childScript, append(os.Environ(),
			"ZCP_CAPTURE_SESSION_ID="+windowID,
			"projectId=proj-linux-test",
			"serviceId=svc-linux-test",
			"ZCP_AUTO_UPDATE=0",
		))

		rec := observeOneProcess(cmd.Process.Pid, windowID, shDigest)
		if rec.Classification != processIdentityCounted {
			t.Fatalf("Classification = %q, want counted (rec=%+v)", rec.Classification, rec)
		}
		if !rec.MatchesCandidate {
			t.Errorf("MatchesCandidate = false, want true (digest=%s want=%s)", rec.Digest, shDigest)
		}
		if rec.ProjectID != "proj-linux-test" {
			t.Errorf("ProjectID = %q, want proj-linux-test", rec.ProjectID)
		}
		if rec.ServiceID != "svc-linux-test" {
			t.Errorf("ServiceID = %q, want svc-linux-test", rec.ServiceID)
		}
		if rec.AutoUpdate != "0" {
			t.Errorf("AutoUpdate = %q, want 0", rec.AutoUpdate)
		}
	})

	t.Run("child without the window env is unobservable", func(t *testing.T) {
		cmd := startRunningChild(t, shPath, childScript, os.Environ())

		rec := observeOneProcess(cmd.Process.Pid, windowID, shDigest)
		if rec.Classification != processIdentityUnobserved {
			t.Fatalf("Classification = %q, want unobservable (rec=%+v)", rec.Classification, rec)
		}
	})

	// observeProcessIdentity end-to-end: mcp/zcp-<pid>.jsonl discovery over a
	// real sessionDir layout.
	t.Run("observeProcessIdentity discovers via mcp jsonl filename", func(t *testing.T) {
		sessionDir := t.TempDir()
		mcpDir := sessionDir + "/mcp"
		if err := os.MkdirAll(mcpDir, 0o700); err != nil {
			t.Fatalf("mkdir mcp dir: %v", err)
		}
		cmd := startRunningChild(t, shPath, childScript, append(os.Environ(), "ZCP_CAPTURE_SESSION_ID="+windowID, "projectId=proj-linux-test"))
		pid := cmd.Process.Pid
		jsonlPath := mcpDir + "/zcp-" + strconv.Itoa(pid) + ".jsonl"
		if err := os.WriteFile(jsonlPath, nil, 0o600); err != nil {
			t.Fatalf("write mcp jsonl: %v", err)
		}

		seen := map[int]bool{}
		observed := observeProcessIdentity(context.Background(), sessionDir, windowID, shDigest, seen)
		if len(observed) != 1 || observed[0].PID != pid {
			t.Fatalf("observeProcessIdentity = %+v, want exactly one observation for pid %d", observed, pid)
		}
		if observed[0].Classification != processIdentityCounted {
			t.Fatalf("Classification = %q, want counted", observed[0].Classification)
		}
		// second call with the same seen map must not re-observe the pid.
		if again := observeProcessIdentity(context.Background(), sessionDir, windowID, shDigest, seen); len(again) != 0 {
			t.Fatalf("second observeProcessIdentity call = %+v, want no re-observation of an already-seen pid", again)
		}
	})
}

// startRunningChild starts sh -c script with env and returns once the shell
// runs it — its first line read back — so its exec is over and
// /proc/<pid>/environ holds env. The child is killed at the test's end.
func startRunningChild(t *testing.T, shPath, script string, env []string) *exec.Cmd {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), shPath, "-c", script)
	cmd.Env = env
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "ready\n" {
		t.Fatalf("child never said it runs: %q, %v", line, err)
	}
	return cmd
}
