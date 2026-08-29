package port

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/authoring/recipe"
	"github.com/zeropsio/zcp/internal/topology"
)

// portFeasibleHardenArgs builds a full-HA harden call with the glue-repo
// readiness flag set as requested.
func portFeasibleHardenArgs(glueURL string, ready bool) map[string]any {
	return map[string]any{
		"action": "harden",
		"rubric": map[string]any{
			"buildSucceeded":      true,
			"reachedActive":       true,
			"stableAfterHold":     true,
			"httpRootPassed":      true,
			"coreFlowProbePassed": true,
			"harden": map[string]any{
				"sentinelSurvivedRedeploy": true,
				"sentinelOnDurableSurface": true,
				"appContainers":            2,
				"haDeps":                   []any{"postgresql"}, // full-HA: the managed dep IS measured HA
				"haVerifyPassed":           true,
			},
		},
		"glueRepo": map[string]any{
			"url":               glueURL,
			"committedSha":      "abc123",
			"buildFromGitReady": ready,
		},
	}
}

// startPortPostHog starts a port session for a PostHog-shaped descriptor: the
// CORRECTED spec-§8 reality — image-only (crane-lift), ALL deps managed (incl.
// ClickHouse in HA mode), multiple runtimes, strict cross-service init ordering.
// Returns the recon response body so the band/acquisition can be asserted before
// the loop runs. Uses the REAL embedded schema catalog (clickhouse/kafka/valkey/
// object-storage are managed in the embedded floor), so the dep→managed mapping
// is the live one, not a stub.
func startPortPostHog(t *testing.T, srv *mcp.Server) map[string]any {
	t.Helper()
	res := callPort(t, srv, map[string]any{
		"action": "start",
		"target": map[string]any{
			"name":            "posthog",
			"acquisitionHint": "image-only", // crane image-lift (posthog/posthog)
			"dependencies": []any{
				"postgresql", "clickhouse", "kafka", "valkey", "object-storage",
			},
			"runtimes": []any{
				"python@3.12", "python@3.12", "nodejs@24", "go@1",
			},
			"crossServiceOrdering": true, // ch-init → migrate → … chain
		},
	})
	if res.IsError {
		t.Fatalf("posthog port start failed: %s", getTextContent(t, res))
	}
	return decodePortJSON(t, res)
}

// posthogHAHardenArgs builds the FULL-HA harden call for the PostHog fixture —
// the corrected spec-§8 reality: C5=2 (durable persistence) AND C6=2 (HA
// achievable, because ClickHouse runs in HA mode, mandatory for its ON CLUSTER
// DDL). The loop-discovered knowledge-depth items ride unresolved.
func posthogHAHardenArgs(glueURL string) map[string]any {
	return map[string]any{
		"action": "harden",
		"rubric": map[string]any{
			"buildSucceeded":      true,
			"reachedActive":       true,
			"stableAfterHold":     true,
			"httpRootPassed":      true,
			"coreFlowProbePassed": true,
			"harden": map[string]any{
				"sentinelSurvivedRedeploy": true,
				"sentinelOnDurableSurface": true,
				"appContainers":            2,
				// MEASURED per-dep topology: ONLY ClickHouse runs HA (mandatory for
				// ON CLUSTER DDL). Postgres/Kafka/Valkey run NON_HA in the real
				// recipe — the honest port must emit that, not force them HA. This
				// list is ALSO the C6 managed-HA gate (non-empty → HA-prod eligible).
				"haDeps":         []any{"clickhouse"},
				"haVerifyPassed": true,
			},
		},
		"glueRepo": map[string]any{
			"url":               glueURL,
			"committedSha":      "deadbeef",
			"buildFromGitReady": true,
		},
		// The HARD-band knowledge-depth residue (the no-failure-signal class).
		"unresolved": []any{
			"Kafka SASL needs a separate prefix per producer mode (PRODUCER/CONSUMER/CDP/METRICS/WARPSTREAM/WAREHOUSE) or a producer hangs forever on idempotence-PID acquisition — NO failure signal",
			"ENCRYPTION_SALT_KEYS must be exactly 32 ASCII chars (Fernet base64 math)",
			"ioredis treats a bare hostname as localhost — the full redis:// URL is required",
			"capture needed a SASL-patched Rust fork (fxck/posthog-capture)",
		},
	}
}

