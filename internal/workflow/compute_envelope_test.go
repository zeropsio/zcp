package workflow

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// fixedTime is the canonical time used across envelope fixtures so test output
// stays byte-stable regardless of wall-clock drift.
var fixedTime = time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)

func TestComputeEnvelope_IdleEmptyProject(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	env, err := ComputeEnvelope(context.Background(), nil, dir, "", runtime.Info{}, fixedTime)
	if err != nil {
		t.Fatalf("ComputeEnvelope: %v", err)
	}
	if env.Phase != PhaseIdle {
		t.Errorf("phase = %q, want idle", env.Phase)
	}
	if env.Environment != EnvLocal {
		t.Errorf("environment = %q, want local", env.Environment)
	}
	if len(env.Services) != 0 {
		t.Errorf("services = %d, want 0", len(env.Services))
	}
	if env.SelfService != nil {
		t.Errorf("selfService = %+v, want nil (local)", env.SelfService)
	}
	if !env.Generated.Equal(fixedTime) {
		t.Errorf("generated = %v, want %v", env.Generated, fixedTime)
	}
}

func TestComputeEnvelope_ContainerSelfService(t *testing.T) {
	t.Parallel()

	rt := runtime.Info{InContainer: true, ServiceName: "zcpdev", ServiceID: "zcp-id"}
	env, err := ComputeEnvelope(context.Background(), nil, t.TempDir(), "", rt, fixedTime)
	if err != nil {
		t.Fatalf("ComputeEnvelope: %v", err)
	}
	if env.Environment != EnvContainer {
		t.Errorf("environment = %q, want container", env.Environment)
	}
	if env.SelfService == nil || env.SelfService.Hostname != "zcpdev" {
		t.Errorf("selfService = %+v, want zcpdev", env.SelfService)
	}
}

func TestComputeEnvelope_ServicesBootstrapped(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Write a complete meta for "appdev" with stage pair "appstage".
	meta := &ServiceMeta{
		Hostname:          "appdev",
		Mode:              PlanModeStandard,
		StageHostname:     "appstage",
		DeployStrategy:    StrategyPushDev,
		StrategyConfirmed: true,
		BootstrappedAt:    fixedTime.Format(time.RFC3339),
		BootstrapSession:  "sess-1",
	}
	if err := WriteServiceMeta(dir, meta); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	svcAppDev := platform.ServiceStack{
		ID: "s1", Name: "appdev", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeVersionName:  "nodejs@22",
			ServiceStackTypeCategoryName: "USER",
		},
		Ports: []platform.Port{{Port: 3000}},
	}
	svcAppStage := platform.ServiceStack{
		ID: "s2", Name: "appstage", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeVersionName:  "nodejs@22",
			ServiceStackTypeCategoryName: "USER",
		},
		Ports: []platform.Port{{Port: 3000}},
	}
	svcDB := platform.ServiceStack{
		ID: "s3", Name: "db", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeVersionName:  "postgresql@16",
			ServiceStackTypeCategoryName: "USER",
		},
	}
	svcCore := platform.ServiceStack{
		ID: "sys1", Name: "l7",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeVersionName:  "l7-balancer",
			ServiceStackTypeCategoryName: "HTTP_L7_BALANCER",
		},
	}

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{svcAppDev, svcAppStage, svcDB, svcCore}).
		WithProject(&platform.Project{ID: "p1", Name: "demo"})

	env, err := ComputeEnvelope(context.Background(), mock, dir, "p1", runtime.Info{}, fixedTime)
	if err != nil {
		t.Fatalf("ComputeEnvelope: %v", err)
	}

	if env.Phase != PhaseIdle {
		t.Errorf("phase = %q, want idle (no work session)", env.Phase)
	}
	if env.Project.ID != "p1" || env.Project.Name != "demo" {
		t.Errorf("project = %+v, want {p1,demo}", env.Project)
	}

	wantHostnames := []string{"appdev", "appstage", "db"}
	if len(env.Services) != len(wantHostnames) {
		t.Fatalf("services = %d, want %d: %+v", len(env.Services), len(wantHostnames), env.Services)
	}
	for i, want := range wantHostnames {
		if env.Services[i].Hostname != want {
			t.Errorf("services[%d] = %q, want %q", i, env.Services[i].Hostname, want)
		}
	}

	// appdev snapshot: dynamic, bootstrapped, stage pair, dev-half of standard.
	appdev := env.Services[0]
	if appdev.RuntimeClass != RuntimeDynamic {
		t.Errorf("appdev runtime = %q, want dynamic", appdev.RuntimeClass)
	}
	if !appdev.Bootstrapped {
		t.Error("appdev.Bootstrapped = false, want true")
	}
	if appdev.Strategy != DeployStrategy(StrategyPushDev) {
		t.Errorf("appdev strategy = %q, want push-dev", appdev.Strategy)
	}
	if appdev.StageHostname != "appstage" {
		t.Errorf("appdev stage = %q, want appstage", appdev.StageHostname)
	}
	if appdev.Mode != ModeStandard {
		t.Errorf("appdev mode = %q, want standard (dev half of pair)", appdev.Mode)
	}

	// appstage snapshot: stage half of standard pair.
	appstage := env.Services[1]
	if appstage.Mode != ModeStage {
		t.Errorf("appstage mode = %q, want stage", appstage.Mode)
	}
	if appstage.StageHostname != "" {
		t.Errorf("appstage stageHostname = %q, want empty", appstage.StageHostname)
	}

	// db snapshot: managed.
	db := env.Services[2]
	if db.RuntimeClass != RuntimeManaged {
		t.Errorf("db runtime = %q, want managed", db.RuntimeClass)
	}
	if db.Bootstrapped {
		t.Error("db.Bootstrapped = true, want false")
	}
}

