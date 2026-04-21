// Tests for: Cx-GUIDANCE-TOPIC-REGISTRY — unknown-topic suggestions,
// zero-byte guard, session-briefing topic list.

package tools

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestGuidanceTool_UnknownTopic_ReturnsNearestMatches verifies that the
// guidance tool, when given a typo'd topic ID, returns an error message
// that names the top-3 nearest-match topic IDs from the registry. This
// short-circuits the v35 hallucination loop where the main agent guessed
// plausible-sounding IDs and got bare "unknown guidance topic" responses.
func TestGuidanceTool_UnknownTopic_ReturnsNearestMatches(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	RegisterGuidance(srv, engine)

	// Pick a known topic, mutate one letter — the mutated form should
	// be an unknown topic whose nearest match is the original.
	ids := workflow.AllTopicIDs()
	if len(ids) == 0 {
		t.Skip("no registered topics")
	}
	target := ids[0]
	if len(target) < 3 {
		t.Skipf("target topic %q too short to mutate", target)
	}
	typo := target[:len(target)/2] + target[len(target)/2+1:]

	result := callTool(t, srv, "zerops_guidance", map[string]any{"topic": typo})
	text := getTextContent(t, result)

	if !strings.Contains(text, "unknown guidance topic") {
		t.Errorf("expected 'unknown guidance topic' prefix; got: %s", text)
	}
	if !strings.Contains(text, "Did you mean") {
		t.Errorf("expected 'Did you mean' suggestion prompt; got: %s", text)
	}
	if !strings.Contains(text, target) {
		t.Errorf("expected suggestions to include the original topic %q; got: %s", target, text)
	}
	if !strings.Contains(text, "guidanceTopicIds") {
		t.Errorf("expected pointer to the recipe-start closed-universe field; got: %s", text)
	}
}

// TestGuidanceTool_UnknownTopic_WithEmptyQuery_HandlesGracefully guards
// against panics in the nearest-match path when the input is empty or
// degenerate. Empty topic is already rejected upstream; here we verify
// a short nonsense query behaves.
func TestGuidanceTool_UnknownTopic_WithEmptyQuery_HandlesGracefully(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	engine := workflow.NewEngine(t.TempDir(), workflow.EnvLocal, nil)
	RegisterGuidance(srv, engine)

	result := callTool(t, srv, "zerops_guidance", map[string]any{"topic": "z"})
	text := getTextContent(t, result)
	if !strings.Contains(text, "unknown guidance topic") {
		t.Errorf("expected 'unknown guidance topic' for nonsense query; got: %s", text)
	}
	if !strings.Contains(text, "Did you mean") {
		t.Errorf("expected 'Did you mean' line even for degenerate query; got: %s", text)
	}
}

// TestGuidanceTool_NilPlan_TierOnlyTopic_Resolves verifies that during
// the research step (before the agent submits recipePlan) a topic whose
// predicate gates only on tier resolves against a synthetic tier-only
// plan derived from state.Recipe.Tier. v36 F-7 regression: at research
// step with tier=showcase, showcase-gated topics (showcase-service-keys,
// dashboard-skeleton, recipe-types) returned a misleading "does not
// apply to your recipe shape" message because predicates ran against a
// nil plan and returned false. The engine already knows the tier from
// action=start — predicate evaluation must use it.
func TestGuidanceTool_NilPlan_TierOnlyTopic_Resolves(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvLocal, nil)
	if _, err := engine.Start("proj-nilplan-tier", workflow.WorkflowRecipe, "nil-plan tier test"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	state, err := engine.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	state.Recipe = workflow.NewRecipeState()
	state.Recipe.Tier = workflow.RecipeTierShowcase
	// Plan stays nil — research-complete has not been called yet.
	if err := workflow.SaveSessionState(dir, engine.SessionID(), state); err != nil {
		t.Fatalf("SaveSessionState: %v", err)
	}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterGuidance(srv, engine)

	// showcase-service-keys is gated on isShowcase only (tier).
	result := callTool(t, srv, "zerops_guidance", map[string]any{"topic": "showcase-service-keys"})
	text := getTextContent(t, result)
	if strings.Contains(text, "does not apply") {
		t.Errorf("tier-only topic incorrectly gated at research step (nil plan, tier=showcase): %s", text)
	}
	if text == "" {
		t.Fatal("expected resolved content for showcase-gated topic at research step; got empty")
	}
}