// TestPortFixture_PostHog_HardBand_HonestCorrectedContract is the canonical HARD
// fixture: a PostHog-shaped descriptor end-to-end (recon → harden → FitCeiling →
// capture), asserting the CORRECTED spec-§8 reality, NOT the old pessimism.
//
// It explicitly asserts PostHog:
//   - is FEASIBLE (does NOT mark itself infeasible),
//   - acquires via crane-image-lift,
//   - reports band=HARD at recon (alongside the measured ceiling),
//   - with C6=2 (ClickHouse HA), honors the ha-prod (Tier 5) tier — it does NOT
//     drop Tier 5,
//   - carries the knowledge-depth items in UnresolvedConstraints,
//   - surfaces the retry-until-ready cross-service ordering choreography in the
//     HARD-band knowledge-base + takeover-guide.
func TestPortFixture_PostHog_HardBand_HonestCorrectedContract(t *testing.T) {
	srv, dir := portTestServer(t)
	stubPortPublisher(t)

	// RECON — crane-lift, all-managed (incl. ClickHouse), cross-service-ordering → HARD.
	recon := startPortPostHog(t, srv)
	plan, ok := recon["portPlan"].(map[string]any)
	if !ok {
		t.Fatalf("recon: expected portPlan, got %T", recon["portPlan"])
	}
	if plan["acquisition"] != string(AcquireCraneImageLift) {
		t.Errorf("PostHog must acquire via crane-image-lift, got %v", plan["acquisition"])
	}
	if plan["band"] != string(BandHard) {
		t.Errorf("PostHog must classify band=HARD, got %v", plan["band"])
	}
	// Every declared dep maps to a managed type — NOTHING is self-run/infeasible.
	deps, _ := plan["dependencies"].([]any)
	if len(deps) != 5 {
		t.Fatalf("expected 5 classified deps, got %d", len(deps))
	}
	for _, d := range deps {
		dm, _ := d.(map[string]any)
		if dm["mapping"] != string(DepMappingManaged) {
			t.Errorf("PostHog dep %v must map to managed (incl. ClickHouse), got %v", dm["declared"], dm["mapping"])
		}
	}
	// Recon recorded the retry-until-ready ordering constraint, NOT a bail.
	cs, _ := plan["constraints"].([]any)
	var sawOrdering bool
	for _, c := range cs {
		if s, _ := c.(string); strings.Contains(s, "retryUntilSuccessful") {
			sawOrdering = true
		}
	}
	if !sawOrdering {
		t.Errorf("recon must record the retry-until-ready ordering constraint, got %v", cs)
	}

	// HARDEN — full-HA grading: C5=2 durable AND C6=2 HA achievable (ClickHouse HA).
	hres := callPort(t, srv,
		posthogHAHardenArgs("https://github.com/zerops-recipe-apps/posthog-app.git"))
	if hres.IsError {
		t.Fatalf("posthog harden failed: %s", getTextContent(t, hres))
	}

	ps, err := LoadPortSession(dir, os.Getpid())
	if err != nil || ps == nil || ps.FitCeiling == nil {
		t.Fatalf("FitCeiling must be scored on the session: err=%v ps=%v", err, ps)
	}
	fc := *ps.FitCeiling

	// FEASIBLE — the corrected reality. NOT infeasible.
	if !fc.Feasible {
		t.Fatalf("PostHog must be FEASIBLE per spec §8 — it must NOT mark itself infeasible")
	}
	// Band=HARD reported alongside the MEASURED ceiling (ha-prod).
	if fc.Band != BandHard {
		t.Errorf("FitCeiling.Band must remain HARD (the recon estimate, reported alongside the ceiling), got %q", fc.Band)
	}
	if fc.MeasuredCeiling != PortTierHAProd {
		t.Errorf("C6=2 → measured ceiling must be ha-prod (Tier 5), got %d", fc.MeasuredCeiling)
	}
	// Tier 5 (ha-prod) IS honored — explicitly NOT dropped, because ClickHouse runs HA.
	var haHonored bool
	for _, ht := range fc.HonoredTiers {
		if ht.Level == PortTierHAProd {
			haHonored = ht.Honored
		}
	}
	if !haHonored {
		t.Errorf("with C6=2, the ha-prod (Tier 5) tier MUST be honored — PostHog does NOT lose Tier 5")
	}
	// Knowledge-depth items present in UnresolvedConstraints (the honest residue).
	joined := strings.Join(fc.UnresolvedConstraints, "\n")
	for _, want := range []string{"Kafka SASL", "ENCRYPTION_SALT_KEYS", "ioredis", "SASL-patched Rust fork", "retryUntilSuccessful"} {
		if !strings.Contains(joined, want) {
			t.Errorf("UnresolvedConstraints must carry the knowledge-depth item %q, got:\n%s", want, joined)
		}
	}

	// CAPTURE — emits + publishes; HARD-band honesty content in the fragments.
	capRes := callPort(t, srv, map[string]any{
		"action":        "capture",
		"publishDryRun": true,
	})
	if capRes.IsError {
		t.Fatalf("posthog capture failed: %s", getTextContent(t, capRes))
	}
	capBody := decodePortJSON(t, capRes)
	if capBody["published"] != true {
		t.Errorf("buildFromGit-ready glue → published, got %v", capBody["published"])
	}
	outputDir, _ := capBody["outputDir"].(string)
	appReadme, rerr := os.ReadFile(filepath.Join(outputDir, "README.md"))
	if rerr != nil {
		t.Fatalf("expected app README: %v", rerr)
	}
	readme := string(appReadme)
	// HARD-band KB/takeover content surfaces the retry-until-ready choreography.
	if !strings.Contains(readme, "retryUntilSuccessful") {
		t.Errorf("HARD-band app README must surface the retry-until-ready choreography, got:\n%s", readme)
	}
	// The Tier-5 env folder was emitted (ha-prod honored).
	haYAML := filepath.Join(outputDir, "environments", "5 — Highly-available Production", "import.yaml")
	haData, herr := os.ReadFile(haYAML)
	if herr != nil {
		t.Fatalf("ha-prod (Tier 5) import.yaml must be emitted (honored), missing: %v", herr)
	}
	// TOPOLOGY assertion (the emitted per-service modes, not a coarse flag): the
	// generated Tier-5 import.yaml must reflect the MEASURED per-service modes.
	// ClickHouse MUST be HA (mandatory for PostHog's ON CLUSTER DDL); Postgres,
	// Kafka and Valkey MUST be NON_HA (the real recipe runs them NON_HA — forcing
	// them HA from a family-table assumption would misrepresent the validated
	// deploy). A regression on ANY of these now fails this test.
	haType, haMode := serviceTypeAndMode(string(haData), "clickhouse")
	if haMode != "HA" {
		t.Errorf("Tier-5 import.yaml must emit ClickHouse in HA mode (mandatory for ON CLUSTER DDL), got mode=%q in:\n%s", haMode, haData)
	}
	// Variant contract: HA-ness lives in the type VARIANT (`:ha`/`:single`), the
	// modern authoritative encoding — NOT a legacy `mode:` field. HA-capable
	// ClickHouse must carry the `:ha` variant.
	if !strings.Contains(haType, ":ha") {
		t.Errorf("ClickHouse type must carry the :ha variant (HA encoded in the type), got %q", haType)
	}
	for _, nonHA := range []string{"postgresql", "kafka", "valkey"} {
		if _, mode := serviceTypeAndMode(string(haData), nonHA); mode != "NON_HA" {
			t.Errorf("Tier-5 import.yaml must emit %s in NON_HA mode (not measured HA → honest port), got mode=%q in:\n%s", nonHA, mode, haData)
		}
	}
}

