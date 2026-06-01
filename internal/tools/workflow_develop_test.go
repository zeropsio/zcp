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
	"github.com/zeropsio/zcp/internal/topology"
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
		Mode:             topology.PlanModeDev,
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
		Mode:             topology.PlanModeDev,
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
		"Pick an ongoing close-mode",
		`action="close-mode"`,
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
		Hostname:                 "appdev",
		Mode:                     topology.PlanModeDev,
		CloseDeployMode:          topology.CloseModeAuto,
		CloseDeployModeConfirmed: true,
		BootstrapSession:         "sess1",
		BootstrappedAt:           "2026-04-18",
		FirstDeployedAt:          "2026-04-19T10:00:00Z",
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
	if strings.Contains(text, "Pick an ongoing close-mode") {
		t.Errorf("strategy-review fired on a confirmed-close-mode service. Got:\n%s", text)
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
			Mode:             topology.PlanModeStandard,
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
			Mode:             topology.PlanModeStandard,
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

// Pair-keyed invariant (spec-workflows.md §8 E8): a container+standard pair
// is stored as ONE meta keyed by the dev hostname, with StageHostname holding
// the stage pair. Scope validation must accept both halves — atom
// develop-first-deploy-promote-stage tells the agent to include both for
// auto-close. Before ManagedRuntimeIndex, scope=["appdev","appstage"] was
// rejected as "non-deployable hostnames" because runtimeMetas was keyed by
// m.Hostname alone.
func TestHandleDevelopBriefing_StandardPair_StageInScope_Accepted(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)

	// One meta file representing the container+standard pair.
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		StageHostname:    "appstage",
		Mode:             topology.PlanModeStandard,
		BootstrapSession: "sess1",
		BootstrappedAt:   "2026-04-22",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-appdev", Name: "appdev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "php-nginx@8.4"}},
		{ID: "svc-appstage", Name: "appstage", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "php-nginx@8.4"}},
	})

	result, _, err := handleDevelopBriefing(context.Background(), engine, mock, "proj1",
		WorkflowInput{Intent: "first deploy + promote", Scope: []string{"appdev", "appstage"}},
		runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("handleDevelopBriefing: %v", err)
	}
	if result.IsError {
		t.Fatalf("scope=[appdev,appstage] must be accepted — got error:\n%s", extractText(result))
	}

	ws, _ := workflow.CurrentWorkSession(dir)
	if ws == nil {
		t.Fatal("work session expected")
	}
	t.Cleanup(func() { _ = workflow.DeleteWorkSession(dir, os.Getpid()) })

	wantScope := map[string]bool{"appdev": true, "appstage": true}
	if len(ws.Services) != 2 {
		t.Fatalf("scope len: want 2, got %d (%v)", len(ws.Services), ws.Services)
	}
	for _, h := range ws.Services {
		if !wantScope[h] {
			t.Errorf("unexpected hostname %q in scope", h)
		}
	}
	// Sorted order per validateDevelopScope contract.
	if ws.Services[0] != "appdev" || ws.Services[1] != "appstage" {
		t.Errorf("scope order: want [appdev,appstage], got %v", ws.Services)
	}
}

// Standard-mode scope auto-expansion: agent passes only the dev half;
// validateDevelopScope auto-includes the paired stage hostname so the
// auto-close gate counts both halves and develop-active atoms can fire
// on the (standard, deployed, unset) triple. Without this, both real
// sessions stopped at the dev URL because appstage was invisible to the
// session-progress accounting (see plans/develop-stage-promotion-2026-05-01.md).
func TestHandleDevelopBriefing_StandardPair_DevOnlyInScope_AutoExpandsStage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)

	// Pair-keyed: ONE meta with Hostname=appdev, StageHostname=appstage.
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		StageHostname:    "appstage",
		Mode:             topology.PlanModeStandard,
		BootstrapSession: "sess1",
		BootstrappedAt:   "2026-04-22",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-appdev", Name: "appdev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{ID: "svc-appstage", Name: "appstage", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "static"}},
	})

	result, _, err := handleDevelopBriefing(context.Background(), engine, mock, "proj1",
		WorkflowInput{Intent: "iterate then promote", Scope: []string{"appdev"}},
		runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("handleDevelopBriefing: %v", err)
	}
	if result.IsError {
		t.Fatalf("scope=[appdev] for standard pair must be accepted — got error:\n%s", extractText(result))
	}

	ws, _ := workflow.CurrentWorkSession(dir)
	if ws == nil {
		t.Fatal("work session expected")
	}
	t.Cleanup(func() { _ = workflow.DeleteWorkSession(dir, os.Getpid()) })

	if len(ws.Services) != 2 {
		t.Fatalf("expected scope auto-expanded to [appdev,appstage], got %v", ws.Services)
	}
	if ws.Services[0] != "appdev" || ws.Services[1] != "appstage" {
		t.Errorf("scope order: want sorted [appdev,appstage], got %v", ws.Services)
	}
}

