package workflow

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
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
		Mode:              topology.PlanModeStandard,
		StageHostname:     "appstage",
		DeployStrategy:    topology.StrategyPushDev,
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
	if appdev.RuntimeClass != topology.RuntimeDynamic {
		t.Errorf("appdev runtime = %q, want dynamic", appdev.RuntimeClass)
	}
	if !appdev.Bootstrapped {
		t.Error("appdev.Bootstrapped = false, want true")
	}
	if appdev.Strategy != topology.DeployStrategy(topology.StrategyPushDev) {
		t.Errorf("appdev strategy = %q, want push-dev", appdev.Strategy)
	}
	if appdev.StageHostname != "appstage" {
		t.Errorf("appdev stage = %q, want appstage", appdev.StageHostname)
	}
	if appdev.Mode != topology.ModeStandard {
		t.Errorf("appdev mode = %q, want standard (dev half of pair)", appdev.Mode)
	}

	// appstage snapshot: stage half of standard pair.
	appstage := env.Services[1]
	if appstage.Mode != topology.ModeStage {
		t.Errorf("appstage mode = %q, want stage", appstage.Mode)
	}
	if appstage.StageHostname != "" {
		t.Errorf("appstage stageHostname = %q, want empty", appstage.StageHostname)
	}

	// db snapshot: managed.
	db := env.Services[2]
	if db.RuntimeClass != topology.RuntimeManaged {
		t.Errorf("db runtime = %q, want managed", db.RuntimeClass)
	}
	if db.Bootstrapped {
		t.Error("db.Bootstrapped = true, want false")
	}

	// Neither appdev nor appstage has FirstDeployedAt — Deployed must be false
	// on both so the first-deploy branch atoms fire for this envelope.
	if appdev.Deployed {
		t.Error("appdev.Deployed = true, want false (no FirstDeployedAt on meta)")
	}
	if appstage.Deployed {
		t.Error("appstage.Deployed = true, want false (no FirstDeployedAt on meta)")
	}
}

// TestComputeEnvelope_ServiceDeployedFlag pins the three signals
// DeriveDeployed OR-composes:
//
//  1. meta.FirstDeployedAt stamped — the durable signal for services that
//     have seen a successful deploy (session deploy or adoption-at-ACTIVE
//     both stamp this).
//  2. Session recorded success for this hostname — in-flight deploy just
//     landed, before meta sync.
//  3. Adopted (empty BootstrapSession) + platform Status=ACTIVE — legacy
//     path covering metas written before stamping shipped.
//
// Fresh ZCP bootstrap (BootstrapSession non-empty) without FirstDeployedAt
// and no session deploy correctly reports Deployed=false even at
// Status=ACTIVE, so the develop first-deploy branch fires.
//
// See compute_envelope.go:DeriveDeployed.
func TestComputeEnvelope_ServiceDeployedFlag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		meta         *ServiceMeta
		svcStatus    string
		recordDeploy bool
		wantDeployed bool
	}{
		{
			name: "fresh bootstrap + ACTIVE + no stamp = not deployed",
			meta: &ServiceMeta{
				Hostname:         "appdev",
				Mode:             topology.PlanModeDev,
				BootstrappedAt:   fixedTime.Format(time.RFC3339),
				BootstrapSession: "sess-1",
			},
			svcStatus:    "ACTIVE",
			wantDeployed: false,
		},
		{
			name: "FirstDeployedAt stamped = deployed regardless of Status",
			meta: &ServiceMeta{
				Hostname:         "appdev",
				Mode:             topology.PlanModeDev,
				BootstrappedAt:   fixedTime.Format(time.RFC3339),
				BootstrapSession: "sess-1",
				FirstDeployedAt:  fixedTime.Format(time.RFC3339),
			},
			svcStatus:    "BUILDING",
			wantDeployed: true,
		},
		{
			name: "adopted + ACTIVE without stamp = deployed (fizzy-export)",
			meta: &ServiceMeta{
				Hostname:         "appdev",
				Mode:             topology.PlanModeDev,
				BootstrappedAt:   fixedTime.Format(time.RFC3339),
				BootstrapSession: "",
			},
			svcStatus:    "ACTIVE",
			wantDeployed: true,
		},
		{
			name: "adopted + READY_TO_DEPLOY = not deployed",
			meta: &ServiceMeta{
				Hostname:         "appdev",
				Mode:             topology.PlanModeDev,
				BootstrappedAt:   fixedTime.Format(time.RFC3339),
				BootstrapSession: "",
			},
			svcStatus:    "READY_TO_DEPLOY",
			wantDeployed: false,
		},
		{
			name: "fresh bootstrap + session success + stamp = deployed",
			meta: &ServiceMeta{
				Hostname:         "appdev",
				Mode:             topology.PlanModeDev,
				BootstrappedAt:   fixedTime.Format(time.RFC3339),
				BootstrapSession: "sess-1",
			},
			svcStatus:    "ACTIVE",
			recordDeploy: true, // RecordDeployAttempt both records session AND stamps meta
			wantDeployed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			if err := WriteServiceMeta(dir, tt.meta); err != nil {
				t.Fatalf("WriteServiceMeta: %v", err)
			}

			if tt.recordDeploy {
				ws := NewWorkSession("p1", string(EnvContainer), "test", []string{tt.meta.Hostname})
				if err := SaveWorkSession(dir, ws); err != nil {
					t.Fatalf("SaveWorkSession: %v", err)
				}
				if err := RecordDeployAttempt(dir, tt.meta.Hostname, DeployAttempt{
					AttemptedAt: fixedTime.Format(time.RFC3339),
					SucceededAt: fixedTime.Format(time.RFC3339),
				}); err != nil {
					t.Fatalf("RecordDeployAttempt: %v", err)
				}
			}

			svc := platform.ServiceStack{
				ID: "s1", Name: tt.meta.Hostname, Status: tt.svcStatus,
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName:  "nodejs@22",
					ServiceStackTypeCategoryName: "USER",
				},
			}
			mock := platform.NewMock().
				WithServices([]platform.ServiceStack{svc}).
				WithProject(&platform.Project{ID: "p1", Name: "demo"})

			env, err := ComputeEnvelope(context.Background(), mock, dir, "p1", runtime.Info{}, fixedTime)
			if err != nil {
				t.Fatalf("ComputeEnvelope: %v", err)
			}
			if len(env.Services) != 1 {
				t.Fatalf("services = %d, want 1", len(env.Services))
			}
			if got := env.Services[0].Deployed; got != tt.wantDeployed {
				t.Errorf("Deployed = %v, want %v", got, tt.wantDeployed)
			}
		})
	}
}