// serviceTypeAndMode returns the `type:` value and `mode:` value of the service
// block whose type's canonical base name equals wantBase. Canonical-base matching
// (not substring) avoids a `clickhouse-proxy` false-match and is version/mode-
// suffix agnostic. A deliberately small block-scanner so the fixture asserts the
// EMITTED topology byte-for-byte, not a re-parsed abstraction.
func serviceTypeAndMode(yaml, wantBase string) (svcType, mode string) {
	lines := strings.Split(yaml, "\n")
	inBlock := false
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(trimmed, "- hostname:"):
			inBlock = false // new service block; reset until we match its type
		case strings.HasPrefix(trimmed, "type:"):
			t := strings.TrimSpace(strings.TrimPrefix(trimmed, "type:"))
			if topology.CanonicalBaseName(t) == wantBase {
				inBlock, svcType = true, t
			}
		case inBlock && strings.HasPrefix(trimmed, "mode:"):
			return svcType, strings.TrimSpace(strings.TrimPrefix(trimmed, "mode:"))
		}
	}
	// No explicit `mode:` field — derive from the type's deployment variant
	// (the modern authoritative HA encoding: `:ha`→HA, `:single`→NON_HA).
	switch topology.DeploymentVariant(svcType) {
	case topology.VariantHA:
		return svcType, "HA"
	case topology.VariantSingle:
		return svcType, "NON_HA"
	}
	return svcType, ""
}

