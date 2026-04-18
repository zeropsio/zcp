// Tests for v8.90 Fix B — substep-scoped briefs delivered via substep-complete.
//
// Before v8.90: subagent-brief and readme-fragments were Eager: true in
// recipe_topic_registry.go, so InjectEagerTopics inlined both briefs into
// the deploy step-entry guide. The agent therefore had the briefs 30+
// minutes before dispatching the corresponding sub-agents; v25 dispatched
// both the feature sub-agent and the README writer without first calling
// complete substep, and the out-of-context briefs were stale by the time
// the main agent backfilled its substep attestations at the end of deploy.
//
// After v8.90: both topics are Eager: false. They land in the response to
// complete-substep-X only, where X is the substep whose completion advances
// the agent into the substep whose mapping RETURNS the brief. Specifically:
//
//  - complete substep=init-commands  → response carries subagent-brief
//    (because current advances to subagent, subStepToTopic(subagent)=subagent-brief)
//  - complete substep=feature-sweep-stage → response carries readme-fragments
//    (because current advances to readmes,  subStepToTopic(readmes)=readme-fragments)
//
// where-commands-run stays Eager: true because the SSH/zcp boundary and the
// zerops_dev_server tool discipline apply from the FIRST deploy substep
// (deploy-dev) onwards — not just at the substep whose mapping returns it.

package workflow

import (
	"strings"
	"testing"
)

// TestRecipeDeployTopics_EagerSet_v8_90 — exactly one deploy topic is eager:
// `where-commands-run`. `subagent-brief` and `readme-fragments` must be
// Eager: false. This is the structural guard against someone re-eagering a
// topic in a future commit.
func TestRecipeDeployTopics_EagerSet_v8_90(t *testing.T) {
	t.Parallel()

	eagerIDs := make(map[string]bool)
	for _, topic := range recipeDeployTopics {
		if topic.Eager {
			eagerIDs[topic.ID] = true
		}
	}

	wantEager := map[string]bool{
		"where-commands-run": true,
		// v8.94: mandatory fact recording is prompt-level pressure applied
		// from the first deploy substep through the last — Eager is the
		// correct delivery so every substep context carries the guidance.
		"fact-recording-mandatory": true,
	}
	wantNotEager := []string{
		"subagent-brief",
		"readme-fragments",
		// v8.94: the showcase `readmes` substep delivery topic.
		"content-authoring-brief",
	}

	for id := range wantEager {
		if !eagerIDs[id] {
			t.Errorf("topic %q must be Eager=true (v8.90 keeps this one eager)", id)
		}
	}
	for _, id := range wantNotEager {
		if eagerIDs[id] {
			t.Errorf("topic %q must NOT be Eager (v8.90 removed eager for substep-scoped briefs)", id)
		}
	}

	// Exact cardinality check: no other deploy topic should have sneaked in
	// as Eager without a recorded design rationale.
	if len(eagerIDs) != len(wantEager) {
		extra := []string{}
		for id := range eagerIDs {
			if !wantEager[id] {
				extra = append(extra, id)
			}
		}
		t.Errorf("unexpected eager deploy topics (v8.90 budget: only where-commands-run): %v", extra)
	}
}

// TestSubStepToTopic_DeploySubagent_ReturnsSubagentBrief — regression guard
// on the existing mapping. With Eager off, this mapping is the ONLY path
// by which the feature sub-agent brief reaches the agent.
func TestSubStepToTopic_DeploySubagent_ReturnsSubagentBrief(t *testing.T) {
	t.Parallel()
	showcase := fixtureForShape(ShapeDualRuntimeShowcase)
	got := subStepToTopic(RecipeStepDeploy, SubStepSubagent, showcase)
	if got != "subagent-brief" {
		t.Errorf("subStepToTopic(deploy, subagent, showcase) = %q, want %q", got, "subagent-brief")
	}
}

// TestSubStepToTopic_DeployReadmes_Delivery — regression guard on the
// substep mapping. v8.94: showcase recipes land on content-authoring-brief
// (surface contracts + classification + citation map); minimal/hello-world
// recipes still land on readme-fragments (marker-format reference only,
// no six-surface authoring scope on non-showcase tiers).
func TestSubStepToTopic_DeployReadmes_Delivery(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		shape     RecipeShape
		wantTopic string
	}{
		{"dual-runtime-showcase", ShapeDualRuntimeShowcase, "content-authoring-brief"},
		{"fullstack-showcase", ShapeFullStackShowcase, "content-authoring-brief"},
		{"backend-minimal", ShapeBackendMinimal, "readme-fragments"},
		{"hello-world", ShapeHelloWorld, "readme-fragments"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plan := fixtureForShape(tt.shape)
			got := subStepToTopic(RecipeStepDeploy, SubStepReadmes, plan)
			if got != tt.wantTopic {
				t.Errorf("subStepToTopic(deploy, readmes, %s) = %q, want %q", tt.name, got, tt.wantTopic)
			}
		})
	}
}

