// Tests for: tools/hq_recipe_inputs.go, the recipe reconcile and
// tools/hq_recipe_required.go — a tier never ships a runtime whose setup
// reads a service the tier does not carry, and while the composer refuses
// for it the refusal is a required step of the Mate's setup.
package tools

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/workflow"
)

// The composer can tell a service the Mate's project runs from a name no
// service has only when the reader hands it every service the project runs
// — the ones the recipe carries and the ones it leaves out alike: a runtime
// nothing wired, a Swarm pool, a Box, zcp itself.
func TestComposeGroupRecipeInputs_ProjectHostnames_EveryServiceTheProjectRuns(t *testing.T) {
	stateDir := t.TempDir()
	writeHQWiredPairMeta(t, stateDir)
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil {
		t.Fatalf("ListServiceMetas: %v", err)
	}
	pool := liveRuntime("svc-pool", "devpool", "swarm@1")
	inputs, _, err := composeGroupRecipeInputs(context.Background(), medusaLikeProject(pool, liveBox("svc-box", "box")),
		"p1", "acme", t.TempDir(), testHQAddress, testHQOrg, labApp, metas, metas, nil)
	if err != nil {
		t.Fatalf("composeGroupRecipeInputs: %v", err)
	}
	want := []string{"appdev", "appstage", "box", "db", "devpool", "mailpit", "orphan", "search", "storage", "zcp"}
	got := slices.Sorted(slices.Values(inputs.ProjectHostnames))
	if !slices.Equal(got, want) {
		t.Errorf("ProjectHostnames = %v, want %v", got, want)
	}
}

// Through the reconcile: a stage setup that reads the Mate's mail catcher,
// which a stage and a production never run, refuses the recipe — nothing is
// proposed, and the pass's line itself names the tier, the setup, the
// variable and the service, rather than a warning among the others.
func TestGroupRecipe_TierClosure_NothingProposedAndTheLineNamesTheRead(t *testing.T) {
	t.Parallel()
	lab := newClosureLab(t)

	outcome := lab.proposeRecipe(lab.mock)

	if outcome.Change != 0 || lab.hq.findOf(labMate, labApp, hq.RecipeRepo) != nil {
		t.Errorf("outcome %+v: want nothing proposed while a tier lacks what its runtime reads", outcome)
	}
	for _, want := range []string{"does not compose", `tier "Stage"`, `tier "Small Production"`, `setup "prod"`, "run.envVariables.MAIL_URL", `"mailpit"`} {
		if !strings.Contains(outcome.Line, want) {
			t.Errorf("line does not say %s:\n%s", want, outcome.Line)
		}
	}
	if strings.Contains(outcome.Line, `tier "AI Agent"`) {
		t.Errorf("line names the AI Agent tier, which carries the mail catcher:\n%s", outcome.Line)
	}
}

// closureReadsMail is the pair's zerops.yaml whose stage setup reads the
// Mate's mail catcher, a utility only the AI Agent tier carries; closureFixed
// is the same setup reading it no more.
const (
	closureReadsMail = "zerops:\n" +
		"  - setup: api\n    run:\n      base: nodejs@22\n      envVariables:\n        DB_PORT: ${db_port}\n" +
		"  - setup: prod\n    run:\n      base: nodejs@22\n      envVariables:\n        DB_PORT: ${db_port}\n        MAIL_URL: smtp://mailpit:1025\n"
	closureFixed = "zerops:\n" +
		"  - setup: api\n    run:\n      base: nodejs@22\n      envVariables:\n        DB_PORT: ${db_port}\n" +
		"  - setup: prod\n    run:\n      base: nodejs@22\n      envVariables:\n        DB_PORT: ${db_port}\n"
)

