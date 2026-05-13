package tools

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
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
	if got := gatherLaunchSourceContext(context.Background(), nil, "p"); got != nil {
		t.Errorf("nil client: got %+v want nil", got)
	}
	if got := gatherLaunchSourceContext(context.Background(), platform.NewMock(), ""); got != nil {
		t.Errorf("empty projectID: got %+v want nil", got)
	}
}

// TestGatherLaunchSourceContext_SingleRuntime_SuggestsAll surfaces the
// fully-populated hint when source has exactly one USER-category service.
// 90% case for typical dev/stage shape.
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

	got := gatherLaunchSourceContext(context.Background(), client, "src")
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
	if len(got.AvailableRuntimes) != 1 || got.AvailableRuntimes[0] != "app" {
		t.Errorf("AvailableRuntimes: got %+v want [app] (managed dep filtered)", got.AvailableRuntimes)
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
	got := gatherLaunchSourceContext(context.Background(), client, "src")
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
				ID:                   "svc-app",
				Name:                 "app",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeCategoryName: "USER"},
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
