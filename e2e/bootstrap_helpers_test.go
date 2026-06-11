//go:build e2e

// Tests for: e2e — shared helpers for bootstrap workflow E2E tests.

package e2e_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// importService defines a service for import YAML generation.
type importService struct {
	Hostname         string
	Type             string
	Mode             string // NON_HA, HA (managed only)
	StartWithoutCode bool
	MinContainers    int
	EnableSubdomain  bool
	ObjStorageSize   int // only for object-storage
	Priority         int
}

// buildImportYAML constructs import YAML from test service entries.
func buildImportYAML(services []importService) string {
	var b strings.Builder
	b.WriteString("services:\n")
	for _, svc := range services {
		b.WriteString("  - hostname: " + svc.Hostname + "\n")
		b.WriteString("    type: " + svc.Type + "\n")
		if svc.Mode != "" {
			b.WriteString("    mode: " + svc.Mode + "\n")
		}
		if svc.StartWithoutCode {
			b.WriteString("    startWithoutCode: true\n")
		}
		if svc.MinContainers > 0 {
			b.WriteString(fmt.Sprintf("    minContainers: %d\n", svc.MinContainers))
		}
		if svc.EnableSubdomain {
			b.WriteString("    enableSubdomainAccess: true\n")
		}
		if svc.ObjStorageSize > 0 {
			b.WriteString(fmt.Sprintf("    objectStorageSize: %d\n", svc.ObjStorageSize))
		}
		if svc.Priority > 0 {
			b.WriteString(fmt.Sprintf("    priority: %d\n", svc.Priority))
		}
	}
	return b.String()
}

// bootstrapAndProvision runs a full bootstrap flow through provision completion.
// Steps: reset → start (two-phase: discover + classic commit) → complete discover (plan)
// → import → wait → complete provision. Returns parsed bootstrapProgress.
//
// Bootstrap start is two-phase: Phase 1 (no route) returns route options without
// committing a session; Phase 2 (route=classic) commits and returns sessionId +
// progress. See plans/backlog/e2e-bootstrap-helper-two-phase-wiring.md.
func bootstrapAndProvision(t *testing.T, s *e2eSession, plan []any, importYAML string, waitHostnames []string) bootstrapProgress {
	t.Helper()

	// Reset and start bootstrap.
	s.callTool("zerops_workflow", map[string]any{"action": "reset"})

	// Phase 1: discovery (no route) — establishes workflow context; response
	// is BootstrapDiscoveryResponse (no sessionId), so we drop it.
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"intent":   t.Name(),
	})
	// Phase 2: commit with route=classic — returns standard progress envelope.
	startText := s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"route":    "classic",
		"intent":   t.Name(),
	})
	var startResp bootstrapProgress
	if err := json.Unmarshal([]byte(startText), &startResp); err != nil {
		t.Fatalf("parse bootstrap start (phase 2): %v", err)
	}
	if startResp.SessionID == "" {
		t.Fatal("phase-2 commit must return sessionId")
	}
	if startResp.Current == nil || startResp.Current.Name != "discover" {
		t.Fatal("expected current step to be 'discover'")
	}

	// Complete discover with plan.
	discoverText := s.mustCallSuccess("zerops_workflow", map[string]any{
		"action": "complete",
		"step":   "discover",
		"plan":   plan,
	})
	var discoverResp bootstrapProgress
	if err := json.Unmarshal([]byte(discoverText), &discoverResp); err != nil {
		t.Fatalf("parse discover complete: %v", err)
	}
	if discoverResp.Current == nil || discoverResp.Current.Name != "provision" {
		t.Fatal("expected current step to advance to 'provision'")
	}

	// Import services.
	importText := s.mustCallSuccess("zerops_import", map[string]any{
		"content": importYAML,
	})
	t.Logf("  Import result: %s", truncate(importText, 200))

	// Wait for all services — runtime services need RUNNING/ACTIVE,
	// managed services just need to exist with any status.
	for _, wh := range waitHostnames {
		waitForServiceStatus(s, wh, "RUNNING", "ACTIVE", "NEW", "READY_TO_DEPLOY")
	}

	// Discover env vars — the provision checker needs this to verify managed service env vars.
	s.mustCallSuccess("zerops_discover", map[string]any{"includeEnvs": true})

	// Complete provision step.
	provText := s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":      "complete",
		"step":        "provision",
		"attestation": "All services created and env vars discovered.",
	})
	var provResp bootstrapProgress
	if err := json.Unmarshal([]byte(provText), &provResp); err != nil {
		t.Fatalf("parse provision complete: %v", err)
	}

	// Verify workflow progress after provision. Bootstrap-core is 3 steps
	// (discover → provision → close): generate/deploy/verify moved to the
	// develop workflow when bootstrap narrowed to infrastructure-only.
	if provResp.Progress.Completed != 2 {
		t.Errorf("expected 2 completed steps (discover + provision), got %d", provResp.Progress.Completed)
	}
	if provResp.Current == nil || provResp.Current.Name != "close" {
		t.Errorf("expected current step 'close' after provision, got %v", provResp.Current)
	}

	return provResp
}

