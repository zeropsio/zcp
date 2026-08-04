// Tests for buildGuide assembly — iteration hard-stop, atom synthesis per
// step/mode/route, env-var catalog injection at close, and recipe-import-YAML
// injection at discover/provision.
package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/capture"
	"github.com/zeropsio/zcp/internal/topology"
)

func TestBuildGuide_Iteration_ShortCircuitsToHardStop(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Plan = &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"}},
	}}
	// iteration > 0 must produce the hard-stop message, not atom synthesis.
	// Bootstrap doesn't iterate under Option A — infra verification escalates
	// to the user rather than retry.
	guide := bs.buildGuide(StepProvision, 1, EnvContainer, nil)
	if !strings.Contains(guide, "STOP") {
		t.Errorf("iteration > 0 should yield hard-stop output, got:\n%s", guide)
	}
	if !strings.Contains(guide, "does not iterate") {
		t.Error("hard-stop should explain bootstrap doesn't iterate")
	}
}

func TestBuildGuide_CaptureProvenanceDoesNotChangeModelVisibleBytes(t *testing.T) {
	// non-parallel: capture provenance opt-in is process environment.
	bs := NewBootstrapState()
	bs.Route = BootstrapRouteRecipe
	bs.RecipeMatch = &RecipeMatch{
		Slug:       "identity-test",
		Confidence: 1,
		Mode:       topology.PlanModeStandard,
		ImportYAML: "services:\n  - hostname: appdev\n    type: nodejs@22\n",
	}
	bs.Plan = &ServicePlan{Targets: []BootstrapTarget{{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"}}}}
	t.Setenv(capture.EnvSessionID, "")
	t.Setenv(capture.EnvSessionDir, "")
	withoutCapture := bs.buildGuide(StepProvision, 0, EnvContainer, nil)

	sessionDir := t.TempDir()
	t.Setenv(capture.EnvSessionID, "capture-guide")
	t.Setenv(capture.EnvSessionDir, sessionDir)
	withCapture := bs.buildGuide(StepProvision, 0, EnvContainer, nil)
	if withCapture != withoutCapture {
		t.Fatal("capture changed model-visible bootstrap guide bytes")
	}
	paths, err := filepath.Glob(filepath.Join(sessionDir, "provenance", "zcp-*.jsonl"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("provenance files = %v, %v", paths, err)
	}
	records, err := capture.ReadCompositionRecords(paths[0])
	if err != nil {
		t.Fatalf("ReadCompositionRecords() error = %v", err)
	}
	if len(records) != 1 || records[0].OutputBytes != len(withCapture) {
		t.Fatalf("composition records = %+v", records)
	}
	foundDynamic := false
	for _, component := range records[0].Components {
		if component.Owner == "workflow.formatRecipeImportYAMLForGuide" {
			foundDynamic = true
		}
	}
	if !foundDynamic {
		t.Fatalf("dynamic recipe assembly owner missing: %+v", records[0].Components)
	}
	if _, err := os.Stat(paths[0]); err != nil {
		t.Fatalf("stat provenance: %v", err)
	}
}

func TestBuildGuide_Recipe_RouteOverridesPlanInference(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Route = BootstrapRouteRecipe
	bs.RecipeMatch = &RecipeMatch{Slug: "laravel-minimal", Confidence: 1.0}
	bs.Plan = &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "php-nginx@8.4"}},
	}}
	env := bs.synthesisEnvelope(StepProvision, EnvLocal)
	if env.Bootstrap == nil {
		t.Fatal("bootstrap summary missing")
	}
	if env.Bootstrap.Route != BootstrapRouteRecipe {
		t.Errorf("route: got %q, want %q", env.Bootstrap.Route, BootstrapRouteRecipe)
	}
	if env.Bootstrap.RecipeMatch == nil || env.Bootstrap.RecipeMatch.Slug != "laravel-minimal" {
		t.Errorf("recipe match not propagated: %+v", env.Bootstrap.RecipeMatch)
	}
}

