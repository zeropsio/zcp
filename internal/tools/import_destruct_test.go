// Tests for: import.go diagnose-before-destruct gate (plan v4 §3.2).
// Pinned by these tests:
//   - override=true on a service with failed appVersion history → first call
//     refused with ErrDiagnosisRequired + structured WouldDestroy
//   - override=true on a service with NO failed history → passes (gate
//     bypassed, agent's standard override warning applies)
//   - matching confirmDestructive on second call → proceeds to ImportServices
//   - partial / mismatched confirmDestructive → still refused
//   - non-override import never gates regardless of failed history
package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

func TestImport_OverrideOnFailedRequiresAck(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "s1", Name: "api", Status: platform.ServiceStatusReadyToDeploy},
		}).
		WithServiceEnv("s1", []platform.ServiceEnvVar{
			{ID: "e1", Key: "DATABASE_URL", Content: "postgresql://..."},
			{ID: "e2", Key: "APP_KEY", Content: "secret"},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-1", ServiceStackID: "s1", Status: platform.BuildStatusBuildFailed, Created: "2026-05-05T10:00:00Z"},
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterImport(srv, mock, "proj-1", testEngine(t), "", nil, runtime.Info{})

	yaml := "services:\n  - hostname: api\n    type: nodejs@22\n"
	result := callTool(t, srv, "zerops_import", map[string]any{
		"content":  yaml,
		"override": true,
	})
	if !result.IsError {
		t.Fatalf("expected IsError on override of failed service without ack")
	}

	var wire ErrorWire
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &wire); err != nil {
		t.Fatalf("parse error wire: %v", err)
	}
	if wire.Code != platform.ErrDiagnosisRequired {
		t.Errorf("Code = %q, want %q", wire.Code, platform.ErrDiagnosisRequired)
	}
	if wire.WouldDestroy == nil {
		t.Fatalf("WouldDestroy missing on first-call refusal")
	}
	if wire.WouldDestroy.Operation != "import-override" {
		t.Errorf("Operation = %q", wire.WouldDestroy.Operation)
	}
	if len(wire.WouldDestroy.Targets) != 1 || wire.WouldDestroy.Targets[0] != "api" {
		t.Errorf("Targets = %v", wire.WouldDestroy.Targets)
	}
	// envVars population was a Phase-7 fix: pre-fix the gate built an
	// empty envVarsByService map and ALWAYS reported wouldDestroy.envVars=[]
	// even when override would erase real keys.
	if got := wire.WouldDestroy.Loss.EnvVars; len(got) != 2 {
		t.Fatalf("WouldDestroy.Loss.EnvVars = %v, want 2 keys", got)
	}
	gotKeys := map[string]bool{}
	for _, k := range wire.WouldDestroy.Loss.EnvVars {
		gotKeys[k] = true
	}
	for _, want := range []string{"DATABASE_URL", "APP_KEY"} {
		if !gotKeys[want] {
			t.Errorf("WouldDestroy.Loss.EnvVars missing %q (got %v)", want, wire.WouldDestroy.Loss.EnvVars)
		}
	}

	// R6-P3: the gate carries its OWN per-target verdict — class + the
	// startWithoutCode-needed flag (READY_TO_DEPLOY lacks an ACTIVE version).
	if len(wire.WouldDestroy.Diagnoses) != 1 {
		t.Fatalf("Diagnoses = %v, want 1", wire.WouldDestroy.Diagnoses)
	}
	d := wire.WouldDestroy.Diagnoses[0]
	if d.Hostname != "api" || d.FailureClass != "build" || !d.NeedsStartWithoutCode {
		t.Errorf("Diagnosis = %+v, want api/build/needsStartWithoutCode", d)
	}

	// R2 (docs/spec-workflows.md §8 "Recovery classification"): a
	// failed-build target carries NO ready-made re-import retry — only a
	// fresh-misconfigured target does. The gate's corrective is read-first
	// (Next=zerops_events) then a plain zerops_deploy (Then), never a
	// re-import.
	if wire.WouldDestroy.Retry != nil {
		t.Errorf("Retry = %+v, want nil — no re-import retry on a failed-build target", wire.WouldDestroy.Retry)
	}
	if wire.WouldDestroy.Next == nil || wire.WouldDestroy.Next.Tool != "zerops_events" {
		t.Fatalf("Next = %+v, want zerops_events", wire.WouldDestroy.Next)
	}
	if !strings.Contains(wire.WouldDestroy.Then, "zerops_deploy") {
		t.Errorf("Then = %q, want it to name zerops_deploy as the non-gated corrective", wire.WouldDestroy.Then)
	}
}

