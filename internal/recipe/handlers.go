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
	sess.MountRoot = s.mountRoot
	s.sessions[slug] = sess
	return sess, nil
}

// errSessionNotOpen is reported when a mutating action arrives for a
// slug that has not been opened via "start".
const errSessionNotOpen = "session not open"

// RecipeInput is the input schema for zerops_recipe.
type RecipeInput struct {
	Action           string      `json:"action"                     jsonschema:"One of: start, enter-phase, complete-phase, build-brief, build-subagent-prompt, verify-subagent-dispatch, record-fact, record-fragment, fill-fact-slot, resolve-chain, emit-yaml, update-plan, stitch-content, status."`
	Slug             string      `json:"slug,omitempty"             jsonschema:"Recipe slug (e.g. {framework}-showcase). Required for every action."`
	OutputRoot       string      `json:"outputRoot,omitempty"       jsonschema:"Directory where the recipe tree + facts log live. Required for 'start'."`
	Phase            string      `json:"phase,omitempty"            jsonschema:"Phase name for enter-phase / complete-phase: research, provision, scaffold, feature, codebase-content, env-content, finalize, refinement."`
	BriefKind        string      `json:"briefKind,omitempty"        jsonschema:"For build-brief: scaffold, feature, codebase-content, claudemd-author, env-content, finalize, refinement."`
	Codebase         string      `json:"codebase,omitempty"         jsonschema:"For build-brief when kind=scaffold: the codebase hostname to compose for. For complete-phase: when set, scopes codebase-surface validators to that one codebase only — the sub-agent's pre-termination self-validate path. Phase advance only fires when codebase is empty (the main-agent's post-sub-agent-return path)."`
	Shape            string      `json:"shape,omitempty"            jsonschema:"For emit-yaml: 'workspace' (services-only YAML for zerops_import at provision) or 'deliverable' (full published template for tierIndex, written to disk)."`
	TierIndex        int         `json:"tierIndex,omitempty"        jsonschema:"For emit-yaml shape=deliverable: tier 0..5. Ignored when shape=workspace."`
	Fact             *FactRecord `json:"fact,omitempty"             jsonschema:"For record-fact: a FactRecord object with topic, symptom, mechanism, surfaceHint, citation fields."`
	Plan             *Plan       `json:"plan,omitempty"             jsonschema:"For update-plan: partial Plan object. Fields present overwrite session.Plan; omitted fields untouched."`
	FragmentID       string      `json:"fragmentId,omitempty"       jsonschema:"For record-fragment: fragment identifier. Valid shapes: root/intro, env/<N>/intro (N=0..5), env/<N>/import-comments/<hostname>, env/<N>/import-comments/project, codebase/<hostname>/intro, codebase/<hostname>/integration-guide, codebase/<hostname>/knowledge-base, codebase/<hostname>/claude-md/service-facts, codebase/<hostname>/claude-md/notes."`
	Fragment         string      `json:"fragment,omitempty"         jsonschema:"For record-fragment: the fragment body. Overwrite for root/* and env/* ids; append-on-extend for codebase/*/integration-guide, knowledge-base, claude-md/* ids so a feature sub-agent extends scaffold's body rather than replacing it."`
	Mode             string      `json:"mode,omitempty"             jsonschema:"For record-fragment: 'append' (default for codebase IG/KB/claude-md ids; concatenates with prior body) or 'replace' (overwrites prior body). Use 'replace' to correct a fragment you authored earlier in the same recipe session, e.g. after a complete-phase validator violation."`
	DispatchedPrompt string      `json:"dispatchedPrompt,omitempty" jsonschema:"For verify-subagent-dispatch: the prompt the main agent intends to pass to Agent. Engine recomposes the brief and confirms its body appears byte-identical inside the dispatched prompt. Wrapper text around the brief (header lines before, context notes after) is allowed; only truncations and paraphrases are rejected."`
	// Classification (run-15 F.3) — optional spec classification for
	// record-fragment. When present, the engine refuses incompatible
	// (classification, fragmentId) pairs per the
	// docs/spec-content-surfaces.md compatibility table. Empty
	// classification keeps prior behavior (no refusal).
	Classification string `json:"classification,omitempty" jsonschema:"For record-fragment: optional fact classification — one of platform-invariant, intersection, framework-quirk, library-metadata, scaffold-decision, operational, self-inflicted. The engine refuses classifications that don't belong on the fragment's surface (e.g. self-inflicted on KB, scaffold-decision on CLAUDE.md). See docs/spec-content-surfaces.md#classification--surface-compatibility."`
}

