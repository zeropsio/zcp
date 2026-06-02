package workflow

import (
	"errors"
	"os"
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

// TestBootstrapCompleteAdoptPlan_SingleRuntime_ScopedAutoCommits — one named
// adoptable runtime plus a managed dep auto-derives a single isExisting dev
// target with the dep as EXISTS, commits, and advances the step. No
// hand-authored plan, no attestation.
func TestBootstrapCompleteAdoptPlan_SingleRuntime_ScopedAutoCommits(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := startAdopt(t, dir)

	existing := []platform.ServiceStack{
		userSvc("appdev", "php-nginx@8.4"),
		userSvc("db", "postgresql@16"),
	}
	resp, err := eng.BootstrapCompleteAdoptPlan(existing, []string{"appdev"}, runtime.Info{}, nil)
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

// TestBootstrapCompleteAdoptPlan_ScopedToNamed — a named subset derives a plan
// from exactly those hostnames, not every adoptable runtime in the project.
func TestBootstrapCompleteAdoptPlan_ScopedToNamed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := startAdopt(t, dir)

	existing := []platform.ServiceStack{
		userSvc("appdev", "php-nginx@8.4"),
		userSvc("api", "go@1"),
		userSvc("workerstage", "nodejs@22"),
		userSvc("db", "postgresql@16"),
	}
	resp, err := eng.BootstrapCompleteAdoptPlan(existing, []string{"workerstage"}, runtime.Info{}, nil)
	if err != nil {
		t.Fatalf("BootstrapCompleteAdoptPlan: %v", err)
	}
	if resp.Current == nil || resp.Current.Name != "provision" {
		t.Fatalf("want advance to provision, got %+v", resp.Current)
	}

	state, err := eng.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state.Bootstrap.Plan == nil || len(state.Bootstrap.Plan.Targets) != 1 {
		t.Fatalf("want exactly 1 scoped target, got %+v", state.Bootstrap.Plan)
	}
	rt := state.Bootstrap.Plan.Targets[0].Runtime
	if rt.DevHostname != "workerstage" || rt.Type != "nodejs@22" {
		t.Fatalf("scope should adopt only workerstage, got %+v", rt)
	}
	if strings.Contains(resp.Message, "appdev") || strings.Contains(resp.Message, "api") {
		t.Errorf("response must not imply unscoped hosts were adopted; got: %q", resp.Message)
	}
	if len(state.Bootstrap.Plan.Targets[0].Dependencies) != 1 || state.Bootstrap.Plan.Targets[0].Dependencies[0].Hostname != "db" {
		t.Errorf("managed deps should still attach as EXISTS, got %+v", state.Bootstrap.Plan.Targets[0].Dependencies)
	}
}

// TestBootstrapCompleteAdoptPlan_EmptyScopeListsCandidates — omitted scope is
// a diagnostic, not permission to adopt every runtime in the project.
func TestBootstrapCompleteAdoptPlan_EmptyScopeListsCandidates(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := startAdopt(t, dir)

	existing := []platform.ServiceStack{
		userSvc("appdev", "php-nginx@8.4"),
		userSvc("api", "go@1"),
		userSvc("db", "postgresql@16"),
	}
	_, err := eng.BootstrapCompleteAdoptPlan(existing, nil, runtime.Info{}, nil)
	if err == nil {
		t.Fatal("want empty-scope diagnostic, got nil")
	}
	msg := err.Error()
	for _, needle := range []string{"adopt scope is required", "available adoptable runtime services", "appdev", "api"} {
		if !strings.Contains(msg, needle) {
			t.Errorf("empty-scope diagnostic missing %q; got:\n%s", needle, msg)
		}
	}
	if strings.Contains(msg, "db") {
		t.Errorf("managed service must not appear as adoptable candidate; got:\n%s", msg)
	}

	state, err := eng.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if got := state.Bootstrap.CurrentStepName(); got != StepDiscover {
		t.Errorf("step must stay at discover after empty-scope diagnostic, got %q", got)
	}
	if state.Bootstrap.Plan != nil {
		t.Errorf("no plan should be stored after empty-scope diagnostic, got %+v", state.Bootstrap.Plan)
	}
	for _, hostname := range []string{"appdev", "api"} {
		meta, err := ReadServiceMeta(dir, hostname)
		if err != nil {
			t.Fatalf("ReadServiceMeta(%s): %v", hostname, err)
		}
		if meta != nil {
			t.Errorf("empty-scope diagnostic must not write ServiceMeta for %s; got %+v", hostname, meta)
		}
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
	_, err := eng.BootstrapCompleteAdoptPlan(existing, []string{"appdev", "appstage"}, runtime.Info{}, nil)
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

// TestBootstrapCompleteAdoptPlan_MixedTypes_ScopedAutoCommitsIndependent — two
// named adoptable runtimes of DIFFERENT types cannot be a dev/stage pair, so
// they commit as two independent dev containers without a prompt.
func TestBootstrapCompleteAdoptPlan_MixedTypes_ScopedAutoCommitsIndependent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := startAdopt(t, dir)

	existing := []platform.ServiceStack{
		userSvc("appdev", "php-nginx@8.4"),
		userSvc("api", "go@1"),
	}
	resp, err := eng.BootstrapCompleteAdoptPlan(existing, []string{"appdev", "api"}, runtime.Info{}, nil)
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
	_, err := eng.BootstrapCompleteAdoptPlan([]platform.ServiceStack{userSvc("appdev", "go@1")}, []string{"appdev"}, runtime.Info{}, nil)
	if err == nil || !strings.Contains(err.Error(), "adopt-route only") {
		t.Fatalf("want adopt-route-only error, got %v", err)
	}
}

// TestBootstrapCompleteAdoptPlan_UsesCanonicalAdoptable — a service with a
// complete ServiceMeta is already adopted; the scoped derive rejects it as
// non-adoptable. Proves auto-derive reuses the canonical classifier rather than
// a divergent ad-hoc filter.
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
	_, err := eng.BootstrapCompleteAdoptPlan(existing, []string{"appdev", "apidone"}, runtime.Info{}, nil)
	if err == nil {
		t.Fatal("want non-adoptable completed-meta rejection, got nil")
	}
	if !strings.Contains(err.Error(), "unknown or non-adoptable") || !strings.Contains(err.Error(), "apidone") {
		t.Fatalf("want apidone rejected as non-adoptable, got %v", err)
	}
	state, err := eng.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state.Bootstrap.Plan != nil {
		t.Fatalf("no plan should be stored after completed-meta rejection, got %+v", state.Bootstrap.Plan)
	}
}

// TestBootstrapCompleteAdoptPlan_NoRuntimes_Errors — a project with only managed
// services has nothing to adopt.
func TestBootstrapCompleteAdoptPlan_NoRuntimes_Errors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := startAdopt(t, dir)

	_, err := eng.BootstrapCompleteAdoptPlan([]platform.ServiceStack{userSvc("db", "postgresql@16")}, []string{"db"}, runtime.Info{}, nil)
	if err == nil || !strings.Contains(err.Error(), "no adoptable runtime services") {
		t.Fatalf("want no-adoptable error, got %v", err)
	}
}