// bootstrapAndProvisionExpectFail runs the bootstrap flow but expects provision to fail.
// Returns the provision response with a failed checkResult.
func bootstrapAndProvisionExpectFail(t *testing.T, s *e2eSession, plan []any, importYAML string, waitHostnames []string) bootstrapProgress {
	t.Helper()

	// Reset and start bootstrap (two-phase — see bootstrapAndProvision godoc).
	s.callTool("zerops_workflow", map[string]any{"action": "reset"})
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"intent":   t.Name(),
	})
	startText := s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"route":    "classic",
		"intent":   t.Name(),
	})
	var startResp bootstrapProgress
	if err := json.Unmarshal([]byte(startText), &startResp); err != nil {
		t.Fatalf("parse bootstrap start (phase 2): %v", err)
	}
	_ = startResp

	// Complete discover with plan.
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action": "complete",
		"step":   "discover",
		"plan":   plan,
	})

	// Import services.
	s.mustCallSuccess("zerops_import", map[string]any{
		"content": importYAML,
	})

	// Wait for services to exist.
	for _, wh := range waitHostnames {
		waitForServiceStatus(s, wh, "RUNNING", "ACTIVE", "NEW", "READY_TO_DEPLOY")
	}

	// Discover env vars.
	s.mustCallSuccess("zerops_discover", map[string]any{"includeEnvs": true})

	// Complete provision step — expect it returns but check fails.
	provText := s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":      "complete",
		"step":        "provision",
		"attestation": "Expecting provision to fail.",
	})
	var provResp bootstrapProgress
	if err := json.Unmarshal([]byte(provText), &provResp); err != nil {
		t.Fatalf("parse provision complete: %v", err)
	}
	return provResp
}

// assertProvisionFailed verifies provision check failed and logs details.
func assertProvisionFailed(t *testing.T, resp bootstrapProgress) {
	t.Helper()
	if resp.CheckResult == nil {
		t.Fatal("expected checkResult in provision response (got nil)")
	}
	if resp.CheckResult.Passed {
		t.Logf("  unexpected pass: %s", resp.CheckResult.Summary)
		for _, c := range resp.CheckResult.Checks {
			t.Logf("    check %s: %s %s", c.Name, c.Status, c.Detail)
		}
		t.Fatal("expected provision check to fail, but it passed")
	}
}

// assertNoStageCheck verifies no stage_status check exists (for simple/dev modes).
func assertNoStageCheck(t *testing.T, resp bootstrapProgress) {
	t.Helper()
	if resp.CheckResult == nil {
		t.Fatal("expected checkResult in provision response (got nil)")
	}
	for _, c := range resp.CheckResult.Checks {
		if strings.HasSuffix(c.Name, "stage_status") {
			t.Errorf("unexpected stage check %q in simple/dev mode", c.Name)
		}
	}
}