// Idempotent: when the agent already passed both halves explicitly, the
// expansion must not duplicate the stage hostname.
func TestHandleDevelopBriefing_StandardPair_BothHalvesInScope_NoDuplicate(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)

	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		StageHostname:    "appstage",
		Mode:             topology.PlanModeStandard,
		BootstrapSession: "sess1",
		BootstrappedAt:   "2026-04-22",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-appdev", Name: "appdev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{ID: "svc-appstage", Name: "appstage", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "static"}},
	})

	result, _, err := handleDevelopBriefing(context.Background(), engine, mock, "proj1",
		WorkflowInput{Intent: "explicit both", Scope: []string{"appdev", "appstage"}},
		runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("handleDevelopBriefing: %v", err)
	}
	if result.IsError {
		t.Fatalf("explicit pair scope rejected:\n%s", extractText(result))
	}

	ws, _ := workflow.CurrentWorkSession(dir)
	t.Cleanup(func() { _ = workflow.DeleteWorkSession(dir, os.Getpid()) })

	if len(ws.Services) != 2 {
		t.Fatalf("expected scope to stay [appdev,appstage], got %v", ws.Services)
	}
}

// Stage-only scope (agent passes only the stage hostname) must NOT be
// expanded — there's no "stage half" to add. The validation accepts it as-is.
func TestHandleDevelopBriefing_StandardPair_StageOnlyInScope_NoExpansion(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)

	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		StageHostname:    "appstage",
		Mode:             topology.PlanModeStandard,
		BootstrapSession: "sess1",
		BootstrappedAt:   "2026-04-22",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-appdev", Name: "appdev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{ID: "svc-appstage", Name: "appstage", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "static"}},
	})

	result, _, err := handleDevelopBriefing(context.Background(), engine, mock, "proj1",
		WorkflowInput{Intent: "stage only", Scope: []string{"appstage"}},
		runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("handleDevelopBriefing: %v", err)
	}
	if result.IsError {
		t.Fatalf("stage-only scope rejected:\n%s", extractText(result))
	}

	ws, _ := workflow.CurrentWorkSession(dir)
	t.Cleanup(func() { _ = workflow.DeleteWorkSession(dir, os.Getpid()) })

	if len(ws.Services) != 1 || ws.Services[0] != "appstage" {
		t.Errorf("expected scope=[appstage], got %v", ws.Services)
	}
}