// TestGuidanceTool_NilPlan_ShapeTopic_ReturnsPlanNotSubmitted verifies
// that when a topic requires plan-shape information (targets/framework)
// and the plan hasn't been submitted yet, the response distinguishes
// "plan not yet submitted" from the pre-existing "does not apply"
// message. The agent then knows to fetch the topic after
// action=complete step=research instead of treating the topic as
// permanently inapplicable.
func TestGuidanceTool_NilPlan_ShapeTopic_ReturnsPlanNotSubmitted(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvLocal, nil)
	if _, err := engine.Start("proj-nilplan-shape", workflow.WorkflowRecipe, "nil-plan shape test"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	state, err := engine.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	state.Recipe = workflow.NewRecipeState()
	state.Recipe.Tier = workflow.RecipeTierShowcase
	if err := workflow.SaveSessionState(dir, engine.SessionID(), state); err != nil {
		t.Fatalf("SaveSessionState: %v", err)
	}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterGuidance(srv, engine)

	// worker-setup is gated on hasWorker — needs plan.Targets.
	result := callTool(t, srv, "zerops_guidance", map[string]any{"topic": "worker-setup"})
	text := getTextContent(t, result)
	if strings.Contains(text, "does not apply to your recipe shape") {
		t.Errorf("shape-dependent topic returned stale 'does not apply' instead of plan-not-submitted message: %s", text)
	}
	if !strings.Contains(text, "recipePlan") {
		t.Errorf("expected plan-not-submitted message naming recipePlan; got: %s", text)
	}
	if !strings.Contains(text, "research") {
		t.Errorf("expected plan-not-submitted message to point at the research step; got: %s", text)
	}
}

// TestGuidanceTool_ValidTopic_WithPredicateFilter_ReturnsDoesNotApply —
// predicate-filtered empty is still a legitimate "topic doesn't apply"
// response. The TOPIC_EMPTY guard only fires when a topic's predicate
// matches the plan but the resolved content is zero bytes (server-side
// block-missing bug). Predicate-filtered empty must NOT surface as an
// error.
//
// Picks a topic with a predicate + a plan the predicate rejects.
func TestGuidanceTool_ValidTopic_WithPredicateFilter_ReturnsDoesNotApply(t *testing.T) {
	t.Parallel()

	// Find a topic with a non-nil predicate.
	var filtered *workflow.GuidanceTopic
	for _, id := range workflow.AllTopicIDs() {
		topic := workflow.LookupTopic(id)
		if topic != nil && topic.Predicate != nil {
			filtered = topic
			break
		}
	}
	if filtered == nil {
		t.Skip("no topic with predicate to exercise filter path")
	}

	// Build minimal + showcase plans inline so this test doesn't reach
	// into the workflow package's internal fixture helpers. Try both —
	// we just need a plan the predicate rejects.
	candidates := []*workflow.RecipePlan{
		{Tier: workflow.RecipeTierMinimal, Framework: "bun", Slug: "min"},
		{Tier: workflow.RecipeTierShowcase, Framework: "bun", Slug: "show"},
	}
	var rejected *workflow.RecipePlan
	for _, p := range candidates {
		if !filtered.Predicate(p) {
			rejected = p
			break
		}
	}
	if rejected == nil {
		t.Skipf("predicate on topic %q accepts every minimal+showcase candidate", filtered.ID)
	}

	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvLocal, nil)
	if _, err := engine.Start("proj-guidance", workflow.WorkflowRecipe, "guidance test"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	state, err := engine.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	state.Recipe = workflow.NewRecipeState()
	state.Recipe.Plan = rejected
	if err := workflow.SaveSessionState(dir, engine.SessionID(), state); err != nil {
		t.Fatalf("SaveSessionState: %v", err)
	}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterGuidance(srv, engine)

	result := callTool(t, srv, "zerops_guidance", map[string]any{"topic": filtered.ID})
	text := getTextContent(t, result)
	// Must NOT surface TOPIC_EMPTY for a predicate-filtered empty.
	if strings.Contains(text, "TOPIC_EMPTY") {
		t.Errorf("predicate-filtered empty must not surface as TOPIC_EMPTY error; got: %s", text)
	}
	// Should surface the benign "does not apply" message.
	if !strings.Contains(text, "does not apply") {
		t.Errorf("expected 'does not apply' message for predicate-filtered topic; got: %s", text)
	}
}
