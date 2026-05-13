package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestDeriveProductionProjectName_TableDriven pins the dev/stage →
// prod suffix transformation. Recognized prefixes get swapped; unknown
// names get appended.
func TestDeriveProductionProjectName_TableDriven(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"dev-suffix", "myapp-dev", "myapp-prod"},
		{"stage-suffix", "myapp-stage", "myapp-prod"},
		{"staging-suffix", "myapp-staging", "myapp-prod"},
		{"development-suffix", "myapp-development", "myapp-prod"},
		{"no-suffix", "myapp", "myapp-prod"},
		{"empty", "", ""},
		{"prod-already", "myapp-prod", "myapp-prod-prod"}, // edge: user-named "myapp-prod" gets weird suffix; that's fine — it's just a suggestion the agent confirms
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := deriveProductionProjectName(tc.in)
			if got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}

// TestGatherLaunchSourceContext_NilOrEmptyClient returns nil so the
// scope-prompt response omits the sourceContext field entirely.
func TestGatherLaunchSourceContext_NilOrEmptyClient(t *testing.T) {
	t.Parallel()
	if got := gatherLaunchSourceContext(context.Background(), nil, "p", "", runtime.Info{}); got != nil {
		t.Errorf("nil client: got %+v want nil", got)
	}
	if got := gatherLaunchSourceContext(context.Background(), platform.NewMock(), "", "", runtime.Info{}); got != nil {
		t.Errorf("empty projectID: got %+v want nil", got)
	}
}

// TestGatherLaunchSourceContext_SingleRuntime_SuggestsAll surfaces the
// fully-populated hint when source has exactly one USER-category service.
// 90% case for typical dev/simple shapes.
func TestGatherLaunchSourceContext_SingleRuntime_SuggestsAll(t *testing.T) {
	t.Parallel()
	client := platform.NewMock().
		WithProject(&platform.Project{ID: "src", Name: "myapp-dev", Status: "ACTIVE"}).
		WithServices([]platform.ServiceStack{
			{
				ID:   "svc-app",
				Name: "app",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName:  "nodejs@22",
					ServiceStackTypeCategoryName: "USER",
				},
				Status: "ACTIVE",
			},
			{
				ID:   "svc-db",
				Name: "db",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName:  "postgresql@17",
					ServiceStackTypeCategoryName: "DATABASE",
				},
				Status: "ACTIVE",
			},
		})

	got := gatherLaunchSourceContext(context.Background(), client, "src", "", runtime.Info{})
	if got == nil {
		t.Fatal("expected non-nil source context")
	}
	if got.SourceProjectName != "myapp-dev" {
		t.Errorf("SourceProjectName: got %q want myapp-dev", got.SourceProjectName)
	}
	if got.SuggestedTargetName != "myapp-prod" {
		t.Errorf("SuggestedTargetName: got %q want myapp-prod", got.SuggestedTargetName)
	}
	// "db" is system-category — must NOT appear in AvailableRuntimes.
	if len(got.AvailableRuntimes) != 1 || got.AvailableRuntimes[0].Hostname != "app" {
		t.Errorf("AvailableRuntimes: got %+v want [app] (managed dep filtered)", got.AvailableRuntimes)
	}
	if got.AvailableRuntimes[0].Type != "nodejs@22" {
		t.Errorf("AvailableRuntimes[0].Type: got %q want nodejs@22", got.AvailableRuntimes[0].Type)
	}
	if got.SuggestedRuntime != "app" {
		t.Errorf("SuggestedRuntime: got %q want app", got.SuggestedRuntime)
	}
}

// TestGatherLaunchSourceContext_MultiRuntime_NoSuggestion forces the
// agent to ask the user when source has multiple runtime services.
// SuggestedRuntime stays empty; AvailableRuntimes lists choices.
func TestGatherLaunchSourceContext_MultiRuntime_NoSuggestion(t *testing.T) {
	t.Parallel()
	client := platform.NewMock().
		WithProject(&platform.Project{ID: "src", Name: "myapp-stage"}).
		WithServices([]platform.ServiceStack{
			{
				ID:                   "svc-frontend",
				Name:                 "frontend",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeCategoryName: "USER"},
			},
			{
				ID:                   "svc-api",
				Name:                 "api",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeCategoryName: "USER"},
			},
		})
	got := gatherLaunchSourceContext(context.Background(), client, "src", "", runtime.Info{})
	if got == nil {
		t.Fatal("expected non-nil")
	}
	if len(got.AvailableRuntimes) != 2 {
		t.Errorf("AvailableRuntimes: got %+v want 2 entries", got.AvailableRuntimes)
	}
	if got.SuggestedRuntime != "" {
		t.Errorf("SuggestedRuntime: got %q want empty (multi-runtime case forces user choice)", got.SuggestedRuntime)
	}
	if got.SuggestedTargetName != "myapp-prod" {
		t.Errorf("SuggestedTargetName: got %q want myapp-prod (stage→prod swap)", got.SuggestedTargetName)
	}
}