// TestBootstrapCompleteAdoptPlan_ExcludesConcurrentSessionService — a runtime
// tagged to a different live bootstrap session is not listed in empty-scope
// candidates and cannot be adopted even if named.
func TestBootstrapCompleteAdoptPlan_ExcludesConcurrentSessionService(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := startAdopt(t, dir)

	if err := WriteServiceMeta(dir, &ServiceMeta{
		Hostname:         "mailpit",
		BootstrapSession: "foreign-live",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	if err := RegisterSession(dir, SessionEntry{
		SessionID: "foreign-live",
		PID:       os.Getpid(),
		StartTime: CurrentProcessStartTime(),
		Workflow:  WorkflowBootstrap,
		ProjectID: "proj-1",
		Intent:    "concurrent mailpit bootstrap",
	}); err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}

	existing := []platform.ServiceStack{
		userSvc("appdev", "php-nginx@8.4"),
		userSvc("mailpit", "nodejs@22"),
	}
	_, err := eng.BootstrapCompleteAdoptPlan(existing, nil, runtime.Info{}, nil)
	if err == nil {
		t.Fatal("want empty-scope diagnostic, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "appdev") {
		t.Fatalf("candidate diagnostic should include appdev; got:\n%s", msg)
	}
	if strings.Contains(msg, "mailpit") {
		t.Fatalf("foreign live-session service must not be listed as adoptable; got:\n%s", msg)
	}

	_, err = eng.BootstrapCompleteAdoptPlan(existing, []string{"mailpit"}, runtime.Info{}, nil)
	if err == nil {
		t.Fatal("want scoped rejection for foreign live-session service, got nil")
	}
	if !strings.Contains(err.Error(), "unknown or non-adoptable") || !strings.Contains(err.Error(), "mailpit") {
		t.Fatalf("want mailpit rejected as non-adoptable, got %v", err)
	}
}
