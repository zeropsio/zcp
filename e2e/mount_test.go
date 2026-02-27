//go:build e2e

// Tests for: e2e — mount/unmount lifecycle via SSH into ZCP container.
//
// This test creates a zcp@1 service and a nodejs target, SSHes into the
// zcp container, and verifies SSHFS mount/unmount/status operations.
//
// TODO: Phase 2 — deploy ZCP binary to the zcp@1 service and call zerops_mount
// tool directly (via MCP) instead of manually running sshfs/zsc commands over SSH.
// This will test the real SystemMounter code path end-to-end. Current approach
// validates the underlying mount mechanics but bypasses the ops/tools layer.
//
// Prerequisites:
//   - ZCP_API_KEY set
//   - zcli vpn up <project-id> active
//
// Run: go test ./e2e/ -tags e2e -run TestE2E_Mount -v -timeout 600s

package e2e_test

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// sshExec runs a command on a remote Zerops service via SSH.
// Requires active VPN connection (zcli vpn up).
func sshExec(t *testing.T, hostname, command string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ssh",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "ConnectTimeout=10",
		hostname, command,
	).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func TestE2E_Mount(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()
	zcpHostname := "zcpmnt" + suffix
	appHostname := "zcpapp" + suffix

	// Register cleanup to delete test services even if test fails.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		// Attempt unmount before deleting services (best-effort).
		_, _ = sshExec(t, zcpHostname, fmt.Sprintf(
			"sudo -E zsc unit remove sshfs-%s 2>/dev/null; fusermount -u /var/www/%s 2>/dev/null",
			appHostname, appHostname,
		))
		cleanupServices(ctx, h.client, h.projectID, zcpHostname, appHostname)
	})

	step := 0

	// --- Step 1: Import zcp + nodejs services ---
	step++
	logStep(t, step, "zerops_import (zcp + nodejs with sshIsolation)")
	importYAML := `services:
  - hostname: ` + zcpHostname + `
    type: zcp@1
    startWithoutCode: true
  - hostname: ` + appHostname + `
    type: nodejs@22
    startWithoutCode: true
    sshIsolation: vpn project
`
	importText := s.mustCallSuccess("zerops_import", map[string]any{
		"content": importYAML,
	})
	processes := parseProcesses(t, importText)
	t.Logf("  Import returned %d processes", len(processes))

	for _, proc := range processes {
		pid, ok := proc["processId"].(string)
		if !ok || pid == "" {
			continue
		}
		t.Logf("  Waiting for process %s (%s)", pid, proc["actionName"])
		waitForProcess(s, pid)
	}

	// --- Step 2: Wait for both services to be ready ---
	step++
	logStep(t, step, "waiting for services to be ready")
	waitForServiceReady(s, zcpHostname)
	waitForServiceReady(s, appHostname)
	t.Log("  Both services ready")

	// Allow extra time for SSH daemon to start on both services.
	time.Sleep(10 * time.Second)

	// --- Step 3: Clear known_hosts for app hostname on zcp container ---
	step++
	logStep(t, step, "clear known_hosts on zcp container")
	_, _ = sshExec(t, zcpHostname, fmt.Sprintf("ssh-keygen -R %s 2>/dev/null", appHostname))

	// --- Step 4: Verify NOT MOUNTED initially ---
	step++
	logStep(t, step, "verify not mounted initially")
	_, err := sshExec(t, zcpHostname, fmt.Sprintf("grep -q 'fuse.sshfs.*/var/www/%s ' /proc/mounts", appHostname))
	if err == nil {
		t.Fatal("expected /proc/mounts check to fail (not mounted), but it succeeded")
	}
	t.Log("  Confirmed: not mounted")

	// --- Step 5: Mount via SSH ---
	step++
	logStep(t, step, "mount %s via SSHFS on %s", appHostname, zcpHostname)

	// Create mount directory.
	out, err := sshExec(t, zcpHostname, fmt.Sprintf("mkdir -p /var/www/%s", appHostname))
	if err != nil {
		t.Fatalf("mkdir failed: %s (%v)", out, err)
	}

	// Start SSHFS via zsc unit (background service manager on Zerops).
	mountCmd := fmt.Sprintf(
		`sudo -E zsc unit create sshfs-%s "sshfs -f -o reconnect,StrictHostKeyChecking=no,ServerAliveInterval=15,ServerAliveCountMax=3 %s:/var/www /var/www/%s"`,
		appHostname, appHostname, appHostname,
	)
	out, err = sshExec(t, zcpHostname, mountCmd)
	if err != nil {
		t.Fatalf("mount command failed: %s (%v)", out, err)
	}
	t.Log("  Mount command issued")

	// Wait for mount to establish.
	time.Sleep(5 * time.Second)

	// --- Step 6: Verify MOUNTED ---
	step++
	logStep(t, step, "verify mounted")
	out, err = sshExec(t, zcpHostname, fmt.Sprintf("grep -q 'fuse.sshfs.*/var/www/%s ' /proc/mounts", appHostname))
	if err != nil {
		t.Fatalf("expected /proc/mounts to show fuse.sshfs mount, but failed: %s (%v)", out, err)
	}
	t.Log("  Confirmed: mounted")

	// --- Step 7: Verify WRITABLE ---
	step++
	logStep(t, step, "verify writable")
	testFile := fmt.Sprintf("/var/www/%s/.mount_test_%s", appHostname, suffix)
	out, err = sshExec(t, zcpHostname, fmt.Sprintf("touch %s && rm %s", testFile, testFile))
	if err != nil {
		t.Fatalf("write test failed: %s (%v)", out, err)
	}
	t.Log("  Confirmed: writable")

	// --- Step 8: Unmount ---
	step++
	logStep(t, step, "unmount %s", appHostname)
	unmountCmd := fmt.Sprintf(
		"sudo -E zsc unit remove sshfs-%s && fusermount -u /var/www/%s",
		appHostname, appHostname,
	)
	out, err = sshExec(t, zcpHostname, unmountCmd)
	if err != nil {
		t.Fatalf("unmount failed: %s (%v)", out, err)
	}
	t.Log("  Unmount command issued")

	// Wait for unmount to complete.
	time.Sleep(2 * time.Second)

	// --- Step 9: Verify NOT MOUNTED after unmount ---
	step++
	logStep(t, step, "verify not mounted after unmount")
	_, err = sshExec(t, zcpHostname, fmt.Sprintf("grep -q 'fuse.sshfs.*/var/www/%s ' /proc/mounts", appHostname))
	if err == nil {
		t.Fatal("expected /proc/mounts check to fail after unmount, but it succeeded")
	}
	t.Log("  Confirmed: not mounted after unmount")

	// --- Step 10: Delete services ---
	step++
	logStep(t, step, "delete services")
	deleteZcpText := s.mustCallSuccess("zerops_delete", map[string]any{
		"serviceHostname": zcpHostname,
		"confirm":         true,
	})
	deleteZcpProcID := extractProcessID(t, deleteZcpText)
	t.Logf("  Delete zcp process: %s", deleteZcpProcID)

	deleteAppText := s.mustCallSuccess("zerops_delete", map[string]any{
		"serviceHostname": appHostname,
		"confirm":         true,
	})
	deleteAppProcID := extractProcessID(t, deleteAppText)
	t.Logf("  Delete app process: %s", deleteAppProcID)

	waitForProcess(s, deleteZcpProcID)
	waitForProcess(s, deleteAppProcID)
	t.Log("  All test services cleaned up successfully")
}
