package tools

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/workflow"
)

// C-10 (2026-04-20 shape flip): the v8.96 Theme A verbose diagnostic
// surface is gone. `ReadSurface`/`Required`/`Actual`/`CoupledWith`/
// `HowToFix`/`PerturbsChecks` + `NextRoundPrediction` do not exist on
// the wire. The P1 contract is: if you want to know whether a check
// would pass, run `PreAttestCmd` and compare exit codes. `Detail` is a
// one-line summary for human consumption; it must still be populated
// on failure so the agent has context before running the shim.

// TestC10Payload_FailRowsCarryDetail pins the minimum invariant of the
// post-C-10 payload: every failing StepCheck emits a non-empty Detail.
// Authors need SOMETHING in the detail to orient the agent before
// running the pre-attest command; an empty-Detail fail row would make
// the agent read the check name alone with no context.
func TestC10Payload_FailRowsCarryDetail(t *testing.T) {
	t.Parallel()

	// Reuse the engineered comment_ratio failure fixture — the v31
	// deploy-readmes offender is the canonical "shape change touched
	// this" surface. Any fail row with empty Detail is a regression.
	readme := "<!-- #ZEROPS_EXTRACT_START:integration-guide# -->\n\n" +
		"```yaml\n" +
		"zerops:\n" +
		"  - setup: dev\n" +
		"    run:\n" +
		"      start: npm run start:prod\n" +
		"```\n" +
		"<!-- #ZEROPS_EXTRACT_END:integration-guide# -->\n"
	checks := checkReadmeFragments(readme, "apidev")
	failCount := 0
	for _, c := range checks {
		if c.Status != "fail" {
			continue
		}
		failCount++
		if strings.TrimSpace(c.Detail) == "" {
			t.Errorf("fail row %q has empty Detail — C-10 contract requires a human-readable one-line summary", c.Name)
		}
	}
	if failCount == 0 {
		t.Fatal("fixture must drive at least one fail; no fail rows returned")
	}
}

// TestC10Payload_PreAttestCmdShape spot-checks the §18-migratable
// predicates that C-10 wired PreAttestCmd for: comment_depth, factual_claims,
// knowledge_base_authenticity. Each predicate's fail row must carry a
// `zcp check <name>` invocation that an author can run.
func TestC10Payload_PreAttestCmdShape(t *testing.T) {
	t.Parallel()

	// comment_depth fail → PreAttestCmd names the env folder. Need ≥3
	// substantive comment BLOCKS (contiguous # lines collapse to one
	// block) none carrying reasoning markers for the check to fire.
	commentDepth := checkCommentDepth(
		t.Context(),
		"# This names the project identifier section header\n"+
			"project:\n"+
			"  name: x\n"+
			"# This names the primary hostname section identifier\n"+
			"  - hostname: app\n"+
			"# This names the datastore hostname section identifier\n"+
			"  - hostname: db\n"+
			"# This names the aux hostname section identifier\n"+
			"  - hostname: aux\n",
		"0 \u2014 AI Agent_import",
	)
	assertPreAttestCmd(t, commentDepth, "comment_depth", "zcp check comment-depth --env=0 ")

	// factual_claims fail → PreAttestCmd names the env folder.
	factualClaims := checkFactualClaims(
		t.Context(),
		"services:\n  - hostname: app\n    type: bun@1\n    # minContainers: 5\n    minContainers: 2\n",
		"0 \u2014 AI Agent_import",
	)
	assertPreAttestCmd(t, factualClaims, "factual_claims", "zcp check factual-claims --env=0 ")

	// kb_authenticity fail → PreAttestCmd includes --hostname when provided.
	kb := checkKnowledgeBaseAuthenticity(
		t.Context(),
		"### Gotchas\n\n- **Stem A** — body\n- **Stem B** — body\n- **Stem C** — body\n",
		"apidev",
	)
	assertPreAttestCmd(t, kb, "knowledge_base_authenticity", "--hostname=apidev")
}

// assertPreAttestCmd verifies the expected substring appears in the
// PreAttestCmd of the first failing check whose name contains nameFrag.
func assertPreAttestCmd(t *testing.T, checks []workflow.StepCheck, nameFrag, wantCmdSubstr string) {
	t.Helper()
	for _, c := range checks {
		if c.Status != "fail" || !strings.Contains(c.Name, nameFrag) {
			continue
		}
		if !strings.Contains(c.PreAttestCmd, wantCmdSubstr) {
			t.Errorf("check %q: PreAttestCmd missing %q; got %q", c.Name, wantCmdSubstr, c.PreAttestCmd)
		}
		return
	}
	t.Fatalf("no fail row with name containing %q; got %+v", nameFrag, checks)
}