// assertHasStageCheck verifies a stage_status check exists for standard mode.
func assertHasStageCheck(t *testing.T, resp bootstrapProgress, stageHostname string) {
	t.Helper()
	if resp.CheckResult == nil {
		t.Fatal("expected checkResult in provision response (got nil)")
	}
	checkName := stageHostname + "_status"
	for _, c := range resp.CheckResult.Checks {
		if c.Name == checkName {
			if c.Status != "pass" {
				t.Errorf("stage check %s: expected pass, got %s (%s)", checkName, c.Status, c.Detail)
			}
			return
		}
	}
	t.Errorf("stage check %s not found in provision checks", checkName)
}

// assertProvisionPassed fatals if provision check is missing or failed.
func assertProvisionPassed(t *testing.T, resp bootstrapProgress) {
	t.Helper()
	if resp.CheckResult == nil {
		t.Fatal("expected checkResult in provision response (got nil)")
	}
	if !resp.CheckResult.Passed {
		t.Errorf("provision check failed: %s", resp.CheckResult.Summary)
		for _, c := range resp.CheckResult.Checks {
			t.Logf("  check %s: %s %s", c.Name, c.Status, c.Detail)
		}
		t.Fatal("provision step check must pass")
	}
}

// assertEnvVarCheck verifies a specific hostname's env_vars check exists and passed.
func assertEnvVarCheck(t *testing.T, resp bootstrapProgress, hostname string) {
	t.Helper()
	if resp.CheckResult == nil {
		t.Fatalf("expected checkResult for env var verification of %s (got nil)", hostname)
	}
	checkName := hostname + "_env_vars"
	for _, c := range resp.CheckResult.Checks {
		if c.Name == checkName {
			if c.Status != "pass" {
				t.Errorf("env var check %s: expected pass, got %s (%s)", checkName, c.Status, c.Detail)
			}
			return
		}
	}
	t.Errorf("env var check %s not found in provision checks", checkName)
}

// assertNoEnvVarCheck verifies no env_vars check exists for a hostname (storage types).
func assertNoEnvVarCheck(t *testing.T, resp bootstrapProgress, hostname string) {
	t.Helper()
	if resp.CheckResult == nil {
		return
	}
	checkName := hostname + "_env_vars"
	for _, c := range resp.CheckResult.Checks {
		if c.Name == checkName {
			t.Errorf("unexpected env var check for %s (storage types should have none)", hostname)
			return
		}
	}
}

// bootstrapDevServiceForDeploy provisions ONE dev-mode runtime through the
// full bootstrap-core flow (discover plan → import → provision → close) so
// the service ends up ZCP-ADOPTED — the deploy gate refuses un-adopted
// targets (ADOPT_REQUIRED) since the adopt-gate shipped. importYAML may
// carry extra managed services; deps lists them for the plan (resolution
// CREATE). Returns after close completes (metas written).
func bootstrapDevServiceForDeploy(t *testing.T, s *e2eSession, devHostname, serviceType, importYAML string, deps []any) {
	t.Helper()

	s.callTool("zerops_workflow", map[string]any{"action": "reset"})
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"intent":   t.Name(),
	})
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"route":    "classic",
		"intent":   t.Name(),
	})

	runtimeSpec := map[string]any{
		"devHostname":   devHostname,
		"type":          serviceType,
		"bootstrapMode": "dev",
	}
	target := map[string]any{"runtime": runtimeSpec}
	if len(deps) > 0 {
		target["dependencies"] = deps
	}
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action": "complete",
		"step":   "discover",
		"plan":   []any{target},
	})

	s.mustCallSuccess("zerops_import", map[string]any{"content": importYAML})
	// Dev-mode runtimes must be LIVE for provision to pass (the mutable
	// container is the working surface) — callers import them with
	// startWithoutCode: true.
	waitForServiceStatus(s, devHostname, "RUNNING", "ACTIVE")
	s.mustCallSuccess("zerops_discover", map[string]any{"includeEnvs": true})

	provText := s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":      "complete",
		"step":        "provision",
		"attestation": "Service created for deploy e2e.",
	})
	var provResp bootstrapProgress
	if err := json.Unmarshal([]byte(provText), &provResp); err != nil {
		t.Fatalf("parse provision complete: %v", err)
	}
	// A failed check returns success-shaped JSON without advancing the
	// step — assert it here so the close call below doesn't fail with an
	// opaque step mismatch.
	assertProvisionPassed(t, provResp)

	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":      "complete",
		"step":        "close",
		"attestation": "Bootstrap closed for deploy e2e.",
	})
}

