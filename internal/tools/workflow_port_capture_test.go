package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/recipe"
	"github.com/zeropsio/zcp/internal/workflow"
)

// portFeasibleHardenArgs builds a full-HA harden call with the glue-repo
// readiness flag set as requested.
func portFeasibleHardenArgs(glueURL string, ready bool) map[string]any {
	return map[string]any{
		"action":   "harden",
		"workflow": "port",
		"portRubric": map[string]any{
			"buildSucceeded":      true,
			"reachedActive":       true,
			"stableAfterHold":     true,
			"httpRootPassed":      true,
			"coreFlowProbePassed": true,
			"harden": map[string]any{
				"sentinelSurvivedRedeploy": true,
				"sentinelOnDurableSurface": true,
				"appContainers":            2,
				"managedDepsHa":            true,
				"haVerifyPassed":           true,
			},
		},
		"portGlueRepo": map[string]any{
			"url":               glueURL,
			"committedSha":      "abc123",
			"buildFromGitReady": ready,
		},
	}
}

// TestPortCapture_Feasible_EmitsAndPublishesDryRun: a feasible, harden-scored
// port with a buildFromGit-ready glue repo emits the honored-tier subset to the
// output dir AND drives the curated two-channel publish (dry-run).
// stubPortPublisher installs a hermetic publish seam (no live gh calls) for the
// duration of a test, restoring the live one on cleanup. It records the dryRun
// it was handed and reports dry-run on every channel so the wiring is asserted
// without touching the network.
func stubPortPublisher(t *testing.T) *portPublishResult {
	t.Helper()
	captured := &portPublishResult{}
	prev := portPublisher
	portPublisher = func(_ string, plan *recipe.Plan, _ workflow.FitCeiling, _ string, dryRun bool) portPublishResult {
		status := "created"
		if dryRun {
			status = "dry-run"
		}
		*captured = portPublishResult{
			AppRepo:    "zerops-recipe-apps/" + plan.Slug + "-app",
			DryRun:     dryRun,
			CreateRepo: status,
			PushApp:    status,
			Publish:    status,
		}
		return *captured
	}
	t.Cleanup(func() { portPublisher = prev })
	return captured
}

func TestPortCapture_Feasible_EmitsAndPublishesDryRun(t *testing.T) {
	srv, dir := portTestServer(t)
	startPort(t, srv)
	stubPortPublisher(t)

	hres := callTool(t, srv, "zerops_workflow",
		portFeasibleHardenArgs("https://github.com/zerops-recipe-apps/strapi-app.git", true))
	if hres.IsError {
		t.Fatalf("harden failed: %s", getTextContent(t, hres))
	}

	res := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":            "capture",
		"workflow":          "port",
		"portPublishDryRun": true,
	})
	if res.IsError {
		t.Fatalf("capture failed: %s", getTextContent(t, res))
	}
	body := decodePortJSON(t, res)
	if body["status"] != "port-capture" {
		t.Errorf("status = %v, want port-capture", body["status"])
	}
	if body["published"] != true {
		t.Errorf("buildFromGit-ready glue → published=true, got %v", body["published"])
	}
	if body["deferred"] == true {
		t.Errorf("buildFromGit-ready glue must NOT defer, got deferred=%v", body["deferred"])
	}
	if body["dryRun"] != true {
		t.Errorf("dryRun should round-trip true, got %v", body["dryRun"])
	}

	// Emitted output dir + honored tiers on disk. Full-HA rubric → all 6 honored.
	outputDir, _ := body["outputDir"].(string)
	if outputDir == "" {
		t.Fatalf("no outputDir in response")
	}
	envRoot := filepath.Join(outputDir, "environments")
	for _, folder := range []string{
		"0 — AI Agent", "5 — Highly-available Production",
	} {
		yamlPath := filepath.Join(envRoot, folder, "import.yaml")
		data, err := os.ReadFile(yamlPath)
		if err != nil {
			t.Fatalf("expected emitted import.yaml at %s: %v", yamlPath, err)
		}
		// The D6 glue override is canonicalized (.git stripped) into buildFromGit.
		if !strings.Contains(string(data), "buildFromGit: https://github.com/zerops-recipe-apps/strapi-app\n") {
			t.Errorf("%s missing canonicalized glue buildFromGit:\n%s", folder, data)
		}
	}

	// App README carries the recipe-level fragment markers + authored bodies.
	appReadme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	if err != nil {
		t.Fatalf("expected app README: %v", err)
	}
	for _, marker := range []string{
		"#ZEROPS_EXTRACT_START:description#",
		"#ZEROPS_EXTRACT_START:features#",
		"#ZEROPS_EXTRACT_START:takeover-guide#",
		"#ZEROPS_EXTRACT_START:knowledge-base#",
	} {
		if !strings.Contains(string(appReadme), marker) {
			t.Errorf("app README missing fragment marker %q:\n%s", marker, appReadme)
		}
	}

	// Publish summary names the curated app repo.
	pub, ok := body["publish"].(map[string]any)
	if !ok {
		t.Fatalf("expected publish summary, got %T", body["publish"])
	}
	if !strings.Contains(pub["appRepo"].(string), "zerops-recipe-apps/strapi-app") {
		t.Errorf("appRepo = %v, want curated zerops-recipe-apps/strapi-app", pub["appRepo"])
	}
	if pub["createRepo"] != "dry-run" || pub["pushApp"] != "dry-run" || pub["publish"] != "dry-run" {
		t.Errorf("dry-run publish must report dry-run on all channels, got %+v", pub)
	}

	// Sanity: the FitCeiling is still on the session.
	ps, err := workflow.LoadPortSession(dir, os.Getpid())
	if err != nil || ps == nil || ps.FitCeiling == nil {
		t.Fatalf("FitCeiling should remain on session: err=%v ps=%v", err, ps)
	}
}

