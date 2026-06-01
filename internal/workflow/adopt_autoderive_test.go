package workflow

import (
	"errors"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// startAdopt opens a fresh adopt-route bootstrap session for the auto-derive tests.
func startAdopt(t *testing.T, dir string) *Engine {
	t.Helper()
	eng := NewEngine(dir, EnvContainer, nil)
	if _, err := eng.BootstrapStartWithRoute("proj-1", "adopt existing services", BootstrapRouteAdopt, ""); err != nil {
		t.Fatalf("BootstrapStartWithRoute(adopt): %v", err)
	}
	return eng
}

// TestBootstrapCompleteAdoptPlan_SingleRuntime_AutoCommits — one adoptable runtime
// plus a managed dep auto-derives a single isExisting dev target with the dep as
// EXISTS, commits, and advances the step. No hand-authored plan, no attestation.
func TestBootstrapCompleteAdoptPlan_SingleRuntime_AutoCommits(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := startAdopt(t, dir)

	existing := []platform.ServiceStack{
		userSvc("appdev", "php-nginx@8.4"),
		userSvc("db", "postgresql@16"),
	}
	resp, err := eng.BootstrapCompleteAdoptPlan(existing, runtime.Info{}, nil)
	if err != nil {
		t.Fatalf("BootstrapCompleteAdoptPlan: %v", err)
	}
	if resp.Current == nil || resp.Current.Name != "provision" {
		t.Fatalf("want advance to provision, got %+v", resp.Current)
	}
	if !strings.Contains(resp.Message, "appdev") {
		t.Errorf("response message should name the adopted host appdev; got: %q", resp.Message)
	}

	state, err := eng.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	plan := state.Bootstrap.Plan
	if plan == nil || len(plan.Targets) != 1 {
		t.Fatalf("want 1 derived target, got %+v", plan)
	}
	rt := plan.Targets[0].Runtime
	if rt.DevHostname != "appdev" || !rt.IsExisting {
		t.Errorf("target should be appdev isExisting=true, got %+v", rt)
	}
	if len(plan.Targets[0].Dependencies) != 1 || plan.Targets[0].Dependencies[0].Hostname != "db" ||
		plan.Targets[0].Dependencies[0].Resolution != ResolutionExists {
		t.Errorf("db should be an EXISTS dependency, got %+v", plan.Targets[0].Dependencies)
	}
}

// TestBootstrapCompleteAdoptPlan_TwoSameType_RefusesWithTemplates — exactly two
// adoptable runtimes of the same type is the canonical dev/stage shape: auto-derive
// MUST refuse (ErrAdoptPairingChoice) with copy-pasteable templates rather than
// silently committing two independent dev containers, and MUST NOT advance the step.
func TestBootstrapCompleteAdoptPlan_TwoSameType_RefusesWithTemplates(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := startAdopt(t, dir)

	existing := []platform.ServiceStack{
		userSvc("appdev", "php-nginx@8.4"),
		userSvc("appstage", "php-nginx@8.4"),
		userSvc("db", "postgresql@16"),
	}
	_, err := eng.BootstrapCompleteAdoptPlan(existing, runtime.Info{}, nil)
	if err == nil {
		t.Fatal("want ErrAdoptPairingChoice refusal, got nil")
	}
	if !errors.Is(err, ErrAdoptPairingChoice) {
		t.Fatalf("want ErrAdoptPairingChoice, got %v", err)
	}
	msg := err.Error()
	for _, needle := range []string{"appdev", "appstage", `"bootstrapMode":"standard"`, `"bootstrapMode":"dev"`, "stageHostname"} {
		if !strings.Contains(msg, needle) {
			t.Errorf("pairing prompt missing %q; got:\n%s", needle, msg)
		}
	}

	// State must remain at discover — nothing committed.
	state, err := eng.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if got := state.Bootstrap.CurrentStepName(); got != StepDiscover {
		t.Errorf("step must stay at discover after refusal, got %q", got)
	}
	if state.Bootstrap.Plan != nil {
		t.Errorf("no plan should be stored after refusal, got %+v", state.Bootstrap.Plan)
	}
}

// TestBootstrapCompleteAdoptPlan_MixedTypes_AutoCommitsIndependent — two adoptable
// runtimes of DIFFERENT types cannot be a dev/stage pair, so they commit as two
// independent dev containers without a prompt.
func TestBootstrapCompleteAdoptPlan_MixedTypes_AutoCommitsIndependent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := startAdopt(t, dir)

	existing := []platform.ServiceStack{
		userSvc("appdev", "php-nginx@8.4"),
		userSvc("api", "go@1"),
	}
	resp, err := eng.BootstrapCompleteAdoptPlan(existing, runtime.Info{}, nil)
	if err != nil {
		t.Fatalf("BootstrapCompleteAdoptPlan: %v", err)
	}
	if resp.Current == nil {
		t.Fatal("want advance after commit, got nil Current")
	}
	state, err := eng.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state.Bootstrap.Plan == nil || len(state.Bootstrap.Plan.Targets) != 2 {
		t.Fatalf("want 2 independent targets, got %+v", state.Bootstrap.Plan)
	}
	for _, tgt := range state.Bootstrap.Plan.Targets {
		if tgt.Runtime.EffectiveMode() != "dev" {
			t.Errorf("independent adoption should be dev mode, got %q for %s", tgt.Runtime.EffectiveMode(), tgt.Runtime.DevHostname)
		}
	}
}

