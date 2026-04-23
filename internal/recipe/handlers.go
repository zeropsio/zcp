package recipe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Store holds live Sessions keyed by slug. One ZCP process may host
// several recipe runs; Store serializes access.
type Store struct {
	mu        sync.Mutex
	sessions  map[string]*Session
	mountRoot string
}

// NewStore returns an empty store whose chain resolver reads from
// mountRoot (typically the zeropsio/recipes clone + zerops-recipe-apps
// mount).
func NewStore(mountRoot string) *Store {
	return &Store{sessions: map[string]*Session{}, mountRoot: mountRoot}
}

// Get returns an existing session by slug or false.
func (s *Store) Get(slug string) (*Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[slug]
	return sess, ok
}

// OpenOrCreate returns an existing session, or creates one at the given
// outputRoot with a freshly-resolved parent recipe.
func (s *Store) OpenOrCreate(slug, outputRoot string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[slug]; ok {
		return sess, nil
	}
	if err := os.MkdirAll(outputRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create output root: %w", err)
	}
	log := OpenFactsLog(filepath.Join(outputRoot, "facts.jsonl"))
	parent, err := ResolveChain(Resolver{MountRoot: s.mountRoot}, slug)
	if err != nil && !errors.Is(err, ErrNoParent) {
		return nil, fmt.Errorf("resolve chain: %w", err)
	}
	sess := NewSession(slug, log, outputRoot, parent)
	s.sessions[slug] = sess
	return sess, nil
}

// errSessionNotOpen is reported when a mutating action arrives for a
// slug that has not been opened via "start".
const errSessionNotOpen = "session not open"

// RecipeInput is the input schema for zerops_recipe.
type RecipeInput struct {
	Action     string          `json:"action"               jsonschema:"One of: start, enter-phase, complete-phase, build-brief, record-fact, resolve-chain, emit-yaml, status."`
	Slug       string          `json:"slug,omitempty"       jsonschema:"Recipe slug (e.g. {framework}-showcase). Required for every action."`
	OutputRoot string          `json:"outputRoot,omitempty" jsonschema:"Directory where the recipe tree + facts log live. Required for 'start'."`
	Phase      string          `json:"phase,omitempty"      jsonschema:"Phase name for enter-phase / complete-phase: research, provision, scaffold, feature, finalize."`
	BriefKind  string          `json:"briefKind,omitempty"  jsonschema:"For build-brief: scaffold, feature, writer."`
	Codebase   string          `json:"codebase,omitempty"   jsonschema:"For build-brief when kind=scaffold: the codebase hostname to compose for."`
	TierIndex  int             `json:"tierIndex,omitempty"  jsonschema:"For emit-yaml: tier 0..5."`
	Fact       json.RawMessage `json:"fact,omitempty"       jsonschema:"For record-fact: JSON object matching FactRecord schema."`
}

// RecipeResult is the generic envelope returned from zerops_recipe.
type RecipeResult struct {
	OK         bool          `json:"ok"`
	Action     string        `json:"action"`
	Slug       string        `json:"slug,omitempty"`
	Status     *Status       `json:"status,omitempty"`
	Brief      *Brief        `json:"brief,omitempty"`
	YAML       string        `json:"yaml,omitempty"`
	Violations []Violation   `json:"violations,omitempty"`
	Parent     *ParentRecipe `json:"parent,omitempty"`
	Error      string        `json:"error,omitempty"`
}

// Register installs the zerops_recipe tool. server.go gates it behind
// the strangler-fig flag during v3 transition.
func Register(srv *mcp.Server, store *Store) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_recipe",
		Description: "zcprecipator3 recipe engine. Actions: start, enter-phase, complete-phase, build-brief, record-fact, resolve-chain, emit-yaml, status. See docs/zcprecipator3/plan.md §6.",
		Annotations: &mcp.ToolAnnotations{Title: "Run a Zerops recipe (v3)"},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in RecipeInput) (*mcp.CallToolResult, any, error) {
		res := dispatch(ctx, store, in)
		if !res.OK {
			return errResult(res), nil, nil
		}
		return okResult(res), nil, nil
	})
}