func TestResolveEnvelopeMode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		meta     *ServiceMeta
		hostname string
		want     Mode
	}{
		{
			name:     "standard_dev_half",
			meta:     &ServiceMeta{Hostname: "appdev", Mode: PlanModeStandard, StageHostname: "appstage"},
			hostname: "appdev",
			want:     ModeStandard,
		},
		{
			name:     "standard_stage_half",
			meta:     &ServiceMeta{Hostname: "appdev", Mode: PlanModeStandard, StageHostname: "appstage"},
			hostname: "appstage",
			want:     ModeStage,
		},
		{
			name:     "dev_only",
			meta:     &ServiceMeta{Hostname: "appdev", Mode: PlanModeDev},
			hostname: "appdev",
			want:     ModeDev,
		},
		{
			name:     "simple",
			meta:     &ServiceMeta{Hostname: "app", Mode: PlanModeSimple},
			hostname: "app",
			want:     ModeSimple,
		},
		{
			name:     "local_standard_stage_only",
			meta:     &ServiceMeta{Hostname: "appstage", Mode: PlanModeStandard, Environment: string(EnvLocal)},
			hostname: "appstage",
			want:     ModeStage,
		},
		{
			name:     "empty_mode_with_stage_treated_as_standard",
			meta:     &ServiceMeta{Hostname: "appdev", Mode: "", StageHostname: "appstage"},
			hostname: "appdev",
			want:     ModeStandard,
		},
		{
			name:     "nil_meta",
			meta:     nil,
			hostname: "appdev",
			want:     "",
		},
		{
			name:     "unknown_hostname",
			meta:     &ServiceMeta{Hostname: "appdev", Mode: PlanModeDev},
			hostname: "other",
			want:     "",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := resolveEnvelopeMode(tt.meta, tt.hostname)
			if got != tt.want {
				t.Errorf("resolveEnvelopeMode(%+v, %q) = %q, want %q", tt.meta, tt.hostname, got, tt.want)
			}
		})
	}
}

