//go:build e2e

// Tests for: e2e — bootstrap workflow against real Zerops API.
//
// Tests both fresh and incremental bootstrap flows, verifying:
// - Workflow state transitions and step completion
// - Evidence file generation
// - Provision checker handles existing runtimes (IsExisting=true)
// - Service creation and env var discovery
//
// Run: go test ./e2e/ -tags e2e -count=1 -v -run TestE2E_Bootstrap -timeout 600s

package e2e_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// bootstrapProgress captures workflow step status.
type bootstrapProgress struct {
	SessionID string `json:"sessionId"`
	Intent    string `json:"intent"`
	Progress  struct {
		Total     int `json:"total"`
		Completed int `json:"completed"`
		Steps     []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"steps"`
	} `json:"progress"`
	Current *struct {
		Name     string `json:"name"`
		Index    int    `json:"index"`
		PlanMode string `json:"planMode,omitempty"`
	} `json:"current,omitempty"`
	CheckResult *struct {
		Passed  bool `json:"passed"`
		Summary string `json:"summary"`
		Checks  []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
			Detail string `json:"detail,omitempty"`
		} `json:"checks,omitempty"`
	} `json:"checkResult,omitempty"`
	Message string `json:"message"`
}

func TestE2E_BootstrapFresh_FullFlow(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()[:4] // keep short to stay under 25 chars
	devHostname := "bs" + suffix + "dev"
	stageHostname := "bs" + suffix + "stage"
	dbHostname := "bsdb" + suffix

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		// Reset workflow before cleanup.
		s.callTool("zerops_workflow", map[string]any{"action": "reset"})
		cleanupServices(ctx, h.client, h.projectID, devHostname, stageHostname, dbHostname)
	})

	step := 0

	// --- Step 1: Start bootstrap workflow ---
	step++
	logStep(t, step, "start bootstrap workflow")
	s.callTool("zerops_workflow", map[string]any{"action": "reset"})
	startText := s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"intent":   "e2e bootstrap test — fresh runtime + db",
	})
	var startResp bootstrapProgress
	if err := json.Unmarshal([]byte(startText), &startResp); err != nil {
		t.Fatalf("parse bootstrap start: %v", err)
	}
	if startResp.SessionID == "" {
		t.Fatal("expected non-empty sessionId")
	}
	if startResp.Progress.Total != 5 {
		t.Errorf("expected 5 steps, got %d", startResp.Progress.Total)
	}
	if startResp.Current == nil || startResp.Current.Name != "discover" {
		t.Fatal("expected current step to be 'discover'")
	}
	t.Logf("  Session: %s, current step: %s", startResp.SessionID, startResp.Current.Name)

	// --- Step 2: Complete discover with structured plan ---
	step++
	logStep(t, step, "complete discover with plan")
	planJSON := []any{
		map[string]any{
			"runtime": map[string]any{
				"devHostname": devHostname,
				"type":        "nodejs@22",
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
	discoverText := s.mustCallSuccess("zerops_workflow", map[string]any{
		"action": "complete",
		"step":   "discover",
		"plan":   planJSON,
	})
	var discoverResp bootstrapProgress
	if err := json.Unmarshal([]byte(discoverText), &discoverResp); err != nil {
		t.Fatalf("parse discover complete: %v", err)
	}
	if discoverResp.Progress.Completed != 1 {
		t.Errorf("expected 1 completed step after discover, got %d", discoverResp.Progress.Completed)
	}
	if discoverResp.Current == nil || discoverResp.Current.Name != "provision" {
		t.Fatal("expected current step to advance to 'provision'")
	}
	t.Logf("  Plan mode: %s, current step: %s", discoverResp.Current.PlanMode, discoverResp.Current.Name)

	// --- Step 3: Import services ---
	step++
	logStep(t, step, "import services")
	importYAML := "services:\n" +
		"  - hostname: " + dbHostname + "\n" +
		"    type: postgresql@16\n" +
		"    mode: NON_HA\n" +
		"    priority: 10\n" +
		"  - hostname: " + devHostname + "\n" +
		"    type: nodejs@22\n" +
		"    startWithoutCode: true\n" +
		"    minContainers: 1\n" +
		"    enableSubdomainAccess: true\n" +
		"  - hostname: " + stageHostname + "\n" +
		"    type: nodejs@22\n" +
		"    enableSubdomainAccess: true\n"

	importText := s.mustCallSuccess("zerops_import", map[string]any{
		"content": importYAML,
	})
	t.Logf("  Import result: %s", truncate(importText, 200))

	// Wait for dev to be RUNNING (startWithoutCode=true), others just need to exist.
	waitForServiceStatus(s, devHostname, "RUNNING", "ACTIVE")
	waitForServiceReady(s, stageHostname)
	waitForServiceReady(s, dbHostname)

	// --- Step 4: Discover env vars ---
	step++
	logStep(t, step, "discover env vars")
	envText := s.mustCallSuccess("zerops_discover", map[string]any{
		"includeEnvs": true,
	})
	if !strings.Contains(envText, dbHostname) {
		t.Errorf("db service %s not found in discover", dbHostname)
	}

	// --- Step 5: Complete provision step ---
	step++
	logStep(t, step, "complete provision step")
	provText := s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":      "complete",
		"step":        "provision",
		"attestation": "All services created. Dev and stage runtimes ready. DB env vars discovered.",
	})
	var provResp bootstrapProgress
	if err := json.Unmarshal([]byte(provText), &provResp); err != nil {
		t.Fatalf("parse provision complete: %v", err)
	}

	// Verify provision check result is populated and passed.
	assertProvisionPassed(t, provResp)
	assertHasStageCheck(t, provResp, stageHostname)

	if provResp.Progress.Completed != 2 {
		t.Errorf("expected 2 completed steps after provision, got %d", provResp.Progress.Completed)
	}
	if provResp.Current == nil || provResp.Current.Name != "generate" {
		t.Errorf("expected current step 'generate', got %v", provResp.Current)
	}
	t.Logf("  Provision complete, current step: %s", provResp.Current.Name)

	// Remaining steps (generate/deploy/close) require actual code deployment
	// which is beyond the scope of this workflow orchestration test.
	t.Log("  Fresh bootstrap provision validated successfully — workflow orchestration correct")
}

func TestE2E_BootstrapIncremental_ExistingRuntime(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()[:4] // keep short to stay under 25 chars
	devHostname := "in" + suffix + "dev"
	stageHostname := "in" + suffix + "stage"
	dbHostname := "indb" + suffix
	cacheHostname := "inc" + suffix

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		s.callTool("zerops_workflow", map[string]any{"action": "reset"})
		cleanupServices(ctx, h.client, h.projectID, devHostname, stageHostname, dbHostname, cacheHostname)
	})

	step := 0

	// --- Phase 1: Run initial bootstrap to create services ---
	step++
	logStep(t, step, "initial bootstrap — create runtime + db")
	s.callTool("zerops_workflow", map[string]any{"action": "reset"})
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"intent":   "e2e phase 1 — initial services for incremental test",
	})

	// Complete discover with plan.
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action": "complete",
		"step":   "discover",
		"plan": []any{
			map[string]any{
				"runtime": map[string]any{
					"devHostname": devHostname,
					"type":        "nodejs@22",
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
		},
	})

	// Import services. Both dev and stage use startWithoutCode so they become ACTIVE
	// immediately (simulating a previously-deployed project for Phase 2 incremental test).
	importYAML := "services:\n" +
		"  - hostname: " + dbHostname + "\n" +
		"    type: postgresql@16\n" +
		"    mode: NON_HA\n" +
		"  - hostname: " + devHostname + "\n" +
		"    type: nodejs@22\n" +
		"    startWithoutCode: true\n" +
		"    minContainers: 1\n" +
		"    enableSubdomainAccess: true\n" +
		"  - hostname: " + stageHostname + "\n" +
		"    type: nodejs@22\n" +
		"    startWithoutCode: true\n" +
		"    enableSubdomainAccess: true\n"

	s.mustCallSuccess("zerops_import", map[string]any{"content": importYAML})
	waitForServiceStatus(s, devHostname, "RUNNING", "ACTIVE")
	waitForServiceStatus(s, stageHostname, "RUNNING", "ACTIVE")
	waitForServiceReady(s, dbHostname)

	// Complete provision.
	s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":      "complete",
		"step":        "provision",
		"attestation": "Phase 1: all services created and running with env vars.",
	})

	// Phase 1 only needs provision to succeed (services created).
	// Reset and start fresh for Phase 2 — the services are now ACTIVE/RUNNING.
	s.callTool("zerops_workflow", map[string]any{"action": "reset"})
	t.Log("  Phase 1 services created — dev/stage/db are now ACTIVE")

	// --- Phase 2: Incremental bootstrap — add cache to existing runtime ---
	step++
	logStep(t, step, "start incremental bootstrap")
	s.callTool("zerops_workflow", map[string]any{"action": "reset"})
	startText := s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "bootstrap",
		"intent":   "e2e incremental — add valkey to existing runtime",
	})
	var startResp bootstrapProgress
	if err := json.Unmarshal([]byte(startText), &startResp); err != nil {
		t.Fatalf("parse start: %v", err)
	}
	if startResp.Current.Name != "discover" {
		t.Fatalf("expected discover step, got %s", startResp.Current.Name)
	}

	// --- Step 3: Complete discover with IsExisting runtime ---
	step++
	logStep(t, step, "complete discover with IsExisting=true plan")
	planJSON := []any{
		map[string]any{
			"runtime": map[string]any{
				"devHostname": devHostname,
				"type":        "nodejs@22",
				"isExisting":  true,
			},
			"dependencies": []any{
				map[string]any{
					"hostname":   dbHostname,
					"type":       "postgresql@16",
					"resolution": "EXISTS",
				},
				map[string]any{
					"hostname":   cacheHostname,
					"type":       "valkey@7.2",
					"mode":       "NON_HA",
					"resolution": "CREATE",
				},
			},
		},
	}
	discoverText := s.mustCallSuccess("zerops_workflow", map[string]any{
		"action": "complete",
		"step":   "discover",
		"plan":   planJSON,
	})
	var discoverResp bootstrapProgress
	if err := json.Unmarshal([]byte(discoverText), &discoverResp); err != nil {
		t.Fatalf("parse discover: %v", err)
	}
	if discoverResp.Current.Name != "provision" {
		t.Fatalf("expected provision step, got %s", discoverResp.Current.Name)
	}

	// --- Step 4: Import new cache service (within active bootstrap session) ---
	step++
	logStep(t, step, "import new cache service")
	cacheYAML := "services:\n" +
		"  - hostname: " + cacheHostname + "\n" +
		"    type: valkey@7.2\n" +
		"    mode: NON_HA\n"

	s.mustCallSuccess("zerops_import", map[string]any{"content": cacheYAML})
	waitForServiceStatus(s, cacheHostname, "RUNNING", "ACTIVE")
	t.Log("  Cache service created and running")

	// --- Step 5: Complete provision — THIS IS THE KEY TEST ---
	// The stage service (stageHostname) is already ACTIVE from Phase 1.
	// With IsExisting=true, the provision checker should accept ACTIVE stage.
	step++
	logStep(t, step, "complete provision (stage is ACTIVE, IsExisting=true)")
	provText := s.mustCallSuccess("zerops_workflow", map[string]any{
		"action":      "complete",
		"step":        "provision",
		"attestation": "Incremental bootstrap: cache added. Existing dev/stage both ACTIVE. DB has env vars.",
	})
	var provResp bootstrapProgress
	if err := json.Unmarshal([]byte(provText), &provResp); err != nil {
		t.Fatalf("parse provision: %v", err)
	}

	// Provision must pass for incremental bootstrap.
	assertProvisionPassed(t, provResp)

	if provResp.Progress.Completed < 2 {
		t.Fatalf("expected at least 2 completed steps, got %d (provision may not have advanced)",
			provResp.Progress.Completed)
	}
	if provResp.Current != nil && provResp.Current.Name != "generate" {
		t.Errorf("expected current step 'generate' after provision, got %s", provResp.Current.Name)
	}
	t.Log("  Provision completed — incremental bootstrap with existing ACTIVE stage PASSED")

	// Verify env vars were discovered for BOTH EXISTS (db) and CREATE (cache) deps.
	assertEnvVarCheck(t, provResp, dbHostname)
	assertEnvVarCheck(t, provResp, cacheHostname)
	t.Log("  This validates: EXISTS deps get env vars discovered alongside CREATE deps")
}

// waitForServiceStatus polls until a service reaches one of the expected statuses.
func waitForServiceStatus(s *e2eSession, hostname string, statuses ...string) {
	s.t.Helper()
	statusSet := make(map[string]bool, len(statuses))
	for _, st := range statuses {
		statusSet[st] = true
	}
	for i := 0; i < maxPollAttempts; i++ {
		text := s.mustCallSuccess("zerops_discover", nil)
		var result struct {
			Services []struct {
				Hostname string `json:"hostname"`
				Status   string `json:"status"`
			} `json:"services"`
		}
		if err := json.Unmarshal([]byte(text), &result); err == nil {
			for _, svc := range result.Services {
				if svc.Hostname == hostname && statusSet[svc.Status] {
					return
				}
			}
		}
		time.Sleep(pollInterval)
	}
	s.t.Fatalf("service %s did not reach status %v after %d attempts", hostname, statuses, maxPollAttempts)
}

// truncate shortens a string for logging.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
