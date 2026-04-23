package recipe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	Action     string         `json:"action"               jsonschema:"One of: start, enter-phase, complete-phase, build-brief, record-fact, resolve-chain, emit-yaml, update-plan, stitch-content, status."`
	Slug       string         `json:"slug,omitempty"       jsonschema:"Recipe slug (e.g. {framework}-showcase). Required for every action."`
	OutputRoot string         `json:"outputRoot,omitempty" jsonschema:"Directory where the recipe tree + facts log live. Required for 'start'."`
	Phase      string         `json:"phase,omitempty"      jsonschema:"Phase name for enter-phase / complete-phase: research, provision, scaffold, feature, finalize."`
	BriefKind  string         `json:"briefKind,omitempty"  jsonschema:"For build-brief: scaffold, feature, writer."`
	Codebase   string         `json:"codebase,omitempty"   jsonschema:"For build-brief when kind=scaffold: the codebase hostname to compose for."`
	Shape      string         `json:"shape,omitempty"      jsonschema:"For emit-yaml: 'workspace' (services-only YAML for zerops_import at provision) or 'deliverable' (full published template for tierIndex, written to disk)."`
	TierIndex  int            `json:"tierIndex,omitempty"  jsonschema:"For emit-yaml shape=deliverable: tier 0..5. Ignored when shape=workspace."`
	Fact       *FactRecord    `json:"fact,omitempty"       jsonschema:"For record-fact: a FactRecord object with topic, symptom, mechanism, surfaceHint, citation fields."`
	Plan       *Plan          `json:"plan,omitempty"       jsonschema:"For update-plan: partial Plan object. Fields present overwrite session.Plan; omitted fields untouched."`
	Payload    map[string]any `json:"payload,omitempty"    jsonschema:"For stitch-content: writer sub-agent's structured completion payload as a JSON object."`
}

// RecipeResult is the generic envelope returned from zerops_recipe.
// ParentStatus is an explicit "mounted" / "absent" / "" signal so the
// agent doesn't have to infer presence from a nil Parent pointer —
// "parent missing" is a legitimate first-time-framework state, not an
// error, and the research atom branches on it.
type RecipeResult struct {
	OK           bool          `json:"ok"`
	Action       string        `json:"action"`
	Slug         string        `json:"slug,omitempty"`
	Status       *Status       `json:"status,omitempty"`
	Brief        *Brief        `json:"brief,omitempty"`
	YAML         string        `json:"yaml,omitempty"`
	Violations   []Violation   `json:"violations,omitempty"`
	Parent       *ParentRecipe `json:"parent,omitempty"`
	ParentStatus string        `json:"parentStatus,omitempty"`
	Guidance     string        `json:"guidance,omitempty"`
	StitchedPath string        `json:"stitchedPath,omitempty"`
	Error        string        `json:"error,omitempty"`
}