// dispatch routes an action to the appropriate session method.
func dispatch(_ context.Context, store *Store, in RecipeInput) RecipeResult {
	r := RecipeResult{Action: in.Action, Slug: in.Slug}
	if in.Slug == "" && in.Action != "" {
		r.Error = "slug is required"
		return r
	}
	// Actions that require an existing session share session-loading.
	needsSession := map[string]bool{
		"enter-phase": true, "complete-phase": true, "build-brief": true,
		"record-fact": true, "emit-yaml": true, "status": true,
	}
	var sess *Session
	if needsSession[in.Action] {
		var ok bool
		sess, ok = store.Get(in.Slug)
		if !ok {
			r.Error = errSessionNotOpen
			return r
		}
	}
	switch in.Action {
	case "start":
		if in.OutputRoot == "" {
			r.Error = "outputRoot is required"
			return r
		}
		sess, err := store.OpenOrCreate(in.Slug, in.OutputRoot)
		if err != nil {
			r.Error = err.Error()
			return r
		}
		snap := sess.Snapshot()
		r.Status, r.Parent, r.OK = &snap, sess.Parent, true
	case "enter-phase":
		if err := sess.EnterPhase(Phase(in.Phase)); err != nil {
			r.Error = err.Error()
			return r
		}
		snap := sess.Snapshot()
		r.Status, r.OK = &snap, true
	case "complete-phase":
		violations, err := sess.CompletePhase(DefaultGates())
		if err != nil {
			r.Error = err.Error()
			return r
		}
		snap := sess.Snapshot()
		r.Violations, r.Status, r.OK = violations, &snap, len(violations) == 0
	case "build-brief":
		var cb Codebase
		for _, c := range sess.Plan.Codebases {
			if c.Hostname == in.Codebase {
				cb = c
			}
		}
		brief, err := sess.BuildBrief(BriefKind(in.BriefKind), cb)
		if err != nil {
			r.Error = err.Error()
			return r
		}
		r.Brief, r.OK = &brief, true
	case "record-fact":
		var f FactRecord
		if err := json.Unmarshal(in.Fact, &f); err != nil {
			r.Error = fmt.Sprintf("fact payload: %v", err)
			return r
		}
		if err := sess.RecordFact(f); err != nil {
			r.Error = err.Error()
			return r
		}
		r.OK = true
	case "resolve-chain":
		parent, err := ResolveChain(Resolver{MountRoot: store.mountRoot}, in.Slug)
		switch {
		case errors.Is(err, ErrNoParent):
			r.OK = true
		case err != nil:
			r.Error = err.Error()
		default:
			r.Parent, r.OK = parent, true
		}
	case "emit-yaml":
		yaml, err := sess.EmitYAML(in.TierIndex)
		if err != nil {
			r.Error = err.Error()
			return r
		}
		r.YAML, r.OK = yaml, true
	case "status":
		snap := sess.Snapshot()
		r.Status, r.OK = &snap, true
	default:
		r.Error = fmt.Sprintf("unknown action %q", in.Action)
	}
	return r
}

func okResult(res RecipeResult) *mcp.CallToolResult {
	text, _ := marshalResult(res)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

func errResult(res RecipeResult) *mcp.CallToolResult {
	text, _ := marshalResult(res)
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
		IsError: true,
	}
}

// marshalResult serializes a RecipeResult. Returns fallback text if
// marshaling ever fails — RecipeResult's fields are all JSON-safe
// concrete types so this is defensive.
func marshalResult(res RecipeResult) (string, error) {
	b, err := json.Marshal(res)
	if err != nil {
		return fmt.Sprintf("{\"ok\":false,\"error\":%q}", err.Error()), err
	}
	return string(b), nil
}