func TestComputeEnvelope_PhaseFromWorkSession(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		closedAt    string
		closeReason string
		wantPhase   Phase
	}{
		{"open_session_is_active", "", "", PhaseDevelopActive},
		{"auto_closed_is_closed_auto", fixedTime.Format(time.RFC3339), CloseReasonAutoComplete, PhaseDevelopClosed},
		{"explicit_closed_is_idle", fixedTime.Format(time.RFC3339), CloseReasonExplicit, PhaseIdle},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			ws := NewWorkSession("p1", string(EnvLocal), "fix login", []string{"appdev"})
			ws.ClosedAt = tt.closedAt
			ws.CloseReason = tt.closeReason
			if err := SaveWorkSession(dir, ws); err != nil {
				t.Fatalf("SaveWorkSession: %v", err)
			}
			env, err := ComputeEnvelope(context.Background(), nil, dir, "", runtime.Info{}, fixedTime)
			if err != nil {
				t.Fatalf("ComputeEnvelope: %v", err)
			}
			if env.Phase != tt.wantPhase {
				t.Errorf("phase = %q, want %q", env.Phase, tt.wantPhase)
			}
			if tt.wantPhase == PhaseIdle && env.WorkSession == nil {
				t.Error("expected WorkSessionSummary even when closed explicitly")
			}
		})
	}
}

func TestComputeEnvelope_ParallelIO(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Sanity: the three reads should complete; result deterministic under the
	// same inputs. We verify determinism by running ComputeEnvelope twice and
	// checking envelope equality (time is supplied explicitly).
	meta := &ServiceMeta{
		Hostname:         "appdev",
		Mode:             PlanModeDev,
		DeployStrategy:   StrategyPushGit,
		BootstrappedAt:   fixedTime.Format(time.RFC3339),
		BootstrapSession: "sess-1",
	}
	if err := WriteServiceMeta(dir, meta); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{
				ID: "s1", Name: "appdev", Status: "ACTIVE",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName:  "nodejs@22",
					ServiceStackTypeCategoryName: "USER",
				},
				Ports: []platform.Port{{Port: 3000}},
			},
		}).
		WithProject(&platform.Project{ID: "p1", Name: "demo"})

	first, err := ComputeEnvelope(context.Background(), mock, dir, "p1", runtime.Info{}, fixedTime)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := ComputeEnvelope(context.Background(), mock, dir, "p1", runtime.Info{}, fixedTime)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if !envelopesEqualByJSON(t, first, second) {
		t.Fatalf("non-deterministic envelope:\n%+v\nvs\n%+v", first, second)
	}
}

// envelopesEqualByJSON is the compaction-safety check: two envelopes MUST
// serialise byte-identically. Used in place of reflect.DeepEqual because the
// canonical equality is JSON bytes (what the LLM sees).
func envelopesEqualByJSON(t *testing.T, a, b StateEnvelope) bool {
	t.Helper()
	aBytes, err := marshalEnvelopeForTest(a)
	if err != nil {
		t.Fatalf("marshal a: %v", err)
	}
	bBytes, err := marshalEnvelopeForTest(b)
	if err != nil {
		t.Fatalf("marshal b: %v", err)
	}
	return string(aBytes) == string(bBytes)
}

// TestComputeEnvelope_StateDirEmptyOK covers the "no state dir, no client"
// path — callers like `zcp --help` must not panic when the envelope is asked
// for without any initialised state.
func TestComputeEnvelope_StateDirEmptyOK(t *testing.T) {
	t.Parallel()

	env, err := ComputeEnvelope(context.Background(), nil, "", "", runtime.Info{}, fixedTime)
	if err != nil {
		t.Fatalf("ComputeEnvelope: %v", err)
	}
	if env.Phase != PhaseIdle {
		t.Errorf("phase = %q, want idle", env.Phase)
	}
}