// TestImport_OverrideOnPriorAttemptWithoutFailedPhaseRequiresAck pins the Wave-1
// gate-bypass fix: a READY_TO_DEPLOY service whose appVersion is in a
// non-failure-phase state (WAITING_TO_BUILD — the recover-failed 0s-build case,
// where the build process FAILED but the appVersion status has no
// FailurePhaseFromStatus mapping) still has prior deploy/build history + code
// worth preserving. Override on it MUST require confirmDestructive even though
// LatestFailedAppVersionContext returns nil. Before the fix the gate keyed only
// on a classified failure, so a 0s-build-fail silently bypassed the gate and
// the override wiped the buildFromGit source under diagnosis.
func TestImport_OverrideOnPriorAttemptWithoutFailedPhaseRequiresAck(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "s1", Name: "api", Status: platform.ServiceStatusReadyToDeploy},
		}).
		WithServiceEnv("s1", []platform.ServiceEnvVar{
			{ID: "e1", Key: "DATABASE_URL", Content: "postgresql://..."},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			// WAITING_TO_BUILD: a real prior attempt (Source=GIT_PUSH, so not a
			// startWithoutCode stamp) but NOT a recognized failure phase.
			{ID: "av-q", ServiceStackID: "s1", Status: "WAITING_TO_BUILD", Source: "GIT_PUSH", Created: "2026-05-18T14:00:00Z"},
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterImport(srv, mock, "proj-1", testEngine(t), "", nil, runtime.Info{})

	yaml := "services:\n  - hostname: api\n    type: nodejs@22\n"
	result := callTool(t, srv, "zerops_import", map[string]any{
		"content":  yaml,
		"override": true,
	})
	if !result.IsError {
		t.Fatalf("expected gate to require ack on override of a service with prior deploy history (no classified failure)")
	}
	var wire ErrorWire
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &wire); err != nil {
		t.Fatalf("parse error wire: %v", err)
	}
	if wire.Code != platform.ErrDiagnosisRequired {
		t.Errorf("Code = %q, want %q", wire.Code, platform.ErrDiagnosisRequired)
	}
	if wire.WouldDestroy == nil || len(wire.WouldDestroy.Targets) != 1 || wire.WouldDestroy.Targets[0] != "api" {
		t.Errorf("WouldDestroy targets = %+v, want [api]", wire.WouldDestroy)
	}
}

// TestImport_RecoveryHint_NoFacilityArg pins that the recovery hint on
// a diagnose-before-destruct refusal does not carry a "facility" arg.
// LogsInput has no Facility field; the MCP layer rejects unknown args
// and the agent's recovery call would fail before the gate could even
// help. Pre-fix the hint included `"facility": "application"`.
func TestImport_RecoveryHint_NoFacilityArg(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "s1", Name: "api", Status: platform.ServiceStatusReadyToDeploy},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-1", ServiceStackID: "s1", Status: platform.BuildStatusBuildFailed, Created: "2026-05-05T10:00:00Z"},
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterImport(srv, mock, "proj-1", testEngine(t), "", nil, runtime.Info{})

	yaml := "services:\n  - hostname: api\n    type: nodejs@22\n"
	result := callTool(t, srv, "zerops_import", map[string]any{
		"content":  yaml,
		"override": true,
	})
	if !result.IsError {
		t.Fatalf("expected IsError")
	}
	var wire ErrorWire
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &wire); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if wire.Recovery == nil {
		t.Fatalf("Recovery missing")
	}
	if _, has := wire.Recovery.Args["facility"]; has {
		t.Errorf("Recovery.Args includes forbidden 'facility' key (LogsInput has no Facility field): %v", wire.Recovery.Args)
	}
}