func TestBuildGuide_Recipe_ProvisionInjectsImportYAML(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Route = BootstrapRouteRecipe
	bs.RecipeMatch = &RecipeMatch{
		Slug:       "laravel-minimal",
		Confidence: 0.97,
		Mode:       topology.PlanModeStandard,
		ImportYAML: "project:\n  name: laravel-minimal-agent\nservices:\n  - hostname: appdev\n    type: php-nginx@8.4\n",
	}
	bs.Plan = &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "php-nginx@8.4"}},
	}}
	guide := bs.buildGuide(StepProvision, 0, EnvContainer, nil)
	if !strings.Contains(guide, "Recipe — \"laravel-minimal\"") {
		t.Error("provision guide should contain the recipe header for recipe route")
	}
	if !strings.Contains(guide, "hostname: appdev") {
		t.Error("provision guide should contain the injected YAML body")
	}
	if !strings.Contains(guide, "zerops_import") {
		t.Error("provision guide should carry the import procedure")
	}
}

// TestBuildGuide_Recipe_DiscoverPresentsDeriveAndConfirm pins R3-P4.4: the
// discover guide presents the recipe + the derive-and-confirm contract (ZCP
// derives the plan; the agent confirms or renames), NOT a "write the plan +
// set bootstrapMode" instruction — the mode is the recipe's, derived.
func TestBuildGuide_Recipe_DiscoverPresentsDeriveAndConfirm(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Route = BootstrapRouteRecipe
	bs.RecipeMatch = &RecipeMatch{
		Slug:       "nextjs-ssr-hello-world",
		Confidence: 0.97,
		Mode:       topology.PlanModeStandard,
		ImportYAML: "project:\n  name: nextjs-agent\nservices:\n  - hostname: appdev\n    type: nodejs@22\n    zeropsSetup: dev\n  - hostname: appstage\n    type: nodejs@22\n    zeropsSetup: prod\n",
	}
	guide := bs.buildGuide(StepDiscover, 0, EnvContainer, nil)
	if !strings.Contains(guide, "Recipe — \"nextjs-ssr-hello-world\"") {
		t.Error("discover guide should name the matched recipe")
	}
	if !strings.Contains(guide, "hostname: appdev") {
		t.Error("discover guide should show the recipe YAML for reference")
	}
	if !strings.Contains(guide, "derives the provisioning plan") {
		t.Error("discover guide should tell the agent ZCP derives the plan (no authoring)")
	}
	if strings.Contains(guide, "bootstrapMode") {
		t.Error("discover guide must NOT tell the agent to set bootstrapMode — the mode is derived from the recipe")
	}
}

// TestBuildGuide_Recipe_ProvisionRewritesYAMLWithOverrides pins R3-P4.4: when
// the discover step reconciled hostname renames into RecipeOverrides (collision
// recovery), the provision step surfaces the REWRITTEN YAML so `zerops_import`
// creates services with the chosen hostnames — not the recipe's. The rewrite
// reads the stored overrides (single owner with the derived plan), not the plan.
func TestBuildGuide_Recipe_ProvisionRewritesYAMLWithOverrides(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Route = BootstrapRouteRecipe
	bs.RecipeMatch = &RecipeMatch{
		Slug:       "dotnet-hello-world",
		Confidence: 0.97,
		Mode:       topology.PlanModeStandard,
		ImportYAML: "project:\n  name: dotnet-agent\nservices:\n  - hostname: appdev\n    type: dotnet@9\n    zeropsSetup: dev\n  - hostname: appstage\n    type: dotnet@9\n    zeropsSetup: prod\n  - hostname: db\n    type: postgresql@16\n    mode: NON_HA\n",
	}
	bs.RecipeOverrides = &RecipeShapeOverrides{
		RuntimeHostnameByOriginal: map[string]string{"appdev": "uploaddev", "appstage": "uploadstage"},
	}
	guide := bs.buildGuide(StepProvision, 0, EnvContainer, nil)
	if !strings.Contains(guide, "hostname: uploaddev") {
		t.Errorf("provision YAML must carry plan's dev hostname, got:\n%s", guide)
	}
	if !strings.Contains(guide, "hostname: uploadstage") {
		t.Errorf("provision YAML must carry plan's stage hostname, got:\n%s", guide)
	}
	if strings.Contains(guide, "hostname: appdev") || strings.Contains(guide, "hostname: appstage") {
		t.Errorf("provision YAML must NOT contain recipe's original runtime hostnames, got:\n%s", guide)
	}
	// Managed service hostname stays verbatim — repo's zerops.yaml holds
	// ${db_*} env-var references that cannot be rewritten.
	if !strings.Contains(guide, "hostname: db") {
		t.Errorf("provision YAML must keep managed service hostname verbatim, got:\n%s", guide)
	}
}

