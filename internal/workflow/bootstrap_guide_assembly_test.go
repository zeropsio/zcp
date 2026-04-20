// Tests for buildGuide assembly — iteration-delta short-circuit, atom
// synthesis per step/mode/route, and post-synthesis env var catalog injection.
package workflow

import (
	"strings"
	"testing"
)

func TestBuildGuide_IterationDelta_ShortCircuits(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Plan = &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"}},
	}}
	// iteration > 0 must produce the delta, not atom synthesis.
	guide := bs.buildGuide(StepDeploy, 1, EnvContainer, nil)
	if !strings.Contains(guide, "ITERATION 1") {
		t.Error("iteration > 0 should yield BuildIterationDelta output, not atom synthesis")
	}
}

func TestBuildGuide_Generate_Standard_HasNoopStart(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Plan = &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"}},
	}}
	guide := bs.buildGuide(StepGenerate, 0, EnvContainer, nil)
	if guide == "" {
		t.Fatal("generate guide should not be empty for standard-mode plan")
	}
	if !strings.Contains(guide, "zsc noop --silent") {
		t.Error("standard-mode generate guide should contain 'zsc noop --silent' from bootstrap-generate-standard atom")
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
		Mode:       PlanModeStandard,
		ImportYAML: "project:\n  name: laravel-minimal-agent\nservices:\n  - hostname: appdev\n    type: php-nginx@8.4\n",
	}
	bs.Plan = &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "php-nginx@8.4"}},
	}}
	guide := bs.buildGuide(StepProvision, 0, EnvContainer, nil)
	if !strings.Contains(guide, "Recipe import YAML") {
		t.Error("provision guide should contain the recipe-import-YAML header for recipe route")
	}
	if !strings.Contains(guide, "hostname: appdev") {
		t.Error("provision guide should contain the injected YAML body")
	}
	if !strings.Contains(guide, "laravel-minimal") {
		t.Error("provision guide should name the matched recipe slug")
	}
	if !strings.Contains(guide, "standard") {
		t.Error("provision guide should surface the recipe mode alongside the YAML")
	}
}

func TestBuildGuide_Recipe_DiscoverInjectsImportYAMLAndMode(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Route = BootstrapRouteRecipe
	bs.RecipeMatch = &RecipeMatch{
		Slug:       "nextjs-ssr-hello-world",
		Confidence: 0.97,
		Mode:       PlanModeStandard,
		ImportYAML: "project:\n  name: nextjs-agent\nservices:\n  - hostname: appdev\n    type: nodejs@22\n    zeropsSetup: dev\n  - hostname: appstage\n    type: nodejs@22\n    zeropsSetup: prod\n",
	}
	guide := bs.buildGuide(StepDiscover, 0, EnvContainer, nil)
	if !strings.Contains(guide, "Recipe import YAML") {
		t.Error("discover guide should contain the recipe-import-YAML header so Claude can write the plan from it")
	}
	if !strings.Contains(guide, "hostname: appdev") {
		t.Error("discover guide should contain the injected YAML body")
	}
	if !strings.Contains(guide, "standard") {
		t.Error("discover guide should surface the recipe mode so Claude sets bootstrapMode correctly on every target")
	}
	if !strings.Contains(guide, "bootstrapMode") {
		t.Error("discover guide should explicitly tell Claude to set bootstrapMode on plan targets")
	}
}

func TestBuildGuide_Recipe_NonProvisionOrDiscoverStepDoesNotInjectYAML(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Route = BootstrapRouteRecipe
	bs.RecipeMatch = &RecipeMatch{
		Slug:       "laravel-minimal",
		Confidence: 0.97,
		Mode:       PlanModeStandard,
		ImportYAML: "project:\n  name: x\nservices:\n  - hostname: appdev\n",
	}
	bs.Plan = &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "php-nginx@8.4"}},
	}}
	guide := bs.buildGuide(StepDeploy, 0, EnvContainer, nil)
	if strings.Contains(guide, "Recipe import YAML") {
		t.Error("deploy guide should NOT contain the recipe-import-YAML block (discover+provision only)")
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

func TestBuildGuide_Generate_Simple_HasRealStart(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Plan = &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "app", Type: "nginx@1", BootstrapMode: PlanModeSimple}},
	}}
	guide := bs.buildGuide(StepGenerate, 0, EnvContainer, nil)
	if guide == "" {
		t.Fatal("generate guide should not be empty for simple-mode plan")
	}
	if !strings.Contains(guide, "REAL start command") {
		t.Error("simple-mode generate guide should contain 'REAL start command' from bootstrap-generate-simple atom")
	}
}

func TestBuildGuide_Generate_DevOnly_HasNoopStart(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Plan = &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "worker", Type: "bun@1.2", BootstrapMode: PlanModeDev}},
	}}
	guide := bs.buildGuide(StepGenerate, 0, EnvContainer, nil)
	if guide == "" {
		t.Fatal("generate guide should not be empty for dev-only-mode plan")
	}
	if !strings.Contains(guide, "zsc noop --silent") {
		t.Error("dev-only-mode generate guide should contain 'zsc noop --silent' from bootstrap-generate-dev atom")
	}
}

func TestBuildGuide_Generate_InjectsDiscoveredEnvVars(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Plan = &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
			Dependencies: []Dependency{{Hostname: "db", Type: "postgresql@16", Resolution: "CREATE"}}},
	}}
	bs.DiscoveredEnvVars = map[string][]string{
		"db": {"connectionString", "port"},
	}
	guide := bs.buildGuide(StepGenerate, 0, EnvContainer, nil)
	if !strings.Contains(guide, "Discovered Managed-Service Env Var Catalog") {
		t.Error("generate guide should contain the dynamic env var catalog when DiscoveredEnvVars is populated")
	}
	if !strings.Contains(guide, "${db_connectionString}") {
		t.Error("generate guide should contain cross-service env var references")
	}
}

func TestBuildGuide_Deploy_NoEnvVarCatalog(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Plan = &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"}},
	}}
	bs.DiscoveredEnvVars = map[string][]string{
		"cache": {"connectionString"},
	}
	guide := bs.buildGuide(StepDeploy, 0, EnvContainer, nil)
	// Env var catalog is injected only at generate — deploy is past that.
	if strings.Contains(guide, "${cache_connectionString}") {
		t.Error("deploy guide should NOT contain env var catalog (generate-only injection)")
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

func TestBuildGuide_Deploy_Standard_HasStandardAtom(t *testing.T) {
	t.Parallel()
	bs := NewBootstrapState()
	bs.Plan = &ServicePlan{Targets: []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"}},
	}}
	guide := bs.buildGuide(StepDeploy, 0, EnvContainer, nil)
	if guide == "" {
		t.Fatal("deploy guide should not be empty for standard-mode plan")
	}
	if !strings.Contains(guide, "Standard mode — deploy flow") {
		t.Error("standard-mode deploy guide should contain the bootstrap-deploy-standard atom body")
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