// TestGateOverrideOnFailedHistory_PopulatesEnvVarLoss pins the multi-
// service path: env vars on every failed target aggregate (dedup-by-key)
// into wouldDestroy.envVars. Previously the gate built but never
// populated envVarsByService — the Loss surface always reported zero env
// vars regardless of how much state would actually disappear.
func TestGateOverrideOnFailedHistory_PopulatesEnvVarLoss(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "s1", Name: "api", Status: platform.ServiceStatusReadyToDeploy},
			{ID: "s2", Name: "worker", Status: platform.ServiceStatusReadyToDeploy},
		}).
		WithServiceEnv("s1", []platform.ServiceEnvVar{
			{ID: "e1", Key: "DATABASE_URL", Content: "postgresql://..."},
			{ID: "e2", Key: "SHARED", Content: "from-api"},
		}).
		WithServiceEnv("s2", []platform.ServiceEnvVar{
			{ID: "e3", Key: "QUEUE_URL", Content: "nats://..."},
			{ID: "e4", Key: "SHARED", Content: "from-worker"},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-1", ServiceStackID: "s1", Status: platform.BuildStatusBuildFailed, Created: "2026-05-05T10:00:00Z"},
			{ID: "av-2", ServiceStackID: "s2", Status: platform.BuildStatusDeployFailed, Created: "2026-05-05T11:00:00Z"},
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterImport(srv, mock, "proj-1", testEngine(t), "", nil, runtime.Info{})

	yaml := "services:\n  - hostname: api\n    type: nodejs@22\n  - hostname: worker\n    type: nodejs@22\n"
	result := callTool(t, srv, "zerops_import", map[string]any{
		"content":  yaml,
		"override": true,
	})
	if !result.IsError {
		t.Fatalf("expected IsError, got success: %s", getTextContent(t, result))
	}
	var wire ErrorWire
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &wire); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if wire.WouldDestroy == nil {
		t.Fatalf("WouldDestroy missing")
	}
	got := map[string]bool{}
	for _, k := range wire.WouldDestroy.Loss.EnvVars {
		got[k] = true
	}
	want := []string{"DATABASE_URL", "SHARED", "QUEUE_URL"}
	if len(got) != len(want) {
		t.Errorf("Loss.EnvVars deduped count = %d, want %d (got %v)", len(got), len(want), wire.WouldDestroy.Loss.EnvVars)
	}
	for _, k := range want {
		if !got[k] {
			t.Errorf("Loss.EnvVars missing %q (got %v)", k, wire.WouldDestroy.Loss.EnvVars)
		}
	}
}

// TestGateOverrideOnFailedHistory_SuggestionIncludesRetryShape pins that
// the first-call rejection's suggestion text includes a copy-pasteable
// JSON snippet of the next zerops_import call (operation +
// acknowledgedTargets matching wouldDestroy). Pre-fix the agent had to
// hand-construct the ack payload from the wouldDestroy shape.
//
// R2 (docs/spec-workflows.md §8 "Recovery classification") restricts the
// ready-made re-import retry to fresh-misconfigured targets — a
// never-deployed READY_TO_DEPLOY service, no appVersion history — so this
// fixture carries none. A failed-build target's suggestion text is pinned
// separately by TestImport_OverrideOnFailedBuild_NoRetrySuggestion_NextIsDeploy.
func TestGateOverrideOnFailedHistory_SuggestionIncludesRetryShape(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "s1", Name: "api", Status: platform.ServiceStatusReadyToDeploy},
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterImport(srv, mock, "proj-1", testEngine(t), "", nil, runtime.Info{})

	yaml := "services:\n  - hostname: api\n    type: nodejs@22\n"
	result := callTool(t, srv, "zerops_import", map[string]any{
		"content":  yaml,
		"override": true,
	})
	if !result.IsError {
		t.Fatalf("expected IsError")
	}
	var wire ErrorWire
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &wire); err != nil {
		t.Fatalf("parse: %v", err)
	}
	wants := []string{
		"zerops_import",
		`"operation":"import-override"`,
		`"acknowledgedTargets":["api"]`,
	}
	for _, want := range wants {
		if !strings.Contains(wire.Suggestion, want) {
			t.Errorf("Suggestion missing %q; full suggestion:\n%s", want, wire.Suggestion)
		}
	}
}

