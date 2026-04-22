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
// the first deploy is always the default push mechanism, and the
// strategy decision surfaces via `develop-strategy-review` once the
// envelope's Deployed projection flips to true (derived from session
// history + platform status; see compute_envelope.DeriveDeployed).
func TestHandleDevelopBriefing_UnsetStrategy_NeverDeployed_CreatesWorkSession_FirstDeployBranch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)

	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             workflow.PlanModeDev,
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
		WorkflowInput{Intent: "test", Scope: []string{"appdev"}}, runtime.Info{InContainer: true})
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
		t.Errorf("strategy-review fired pre-first-deploy — it must wait until Deployed flips true. Got:\n%s", text)
	}
}

// Once FirstDeployedAt is stamped (via a successful session deploy or
// adoption-at-ACTIVE), develop renders strategy-review instead of
// first-deploy atoms.
func TestHandleDevelopBriefing_UnsetStrategy_Deployed_StrategyReview(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)

	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             workflow.PlanModeDev,
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
		WorkflowInput{Intent: "test", Scope: []string{"appdev"}}, runtime.Info{InContainer: true})
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
		WorkflowInput{Intent: "test", Scope: []string{"appdev"}}, runtime.Info{InContainer: true})
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

// P1 invariant: scope is fixed at start to the agent-supplied hostnames,
// NOT derived from the full set of runtime ServiceMetas in the project.
// The fizzy conversation bug was that a prior Laravel session's metas
// (appdev/appstage) polluted the new Fizzy session's scope, so auto-close
// could never fire. After P1 the scope follows the agent's `scope` input
// exactly — appdev/appstage are visible as services but not in-scope for
// the Fizzy task's auto-close.
func TestHandleDevelopBriefing_MultiStack_ScopeHonorsInput(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)

	// Leftover Laravel metas from a prior session (the classic fizzy bug shape).
	for _, h := range []string{"appdev", "appstage"} {
		if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
			Hostname:         h,
			Mode:             workflow.PlanModeStandard,
			BootstrapSession: "laravel-sess",
			BootstrappedAt:   "2026-04-10",
		}); err != nil {
			t.Fatalf("WriteServiceMeta(%s): %v", h, err)
		}
	}
	// New Fizzy metas from today's bootstrap.
	for _, h := range []string{"fizzydev", "fizzystage"} {
		if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
			Hostname:         h,
			Mode:             workflow.PlanModeStandard,
			BootstrapSession: "fizzy-sess",
			BootstrappedAt:   "2026-04-21",
		}); err != nil {
			t.Fatalf("WriteServiceMeta(%s): %v", h, err)
		}
	}

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-appdev", Name: "appdev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "php-nginx@8.4"}},
		{ID: "svc-appstage", Name: "appstage", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "php-nginx@8.4"}},
		{ID: "svc-fizzydev", Name: "fizzydev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "ruby@3.4"}},
		{ID: "svc-fizzystage", Name: "fizzystage", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "ruby@3.4"}},
	})

	result, _, err := handleDevelopBriefing(context.Background(), engine, mock, "proj1",
		WorkflowInput{Intent: "run fizzy", Scope: []string{"fizzydev", "fizzystage"}},
		runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("handleDevelopBriefing: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", extractText(result))
	}

	ws, _ := workflow.CurrentWorkSession(dir)
	if ws == nil {
		t.Fatal("expected work session to be created")
	}
	t.Cleanup(func() { _ = workflow.DeleteWorkSession(dir, os.Getpid()) })

	wantScope := map[string]bool{"fizzydev": true, "fizzystage": true}
	if len(ws.Services) != len(wantScope) {
		t.Fatalf("scope pollution: got %v, want only fizzydev+fizzystage", ws.Services)
	}
	for _, h := range ws.Services {
		if !wantScope[h] {
			t.Errorf("unexpected host %q in scope — Laravel services must not leak into Fizzy session", h)
		}
	}
}

// P1: scope must be supplied at start — no implicit derivation from metas.
func TestHandleDevelopBriefing_MissingScope_Rejected(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)

	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             workflow.PlanModeDev,
		BootstrapSession: "sess1",
		BootstrappedAt:   "2026-04-18",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-appdev", Name: "appdev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	})

	result, _, err := handleDevelopBriefing(context.Background(), engine, mock, "proj1",
		WorkflowInput{Intent: "no scope"}, runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("handleDevelopBriefing: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error when scope is missing, got:\n%s", extractText(result))
	}
	text := extractText(result)
	for _, needle := range []string{"scope", "appdev"} {
		if !strings.Contains(text, needle) {
			t.Errorf("missing hint %q in error. Got:\n%s", needle, text)
		}
	}
	if ws, _ := workflow.CurrentWorkSession(dir); ws != nil {
		t.Error("work session must not be created when scope is missing")
		_ = workflow.DeleteWorkSession(dir, os.Getpid())
	}
}