// TestComputeEnvelope_ResumableFlag covers the Resumable-field projection
// from an incomplete ServiceMeta tagged with a BootstrapSession. IdleIncomplete
// scenario fires when any non-managed service is resumable.
func TestComputeEnvelope_ResumableFlag(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	meta := &ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeDev,
		BootstrapSession: "sess-abandoned",
		// BootstrappedAt intentionally empty — incomplete.
	}
	if err := WriteServiceMeta(dir, meta); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	svc := platform.ServiceStack{
		ID: "s1", Name: "appdev", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeVersionName:  "nodejs@22",
			ServiceStackTypeCategoryName: "USER",
		},
	}
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{svc}).
		WithProject(&platform.Project{ID: "p1", Name: "demo"})

	env, err := ComputeEnvelope(context.Background(), mock, dir, "p1", runtime.Info{}, fixedTime)
	if err != nil {
		t.Fatalf("ComputeEnvelope: %v", err)
	}
	if len(env.Services) != 1 {
		t.Fatalf("services = %d, want 1", len(env.Services))
	}
	if !env.Services[0].Resumable {
		t.Error("appdev.Resumable = false, want true (incomplete meta with BootstrapSession)")
	}
	if env.Services[0].Bootstrapped {
		t.Error("appdev.Bootstrapped = true, want false (meta is incomplete)")
	}
	if env.IdleScenario != IdleIncomplete {
		t.Errorf("idleScenario = %q, want %q (resumable service present)", env.IdleScenario, IdleIncomplete)
	}
}