// newClosureLab is the lab's pair wired beside the Mate's mail catcher, its
// stage recorded building prod, whose setup on main of the pair's repository
// reads the mail catcher: every tier but the AI Agent's refuses the recipe.
func newClosureLab(t *testing.T) *hqLab {
	t.Helper()
	lab := newHQLab(t)
	lab.mock = recipeReconcileClient(publicBuildRuntime("svc-mailpit", "mailpit", "alpine@3.21", "https://github.com/zerops-recipe-apps/mailpit-app"))
	lab.ssh.mock = lab.mock
	lab.wire()
	if err := workflow.UpdateServiceMeta(lab.stateDir, "appdev", func(m *workflow.ServiceMeta) error {
		m.PrimarySetupName = "api"
		m.SetStageSetup(m.FirstStage().Hostname, "prod")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	lab.hq.landOnMain("appdev", map[string]string{"zerops.yaml": closureReadsMail})
	return lab
}

// closureRefusalFrom is the composer's refusal as a pass's line carries it:
// "the group recipe does not compose yet (<refusal>)."
func closureRefusalFrom(t *testing.T, lines ...string) string {
	t.Helper()
	const head = "the group recipe does not compose yet ("
	for _, line := range lines {
		if _, refusal, found := strings.Cut(line, head); found {
			return strings.TrimSuffix(refusal, ").")
		}
	}
	t.Fatalf("no line carries the composer's refusal: %q", lines)
	return ""
}

// closureStepOf is the step among required that says the closure refusal —
// the read of the mail catcher and the way out — "" for none.
func closureStepOf(required []string) string {
	for _, step := range required {
		if strings.Contains(step, "run.envVariables.MAIL_URL") || strings.Contains(step, "stop the setup reading it") {
			return step
		}
	}
	return ""
}

// recipeStatusText is what the agent reads in status for the lab's Mate.
func recipeStatusText(t *testing.T, lab *hqLab) string {
	t.Helper()
	eng := workflow.NewEngine(lab.stateDir, workflow.EnvContainer, nil)
	result, _, err := handleLifecycleStatus(t.Context(), eng, lab.mock, lab.rt.ProjectID, lab.rt, nil, nil)
	if err != nil {
		t.Fatalf("handleLifecycleStatus: %v", err)
	}
	return getTextContent(t, result)
}

// While the composer refuses the recipe because a tier lacks what its
// runtime reads, the group recipe stays stale: a second Mate, the group's
// stage and its production are built from tiers that no longer describe the
// project. So the refusal is a required step of the Mate's setup — on the
// bootstrap response, beside a develop start's result and at the head of
// status — in the composer's own words: each tier, setup, variable and
// service, and the way out (carry the service in that tier, or stop the
// setup reading it), with the call that tries again.
func TestGroupRecipe_TierClosure_RefusalIsARequiredStep(t *testing.T) {
	t.Parallel()
	lab := newClosureLab(t)

	report := reconcileMateRepositories(t.Context(), lab.mock, lab.hq.srv.Client(), lab.ssh, nil, lab.rt, lab.rt.ProjectID, lab.stateDir, hqPassBackedOff)

	step := closureStepOf(report.required)
	if step == "" {
		t.Fatalf("required = %v, want the closure refusal's step (lines %v)", report.required, report.lines)
	}
	for _, want := range []string{`tier "Stage"`, `tier "Small Production"`, `setup "prod"`, "run.envVariables.MAIL_URL", `"mailpit"`,
		"Carry the service in that tier", "stop the setup reading it", `action="group-recipe"`} {
		if !strings.Contains(step, want) {
			t.Errorf("the step does not say %s:\n%s", want, step)
		}
	}
	if refusal := closureRefusalFrom(t, report.lines...); !strings.Contains(step, refusal) {
		t.Errorf("the step words the refusal otherwise than the composer:\nstep:    %s\nrefusal: %s", step, refusal)
	}
	if strings.Contains(step, `tier "AI Agent"`) {
		t.Errorf("the step names the AI Agent tier, which carries the mail catcher:\n%s", step)
	}
	resp := &workflow.BootstrapResponse{Message: "Bootstrap complete."}
	appendRepositoryReport(resp, report)
	if closureStepOf(resp.Required) != step || !strings.Contains(resp.Message, "REQUIRED") {
		t.Errorf("bootstrap response = required %v, message %q; want the step required and said", resp.Required, resp.Message)
	}
	if got := readRecipeRequired(lab.stateDir); closureStepOf(got) != step {
		t.Errorf("the steps status reads = %v, want %q among them", got, step)
	}
	if text := recipeStatusText(t, lab); !strings.Contains(text, "→ REQUIRED: "+step) {
		t.Errorf("status does not head with the step:\n%s", text)
	}
}

// The step is the last pass's verdict, not a mark: once the setup stops
// reading the service, the next pass composes the recipe and proposes it,
// and the step is gone — from the pass's report, zcp's record and status.
func TestGroupRecipe_TierClosure_StepGoneOnceItComposes(t *testing.T) {
	t.Parallel()
	lab := newClosureLab(t)
	lab.proposeRecipe(lab.mock)
	if closureStepOf(readRecipeRequired(lab.stateDir)) == "" {
		t.Fatalf("the refusal is no required step: %v", readRecipeRequired(lab.stateDir))
	}

	lab.hq.landOnMain("appdev", map[string]string{"zerops.yaml": closureFixed})
	report := reconcileMateRepositories(t.Context(), lab.mock, lab.hq.srv.Client(), lab.ssh, nil, lab.rt, lab.rt.ProjectID, lab.stateDir, hqPassBackedOff)

	if len(report.required) != 0 {
		t.Errorf("required = %v once the recipe composes, want none (lines %v)", report.required, report.lines)
	}
	if got := readRecipeRequired(lab.stateDir); len(got) != 0 {
		t.Errorf("status still reads a required step: %v", got)
	}
	if text := recipeStatusText(t, lab); strings.Contains(text, "REQUIRED") {
		t.Errorf("status still says REQUIRED once the recipe composes:\n%s", text)
	}
	if lab.hq.openCount(hq.RecipeRepo) == 0 {
		t.Errorf("no recipe proposed once it composes (lines %v)", report.lines)
	}
}

// Only the closure refusal is this step: a pass whose compose fails for
// anything else — here a stage setup no record names and the zerops.yaml
// does not tell — carries that refusal's own step, and the closure step a
// pass before it recorded is gone, never left standing beside it.
func TestGroupRecipe_NonClosureComposeError_NoClosureStep(t *testing.T) {
	t.Parallel()
	lab := newClosureLab(t)
	lab.proposeRecipe(lab.mock)
	if closureStepOf(readRecipeRequired(lab.stateDir)) == "" {
		t.Fatalf("the refusal is no required step: %v", readRecipeRequired(lab.stateDir))
	}

	if err := workflow.UpdateServiceMeta(lab.stateDir, "appdev", func(m *workflow.ServiceMeta) error {
		m.SetStageSetup(m.FirstStage().Hostname, "")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	lab.hq.landOnMain("appdev", map[string]string{"zerops.yaml": closureReadsMail +
		"  - setup: preview\n    run:\n      base: nodejs@22\n"})
	report := reconcileMateRepositories(t.Context(), lab.mock, lab.hq.srv.Client(), lab.ssh, nil, lab.rt, lab.rt.ProjectID, lab.stateDir, hqPassBackedOff)

	if step := closureStepOf(report.required); step != "" {
		t.Errorf("a compose that failed for another reason carries the closure step:\n%s", step)
	}
	if len(report.required) != 1 || !strings.Contains(report.required[0], `action="set-default-setup"`) {
		t.Errorf("required = %v, want the unrecorded stage setup's step alone (lines %v)", report.required, report.lines)
	}
	if got := readRecipeRequired(lab.stateDir); closureStepOf(got) != "" || len(got) != len(report.required) {
		t.Errorf("the steps status reads = %v, want %v", got, report.required)
	}
}

// A pass that finds nothing left to compose — the pair is gone — can never
// check the refusal again, so it leaves no step behind: the recipe of a
// project with no pair requires nothing.
func TestGroupRecipe_TierClosure_NothingLeftToCompose_StepGone(t *testing.T) {
	t.Parallel()
	lab := newClosureLab(t)
	lab.proposeRecipe(lab.mock)
	if closureStepOf(readRecipeRequired(lab.stateDir)) == "" {
		t.Fatalf("the refusal is no required step: %v", readRecipeRequired(lab.stateDir))
	}

	if err := workflow.DeleteServiceMeta(lab.stateDir, "appdev"); err != nil {
		t.Fatal(err)
	}
	lab.proposeRecipe(lab.mock)

	if got := readRecipeRequired(lab.stateDir); len(got) != 0 {
		t.Errorf("status still reads a required step with no pair left: %v", got)
	}
}