// Broken pair: meta carries StageHostname but the stage service was
// deleted from the project. PruneServiceMetas keeps the meta as long as
// either half is alive (the dev half here), so naive scope expansion
// would silently widen scope to a hostname deploy/verify can't reach
// — auto-close would never fire. validateDevelopScope must fail-fast
// with repair guidance instead.
func TestHandleDevelopBriefing_StandardPair_StageMissingFromLive_FailsWithRepairGuidance(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)

	// Pair meta names appstage as the stage half.
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		StageHostname:    "appstage",
		Mode:             topology.PlanModeStandard,
		BootstrapSession: "sess1",
		BootstrappedAt:   "2026-04-22",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	// Live services include only appdev — appstage was deleted from Zerops UI
	// while the meta still references it. PruneServiceMetas keeps the meta
	// (Hostname matches a live service).
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-appdev", Name: "appdev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	})

	result, _, err := handleDevelopBriefing(context.Background(), engine, mock, "proj1",
		WorkflowInput{Intent: "iterate broken pair", Scope: []string{"appdev"}},
		runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("handleDevelopBriefing: %v", err)
	}
	if !result.IsError {
		t.Fatalf("broken pair must surface as error, got success:\n%s", extractText(result))
	}
	body := extractText(result)
	if !strings.Contains(body, "appstage") {
		t.Errorf("error message must name the missing stage hostname; got:\n%s", body)
	}
	if !strings.Contains(body, "bootstrap") {
		t.Errorf("error suggestion must point at re-bootstrap as repair path; got:\n%s", body)
	}

	// Session must NOT be created — broken pair is a precondition failure,
	// not a "try again with different scope" scenario.
	if ws, _ := workflow.CurrentWorkSession(dir); ws != nil {
		t.Errorf("work session must not be created for broken pair; found %+v", ws)
		_ = workflow.DeleteWorkSession(dir, os.Getpid())
	}
}

// Non-standard modes must NOT trigger expansion (dev / simple have no
// stage to promote to). Codex's recommendation §5.
func TestHandleDevelopBriefing_DevMode_NoExpansion(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)

	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeDev,
		BootstrapSession: "sess1",
		BootstrappedAt:   "2026-04-22",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-appdev", Name: "appdev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	})

	result, _, err := handleDevelopBriefing(context.Background(), engine, mock, "proj1",
		WorkflowInput{Intent: "dev mode iterate", Scope: []string{"appdev"}},
		runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("handleDevelopBriefing: %v", err)
	}
	if result.IsError {
		t.Fatalf("dev-mode scope rejected:\n%s", extractText(result))
	}

	ws, _ := workflow.CurrentWorkSession(dir)
	t.Cleanup(func() { _ = workflow.DeleteWorkSession(dir, os.Getpid()) })

	if len(ws.Services) != 1 || ws.Services[0] != "appdev" {
		t.Errorf("dev mode must not auto-expand; got %v", ws.Services)
	}
}

// H2 defense: when no ServiceMeta exists (services live but unmanaged),
// develop's ADOPT_REQUIRED rejection must carry a structured Recovery
// pointing the agent at bootstrap+route=adopt. Generic
// `WithRecoveryStatus()` (the prior shape) forced agents to round-trip
// through `zerops_workflow action=status` before discovering the next call,
// which the workflow router was supposed to telegraph in the first place
// (build_plan.go:382-388, fixed in same plan). This test pins the
// defense-in-depth shape so any future regression at workflow_develop.go
// is caught.
//
// Wire code is `ADOPT_REQUIRED` (the narrowed semantic) post-S1 from
// plans/test-pinning-elevation-2026-05-06.md — TestErrAdoptRequiredCarriesAdoptRecovery
// pins the per-code Recovery contract uniformly; this test pins the
// specific develop-rejection behavior end-to-end.
func TestHandleDevelopBriefing_NoBootstrappedServices_RecoveryPointsAtBootstrapAdopt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)

	// No ServiceMeta written — simulating live unmanaged services that
	// the agent skipped past bootstrap and hit develop directly.
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-appdev", Name: "appdev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	})

	result, _, err := handleDevelopBriefing(context.Background(), engine, mock, "proj1",
		WorkflowInput{Intent: "deploy something"}, runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("handleDevelopBriefing: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected ADOPT_REQUIRED rejection, got success:\n%s", extractText(result))
	}
	text := extractText(result)

	// Structured Recovery shape — agent reads JSON, not prose.
	for _, needle := range []string{
		`"recovery"`,
		`"tool":"zerops_workflow"`,
		`"action":"start"`,
		`"workflow":"bootstrap"`,
		`"route":"adopt"`,
	} {
		if !strings.Contains(text, needle) {
			t.Errorf("Recovery missing %q in error wire shape. Got:\n%s", needle, text)
		}
	}
	// Defense — must NOT fall back to generic status Recovery.
	if strings.Contains(text, `"recovery":{"tool":"zerops_workflow","action":"status"}`) {
		t.Errorf("Recovery is generic status; expected specific bootstrap+adopt shape. Got:\n%s", text)
	}
}

