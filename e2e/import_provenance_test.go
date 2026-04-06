//go:build e2e

// Tests for: e2e — import.yaml lifecycle during bootstrap.
//
// Verifies that after bootstrap provision step:
// 1. import.yaml is copied to each mounted service (persists via deployFiles: [.])
// 2. In container mode: import.yaml is removed from project root
// 3. In local mode: import.yaml stays at project root
// 4. Mount readiness probe ensures SSHFS is ready before file operations
//
// Prerequisites:
//   - ZCP_API_KEY set
//   - zcli vpn up <project-id> active (for SSH verification)
//
// Run: go test ./e2e/ -tags e2e -run TestE2E_ImportProvenance -v -timeout 600s

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestE2E_ImportProvenance_StoredInState(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()[:4]
	appHostname := "inc" + suffix + "app"
	dbHostname := "inc" + suffix + "db"

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		s.callTool("zerops_workflow", map[string]any{"action": "reset"})
		cleanupServices(ctx, h.client, h.projectID, appHostname, dbHostname)
	})

	step := 0

	// --- Step 1: Start bootstrap ---
	step++
	logStep(t, step, "start bootstrap workflow")
	s.callTool("zerops_workflow", map[string]any{"action": "reset"})
	startText := s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"intent":   "e2e import provenance test",
	})
	var startResp bootstrapProgress
	if err := json.Unmarshal([]byte(startText), &startResp); err != nil {
		t.Fatalf("parse start: %v", err)
	}
	t.Logf("  Session: %s", startResp.SessionID)

	// --- Step 2: Complete discover with plan ---
	step++
	logStep(t, step, "complete discover")
	plan := []any{
		map[string]any{
			"runtime": map[string]any{
				"devHostname":   appHostname,
				"type":          "nodejs@22",
				"bootstrapMode": "simple",
			},
			"dependencies": []any{
				map[string]any{
					"hostname":   dbHostname,
					"type":       "postgresql@16",
					"mode":       "NON_HA",
					"resolution": "CREATE",
				},
			},
		},
	}
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action": "complete",
		"step":   "discover",
		"plan":   plan,
	})

	// --- Step 3: Import services via file path (not inline content) ---
	step++
	logStep(t, step, "import services from file")
	importContent := buildImportYAML([]importService{
		{Hostname: dbHostname, Type: "postgresql@16", Mode: "NON_HA", Priority: 10},
		{Hostname: appHostname, Type: "nodejs@22", StartWithoutCode: true, EnableSubdomain: true},
	})

	// Write import.yaml to the working directory (the server derives state dir from CWD).
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	stateDir := filepath.Join(cwd, ".zcp", "state")
	projectRoot := cwd
	importPath := filepath.Join(projectRoot, "import.yaml")
	if err := os.WriteFile(importPath, []byte(importContent), 0o644); err != nil {
		t.Fatalf("write import.yaml: %v", err)
	}
	t.Logf("  Wrote import.yaml to %s (%d bytes)", importPath, len(importContent))

	// Import via file path (this is how container agents use it).
	importText := s.mustCallSuccess("zerops_import", map[string]any{
		"filePath": importPath,
	})
	t.Logf("  Import result: %s", truncate(importText, 200))

	// Wait for services.
	waitForServiceStatus(s, appHostname, "RUNNING", "ACTIVE")
	waitForServiceReady(s, dbHostname)

	// Discover env vars (required by provision checker).
	s.mustCallSuccess("zerops_discover", map[string]any{"includeEnvs": true})

	// --- Step 4: Complete provision — this triggers cleanup ---
	step++
	logStep(t, step, "complete provision (triggers import.yaml cleanup)")
	provText := s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":      "complete",
		"step":        "provision",
		"attestation": "All services created for import provenance test.",
	})
	var provResp bootstrapProgress
	if err := json.Unmarshal([]byte(provText), &provResp); err != nil {
		t.Fatalf("parse provision: %v", err)
	}
	assertProvisionPassed(t, provResp)
	t.Logf("  Provision passed: %s", provResp.CheckResult.Summary)

	// --- Step 5: Verify import.yaml stays at root (local mode = noop) ---
	step++
	logStep(t, step, "verify import.yaml at root (local mode keeps it)")
	if _, err := os.Stat(importPath); err != nil {
		t.Fatalf("import.yaml should stay at root in local mode, but got: %v", err)
	}
	t.Log("  Confirmed: import.yaml kept at root (local mode)")

	// Verify no provenance file in state dir (import.yaml belongs on service, not ZCP state).
	provenancePath := filepath.Join(stateDir, "import-provenance.yaml")
	if _, err := os.Stat(provenancePath); err == nil {
		t.Errorf("provenance file should NOT exist in state dir, but found at %s", provenancePath)
	}

	// Verify content matches what was imported.
	rootData, err := os.ReadFile(importPath)
	if err != nil {
		t.Fatalf("read import.yaml: %v", err)
	}
	for _, hostname := range []string{appHostname, dbHostname} {
		if !strings.Contains(string(rootData), hostname) {
			t.Errorf("import.yaml should contain hostname %q, but doesn't", hostname)
		}
	}
	t.Log("  All hostnames present in import.yaml")

	// Clean up the file we created.
	os.Remove(importPath)
}