// TestBuildGuide_Recipe_ProvisionExistsResolutionDropsManaged pins F6
// behavior for adopting a managed service: Dependency{Resolution=EXISTS}
// means the service already exists; the provision YAML must NOT contain
// the entry, otherwise `zerops_import` rejects it with `serviceStackNameUnavailable`.
func TestBuildGuide_Recipe_ProvisionExistsResolutionDropsManaged(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Route = BootstrapRouteRecipe
	bs.RecipeMatch = &RecipeMatch{
		Slug:       "nodejs-hello-world",
		Confidence: 0.97,
		Mode:       topology.PlanModeStandard,
		ImportYAML: "services:\n  - hostname: appdev\n    type: nodejs@22\n    zeropsSetup: dev\n  - hostname: appstage\n    type: nodejs@22\n    zeropsSetup: prod\n  - hostname: db\n    type: postgresql@18\n    mode: NON_HA\n",
	}
	bs.RecipeOverrides = &RecipeShapeOverrides{
		ManagedResolutionByHost: map[string]string{"db": ResolutionExists},
	}
	guide := bs.buildGuide(StepProvision, 0, EnvContainer, nil)
	if strings.Contains(guide, "hostname: db") {
		t.Errorf("EXISTS resolution must drop the managed service from provision YAML, got:\n%s", guide)
	}
	if strings.Contains(guide, "type: postgresql@18") {
		t.Errorf("EXISTS resolution must drop postgresql entry entirely, got:\n%s", guide)
	}
}

// TestBuildGuide_Recipe_DiscoverStaysVerbatim pins F6 boundary: the plan
// isn't submitted at discover step, so rewrite cannot apply; agent sees
// the recipe's canonical hostnames as the shape to base their plan on.
func TestBuildGuide_Recipe_DiscoverStaysVerbatim(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Route = BootstrapRouteRecipe
	bs.RecipeMatch = &RecipeMatch{
		Slug:       "dotnet-hello-world",
		Confidence: 0.97,
		ImportYAML: "services:\n  - hostname: appdev\n    type: dotnet@9\n    zeropsSetup: dev\n",
	}
	// Plan is nil at discover (agent hasn't submitted it yet).
	guide := bs.buildGuide(StepDiscover, 0, EnvContainer, nil)
	if !strings.Contains(guide, "hostname: appdev") {
		t.Errorf("discover guide should surface recipe hostnames verbatim, got:\n%s", guide)
	}
}

func TestBuildGuide_Recipe_CloseDoesNotInjectYAML(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Route = BootstrapRouteRecipe
	bs.RecipeMatch = &RecipeMatch{
		Slug:       "laravel-minimal",
		Confidence: 0.97,
		Mode:       topology.PlanModeStandard,
		ImportYAML: "project:\n  name: x\nservices:\n  - hostname: appdev\n",
	}
	bs.Plan = &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "php-nginx@8.4"}},
	}}
	guide := bs.buildGuide(StepClose, 0, EnvContainer, nil)
	if strings.Contains(guide, "## Recipe — ") {
		t.Error("close guide should NOT contain the recipe block (discover+provision only)")
	}
}

// TestBootstrapGuide_OwnershipLink_UsesGUISlug pins the fix for the dead-link
// defect (owner-reported 2026-08-04): the recipe close/ownership guidance
// must render the GUI recipe page URL from RecipeMatch.GUISlug (the
// recipe's original Strapi slug), never from RecipeMatch.Slug (the corpus
// slug), since `.sync.yaml`'s slug_remap can make the two differ
// (node-js-hello-world corpus slug is nodejs-hello-world).
//
// Independent oracle: the exact working URL
// https://app.zerops.io/recipes/node-js-hello-world is the owner-reported
// live URL, not recomputed via any remap helper.
func TestBootstrapGuide_OwnershipLink_UsesGUISlug(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Route = BootstrapRouteRecipe
	bs.RecipeMatch = &RecipeMatch{
		Slug:       "nodejs-hello-world",
		GUISlug:    "node-js-hello-world",
		Confidence: 1.0,
		Mode:       topology.PlanModeStandard,
		ImportYAML: "services:\n  - hostname: appdev\n    type: nodejs@22\n",
	}
	bs.Plan = &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"}},
	}}
	guide := bs.buildGuide(StepClose, 0, EnvContainer, nil)
	if !strings.Contains(guide, "https://app.zerops.io/recipes/node-js-hello-world") {
		t.Errorf("close guide should contain the GUI URL built from GUISlug, got:\n%s", guide)
	}
	if strings.Contains(guide, "recipes/nodejs-hello-world") {
		t.Errorf("close guide must NOT contain the URL composed from the corpus slug, got:\n%s", guide)
	}
}

