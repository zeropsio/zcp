// Tests for buildGuide assembly — iteration hard-stop, atom synthesis per
// step/mode/route, env-var catalog injection at close, and recipe-import-YAML
// injection at discover/provision.
package workflow

import (
	"strings"
	"testing"
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

func TestBuildGuide_Recipe_CloseDoesNotInjectYAML(t *testing.T) {
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
	guide := bs.buildGuide(StepClose, 0, EnvContainer, nil)
	if strings.Contains(guide, "Recipe import YAML") {
		t.Error("close guide should NOT contain the recipe-import-YAML block (discover+provision only)")
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