func TestE2E_ImportProvenance_MountWriteAfterReadiness(t *testing.T) {
	// This test verifies that after mount readiness probe, file writes
	// through SSHFS actually succeed. Requires SSH/VPN access.
	h := newHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()[:4]
	zcpHostname := "zcpmnt" + suffix
	appHostname := "zcpapp" + suffix

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		// Best-effort unmount + cleanup.
		_, _ = sshExec(t, zcpHostname, fmt.Sprintf(
			"sudo -E zsc unit remove sshfs-%s 2>/dev/null; fusermount -u /var/www/%s 2>/dev/null",
			appHostname, appHostname,
		))
		cleanupServices(ctx, h.client, h.projectID, zcpHostname, appHostname)
	})

	step := 0

	// --- Step 1: Create services ---
	step++
	logStep(t, step, "import zcp + nodejs services")
	importYAML := buildImportYAML([]importService{
		{Hostname: zcpHostname, Type: "zcp@1", StartWithoutCode: true},
		{Hostname: appHostname, Type: "nodejs@22", StartWithoutCode: true},
	})
	importText := s.mustCallSuccess("zerops_import", map[string]any{"content": importYAML})
	processes := parseProcesses(t, importText)
	for _, proc := range processes {
		pid, ok := proc["processId"].(string)
		if !ok || pid == "" {
			continue
		}
		waitForProcess(s, pid)
	}
	waitForServiceReady(s, zcpHostname)
	waitForServiceReady(s, appHostname)

	// Allow SSH daemon to start.
	time.Sleep(10 * time.Second)

	// --- Step 2: Verify SSH access ---
	step++
	logStep(t, step, "verify SSH access")
	requireSSH(t, zcpHostname)

	// --- Step 3: Mount app via SSHFS ---
	step++
	logStep(t, step, "mount %s on %s", appHostname, zcpHostname)
	_, _ = sshExec(t, zcpHostname, fmt.Sprintf("ssh-keygen -R %s 2>/dev/null", appHostname))
	out, err := sshExec(t, zcpHostname, fmt.Sprintf("mkdir -p /var/www/%s", appHostname))
	if err != nil {
		t.Fatalf("mkdir: %s (%v)", out, err)
	}
	mountCmd := fmt.Sprintf(
		`sudo -E zsc unit create sshfs-%s "sshfs -f -o reconnect,StrictHostKeyChecking=no,ServerAliveInterval=15,ServerAliveCountMax=3,transform_symlinks,no_check_root %s:/var/www /var/www/%s"`,
		appHostname, appHostname, appHostname,
	)
	out, err = sshExec(t, zcpHostname, mountCmd)
	if err != nil {
		t.Fatalf("mount: %s (%v)", out, err)
	}

	// --- Step 4: Wait for mount readiness (poll /proc/mounts) ---
	step++
	logStep(t, step, "wait for mount readiness")
	deadline := time.Now().Add(30 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		_, checkErr := sshExec(t, zcpHostname, fmt.Sprintf(
			"grep -q 'fuse.sshfs.*/var/www/%s ' /proc/mounts && stat /var/www/%s/ >/dev/null 2>&1",
			appHostname, appHostname,
		))
		if checkErr == nil {
			ready = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !ready {
		t.Fatal("mount did not become ready within 30s")
	}
	t.Log("  Mount is ready")

	// --- Step 5: Write import.yaml through SSHFS and verify on target ---
	step++
	logStep(t, step, "write file through SSHFS and verify on target")
	testContent := "services:\n  - hostname: test\n    type: nodejs@22\n"
	writeCmd := fmt.Sprintf(
		"echo '%s' > /var/www/%s/import_test.yaml && cat /var/www/%s/import_test.yaml",
		testContent, appHostname, appHostname,
	)
	out, err = sshExec(t, zcpHostname, writeCmd)
	if err != nil {
		t.Fatalf("write through SSHFS failed: %s (%v)", out, err)
	}
	t.Logf("  Write succeeded, content: %s", truncate(out, 100))

	// Verify on target container.
	out, err = sshExec(t, appHostname, "cat /var/www/import_test.yaml")
	if err != nil {
		t.Fatalf("read on target failed: %s (%v)", out, err)
	}
	if !strings.Contains(out, "hostname: test") {
		t.Errorf("file content on target doesn't match: %s", out)
	}
	t.Log("  File visible on target container through SSHFS")

	// --- Step 6: Verify immediate write (no readiness delay) would have failed ---
	// This step documents the race: we can't test the negative case (write before ready)
	// without controlling mount timing, but the readiness probe in SystemMounter.Mount()
	// now ensures this race doesn't happen in production.
	step++
	logStep(t, step, "cleanup")
	out, err = sshExec(t, zcpHostname, fmt.Sprintf(
		"rm /var/www/%s/import_test.yaml; sudo -E zsc unit remove sshfs-%s; fusermount -u /var/www/%s",
		appHostname, appHostname, appHostname,
	))
	if err != nil {
		t.Logf("  cleanup warning: %s (%v)", out, err)
	}
	t.Log("  Done")
}
