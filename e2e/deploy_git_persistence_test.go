//go:build e2e

// Tests for: e2e — git persistence behavior across deploys.
//
// Verifies whether .git directory survives on the target service (appdev)
// after deploy with various freshGit/includeGit flag combinations.
// Uses existing services: zcpx (zcp@1, deploy source) and appdev (php-nginx@8.4, target).
//
// This is an observational test — it records .git existence after each deploy
// scenario to inform tool description and documentation accuracy.
//
// Prerequisites:
//   - ZCP_API_KEY set
//   - zcli vpn up <project-id> active (SSH access to both zcpx and appdev)
//
// Run: go test ./e2e/ -tags e2e -run TestE2E_DeployGitPersistence -v -timeout 600s

package e2e_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// sshExecWithTimeout runs a command on a remote Zerops service via SSH with custom timeout.
// Extends sshExec with configurable timeout and ServerAliveInterval for long-running commands
// like zcli push that block during build.
func sshExecWithTimeout(t *testing.T, hostname, command string, timeout time.Duration) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ssh",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "ConnectTimeout=10",
		"-o", "ServerAliveInterval=15",
		hostname, command,
	).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func TestE2E_DeployGitPersistence(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	const (
		sourceHost = "zcpx"
		targetHost = "appdev"
		deployDir  = "/tmp/gitpersist"
	)

	// Verify SSH access to both services.
	for _, host := range []string{sourceHost, targetHost} {
		out, err := sshExec(t, host, "echo ok")
		if err != nil {
			t.Skipf("SSH to %s failed (VPN not active?): %s (%v)", host, out, err)
		}
	}

	// Resolve appdev service ID for zcli push --serviceId.
	discoverText := s.mustCallSuccess("zerops_discover", map[string]any{
		"service": targetHost,
	})
	var discoverResult struct {
		Services []struct {
			Hostname  string `json:"hostname"`
			ServiceID string `json:"serviceId"`
		} `json:"services"`
	}
	if err := json.Unmarshal([]byte(discoverText), &discoverResult); err != nil {
		t.Fatalf("parse discover: %v", err)
	}
	if len(discoverResult.Services) == 0 {
		t.Fatalf("service %s not found", targetHost)
	}
	targetServiceID := discoverResult.Services[0].ServiceID
	token := h.authInfo.Token
	t.Logf("Target: %s (ID: %s)", targetHost, targetServiceID)

	// Prepare deploy payload on zcpx via base64-encoded files.
	// Uses bun@1.2 runtime matching the current appdev service type.
	zeropsYml := fmt.Sprintf(`zerops:
  - setup: %s
    build:
      base: bun@1.2
      buildCommands:
        - echo build-done
      deployFiles: .
    run:
      base: bun@1.2
      ports:
        - port: 3000
          httpSupport: true
      start: bun run index.ts
`, targetHost)

	indexTS := `const server = Bun.serve({
  port: 3000,
  fetch(req) {
    return new Response("git-persist-test");
  },
});
console.log("listening on " + server.port);
`

	zeropsB64 := base64.StdEncoding.EncodeToString([]byte(zeropsYml))
	indexB64 := base64.StdEncoding.EncodeToString([]byte(indexTS))

	prepareCmd := fmt.Sprintf(
		"rm -rf %s && mkdir -p %s && echo %s | base64 -d > %s/zerops.yml && echo %s | base64 -d > %s/index.ts",
		deployDir, deployDir, zeropsB64, deployDir, indexB64, deployDir,
	)
	out, err := sshExec(t, sourceHost, prepareCmd)
	if err != nil {
		t.Fatalf("prepare deploy dir on %s: %s (%v)", sourceHost, out, err)
	}
	t.Log("Prepared deploy directory on zcpx")

	// Verify files were written correctly.
	out, err = sshExec(t, sourceHost, fmt.Sprintf("cat %s/zerops.yml", deployDir))
	if err != nil {
		t.Fatalf("verify zerops.yml: %s (%v)", out, err)
	}
	t.Logf("zerops.yml content:\n%s", out)

	// Clean up deploy dir after test.
	t.Cleanup(func() {
		_, _ = sshExec(t, sourceHost, fmt.Sprintf("rm -rf %s", deployDir))
	})

	// buildDeployCmd constructs the SSH command to run on zcpx for deploying to appdev.
	// Mirrors the logic in ops.buildSSHCommand().
	buildDeployCmd := func(freshGit, includeGit bool) string {
		var parts []string
		parts = append(parts, fmt.Sprintf("zcli login %s", token))

		gitInit := "git init -q && git config user.email 'test@test.com' && git config user.name 'test' && git add -A && git commit -q -m 'deploy'"

		pushArgs := fmt.Sprintf("zcli push --serviceId %s", targetServiceID)
		if includeGit {
			pushArgs += " -g"
		}

		if freshGit {
			parts = append(parts, fmt.Sprintf("cd %s && rm -rf .git && sync && %s && %s",
				deployDir, gitInit, pushArgs))
		} else {
			parts = append(parts, fmt.Sprintf("cd %s && (test -d .git || (%s)) && %s",
				deployDir, gitInit, pushArgs))
		}
		return strings.Join(parts, " && ")
	}

	// waitForActive polls until appdev returns to ACTIVE/RUNNING after deploy.
	waitForActive := func() {
		t.Helper()
		time.Sleep(5 * time.Second)
		for i := 0; i < 40; i++ {
			text := s.mustCallSuccess("zerops_discover", map[string]any{
				"service": targetHost,
			})
			var result struct {
				Services []struct {
					Status string `json:"status"`
				} `json:"services"`
			}
			if err := json.Unmarshal([]byte(text), &result); err == nil && len(result.Services) > 0 {
				st := result.Services[0].Status
				if st == "ACTIVE" || st == "RUNNING" {
					return
				}
				t.Logf("  status: %s (waiting...)", st)
			}
			time.Sleep(5 * time.Second)
		}
		t.Error("appdev did not return to ACTIVE after deploy")
	}

	// checkGitOnTarget checks .git existence on appdev's /var/www.
	checkGitOnTarget := func() bool {
		t.Helper()
		out, _ := sshExec(t, targetHost, "test -d /var/www/.git && echo EXISTS || echo MISSING")
		return strings.Contains(out, "EXISTS")
	}

	// checkGitOnSource checks .git existence in the deploy dir on zcpx.
	checkGitOnSource := func() bool {
		t.Helper()
		out, _ := sshExec(t, sourceHost, fmt.Sprintf("test -d %s/.git && echo EXISTS || echo MISSING", deployDir))
		return strings.Contains(out, "EXISTS")
	}

	type scenario struct {
		name       string
		freshGit   bool
		includeGit bool
		cleanFirst bool // rm -rf .git on appdev before deploy
	}

	scenarios := []scenario{
		{
			name:       "freshGit+noIncludeGit",
			freshGit:   true,
			includeGit: false,
			cleanFirst: true,
		},
		{
			name:       "freshGit+includeGit",
			freshGit:   true,
			includeGit: true,
			cleanFirst: true,
		},
		{
			name:       "noFreshGit+includeGit",
			freshGit:   false,
			includeGit: true,
			cleanFirst: false, // .git may exist on target from previous scenario
		},
		{
			name:       "noFreshGit+noIncludeGit",
			freshGit:   false,
			includeGit: false,
			cleanFirst: false,
		},
	}

	type scenarioResult struct {
		name          string
		deployOK      bool
		gitOnSource   bool
		gitOnTarget   bool
		deployOutput  string
	}
	results := make([]scenarioResult, len(scenarios))

	for i, sc := range scenarios {
		logStep(t, i+1, "Scenario: %s (freshGit=%v, includeGit=%v, cleanFirst=%v)",
			sc.name, sc.freshGit, sc.includeGit, sc.cleanFirst)

		// Record .git state on source BEFORE deploy.
		srcBefore := checkGitOnSource()
		t.Logf("  .git on source (before): %v", srcBefore)

		if sc.cleanFirst {
			_, _ = sshExec(t, targetHost, "rm -rf /var/www/.git")
			t.Log("  Cleaned .git from appdev /var/www")
		}

		// Record .git state on target BEFORE deploy.
		tgtBefore := checkGitOnTarget()
		t.Logf("  .git on target (before): %v", tgtBefore)

		// Deploy from zcpx to appdev.
		cmd := buildDeployCmd(sc.freshGit, sc.includeGit)
		t.Logf("  Deploying (freshGit=%v, includeGit=%v)...", sc.freshGit, sc.includeGit)

		deployOut, deployErr := sshExecWithTimeout(t, sourceHost, cmd, 3*time.Minute)
		deployOK := true
		if deployErr != nil {
			// zcli push may exit non-zero after successful build (SSH disconnect).
			if strings.Contains(deployOut, "Deploying service") ||
				strings.Contains(deployOut, "BUILD ARTEFACTS READY") ||
				strings.Contains(deployOut, "DEPLOYED") {
				t.Log("  Build triggered (SSH exited after push, build submitted)")
			} else {
				t.Errorf("  Deploy failed: %s (%v)", deployOut, deployErr)
				deployOK = false
			}
		} else {
			t.Log("  Deploy command completed successfully")
		}

		if deployOK {
			// Wait for appdev to stabilize after deploy.
			waitForActive()
			// Extra wait for container to fully start and SSH to be available.
			time.Sleep(10 * time.Second)
		}

		// Record .git state AFTER deploy.
		srcAfter := checkGitOnSource()
		tgtAfter := checkGitOnTarget()

		t.Logf("  .git on source (after): %v", srcAfter)
		t.Logf("  .git on target (after): %v", tgtAfter)

		// Truncate deploy output for logging.
		logOutput := deployOut
		if len(logOutput) > 500 {
			logOutput = logOutput[:500] + "...(truncated)"
		}

		results[i] = scenarioResult{
			name:         sc.name,
			deployOK:     deployOK,
			gitOnSource:  srcAfter,
			gitOnTarget:  tgtAfter,
			deployOutput: logOutput,
		}
	}

	// Print summary table.
	t.Log("")
	t.Log("=== GIT PERSISTENCE RESULTS ===")
	t.Log("")
	t.Logf("%-30s | %-6s | %-11s | %-11s", "Scenario", "Deploy", "Source .git", "Target .git")
	t.Logf("%-30s-|-%6s-|-%11s-|-%11s", "------------------------------", "------", "-----------", "-----------")
	for _, r := range results {
		deploy := "OK"
		if !r.deployOK {
			deploy = "FAIL"
		}
		src := "MISSING"
		if r.gitOnSource {
			src = "EXISTS"
		}
		tgt := "MISSING"
		if r.gitOnTarget {
			tgt = "EXISTS"
		}
		t.Logf("%-30s | %-6s | %-11s | %-11s", r.name, deploy, src, tgt)
	}
	t.Log("")

	// Log implications for documentation.
	t.Log("=== IMPLICATIONS ===")
	if results[0].deployOK && !results[0].gitOnTarget {
		t.Log("- Without -g: .git does NOT survive deploy (as expected)")
	}
	if results[0].deployOK && results[0].gitOnTarget {
		t.Log("- UNEXPECTED: .git survived deploy WITHOUT -g flag!")
	}
	if results[1].deployOK && results[1].gitOnTarget {
		t.Log("- With -g: .git DOES survive deploy -> includeGit is important, not 'rarely needed'")
	}
	if results[1].deployOK && !results[1].gitOnTarget {
		t.Log("- With -g: .git does NOT survive deploy -> -g flag alone is insufficient")
	}
	if results[2].deployOK {
		if results[2].gitOnTarget {
			t.Log("- Subsequent deploy (no freshGit, with -g): .git persists -> freshGit only needed for first deploy")
		} else {
			t.Log("- Subsequent deploy (no freshGit, with -g): .git lost -> freshGit may be needed every time")
		}
	}
	if results[3].deployOK {
		if results[3].gitOnTarget {
			t.Log("- Deploy without -g but .git existed: .git persists -> deploy preserves existing .git on target")
		} else {
			t.Log("- Deploy without -g: .git lost -> each deploy replaces /var/www entirely")
		}
	}
}
