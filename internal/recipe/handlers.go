package recipe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
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

// HasAnySession reports whether at least one recipe session is open.
// Used by the workflow-context guard in internal/tools/guard.go so an
// active recipe run satisfies zerops_import/zerops_mount's "must be in
// a workflow" precondition without starting a separate bootstrap/
// develop workflow.
func (s *Store) HasAnySession() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions) > 0
}

// CurrentSingleSession returns the slug + per-session file paths for the
// single open recipe session, or ok=false when zero or >1 sessions are open.
// Ambiguity must not be resolved by inference — the caller should surface an
// error instead of picking one.
//
// Two cross-tool routing primitives come out of this: the legacy-facts path,
// used by zerops_record_fact (v2 schema) so v2-authored facts land inside
// the recipe run dir instead of a v2 session's /tmp; and the manifest path,
// used by zerops_workspace_manifest so the workspace manifest lives next to
// the rest of the recipe artifacts. The v3 FactsLog at <outputRoot>/facts.jsonl
// stays reserved for structurally-classified records written via
// zerops_recipe action=record-fact.
func (s *Store) CurrentSingleSession() (slug, legacyFactsPath, manifestPath string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sessions) != 1 {
		return "", "", "", false
	}
	var sess *Session
	for sl, sv := range s.sessions {
		slug, sess = sl, sv
	}
	out := sess.OutputRoot
	return slug,
		filepath.Join(out, "legacy-facts.jsonl"),
		filepath.Join(out, "workspace-manifest.json"),
		true
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
	Action     string      `json:"action"               jsonschema:"One of: start, enter-phase, complete-phase, build-brief, record-fact, record-fragment, resolve-chain, emit-yaml, update-plan, stitch-content, status."`
	Slug       string      `json:"slug,omitempty"       jsonschema:"Recipe slug (e.g. {framework}-showcase). Required for every action."`
	OutputRoot string      `json:"outputRoot,omitempty" jsonschema:"Directory where the recipe tree + facts log live. Required for 'start'."`
	Phase      string      `json:"phase,omitempty"      jsonschema:"Phase name for enter-phase / complete-phase: research, provision, scaffold, feature, finalize."`
	BriefKind  string      `json:"briefKind,omitempty"  jsonschema:"For build-brief: scaffold, feature."`
	Codebase   string      `json:"codebase,omitempty"   jsonschema:"For build-brief when kind=scaffold: the codebase hostname to compose for."`
	Shape      string      `json:"shape,omitempty"      jsonschema:"For emit-yaml: 'workspace' (services-only YAML for zerops_import at provision) or 'deliverable' (full published template for tierIndex, written to disk)."`
	TierIndex  int         `json:"tierIndex,omitempty"  jsonschema:"For emit-yaml shape=deliverable: tier 0..5. Ignored when shape=workspace."`
	Fact       *FactRecord `json:"fact,omitempty"       jsonschema:"For record-fact: a FactRecord object with topic, symptom, mechanism, surfaceHint, citation fields."`
	Plan       *Plan       `json:"plan,omitempty"       jsonschema:"For update-plan: partial Plan object. Fields present overwrite session.Plan; omitted fields untouched."`
	FragmentID string      `json:"fragmentId,omitempty" jsonschema:"For record-fragment: fragment identifier. Valid shapes: root/intro, env/<N>/intro (N=0..5), env/<N>/import-comments/<hostname>, env/<N>/import-comments/project, codebase/<hostname>/intro, codebase/<hostname>/integration-guide, codebase/<hostname>/knowledge-base, codebase/<hostname>/claude-md/service-facts, codebase/<hostname>/claude-md/notes."`
	Fragment   string      `json:"fragment,omitempty"   jsonschema:"For record-fragment: the fragment body. Overwrite for root/* and env/* ids; append-on-extend for codebase/*/integration-guide, knowledge-base, claude-md/* ids so a feature sub-agent extends scaffold's body rather than replacing it."`
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
		"record-fact": true, "record-fragment": true, "emit-yaml": true,
		"status": true, "update-plan": true, "stitch-content": true,
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
	case "record-fragment":
		if in.FragmentID == "" {
			r.Error = "record-fragment: fragmentId is required"
			return r
		}
		if err := recordFragment(sess, in.FragmentID, in.Fragment); err != nil {
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
		missing, err := stitchContent(sess)
		if err != nil {
			r.Error = err.Error()
			return r
		}
		if len(missing) > 0 {
			r.Error = fmt.Sprintf("stitch-content: missing fragments: %s", strings.Join(missing, ", "))
			r.StitchedPath = sess.OutputRoot
			return r
		}
		r.StitchedPath, r.OK = sess.OutputRoot, true
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
	if len(incoming.Fragments) > 0 {
		if cur.Fragments == nil {
			cur.Fragments = map[string]string{}
		}
		maps.Copy(cur.Fragments, incoming.Fragments)
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

// stitchContent walks the surface templates, renders each with the
// plan's structural data + in-phase-authored fragments, and writes the
// finished files to the output tree. Returns the list of missing
// fragment ids discovered during render — an empty list means every
// marker had a body. Callers treat non-empty as a gate failure (plan
// §2.A.5: missing fragment → gate failure, not silent empty).
//
// Regenerates every tier's import.yaml to disk so the writer-free
// stitch still emits env YAMLs as before.
func stitchContent(sess *Session) ([]string, error) {
	sess.mu.Lock()
	plan := sess.Plan
	outputRoot := sess.OutputRoot
	sess.mu.Unlock()
	if plan == nil {
		return nil, errors.New("stitch-content: nil plan")
	}

	// Regenerate tier yamls.
	for i := range Tiers() {
		if _, err := sess.EmitYAML(ShapeDeliverable, i); err != nil {
			return nil, fmt.Errorf("regenerate tier %d import.yaml: %w", i, err)
		}
	}

	var missing []string

	// Root README.
	rootBody, m, err := AssembleRootREADME(plan)
	if err != nil {
		return nil, fmt.Errorf("assemble root: %w", err)
	}
	missing = append(missing, m...)
	if err := writeSurfaceFile(filepath.Join(outputRoot, "README.md"), rootBody); err != nil {
		return nil, err
	}

	// Env READMEs.
	for i := range Tiers() {
		envBody, m, err := AssembleEnvREADME(plan, i)
		if err != nil {
			return nil, fmt.Errorf("assemble env %d: %w", i, err)
		}
		missing = append(missing, m...)
		tier, _ := TierAt(i)
		if err := writeSurfaceFile(filepath.Join(outputRoot, tier.Folder, "README.md"), envBody); err != nil {
			return nil, err
		}
	}

	// Per-codebase apps-repo shape — README + CLAUDE.md + zerops.yaml
	// (copied from the scaffold-authored workspace, not re-emitted, so
	// inline comments survive byte-identical). See plan §2.A.5.
	for _, cb := range plan.Codebases {
		readmeBody, m, err := AssembleCodebaseREADME(plan, cb.Hostname)
		if err != nil {
			return nil, fmt.Errorf("assemble codebase %s README: %w", cb.Hostname, err)
		}
		missing = append(missing, m...)
		cbRoot := filepath.Join(outputRoot, "codebases", cb.Hostname)
		if err := writeSurfaceFile(filepath.Join(cbRoot, "README.md"), readmeBody); err != nil {
			return nil, err
		}
		claudeBody, m, err := AssembleCodebaseClaudeMD(plan, cb.Hostname)
		if err != nil {
			return nil, fmt.Errorf("assemble codebase %s CLAUDE.md: %w", cb.Hostname, err)
		}
		missing = append(missing, m...)
		if err := writeSurfaceFile(filepath.Join(cbRoot, "CLAUDE.md"), claudeBody); err != nil {
			return nil, err
		}
		if err := copyCommittedYAML(cb, cbRoot); err != nil {
			return nil, fmt.Errorf("copy %s zerops.yaml: %w", cb.Hostname, err)
		}
	}

	return missing, nil
}

// copyCommittedYAML copies <cb.SourceRoot>/zerops.yaml into the apps-
// repo shape at <cbRoot>/zerops.yaml verbatim. SourceRoot unset is a
// soft fail: the stitch proceeds but the codebase ships without a
// zerops.yaml so the finalize validator surfaces the gap (run-8
// acceptance #4). Missing source file is a hard fail — it signals
// scaffold never committed the yaml.
func copyCommittedYAML(cb Codebase, cbRoot string) error {
	if cb.SourceRoot == "" {
		return nil
	}
	src := filepath.Join(cb.SourceRoot, "zerops.yaml")
	body, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read scaffold yaml at %s: %w", src, err)
	}
	if err := os.MkdirAll(cbRoot, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", cbRoot, err)
	}
	dst := filepath.Join(cbRoot, "zerops.yaml")
	if err := os.WriteFile(dst, body, 0o600); err != nil {
		return fmt.Errorf("write copied yaml: %w", err)
	}
	return nil
}

// recordFragment validates the fragment id against the plan, applies
// append-or-overwrite semantics, and stores the body on plan.Fragments
// (or on the typed EnvComments for env/*/import-comments/* ids).
func recordFragment(sess *Session, id, body string) error {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.Plan == nil {
		return errors.New("record-fragment: plan not initialized — call update-plan first")
	}
	if !isValidFragmentID(sess.Plan, id) {
		return fmt.Errorf("record-fragment: unknown fragmentId %q", id)
	}
	if strings.HasPrefix(id, "env/") && strings.Contains(id, "/import-comments/") {
		return applyEnvComment(sess.Plan, id, body)
	}
	if sess.Plan.Fragments == nil {
		sess.Plan.Fragments = map[string]string{}
	}
	if isAppendFragmentID(id) {
		existing := sess.Plan.Fragments[id]
		if existing == "" {
			sess.Plan.Fragments[id] = body
			return nil
		}
		sess.Plan.Fragments[id] = existing + "\n\n" + body
		return nil
	}
	sess.Plan.Fragments[id] = body
	return nil
}

// applyEnvComment routes env/<N>/import-comments/<target> into the
// typed plan.EnvComments map so the yaml emitter reads writer-authored
// comments without a separate fragment-consumption layer.
func applyEnvComment(plan *Plan, id, body string) error {
	// id = "env/<N>/import-comments/<target>"
	rest := strings.TrimPrefix(id, "env/")
	slash := strings.IndexByte(rest, '/')
	if slash <= 0 {
		return fmt.Errorf("record-fragment: malformed env id %q", id)
	}
	tierKey := rest[:slash]
	target := strings.TrimPrefix(rest[slash+1:], "import-comments/")
	if plan.EnvComments == nil {
		plan.EnvComments = map[string]EnvComments{}
	}
	ec := plan.EnvComments[tierKey]
	if target == "project" {
		ec.Project = body
	} else {
		if ec.Service == nil {
			ec.Service = map[string]string{}
		}
		ec.Service[target] = body
	}
	plan.EnvComments[tierKey] = ec
	return nil
}

// isAppendFragmentID reports whether an id uses append-on-extend
// semantics. Per plan §2.A.4: feature sub-agent extends IG, KB, and
// CLAUDE.md sections; root and env overwrite (main agent authors once).
func isAppendFragmentID(id string) bool {
	if !strings.HasPrefix(id, "codebase/") {
		return false
	}
	switch {
	case strings.HasSuffix(id, "/integration-guide"):
		return true
	case strings.HasSuffix(id, "/knowledge-base"):
		return true
	case strings.Contains(id, "/claude-md/"):
		return true
	}
	return false
}

// fragmentIDRoot is the only root-scoped fragment id. Constants prevent
// a typo here from silently diverging from the assembler's marker id.
const fragmentIDRoot = "root/intro"

// isValidFragmentID reports whether id matches one of the declared
// fragment shapes given the plan's codebases. Covers root/, env/<N>/,
// env/<N>/import-comments/<hostname|project>, codebase/<hostname>/...
func isValidFragmentID(plan *Plan, id string) bool {
	if id == fragmentIDRoot {
		return true
	}
	if rest, ok := strings.CutPrefix(id, "env/"); ok {
		slash := strings.IndexByte(rest, '/')
		if slash <= 0 {
			return false
		}
		tierIdx, err := parseTierIndex(rest[:slash])
		if err != nil {
			return false
		}
		if _, ok := TierAt(tierIdx); !ok {
			return false
		}
		tail := rest[slash+1:]
		switch {
		case tail == "intro":
			return true
		case tail == "import-comments/project":
			return true
		case strings.HasPrefix(tail, "import-comments/"):
			host := strings.TrimPrefix(tail, "import-comments/")
			return codebaseKnown(plan, host) || serviceKnown(plan, host)
		}
		return false
	}
	if rest, ok := strings.CutPrefix(id, "codebase/"); ok {
		slash := strings.IndexByte(rest, '/')
		if slash <= 0 {
			return false
		}
		host := rest[:slash]
		if !codebaseKnown(plan, host) {
			return false
		}
		tail := rest[slash+1:]
		switch tail {
		case "intro", "integration-guide", "knowledge-base",
			"claude-md/service-facts", "claude-md/notes":
			return true
		}
		return false
	}
	return false
}

// parseTierIndex returns the numeric tier index parsed from a string
// key; returns an error on any non-numeric input.
func parseTierIndex(s string) (int, error) {
	var i int
	_, err := fmt.Sscanf(s, "%d", &i)
	return i, err
}

// serviceKnown reports whether a hostname matches one of the plan's
// managed services. Env import-comments may address a managed service
// block (db, cache, storage) in addition to runtime codebases.
func serviceKnown(plan *Plan, hostname string) bool {
	if plan == nil {
		return false
	}
	for _, s := range plan.Services {
		if s.Hostname == hostname {
			return true
		}
	}
	return false
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
