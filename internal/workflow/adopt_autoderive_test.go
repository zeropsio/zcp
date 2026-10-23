package workflow

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
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
	resp, err := eng.BootstrapCompleteAdoptPlan(existing, []string{"appdev"}, runtime.Info{}, nil, nil)
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
	resp, err := eng.BootstrapCompleteAdoptPlan(existing, []string{"workerstage"}, runtime.Info{}, nil, nil)
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
	_, err := eng.BootstrapCompleteAdoptPlan(existing, nil, runtime.Info{}, nil, nil)
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
	_, err := eng.BootstrapCompleteAdoptPlan(existing, []string{"appdev", "appstage"}, runtime.Info{}, nil, nil)
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

// TestBootstrapCompleteAdoptPlan_SameBareTypeDifferentOS_RefusesWithTemplates pins
// the composite-aware pairing guard (Wave-1 finding): the common dev/stage shape is
// ubuntu/<rt> (dev) + alpine/<rt> (prod) — DIFFERENT composite bases, SAME bare type.
// Raw string equality missed this and silently committed two independent dev
// containers, which then dead-ended launch's git-push gate. The guard MUST compare
// topology.CanonicalBareForm so the pair is recognized and ErrAdoptPairingChoice fires.
func TestBootstrapCompleteAdoptPlan_SameBareTypeDifferentOS_RefusesWithTemplates(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := startAdopt(t, dir)

	existing := []platform.ServiceStack{
		userSvc("appdev", "ubuntu/nodejs@22"),
		userSvc("appstage", "alpine/nodejs@22"),
	}
	_, err := eng.BootstrapCompleteAdoptPlan(existing, []string{"appdev", "appstage"}, runtime.Info{}, nil, nil)
	if err == nil {
		t.Fatal("want ErrAdoptPairingChoice for same-bare-type (ubuntu/alpine) dev/stage pair, got nil")
	}
	if !errors.Is(err, ErrAdoptPairingChoice) {
		t.Fatalf("want ErrAdoptPairingChoice, got %v", err)
	}
	// Wave-3 render regression: the prompt must name the ACTUAL per-host types,
	// not render the dev-half type for both halves. appstage is alpine/nodejs@22,
	// NOT ubuntu/nodejs@22 — surfacing the Wave-1 composite-aware guard exposed
	// that adoptPairingChoice took a single svcType.
	msg := err.Error()
	for _, want := range []string{"ubuntu/nodejs@22", "alpine/nodejs@22"} {
		if !strings.Contains(msg, want) {
			t.Errorf("pairing prompt must name per-host type %q; got:\n%s", want, msg)
		}
	}
}

// TestBootstrapCompleteAdoptPlan_DifferentVersionSameRuntime_CommitsIndependent — two
// adoptable runtimes of the same family but DIFFERENT versions (nodejs@22 vs nodejs@20)
// are NOT a dev/stage pair (a pair shares the version). CanonicalBareForm keeps the
// version, so they must NOT trigger the pairing prompt — they commit independent.
func TestBootstrapCompleteAdoptPlan_DifferentVersionSameRuntime_CommitsIndependent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := startAdopt(t, dir)

	existing := []platform.ServiceStack{
		userSvc("appdev", "nodejs@22"),
		userSvc("api", "nodejs@20"),
	}
	resp, err := eng.BootstrapCompleteAdoptPlan(existing, []string{"appdev", "api"}, runtime.Info{}, nil, nil)
	if err != nil {
		t.Fatalf("different-version runtimes must commit independent, got %v", err)
	}
	if resp.Current == nil {
		t.Fatal("want advance after commit, got nil Current")
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
	resp, err := eng.BootstrapCompleteAdoptPlan(existing, []string{"appdev", "api"}, runtime.Info{}, nil, nil)
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
	_, err := eng.BootstrapCompleteAdoptPlan([]platform.ServiceStack{userSvc("appdev", "go@1")}, []string{"appdev"}, runtime.Info{}, nil, nil)
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
	_, err := eng.BootstrapCompleteAdoptPlan(existing, []string{"appdev", "apidone"}, runtime.Info{}, nil, nil)
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

	_, err := eng.BootstrapCompleteAdoptPlan([]platform.ServiceStack{userSvc("db", "postgresql@16")}, []string{"db"}, runtime.Info{}, nil, nil)
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
	_, err := eng.BootstrapCompleteAdoptPlan(existing, nil, runtime.Info{}, nil, nil)
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

	_, err = eng.BootstrapCompleteAdoptPlan(existing, []string{"mailpit"}, runtime.Info{}, nil, nil)
	if err == nil {
		t.Fatal("want scoped rejection for foreign live-session service, got nil")
	}
	if !strings.Contains(err.Error(), "unknown or non-adoptable") || !strings.Contains(err.Error(), "mailpit") {
		t.Fatalf("want mailpit rejected as non-adoptable, got %v", err)
	}
}