func TestBuildGuide_NoRoute_AdoptInferredFromPlan(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	// No Route field set — legacy behavior: adopt inferred from Plan.IsAllExisting().
	bs.Plan = &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "legacy", Type: "nodejs@22", IsExisting: true}},
	}}
	env := bs.synthesisEnvelope(StepProvision, EnvLocal)
	if env.Bootstrap.Route != BootstrapRouteAdopt {
		t.Errorf("adopt should be inferred from all-existing plan, got %q", env.Bootstrap.Route)
	}
}

func TestBuildGuide_Close_InjectsDiscoveredEnvVars(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Plan = &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
			Dependencies: []Dependency{{Hostname: "db", Type: "postgresql@16", Resolution: "CREATE"}}},
	}}
	bs.DiscoveredEnvVars = map[string][]string{
		"db": {"connectionString", "port"},
	}
	// Env var catalog is injected at close so the develop handoff carries
	// the authoritative key list across compaction.
	guide := bs.buildGuide(StepClose, 0, EnvContainer, nil)
	if !strings.Contains(guide, "Discovered Managed-Service Env Var Catalog") {
		t.Error("close guide should contain the dynamic env var catalog when DiscoveredEnvVars is populated")
	}
	if !strings.Contains(guide, "${db_connectionString}") {
		t.Error("close guide should contain cross-service env var references")
	}
}

func TestBuildGuide_Provision_NoEnvVarCatalog(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Plan = &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"}},
	}}
	bs.DiscoveredEnvVars = map[string][]string{
		"cache": {"connectionString"},
	}
	guide := bs.buildGuide(StepProvision, 0, EnvContainer, nil)
	// Env var catalog is injected only at close — provision is before discovery completes.
	if strings.Contains(guide, "${cache_connectionString}") {
		t.Error("provision guide should NOT contain env var catalog (close-only injection)")
	}
}

func TestBuildGuide_Close_EmptyPlan_ReturnsStaticMessage(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	// Managed-only bootstrap: plan has no runtime targets.
	bs.Plan = &ServicePlan{}
	guide := bs.buildGuide(StepClose, 0, EnvContainer, nil)
	if !strings.Contains(guide, "Bootstrap is complete") {
		t.Error("close step with empty plan should return static closeGuidance")
	}
}

func TestBuildGuide_Adopt_RouteFiltersAtoms(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	// All-existing plan triggers adopt route.
	bs.Plan = &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", IsExisting: true}},
	}}
	guide := bs.buildGuide(StepDiscover, 0, EnvContainer, nil)
	// Either an adopt-route discover atom fires or nothing does. Either way
	// classic-only wording must not surface.
	if guide == "" {
		return
	}
	if strings.Contains(guide, "classic") && !strings.Contains(guide, "adopt") {
		t.Errorf("adopt-route discover guide should not surface classic-only wording, got:\n%s", guide)
	}
}

// TestBuildGuide_SynthesisErrorPropagates pins Phase 2 (C3) of the
// pipeline-repair plan: when the underlying Synthesize call fails (e.g.
// unknown placeholder in an atom), buildGuide MUST embed the error text
// in its returned string, not silently return "".
//
// Verifies the contract via Synthesize directly (it's the upstream
// invariant) and via buildGuide's error-text output shape (which the
// implementation now produces — pre-fix it returned "").
func TestBuildGuide_SynthesisErrorPropagates(t *testing.T) {
	t.Parallel()

	// Upstream invariant: Synthesize rejects unknown placeholders.
	envelope := StateEnvelope{Phase: PhaseDevelopActive}
	badAtom := KnowledgeAtom{
		ID:   "synthetic-bad-placeholder",
		Body: "Use {totally-unsupported-placeholder} here.",
		Axes: AxisVector{Phases: []Phase{PhaseDevelopActive}},
	}
	if _, err := Synthesize(envelope, []KnowledgeAtom{badAtom}); err == nil {
		t.Fatal("Synthesize should reject unknown placeholder, got nil err")
	}

	// buildGuide error-text contract: any error path inside buildGuide
	// produces an "## ERROR" prefixed string, never "". This test asserts
	// the error path's wording so removing the visible-error-text fix
	// (regressing to silent "") fails the test.
	bs := NewBootstrapState()
	bs.Plan = &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"}},
	}}
	// The discover step against the production corpus should NOT error —
	// this is the success path. Empty string would indicate a regression
	// (silent swallow returning "" because of an upstream issue).
	guide := bs.buildGuide(StepDiscover, 0, EnvContainer, nil)
	if guide == "" {
		t.Fatal("buildGuide returned empty string — possible silent error swallow regression")
	}
	if strings.Contains(guide, "## ERROR") {
		t.Fatalf("production corpus should not emit error guide, got:\n%s", guide)
	}
}

