// Tests for: workflow/lifecycle_status.go — BuildLifecycleStatus canonical
// orientation block returned by zerops_workflow action="status" when no
// bootstrap/recipe is active.
package workflow

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestBuildLifecycleStatus_EmptyProject_IdlePhaseNoServices(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices(nil)
	out := BuildLifecycleStatus(context.Background(), mock, "proj-1", t.TempDir(), "")

	mustContain(t, out, "## Status")
	mustContain(t, out, "Phase: idle")
	mustContain(t, out, "Services: none")
	mustContain(t, out, `workflow="bootstrap"`)
}

// TestBuildLifecycleStatus_EmptyProject_NextListsBothBootstrapAndRecipe —
// v8.100. When the project is empty, the Next section must list BOTH
// workflow="bootstrap" AND workflow="recipe" so an agent invoked to
// build a recipe doesn't read the hint as authoritative and switch to
// bootstrap. The prior version named only bootstrap, which silently
// steered showcase-recipe requests into bootstrap mode.
func TestBuildLifecycleStatus_EmptyProject_NextListsBothBootstrapAndRecipe(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices(nil)
	out := BuildLifecycleStatus(context.Background(), mock, "proj-1", t.TempDir(), "")

	if !strings.Contains(out, `workflow="bootstrap"`) {
		t.Errorf("Next section must include bootstrap option; got:\n%s", out)
	}
	if !strings.Contains(out, `workflow="recipe"`) {
		t.Errorf("Next section must include recipe option (v8.100 bar); got:\n%s", out)
	}
	// The recipe hint must name the three legal tier values so the agent
	// classifies up front rather than defaulting silently.
	for _, tierHint := range []string{"minimal", "showcase"} {
		if !strings.Contains(out, tierHint) {
			t.Errorf("recipe hint must name tier value %q; got:\n%s", tierHint, out)
		}
	}
	// Must name the clientModel requirement so the agent doesn't hit the
	// first-call rejection and have to retry.
	if !strings.Contains(out, "clientModel") {
		t.Errorf("recipe hint must name clientModel requirement; got:\n%s", out)
	}
}

func TestBuildLifecycleStatus_Idle_ListsServicesWithSetup(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{Name: "appdev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{Name: "appstage", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{Name: "db", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"}},
	})
	stateDir := t.TempDir()
	if err := WriteServiceMeta(stateDir, &ServiceMeta{
		Hostname: "appdev", Mode: PlanModeStandard, StageHostname: "appstage",
		DeployStrategy: StrategyPushDev, StrategyConfirmed: true,
		BootstrappedAt: "2026-04-01", BootstrapSession: "s1",
	}); err != nil {
		t.Fatal(err)
	}

	out := BuildLifecycleStatus(context.Background(), mock, "proj-1", stateDir, "")

	mustContain(t, out, "Phase: idle")
	mustContain(t, out, "appdev")
	mustContain(t, out, "nodejs@22")
	mustContain(t, out, "push-dev")
	mustContain(t, out, "db")
	mustContain(t, out, "managed")
	mustContain(t, out, `workflow="develop"`)
}

func TestBuildLifecycleStatus_DevelopActive_ShowsIntentProgress(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{Name: "appdev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	})
	stateDir := t.TempDir()
	if err := WriteServiceMeta(stateDir, &ServiceMeta{
		Hostname: "appdev", Mode: PlanModeDev,
		DeployStrategy: StrategyPushDev, StrategyConfirmed: true,
		BootstrappedAt: "2026-04-01", BootstrapSession: "s1",
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	ws := NewWorkSession("proj-1", "container", "add OAuth login", []string{"appdev"})
	ws.Deploys = map[string][]DeployAttempt{
		"appdev": {{AttemptedAt: now, SucceededAt: now, Setup: "dev", Strategy: "push-dev"}},
	}
	if err := SaveWorkSession(stateDir, ws); err != nil {
		t.Fatal(err)
	}

	out := BuildLifecycleStatus(context.Background(), mock, "proj-1", stateDir, "")

	mustContain(t, out, "Phase: develop")
	mustContain(t, out, "add OAuth login")
	mustContain(t, out, "Progress:")
	mustContain(t, out, "appdev")
	mustContain(t, out, "zerops_verify")
}

func TestBuildLifecycleStatus_AutoClosedSession_SuggestsNextTask(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{Name: "appdev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	})
	stateDir := t.TempDir()
	now := time.Now().UTC().Format(time.RFC3339)
	ws := NewWorkSession("proj-1", "container", "prior task", []string{"appdev"})
	ws.ClosedAt = now
	ws.CloseReason = CloseReasonAutoComplete
	if err := SaveWorkSession(stateDir, ws); err != nil {
		t.Fatal(err)
	}

	out := BuildLifecycleStatus(context.Background(), mock, "proj-1", stateDir, "")

	mustContain(t, out, "complete")
	mustContain(t, out, `action="close"`)
	mustContain(t, out, `action="start"`)
}

func TestBuildLifecycleStatus_NoInternalVocabularyLeaks(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{Name: "appdev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	})
	stateDir := t.TempDir()
	if err := WriteServiceMeta(stateDir, &ServiceMeta{
		Hostname: "appdev", Mode: PlanModeDev,
		BootstrappedAt: "2026-04-01", BootstrapSession: "s1",
	}); err != nil {
		t.Fatal(err)
	}
	ws := NewWorkSession("proj-1", "container", "work", []string{"appdev"})
	if err := SaveWorkSession(stateDir, ws); err != nil {
		t.Fatal(err)
	}

	out := BuildLifecycleStatus(context.Background(), mock, "proj-1", stateDir, "")

	// No implementation-detail vocabulary should leak to the LLM.
	for _, leak := range []string{
		"ServiceMeta", "BootstrapSession", ".zcp/state", "work session", "PID", "stateDir",
	} {
		if strings.Contains(out, leak) {
			t.Errorf("status leaks internal vocabulary %q:\n%s", leak, out)
		}
	}
}

func TestBuildLifecycleStatus_SelfHostnameExcluded(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{Name: "zcpx", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "zcp@1"}},
		{Name: "appdev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	})
	out := BuildLifecycleStatus(context.Background(), mock, "proj-1", t.TempDir(), "zcpx")

	if strings.Contains(out, "zcpx") {
		t.Errorf("self-hostname must be excluded from status:\n%s", out)
	}
	mustContain(t, out, "appdev")
}

func mustContain(t *testing.T, s, sub string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		t.Errorf("expected %q in output:\n%s", sub, s)
	}
}