// TestBootstrapCompleteAdoptPlan_DevServicesSharingAVolume_AreOneVariantBPair
// (docs/spec-mate.md §2.8a, Decision: B is recognised from topology on every
// route): two dev services whose dev setups mount one local-storage volume
// are adopted as one pair holding the code volume, the volume is no
// dependency, and one dev service alone stays an ordinary adopt.
func TestBootstrapCompleteAdoptPlan_DevServicesSharingAVolume_AreOneVariantBPair(t *testing.T) {
	t.Parallel()
	existing := []platform.ServiceStack{
		userSvc("webdev", "bun@1.3"),
		userSvc("apidev", "go@1"),
		userSvc("vol", "local-storage:single@1"),
	}
	mount := func(setup, base string) AdoptVolumeSetup {
		return AdoptVolumeSetup{Setup: setup, RunBase: base, Volume: "vol", MountPath: "/var/www/ws", WorksIn: true,
			Content: map[string]topology.VolumeContent{"webdev": topology.VolumeAbsent, "apidev": topology.VolumeAbsent}}
	}
	eng := startAdopt(t, t.TempDir())
	if _, err := eng.BootstrapCompleteAdoptPlan(existing, []string{"webdev", "apidev"}, runtime.Info{}, nil,
		[]AdoptVolumeSetup{mount("dev", "bun@1.3"), mount("dev-api", "go@1")}); err != nil {
		t.Fatalf("BootstrapCompleteAdoptPlan: %v", err)
	}
	state, err := eng.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	targets := state.Bootstrap.Plan.Targets
	if len(targets) != 1 {
		t.Fatalf("want one pair, got %+v", targets)
	}
	rt := targets[0].Runtime
	cv := rt.CodeVolume
	if rt.DevHostname != "webdev" || cv == nil || cv.Hostname != "vol" || cv.MountPath != "/var/www/ws" ||
		len(cv.DevServices) != 1 || cv.DevServices[0].Hostname != "apidev" || cv.DevServices[0].SetupName != "dev-api" || rt.PrimarySetupName != "dev" {
		t.Errorf("pair = %+v codeVolume = %+v", rt, cv)
	}
	if len(targets[0].Dependencies) != 0 {
		t.Errorf("the code volume is no dependency: %+v", targets[0].Dependencies)
	}

	one := startAdopt(t, t.TempDir())
	if _, err := one.BootstrapCompleteAdoptPlan(existing, []string{"webdev"}, runtime.Info{}, nil, []AdoptVolumeSetup{mount("dev", "bun@1.3")}); err != nil {
		t.Fatalf("single: %v", err)
	}
	st, _ := one.GetState()
	if got := st.Bootstrap.Plan.Targets[0].Runtime.CodeVolume; got != nil {
		t.Errorf("one dev service has no code volume, got %+v", got)
	}
}