// TestBootstrapCompleteAdoptPlan_RejectsNonAdoptRoute — auto-derive is adopt-route only.
func TestBootstrapCompleteAdoptPlan_RejectsNonAdoptRoute(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvContainer, nil)
	if _, err := eng.BootstrapStartWithRoute("proj-1", "classic", BootstrapRouteClassic, ""); err != nil {
		t.Fatalf("BootstrapStartWithRoute(classic): %v", err)
	}
	_, err := eng.BootstrapCompleteAdoptPlan([]platform.ServiceStack{userSvc("appdev", "go@1")}, runtime.Info{}, nil)
	if err == nil || !strings.Contains(err.Error(), "adopt-route only") {
		t.Fatalf("want adopt-route-only error, got %v", err)
	}
}

// TestBootstrapCompleteAdoptPlan_UsesCanonicalAdoptable — a service with a complete
// ServiceMeta is already adopted; the canonical adoptableServices classifier excludes
// it, so auto-derive must not re-adopt it. Proves auto-derive reuses the one classifier
// rather than a divergent ad-hoc filter.
func TestBootstrapCompleteAdoptPlan_UsesCanonicalAdoptable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := startAdopt(t, dir)

	// apidone is already adopted (complete meta) → excluded from candidates.
	if err := WriteServiceMeta(dir, &ServiceMeta{Hostname: "apidone", BootstrappedAt: "2026-06-01T00:00:00Z"}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	existing := []platform.ServiceStack{
		userSvc("appdev", "php-nginx@8.4"),
		userSvc("apidone", "go@1"),
	}
	resp, err := eng.BootstrapCompleteAdoptPlan(existing, runtime.Info{}, nil)
	if err != nil {
		t.Fatalf("BootstrapCompleteAdoptPlan: %v", err)
	}
	_ = resp
	state, err := eng.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state.Bootstrap.Plan == nil || len(state.Bootstrap.Plan.Targets) != 1 {
		t.Fatalf("want only appdev adopted (apidone has complete meta), got %+v", state.Bootstrap.Plan)
	}
	if state.Bootstrap.Plan.Targets[0].Runtime.DevHostname != "appdev" {
		t.Errorf("want appdev, got %q", state.Bootstrap.Plan.Targets[0].Runtime.DevHostname)
	}
}

// TestBootstrapCompleteAdoptPlan_NoRuntimes_Errors — a project with only managed
// services has nothing to adopt.
func TestBootstrapCompleteAdoptPlan_NoRuntimes_Errors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := startAdopt(t, dir)

	_, err := eng.BootstrapCompleteAdoptPlan([]platform.ServiceStack{userSvc("db", "postgresql@16")}, runtime.Info{}, nil)
	if err == nil || !strings.Contains(err.Error(), "no adoptable runtime services") {
		t.Fatalf("want no-adoptable error, got %v", err)
	}
}
