package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/recipe"
)

func TestRecordFact_AppendsToSessionLog(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_ = ctx
	engine := testEngine(t)

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterRecordFact(srv, engine, nil)

	result := callTool(t, srv, "zerops_record_fact", map[string]any{
		"type":        ops.FactTypeGotchaCandidate,
		"title":       "module: nodenext + raw ts-node",
		"substep":     "deploy.deploy-dev",
		"codebase":    "workerdev",
		"mechanism":   "ts-node against module: nodenext",
		"failureMode": "Cannot find module",
		"fixApplied":  "Flip tsconfig to commonjs",
		"evidence":    "deploy log line 12:35",
	})
	if result.IsError {
		t.Fatalf("tool returned error: %s", getTextContent(t, result))
	}

	path := ops.FactLogPath(engine.SessionID())
	got, err := ops.ReadFacts(path)
	if err != nil {
		t.Fatalf("read facts: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 record, got %d", len(got))
	}
	if got[0].Title != "module: nodenext + raw ts-node" {
		t.Errorf("title: %q", got[0].Title)
	}
	if got[0].Codebase != "workerdev" {
		t.Errorf("codebase: %q", got[0].Codebase)
	}
}

func TestRecordFact_RejectsUnknownType(t *testing.T) {
	t.Parallel()
	engine := testEngine(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterRecordFact(srv, engine, nil)

	result := callTool(t, srv, "zerops_record_fact", map[string]any{
		"type":  "wrong_kind",
		"title": "x",
	})
	text := getTextContent(t, result)
	if !strings.Contains(text, "unknown") {
		t.Errorf("expected unknown-type error, got: %s", text)
	}
}

func TestRecordFact_RequiresActiveSession(t *testing.T) {
	t.Parallel()
	engine := testEngine(t)
	if err := engine.Reset(); err != nil {
		t.Fatalf("reset: %v", err)
	}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterRecordFact(srv, engine, nil)

	result := callTool(t, srv, "zerops_record_fact", map[string]any{
		"type":  ops.FactTypeGotchaCandidate,
		"title": "test",
	})
	text := getTextContent(t, result)
	if !strings.Contains(strings.ToLower(text), "session") {
		t.Errorf("expected session error, got: %s", text)
	}
}

// TestRecordFact_NudgeOnMissingRouteTo — v39 Commit 4. When the caller
// records a fact without passing routeTo, the response includes a nudge
// naming the inferred default route so the caller can confirm or
// override. Not a refusal; the fact is still appended.
func TestRecordFact_NudgeOnMissingRouteTo(t *testing.T) {
	t.Parallel()
	engine := testEngine(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterRecordFact(srv, engine, nil)

	result := callTool(t, srv, "zerops_record_fact", map[string]any{
		"type":  ops.FactTypeGotchaCandidate,
		"title": "execOnce silently no-ops when migration command exits 0 without SQL",
	})
	if result.IsError {
		t.Fatalf("nudge must not turn into refusal; got error result: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if !strings.Contains(strings.ToLower(text), "nudge") {
		t.Errorf("expected nudge-prefixed message, got: %s", text)
	}
	if !strings.Contains(text, ops.FactRouteToContentGotcha) {
		t.Errorf("expected inferred route %q in nudge, got: %s", ops.FactRouteToContentGotcha, text)
	}

	// Fact must still be persisted — nudge is advisory.
	facts, err := ops.ReadFacts(ops.FactLogPath(engine.SessionID()))
	if err != nil {
		t.Fatalf("read facts: %v", err)
	}
	if len(facts) != 1 {
		t.Fatalf("want 1 fact recorded, got %d", len(facts))
	}
	if facts[0].RouteTo != "" {
		t.Errorf("persisted RouteTo should remain empty when caller didn't set it, got %q", facts[0].RouteTo)
	}
}

// TestRecordFact_RouteToPassthrough — when the caller supplies routeTo
// explicitly, it's persisted on the FactRecord and no nudge appears.
func TestRecordFact_RouteToPassthrough(t *testing.T) {
	t.Parallel()
	engine := testEngine(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterRecordFact(srv, engine, nil)

	result := callTool(t, srv, "zerops_record_fact", map[string]any{
		"type":    ops.FactTypeIGItemCandidate,
		"title":   "bind 0.0.0.0 for L7 balancer reachability",
		"routeTo": ops.FactRouteToContentIG,
	})
	if result.IsError {
		t.Fatalf("tool returned error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if strings.Contains(strings.ToLower(text), "nudge") {
		t.Errorf("nudge should not fire when routeTo is set; got: %s", text)
	}

	facts, err := ops.ReadFacts(ops.FactLogPath(engine.SessionID()))
	if err != nil {
		t.Fatalf("read facts: %v", err)
	}
	if len(facts) != 1 {
		t.Fatalf("want 1 fact recorded, got %d", len(facts))
	}
	if facts[0].RouteTo != ops.FactRouteToContentIG {
		t.Errorf("persisted RouteTo = %q, want %q", facts[0].RouteTo, ops.FactRouteToContentIG)
	}
}

// TestRecordFact_RoutesToRecipeSession — when the v2 workflow engine has no
// active session but a v3 recipe session is open, zerops_record_fact must
// append into the recipe's legacy-facts.jsonl rather than erroring. Exercises
// the Workstream E deferred gate plumbing from run-8-readiness.
func TestRecordFact_RoutesToRecipeSession(t *testing.T) {
	t.Parallel()
	engine := testEngine(t)
	if err := engine.Reset(); err != nil {
		t.Fatalf("reset engine: %v", err)
	}

	// Open a recipe session pointed at a per-test outputRoot — the probe
	// will resolve CurrentSingleSession to this one and write into
	// <outputRoot>/legacy-facts.jsonl.
	store := recipe.NewStore(t.TempDir())
	outputRoot := filepath.Join(t.TempDir(), "recipe-run")
	if _, err := store.OpenOrCreate("alpha-showcase", outputRoot); err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterRecordFact(srv, engine, store)

	result := callTool(t, srv, "zerops_record_fact", map[string]any{
		"type":      ops.FactTypeGotchaCandidate,
		"title":     "execOnce burned on mid-seed crash",
		"substep":   "feature.seed",
		"codebase":  "apidev",
		"mechanism": "zsc execOnce + per-deploy appVersionId",
		"routeTo":   ops.FactRouteToContentGotcha,
	})
	if result.IsError {
		t.Fatalf("tool returned error: %s", getTextContent(t, result))
	}

	// Fact landed in the recipe's legacy bucket, not in the v2 /tmp path.
	path := filepath.Join(outputRoot, "legacy-facts.jsonl")
	got, err := ops.ReadFacts(path)
	if err != nil {
		t.Fatalf("read recipe legacy facts: %v", err)
	}
	if len(got) != 1 || got[0].Title != "execOnce burned on mid-seed crash" {
		t.Errorf("want 1 fact with matching title, got: %+v", got)
	}
}

// TestRecordFact_AmbiguousMultipleSessionsErrors — two open recipe sessions
// make "which session owns this fact?" unanswerable by inference; the tool
// must error rather than silently picking one.
func TestRecordFact_AmbiguousMultipleSessionsErrors(t *testing.T) {
	t.Parallel()
	engine := testEngine(t)
	if err := engine.Reset(); err != nil {
		t.Fatalf("reset engine: %v", err)
	}

	dir := t.TempDir()
	store := recipe.NewStore(dir)
	if _, err := store.OpenOrCreate("alpha-showcase", filepath.Join(dir, "a")); err != nil {
		t.Fatalf("open alpha: %v", err)
	}
	if _, err := store.OpenOrCreate("beta-showcase", filepath.Join(dir, "b")); err != nil {
		t.Fatalf("open beta: %v", err)
	}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterRecordFact(srv, engine, store)

	result := callTool(t, srv, "zerops_record_fact", map[string]any{
		"type":  ops.FactTypeGotchaCandidate,
		"title": "x",
	})
	text := getTextContent(t, result)
	if !strings.Contains(strings.ToLower(text), "session") {
		t.Errorf("expected session-ambiguity error, got: %s", text)
	}
}

// TestRecordFact_InferLikelyRouteTo covers the type → route mapping so
// the nudge message stays accurate if new FactType constants are added
// and inferLikelyRouteTo needs to pick up a new case.
func TestRecordFact_InferLikelyRouteTo(t *testing.T) {
	t.Parallel()
	cases := []struct {
		factType string
		want     string
	}{
		{ops.FactTypeGotchaCandidate, ops.FactRouteToContentGotcha},
		{ops.FactTypeIGItemCandidate, ops.FactRouteToContentIG},
		{ops.FactTypeCrossCodebaseContract, ops.FactRouteToContentIG},
		{ops.FactTypeFixApplied, ops.FactRouteToContentGotcha},
		{ops.FactTypeVerifiedBehavior, ops.FactRouteToZeropsYAMLComment},
		{ops.FactTypePlatformObservation, ops.FactRouteToZeropsYAMLComment},
		{"unknown_type", ""},
	}
	for _, tc := range cases {
		t.Run(tc.factType, func(t *testing.T) {
			t.Parallel()
			got := inferLikelyRouteTo(tc.factType)
			if got != tc.want {
				t.Errorf("inferLikelyRouteTo(%q) = %q, want %q", tc.factType, got, tc.want)
			}
		})
	}
}