// bootstrapPairForDeploy is the standard-pair sibling of
// bootstrapDevServiceForDeploy: one dev/stage pair through discover →
// import → provision → close so BOTH halves end up adopted (cross-deploy
// targets the stage half; the deploy gate needs the pair meta).
func bootstrapPairForDeploy(t *testing.T, s *e2eSession, devHostname, stageHostname, serviceType, importYAML string) {
	t.Helper()

	s.callTool("zerops_workflow", map[string]any{"action": "reset"})
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"intent":   t.Name(),
	})
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"route":    "classic",
		"intent":   t.Name(),
	})
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action": "complete",
		"step":   "discover",
		"plan": []any{map[string]any{
			"runtime": map[string]any{
				"devHostname":   devHostname,
				"stageHostname": stageHostname,
				"type":          serviceType,
				"bootstrapMode": "standard",
			},
		}},
	})

	s.mustCallSuccess("zerops_import", map[string]any{"content": importYAML})
	waitForServiceStatus(s, devHostname, "RUNNING", "ACTIVE")
	waitForServiceStatus(s, stageHostname, "RUNNING", "ACTIVE", "NEW", "READY_TO_DEPLOY")
	s.mustCallSuccess("zerops_discover", map[string]any{"includeEnvs": true})

	provText := s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":      "complete",
		"step":        "provision",
		"attestation": "Pair created for deploy e2e.",
	})
	var provResp bootstrapProgress
	if err := json.Unmarshal([]byte(provText), &provResp); err != nil {
		t.Fatalf("parse provision complete: %v", err)
	}
	assertProvisionPassed(t, provResp)

	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":      "complete",
		"step":        "close",
		"attestation": "Bootstrap closed for deploy e2e.",
	})
}

// pushDirViaSSH copies a locally created app directory into the target
// container (tar over ssh) and git-initializes it — the shape SSH
// self-deploy needs (it reads the SOURCE service's filesystem; a local
// Mac path in workingDir is meaningless on the container).
func pushDirViaSSH(t *testing.T, hostname, localDir, targetDir string) {
	t.Helper()
	tarCmd := exec.Command("tar", "-C", localDir, "-czf", "-", ".")
	archive, err := tarCmd.Output()
	if err != nil {
		t.Fatalf("tar %s: %v", localDir, err)
	}
	b64 := base64.StdEncoding.EncodeToString(archive)
	cmd := fmt.Sprintf("mkdir -p %s && echo %s | base64 -d | tar -xzf - -C %s", targetDir, b64, targetDir)
	if out, err := sshExec(t, hostname, cmd); err != nil {
		t.Fatalf("push dir to %s:%s: %s (%v)", hostname, targetDir, out, err)
	}
	gitCmd := fmt.Sprintf(`cd %s && git init -q -b main 2>/dev/null; git config user.email 'test@test.com' && git config user.name 'test' && git add -A && git diff-index --quiet HEAD 2>/dev/null || git commit -q -m 'e2e app'`, targetDir)
	if out, err := sshExec(t, hostname, gitCmd); err != nil {
		t.Fatalf("git init on %s: %s (%v)", hostname, out, err)
	}
}