// P1: scope must be supplied at start — no implicit derivation from metas.
func TestHandleDevelopBriefing_MissingScope_Rejected(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)

	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeDev,
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
		Mode:             topology.PlanModeDev,
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
		Mode:             topology.PlanModeDev,
		BootstrapSession: "sess1",
		BootstrappedAt:   "2026-04-18",
		// CloseDeployMode=auto keeps the new-intent auto-delete path
		// open under the deploy-decomp P6 close-mode gate (manual / unset
		// services block implicit discard; auto / git-push permit it).
		CloseDeployMode: topology.CloseModeAuto,
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
		Mode:             topology.PlanModeDev,
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

// Phase 2 of the local-mode prune fix: PruneServiceMetas now keeps the
// local-only project meta (commit e9c2be9c). The next layer is develop
// scope: local-only meta is project-keyed (Hostname=project.Name) and
// not a deployable runtime. validateDevelopScope used to list the
// project name as "available runtime services", which sent the agent
// chasing a hostname that isn't a service. The fix is to surface a
// specific adopt-local recovery when the only metas are local-only.
//
// Reproducer (live): eval/behavioral/runs-local/20260506-144837 self-review §1.
func TestHandleDevelopBriefing_LocalOnly_GuidesAdoptLocal(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvLocal, nil)

	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:       "eval-zcp", // project name (LocalAutoAdopt convention)
		Mode:           topology.PlanModeLocalOnly,
		BootstrappedAt: "2026-05-06",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-app", Name: "app", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{ID: "svc-zcp", Name: "zcp", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "zcp@1"}},
	})

	result, _, err := handleDevelopBriefing(context.Background(), engine, mock, "proj1",
		WorkflowInput{Intent: "deploy notes API", Scope: []string{"app"}}, runtime.Info{InContainer: false})
	if err != nil {
		t.Fatalf("handleDevelopBriefing: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected adopt-local guidance, got success:\n%s", extractText(result))
	}
	text := extractText(result)
	for _, needle := range []string{
		`"recovery"`,
		`"action":"adopt-local"`,
		"local-only",
	} {
		if !strings.Contains(text, needle) {
			t.Errorf("expected adopt-local guidance with %q. Got:\n%s", needle, text)
		}
	}
	if strings.Contains(text, "eval-zcp") && strings.Contains(text, "available runtime services") {
		t.Errorf("must not list project name as 'available runtime services'. Got:\n%s", text)
	}
}

// Local-stage's project-name key (m.Hostname=projectName) MUST NOT appear
// in the "available runtime services" list emitted by validateDevelopScope.
// Only the linked stage-hostname is a deployable scope target.
func TestHandleDevelopBriefing_LocalStage_ProjectNameNotInAvailable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvLocal, nil)

	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:       "eval-zcp",
		StageHostname:  "app",
		Mode:           topology.PlanModeLocalStage,
		BootstrappedAt: "2026-05-06",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-app", Name: "app", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	})

	result, _, err := handleDevelopBriefing(context.Background(), engine, mock, "proj1",
		WorkflowInput{Intent: "wrong scope", Scope: []string{"wrongHost"}}, runtime.Info{InContainer: false})
	if err != nil {
		t.Fatalf("handleDevelopBriefing: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error, got:\n%s", extractText(result))
	}
	text := extractText(result)
	if !strings.Contains(text, "app") {
		t.Errorf("expected 'app' (stage hostname) listed as available; got:\n%s", text)
	}
	if strings.Contains(text, "eval-zcp") {
		t.Errorf("must not list project name 'eval-zcp' as scopable; got:\n%s", text)
	}
}