// TestImport_OverrideOnFailedBuild_NoRetrySuggestion_NextIsDeploy pins R2
// (docs/spec-workflows.md §8 "Recovery classification"): the ready-made
// zerops_import override=true retry is emitted ONLY for fresh-misconfigured
// targets. A failed-build target's wouldDestroy payload carries no retryCall
// at all — Next points at zerops_events and Then names zerops_deploy as the
// non-gated corrective (the prior appVersion keeps serving).
func TestImport_OverrideOnFailedBuild_NoRetrySuggestion_NextIsDeploy(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "s1", Name: "api", Status: platform.ServiceStatusReadyToDeploy},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-1", ServiceStackID: "s1", Status: platform.BuildStatusBuildFailed, Created: "2026-05-05T10:00:00Z"},
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterImport(srv, mock, "proj-1", testEngine(t), "", nil, runtime.Info{})

	yaml := "services:\n  - hostname: api\n    type: nodejs@22\n"
	result := callTool(t, srv, "zerops_import", map[string]any{
		"content":  yaml,
		"override": true,
	})
	if !result.IsError {
		t.Fatalf("expected IsError on override of a failed-build service without ack")
	}
	var wire ErrorWire
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &wire); err != nil {
		t.Fatalf("parse error wire: %v", err)
	}
	if wire.Code != platform.ErrDiagnosisRequired {
		t.Errorf("Code = %q, want %q", wire.Code, platform.ErrDiagnosisRequired)
	}
	if wire.WouldDestroy == nil {
		t.Fatalf("WouldDestroy missing")
	}
	if wire.WouldDestroy.Retry != nil {
		t.Errorf("Retry = %+v, want nil — no ready-made re-import retry on a failed-build target", wire.WouldDestroy.Retry)
	}
	if wire.WouldDestroy.Next == nil || wire.WouldDestroy.Next.Tool != "zerops_events" {
		t.Fatalf("Next = %+v, want zerops_events", wire.WouldDestroy.Next)
	}
	if !strings.Contains(wire.WouldDestroy.Then, "zerops_deploy") {
		t.Errorf("Then = %q, want it to name zerops_deploy as the non-gated corrective", wire.WouldDestroy.Then)
	}
}

// TestImport_OverrideOnFreshMisconfigured_CarriesRetry pins R2: a
// never-deployed READY_TO_DEPLOY target (no appVersion history at all) is
// the ONLY shape whose gate carries the ready-made override retry, with a
// startWithoutCode:true patch hint.
func TestImport_OverrideOnFreshMisconfigured_CarriesRetry(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "s1", Name: "api", Status: platform.ServiceStatusReadyToDeploy},
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterImport(srv, mock, "proj-1", testEngine(t), "", nil, runtime.Info{})

	yaml := "services:\n  - hostname: api\n    type: nodejs@22\n"
	result := callTool(t, srv, "zerops_import", map[string]any{
		"content":  yaml,
		"override": true,
	})
	if !result.IsError {
		t.Fatalf("expected IsError — fresh-misconfigured is gated too (R1: Shape != healthy)")
	}
	var wire ErrorWire
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &wire); err != nil {
		t.Fatalf("parse error wire: %v", err)
	}
	if wire.Code != platform.ErrDiagnosisRequired {
		t.Errorf("Code = %q, want %q", wire.Code, platform.ErrDiagnosisRequired)
	}
	if wire.WouldDestroy == nil {
		t.Fatalf("WouldDestroy missing")
	}
	rc := wire.WouldDestroy.Retry
	if rc == nil || rc.Tool != "zerops_import" || rc.Args["override"] != true {
		t.Fatalf("Retry = %+v, want zerops_import override=true", rc)
	}
	hintFound := false
	for _, h := range rc.PatchHints {
		if strings.Contains(h, "startWithoutCode") && strings.Contains(h, "api") {
			hintFound = true
		}
	}
	if !hintFound {
		t.Errorf("retryCall.patchHints missing startWithoutCode hint for api: %v", rc.PatchHints)
	}
}

// TestImport_OverrideOnFailedInit_NoContainer_ThenNamesAppVersionRedeploy
// pins R2's artifact-redeploy amendment (docs/spec-workflows.md §8 R2,
// live-verified 2026-09-14): a never-activated buildFromGit service —
// failed-init (DEPLOY_FAILED), no container — carries NO ready-made
// re-import retry (the built artifact would be destroyed for nothing); Then
// names the in-place `zerops_deploy appVersion=latest` corrective instead.
func TestImport_OverrideOnFailedInit_NoContainer_ThenNamesAppVersionRedeploy(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "s1", Name: "api", Status: platform.ServiceStatusReadyToDeploy},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-2", ServiceStackID: "s1", Status: platform.BuildStatusDeployFailed, Source: "GIT", Created: "2026-09-14T10:00:00Z"},
		}).
		WithServiceAppVersions("s1", []platform.AppVersionEvent{
			{ID: "av-2", ServiceStackID: "s1", Status: platform.BuildStatusDeployFailed, Source: "GIT", Sequence: 2},
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterImport(srv, mock, "proj-1", testEngine(t), "", nil, runtime.Info{})

	yaml := "services:\n  - hostname: api\n    type: nodejs@22\n"
	result := callTool(t, srv, "zerops_import", map[string]any{
		"content":  yaml,
		"override": true,
	})
	if !result.IsError {
		t.Fatalf("expected IsError on override of a never-activated failed-init service without ack")
	}
	var wire ErrorWire
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &wire); err != nil {
		t.Fatalf("parse error wire: %v", err)
	}
	if wire.WouldDestroy == nil {
		t.Fatalf("WouldDestroy missing")
	}
	if wire.WouldDestroy.Retry != nil {
		t.Errorf("Retry = %+v, want nil — the built artifact must not be discarded via re-import", wire.WouldDestroy.Retry)
	}
	if !strings.Contains(wire.WouldDestroy.Then, "appVersion=latest") {
		t.Errorf("Then = %q, want it to name the appVersion=latest in-place redeploy", wire.WouldDestroy.Then)
	}
}