// TestComputeEnvelope_OrphanIncompleteMeta covers the edge case of an
// incomplete meta without BootstrapSession — an orphan that fell off a
// session that no longer exists. Resumable should stay false (nothing to
// resume), and idleScenario should fall through to adopt.
func TestComputeEnvelope_OrphanIncompleteMeta(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	meta := &ServiceMeta{
		Hostname: "appdev",
		Mode:     topology.PlanModeDev,
		// Neither BootstrappedAt nor BootstrapSession set.
	}
	if err := WriteServiceMeta(dir, meta); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	svc := platform.ServiceStack{
		ID: "s1", Name: "appdev", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeVersionName:  "nodejs@22",
			ServiceStackTypeCategoryName: "USER",
		},
	}
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{svc}).
		WithProject(&platform.Project{ID: "p1", Name: "demo"})

	env, err := ComputeEnvelope(context.Background(), mock, dir, "p1", runtime.Info{}, fixedTime)
	if err != nil {
		t.Fatalf("ComputeEnvelope: %v", err)
	}
	if env.Services[0].Resumable {
		t.Error("orphan incomplete meta should not be Resumable")
	}
	if env.IdleScenario != IdleAdopt {
		t.Errorf("idleScenario = %q, want %q (orphan falls under adopt)", env.IdleScenario, IdleAdopt)
	}
}