// Register installs the zerops_recipe tool. server.go gates it behind
// the strangler-fig flag during v3 transition.
func Register(srv *mcp.Server, store *Store) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_recipe",
		Description: "zcprecipator3 recipe engine. Actions: start, enter-phase, complete-phase, build-brief, record-fact, resolve-chain, emit-yaml, update-plan, stitch-content, status. Call start first — it returns the research-phase guidance and the parent recipe inline. See docs/zcprecipator3/plan.md §6.",
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
		"update-plan": true, "stitch-content": true,
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
		r.Status, r.Parent = &snap, sess.Parent
		r.ParentStatus = parentStatus(sess.Parent)
		r.Guidance = loadPhaseEntry(sess.Current)
		r.OK = true
	case "enter-phase":
		if err := sess.EnterPhase(Phase(in.Phase)); err != nil {
			r.Error = err.Error()
			return r
		}
		snap := sess.Snapshot()
		r.Status = &snap
		r.Guidance = loadPhaseEntry(sess.Current)
		r.OK = true
	case "complete-phase":
		violations, err := sess.CompletePhase(gatesForPhase(sess.Current))
		if err != nil {
			r.Error = err.Error()
			return r
		}
		snap := sess.Snapshot()
		r.Violations, r.Status = violations, &snap
		r.OK = len(violations) == 0
		// On success, include next phase's entry guidance so the agent
		// knows what to do after transitioning.
		if r.OK {
			if next, ok := nextPhase(sess.Current); ok {
				r.Guidance = "Next phase: " + string(next) + "\n\n" + loadPhaseEntry(next)
			}
		}
	case "update-plan":
		if err := mergePlan(sess, in.Plan); err != nil {
			r.Error = err.Error()
			return r
		}
		snap := sess.Snapshot()
		r.Status, r.OK = &snap, true
	case "build-brief":
		brief, err := buildBriefForRequest(sess, in)
		if err != nil {
			r.Error = err.Error()
			return r
		}
		r.Brief, r.OK = &brief, true
	case "record-fact":
		if in.Fact == nil {
			r.Error = "record-fact: fact payload is required"
			return r
		}
		if err := sess.RecordFact(*in.Fact); err != nil {
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
		shape := Shape(in.Shape)
		if shape == "" {
			shape = ShapeDeliverable
		}
		yaml, err := sess.EmitYAML(shape, in.TierIndex)
		if err != nil {
			r.Error = err.Error()
			return r
		}
		r.YAML, r.OK = yaml, true
	case "stitch-content":
		path, err := stitchContent(sess, in.Payload)
		if err != nil {
			r.Error = err.Error()
			return r
		}
		r.StitchedPath, r.OK = path, true
	case "status":
		snap := sess.Snapshot()
		r.Status = &snap
		r.Guidance = loadPhaseEntry(sess.Current)
		r.OK = true
	default:
		r.Error = fmt.Sprintf("unknown action %q", in.Action)
	}
	return r
}