// TestGatherLaunchSourceContext_FiltersZCPByTypeName pins the F2.1
// self-filter: any service-stack of type zcp@1 is excluded from
// AvailableRuntimes regardless of hostname. The control-plane container
// reports USER category to the platform but must never appear as a
// promotion candidate.
func TestGatherLaunchSourceContext_FiltersZCPByTypeName(t *testing.T) {
	t.Parallel()
	client := platform.NewMock().
		WithProject(&platform.Project{ID: "src", Name: "myapp-dev"}).
		WithServices([]platform.ServiceStack{
			{
				ID:   "svc-zcp",
				Name: "zcp",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName:  "zcp@1",
					ServiceStackTypeCategoryName: "USER",
				},
			},
			{
				ID:   "svc-app",
				Name: "app",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName:  "nodejs@22",
					ServiceStackTypeCategoryName: "USER",
				},
			},
		})

	got := gatherLaunchSourceContext(context.Background(), client, "src", "", runtime.Info{})
	if got == nil {
		t.Fatal("expected non-nil")
	}
	if len(got.AvailableRuntimes) != 1 || got.AvailableRuntimes[0].Hostname != "app" {
		t.Errorf("AvailableRuntimes: got %+v want [app] (zcp@1 filtered)", got.AvailableRuntimes)
	}
}

// TestGatherLaunchSourceContext_FiltersByRuntimeHostname pins the
// defense-in-depth hostname branch: a USER-category runtime named the
// same as rt.ServiceName is excluded even if its type is normal. Uses
// a non-zcp@1 type to isolate the hostname-filter branch from the
// type-filter branch.
func TestGatherLaunchSourceContext_FiltersByRuntimeHostname(t *testing.T) {
	t.Parallel()
	client := platform.NewMock().
		WithProject(&platform.Project{ID: "src", Name: "myapp-dev"}).
		WithServices([]platform.ServiceStack{
			{
				ID:   "svc-self",
				Name: "appdev",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName:  "nodejs@22",
					ServiceStackTypeCategoryName: "USER",
				},
			},
			{
				ID:   "svc-other",
				Name: "worker",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName:  "nodejs@22",
					ServiceStackTypeCategoryName: "USER",
				},
			},
		})

	got := gatherLaunchSourceContext(context.Background(), client, "src", "", runtime.Info{ServiceName: "appdev"})
	if got == nil {
		t.Fatal("expected non-nil")
	}
	if len(got.AvailableRuntimes) != 1 || got.AvailableRuntimes[0].Hostname != "worker" {
		t.Errorf("AvailableRuntimes: got %+v want [worker] (self-hostname appdev filtered)", got.AvailableRuntimes)
	}
}

// TestGatherLaunchSourceContext_CollapsesStandardPair pins F13.1:
// when ZCP holds a ServiceMeta for a dev/stage pair (mode=standard),
// the dev-half is hidden from AvailableRuntimes and the STAGE-half
// surfaces as the promotion headline with DevHostname populated for
// disclosure. Stage is the validated last-known-good copy — the
// natural promotion-source mental model.
func TestGatherLaunchSourceContext_CollapsesStandardPair(t *testing.T) {
	t.Parallel()
	stateDir := writeRuntimeMeta(t, &workflow.ServiceMeta{
		Hostname:       "appdev",
		Mode:           topology.ModeStandard,
		StageHostname:  "appstage",
		BootstrappedAt: "2026-05-01T00:00:00Z",
	})

	client := platform.NewMock().
		WithProject(&platform.Project{ID: "src", Name: "myapp-dev"}).
		WithServices([]platform.ServiceStack{
			{
				ID:   "svc-appdev",
				Name: "appdev",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName:  "nodejs@22",
					ServiceStackTypeCategoryName: "USER",
				},
			},
			{
				ID:   "svc-appstage",
				Name: "appstage",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName:  "nodejs@22",
					ServiceStackTypeCategoryName: "USER",
				},
			},
		})

	got := gatherLaunchSourceContext(context.Background(), client, "src", stateDir, runtime.Info{})
	if got == nil {
		t.Fatal("expected non-nil")
	}
	if len(got.AvailableRuntimes) != 1 {
		t.Fatalf("AvailableRuntimes: got %d entries want 1 (pair collapsed)\n%+v", len(got.AvailableRuntimes), got.AvailableRuntimes)
	}
	rc := got.AvailableRuntimes[0]
	if rc.Hostname != "appstage" {
		t.Errorf("collapsed pair Hostname: got %q want appstage (stage-half headline)", rc.Hostname)
	}
	if rc.DevHostname != "appdev" {
		t.Errorf("collapsed pair DevHostname: got %q want appdev (dev-half disclosure)", rc.DevHostname)
	}
	if rc.Mode != string(topology.ModeStandard) {
		t.Errorf("collapsed pair Mode: got %q want %q", rc.Mode, topology.ModeStandard)
	}
	if got.SuggestedRuntime != "appstage" {
		t.Errorf("SuggestedRuntime: got %q want appstage (stage-half headline)", got.SuggestedRuntime)
	}
}