func TestResolveEnvelopeMode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		meta     *ServiceMeta
		hostname string
		want     topology.Mode
	}{
		{
			name:     "standard_dev_half",
			meta:     &ServiceMeta{Hostname: "appdev", Mode: topology.PlanModeStandard, StageHostname: "appstage"},
			hostname: "appdev",
			want:     topology.ModeStandard,
		},
		{
			name:     "standard_stage_half",
			meta:     &ServiceMeta{Hostname: "appdev", Mode: topology.PlanModeStandard, StageHostname: "appstage"},
			hostname: "appstage",
			want:     topology.ModeStage,
		},
		{
			name:     "dev_only",
			meta:     &ServiceMeta{Hostname: "appdev", Mode: topology.PlanModeDev},
			hostname: "appdev",
			want:     topology.ModeDev,
		},
		{
			name:     "simple",
			meta:     &ServiceMeta{Hostname: "app", Mode: topology.PlanModeSimple},
			hostname: "app",
			want:     topology.ModeSimple,
		},
		{
			name:     "nil_meta",
			meta:     nil,
			hostname: "appdev",
			want:     "",
		},
		{
			name:     "unknown_hostname",
			meta:     &ServiceMeta{Hostname: "appdev", Mode: topology.PlanModeDev},
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
		Mode:             topology.PlanModeDev,
		DeployStrategy:   topology.StrategyPushGit,
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

// marshalEnvelopeForTest is the canonical JSON encoder used in tests. Go's
// encoding/json already sorts map keys alphabetically, so plain Marshal is
// deterministic when slice ordering is controlled — which buildServiceSnapshots
// does by sorting on Hostname.
func marshalEnvelopeForTest(env StateEnvelope) ([]byte, error) {
	return json.Marshal(env)
}

// TestAttemptInfo_PreservesFailureContext pins Phase 1 (C1) of the pipeline-
// repair plan: the persisted DeployAttempt and VerifyAttempt fields that
// describe failure context (Reason via Error/Summary, FailureClass, Setup,
// Strategy) reach the envelope projection. Without this, `action="status"`
// renders only "deploy failed" / "verify failed" with no actionable info,
// breaking compaction recovery.
func TestAttemptInfo_PreservesFailureContext(t *testing.T) {
	t.Parallel()

	ws := &WorkSession{
		PID:       42,
		Services:  []string{"apidev"},
		CreatedAt: "2026-04-26T09:00:00Z",
		Deploys: map[string][]DeployAttempt{
			"apidev": {{
				AttemptedAt:  "2026-04-26T10:00:00Z",
				Setup:        "dev",
				Strategy:     "push-dev",
				Error:        "build timeout after 15 minutes",
				FailureClass: topology.FailureClassBuild,
			}},
		},
		Verifies: map[string][]VerifyAttempt{
			"apidev": {{
				AttemptedAt:  "2026-04-26T10:05:00Z",
				Passed:       false,
				Summary:      "http_root: 502 Bad Gateway",
				FailureClass: topology.FailureClassVerify,
			}},
		},
	}

	summary := buildWorkSessionSummary(ws)
	if summary == nil {
		t.Fatal("buildWorkSessionSummary returned nil")
	}

	gotDeploy := summary.Deploys["apidev"][0]
	if gotDeploy.Reason != "build timeout after 15 minutes" {
		t.Errorf("deploy Reason: got %q, want %q", gotDeploy.Reason, "build timeout after 15 minutes")
	}
	if gotDeploy.FailureClass != topology.FailureClassBuild {
		t.Errorf("deploy FailureClass: got %q, want %q", gotDeploy.FailureClass, topology.FailureClassBuild)
	}
	if gotDeploy.Setup != "dev" {
		t.Errorf("deploy Setup: got %q, want %q", gotDeploy.Setup, "dev")
	}
	if gotDeploy.Strategy != "push-dev" {
		t.Errorf("deploy Strategy: got %q, want %q", gotDeploy.Strategy, "push-dev")
	}
	if gotDeploy.Success {
		t.Errorf("deploy Success: got true, want false")
	}

	gotVerify := summary.Verifies["apidev"][0]
	if gotVerify.Reason != "http_root: 502 Bad Gateway" {
		t.Errorf("verify Reason: got %q, want %q", gotVerify.Reason, "http_root: 502 Bad Gateway")
	}
	if gotVerify.FailureClass != topology.FailureClassVerify {
		t.Errorf("verify FailureClass: got %q, want %q", gotVerify.FailureClass, topology.FailureClassVerify)
	}
	if gotVerify.Summary != "http_root: 502 Bad Gateway" {
		t.Errorf("verify Summary: got %q, want %q", gotVerify.Summary, "http_root: 502 Bad Gateway")
	}
	if gotVerify.Success {
		t.Errorf("verify Success: got true, want false")
	}
}

// TestAttemptInfo_SuccessLeavesFailureFieldsEmpty pins the inverse: a
// successful attempt MUST NOT carry Reason/FailureClass — those are
// failure-only signals. Setup/Strategy persist; Summary persists for
// verifies (e.g. "healthy").
//
// The fixture deliberately seeds non-zero Error and FailureClass values
// on the SUCCESSFUL attempts (a stale carry-over from an earlier failed
// retry that later succeeded — possible if a caller mutates the same
// DeployAttempt struct in-place, or in test fixtures that copy across
// states). The projection MUST drop them. Without this stale-seed shape,
// the test would still pass even if the projection's `if !info.Success`
// guard were removed — defeating its purpose as a regression guard.
func TestAttemptInfo_SuccessLeavesFailureFieldsEmpty(t *testing.T) {
	t.Parallel()

	ws := &WorkSession{
		PID:       42,
		Services:  []string{"apidev"},
		CreatedAt: "2026-04-26T09:00:00Z",
		Deploys: map[string][]DeployAttempt{
			"apidev": {{
				AttemptedAt: "2026-04-26T10:00:00Z",
				SucceededAt: "2026-04-26T10:02:00Z",
				Setup:       "dev",
				Strategy:    "push-dev",
				// Stale failure fields from a previous failed retry.
				// Projection MUST drop them on Success=true.
				Error:        "stale: build timeout",
				FailureClass: topology.FailureClassBuild,
			}},
		},
		Verifies: map[string][]VerifyAttempt{
			"apidev": {{
				AttemptedAt: "2026-04-26T10:05:00Z",
				PassedAt:    "2026-04-26T10:05:30Z",
				Passed:      true,
				Summary:     "healthy",
				// Stale failure class from previous attempt.
				FailureClass: topology.FailureClassVerify,
			}},
		},
	}

	summary := buildWorkSessionSummary(ws)

	gotDeploy := summary.Deploys["apidev"][0]
	if !gotDeploy.Success {
		t.Errorf("deploy Success: got false, want true")
	}
	if gotDeploy.Reason != "" {
		t.Errorf("deploy Reason on success: got %q, want empty (projection MUST drop stale failure fields)", gotDeploy.Reason)
	}
	if gotDeploy.FailureClass != "" {
		t.Errorf("deploy FailureClass on success: got %q, want empty (projection MUST drop stale failure fields)", gotDeploy.FailureClass)
	}
	if gotDeploy.Setup != "dev" {
		t.Errorf("deploy Setup: got %q, want %q", gotDeploy.Setup, "dev")
	}

	gotVerify := summary.Verifies["apidev"][0]
	if !gotVerify.Success {
		t.Errorf("verify Success: got false, want true")
	}
	if gotVerify.Reason != "" {
		t.Errorf("verify Reason on success: got %q, want empty (projection MUST drop stale failure fields)", gotVerify.Reason)
	}
	if gotVerify.FailureClass != "" {
		t.Errorf("verify FailureClass on success: got %q, want empty (projection MUST drop stale failure fields)", gotVerify.FailureClass)
	}
	if gotVerify.Summary != "healthy" {
		t.Errorf("verify Summary on success: got %q, want %q", gotVerify.Summary, "healthy")
	}
}