// TestComputeEnvelope_SkipsSystemAndSelf verifies the services list is cleaned
// of system containers and the self-hostname (container-mode only).
func TestComputeEnvelope_SkipsSystemAndSelf(t *testing.T) {
	t.Parallel()

	svcSelf := platform.ServiceStack{
		ID: "self", Name: "zcpdev", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeVersionName:  "go@1.23",
			ServiceStackTypeCategoryName: "USER",
		},
	}
	svcCore := platform.ServiceStack{
		ID: "sys", Name: "balancer",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeVersionName:  "l7",
			ServiceStackTypeCategoryName: "HTTP_L7_BALANCER",
		},
	}
	svcOther := platform.ServiceStack{
		ID: "app", Name: "appdev", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeVersionName:  "bun@1",
			ServiceStackTypeCategoryName: "USER",
		},
		Ports: []platform.Port{{Port: 3000}},
	}

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{svcSelf, svcCore, svcOther}).
		WithProject(&platform.Project{ID: "p1", Name: "demo"})

	rt := runtime.Info{InContainer: true, ServiceName: "zcpdev"}
	env, err := ComputeEnvelope(context.Background(), mock, t.TempDir(), "p1", rt, fixedTime)
	if err != nil {
		t.Fatalf("ComputeEnvelope: %v", err)
	}

	if len(env.Services) != 1 || env.Services[0].Hostname != "appdev" {
		t.Errorf("services = %+v, want exactly [appdev]", env.Services)
	}
}

// TestComputeEnvelope_BootstrapSummaryFromSession verifies that an existing
// BootstrapSession on disk surfaces as StateEnvelope.Bootstrap with the
// correct route. This is the signal atoms use to target bootstrap routes.
func TestComputeEnvelope_BootstrapSummaryFromSession(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	match := &RecipeMatch{Slug: "laravel-minimal", Confidence: 0.92}
	sess := NewBootstrapSession(BootstrapRouteRecipe, "laravel dashboard", match)
	if err := SaveBootstrapSession(dir, sess); err != nil {
		t.Fatalf("SaveBootstrapSession: %v", err)
	}

	env, err := ComputeEnvelope(context.Background(), nil, dir, "", runtime.Info{}, fixedTime)
	if err != nil {
		t.Fatalf("ComputeEnvelope: %v", err)
	}

	if env.Bootstrap == nil {
		t.Fatal("expected Bootstrap summary, got nil")
	}
	if env.Bootstrap.Route != BootstrapRouteRecipe {
		t.Errorf("Route = %q, want recipe", env.Bootstrap.Route)
	}
	if env.Bootstrap.Intent != "laravel dashboard" {
		t.Errorf("Intent = %q", env.Bootstrap.Intent)
	}
	if env.Bootstrap.RecipeMatch == nil || env.Bootstrap.RecipeMatch.Slug != "laravel-minimal" {
		t.Errorf("RecipeMatch = %+v, want slug laravel-minimal", env.Bootstrap.RecipeMatch)
	}
	// Top-level Recipe summary mirrors Bootstrap.RecipeMatch.
	if env.Recipe == nil || env.Recipe.Slug != "laravel-minimal" {
		t.Errorf("Recipe = %+v, want slug laravel-minimal", env.Recipe)
	}
}

// TestComputeEnvelope_BootstrapAbsentWhenNoSession confirms that envelopes
// computed without a bootstrap session file on disk leave Bootstrap nil.
// Guards against accidental coupling of the Bootstrap field to other state.
func TestComputeEnvelope_BootstrapAbsentWhenNoSession(t *testing.T) {
	t.Parallel()

	env, err := ComputeEnvelope(context.Background(), nil, t.TempDir(), "", runtime.Info{}, fixedTime)
	if err != nil {
		t.Fatalf("ComputeEnvelope: %v", err)
	}
	if env.Bootstrap != nil {
		t.Errorf("Bootstrap = %+v, want nil", env.Bootstrap)
	}
}

// marshalEnvelopeForTest is the canonical JSON encoder used in tests. Go's
// encoding/json already sorts map keys alphabetically, so plain Marshal is
// deterministic when slice ordering is controlled — which buildServiceSnapshots
// does by sorting on Hostname.
func marshalEnvelopeForTest(env StateEnvelope) ([]byte, error) {
	return json.Marshal(env)
}