// TestInjectEagerTopics_Deploy_DoesNotInclineSubagentBrief — the post-v8.90
// step-entry guide composition must NOT contain the subagent brief's
// distinctive strings.
func TestInjectEagerTopics_Deploy_DoesNotInlineSubagentBrief(t *testing.T) {
	t.Parallel()
	plan := fixtureForShape(ShapeDualRuntimeShowcase)
	got := InjectEagerTopics(recipeDeployTopics, plan)

	// Distinctive phrases lifted from block "dev-deploy-subagent-brief" —
	// if any of these appear, the brief was inlined. Chosen to be unique to
	// that block (not shared with `where-commands-run`, which stays eager
	// and does mention "feature sub-agent" in passing).
	forbidden := []string{
		"Installed-package verification rule",
		"Contract-first rule",
		"Single author, not parallel authors",
		"UX quality contract",
		"Feature implementation rule",
	}
	for _, p := range forbidden {
		if strings.Contains(got, p) {
			t.Errorf("eager injection for deploy MUST NOT contain subagent-brief phrase %q — remove Eager from subagent-brief", p)
		}
	}
}

// TestInjectEagerTopics_Deploy_DoesNotIncludeReadmeFragments — same guard
// for the README-writer brief.
func TestInjectEagerTopics_Deploy_DoesNotIncludeReadmeFragments(t *testing.T) {
	t.Parallel()
	plan := fixtureForShape(ShapeDualRuntimeShowcase)
	got := InjectEagerTopics(recipeDeployTopics, plan)

	forbidden := []string{
		"#ZEROPS_EXTRACT_START:knowledge-base#",
		"#ZEROPS_EXTRACT_START:intro#",
		"#ZEROPS_EXTRACT_START:integration-guide#",
		"Per-codebase README with extract fragments",
	}
	for _, p := range forbidden {
		if strings.Contains(got, p) {
			t.Errorf("eager injection for deploy MUST NOT contain readme-fragments phrase %q — remove Eager from readme-fragments", p)
		}
	}
}

// TestInjectEagerTopics_Deploy_DoesNotIncludeContentAuthoringBrief — v8.94
// guard. The content-authoring brief is substep-scoped; it must NOT land in
// the deploy step-entry guide. Distinctive phrases taken from the block
// that would not appear in other deploy topics.
func TestInjectEagerTopics_Deploy_DoesNotIncludeContentAuthoringBrief(t *testing.T) {
	t.Parallel()
	plan := fixtureForShape(ShapeDualRuntimeShowcase)
	got := InjectEagerTopics(recipeDeployTopics, plan)

	forbidden := []string{
		"Classification taxonomy — apply BEFORE routing",
		"Citation map — MANDATORY guide consultation",
		"Counter-examples — wrong-surface / folk-doctrine patterns",
		"You are a content-authoring sub-agent",
	}
	for _, p := range forbidden {
		if strings.Contains(got, p) {
			t.Errorf("eager injection for deploy MUST NOT contain content-authoring-brief phrase %q — remove Eager from content-authoring-brief", p)
		}
	}
}

// TestInjectEagerTopics_Deploy_StillIncludesWhereCommandsRun — the one
// remaining eager deploy topic. The SSH-vs-zcp boundary must be in the
// step-entry guide for every deploy run.
func TestInjectEagerTopics_Deploy_StillIncludesWhereCommandsRun(t *testing.T) {
	t.Parallel()
	plan := fixtureForShape(ShapeDualRuntimeShowcase)
	got := InjectEagerTopics(recipeDeployTopics, plan)

	wants := []string{
		"where-commands-run", // topic ID appears in header
		"Where app-level commands run",
		"zerops_dev_server",
		"SSHFS network mount",
	}
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("eager injection for deploy MUST still contain where-commands-run phrase %q", w)
		}
	}
}

// TestSubStepGuide_InitCommandsResponse_ContainsSubagentBrief — integration:
// when the agent's current substep advances into `subagent` (which happens
// after calling complete substep=init-commands), buildSubStepGuide must
// return the byte-literal content of the subagent-brief block.
func TestSubStepGuide_InitCommandsResponse_ContainsSubagentBrief(t *testing.T) {
	t.Parallel()
	plan := fixtureForShape(ShapeDualRuntimeShowcase)
	rs := &RecipeState{Plan: plan, Tier: RecipeTierShowcase}

	// buildSubStepGuide is what feeds resp.Current.DetailedGuide when the
	// agent is in the named substep. Here we simulate "current substep =
	// subagent" — the state after complete substep=init-commands advances.
	got := rs.buildSubStepGuide(RecipeStepDeploy, SubStepSubagent)
	if got == "" {
		t.Fatal("expected non-empty sub-step guide for (deploy, subagent)")
	}
	if len(got) < 10*1024 {
		t.Errorf("subagent-brief guide is only %d bytes, expected >= 10 KB (v25's was 14 KB)", len(got))
	}

	wants := []string{
		"Installed-package verification rule",
		"Contract-first rule",
		"feature sub-agent",
	}
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("subagent-brief guide missing %q", w)
		}
	}
}

