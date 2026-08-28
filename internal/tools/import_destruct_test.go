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

	// R6-P4: the gate emits a complete, non-authoring retryCall — same call,
	// override=true, confirmDestructive pre-filled, + a startWithoutCode patch hint.
	rc := wire.WouldDestroy.Retry
	if rc == nil || rc.Tool != "zerops_import" || rc.Args["override"] != true {
		t.Fatalf("Retry = %+v, want zerops_import override=true", rc)
	}
	cd, _ := rc.Args["confirmDestructive"].(map[string]any)
	if cd == nil || cd["operation"] != "import-override" {
		t.Errorf("retryCall confirmDestructive = %v, want pre-filled operation", cd)
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
func TestGateOverrideOnFailedHistory_SuggestionIncludesRetryShape(t *testing.T) {
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