// TestPortFixture_Strapi_EasyBand_Contrast keeps an EASY Strapi fixture as the
// contrast case: source-build, all-managed, single runtime, no ordering → EASY,
// full-HA → all six tiers, no HARD-band choreography noise.
func TestPortFixture_Strapi_EasyBand_Contrast(t *testing.T) {
	srv, dir := portTestServer(t)
	stubPortPublisher(t)

	// startPort uses the Strapi EASY descriptor (source-repo, postgresql, nodejs).
	startPort(t, srv)

	hres := callPort(t, srv,
		portFeasibleHardenArgs("https://github.com/zerops-recipe-apps/strapi-app.git", true))
	if hres.IsError {
		t.Fatalf("strapi harden failed: %s", getTextContent(t, hres))
	}
	ps, err := LoadPortSession(dir, os.Getpid())
	if err != nil || ps == nil || ps.FitCeiling == nil {
		t.Fatalf("FitCeiling must be scored: err=%v ps=%v", err, ps)
	}
	fc := *ps.FitCeiling
	if fc.Band != BandEasy {
		t.Errorf("Strapi must be EASY, got %q", fc.Band)
	}
	if !fc.Feasible || fc.MeasuredCeiling != PortTierHAProd {
		t.Errorf("full-HA Strapi → feasible ha-prod ceiling, got feasible=%v ceiling=%d", fc.Feasible, fc.MeasuredCeiling)
	}
	// No HARD-band choreography in the EASY knowledge-base.
	kb := portFragmentKnowledgeBase(fc)
	if strings.Contains(kb, "retryUntilSuccessful") {
		t.Errorf("EASY-band Strapi must NOT carry the HARD-band choreography note, got:\n%s", kb)
	}
}

// stubPortPublisher installs a hermetic publish seam (no live gh calls) for the
// duration of a test, restoring the live one on cleanup. It records the dryRun
// it was handed and reports dry-run on every channel so the wiring is asserted
// without touching the network.
func stubPortPublisher(t *testing.T) {
	t.Helper()
	prev := portPublisher
	portPublisher = func(_ string, plan *recipe.Plan, _ FitCeiling, _ string, dryRun bool) portPublishResult {
		status := "created"
		if dryRun {
			status = "dry-run"
		}
		return portPublishResult{
			AppRepo:    "zerops-recipe-apps/" + plan.Slug + "-app",
			DryRun:     dryRun,
			CreateRepo: status,
			PushApp:    status,
			Publish:    status,
		}
	}
	t.Cleanup(func() { portPublisher = prev })
}