// Local-stage with scope=[stageHostname] is the happy path — work
// session created, scope normalized to the stage hostname.
func TestHandleDevelopBriefing_LocalStage_StageHostInScope_Accepted(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvLocal, nil)

	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:        "eval-zcp",
		StageHostname:   "app",
		Mode:            topology.PlanModeLocalStage,
		BootstrappedAt:  "2026-05-06",
		CloseDeployMode: topology.CloseModeAuto,
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	t.Cleanup(func() { _ = workflow.DeleteWorkSession(dir, os.Getpid()) })

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-app", Name: "app", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	})

	result, _, err := handleDevelopBriefing(context.Background(), engine, mock, "proj1",
		WorkflowInput{Intent: "deploy notes API", Scope: []string{"app"}}, runtime.Info{InContainer: false})
	if err != nil {
		t.Fatalf("handleDevelopBriefing: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error:\n%s", extractText(result))
	}
	ws, _ := workflow.CurrentWorkSession(dir)
	if ws == nil {
		t.Fatalf("expected work session created")
	}
	if len(ws.Services) != 1 || ws.Services[0] != "app" {
		t.Errorf("expected scope=[app], got %v", ws.Services)
	}
}

// TestDevelopRoles_Validation pins the RC-B outOfScope validation contract.
func TestDevelopRoles_Validation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		scope      []string
		outOfScope []string
		wantErr    bool
		wantRole   map[string]string
	}{
		{name: "none → nil roles", scope: []string{"appdev", "appstage"}, outOfScope: nil, wantRole: nil},
		{name: "stage out-of-scope", scope: []string{"appdev", "appstage"}, outOfScope: []string{"appstage"}, wantRole: map[string]string{"appstage": workflow.RoleOutOfScope}},
		{name: "outOfScope not in scope → error", scope: []string{"appdev"}, outOfScope: []string{"appstage"}, wantErr: true},
		{name: "all out-of-scope → error (none required)", scope: []string{"appdev"}, outOfScope: []string{"appdev"}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			roles, err := developRoles(tc.scope, tc.outOfScope)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got roles=%v", roles)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(roles) != len(tc.wantRole) {
				t.Fatalf("roles=%v, want %v", roles, tc.wantRole)
			}
			for h, r := range tc.wantRole {
				if roles[h] != r {
					t.Fatalf("roles[%q]=%q, want %q", h, roles[h], r)
				}
			}
		})
	}
}

// TestHandleDevelopBriefing_StandardPair_OutOfScopeStage pins RC-B end-to-end at
// the handler: "leave staging as it is" → outOfScope=["appstage"] records the
// role so the stage half no longer blocks auto-close.
func TestHandleDevelopBriefing_StandardPair_OutOfScopeStage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)

	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		StageHostname:    "appstage",
		Mode:             topology.PlanModeStandard,
		BootstrapSession: "sess1",
		BootstrappedAt:   "2026-06-01",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-appdev", Name: "appdev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "php-nginx@8.4"}},
		{ID: "svc-appstage", Name: "appstage", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "php-nginx@8.4"}},
	})

	result, _, err := handleDevelopBriefing(context.Background(), engine, mock, "proj1",
		WorkflowInput{Intent: "redesign dev homepage, leave staging", Scope: []string{"appdev"}, OutOfScope: []string{"appstage"}},
		runtime.Info{InContainer: true})
	if err != nil {
		t.Fatalf("handleDevelopBriefing: %v", err)
	}
	if result.IsError {
		t.Fatalf("scope=[appdev] outOfScope=[appstage] must be accepted — got error:\n%s", extractText(result))
	}
	t.Cleanup(func() { _ = workflow.DeleteWorkSession(dir, os.Getpid()) })

	ws, _ := workflow.CurrentWorkSession(dir)
	if ws == nil {
		t.Fatal("work session expected")
	}
	// Stage still declared (visible) but role out-of-scope.
	if len(ws.Services) != 2 {
		t.Fatalf("expected appstage still declared in scope, got %v", ws.Services)
	}
	if workflow.RoleFor(ws, "appstage") != workflow.RoleOutOfScope {
		t.Fatalf("appstage role = %q, want out-of-scope", workflow.RoleFor(ws, "appstage"))
	}
	if workflow.RoleFor(ws, "appdev") != workflow.RoleRequired {
		t.Fatalf("appdev role = %q, want required", workflow.RoleFor(ws, "appdev"))
	}
	req := workflow.RequiredServices(ws)
	if len(req) != 1 || req[0] != "appdev" {
		t.Fatalf("RequiredServices = %v, want [appdev]", req)
	}
}