// TestImport_OverrideOnFailedBuild_GitNoContainer_CarriesRetry pins R2's
// amendment: a never-activated buildFromGit service whose BUILD failed (no
// artifact ever produced, no container) has nothing deployed to lose — the
// gate now carries the ready-made override retry for this shape too (not
// just fresh-misconfigured), alongside Then naming the fix-then-re-import
// sequence.
func TestImport_OverrideOnFailedBuild_GitNoContainer_CarriesRetry(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "s1", Name: "api", Status: platform.ServiceStatusReadyToDeploy},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-1", ServiceStackID: "s1", Status: platform.BuildStatusBuildFailed, Source: "GIT", Created: "2026-09-14T10:00:00Z"},
		}).
		WithServiceAppVersions("s1", []platform.AppVersionEvent{
			{ID: "av-1", ServiceStackID: "s1", Status: platform.BuildStatusBuildFailed, Source: "GIT", Sequence: 1},
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterImport(srv, mock, "proj-1", testEngine(t), "", nil, runtime.Info{})

	yaml := "services:\n  - hostname: api\n    type: nodejs@22\n"
	result := callTool(t, srv, "zerops_import", map[string]any{
		"content":  yaml,
		"override": true,
	})
	if !result.IsError {
		t.Fatalf("expected IsError on override of a never-activated failed-build service without ack")
	}
	var wire ErrorWire
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &wire); err != nil {
		t.Fatalf("parse error wire: %v", err)
	}
	if wire.WouldDestroy == nil {
		t.Fatalf("WouldDestroy missing")
	}
	rc := wire.WouldDestroy.Retry
	if rc == nil || rc.Tool != "zerops_import" || rc.Args["override"] != true {
		t.Fatalf("Retry = %+v, want zerops_import override=true — no version was ever activated, nothing is lost", rc)
	}
	if !strings.Contains(wire.WouldDestroy.Then, "override=true") {
		t.Errorf("Then = %q, want it to name the fix-then-re-import sequence", wire.WouldDestroy.Then)
	}
}

func TestImport_OverrideOnHealthyPasses(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "s1", Name: "api", Status: platform.ServiceStatusActive},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-1", ServiceStackID: "s1", Status: "ACTIVE", Created: "2026-05-05T10:00:00Z"},
		}).
		WithImportResult(&platform.ImportResult{
			ProjectID:   "proj-1",
			ProjectName: "myproject",
			ServiceStacks: []platform.ImportedServiceStack{
				{ID: "s1", Name: "api", Processes: []platform.Process{
					{ID: "p-1", ActionName: "serviceStackImport", Status: serviceStatusRunning},
				}},
			},
		}).
		WithProcess(&platform.Process{ID: "p-1", Status: statusFinished})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterImport(srv, mock, "proj-1", testEngine(t), "", nil, runtime.Info{})

	yaml := "services:\n  - hostname: api\n    type: nodejs@22\n"
	result := callTool(t, srv, "zerops_import", map[string]any{
		"content":  yaml,
		"override": true,
	})
	if result.IsError {
		t.Fatalf("override of healthy service should pass; got error: %s", getTextContent(t, result))
	}
}