// TestGatherLaunchSourceContext_SimpleAndDevModesUnaffected verifies
// that non-pair modes pass through unchanged — no pair collapse,
// no DevHostname populated.
func TestGatherLaunchSourceContext_SimpleAndDevModesUnaffected(t *testing.T) {
	t.Parallel()
	stateDir := writeRuntimeMeta(t, &workflow.ServiceMeta{
		Hostname:       "app",
		Mode:           topology.ModeSimple,
		BootstrappedAt: "2026-05-01T00:00:00Z",
	})

	client := platform.NewMock().
		WithProject(&platform.Project{ID: "src", Name: "myapp-dev"}).
		WithServices([]platform.ServiceStack{
			{
				ID:   "svc-app",
				Name: "app",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName:  "nodejs@22",
					ServiceStackTypeCategoryName: "USER",
				},
			},
		})

	got := gatherLaunchSourceContext(context.Background(), client, "src", stateDir, runtime.Info{})
	if got == nil || len(got.AvailableRuntimes) != 1 {
		t.Fatalf("expected single entry, got %+v", got)
	}
	rc := got.AvailableRuntimes[0]
	if rc.Hostname != "app" {
		t.Errorf("Hostname: got %q want app", rc.Hostname)
	}
	if rc.DevHostname != "" {
		t.Errorf("DevHostname: got %q want empty (no pair)", rc.DevHostname)
	}
	if rc.Mode != string(topology.ModeSimple) {
		t.Errorf("Mode: got %q want %q", rc.Mode, topology.ModeSimple)
	}
}

// TestNormalizeTargetServiceForLaunch_StageInputCollapsesToDev pins
// the F13 acceptance path: stage-half hostname normalizes to the
// canonical dev-half (ServiceMeta primary key) so downstream meta
// lookup and bundle composition use the consistent key. Pre-F13 this
// resolver fired a scope-prompt blocker instead — Karel pushback
// flipped the model to accept both halves.
func TestNormalizeTargetServiceForLaunch_StageInputCollapsesToDev(t *testing.T) {
	t.Parallel()
	stateDir := writeRuntimeMeta(t, &workflow.ServiceMeta{
		Hostname:       "appdev",
		StageHostname:  "appstage",
		Mode:           topology.ModeStandard,
		BootstrappedAt: "2026-05-01T00:00:00Z",
	})

	if got := normalizeTargetServiceForLaunch(stateDir, "appstage"); got != "appdev" {
		t.Errorf("stage-half input: got %q want appdev (normalized to dev-half meta key)", got)
	}
	if got := normalizeTargetServiceForLaunch(stateDir, "appdev"); got != "appdev" {
		t.Errorf("dev-half input: got %q want appdev (passthrough)", got)
	}
	if got := normalizeTargetServiceForLaunch(stateDir, "unknown"); got != "unknown" {
		t.Errorf("unknown hostname: got %q want unknown (fall through, downstream validates)", got)
	}
	if got := normalizeTargetServiceForLaunch("", "appstage"); got != "appstage" {
		t.Errorf("empty stateDir: got %q want appstage (no state to consult)", got)
	}
}

// TestHandleLaunchProduction_ScopePrompt_SurfacesSourceContext pins the
// integration: when the source project is reachable and has a single
// runtime, the scope-prompt response carries SourceContext with all
// fields populated so the agent's next call can apply suggestions.
func TestHandleLaunchProduction_ScopePrompt_SurfacesSourceContext(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := platform.NewMock().
		WithProject(&platform.Project{ID: "src", Name: "myapp-dev"}).
		WithServices([]platform.ServiceStack{
			{
				ID:   "svc-app",
				Name: "app",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName:  "nodejs@22",
					ServiceStackTypeCategoryName: "USER",
				},
			},
		})

	input := WorkflowInput{Workflow: workflowLaunchProduction}
	result, _, err := handleLaunchProduction(ctx, "src", client, input, t.TempDir(), runtime.Info{}, nil)
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	resp := decodeLaunchResp(t, []byte(extractText(result)))
	if resp.Status != "scope-prompt" {
		t.Fatalf("status: got %q want scope-prompt", resp.Status)
	}
	if resp.SourceContext == nil {
		t.Fatal("expected non-nil SourceContext on scope-prompt response")
	}
	if resp.SourceContext.SuggestedTargetName != "myapp-prod" {
		t.Errorf("SuggestedTargetName: got %q want myapp-prod", resp.SourceContext.SuggestedTargetName)
	}
	if resp.SourceContext.SuggestedRuntime != "app" {
		t.Errorf("SuggestedRuntime: got %q want app", resp.SourceContext.SuggestedRuntime)
	}
}

// writeRuntimeMeta writes one ServiceMeta into a fresh temp state-dir
// and returns the stateDir path. Helper for tests that exercise
// pair-keyed collapse and stage-half normalization.
func writeRuntimeMeta(t *testing.T, meta *workflow.ServiceMeta) string {
	t.Helper()
	dir := t.TempDir()
	servicesDir := filepath.Join(dir, "services")
	if err := os.MkdirAll(servicesDir, 0o755); err != nil {
		t.Fatalf("mkdir services: %v", err)
	}
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatalf("marshal meta: %v", err)
	}
	path := filepath.Join(servicesDir, meta.Hostname+".json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("write meta: %v", err)
	}
	return dir
}
