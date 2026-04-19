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
