//go:build linux

package eval

import (
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
	// no exec runs while /proc/<pid>/environ is read.
	const childScript = "sleep 30; :"

	shPath, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("no sh on PATH: %v", err)
	}
	shDigest, err := sha256File(shPath)
	if err != nil {
		t.Fatalf("sha256File(%s): %v", shPath, err)
	}

	t.Run("child with the window env is counted", func(t *testing.T) {
		cmd := exec.CommandContext(t.Context(), shPath, "-c", childScript)
		cmd.Env = append(os.Environ(),
			"ZCP_CAPTURE_SESSION_ID="+windowID,
			"projectId=proj-linux-test",
			"serviceId=svc-linux-test",
			"ZCP_AUTO_UPDATE=0",
		)
		if err := cmd.Start(); err != nil {
			t.Fatalf("start child: %v", err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill() })

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
		cmd := exec.CommandContext(t.Context(), shPath, "-c", childScript)
		cmd.Env = os.Environ()
		if err := cmd.Start(); err != nil {
			t.Fatalf("start child: %v", err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill() })

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
		cmd := exec.CommandContext(t.Context(), shPath, "-c", childScript)
		cmd.Env = append(os.Environ(), "ZCP_CAPTURE_SESSION_ID="+windowID, "projectId=proj-linux-test")
		if err := cmd.Start(); err != nil {
			t.Fatalf("start child: %v", err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill() })
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