// TestDeploySkeleton_ContainsSubstepDisciplineNote — Fix D content guard:
// the deploy step-entry must teach substep-complete discipline loudly,
// names the two load-bearing brief deliveries, and calls out the v25
// anti-pattern by name so a future agent reading the guide recognises
// the failure mode.
func TestDeploySkeleton_ContainsSubstepDisciplineNote(t *testing.T) {
	t.Parallel()
	plan := fixtureForShape(ShapeDualRuntimeShowcase)
	got := resolveRecipeGuidance(RecipeStepDeploy, RecipeTierShowcase, plan)
	if got == "" {
		t.Fatal("expected non-empty deploy step-entry guide")
	}

	wants := []string{
		"Substep-Complete is load-bearing",
		"init-commands",
		"feature-sweep-stage",
		"Anti-pattern",
		"backfill",
		"Correct pattern",
	}
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("deploy step-entry guide missing discipline teaching %q", w)
		}
	}
}

// TestDeploySkeleton_PointsAtSubstepDeliveredBriefs — Fix B/D content guard:
// the execution-order list for the two delegation substeps must tell the
// agent that the brief arrives in the complete-substep response, NOT that
// it is inlined in the step guide.
func TestDeploySkeleton_PointsAtSubstepDeliveredBriefs(t *testing.T) {
	t.Parallel()
	plan := fixtureForShape(ShapeDualRuntimeShowcase)
	got := resolveRecipeGuidance(RecipeStepDeploy, RecipeTierShowcase, plan)

	wants := []string{
		"delivers the feature-sub-agent brief",
		"delivers the README-writer brief",
		"Do NOT dispatch the feature sub-agent until you have received that response",
		"Do NOT dispatch the README writer sub-agent until you have received that response",
	}
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("deploy step-entry guide missing pointer %q", w)
		}
	}
}

// TestDeploySkeleton_ContainsFactRecordingMandatory — v8.94. The deploy
// step-entry guide must carry the mandatory-fact-recording block so every
// substep the main agent reaches has the prompt-level pressure to call
// zerops_record_fact at the moment of freshest knowledge. The authoring
// sub-agent at readmes substep depends on the facts log as its primary
// input, not on the run transcript.
func TestDeploySkeleton_ContainsFactRecordingMandatory(t *testing.T) {
	t.Parallel()
	plan := fixtureForShape(ShapeDualRuntimeShowcase)
	got := resolveRecipeGuidance(RecipeStepDeploy, RecipeTierShowcase, plan)
	if got == "" {
		t.Fatal("expected non-empty deploy step-entry guide")
	}

	wants := []string{
		"Fact recording — MANDATORY during deploy",
		"zerops_record_fact",
		"gotcha_candidate",
		"ig_item_candidate",
		"verified_behavior",
		"platform_observation",
		"fix_applied",
		"cross_codebase_contract",
		"DO NOT write content during deploy",
	}
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("deploy step-entry guide missing mandatory-fact phrase %q", w)
		}
	}
}

// TestSubStepGuide_FeatureSweepStageResponse_ContainsContentAuthoringBrief —
// integration: when current substep advances to `readmes` on a showcase plan
// (after complete substep=feature-sweep-stage), buildSubStepGuide must return
// the byte-literal content of the content-authoring-brief block. v8.94 moved
// the showcase delivery from readme-fragments (which stays as the marker-
// format reference) to the new brief with surface contracts + classification
// taxonomy + citation map.
func TestSubStepGuide_FeatureSweepStageResponse_ContainsContentAuthoringBrief(t *testing.T) {
	t.Parallel()
	plan := fixtureForShape(ShapeDualRuntimeShowcase)
	rs := &RecipeState{Plan: plan, Tier: RecipeTierShowcase}

	got := rs.buildSubStepGuide(RecipeStepDeploy, SubStepReadmes)
	if got == "" {
		t.Fatal("expected non-empty sub-step guide for (deploy, readmes)")
	}
	if len(got) < 8*1024 {
		t.Errorf("content-authoring-brief guide is only %d bytes, expected >= 8 KB", len(got))
	}

	wants := []string{
		"You are a content-authoring sub-agent",
		"Classification taxonomy",
		"Citation map",
		"Counter-examples",
		"Self-review checklist",
		"env-comment-set",
	}
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("content-authoring-brief guide missing %q", w)
		}
	}
}