// P1: scope containing an unknown (or managed) hostname is rejected with
// a diagnostic listing the known runtime services.
func TestHandleDevelopBriefing_UnknownHostInScope_Rejected(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)

	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             workflow.PlanModeDev,
		BootstrapSession: "sess1",
		BootstrappedAt:   "2026-04-18",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-appdev", Name: "appdev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	})

	result, _, err := handleDevelopBriefing(context.Background(), engine, mock, "proj1",
		WorkflowInput{Intent: "typo", Scope: []string{"appdev", "ghost"}}, runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("handleDevelopBriefing: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error when scope has unknown host, got:\n%s", extractText(result))
	}
	text := extractText(result)
	for _, needle := range []string{"ghost", "appdev"} {
		if !strings.Contains(text, needle) {
			t.Errorf("missing %q in error. Got:\n%s", needle, text)
		}
	}
}

// P2: a new intent on an open session auto-closes the prior one and
// replaces it with a fresh session for the new task. No WORKFLOW_ACTIVE
// error, no need to manually close first.
func TestHandleDevelopBriefing_NewIntent_AutoClosesPrior(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)

	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             workflow.PlanModeDev,
		BootstrapSession: "sess1",
		BootstrappedAt:   "2026-04-18",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-appdev", Name: "appdev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	})

	// First start — task A.
	resA, _, err := handleDevelopBriefing(context.Background(), engine, mock, "proj1",
		WorkflowInput{Intent: "task A", Scope: []string{"appdev"}}, runtime.Info{InContainer: true})
	if err != nil || resA.IsError {
		t.Fatalf("first start failed: %v / %s", err, extractText(resA))
	}

	wsA, _ := workflow.CurrentWorkSession(dir)
	if wsA == nil || wsA.Intent != "task A" {
		t.Fatalf("expected session with intent 'task A', got %+v", wsA)
	}
	// Simulate some attempt history on task A.
	_ = workflow.RecordDeployAttempt(dir, "appdev", workflow.DeployAttempt{
		AttemptedAt: "2026-04-21T10:00:00Z",
		SucceededAt: "2026-04-21T10:00:30Z",
		Strategy:    workflow.StrategyPushDev,
	})

	// Second start — task B with different intent.
	resB, _, err := handleDevelopBriefing(context.Background(), engine, mock, "proj1",
		WorkflowInput{Intent: "task B", Scope: []string{"appdev"}}, runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("second start error: %v", err)
	}
	if resB.IsError {
		t.Fatalf("new intent must auto-close prior, not error. Got:\n%s", extractText(resB))
	}

	wsB, _ := workflow.CurrentWorkSession(dir)
	if wsB == nil {
		t.Fatal("expected fresh session for task B")
	}
	t.Cleanup(func() { _ = workflow.DeleteWorkSession(dir, os.Getpid()) })

	if wsB.Intent != "task B" {
		t.Errorf("intent = %q, want %q", wsB.Intent, "task B")
	}
	if len(wsB.Deploys) != 0 {
		t.Errorf("new session must start clean, has deploys: %+v", wsB.Deploys)
	}
}

// P2 idempotency: repeated start with the SAME intent returns the current
// briefing without wiping session state (no accidental history loss).
func TestHandleDevelopBriefing_SameIntent_Idempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)

	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             workflow.PlanModeDev,
		BootstrapSession: "sess1",
		BootstrappedAt:   "2026-04-18",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-appdev", Name: "appdev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	})

	_, _, err := handleDevelopBriefing(context.Background(), engine, mock, "proj1",
		WorkflowInput{Intent: "task A", Scope: []string{"appdev"}}, runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("first start: %v", err)
	}
	_ = workflow.RecordDeployAttempt(dir, "appdev", workflow.DeployAttempt{
		AttemptedAt: "2026-04-21T10:00:00Z",
		SucceededAt: "2026-04-21T10:00:30Z",
		Strategy:    workflow.StrategyPushDev,
	})
	t.Cleanup(func() { _ = workflow.DeleteWorkSession(dir, os.Getpid()) })

	// Same intent again — must not drop history.
	_, _, err = handleDevelopBriefing(context.Background(), engine, mock, "proj1",
		WorkflowInput{Intent: "task A", Scope: []string{"appdev"}}, runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("idempotent start: %v", err)
	}

	ws, _ := workflow.CurrentWorkSession(dir)
	if ws == nil {
		t.Fatal("session disappeared on idempotent restart")
	}
	if ws.Intent != "task A" {
		t.Errorf("intent = %q, want %q", ws.Intent, "task A")
	}
	if len(ws.Deploys["appdev"]) != 1 {
		t.Errorf("idempotent restart must preserve deploy history, got: %+v", ws.Deploys)
	}
}
