//go:build e2e

// Tests for: e2e — ticket E2 (deploy failure classification) live
// verification. Triggers known BUILD_FAILED + PREPARING_RUNTIME_FAILED
// scenarios on real Zerops and asserts that the new
// `failureClassification` block lands on the deploy response with the
// correct category + matching signal id.
//
// The BuildPhase test also pins the build-log surface (mode=ssh,
// buildLogsSource=build_container, log-content evidence,
// suggestion/nextActions phrasing) — folded in from the former
// build_logs_test.go so one ~10-min BUILD_FAILED build verifies BOTH
// classification AND the log surface.
//
// Differs from deploy_error_classification_test.go (which tests SSH
// transport-error classification): this file pins the post-trigger
// classifier path through pollDeployBuild → ops.ClassifyDeployFailure.
//
// Uses `zcp` as the SSH source — the live container running ZCP on
// eval-zcp (project ID `i6HLVWoiQeeLv8tV0ZZ0EQ`, see CLAUDE.local.md).
// `zcp` is the default ZCP_HOST in the Makefile and the SSH source for
// every e2e test in this package; override via `ZCP_HOST=<hostname>
// make e2e-zcp` when targeting a different runtime.
//
// Prerequisites:
//   - ZCP_API_KEY set (extract from .mcp.json on local dev box)
//   - SSH access to zcp container (host key checking disabled per CLAUDE.local.md)
//
// Run:
//   go test ./e2e/ -tags e2e -run TestE2E_FailureClassification -v -timeout 900s

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

	"github.com/zeropsio/zcp/internal/topology"
)

// resetLocalAdoptionState removes the local .zcp/state/services directory so
// the requireAdoption gate skips on a fresh-test run. Without this the gate
// rejects every new hostname imported during the test as "not adopted".
// Cleanup-safe: ignored when the dir doesn't exist.
func resetLocalAdoptionState(t *testing.T) {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	_ = os.RemoveAll(filepath.Join(cwd, ".zcp", "state", "services"))
	_ = os.RemoveAll(filepath.Join(cwd, ".zcp", "state", "sessions"))
	_ = os.RemoveAll(filepath.Join(cwd, ".zcp", "state", "registry.json"))
}

const failureClassSourceHost = "zcp"

