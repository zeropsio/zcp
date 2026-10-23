package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
		Hostname:                 "appdev",
		Mode:                     topology.PlanModeStandard,
		Stages:                   []topology.StageRef{{Hostname: "appstage"}},
		CloseDeployMode:          topology.CloseModeAuto,
		CloseDeployModeConfirmed: true,
		BootstrappedAt:           fixedTime.Format(time.RFC3339),
		BootstrapSession:         "sess-1",
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
	if appdev.CloseDeployMode != topology.CloseModeAuto {
		t.Errorf("appdev closeDeployMode = %q, want auto", appdev.CloseDeployMode)
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

	deployedVersion := &platform.ActiveAppVersionDigest{ID: "av-2", Source: "CLI", Built: true}
	tests := []struct {
		name         string
		meta         *ServiceMeta
		svcStatus    string
		svcActive    *platform.ActiveAppVersionDigest
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
			svcActive:    deployedVersion,
			wantDeployed: true,
		},
		{
			// The Beviro trial (2026-09-29): a Mate's recipe runtimes are
			// imported startWithoutCode, ACTIVE with a placeholder version;
			// adopting them must not read them as deployed, or develop skips
			// the first deploy and the agent treats empty containers as apps.
			name: "adopted + ACTIVE holding only the startWithoutCode placeholder = not deployed",
			meta: &ServiceMeta{
				Hostname:         "appdev",
				Mode:             topology.PlanModeStandard,
				Stages:           []topology.StageRef{{Hostname: "appstage"}},
				BootstrappedAt:   fixedTime.Format(time.RFC3339),
				BootstrapSession: "",
			},
			svcStatus:    "ACTIVE",
			svcActive:    &platform.ActiveAppVersionDigest{ID: "av-1", Source: platform.AppVersionSourceNone},
			wantDeployed: false,
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
				ActiveAppVersion: tt.svcActive,
			}
			mock := platform.NewMock().
				WithServicesDirect([]platform.ServiceStack{svc}).
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

// TestBuildServiceSnapshots_LocalOnly_AppendsSyntheticSnapshot pins that
// a PlanModeLocalOnly meta produces a project-keyed snapshot even when
// no live platform service shares its hostname. Without this, atoms
// with `modes: [..., local-only]` had no service to fire on for an
// unlinked local-only project (the local CWD has no Zerops-side
// representation).
func TestBuildServiceSnapshots_LocalOnly_AppendsSyntheticSnapshot(t *testing.T) {
	t.Parallel()
	metas := []*ServiceMeta{
		{
			Hostname:        "myproject",
			Mode:            topology.PlanModeLocalOnly,
			CloseDeployMode: topology.CloseModeUnset,
			GitPushState:    topology.GitPushUnconfigured,
			BootstrappedAt:  fixedTime.Format(time.RFC3339),
		},
	}
	// No platform service for "myproject" — local-only has no Zerops-side
	// runtime by definition. Synthetic snapshot fills the gap.
	services := []platform.ServiceStack{
		{
			ID: "s1", Name: "db",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"},
		},
	}
	snaps := buildServiceSnapshots(services, metas, nil, "")
	var found *ServiceSnapshot
	for i := range snaps {
		if snaps[i].Hostname == "myproject" {
			found = &snaps[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected synthetic snapshot for local-only meta; got %d snapshots: %+v", len(snaps), snaps)
	}
	if found.Mode != topology.ModeLocalOnly {
		t.Errorf("synthetic snapshot Mode = %q, want %q", found.Mode, topology.ModeLocalOnly)
	}
	if found.CloseDeployMode != topology.CloseModeUnset {
		t.Errorf("synthetic snapshot CloseDeployMode = %q, want unset", found.CloseDeployMode)
	}
	if found.GitPushState != topology.GitPushUnconfigured {
		t.Errorf("synthetic snapshot GitPushState = %q, want unconfigured", found.GitPushState)
	}
	if found.RuntimeClass != "" {
		t.Errorf("synthetic snapshot has RuntimeClass = %q; local-only has no linked runtime, want empty", found.RuntimeClass)
	}
}

// TestBuildServiceSnapshots_ContainerStandard_NoSynthetic — regression:
// non-local metas must NOT produce synthetic snapshots. Container
// standard pairs are platform-keyed; their snapshots come from live
// services.
func TestBuildServiceSnapshots_ContainerStandard_NoSynthetic(t *testing.T) {
	t.Parallel()
	metas := []*ServiceMeta{
		{
			Hostname:       "appdev",
			Mode:           topology.PlanModeStandard,
			Stages:         []topology.StageRef{{Hostname: "appstage"}},
			BootstrappedAt: fixedTime.Format(time.RFC3339),
		},
	}
	services := []platform.ServiceStack{
		{
			ID: "s1", Name: "appdev", Status: "ACTIVE",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
		},
		{
			ID: "s2", Name: "appstage", Status: "ACTIVE",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
		},
	}
	snaps := buildServiceSnapshots(services, metas, nil, "")
	if got := len(snaps); got != 2 {
		t.Errorf("snapshots = %d, want 2 (live appdev + live appstage); got: %+v", got, snaps)
	}
	for _, s := range snaps {
		if s.Hostname != "appdev" && s.Hostname != "appstage" {
			t.Errorf("unexpected synthetic snapshot for hostname %q", s.Hostname)
		}
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
		CloseDeployMode:  topology.CloseModeGitPush,
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
				Strategy:     "git-push",
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

	summary := buildWorkSessionSummary(t.TempDir(), ws)
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
	if gotDeploy.Strategy != "git-push" {
		t.Errorf("deploy Strategy: got %q, want %q", gotDeploy.Strategy, "git-push")
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

// TestAttemptInfo_CarriesSHAAndAppVersionID pins that a deploy-from-commit
// attempt's SHA/AppVersionID (docs/spec-workflows.md §4.9) reach the
// envelope projection — otherwise action="status" has no way to tell the
// agent which commit is running without a fresh zerops_deploy call.
func TestAttemptInfo_CarriesSHAAndAppVersionID(t *testing.T) {
	t.Parallel()

	ws := &WorkSession{
		PID:       42,
		Services:  []string{"apistage"},
		CreatedAt: "2026-04-26T09:00:00Z",
		Deploys: map[string][]DeployAttempt{
			"apistage": {{
				AttemptedAt:  "2026-04-26T10:00:00Z",
				SucceededAt:  "2026-04-26T10:02:00Z",
				Setup:        "stage",
				Strategy:     "zcli",
				SHA:          "abc123def456",
				AppVersionID: "av-99",
			}},
		},
	}

	summary := buildWorkSessionSummary(t.TempDir(), ws)
	if summary == nil {
		t.Fatal("buildWorkSessionSummary returned nil")
	}

	gotDeploy := summary.Deploys["apistage"][0]
	if gotDeploy.SHA != "abc123def456" {
		t.Errorf("deploy SHA: got %q, want %q", gotDeploy.SHA, "abc123def456")
	}
	if gotDeploy.AppVersionID != "av-99" {
		t.Errorf("deploy AppVersionID: got %q, want %q", gotDeploy.AppVersionID, "av-99")
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
				Strategy:    "git-push",
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

	summary := buildWorkSessionSummary(t.TempDir(), ws)

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

// TestTOPO1_SnapshotHealsEmptyDeployDims pins TOPO-1/WF-2/DELIV-2: the
// snapshot builder normalizes an empty on-disk GitPushState/BuildIntegration
// to their canonical zero values so the git-push-setup + build-integration
// atoms (matched via slices.Contains over [unconfigured,broken] / [none],
// which "" never satisfies) fire for local-stage metas written before
// NewServiceMeta stamped them. Pre-fix only CloseDeployMode was normalized.
func TestTOPO1_SnapshotHealsEmptyDeployDims(t *testing.T) {
	// Legacy on-disk local-stage meta with empty deploy dimensions.
	meta := &ServiceMeta{
		Hostname:       "myproject",
		Stages:         []topology.StageRef{{Hostname: "appstage"}},
		Mode:           topology.PlanModeLocalStage,
		BootstrappedAt: "2026-01-01T00:00:00Z",
	}
	snap := buildOneSnapshot(platform.ServiceStack{Name: "appstage", Status: "ACTIVE"}, meta, nil)
	if snap.GitPushState != topology.GitPushUnconfigured {
		t.Errorf("empty GitPushState should heal to %q; got %q", topology.GitPushUnconfigured, snap.GitPushState)
	}
	if snap.BuildIntegration != topology.BuildIntegrationNone {
		t.Errorf("empty BuildIntegration should heal to %q; got %q", topology.BuildIntegrationNone, snap.BuildIntegration)
	}
	if snap.CloseDeployMode != topology.CloseModeUnset {
		t.Errorf("empty CloseDeployMode should heal to %q; got %q", topology.CloseModeUnset, snap.CloseDeployMode)
	}
}

// TestNewServiceMeta_StampsThreeDims pins the canonical constructor: no
// fresh-meta writer leaves a deploy dimension empty (TOPO-1).
func TestNewServiceMeta_StampsThreeDims(t *testing.T) {
	m := NewServiceMeta("app", topology.PlanModeLocalStage)
	if m.CloseDeployMode != topology.CloseModeUnset {
		t.Errorf("CloseDeployMode: got %q want %q", m.CloseDeployMode, topology.CloseModeUnset)
	}
	if m.GitPushState != topology.GitPushUnconfigured {
		t.Errorf("GitPushState: got %q want %q", m.GitPushState, topology.GitPushUnconfigured)
	}
	if m.BuildIntegration != topology.BuildIntegrationNone {
		t.Errorf("BuildIntegration: got %q want %q", m.BuildIntegration, topology.BuildIntegrationNone)
	}
}

// TestDeriveDeployed_ProvisionedFromGit pins B4/F11 signal 4: a recipe-
// buildFromGit runtime (ProvisionedFromGit) reads deployed=true once it reaches
// ACTIVE — the platform deployed curated code at import. It stays false before
// ACTIVE, and a classic meta (no flag) is unaffected.
func TestDeriveDeployed_ProvisionedFromGit(t *testing.T) {
	t.Parallel()
	gitMeta := &ServiceMeta{Hostname: "appdev", ProvisionedFromGit: true, BootstrapSession: "s", BootstrappedAt: "2026-06-08"}
	classicMeta := &ServiceMeta{Hostname: "appdev", BootstrapSession: "s", BootstrappedAt: "2026-06-08"}
	cases := []struct {
		name   string
		meta   *ServiceMeta
		status string
		want   bool
	}{
		{"git + ACTIVE → deployed", gitMeta, StatusActive, true},
		{"git + READY_TO_DEPLOY → not yet", gitMeta, "READY_TO_DEPLOY", false},
		{"classic + ACTIVE → not deployed (no flag)", classicMeta, StatusActive, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			live := &platform.ServiceStack{Name: "appdev", Status: tc.status}
			if got := DeriveDeployed("appdev", live, tc.meta, nil); got != tc.want {
				t.Errorf("DeriveDeployed = %v, want %v", got, tc.want)
			}
		})
	}
}

// The pair's first-deploy stamp is shared by its dev half and every stage: a
// dev deploy marked three never-deployed stages deployed while the platform
// still listed them READY_TO_DEPLOY. A service the platform lists
// READY_TO_DEPLOY without deployed code has had no deploy land, whatever the
// pair's stamp says — unless this session deployed it (the list lags).
func TestDeriveDeployed_AStageThePlatformStillListsReadyToDeployIsNotDeployed(t *testing.T) {
	t.Parallel()
	meta := &ServiceMeta{
		Hostname: "petclinicdev", Mode: topology.ModeStandard, BootstrapSession: "s", BootstrappedAt: "2026-10-16",
		Stages:          []topology.StageRef{{Hostname: "vetsstage", SetupName: "vets"}, {Hostname: "gatewaystage", SetupName: "gateway"}},
		FirstDeployedAt: "2026-10-16T07:28:20Z",
	}
	ws := &WorkSession{Deploys: map[string][]DeployAttempt{"gatewaystage": {{SucceededAt: "2026-10-16T07:40:00Z"}}}}
	cases := []struct {
		host, status string
		want         bool
	}{
		{"petclinicdev", StatusActive, true},
		{"vetsstage", "READY_TO_DEPLOY", false},
		{"gatewaystage", "READY_TO_DEPLOY", true},
	}
	for _, tc := range cases {
		live := &platform.ServiceStack{Name: tc.host, Status: tc.status}
		if got := DeriveDeployed(tc.host, live, meta, ws); got != tc.want {
			t.Errorf("DeriveDeployed(%s, %s) = %v, want %v", tc.host, tc.status, got, tc.want)
		}
	}
}

// TestComputeEnvelope_ReadsDirectList pins that the envelope reads services
// off the DIRECT list: it is the only read that carries the active version's
// source, which tells an empty startWithoutCode runtime from a deployed one,
// and it is lag-free — a service the ES index has not caught up with yet is
// still in the envelope.
func TestComputeEnvelope_ReadsDirectList(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "demo"}).
		WithServices(nil). // ES list not indexed yet
		WithServicesDirect([]platform.ServiceStack{{
			ID: "s1", Name: "appdev", Status: "ACTIVE",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"},
		}})
	env, err := ComputeEnvelope(context.Background(), mock, t.TempDir(), "p1", runtime.Info{}, fixedTime)
	if err != nil {
		t.Fatalf("ComputeEnvelope: %v", err)
	}
	if len(env.Services) != 1 || env.Services[0].Hostname != "appdev" {
		t.Fatalf("envelope must read the direct list, got %+v", env.Services)
	}
}

// TestComputeEnvelope_UntrackedRuntimeDeployedFromPlatform pins deployed for a
// runtime zcp does not track yet: with no record of its own, the platform's
// active version is the whole answer, so an adoptable service the recipe
// imported without code reads deployed=false before anyone adopts it.
func TestComputeEnvelope_UntrackedRuntimeDeployedFromPlatform(t *testing.T) {
	t.Parallel()
	svc := func(name, typ string, active *platform.ActiveAppVersionDigest) platform.ServiceStack {
		return platform.ServiceStack{ID: name, Name: name, Status: "ACTIVE",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: typ, ServiceStackTypeCategoryName: "USER"},
			ActiveAppVersion:     active}
	}
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "demo"}).
		WithServicesDirect([]platform.ServiceStack{
			svc("appdev", "alpine/nodejs@24", &platform.ActiveAppVersionDigest{ID: "av-1", Source: platform.AppVersionSourceNone}),
			svc("api", "alpine/nodejs@24", &platform.ActiveAppVersionDigest{ID: "av-2", Source: "CLI", Built: true}),
			svc("db", "postgresql:single@17", nil),
		})
	env, err := ComputeEnvelope(context.Background(), mock, t.TempDir(), "p1", runtime.Info{}, fixedTime)
	if err != nil {
		t.Fatalf("ComputeEnvelope: %v", err)
	}
	want := map[string]bool{"appdev": false, "api": true, "db": false}
	for _, s := range env.Services {
		if s.Bootstrapped {
			t.Fatalf("%s: no meta, must not be bootstrapped", s.Hostname)
		}
		if s.Deployed != want[s.Hostname] {
			t.Errorf("%s: Deployed = %v, want %v", s.Hostname, s.Deployed, want[s.Hostname])
		}
	}
}

// A Swarm pool and a Zerops Box come through the envelope as their own
// classes and never as adoptable runtimes (spec-workflows.md §8 PS-1, PS-2).
func TestComputeEnvelope_PoolClass(t *testing.T) {
	t.Parallel()

	pool := platform.ServiceStack{
		ID: "pool-id", Name: "swarmpool", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeVersionName:  "swarm@1",
			ServiceStackTypeCategoryName: "USER",
		},
	}
	box := platform.ServiceStack{
		ID: "box-id", Name: "sandbox", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeVersionName:  "ubuntu/zbox@1",
			ServiceStackTypeCategoryName: "STANDARD",
		},
	}
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{pool, box}).
		WithProject(&platform.Project{ID: "p1", Name: "demo"})

	env, err := ComputeEnvelope(context.Background(), mock, t.TempDir(), "p1",
		runtime.Info{InContainer: true, ServiceName: "zcpdev"}, fixedTime)
	if err != nil {
		t.Fatalf("ComputeEnvelope: %v", err)
	}

	want := map[string]topology.RuntimeClass{"swarmpool": "pool", "sandbox": "sandbox"}
	if len(env.Services) != len(want) {
		t.Fatalf("services = %+v, want exactly %d", env.Services, len(want))
	}
	for _, svc := range env.Services {
		if svc.RuntimeClass != want[svc.Hostname] {
			t.Errorf("%s RuntimeClass = %q, want %q", svc.Hostname, svc.RuntimeClass, want[svc.Hostname])
		}
		if svc.Bootstrapped || svc.Mode != "" {
			t.Errorf("%s Bootstrapped=%t Mode=%q, want an unbootstrapped, modeless service", svc.Hostname, svc.Bootstrapped, svc.Mode)
		}
	}
	// Neither is an adoptable runtime: a project holding only a pool and a
	// Box is an empty project for the bootstrap/adopt routing.
	if env.IdleScenario != IdleEmpty {
		t.Errorf("IdleScenario = %q, want %q (pool and Box are not adoptable runtimes)", env.IdleScenario, IdleEmpty)
	}
}