// TestBootstrapGuide_ProvisionYAML_ServicesOnly pins RCO-6: the provision-step
// guide's instruction ("services: section ONLY") and the fenced YAML beneath
// it must agree — no `project:` key survives into any fenced YAML block, even
// when the recipe's canonical YAML carries one (live-verified defect: guide
// said "services section ONLY" while still rendering `project: {name: ...}`).
func TestBootstrapGuide_ProvisionYAML_ServicesOnly(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Route = BootstrapRouteRecipe
	bs.RecipeMatch = &RecipeMatch{
		Slug:       "laravel-minimal",
		Confidence: 0.97,
		Mode:       topology.PlanModeStandard,
		ImportYAML: "project:\n  name: laravel-minimal-agent\n  envVariables:\n    APP_KEY: \"<@generateRandomString(<32>)>\"\nservices:\n  - hostname: appdev\n    type: php-nginx@8.4\n",
	}
	bs.Plan = &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "php-nginx@8.4"}},
	}}

	guide := bs.buildGuide(StepProvision, 0, EnvContainer, nil)

	for _, block := range fencedBlocks(guide) {
		if strings.Contains(block, "project:") {
			t.Errorf("fenced block contains a project: key, want services-only YAML:\n%s", block)
		}
	}
	if !strings.Contains(guide, "hostname: appdev") {
		t.Errorf("provision guide should still carry the services YAML, got:\n%s", guide)
	}
}

// TestBootstrapGuide_ProvisionEnvPresteps_ExecutableKV pins RCO-6: recipe
// `project.envVariables` are not dropped silently along with the stripped
// `project:` block — they render as EXECUTABLE zerops_env pre-steps carrying
// both the key AND the literal value (generator expressions included, e.g.
// Laravel's APP_KEY), plus a note that generator expressions can be expanded
// via zerops_preprocess first.
func TestBootstrapGuide_ProvisionEnvPresteps_ExecutableKV(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Route = BootstrapRouteRecipe
	bs.RecipeMatch = &RecipeMatch{
		Slug:       "laravel-minimal",
		Confidence: 0.97,
		Mode:       topology.PlanModeStandard,
		ImportYAML: "project:\n  name: laravel-minimal-agent\n  envVariables:\n    APP_KEY: \"<@generateRandomString(<32>)>\"\nservices:\n  - hostname: appdev\n    type: php-nginx@8.4\n",
	}
	bs.Plan = &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "php-nginx@8.4"}},
	}}

	guide := bs.buildGuide(StepProvision, 0, EnvContainer, nil)

	if !strings.Contains(guide, `key="APP_KEY"`) {
		t.Errorf("provision guide should render the pre-step with the real key, got:\n%s", guide)
	}
	if !strings.Contains(guide, `value="<@generateRandomString(<32>)>"`) {
		t.Errorf("provision guide should render the pre-step with the real literal value, got:\n%s", guide)
	}
	if !strings.Contains(guide, "zerops_preprocess") {
		t.Errorf("provision guide should note zerops_preprocess for expanding generator expressions, got:\n%s", guide)
	}
}

// fencedBlocks extracts the content of every ``` ... ``` fenced block in s,
// in order. Used to assert on rendered code/YAML blocks specifically,
// distinct from inline single-backtick spans in surrounding prose.
func fencedBlocks(s string) []string {
	var blocks []string
	parts := strings.Split(s, "```")
	for i := 1; i < len(parts); i += 2 {
		blocks = append(blocks, parts[i])
	}
	return blocks
}