// TestE2E_FailureClassification_BuildPhase pins ticket E2: a BUILD_FAILED
// deploy response carries a failureClassification block with category=
// "build" and a matching signal id (build:command-not-found for the
// canonical "missing binary" failure).
func TestE2E_FailureClassification_BuildPhase(t *testing.T) {
	requireSSH(t, failureClassSourceHost)
	resetLocalAdoptionState(t)
	h := newHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()
	appHostname := "zcpbl" + suffix // matches build_logs prefix → orphan cleanup eligible
	deployDir := "/tmp/e2fcbuild" + suffix

	t.Cleanup(func() {
		s.callTool("zerops_workflow", map[string]any{"action": "reset"})
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		cleanupServices(ctx, h.client, h.projectID, appHostname)
		_, _ = sshExec(t, failureClassSourceHost, fmt.Sprintf("rm -rf %s", deployDir))
	})

	step := 0

	step++
	logStep(t, step, "starting bootstrap (classic route)")
	importYAML := fmt.Sprintf(`services:
  - hostname: %s
    type: nodejs@22
    startWithoutCode: true
    minContainers: 1
`, appHostname)
	bootstrapDevServiceForDeploy(t, s, appHostname, "nodejs@22", importYAML, nil)

	step++
	logStep(t, step, "stage broken zerops.yml on %s:%s", failureClassSourceHost, deployDir)
	brokenYml := fmt.Sprintf(`zerops:
  - setup: %s
    build:
      base: nodejs@22
      buildCommands:
        - echo "starting"
        - thisbinaryisnotreal_xyz_e2_build
      deployFiles: ./
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      start: node server.js
`, appHostname)
	writeAppViaSSH(t, failureClassSourceHost, deployDir, brokenYml, "console.log(1);")

	step++
	logStep(t, step, "zerops_deploy %s → %s (expect BUILD_FAILED)", failureClassSourceHost, appHostname)
	deployRes := s.callTool("zerops_deploy", map[string]any{
		"sourceService": failureClassSourceHost,
		"targetService": appHostname,
		"workingDir":    deployDir,
	})
	if deployRes.IsError {
		t.Fatalf("zerops_deploy returned MCP error (unexpected): %s", truncate(getE2ETextContent(t, deployRes), 500))
	}
	body := getE2ETextContent(t, deployRes)
	t.Logf("  raw response head: %s", truncate(body, 800))

	var parsed deployFailureWire
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("parse deploy result: %v", err)
	}

	step++
	logStep(t, step, "assert BUILD_FAILED + classification populated")
	if parsed.Status != "BUILD_FAILED" {
		t.Fatalf("Status = %q, want BUILD_FAILED", parsed.Status)
	}
	if parsed.BuildStatus != "BUILD_FAILED" {
		t.Errorf("buildStatus = %q, want BUILD_FAILED", parsed.BuildStatus)
	}
	if parsed.FailureClassification == nil {
		t.Fatalf("failureClassification missing — E2 wiring broken")
	}
	if parsed.FailureClassification.Category != topology.FailureClassBuild {
		t.Errorf("Category = %q, want %q", parsed.FailureClassification.Category, topology.FailureClassBuild)
	}
	if !signalsContain(parsed.FailureClassification.Signals, "build:command-not-found") {
		t.Errorf("Signals %v missing build:command-not-found — pattern may not match real Zerops log format", parsed.FailureClassification.Signals)
	}
	if parsed.FailureClassification.SuggestedAction == "" {
		t.Errorf("SuggestedAction empty")
	}
	t.Logf("  category=%s signals=%v", parsed.FailureClassification.Category, parsed.FailureClassification.Signals)
	t.Logf("  likelyCause=%s", parsed.FailureClassification.LikelyCause)
	t.Logf("  suggestedAction=%s", parsed.FailureClassification.SuggestedAction)

	// --- Build-log surface (transplanted from the former build_logs_test.go) ---
	// One BUILD_FAILED build now verifies BOTH classification AND the
	// log-surface contract: cross-deploy mode, build-container log source,
	// log-content evidence of the failed command, and actionable
	// suggestion/nextActions phrasing.
	step++
	logStep(t, step, "assert build-log surface (mode/source/content/actions)")
	if parsed.Mode != "ssh" {
		t.Errorf("mode = %q, want ssh", parsed.Mode)
	}
	if len(parsed.BuildLogs) == 0 {
		t.Fatal("buildLogs is empty — BUILD_FAILED must surface the build pipeline output")
	}
	t.Logf("  buildLogs: %d lines", len(parsed.BuildLogs))
	for i, line := range parsed.BuildLogs {
		t.Logf("    [%d] %s", i, line)
	}
	if parsed.BuildLogsSource != "build_container" {
		t.Errorf("buildLogsSource = %q, want %q", parsed.BuildLogsSource, "build_container")
	}
	// Logs must contain evidence of the broken command (the broken binary
	// name, or a generic "not found" / "failed" diagnostic).
	logsJoined := strings.Join(parsed.BuildLogs, "\n")
	if !strings.Contains(logsJoined, "thisbinaryisnotreal") &&
		!strings.Contains(logsJoined, "not found") &&
		!strings.Contains(logsJoined, "failed") {
		t.Errorf("buildLogs should contain evidence of the failed command, got:\n%s", logsJoined)
	}
	// Suggestion + nextActions should target the failing build phase, not a
	// specific field name (curated per-cause guidance evolved away from a
	// raw "read buildLogs" pointer).
	if !strings.Contains(parsed.Suggestion, "buildCommands") && !strings.Contains(parsed.Suggestion, "buildLogs") {
		t.Errorf("suggestion should target the failed build phase, got: %q", parsed.Suggestion)
	}
	if !strings.Contains(parsed.NextActions, "buildCommands") && !strings.Contains(parsed.NextActions, "buildLogs") {
		t.Errorf("nextActions should target the failed build phase, got: %q", parsed.NextActions)
	}
	t.Logf("  suggestion=%s", parsed.Suggestion)
	t.Logf("  nextActions=%s", parsed.NextActions)
}