// poolService is a Swarm pool the way the platform lists it.
func poolService(hostname string) platform.ServiceStack {
	return platform.ServiceStack{
		ID: "pool-id", Name: hostname, Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeVersionName:  "swarm@1",
			ServiceStackTypeCategoryName: "USER",
		},
	}
}

// writeLeaseRecordFile writes a lease record the way zcp stores it
// (docs/spec-workflows.md §8 DP-2: .zcp/state/leases/<alias>.json).
func writeLeaseRecordFile(t *testing.T, stateDir, alias, body string) {
	t.Helper()
	dir := filepath.Join(stateDir, "leases")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, alias+".json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestComputeEnvelope_LeasesFromReader(t *testing.T) {
	t.Parallel()

	held := LeaseSnapshot{Alias: "devpool-3", Pool: "devpool", Branch: "feat-x", ContainerHostname: "node-id-3.runtime.devpool.zerops", State: "held"}
	lost := LeaseSnapshot{Alias: "devpool-12", Pool: "devpool", Branch: "feat-y", ContainerHostname: "node-id-12.runtime.devpool.zerops", State: "lost"}
	other := LeaseSnapshot{Alias: "apool-1", Pool: "apool", ContainerHostname: "node-id-1.runtime.apool.zerops", State: "unknown-holder"}

	tests := []struct {
		name       string
		services   []platform.ServiceStack
		reader     LeaseReader
		wantCalled bool
		want       []LeaseSnapshot
		wantRead   bool
	}{
		{"reader snapshots reach the envelope, sorted by pool then container number", []platform.ServiceStack{poolService("devpool")},
			func(context.Context) ([]LeaseSnapshot, error) { return []LeaseSnapshot{lost, other, held}, nil },
			true, []LeaseSnapshot{other, held, lost}, true},
		{"project without a pool or a record never asks the reader", nil,
			func(context.Context) ([]LeaseSnapshot, error) { return []LeaseSnapshot{held}, nil },
			false, nil, false},
		{"no reader injected leaves the leases empty", []platform.ServiceStack{poolService("devpool")}, nil, false, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			called := false
			var opts []EnvelopeOption
			if tt.reader != nil {
				reader := tt.reader
				opts = append(opts, WithLeaseReader(func(ctx context.Context) ([]LeaseSnapshot, error) {
					called = true
					return reader(ctx)
				}))
			}
			mock := platform.NewMock().WithServices(tt.services).WithProject(&platform.Project{ID: "p1", Name: "demo"})
			env, err := ComputeEnvelope(context.Background(), mock, t.TempDir(), "p1", runtime.Info{}, fixedTime, opts...)
			if err != nil {
				t.Fatalf("ComputeEnvelope: %v", err)
			}
			if called != tt.wantCalled {
				t.Errorf("reader called = %v, want %v", called, tt.wantCalled)
			}
			if (env.Leases != nil) != tt.wantRead {
				t.Errorf("leases read = %v, want %v", env.Leases != nil, tt.wantRead)
			}
			if fmt.Sprintf("%+v", env.LeaseList()) != fmt.Sprintf("%+v", tt.want) {
				t.Errorf("Leases = %+v, want %+v", env.LeaseList(), tt.want)
			}
		})
	}
}