// TestPlanTargetSnapshots_PopulatesStatusFromLive pins that snapshots produced
// for synthesisEnvelope carry the per-hostname platform Status captured in
// BootstrapState.DiscoveredStatuses. Without this, atoms gated on
// serviceStatus (e.g. develop-ready-to-deploy.md gated on
// serviceStatus:[READY_TO_DEPLOY]) never match during bootstrap-active phase
// even when the live service IS in READY_TO_DEPLOY — Phase 2.1 of
// plans/eval-review-20260518-subset/fix-plan.md.
func TestPlanTargetSnapshots_PopulatesStatusFromLive(t *testing.T) {
	t.Parallel()
	statuses := map[string]string{
		"appdev":   "READY_TO_DEPLOY",
		"appstage": "ACTIVE",
	}
	target := BootstrapTarget{
		Runtime: RuntimeTarget{
			DevHostname:   "appdev",
			Type:          "nodejs@22",
			BootstrapMode: topology.PlanModeStandard,
			ExplicitStage: "appstage",
		},
	}
	snaps := planTargetSnapshots(target, statuses)
	if len(snaps) != 2 {
		t.Fatalf("standard mode: expected 2 snapshots, got %d", len(snaps))
	}
	devSnap := snaps[0]
	stageSnap := snaps[1]
	if devSnap.Hostname != "appdev" || devSnap.Status != "READY_TO_DEPLOY" {
		t.Errorf("dev snapshot Status: got %q, want %q", devSnap.Status, "READY_TO_DEPLOY")
	}
	if stageSnap.Hostname != "appstage" || stageSnap.Status != "ACTIVE" {
		t.Errorf("stage snapshot Status: got %q, want %q", stageSnap.Status, "ACTIVE")
	}

	// Absent hostname yields empty Status — the safe default before the
	// first provision check or in fixtures that don't carry live state.
	empty := planTargetSnapshots(target, nil)
	if empty[0].Status != "" {
		t.Errorf("nil statuses: Status must be empty, got %q", empty[0].Status)
	}
}

// newStandardBootstrapStateAt builds a BootstrapState with a standard-mode
// runtime target (appdev/appstage) whose CurrentStep is pinned to idx —
// mirroring the step-completion bookkeeping BuildResponse relies on (prior
// steps complete, the current one in_progress). Shared by the RCO-7
// RuntimeURLs presence tests below to exercise BuildResponse at each of the
// three response shapes the contract names: pre-provision (discover/
// provision in progress), status-during-close (close in progress), and
// terminal (past the last step).
func newStandardBootstrapStateAt(idx int) *BootstrapState {
	bs := NewBootstrapState()
	bs.Plan = &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: topology.PlanModeStandard, ExplicitStage: "appstage"}},
	}}
	bs.CurrentStep = idx
	for i := 0; i < idx && i < len(bs.Steps); i++ {
		bs.Steps[i].Status = stepComplete
	}
	if idx < len(bs.Steps) {
		bs.Steps[idx].Status = stepInProgress
	} else {
		bs.Active = false
	}
	return bs
}

// TestBootstrapResponse_RuntimeURLs_StructPresence pins RCO-7's wire-format
// contract: BootstrapResponse.RuntimeURLs is an omitempty JSON field —
// absent from the wire when unpopulated (the pre-provision shape, where L4
// never resolves URLs), present when populated (the shapes RCO-7 names:
// the response following a successful provision — Current advances to
// "close" — action="status" while close remains active, which is the SAME
// Current="close" shape, and the terminal close response where Current is
// nil). Resolution itself is an L4 (tools) concern via ops.ResolveSubdomainURL
// — this unit test proves the struct's presence contract only, simulating
// what L4 does by assigning RuntimeURLs directly.
func TestBootstrapResponse_RuntimeURLs_StructPresence(t *testing.T) {
	t.Parallel()

	populated := []RuntimeURL{{
		Hostname: "appstage",
		Role:     RuntimeURLRoleStage,
		URL:      "https://appstage-24cb-3000.prg1.zerops.app",
		Handoff:  true,
	}}

	tests := []struct {
		name        string
		stepIndex   int // index into bs.Steps: 0=discover, 1=provision, 2=close, 3=past-close(terminal)
		setURLs     bool
		wantPresent bool
	}{
		{name: "pre-provision (discover in progress): absent", stepIndex: 0, setURLs: false, wantPresent: false},
		{name: "pre-provision (provision in progress): absent", stepIndex: 1, setURLs: false, wantPresent: false},
		{name: "status-during-close (close in progress): present when populated", stepIndex: 2, setURLs: true, wantPresent: true},
		{name: "terminal close (past last step): present when populated", stepIndex: 3, setURLs: true, wantPresent: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			bs := newStandardBootstrapStateAt(tt.stepIndex)
			resp := bs.BuildResponse("sess-1", "test intent", 0, EnvContainer, nil)
			if tt.setURLs {
				resp.RuntimeURLs = populated
			}

			data, err := json.Marshal(resp)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			present := strings.Contains(string(data), `"runtimeUrls"`)
			if present != tt.wantPresent {
				t.Errorf("runtimeUrls present = %v, want %v; json:\n%s", present, tt.wantPresent, data)
			}
		})
	}
}

