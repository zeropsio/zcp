// Tests for: handleDevelopBriefing — work-session creation + post-first-deploy
// strategy review (spec-work-session.md §6.1, spec-workflows.md §4.2).
package tools

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// handleDevelopBriefing creates a work session regardless of meta strategy:
// the first deploy is always the default self-deploy mechanism, and the
// strategy decision surfaces via `develop-strategy-review` once
// FirstDeployedAt is stamped.
func TestHandleDevelopBriefing_UnsetStrategy_NeverDeployed_CreatesWorkSession_FirstDeployBranch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)

	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             workflow.PlanModeDev,
		Environment:      string(workflow.EnvContainer),
		BootstrapSession: "sess1",
		BootstrappedAt:   "2026-04-18",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{
			ID:   "svc-appdev",
			Name: "appdev",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{
				ServiceStackTypeVersionName: "nodejs@22",
			},
		},
	})

	result, _, err := handleDevelopBriefing(context.Background(), engine, mock, "proj1",
		WorkflowInput{Intent: "test"}, nil, nil, "", runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("handleDevelopBriefing: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", extractText(result))
	}

	ws, _ := workflow.CurrentWorkSession(dir)
	if ws == nil {
		t.Fatal("expected work session to be created even with unset strategy")
	}
	t.Cleanup(func() { _ = workflow.DeleteWorkSession(dir, os.Getpid()) })

	text := extractText(result)
	// Never-deployed branch owns guidance; strategy-review does NOT fire yet.
	if !strings.Contains(text, "You're in the develop first-deploy branch") {
		t.Errorf("response missing first-deploy-intro marker. Got:\n%s", text)
	}
	if strings.Contains(text, "Pick an ongoing deploy strategy") {
		t.Errorf("strategy-review fired pre-first-deploy — it must wait for FirstDeployedAt. Got:\n%s", text)
	}
}

// After FirstDeployedAt is stamped the develop briefing renders the
// strategy-review atom instead of the first-deploy atoms.
func TestHandleDevelopBriefing_UnsetStrategy_Deployed_StrategyReview(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)

	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             workflow.PlanModeDev,
		Environment:      string(workflow.EnvContainer),
		BootstrapSession: "sess1",
		BootstrappedAt:   "2026-04-18",
		FirstDeployedAt:  "2026-04-19T10:00:00Z",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{
			ID:   "svc-appdev",
			Name: "appdev",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{
				ServiceStackTypeVersionName: "nodejs@22",
			},
		},
	})

	result, _, err := handleDevelopBriefing(context.Background(), engine, mock, "proj1",
		WorkflowInput{Intent: "test"}, nil, nil, "", runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("handleDevelopBriefing: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", extractText(result))
	}

	if ws, _ := workflow.CurrentWorkSession(dir); ws == nil {
		t.Fatal("expected work session to be created")
	}
	t.Cleanup(func() { _ = workflow.DeleteWorkSession(dir, os.Getpid()) })

	text := extractText(result)
	for _, needle := range []string{
		"Pick an ongoing deploy strategy",
		`action="strategy"`,
	} {
		if !strings.Contains(text, needle) {
			t.Errorf("response missing %q — strategy-review atom did not render. Got:\n%s", needle, text)
		}
	}
	if strings.Contains(text, "You're in the develop first-deploy branch") {
		t.Errorf("first-deploy-intro fired on a deployed service. Got:\n%s", text)
	}
}

// Strategy confirmed + deployed → normal edit-loop: neither the
// first-deploy branch nor the review atom fires.
func TestHandleDevelopBriefing_ConfirmedStrategy_Deployed_NoReview(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)

	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:          "appdev",
		Mode:              workflow.PlanModeDev,
		Environment:       string(workflow.EnvContainer),
		DeployStrategy:    workflow.StrategyPushDev,
		StrategyConfirmed: true,
		BootstrapSession:  "sess1",
		BootstrappedAt:    "2026-04-18",
		FirstDeployedAt:   "2026-04-19T10:00:00Z",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{
			ID:   "svc-appdev",
			Name: "appdev",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{
				ServiceStackTypeVersionName: "nodejs@22",
			},
		},
	})

	result, _, err := handleDevelopBriefing(context.Background(), engine, mock, "proj1",
		WorkflowInput{Intent: "test"}, nil, nil, "", runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("handleDevelopBriefing: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", extractText(result))
	}

	if ws, _ := workflow.CurrentWorkSession(dir); ws == nil {
		t.Fatal("expected work session to be created")
	}
	t.Cleanup(func() { _ = workflow.DeleteWorkSession(dir, os.Getpid()) })

	text := extractText(result)
	if strings.Contains(text, "Pick an ongoing deploy strategy") {
		t.Errorf("strategy-review fired on a confirmed-strategy service. Got:\n%s", text)
	}
	if strings.Contains(text, "You're in the develop first-deploy branch") {
		t.Errorf("first-deploy-intro fired on a deployed service. Got:\n%s", text)
	}
}