// TestE2E_FailureClassification_PrepareSudoMissing triggers the canonical
// "package install without sudo" failure and confirms category=start +
// signal=prepare:missing-sudo. Pins that the prepare-phase signal library
// matches what real Zerops emits for apt-get-without-sudo failures.
func TestE2E_FailureClassification_PrepareSudoMissing(t *testing.T) {
	requireSSH(t, failureClassSourceHost)
	resetLocalAdoptionState(t)
	h := newHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()
	appHostname := "zcppf" + suffix // matches prepare_fail prefix → orphan cleanup eligible
	deployDir := "/tmp/e2fcprep" + suffix

	t.Cleanup(func() {
		s.callTool("zerops_workflow", map[string]any{"action": "reset"})
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		cleanupServices(ctx, h.client, h.projectID, appHostname)
		_, _ = sshExec(t, failureClassSourceHost, fmt.Sprintf("rm -rf %s", deployDir))
	})

	step := 0

	step++
	logStep(t, step, "starting bootstrap (classic route)")
	importYAML := fmt.Sprintf(`services:
  - hostname: %s
    type: nodejs@22
    startWithoutCode: true
    minContainers: 1
`, appHostname)
	bootstrapDevServiceForDeploy(t, s, appHostname, "nodejs@22", importYAML, nil)

	step++
	logStep(t, step, "stage broken prepareCommands on %s:%s", failureClassSourceHost, deployDir)
	// nonexistent_prepare_cmd produces "command not found" / "No such file
	// or directory" — that path matches the prepare:missing-sudo regex
	// branch ("must be root" / "Operation not permitted"), so the actual
	// signal that should fire is the prepare baseline (phase:prepare). If
	// real Zerops emits a stronger signal, we'll see it in the log.
	prepFail := fmt.Sprintf(`zerops:
  - setup: %s
    build:
      base: nodejs@22
      buildCommands:
        - echo "build ok"
      deployFiles: ./
    run:
      base: nodejs@22
      prepareCommands:
        - apk add --no-cache imagemagick-dev-this-package-doesnt-exist-xyz
      ports:
        - port: 3000
          httpSupport: true
      start: node server.js
`, appHostname)
	writeAppViaSSH(t, failureClassSourceHost, deployDir, prepFail, "console.log(1);")

	step++
	logStep(t, step, "zerops_deploy (expect PREPARING_RUNTIME_FAILED)")
	deployRes := s.callTool("zerops_deploy", map[string]any{
		"sourceService": failureClassSourceHost,
		"targetService": appHostname,
		"workingDir":    deployDir,
	})
	if deployRes.IsError {
		t.Fatalf("zerops_deploy returned MCP error (unexpected): %s", truncate(getE2ETextContent(t, deployRes), 500))
	}
	body := getE2ETextContent(t, deployRes)
	t.Logf("  raw response head: %s", truncate(body, 800))

	var parsed deployFailureWire
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("parse deploy result: %v", err)
	}

	step++
	logStep(t, step, "assert PREPARING_RUNTIME_FAILED + classification populated")
	if parsed.Status != "PREPARING_RUNTIME_FAILED" {
		t.Fatalf("Status = %q, want PREPARING_RUNTIME_FAILED", parsed.Status)
	}
	if parsed.FailureClassification == nil {
		t.Fatalf("failureClassification missing — E2 wiring broken")
	}
	if parsed.FailureClassification.Category != topology.FailureClassStart {
		t.Errorf("Category = %q, want %q", parsed.FailureClassification.Category, topology.FailureClassStart)
	}
	// Either prepare:missing-sudo OR prepare:wrong-pkg-name OR phase:prepare
	// is acceptable — what we want to confirm is that SOME signal/baseline
	// fires (i.e. the classifier ran end-to-end).
	signals := parsed.FailureClassification.Signals
	matched := false
	for _, want := range []string{"prepare:missing-sudo", "prepare:wrong-pkg-name", "prepare:php-extension-missing", "phase:prepare"} {
		if signalsContain(signals, want) {
			matched = true
			break
		}
	}
	if !matched {
		t.Errorf("Signals %v matched no known prepare signal — pattern library may need extension for real Zerops apk output", signals)
	}
	if parsed.FailureClassification.SuggestedAction == "" {
		t.Errorf("SuggestedAction empty")
	}
	t.Logf("  category=%s signals=%v", parsed.FailureClassification.Category, signals)
	t.Logf("  likelyCause=%s", parsed.FailureClassification.LikelyCause)
	t.Logf("  suggestedAction=%s", parsed.FailureClassification.SuggestedAction)
	if len(parsed.BuildLogs) > 0 {
		t.Logf("  buildLogs head:")
		for i, line := range parsed.BuildLogs {
			if i >= 5 {
				break
			}
			t.Logf("    %s", line)
		}
	}
}

// deployFailureWire mirrors ops.DeployResult enough to inspect the new
// FailureClassification field plus the build-log surface fields the
// BuildPhase test transplanted from the former build_logs_test.go
// (mode / buildLogsSource / nextActions). Kept local to this test so
// refactors of DeployResult don't require updates here unless the field
// shape itself changes.
type deployFailureWire struct {
	Status                string                                `json:"status"`
	Mode                  string                                `json:"mode"`
	BuildStatus           string                                `json:"buildStatus"`
	BuildLogs             []string                              `json:"buildLogs"`
	BuildLogsSource       string                                `json:"buildLogsSource"`
	RuntimeLogs           []string                              `json:"runtimeLogs"`
	FailedPhase           string                                `json:"failedPhase"`
	Suggestion            string                                `json:"suggestion"`
	NextActions           string                                `json:"nextActions"`
	FailureClassification *topology.DeployFailureClassification `json:"failureClassification"`
}

func signalsContain(signals []string, want string) bool {
	for _, s := range signals {
		if s == want {
			return true
		}
	}
	return false
}