// TestBootstrapCompleteAdoptPlan_ASharedVolume_IsACodeVolumeOnlyWhenTheDevSetupsWorkInItAndItHoldsNoOtherContent:
// zcp never writes code into a volume it did not prove is a code volume.
// Adopt takes two dev services sharing a local-storage volume for a variant-B
// pair only when every dev setup works in the mount and every one of them
// finds nothing there yet, an empty folder or this repository. A Laravel app
// and worker sharing their storage volume adopt as two services, as before.
func TestBootstrapCompleteAdoptPlan_ASharedVolume_IsACodeVolumeOnlyWhenTheDevSetupsWorkInItAndItHoldsNoOtherContent(t *testing.T) {
	t.Parallel()
	type seen = map[string]topology.VolumeContent
	f2 := []platform.ServiceStack{userSvc("webdev", "bun@1.3"), userSvc("apidev", "go@1"), userSvc("vol", "local-storage:single@1")}
	f2Mounts := func(worksIn bool, content seen) []AdoptVolumeSetup {
		return []AdoptVolumeSetup{
			{Setup: "dev", RunBase: "bun@1.3", Volume: "vol", MountPath: "/var/www/ws", WorksIn: true, Content: content},
			{Setup: "dev-api", RunBase: "go@1", Volume: "vol", MountPath: "/var/www/ws", WorksIn: worksIn, Content: content},
		}
	}
	laravel := []platform.ServiceStack{userSvc("app", "php-nginx@8.4"), userSvc("worker", "php@8.4"), userSvc("storage", "local-storage:single@1")}
	laravelMounts := func(worksIn bool, content topology.VolumeContent) []AdoptVolumeSetup {
		c := seen{"app": content, "worker": content}
		return []AdoptVolumeSetup{
			{Setup: "app", RunBase: "php-nginx@8.4", Volume: "storage", MountPath: "/var/www/storage", WorksIn: worksIn, Content: c},
			{Setup: "worker", RunBase: "php@8.4", Volume: "storage", MountPath: "/var/www/storage", WorksIn: worksIn, Content: c},
		}
	}
	tests := []struct {
		name     string
		existing []platform.ServiceStack
		scope    []string
		mounts   []AdoptVolumeSetup
		wantPair bool
	}{
		{name: "F2 before its first deploy mounts the volume", existing: f2, scope: []string{"webdev", "apidev"},
			mounts: f2Mounts(true, seen{"webdev": topology.VolumeAbsent, "apidev": topology.VolumeAbsent}), wantPair: true},
		{name: "F2 with an empty volume", existing: f2, scope: []string{"webdev", "apidev"},
			mounts: f2Mounts(true, seen{"webdev": topology.VolumeEmpty, "apidev": topology.VolumeEmpty}), wantPair: true},
		{name: "F2 whose volume already holds the repository", existing: f2, scope: []string{"webdev", "apidev"},
			mounts: f2Mounts(true, seen{"webdev": topology.VolumeCheckout, "apidev": topology.VolumeCheckout}), wantPair: true},
		{name: "Laravel app and worker sharing their storage volume", existing: laravel, scope: []string{"app", "worker"},
			mounts: laravelMounts(false, topology.VolumeForeign)},
		{name: "Laravel's storage still empty", existing: laravel, scope: []string{"app", "worker"},
			mounts: laravelMounts(false, topology.VolumeEmpty)},
		{name: "a volume the dev setups work in that holds data", existing: laravel, scope: []string{"app", "worker"},
			mounts: laravelMounts(true, topology.VolumeForeign)},
		{name: "F2 where one dev setup works in /var/www", existing: f2, scope: []string{"webdev", "apidev"},
			mounts: f2Mounts(false, seen{"webdev": topology.VolumeAbsent, "apidev": topology.VolumeAbsent})},
		{name: "F2 where one dev service could not be read", existing: f2, scope: []string{"webdev", "apidev"},
			mounts: f2Mounts(true, seen{"webdev": topology.VolumeAbsent})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			eng := startAdopt(t, t.TempDir())
			if _, err := eng.BootstrapCompleteAdoptPlan(tt.existing, tt.scope, runtime.Info{}, nil, tt.mounts); err != nil {
				t.Fatalf("BootstrapCompleteAdoptPlan: %v", err)
			}
			state, err := eng.GetState()
			if err != nil {
				t.Fatalf("GetState: %v", err)
			}
			targets := state.Bootstrap.Plan.Targets
			if tt.wantPair {
				if len(targets) != 1 || targets[0].Runtime.CodeVolume == nil {
					t.Fatalf("want one variant-B pair, got %+v", targets)
				}
				return
			}
			if len(targets) != len(tt.scope) {
				t.Fatalf("want %d services adopted on their own, got %+v", len(tt.scope), targets)
			}
			for _, target := range targets {
				if target.Runtime.CodeVolume != nil {
					t.Errorf("%s holds no code volume, got %+v", target.Runtime.DevHostname, target.Runtime.CodeVolume)
				}
			}
		})
	}
}

// TestInferServicePairing_TwoPools_NoPairQuestion — two Swarm pools of one type
// are not a dev/stage pair (spec-workflows.md §8 PS-2): the adopt completion
// path never asks the pairing question for them, and an app next to them
// still commits as its own single target.
func TestInferServicePairing_TwoPools_NoPairQuestion(t *testing.T) {
	t.Parallel()
	existing := []platform.ServiceStack{
		userSvc("pool1", "swarm@1"),
		userSvc("pool2", "swarm@1"),
		userSvc("app", "nodejs@22"),
	}

	t.Run("scoped to both pools", func(t *testing.T) {
		t.Parallel()
		eng := startAdopt(t, t.TempDir())
		_, err := eng.BootstrapCompleteAdoptPlan(existing, []string{"pool1", "pool2"}, runtime.Info{}, nil, nil)
		if err == nil {
			t.Fatal("want a refusal for pool-only scope, got nil")
		}
		if errors.Is(err, ErrAdoptPairingChoice) {
			t.Fatalf("two pools must not raise the dev/stage pair question; got %v", err)
		}
	})

	t.Run("app beside two pools", func(t *testing.T) {
		t.Parallel()
		eng := startAdopt(t, t.TempDir())
		if _, err := eng.BootstrapCompleteAdoptPlan(existing, []string{"app"}, runtime.Info{}, nil, nil); err != nil {
			t.Fatalf("BootstrapCompleteAdoptPlan: %v", err)
		}
		state, err := eng.GetState()
		if err != nil {
			t.Fatalf("GetState: %v", err)
		}
		plan := state.Bootstrap.Plan
		if plan == nil || len(plan.Targets) != 1 || plan.Targets[0].Runtime.DevHostname != "app" {
			t.Fatalf("want a single app target, got %+v", plan)
		}
		if len(plan.Targets[0].Dependencies) != 0 {
			t.Errorf("a pool is no dependency of the app; got %+v", plan.Targets[0].Dependencies)
		}
	})
}