// TestPortCapture_Infeasible_Refuses: an infeasible (or unscored) FitCeiling is
// refused — capture never publishes a port that did not build/boot/serve.
func TestPortCapture_Infeasible_Refuses(t *testing.T) {
	srv, _ := portTestServer(t)
	startPort(t, srv)

	// Harden with a FAILING gate (build never succeeded) → infeasible FitCeiling.
	hres := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "harden",
		"workflow": "port",
		"portRubric": map[string]any{
			"buildSucceeded": false,
		},
	})
	if hres.IsError {
		t.Fatalf("harden(infeasible) failed: %s", getTextContent(t, hres))
	}

	res := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "capture",
		"workflow": "port",
	})
	if !res.IsError {
		t.Fatalf("capture of an infeasible port must refuse; got non-error: %s", getTextContent(t, res))
	}
	if !strings.Contains(getTextContent(t, res), "feasible") {
		t.Errorf("refusal should explain the feasibility gate, got: %s", getTextContent(t, res))
	}
}

// TestPortCapture_NotScored_Refuses: capture before any harden score refuses
// (no FitCeiling on the session yet).
func TestPortCapture_NotScored_Refuses(t *testing.T) {
	srv, _ := portTestServer(t)
	startPort(t, srv)

	res := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "capture",
		"workflow": "port",
	})
	if !res.IsError {
		t.Fatalf("capture before harden must refuse; got: %s", getTextContent(t, res))
	}
}

// TestPortCapture_GlueNotReady_DefersPublish: a feasible FitCeiling whose glue
// repo is NOT buildFromGit-ready (OQ-1 unresolved) EMITS the recipe but DEFERS
// the publish — it never fails, and nothing is pushed.
func TestPortCapture_GlueNotReady_DefersPublish(t *testing.T) {
	srv, _ := portTestServer(t)
	startPort(t, srv)

	hres := callTool(t, srv, "zerops_workflow",
		portFeasibleHardenArgs("https://github.com/zerops-recipe-apps/strapi-app", false))
	if hres.IsError {
		t.Fatalf("harden failed: %s", getTextContent(t, hres))
	}

	res := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "capture",
		"workflow": "port",
	})
	if res.IsError {
		t.Fatalf("capture with not-ready glue must NOT error (it defers): %s", getTextContent(t, res))
	}
	body := decodePortJSON(t, res)
	if body["deferred"] != true {
		t.Errorf("not-ready glue → deferred=true, got %v", body["deferred"])
	}
	if body["published"] != false {
		t.Errorf("not-ready glue → published=false, got %v", body["published"])
	}
	if _, hasPublish := body["publish"]; hasPublish {
		t.Errorf("deferred capture must NOT carry a publish summary (nothing pushed)")
	}
	// The recipe output IS still emitted (emit is unconditional).
	outputDir, _ := body["outputDir"].(string)
	if _, err := os.Stat(filepath.Join(outputDir, "environments")); err != nil {
		t.Errorf("deferred capture must still emit the recipe output dir: %v", err)
	}
}

// TestPortSessionToPlan_HonoredSubsetAndGlueThreaded pins the conversion: the
// glue URL is threaded onto the Plan, managed deps become Services, runtimes
// become Codebases, and only the honored tiers drive the emit.
func TestPortSessionToPlan_HonoredSubsetAndGlueThreaded(t *testing.T) {
	t.Parallel()

	ps := &workflow.PortSession{
		Plan: workflow.PortPlan{
			Target:   "strapi",
			Runtimes: []string{"nodejs@22"},
			Dependencies: []workflow.PortDependency{
				{Declared: "postgresql", Mapping: workflow.DepMappingManaged, ManagedType: "postgresql@16"},
				{Declared: "s3", Mapping: workflow.DepMappingManaged, ManagedType: "object-storage"},
				{Declared: "exotic", Mapping: workflow.DepMappingSelfRun},
			},
		},
	}
	fc := workflow.FitCeiling{
		Target:   "strapi",
		Feasible: true,
		GlueRepo: workflow.GlueRepo{URL: "https://github.com/zerops-recipe-apps/strapi-app.git", BuildFromGitReady: true},
		HonoredTiers: []workflow.HonoredTier{
			{Level: workflow.PortTierAIAgent, Label: "AI Agent", Honored: true},
			{Level: workflow.PortTierHAProd, Label: "Highly-available Production", Honored: false, Reason: "C6 not HA"},
		},
	}

	plan := portSessionToPlan(ps, fc)
	if plan.Slug != "strapi" {
		t.Errorf("slug = %q, want strapi", plan.Slug)
	}
	if plan.GlueRepoURL != fc.GlueRepo.URL {
		t.Errorf("glue URL not threaded: got %q", plan.GlueRepoURL)
	}
	if len(plan.Codebases) != 1 || plan.Codebases[0].BaseRuntime != "nodejs@22" {
		t.Errorf("expected one nodejs@22 codebase, got %+v", plan.Codebases)
	}
	// Managed deps → Services (2); the self-run dep is NOT emitted as managed.
	if len(plan.Services) != 2 {
		t.Fatalf("expected 2 managed services (self-run excluded), got %d: %+v", len(plan.Services), plan.Services)
	}
	var sawStorage bool
	for _, s := range plan.Services {
		if s.Type == "object-storage" {
			sawStorage = true
			if s.Kind != "storage" {
				t.Errorf("object-storage must classify as storage kind, got %q", s.Kind)
			}
		}
	}
	if !sawStorage {
		t.Errorf("object-storage dep missing from services: %+v", plan.Services)
	}
}