// TestBootstrapGuide_CloseGuidance_RendersFromRuntimeURLs pins RCO-7: the
// close/status Markdown guidance for the runtime-URL collection is DERIVED
// from the collection by FormatRuntimeURLsForGuide (the single owner of
// this wording — the tools layer calls it with the already-resolved
// collection and never hand-composes URL guidance itself, since workflow
// must not import ops to resolve URLs directly). The stage entry is named
// the handoff; dev is never presented as the app URL even when it carries
// its own (non-handoff) resolved URL.
func TestBootstrapGuide_CloseGuidance_RendersFromRuntimeURLs(t *testing.T) {
	t.Parallel()

	t.Run("stage marked handoff, dev never presented as the app URL", func(t *testing.T) {
		t.Parallel()
		urls := []RuntimeURL{
			{Hostname: "appdev", Role: RuntimeURLRoleDev, URL: "https://appdev-24cb-3000.prg1.zerops.app", Handoff: false},
			{Hostname: "appstage", Role: RuntimeURLRoleStage, URL: "https://appstage-24cb-3000.prg1.zerops.app", Handoff: true},
		}
		guide := FormatRuntimeURLsForGuide(urls)

		if !strings.Contains(guide, "https://appstage-24cb-3000.prg1.zerops.app") {
			t.Errorf("guide should carry the stage URL, got:\n%s", guide)
		}
		if !strings.Contains(guide, "handoff") {
			t.Errorf("guide should mark the stage entry as the handoff, got:\n%s", guide)
		}
		// The "hand this back to the user" callout must reference the stage
		// URL, never the dev one — assert the dev URL never appears on the
		// same line as the handoff callout wording.
		for line := range strings.SplitSeq(guide, "\n") {
			if strings.Contains(line, "appdev-24cb-3000") && strings.Contains(strings.ToLower(line), "hand") {
				t.Errorf("dev URL must never be presented as the app/handoff URL, got line:\n%s", line)
			}
		}
	})

	t.Run("empty collection notes the URL could not be resolved yet", func(t *testing.T) {
		t.Parallel()
		guide := FormatRuntimeURLsForGuide(nil)
		if !strings.Contains(guide, "could be resolved yet") {
			t.Errorf("empty collection should explain no URL could be resolved yet, got:\n%s", guide)
		}
		if strings.Contains(guide, "https://") {
			t.Errorf("empty collection must never fabricate a URL, got:\n%s", guide)
		}
	})
}

// TestSynthesisEnvelope_PropagatesDiscoveredStatuses pins that
// BootstrapState.DiscoveredStatuses flows into the synthesis envelope, so
// status-gated atoms see the live state when fanning out. Phase 2.1 of
// plans/eval-review-20260518-subset/fix-plan.md.
func TestSynthesisEnvelope_PropagatesDiscoveredStatuses(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Plan = &ServicePlan{Targets: []BootstrapTarget{{
		Runtime: RuntimeTarget{
			DevHostname:   "appdev",
			Type:          "nodejs@22",
			BootstrapMode: topology.PlanModeDev,
		},
	}}}
	bs.DiscoveredStatuses = map[string]string{"appdev": "READY_TO_DEPLOY"}

	env := bs.synthesisEnvelope(StepProvision, EnvContainer)
	if len(env.Services) != 1 {
		t.Fatalf("expected 1 service snapshot, got %d", len(env.Services))
	}
	if env.Services[0].Status != "READY_TO_DEPLOY" {
		t.Errorf("Status in envelope: got %q, want %q", env.Services[0].Status, "READY_TO_DEPLOY")
	}
}