// mergePlan applies an incoming partial Plan payload to the session.
// Non-empty fields overwrite; empty fields leave existing state
// untouched. Enables progressive planning without the agent needing to
// re-submit the whole Plan on every tweak.
func mergePlan(sess *Session, incoming *Plan) error {
	if incoming == nil {
		return errors.New("update-plan: missing plan payload")
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	cur := sess.Plan
	if cur == nil {
		cur = &Plan{Slug: sess.Slug}
	}
	if incoming.Framework != "" {
		cur.Framework = incoming.Framework
	}
	if incoming.Tier != "" {
		cur.Tier = incoming.Tier
	}
	if (incoming.Research != ResearchResult{}) {
		cur.Research = incoming.Research
	}
	if len(incoming.Codebases) > 0 {
		cur.Codebases = incoming.Codebases
	}
	if len(incoming.Services) > 0 {
		cur.Services = incoming.Services
	}
	if len(incoming.EnvComments) > 0 {
		cur.EnvComments = incoming.EnvComments
	}
	if len(incoming.ProjectEnvVars) > 0 {
		cur.ProjectEnvVars = incoming.ProjectEnvVars
	}
	sess.Plan = cur
	return nil
}

// buildBriefForRequest resolves a codebase (for scaffold briefs) and
// delegates to the session's brief builder. Returns a clear error when
// the named codebase isn't in the plan — the most common cause of
// "unknown role" errors the v1 dogfood run surfaced.
func buildBriefForRequest(sess *Session, in RecipeInput) (Brief, error) {
	var cb Codebase
	if BriefKind(in.BriefKind) == BriefScaffold {
		if in.Codebase == "" {
			return Brief{}, errors.New("build-brief kind=scaffold: codebase hostname required")
		}
		found := false
		for _, c := range sess.Plan.Codebases {
			if c.Hostname == in.Codebase {
				cb, found = c, true
				break
			}
		}
		if !found {
			return Brief{}, fmt.Errorf(
				"codebase %q not in plan — call action=update-plan first with plan.codebases populated",
				in.Codebase,
			)
		}
	}
	return sess.BuildBrief(BriefKind(in.BriefKind), cb)
}

// stitchContent absorbs the writer sub-agent's completion payload into
// the recipe output tree. Steps:
//
//  1. Archive the raw payload at <outputRoot>/.writer-payload.json
//     (gate checks still read this).
//  2. Merge structured env fields into the plan:
//     - env_import_comments → plan.EnvComments
//     - project_env_vars    → plan.ProjectEnvVars
//  3. Regenerate all 6 deliverable import.yaml files using the merged
//     plan (writer-authored comments + per-tier project env vars land
//     in the published yaml).
//  4. Write the 7 content surfaces (root README, env READMEs, per-
//     codebase README fragments + CLAUDE.md) to their canonical paths.
//
// Returns the path of the archived payload for backwards-compatible
// tests; callers inspect the output tree directly for stitched content.
func stitchContent(sess *Session, payload map[string]any) (string, error) {
	if len(payload) == 0 {
		return "", errors.New("stitch-content: payload is required")
	}

	// Step 1 — archive the raw payload under the session lock.
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("re-marshal payload: %w", err)
	}
	sess.mu.Lock()
	outputRoot := sess.OutputRoot
	sess.mu.Unlock()
	archivePath := filepath.Join(outputRoot, ".writer-payload.json")
	if err := os.WriteFile(archivePath, raw, 0o600); err != nil {
		return "", fmt.Errorf("write payload: %w", err)
	}

	// Step 2 — merge env comments + project env vars into the plan so
	// the next emit reads them. Uses the typed mergePlan path so field
	// merge semantics stay consistent.
	envComments := extractEnvComments(payload)
	projectEnvVars := extractProjectEnvVars(payload)
	if len(envComments) > 0 || len(projectEnvVars) > 0 {
		patch := &Plan{EnvComments: envComments, ProjectEnvVars: projectEnvVars}
		if err := mergePlan(sess, patch); err != nil {
			return "", fmt.Errorf("merge writer plan fields: %w", err)
		}
	}

	// Step 3 — regenerate every deliverable yaml to disk.
	for i := range Tiers() {
		if _, err := sess.EmitYAML(ShapeDeliverable, i); err != nil {
			return "", fmt.Errorf("regenerate tier %d import.yaml: %w", i, err)
		}
	}

	// Step 4 — write the content surfaces. Errors abort; a partial stitch
	// is worse than a clear failure the agent can retry.
	if err := writeContentSurfaces(outputRoot, payload); err != nil {
		return "", fmt.Errorf("write content surfaces: %w", err)
	}

	return archivePath, nil
}