func TestComputeEnvelope_OrchestratorUnreachable_LeasesUnverified(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeLeaseRecordFile(t, dir, "devpool-3", `{"poolHostname":"devpool","poolServiceId":"pool-id","containerId":"c3","containerNumber":3,`+
		`"containerHostname":"node-id-3.runtime.devpool.zerops","lease":"SECRET-LEASE-VALUE","branch":"feat-x","acquiredAt":"2026-10-23T10:00:00Z"}`)
	// A record that names no lease (alias does not match its file) is skipped.
	writeLeaseRecordFile(t, dir, "bogus-9", `{"poolHostname":"other","containerNumber":1,"lease":"SECRET-LEASE-VALUE"}`)

	reader := WithLeaseReader(func(context.Context) ([]LeaseSnapshot, error) {
		return nil, errors.New("orchestrator unreachable")
	})
	// The orchestrator is down; the project has no pool listed either — the
	// record alone is enough to ask, and to report.
	mock := platform.NewMock().WithProject(&platform.Project{ID: "p1", Name: "demo"})
	env, err := ComputeEnvelope(context.Background(), mock, dir, "p1", runtime.Info{}, fixedTime, reader)
	if err != nil {
		t.Fatalf("an unreachable orchestrator must never fail the envelope: %v", err)
	}
	want := []LeaseSnapshot{{
		Alias: "devpool-3", Pool: "devpool", Branch: "feat-x",
		ContainerHostname: "node-id-3.runtime.devpool.zerops", State: "held", Unverified: true,
	}}
	if fmt.Sprintf("%+v", env.LeaseList()) != fmt.Sprintf("%+v", want) {
		t.Errorf("Leases = %+v, want %+v", env.LeaseList(), want)
	}
	if strings.Contains(fmt.Sprintf("%+v", env), "SECRET-LEASE-VALUE") {
		t.Error("the lease string reached the envelope")
	}
}