// RecipeResult is the generic envelope returned from zerops_recipe.
// ParentStatus is an explicit "mounted" / "absent" / "" signal so the
// agent doesn't have to infer presence from a nil Parent pointer —
// "parent missing" is a legitimate first-time-framework state, not an
// error, and the research atom branches on it.
type RecipeResult struct {
	OK         bool        `json:"ok"`
	Action     string      `json:"action"`
	Slug       string      `json:"slug,omitempty"`
	Status     *Status     `json:"status,omitempty"`
	Brief      *Brief      `json:"brief,omitempty"`
	YAML       string      `json:"yaml,omitempty"`
	Violations []Violation `json:"violations,omitempty"`
	// Notices are gate findings that did NOT block phase completion —
	// SeverityNotice violations from validators wired on the DISCOVER
	// side of the TEACH/DISCOVER line (system.md §4). The agent sees
	// the lesson; publication continues.
	Notices      []Violation   `json:"notices,omitempty"`
	Parent       *ParentRecipe `json:"parent,omitempty"`
	ParentStatus string        `json:"parentStatus,omitempty"`
	Guidance     string        `json:"guidance,omitempty"`
	StitchedPath string        `json:"stitchedPath,omitempty"`
	// FragmentID, BodyBytes, Appended — run-9-readiness §2.J. Echoed on
	// record-fragment success so the caller sees which fragment landed,
	// the post-write body size, and whether append semantics fired
	// (previously 22 record-fragment calls returned byte-identical
	// envelopes, leaving the author no signal).
	FragmentID string `json:"fragmentId,omitempty"`
	BodyBytes  int    `json:"bodyBytes,omitempty"`
	Appended   bool   `json:"appended,omitempty"`
	// PriorBody is the fragment body that was overwritten by a
	// successful mode=replace call. Empty for append-class operations
	// and for mode=replace on a fragment that had no prior body.
	// Run-14 §B.1 (R-13-3) — agents extending an existing fragment
	// can merge against this baseline instead of grep+reconstructing
	// from the on-disk README.
	PriorBody string `json:"priorBody,omitempty"`
	// Notice carries an advisory message — currently used by record-fact
	// when V-1's classifier override re-routes a self-inflicted fact away
	// from the agent's platform-trap surfaceHint. Empty when no override
	// fires. Run-11 gap V-1.
	Notice string `json:"notice,omitempty"`
	// SurfaceContract is the surface contract (reader, test, caps,
	// FormatSpec) the agent should author the fragment against. Returned
	// on every record-fragment response so the per-surface contract
	// reaches the author at authoring decision time, not just at brief-
	// preface time. Run-15 F.2.
	SurfaceContract *SurfaceContract `json:"surfaceContract,omitempty"`
	// Prompt is the engine-composed sub-agent dispatch prompt — engine-
	// owned wrapper + brief body + close criteria. Returned by
	// action=build-subagent-prompt; main agent dispatches with
	// `prompt=<response.prompt>` byte-identical, eliminating the hand-
	// typed wrapper that compounded math/path drift across runs.
	// Run-13 §B2.
	Prompt string `json:"prompt,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Register installs the zerops_recipe tool. server.go gates it behind
// the strangler-fig flag during v3 transition.
func Register(srv *mcp.Server, store *Store) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_recipe",
		Description: "zcprecipator3 recipe engine. Actions: start, enter-phase, complete-phase, build-brief, build-subagent-prompt, verify-subagent-dispatch, record-fact, resolve-chain, emit-yaml, update-plan, stitch-content, status. Call start first — it returns the research-phase guidance and the parent recipe inline. See docs/zcprecipator3/plan.md §6.",
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
		"build-subagent-prompt":    true,
		"verify-subagent-dispatch": true,
		"record-fact":              true, "record-fragment": true, "fill-fact-slot": true, "emit-yaml": true,
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
		if sess.Current == PhaseScaffold {
			if err := populateSourceRootsForScaffold(sess); err != nil {
				r.Error = err.Error()
				return r
			}
		}
		snap := sess.Snapshot()
		r.Status = &snap
		r.Guidance = loadPhaseEntry(sess.Current)
		r.OK = true
	case "complete-phase":
		r = completePhase(sess, in, r)
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
	case "build-subagent-prompt":
		r = handleBuildSubagentPrompt(sess, in, r)
	case "verify-subagent-dispatch":
		r = verifyDispatch(sess, in, r)
	case "record-fact":
		if in.Fact == nil {
			r.Error = "record-fact: fact payload is required"
			return r
		}
		if err := sess.RecordFact(*in.Fact); err != nil {
			r.Error = err.Error()
			return r
		}
		// V-1 — notice when the classifier auto-overrides the agent's
		// surfaceHint to self-inflicted. The fact is recorded either way
		// (the override only affects publish-time routing), but the
		// notice gives the author a chance to course-correct on the next
		// call.
		if _, notice := ClassifyWithNotice(*in.Fact); notice != "" {
			r.Notice = notice
		}
		r.OK = true
	case "record-fragment":
		r = handleRecordFragment(sess, in, r)
	case "fill-fact-slot":
		r = handleFillFactSlot(sess, in, r)
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

// handleBuildSubagentPrompt implements the build-subagent-prompt
// dispatch branch. Extracted from dispatch's switch (run-16 §6.2/§7.1
// added engine-fact seeding + FactsLog threading, pushing the inline
// branch over the maintainability index threshold).
func handleBuildSubagentPrompt(sess *Session, in RecipeInput, r RecipeResult) RecipeResult {
	// Run-16 §7.1 / §5.3 — seed engine-emitted facts to the session's
	// FactsLog so the dispatched sub-agent can fill empty slots via
	// fill-fact-slot. Per-codebase shells emit on every codebase-bound
	// kind; tier_decision facts emit on env-content. Idempotent —
	// duplicate topics are skipped.
	if err := seedEngineEmittedFacts(sess, BriefKind(in.BriefKind), in.Codebase); err != nil {
		r.Error = err.Error()
		return r
	}
	// Run-16 §6.2 — content briefs (codebase-content, env-content)
	// thread the FactsLog snapshot so the agent sees deploy-phase
	// recorded porter_change / field_rationale / contract facts +
	// engine-emitted shells side-by-side.
	var factsSnapshot []FactRecord
	if sess.FactsLog != nil {
		recs, fErr := sess.FactsLog.Read()
		if fErr != nil {
			r.Error = fErr.Error()
			return r
		}
		factsSnapshot = recs
	}
	prompt, err := buildSubagentPromptForPhase(sess.Plan, sess.Parent, in, sess.Current, sess.MountRoot, factsSnapshot)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	r.Prompt, r.OK = prompt, true
	return r
}

// handleFillFactSlot implements the fill-fact-slot dispatch branch.
// Run-16 §6.4 — agent fills empty slots on an engine-emitted fact shell
// (per-managed-service IG items, worker no-HTTP heading, tier_decision
// tierContext). The merged record replaces the shell in-place via
// FactsLog.ReplaceByTopic.
func handleFillFactSlot(sess *Session, in RecipeInput, r RecipeResult) RecipeResult {
	if in.Fact == nil {
		r.Error = "fill-fact-slot: fact payload is required"
		return r
	}
	if err := sess.FillFactSlot(*in.Fact); err != nil {
		r.Error = err.Error()
		return r
	}
	r.OK = true
	return r
}

// handleRecordFragment implements the record-fragment dispatch branch.
// Extracted from dispatch's switch for cyclomatic-complexity hygiene
// (run-15 F.2/F.3 added contract attachment + classification refusal,
// pushing the inline branch over the maintainability threshold).
func handleRecordFragment(sess *Session, in RecipeInput, r RecipeResult) RecipeResult {
	if in.FragmentID == "" {
		r.Error = "record-fragment: fragmentId is required"
		return r
	}
	// F.3 — classification × surface compatibility refusal. Runs BEFORE
	// recordFragment so an incompatible classification never touches the
	// plan's fragment store.
	//
	// Run-19 prep: surfaces that admit MULTIPLE compatible classes
	// (KB takes platform-invariant + intersection; IG takes
	// platform-invariant + scaffold-decision-config + scaffold-decision-
	// code) REQUIRE classification at record-time. Run-18 surfaced the
	// failure mode: codebase-content-app submitted 5 KB bullets without
	// Classification, the optional check skipped, and four bullets with
	// agent-set candidateClass of framework-quirk / library-metadata /
	// self-inflicted shipped to porter-facing KB despite spec §337-347
	// forbidding any surface for those classes. Single-class surfaces
	// (zerops-yaml-comments, CLAUDE.md, intro) keep classification
	// optional — the surface itself disambiguates.
	if surf, ok := SurfaceFromFragmentID(in.FragmentID); ok {
		// Intro fragments (`codebase/<h>/intro`, `env/<N>/intro`,
		// `root/intro`) are 1-2 sentence engine-shaped extracts — they
		// don't carry classified facts, so the require-classification
		// rule doesn't apply even though the legacy SurfaceFromFragmentID
		// maps codebase intros to IG. Other intro fragmentIDs are
		// multi-surface in their own right.
		isIntro := strings.HasSuffix(in.FragmentID, "/intro") || in.FragmentID == fragmentIDRoot
		if !isIntro && in.Classification == "" && surfaceRequiresClassification(surf) {
			r.Error = fmt.Sprintf(
				"record-fragment: classification is required for fragments on surface %q (multiple spec-compatible classes; engine cannot disambiguate). Set the `classification` field to one of: %s. See docs/spec-content-surfaces.md#classification--surface-compatibility.",
				surf, surfaceClassesList(surf))
			return r
		}
		if in.Classification != "" {
			if err := classificationCompatibleWithSurface(Classification(in.Classification), surf); err != nil {
				r.Error = "record-fragment: " + err.Error()
				return r
			}
		}
	}
	// Run-16 §8 — slot-shape refusal at record-fragment time. Per-fragment-id
	// structural caps (line counts, heading counts, prohibited tokens) move
	// from finalize-validator post-hoc detection to record-time refusal so
	// the agent gets same-context recovery. Closes R-15-3, R-15-4 (in concert
	// with §6.7a's claudemd-author Zerops-free brief), R-15-5.
	//
	// Implementation note: §8.2 specified `r.OK = false; r.Notice = violation`
	// but a Notice + OK=true semantics lets the agent proceed past a slot
	// violation, defeating the same-context-recovery loop. Set r.Error +
	// implicit OK=false instead so the agent's record-fragment call clearly
	// fails and it knows to re-author. The refusal message text matches the
	// Notice prose in §8.1's table (R-id named, spec section cited).
	if violations := checkSlotShapeWithPlan(in.FragmentID, in.Fragment, sess.Plan); len(violations) > 0 {
		// Run-17 §10 — aggregate refusal. KB and CLAUDE.md surfaces
		// can carry multiple offenders per body; surfacing them all in
		// one round-trip cuts the run-16 CLAUDE.md churn (8 successive
		// single-violation refusals) to one re-author cycle.
		if len(violations) == 1 {
			r.Error = "record-fragment: " + violations[0]
		} else {
			r.Error = fmt.Sprintf("record-fragment: %d offenders\n  - %s",
				len(violations),
				strings.Join(violations, "\n  - "))
		}
		return r
	}
	// Plan-services-aware claude-md check — extends checkClaudeMDAll
	// with per-hostname leakage refusal. Catches managed-service
	// hostnames the static-token list can't enumerate (db, cache,
	// search, meilisearch …).
	if singleSlotClaudeMDRe.MatchString(in.FragmentID) && sess.Plan != nil {
		for _, svc := range sess.Plan.Services {
			if svc.Kind != ServiceKindManaged {
				continue
			}
			if violation := claudeMDFragmentRefusalForHostname(in.Fragment, svc.Hostname); violation != "" {
				r.Error = "record-fragment: " + violation
				return r
			}
		}
	}

	// Run-17 §9.5 — refinement-phase Replace transactional wrapper.
	// On PhaseRefinement Replace of a codebase/<host>/... fragment, run
	// surface validators pre- and post-Replace; if the Replace
	// introduces a new blocking violation that wasn't present before,
	// revert the fragment to its pre-Replace body and surface a notice.
	// Per the refinement contract: per-fragment edit cap = 1, so this
	// is the agent's only attempt; the rollback prevents a degraded
	// refinement from persisting.
	wrapRefinement := sess.Current == PhaseRefinement && in.Mode == modeReplace
	host := codebaseHostFromFragmentID(in.FragmentID)
	var preBlocking []Violation
	if wrapRefinement && host != "" {
		preBlocking = refinementPreCheckScoped(sess, host)
	}

	bodyBytes, appended, priorBody, err := recordFragment(sess, in.FragmentID, in.Fragment, in.Mode)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	r.FragmentID = in.FragmentID
	r.BodyBytes = bodyBytes
	r.Appended = appended
	r.PriorBody = priorBody

	if wrapRefinement && host != "" {
		postBlocking := refinementPreCheckScoped(sess, host)
		if newBlocking := newViolationsIntroduced(preBlocking, postBlocking); len(newBlocking) > 0 {
			sess.RestoreFragment(in.FragmentID, priorBody)
			r.BodyBytes = len(priorBody)
			for _, v := range newBlocking {
				r.Notices = append(r.Notices, Violation{
					Code:     "refinement-replace-reverted",
					Path:     in.FragmentID,
					Severity: SeverityNotice,
					Message: fmt.Sprintf(
						"post-replace validator surfaced %s on %s — fragment reverted to its pre-refinement body. %s",
						v.Code, v.Path, v.Message),
				})
			}
		}
	}

	// F.2 — attach the per-surface contract for the resolved fragment id
	// so the agent reads reader / test / caps verbatim at authoring
	// decision time, not just at brief-preface time.
	if surf, ok := SurfaceFromFragmentID(in.FragmentID); ok {
		if c, ok := ContractFor(surf); ok {
			contract := c
			r.SurfaceContract = &contract
		}
	}
	r.OK = true
	return r
}

// refinementPreCheckScoped runs the codebase-surface-validators gate
// scoped to the named codebase. Used by the refinement transactional
// wrapper to compare pre- and post-Replace blocking violations. Errors
// degrade gracefully — a missing codebase or scoping failure returns
// nil so the wrapper falls through (the unwrapped recordFragment path
// already wrote the new body).
func refinementPreCheckScoped(sess *Session, host string) []Violation {
	gates := []Gate{{Name: "codebase-surface-validators", Run: gateCodebaseSurfaceValidators}}
	blocking, _, err := sess.CompletePhaseScoped(gates, host)
	if err != nil {
		return nil
	}
	return blocking
}

// newViolationsIntroduced returns the post-state violations whose
// (Code, Path) tuple was absent from the pre-state violation set —
// i.e. the violations the Replace introduced. A pre-state notice that
// remains in post-state does not count as new; a fresh blocking
// violation triggered by the Replace does.
func newViolationsIntroduced(pre, post []Violation) []Violation {
	preSet := make(map[string]bool, len(pre))
	for _, v := range pre {
		preSet[v.Code+"\x00"+v.Path] = true
	}
	var diff []Violation
	for _, v := range post {
		key := v.Code + "\x00" + v.Path
		if preSet[key] {
			continue
		}
		diff = append(diff, v)
	}
	return diff
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
		// Run-12 §Y3 — derive SupportsHA from the service family at
		// merge time so the yaml emitter never has to second-guess the
		// agent's payload. Conservative default for unknown families
		// is false (NON_HA emit).
		cur.Services = make([]Service, len(incoming.Services))
		for i, s := range incoming.Services {
			if s.Kind == ServiceKindManaged && !s.SupportsHA {
				s.SupportsHA = managedServiceSupportsHA(s.Type)
			}
			cur.Services[i] = s
		}
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
	if len(incoming.FeatureKinds) > 0 {
		cur.FeatureKinds = incoming.FeatureKinds
	}
	sess.Plan = cur
	// Snapshot before releasing the lock so file IO runs unlocked
	// (CLAUDE.md "Hold mutexes during I/O" convention).
	planSnapshot := *cur
	outputRoot := sess.OutputRoot
	sess.mu.Unlock()

	return WritePlan(outputRoot, &planSnapshot)
}

// completePhase runs the gate set for the current phase and advances
// state on success.
//
// Run-13 §3: for scaffold + feature, auto-stitches per-codebase
// surfaces first so codebase validators see freshly-authored fragments
// — eliminating the "remember to call stitch-content before complete-
// phase" ritual that has no porter-facing meaning.
//
// Run-13 §G2: when in.Codebase is set, runs the codebase-scoped
// validators against just that codebase — the sub-agent's pre-
// termination self-validate path. Phase advance only fires on the
// no-codebase form (main-agent's post-sub-agent-return path);
// scoped close is a self-validate, not a state transition.
func completePhase(sess *Session, in RecipeInput, r RecipeResult) RecipeResult {
	if in.Codebase != "" {
		// Validate the requested codebase before doing any stitch
		// work — keeps "unknown codebase" errors clean (no
		// pre-stitch noise) and avoids materializing surfaces for a
		// typo'd hostname.
		if err := validateCodebaseHostname(sess.Plan, in.Codebase); err != nil {
			r.Error = "complete-phase: " + err.Error()
			return r
		}
	}
	if sess.Current == PhaseScaffold || sess.Current == PhaseFeature {
		if err := stitchCodebases(sess); err != nil {
			r.Error = "complete-phase: pre-stitch codebases: " + err.Error()
			return r
		}
	}
	if in.Codebase != "" {
		// Sub-agent's pre-termination self-validate. Run-17 §8 — pick
		// the gate set matching the current phase so scaffold/feature
		// scoped close runs only the fact-quality gates and codebase-
		// content scoped close runs the surface validators.
		var scopedGates []Gate
		//exhaustive:ignore — fall-through covers Research/Provision/Env/Finalize.
		switch sess.Current {
		case PhaseScaffold, PhaseFeature:
			scopedGates = CodebaseScaffoldGates()
		case PhaseCodebaseContent:
			scopedGates = CodebaseContentGates()
		default:
			scopedGates = CodebaseGates()
		}
		blocking, notices, err := sess.CompletePhaseScoped(scopedGates, in.Codebase)
		if err != nil {
			r.Error = err.Error()
			return r
		}
		snap := sess.Snapshot()
		r.Violations, r.Notices, r.Status = blocking, notices, &snap
		r.OK = len(blocking) == 0
		// No phase advance, no Guidance — scoped close doesn't
		// transition. Sub-agent re-calls until ok:true and terminates.
		return r
	}
	blocking, notices, err := sess.CompletePhase(gatesForPhase(sess.Current))
	if err != nil {
		r.Error = err.Error()
		return r
	}
	r.Violations, r.Notices = blocking, notices
	r.OK = len(blocking) == 0
	if r.OK {
		if next, ok := nextPhase(sess.Current); ok {
			// Run-18: finalize → refinement is always-on. Snapshot/
			// restore (run-17 §9 T4) wraps every refinement
			// `record-fragment mode=replace` so a regression-causing
			// edit reverts; the editorial pass therefore costs at most
			// the wall-time of one extra sub-agent dispatch and never
			// makes the artifact worse. Mandatory refinement closes
			// run-17's failure mode where the agent saw notices and
			// declined the optional pass.
			//
			// All other phase transitions remain explicit (the agent
			// calls `enter-phase` after a sub-agent terminates).
			// Refinement is the one place the engine drives the
			// transition because it's the ALWAYS-ON quality gate.
			if sess.Current == PhaseFinalize {
				if eErr := sess.EnterPhase(next); eErr != nil {
					r.Error = "complete-phase: auto-advance to refinement: " + eErr.Error()
					return r
				}
			}
			r.Guidance = "Next phase: " + string(next) + "\n\n" + loadPhaseEntry(next)
		}
	}
	snap := sess.Snapshot()
	r.Status = &snap
	return r
}

// verifyDispatch implements the verify-subagent-dispatch action: the
// engine recomposes the brief identified by briefKind+codebase and
// confirms its body appears byte-identical inside the dispatched
// prompt. Wrapper text around the brief (header before, context
// after) is allowed — only truncations and paraphrases are rejected.
// Run-12 §D; run-13 §4 clarified position semantics.
func verifyDispatch(sess *Session, in RecipeInput, r RecipeResult) RecipeResult {
	if in.BriefKind == "" {
		r.Error = "verify-subagent-dispatch: briefKind required"
		return r
	}
	if in.DispatchedPrompt == "" {
		r.Error = "verify-subagent-dispatch: dispatchedPrompt required"
		return r
	}
	expected, err := buildBriefForRequest(sess, in)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	if !strings.Contains(in.DispatchedPrompt, expected.Body) {
		r.Error = fmt.Sprintf(
			"verify-subagent-dispatch: dispatch missing engine brief body. "+
				"Engine brief is %d bytes; dispatched prompt is %d bytes. "+
				"Pass brief.body byte-identical — main agent must NOT paraphrase or truncate.",
			len(expected.Body), len(in.DispatchedPrompt),
		)
		return r
	}
	r.OK = true
	return r
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

	if err := validateCodebaseSourceRoots(plan); err != nil {
		return nil, err
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

	// Per-codebase apps-repo shape — README + CLAUDE.md land at
	// <cb.SourceRoot>/ alongside source, matching the reference
	// apps-repo shape at /Users/fxck/www/laravel-showcase-app/.
	// The scaffold-authored zerops.yaml already lives there; no copy.
	// SourceRoot validation already happened upfront (M-1).
	// Run-10-readiness §L.
	cbMissing, err := writeCodebaseSurfaces(plan)
	if err != nil {
		return nil, err
	}
	missing = append(missing, cbMissing...)

	return missing, nil
}

// validateCodebaseSourceRoots enforces M-1 (run-11): every codebase
// SourceRoot must be absolute and end in `dev` — the SSHFS-mounted
// dev slot. Run 10 closed with SourceRoot carrying bare hostnames,
// causing README/CLAUDE to land at cwd-relative paths nothing reads.
// Fail loud upfront so the regression cannot recur invisibly.
// Background: docs/zcprecipator3/runs/10/ANALYSIS.md §3 gap M.
func validateCodebaseSourceRoots(plan *Plan) error {
	for _, cb := range plan.Codebases {
		if cb.SourceRoot == "" {
			return fmt.Errorf("codebase %q has no SourceRoot — scaffold did not run or was skipped", cb.Hostname)
		}
		if !filepath.IsAbs(cb.SourceRoot) {
			return fmt.Errorf("stitch refused: codebase %q has non-absolute SourceRoot %q (expected absolute path ending in 'dev'). This indicates the gap-M regression — see docs/zcprecipator3/runs/10/ANALYSIS.md §3 gap M",
				cb.Hostname, cb.SourceRoot)
		}
		if !strings.HasSuffix(cb.SourceRoot, "dev") {
			return fmt.Errorf("stitch refused: codebase %q has SourceRoot %q without 'dev' suffix (expected SSHFS dev slot, e.g. /var/www/%sdev). This indicates the gap-M regression — see docs/zcprecipator3/runs/10/ANALYSIS.md §3 gap M",
				cb.Hostname, cb.SourceRoot, cb.Hostname)
		}
	}
	return nil
}

// stitchCodebases writes per-codebase README + CLAUDE.md to each
// <cb.SourceRoot>/ — the apps-repo shape. Used at scaffold + feature
// complete-phase to materialize fragments authored in-phase so the
// codebase surface validators (which read from disk) can see them.
// Root + env surfaces are NOT written here; finalize owns those.
// Run-13 §3.
func stitchCodebases(sess *Session) error {
	sess.mu.Lock()
	plan := sess.Plan
	sess.mu.Unlock()
	if plan == nil {
		return errors.New("nil plan")
	}
	if err := validateCodebaseSourceRoots(plan); err != nil {
		return err
	}
	_, err := writeCodebaseSurfaces(plan)
	return err
}

// writeCodebaseSurfaces is the shared codebase-write loop used by both
// stitchContent and stitchCodebases. Returns missing fragment ids
// surfaced by the assemble pipeline; caller decides whether to treat
// them as fatal.
func writeCodebaseSurfaces(plan *Plan) ([]string, error) {
	var missing []string
	for _, cb := range plan.Codebases {
		readmeBody, m, err := AssembleCodebaseREADME(plan, cb.Hostname)
		if err != nil {
			return nil, fmt.Errorf("assemble codebase %s README: %w", cb.Hostname, err)
		}
		missing = append(missing, m...)
		if err := writeSurfaceFile(filepath.Join(cb.SourceRoot, "README.md"), readmeBody); err != nil {
			return nil, err
		}
		claudeBody, m, err := AssembleCodebaseClaudeMD(plan, cb.Hostname)
		if err != nil {
			return nil, fmt.Errorf("assemble codebase %s CLAUDE.md: %w", cb.Hostname, err)
		}
		missing = append(missing, m...)
		if err := writeSurfaceFile(filepath.Join(cb.SourceRoot, "CLAUDE.md"), claudeBody); err != nil {
			return nil, err
		}
	}
	return missing, nil
}

// DefaultSourceRoot is the convention-based SSHFS mount path where the
// scaffold sub-agent authors a codebase. Every codebase hostname `<h>`
// materializes as `<h>dev` (mountable) + `<h>stage` (cross-deploy
// target); the authoring workspace is always the dev slot.
func DefaultSourceRoot(hostname string) string {
	return "/var/www/" + hostname + "dev"
}

// populateSourceRootsForScaffold fills empty Codebase.SourceRoot fields
// with the convention-based path at the moment scaffold authoring
// begins. Explicit values (chain-resolver or non-standard mount) are
// preserved. Run-9-readiness Workstream A2.
//
// After mutation, persists the refreshed Plan to <outputRoot>/plan.json
// so on-disk replay tooling sees the post-scaffold-entry state (with
// SourceRoots populated) and not the pre-scaffold initial plan.
func populateSourceRootsForScaffold(sess *Session) error {
	sess.mu.Lock()
	if sess.Plan == nil {
		sess.mu.Unlock()
		return nil
	}
	for i, cb := range sess.Plan.Codebases {
		if cb.SourceRoot == "" {
			sess.Plan.Codebases[i].SourceRoot = DefaultSourceRoot(cb.Hostname)
		}
	}
	snapshot := *sess.Plan
	outputRoot := sess.OutputRoot
	sess.mu.Unlock()

	return WritePlan(outputRoot, &snapshot)
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