// extractEnvComments maps the writer's env_import_comments payload into
// the plan's EnvComments shape. Missing or wrong-type entries are
// skipped silently — gate checks catch missing content downstream.
func extractEnvComments(payload map[string]any) map[string]EnvComments {
	raw, ok := payload["env_import_comments"].(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := make(map[string]EnvComments, len(raw))
	for key, v := range raw {
		entry, ok := v.(map[string]any)
		if !ok {
			continue
		}
		var ec EnvComments
		if p, ok := entry["project"].(string); ok {
			ec.Project = p
		}
		if svc, ok := entry["service"].(map[string]any); ok {
			ec.Service = make(map[string]string, len(svc))
			for host, c := range svc {
				if s, ok := c.(string); ok {
					ec.Service[host] = s
				}
			}
		}
		out[key] = ec
	}
	return out
}

// extractProjectEnvVars maps the writer's project_env_vars payload into
// the plan's ProjectEnvVars shape (per-env map of env var name → value).
// Preprocessor expressions and ${zeropsSubdomainHost} literals pass
// through verbatim — the emitter writes them byte-identical for end-user
// project-import resolution.
func extractProjectEnvVars(payload map[string]any) map[string]map[string]string {
	raw, ok := payload["project_env_vars"].(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := make(map[string]map[string]string, len(raw))
	for key, v := range raw {
		entry, ok := v.(map[string]any)
		if !ok {
			continue
		}
		inner := make(map[string]string, len(entry))
		for name, val := range entry {
			if s, ok := val.(string); ok {
				inner[name] = s
			}
		}
		out[key] = inner
	}
	return out
}

// writeContentSurfaces writes the writer's string-valued payload fields
// to their canonical paths in the output tree. See
// internal/recipe/content/briefs/writer/completion_payload.md for the
// schema.
func writeContentSurfaces(outputRoot string, payload map[string]any) error {
	// Root README.
	if body, ok := payload["root_readme"].(string); ok && body != "" {
		if err := writeSurfaceFile(filepath.Join(outputRoot, "README.md"), body); err != nil {
			return err
		}
	}
	// Per-env READMEs, keyed "0".."5" → <tier.Folder>/README.md.
	if envReadmes, ok := payload["env_readmes"].(map[string]any); ok {
		for key, v := range envReadmes {
			body, ok := v.(string)
			if !ok || body == "" {
				continue
			}
			idx := 0
			if _, err := fmt.Sscanf(key, "%d", &idx); err != nil {
				continue
			}
			tier, ok := TierAt(idx)
			if !ok {
				continue
			}
			if err := writeSurfaceFile(filepath.Join(outputRoot, tier.Folder, "README.md"), body); err != nil {
				return err
			}
		}
	}
	// Per-codebase README (integration guide + gotchas fragments).
	if readmes, ok := payload["codebase_readmes"].(map[string]any); ok {
		for host, v := range readmes {
			frag, ok := v.(map[string]any)
			if !ok {
				continue
			}
			body := assembleCodebaseReadme(frag)
			if body == "" {
				continue
			}
			if err := writeSurfaceFile(filepath.Join(outputRoot, "codebases", host, "README.md"), body); err != nil {
				return err
			}
		}
	}
	// Per-codebase CLAUDE.md.
	if claudeMap, ok := payload["codebase_claude"].(map[string]any); ok {
		for host, v := range claudeMap {
			body, ok := v.(string)
			if !ok || body == "" {
				continue
			}
			if err := writeSurfaceFile(filepath.Join(outputRoot, "codebases", host, "CLAUDE.md"), body); err != nil {
				return err
			}
		}
	}
	return nil
}

// assembleCodebaseReadme glues the two writer-owned fragments (IG +
// gotchas) into a single README body. Each fragment is wrapped in a
// named marker so downstream tooling can extract them individually.
func assembleCodebaseReadme(frag map[string]any) string {
	ig, _ := frag["integration_guide"].(string)
	kb, _ := frag["gotchas"].(string)
	if ig == "" && kb == "" {
		return ""
	}
	var b strings.Builder
	if ig != "" {
		b.WriteString("<!-- integration-guide-start -->\n")
		b.WriteString(strings.TrimSpace(ig))
		b.WriteString("\n<!-- integration-guide-end -->\n\n")
	}
	if kb != "" {
		b.WriteString("<!-- knowledge-base-start -->\n")
		b.WriteString(strings.TrimSpace(kb))
		b.WriteString("\n<!-- knowledge-base-end -->\n")
	}
	return b.String()
}

func writeSurfaceFile(path, body string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// parentStatus returns a short tag telling the agent whether the chain
// resolver found a parent. "absent" means first-time-framework run OR
// parent not mounted — both legitimate, and the research atom branches
// on this string to tell the agent not to freeform-search for
// substitute knowledge.
func parentStatus(p *ParentRecipe) string {
	if p == nil {
		return "absent"
	}
	return "mounted"
}

// nextPhase returns the phase immediately after p, if any.
func nextPhase(p Phase) (Phase, bool) {
	all := Phases()
	for i, q := range all {
		if q == p && i+1 < len(all) {
			return all[i+1], true
		}
	}
	return "", false
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