// TestPortCapture_Feasible_EmitsAndPublishesDryRun: a feasible, harden-scored
// port with a buildFromGit-ready glue repo emits the honored-tier subset to the
// output dir AND drives the curated two-channel publish (dry-run).
func TestPortCapture_Feasible_EmitsAndPublishesDryRun(t *testing.T) {
	srv, dir := portTestServer(t)
	startPort(t, srv)
	stubPortPublisher(t)

	hres := callPort(t, srv,
		portFeasibleHardenArgs("https://github.com/zerops-recipe-apps/strapi-app.git", true))
	if hres.IsError {
		t.Fatalf("harden failed: %s", getTextContent(t, hres))
	}

	res := callPort(t, srv, map[string]any{
		"action":        "capture",
		"publishDryRun": true,
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
	ps, err := LoadPortSession(dir, os.Getpid())
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
	hres := callPort(t, srv, map[string]any{
		"action": "harden",
		"rubric": map[string]any{
			"buildSucceeded": false,
		},
	})
	if hres.IsError {
		t.Fatalf("harden(infeasible) failed: %s", getTextContent(t, hres))
	}

	res := callPort(t, srv, map[string]any{
		"action": "capture",
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

	res := callPort(t, srv, map[string]any{
		"action": "capture",
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

	hres := callPort(t, srv,
		portFeasibleHardenArgs("https://github.com/zerops-recipe-apps/strapi-app", false))
	if hres.IsError {
		t.Fatalf("harden failed: %s", getTextContent(t, hres))
	}

	res := callPort(t, srv, map[string]any{
		"action": "capture",
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

// TestPortFragments_HardBand_IncludeRetryUntilReadyChoreography: a HARD-band
// FitCeiling's knowledge-base AND takeover-guide fragments surface the
// retry-until-ready cross-service ordering choreography note, so the porter
// taking over the recipe knows the init-ordering idiom the deployment relies on.
func TestPortFragments_HardBand_IncludeRetryUntilReadyChoreography(t *testing.T) {
	t.Parallel()

	fc := FitCeiling{
		Target:   "posthog",
		Band:     BandHard,
		Feasible: true,
		HonoredTiers: []HonoredTier{
			{Level: PortTierAIAgent, Label: "AI Agent", Honored: true},
			{Level: PortTierHAProd, Label: "Highly-available Production", Honored: true},
		},
		// Note: no explicit ordering constraint in UnresolvedConstraints — the
		// HARD-band synthesis must surface the choreography note regardless.
		UnresolvedConstraints: []string{"Kafka SASL per-producer prefix (no failure signal)"},
	}
	plan := &recipe.Plan{Name: "Posthog", Slug: "posthog"}

	kb := portFragmentKnowledgeBase(fc)
	if !strings.Contains(kb, "retryUntilSuccessful") {
		t.Errorf("HARD-band knowledge-base must include the retry-until-ready choreography note, got:\n%s", kb)
	}
	tg := portFragmentTakeoverGuide(plan, fc)
	if !strings.Contains(tg, "retryUntilSuccessful") {
		t.Errorf("HARD-band takeover-guide must include the retry-until-ready choreography note, got:\n%s", tg)
	}
}

// TestPortFragments_EasyBand_NoChoreographyNoise: an EASY-band port whose
// constraints carry no ordering note must NOT inject the HARD-band choreography
// boilerplate — the honesty content is HARD-band-specific.
func TestPortFragments_EasyBand_NoChoreographyNoise(t *testing.T) {
	t.Parallel()

	fc := FitCeiling{
		Target:   "strapi",
		Band:     BandEasy,
		Feasible: true,
		HonoredTiers: []HonoredTier{
			{Level: PortTierAIAgent, Label: "AI Agent", Honored: true},
		},
	}
	kb := portFragmentKnowledgeBase(fc)
	if strings.Contains(kb, "retryUntilSuccessful") {
		t.Errorf("EASY-band knowledge-base must NOT inject the HARD-band choreography note, got:\n%s", kb)
	}
}

// TestPortSessionToPlan_HonoredSubsetAndGlueThreaded pins the conversion: the
// glue URL is threaded onto the Plan, managed deps become Services, runtimes
// become Codebases, and only the honored tiers drive the emit.
func TestPortSessionToPlan_HonoredSubsetAndGlueThreaded(t *testing.T) {
	t.Parallel()

	ps := &PortSession{
		Plan: PortPlan{
			Target:   "strapi",
			Runtimes: []string{"nodejs@22"},
			Dependencies: []PortDependency{
				{Declared: "postgresql", Mapping: DepMappingManaged, ManagedType: "postgresql@16"},
				{Declared: "s3", Mapping: DepMappingManaged, ManagedType: "object-storage"},
				{Declared: "exotic", Mapping: DepMappingSelfRun},
			},
		},
	}
	fc := FitCeiling{
		Target:   "strapi",
		Feasible: true,
		GlueRepo: GlueRepo{URL: "https://github.com/zerops-recipe-apps/strapi-app.git", BuildFromGitReady: true},
		HonoredTiers: []HonoredTier{
			{Level: PortTierAIAgent, Label: "AI Agent", Honored: true},
			{Level: PortTierHAProd, Label: "Highly-available Production", Honored: false, Reason: "C6 not HA"},
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

func TestPortSessionToPlan_LocalStorage_UsesDedicatedKind(t *testing.T) {
	t.Parallel()
	ps := &PortSession{Plan: PortPlan{Target: "app", Dependencies: []PortDependency{{
		Declared: "local", Mapping: DepMappingManaged, ManagedType: "local-storage:single@1",
	}}}}
	plan := portSessionToPlan(ps, FitCeiling{Target: "app", Feasible: true})
	if len(plan.Services) != 1 {
		t.Fatalf("services = %+v, want one Local Storage service", plan.Services)
	}
	if plan.Services[0].Kind != recipe.ServiceKindLocalStorage {
		t.Errorf("Local Storage kind = %q, want %q", plan.Services[0].Kind, recipe.ServiceKindLocalStorage)
	}
	if plan.Services[0].Type != "local-storage:single@1" {
		t.Errorf("Local Storage type = %q, want exact composite", plan.Services[0].Type)
	}
}