// TestComputeEnvelope_PoolDevHalf_CarriesStages pins the envelope of a pair
// keyed by its dev pool: the pool's snapshot keys the pair — it carries the
// pair's stages (stageHostname today, stages[] from the field order's step 3)
// and HQ record, and is marked a pool dev half for pure readers (off the
// wire) — while it stays a pool for atom matching: no mode, so no dev-runtime
// guidance lands on it. The stage keeps its stage mode.
func TestComputeEnvelope_PoolDevHalf_CarriesStages(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pair, err := devKeyedPair().keyedOnPool(appPoolMove())
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteServiceMeta(dir, pair); err != nil {
		t.Fatal(err)
	}
	stage := platform.ServiceStack{
		ID: "s2", Name: "appstage", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"},
	}
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{poolService("apppool"), stage}).
		WithProject(&platform.Project{ID: "p1", Name: "demo"})
	env, err := ComputeEnvelope(context.Background(), mock, dir, "p1", runtime.Info{InContainer: true, ServiceName: "zcp"}, fixedTime)
	if err != nil {
		t.Fatalf("ComputeEnvelope: %v", err)
	}
	if len(env.Services) != 2 {
		t.Fatalf("services = %+v, want apppool and appstage", env.Services)
	}
	pool, st := env.Services[0], env.Services[1]
	if pool.Hostname != "apppool" || pool.RuntimeClass != topology.RuntimePool {
		t.Fatalf("services[0] = %+v, want the pool", pool)
	}
	if !pool.DevIsPool || !pool.Bootstrapped || pool.Mode != "" {
		t.Errorf("pool snapshot DevIsPool=%v Bootstrapped=%v Mode=%q, want a marked, tracked, modeless pool", pool.DevIsPool, pool.Bootstrapped, pool.Mode)
	}
	if pool.StageHostname != "appstage" || pool.StageSetupName != "prod" ||
		!reflect.DeepEqual(pool.Stages, []topology.StageRef{{Hostname: "appstage", SetupName: "prod"}}) {
		t.Errorf("pool snapshot stages = %q/%q %+v, want appstage/prod", pool.StageHostname, pool.StageSetupName, pool.Stages)
	}
	if pool.HQRepo != "appdev" || pool.HQChange != 7 || pool.HQBranch != "feat-x" {
		t.Errorf("pool snapshot HQ = %q #%d %q, want the pair's HQ record", pool.HQRepo, pool.HQChange, pool.HQBranch)
	}
	if pool.SetupName != "pool" || pool.GitPushState != topology.GitPushConfigured {
		t.Errorf("pool snapshot setup=%q gitPush=%q, want the pool setup and the pair's git-push state", pool.SetupName, pool.GitPushState)
	}
	if st.Mode != topology.ModeStage || st.DevIsPool || st.StageHostname != "" || st.SetupName != "prod" {
		t.Errorf("stage snapshot = %+v, want the stage half, unmarked", st)
	}

	wire, err := json.Marshal(env.WithParts(nil))
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Services []map[string]json.RawMessage `json:"services"`
	}
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	if got := string(decoded.Services[0]["stages"]); got != `[{"hostname":"appstage","setup":"prod"}]` {
		t.Errorf("the pool's wire stages = %s, want [{appstage prod}]", got)
	}
	if _, ok := decoded.Services[1]["stages"]; ok {
		t.Error("the stage's snapshot carries stages[]")
	}
	if strings.Contains(string(wire), "devIsPool") || strings.Contains(string(wire), "DevIsPool") {
		t.Errorf("the marker rode the wire: %s", wire)
	}
}
