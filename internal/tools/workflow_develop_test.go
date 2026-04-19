// Tests for: handleDevelopBriefing — F8 strategy gate before work-session creation.
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

// F8 regression: when any runtime meta has no confirmed strategy, handleDevelopBriefing
// must return the strategy-selection briefing WITHOUT creating a work session.
// Spec-work-session.md §6.1: "Work session is NOT created yet" when strategy is unset.
func TestHandleDevelopBriefing_NoStrategy_DoesNotCreateWorkSession(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)

	// Bootstrapped meta with NO strategy set.
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             workflow.PlanModeDev,
		Environment:      string(workflow.EnvContainer),
		BootstrapSession: "sess1",
		BootstrappedAt:   "2026-04-18",
		// DeployStrategy == "" && StrategyConfirmed == false — unset.
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

	if ws, _ := workflow.CurrentWorkSession(dir); ws != nil {
		t.Errorf("work session was created despite unset strategy (PID=%d)", os.Getpid())
	}

	text := extractText(result)
	if !strings.Contains(text, "strategy") {
		t.Errorf("response should prompt for strategy selection, got: %s", text)
	}
}

// Negative control: with strategy confirmed, a work session IS created.
func TestHandleDevelopBriefing_WithStrategy_CreatesWorkSession(t *testing.T) {
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
		t.Fatal("expected work session to be created when strategy is confirmed")
	}
	t.Cleanup(func() { _ = workflow.DeleteWorkSession(dir, os.Getpid()) })
}
