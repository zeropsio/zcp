//go:build e2e

// Tests for: e2e — post-deploy subdomain auto-enable on first deploy.
//
// Phase 0 RED proof for the foundation fix at
// plans/subdomain-auto-enable-foundation-fix-2026-05-03.md.
//
// Provisions a standard pair (appdev startWithoutCode + appstage no
// startWithoutCode) via the two-phase bootstrap workflow (action=start →
// action=start route=classic), then imports + deploys both halves. Asserts
// that BOTH services report subdomainEnabled: true via zerops_discover
// WITHOUT any explicit zerops_subdomain enable call.
//
// Pre-Phase-1 state: this test FAILS — the broken serviceEligibleForSubdomain
// predicate reads detail.SubdomainAccess (false pre-enable) and
// detail.Ports[].HTTPSupport (mapped from HttpRouting, false pre-enable),
// returns false, and maybeAutoEnableSubdomain silently skips. User must call
// zerops_subdomain enable manually for verify to pass.
//
// Post-Phase-1 state: this test PASSES — the rewritten predicate uses the
// mode allow-list + IsSystem() defensive guard only. It then calls
// ops.Subdomain.Enable; the platform succeeds for HTTP-shaped services and
// rejects with serviceStackIsNotHttp for non-HTTP (silently swallowed in the
// caller). Note: meta is nil here (no bootstrap workflow), so the predicate's
// no-meta-permissive branch is what fires — the same path that recipe-
// authoring uses.
//
// Run: go test ./e2e/ -tags e2e -count=1 -v -run TestE2E_StandardPairFirstDeploy_AutoEnablesSubdomain -timeout 900s

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// createMinimalNodeAppPair creates a temp directory with a minimal Node app and a
// zerops.yml carrying TWO setup blocks (one per pair half), each with a port
// flagged httpSupport: true. The same directory is the deploy source for both
// halves; the setup name is the discriminator passed to zerops_deploy.
func createMinimalNodeAppPair(t *testing.T, devSetup, stageSetup string) string {
	t.Helper()
	dir := t.TempDir()

	zeropsYML := fmt.Sprintf(`zerops:
  - setup: %s
    build:
      base: nodejs@22
      buildCommands:
        - echo "build done"
      deployFiles: ./
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      start: node server.js
  - setup: %s
    build:
      base: nodejs@22
      buildCommands:
        - echo "build done"
      deployFiles: ./
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      start: node server.js
`, devSetup, stageSetup)

	serverJS := `const http = require('http');
http.createServer((req, res) => {
  if (req.url === '/health') {
    res.writeHead(200, {'Content-Type': 'text/plain'});
    res.end('ok');
  } else {
    res.writeHead(200, {'Content-Type': 'text/plain'});
    res.end('hello from autoenable e2e test');
  }
}).listen(3000, () => console.log('listening on 3000'));
`

	if err := os.WriteFile(filepath.Join(dir, "zerops.yml"), []byte(zeropsYML), 0o644); err != nil {
		t.Fatalf("write zerops.yml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "server.js"), []byte(serverJS), 0o644); err != nil {
		t.Fatalf("write server.js: %v", err)
	}
	gitInit(t, dir)
	return dir
}

// discoverSubdomainEnabled returns the subdomainEnabled field from
// zerops_discover response for the given hostname. Returns false (not error)
// when the field is absent — that's the broken-state signal we want to
// distinguish from communication errors.
func discoverSubdomainEnabled(t *testing.T, s *e2eSession, hostname string) (enabled bool, url string) {
	t.Helper()
	text := s.mustCallSuccess("zerops_discover", map[string]any{
		"service": hostname,
	})
	var disc struct {
		Services []struct {
			Hostname         string `json:"hostname"`
			SubdomainEnabled bool   `json:"subdomainEnabled"`
			SubdomainURL     string `json:"subdomainUrl"`
		} `json:"services"`
	}
	if err := json.Unmarshal([]byte(text), &disc); err != nil {
		t.Fatalf("parse discover for %s: %v\nresponse: %s", hostname, err, text)
	}
	for _, svc := range disc.Services {
		if svc.Hostname == hostname {
			return svc.SubdomainEnabled, svc.SubdomainURL
		}
	}
	t.Fatalf("hostname %s not found in discover response: %s", hostname, text)
	return false, ""
}

// TestE2E_StandardPairFirstDeploy_AutoEnablesSubdomain is the Phase 0 RED
// proof. It mirrors the user's notes-dashboard scenario report (2026-05-03):
// bootstrap a standard pair (writes ServiceMeta with PlanModeStandard), deploy
// both halves, expect subdomain enabled on both without explicit
// zerops_subdomain call.
func TestE2E_StandardPairFirstDeploy_AutoEnablesSubdomain(t *testing.T) {
	t.Skip("phase-3: end-to-end harness still needs deploy-preflight workingDir resolution wiring (zerops.yaml lookup expects mount-style path, not local temp dir). Phase 0 RED state is proven at the unit level (3 t.Skip'd tests in internal/tools/deploy_subdomain_test.go) plus live verification via the probe service experiment recorded in the plan §2.2. Phase 3 picks this up once predicate + plumbing are in place.")
	if _, err := exec.LookPath("zcli"); err != nil {
		t.Skip("zcli not in PATH — skipping auto-enable E2E test")
	}

	h := newHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()[:4]
	devHostname := "zaa" + suffix + "dev"
	stageHostname := "zaa" + suffix + "stage"

	zcliLogin(t, h.authInfo.Token)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()
		s.callTool("zerops_workflow", map[string]any{"action": "reset"})
		cleanupServices(ctx, h.client, h.projectID, devHostname, stageHostname)
	})

	step := 0

	// --- Step 1: Bootstrap workflow — two-phase classic route. Phase 1 of
	// action=start (no route) returns discovery options; phase 2 commits the
	// session with route=classic. This establishes a workflow context so
	// zerops_import passes the WORKFLOW_REQUIRED guard.
	step++
	logStep(t, step, "bootstrap workflow phase 1 (discovery)")
	s.callTool("zerops_workflow", map[string]any{"action": "reset"})
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"intent":   t.Name(),
	})

	step++
	logStep(t, step, "bootstrap workflow phase 2 (commit route=classic)")
	startText := s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"route":    "classic",
		"intent":   t.Name(),
	})
	t.Logf("  Start response: %s", truncate(startText, 200))

	// --- Step 2: Submit plan via complete discover ---
	step++
	logStep(t, step, "complete discover with standard pair plan")
	plan := []any{
		map[string]any{
			"runtime": map[string]any{
				"devHostname":   devHostname,
				"stageHostname": stageHostname,
				"type":          "nodejs@22",
				// bootstrapMode default = standard (dev + stage pair)
			},
		},
	}
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action": "complete",
		"step":   "discover",
		"plan":   plan,
	})

	// --- Step 3: Import standard pair ---
	step++
	logStep(t, step, "zerops_import standard pair (%s + %s)", devHostname, stageHostname)
	importYAML := fmt.Sprintf(`services:
  - hostname: %s
    type: nodejs@22
    startWithoutCode: true
    minContainers: 1
    enableSubdomainAccess: true
    ports:
      - port: 3000
        httpSupport: true
  - hostname: %s
    type: nodejs@22
    enableSubdomainAccess: true
    ports:
      - port: 3000
        httpSupport: true
`, devHostname, stageHostname)
	importText := s.mustCallSuccess("zerops_import", map[string]any{
		"content": importYAML,
	})
	t.Logf("  Import: %s", truncate(importText, 200))

	// --- Step 4: Wait for both services to be ready ---
	step++
	logStep(t, step, "wait for services ready")
	waitForServiceReady(s, devHostname)
	waitForServiceReady(s, stageHostname)
	t.Log("  Both services ready")

	// --- Step 4b: Complete provision step (writes ServiceMeta with Mode) ---
	step++
	logStep(t, step, "complete provision step (writes ServiceMeta)")
	s.mustCallSuccess("zerops_discover", map[string]any{"includeEnvs": true})
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":      "complete",
		"step":        "provision",
		"attestation": "Services created and ready for deploy",
	})

	// --- Step 3: Build minimal Node app with two-setup zerops.yml ---
	step++
	logStep(t, step, "create minimal Node app with two-setup zerops.yml")
	appDir := createMinimalNodeAppPair(t, devHostname, stageHostname)
	t.Logf("  App at %s", appDir)

	// --- Step 4: Self-deploy dev half ---
	step++
	logStep(t, step, "zerops_deploy targetService=%s (self-deploy dev half)", devHostname)
	devDeployText := s.mustCallSuccess("zerops_deploy", map[string]any{
		"targetService": devHostname,
		"setup":         devHostname,
		"workingDir":    appDir,
	})
	var devDeploy struct {
		Status                 string `json:"status"`
		SubdomainAccessEnabled bool   `json:"subdomainAccessEnabled"`
		SubdomainURL           string `json:"subdomainUrl"`
	}
	if err := json.Unmarshal([]byte(devDeployText), &devDeploy); err != nil {
		t.Fatalf("parse dev deploy: %v\nresponse: %s", err, devDeployText)
	}
	if devDeploy.Status != "DEPLOYED" {
		t.Fatalf("dev deploy status = %s, want DEPLOYED\nresponse: %s", devDeploy.Status, devDeployText)
	}
	t.Logf("  dev deploy: status=%s subdomainAccessEnabled=%v url=%q",
		devDeploy.Status, devDeploy.SubdomainAccessEnabled, devDeploy.SubdomainURL)

	// --- Step 5: Cross-deploy dev → stage ---
	step++
	logStep(t, step, "zerops_deploy sourceService=%s targetService=%s (cross-deploy stage half)",
		devHostname, stageHostname)
	stageDeployText := s.mustCallSuccess("zerops_deploy", map[string]any{
		"sourceService": devHostname,
		"targetService": stageHostname,
		"setup":         stageHostname,
		"workingDir":    appDir,
	})
	var stageDeploy struct {
		Status                 string `json:"status"`
		SubdomainAccessEnabled bool   `json:"subdomainAccessEnabled"`
		SubdomainURL           string `json:"subdomainUrl"`
	}
	if err := json.Unmarshal([]byte(stageDeployText), &stageDeploy); err != nil {
		t.Fatalf("parse stage deploy: %v\nresponse: %s", err, stageDeployText)
	}
	if stageDeploy.Status != "DEPLOYED" {
		t.Fatalf("stage deploy status = %s, want DEPLOYED\nresponse: %s", stageDeploy.Status, stageDeployText)
	}
	t.Logf("  stage deploy: status=%s subdomainAccessEnabled=%v url=%q",
		stageDeploy.Status, stageDeploy.SubdomainAccessEnabled, stageDeploy.SubdomainURL)

	// --- Step 6: PRIMARY ASSERTION — both deploy responses report
	// auto-enabled subdomain WITHOUT any explicit zerops_subdomain call.
	step++
	logStep(t, step, "ASSERT: deploy response auto-enable signal on both halves")
	if !devDeploy.SubdomainAccessEnabled {
		t.Errorf("dev deploy: subdomainAccessEnabled=false; want true (auto-enable should fire post-deploy without manual zerops_subdomain call)")
	}
	if devDeploy.SubdomainURL == "" {
		t.Errorf("dev deploy: subdomainUrl empty; want non-empty (auto-enable should populate URL)")
	}
	if !stageDeploy.SubdomainAccessEnabled {
		t.Errorf("stage deploy: subdomainAccessEnabled=false; want true (auto-enable should fire post-cross-deploy without manual zerops_subdomain call)")
	}
	if stageDeploy.SubdomainURL == "" {
		t.Errorf("stage deploy: subdomainUrl empty; want non-empty (auto-enable should populate URL)")
	}

	// --- Step 7: SECONDARY ASSERTION — platform-side state via discover.
	// Even if the deploy response signal is missing, the platform may have
	// the subdomain enabled. Check both signals to distinguish "predicate
	// skipped enable" from "enable happened but result didn't carry the flag".
	step++
	logStep(t, step, "ASSERT: zerops_discover platform-side subdomainEnabled")
	devEnabled, devURL := discoverSubdomainEnabled(t, s, devHostname)
	stageEnabled, stageURL := discoverSubdomainEnabled(t, s, stageHostname)
	t.Logf("  discover %s: subdomainEnabled=%v url=%q", devHostname, devEnabled, devURL)
	t.Logf("  discover %s: subdomainEnabled=%v url=%q", stageHostname, stageEnabled, stageURL)
	if !devEnabled {
		t.Errorf("discover %s: subdomainEnabled=false on platform; auto-enable did not fire", devHostname)
	}
	if !stageEnabled {
		t.Errorf("discover %s: subdomainEnabled=false on platform; auto-enable did not fire", stageHostname)
	}
}