func TestImport_AcknowledgedOverrideProceeds(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "s1", Name: "api", Status: platform.ServiceStatusReadyToDeploy},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-1", ServiceStackID: "s1", Status: platform.BuildStatusBuildFailed, Created: "2026-05-05T10:00:00Z"},
		}).
		WithImportResult(&platform.ImportResult{
			ProjectID:   "proj-1",
			ProjectName: "myproject",
			ServiceStacks: []platform.ImportedServiceStack{
				{ID: "s1", Name: "api", Processes: []platform.Process{
					{ID: "p-1", ActionName: "serviceStackImport", Status: serviceStatusRunning},
				}},
			},
		}).
		WithProcess(&platform.Process{ID: "p-1", Status: statusFinished})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterImport(srv, mock, "proj-1", testEngine(t), "", nil, runtime.Info{})

	yaml := "services:\n  - hostname: api\n    type: nodejs@22\n"
	result := callTool(t, srv, "zerops_import", map[string]any{
		"content":  yaml,
		"override": true,
		"confirmDestructive": map[string]any{
			"operation":           "import-override",
			"acknowledgedTargets": []string{"api"},
		},
	})
	if result.IsError {
		t.Fatalf("matching ack should proceed; got error: %s", getTextContent(t, result))
	}
}

func TestImport_PartialAckRejected(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "s1", Name: "api", Status: platform.ServiceStatusReadyToDeploy},
			{ID: "s2", Name: "worker", Status: platform.ServiceStatusReadyToDeploy},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-1", ServiceStackID: "s1", Status: platform.BuildStatusBuildFailed, Created: "2026-05-05T10:00:00Z"},
			{ID: "av-2", ServiceStackID: "s2", Status: platform.BuildStatusDeployFailed, Created: "2026-05-05T11:00:00Z"},
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterImport(srv, mock, "proj-1", testEngine(t), "", nil, runtime.Info{})

	yaml := "services:\n  - hostname: api\n    type: nodejs@22\n  - hostname: worker\n    type: nodejs@22\n"
	result := callTool(t, srv, "zerops_import", map[string]any{
		"content":  yaml,
		"override": true,
		"confirmDestructive": map[string]any{
			"operation":           "import-override",
			"acknowledgedTargets": []string{"api"}, // missing "worker"
		},
	})
	if !result.IsError {
		t.Fatalf("partial ack must be rejected; got success")
	}
	var wire ErrorWire
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &wire); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if wire.Code != platform.ErrDiagnosisRequired {
		t.Errorf("Code = %q, want %q", wire.Code, platform.ErrDiagnosisRequired)
	}
}

func TestImport_AckOperationMismatchRejected(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "s1", Name: "api", Status: platform.ServiceStatusReadyToDeploy},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-1", ServiceStackID: "s1", Status: platform.BuildStatusBuildFailed, Created: "2026-05-05T10:00:00Z"},
		})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterImport(srv, mock, "proj-1", testEngine(t), "", nil, runtime.Info{})

	yaml := "services:\n  - hostname: api\n    type: nodejs@22\n"
	result := callTool(t, srv, "zerops_import", map[string]any{
		"content":  yaml,
		"override": true,
		"confirmDestructive": map[string]any{
			"operation":           "env-set", // wrong operation
			"acknowledgedTargets": []string{"api"},
		},
	})
	if !result.IsError {
		t.Fatalf("operation mismatch must be rejected")
	}
	if !strings.Contains(getTextContent(t, result), "operation") {
		t.Errorf("error text should mention operation mismatch: %s", getTextContent(t, result))
	}
}

func TestImport_NonOverrideImportNotGated(t *testing.T) {
	t.Parallel()
	// New service, no override — failed history on a same-named service in
	// the project should NOT block a new import; failed history only matters
	// when override=true would replace the failed service.
	mock := platform.NewMock().
		WithImportResult(&platform.ImportResult{
			ProjectID:   "proj-1",
			ProjectName: "myproject",
			ServiceStacks: []platform.ImportedServiceStack{
				{ID: "s2", Name: "newsvc", Processes: []platform.Process{
					{ID: "p-1", ActionName: "serviceStackImport", Status: serviceStatusRunning},
				}},
			},
		}).
		WithProcess(&platform.Process{ID: "p-1", Status: statusFinished})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterImport(srv, mock, "proj-1", testEngine(t), "", nil, runtime.Info{})

	yaml := "services:\n  - hostname: newsvc\n    type: nodejs@22\n"
	result := callTool(t, srv, "zerops_import", map[string]any{
		"content": yaml,
		// no override
	})
	if result.IsError {
		t.Errorf("non-override import should pass: %s", getTextContent(t, result))
	}
}
