package recipe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Store holds live Sessions keyed by slug. One ZCP process may host
// several recipe runs; Store serializes access.
type Store struct {
	mu            sync.Mutex
	sessions      map[string]*Session
	mountRoot     string
	engineVersion string
}

// NewStore returns an empty store whose chain resolver reads from
// mountRoot (typically the zeropsio/recipes clone + zerops-recipe-apps
// mount).
func NewStore(mountRoot string, engineVersion ...string) *Store {
	version := "dev"
	if len(engineVersion) > 0 && engineVersion[0] != "" {
		version = engineVersion[0]
	}
	return &Store{sessions: map[string]*Session{}, mountRoot: mountRoot, engineVersion: version}
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

// CoversHost reports whether any open recipe session's Plan owns the
// given service hostname. Strict matching via Plan.CoversHost — empty
// Plan returns false (NO permissive fallback).
//
// Used by the deploy-adoption gate (internal/tools.requireAdoption) so a
// recipe authoring session can deploy its own `apistage` / `appdev`
// cross-targets without first running the bootstrap workflow. The
// exemption is narrow: only `requireAdoption` consults the probe.
//
// Concurrency: snapshots the session pointers under Store.mu, releases
// the store lock, then takes each Session.mu while reading its Plan.
// Holding Store.mu across Session reads would race with mergePlan and
// other handler code that locks Session.mu under Store.mu first.
func (s *Store) CoversHost(host string) bool {
	if host == "" {
		return false
	}
	s.mu.Lock()
	sessions := make([]*Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	s.mu.Unlock()

	for _, sess := range sessions {
		sess.mu.Lock()
		plan := sess.Plan
		sess.mu.Unlock()
		if plan.CoversHost(host) {
			return true
		}
	}
	return false
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
//
// Rehydration: when no in-memory session exists for slug AND
// <outputRoot>/plan.json exists on disk, the freshly-created session's
// Plan is loaded from disk before return. Run-24 surfaced the gap: a
// sub-agent dispatch runs in a separate MCP server instance from the
// main agent; without rehydration the sub-agent saw `Plan codebases:
// []` while the on-disk plan.json carried the full plan, breaking
// `complete-phase scoped` validation. A read failure is non-fatal — we
// log via the FactsLog rather than refusing the open (a corrupted
// plan.json shouldn't block recipe progress entirely; the agent can
// re-issue update-plan to repair).
func (s *Store) OpenOrCreate(slug, outputRoot string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[slug]; ok {
		return sess, nil
	}
	// F-28 (run-25 §Axis Z) — refuse outputRoot AT or above the SSHFS
	// mount base before MkdirAll touches disk. The mount base hosts the
	// dev codebase mounts; writing recipe outputs there shadows source.
	if err := refuseIfMountBaseAncestor(outputRoot, slug); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(outputRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create output root: %w", err)
	}
	log := OpenFactsLog(filepath.Join(outputRoot, "facts.jsonl"))
	// Parent recipe resolution is lazy — sess.Parent starts nil and is
	// populated by sess.LoadParent() on first consumer demand (scaffold
	// brief composition is the first phase that needs the body to inject
	// the parent baseline section). Research/provision/feature phases
	// have no parent consumer; loading at session start would be wasted
	// work for those paths.
	sess := NewSession(slug, s.engineVersion, log, outputRoot, nil)
	sess.MountRoot = s.mountRoot
	// Rehydrate Plan from disk when plan.json exists. Cross-process
	// continuity: dispatched sub-agents share outputRoot with the main
	// agent's recipe state, so reading the persisted plan keeps their
	// view consistent. ReadPlan fails fast on absent file; we test
	// existence first so an absent plan.json is the no-op fresh path.
	planPath := filepath.Join(outputRoot, "plan.json")
	if _, statErr := os.Stat(planPath); statErr == nil {
		if persisted, readErr := ReadPlan(outputRoot); readErr == nil && persisted != nil {
			sess.Plan = persisted
		}
	}
	s.sessions[slug] = sess
	return sess, nil
}

// errSessionNotOpen is reported when a mutating action arrives for a
// slug that has not been opened via "start".
const errSessionNotOpen = "session not open"

// RecipeInput is the input schema for zerops_recipe.
type RecipeInput struct {
	Action           string      `json:"action"                     jsonschema:"One of: start, enter-phase, complete-phase, build-brief, build-subagent-prompt, verify-subagent-dispatch, record-fact, record-fragment, fill-fact-slot, resolve-chain, emit-yaml, update-plan, stitch-content, status, enrich-findings. For build-subagent-prompt: bodies > 40 KB return 'briefPath' (absolute path under <outputRoot>/.briefs/) instead of 'prompt'; dispatch the sub-agent with a thin wrapper telling it to Read briefPath first thing. Either-or — branch on briefPath != ''. For enrich-findings: pass the refinement-2 audit sub-agent's slim findings JSON via 'findings' (parsed envelope) or 'findingsJson' (raw fenced block); engine returns 'enrichedFindings' with deterministic fragmentId / classification / suggestedReplacement filled."`
	Slug             string      `json:"slug,omitempty"             jsonschema:"Recipe slug (e.g. {framework}-showcase). Required for every action."`
	OutputRoot       string      `json:"outputRoot,omitempty"       jsonschema:"Directory where the recipe tree + facts log live. Required for 'start'. Canonical shape: '/var/www/zcprecipator/<slug>/' — outputs MUST nest one level under the SSHFS mount base ('/var/www/'); the engine refuses outputRoot at or above the mount base because that path hosts dev-codebase mounts (apidev/, appdev/, workerdev/) and stitched output would shadow source."`
	Phase            string      `json:"phase,omitempty"            jsonschema:"Phase name for enter-phase / complete-phase: research, provision, scaffold, feature, codebase-content, env-content, finalize, refinement."`
	BriefKind        string      `json:"briefKind,omitempty"        jsonschema:"For build-brief: scaffold, feature, codebase-content, claudemd-author, env-content, finalize, refinement, refinement2. refinement2 is the cross-surface audit pass dispatched after refinement-1 closes; both must dispatch before complete-phase phase=refinement can close."`
	Codebase         string      `json:"codebase,omitempty"         jsonschema:"For build-brief when kind=scaffold: the codebase hostname to compose for. For complete-phase: when set, scopes codebase-surface validators to that one codebase only — the sub-agent's pre-termination self-validate path. Phase advance only fires when codebase is empty (the main-agent's post-sub-agent-return path)."`
	Shape            string      `json:"shape,omitempty"            jsonschema:"For emit-yaml: 'workspace' (services-only YAML for zerops_import at provision) or 'deliverable' (full published template for tierIndex, written to disk)."`
	TierIndex        int         `json:"tierIndex,omitempty"        jsonschema:"For emit-yaml shape=deliverable: tier 0..5. Ignored when shape=workspace."`
	Fact             *FactRecord `json:"fact,omitempty"             jsonschema:"For record-fact: a FactRecord object. Required: topic + a kind discriminator picking the validation path. Allowed kind values: 'porter_change' (requires why + candidateClass + candidateSurface — candidateSurface is locked to one of: ROOT_README, ENV_README, ENV_IMPORT_COMMENTS, CODEBASE_IG, CODEBASE_KB, CODEBASE_CLAUDE, CODEBASE_ZEROPS_COMMENTS), 'field_rationale' (requires fieldPath + why), 'tier_decision' (requires tier in 0..5 + fieldPath + chosenValue + why), 'contract' (requires publishers + subscribers + subject + purpose), 'curl_verification' (requires subject + service + why; the close signal for the feature backend pass after the curl smoke-test confirms an endpoint), 'browser_verification' (requires subject + service + why; the close signal for the feature frontend pass after browser-walk confirms a panel renders), 'negation' (requires service + scope + why; records a deliberate ABSENCE — 'this codebase does NOT consume service X' or 'this codebase does NOT author surface Y'; engine reads to skip dead env-var wiring and similar). Empty kind is the legacy platform-trap shape (requires symptom + mechanism + surfaceHint + citation). For porter_change, candidateClass takes one of: platform-invariant, intersection, scaffold-decision, framework-quirk, library-metadata, operational, self-inflicted; the first three are surface-bearing, the rest are skip-classes. There is no separate 'classification' field — the candidateClass slot carries it."`
	Plan             *Plan       `json:"plan,omitempty"             jsonschema:"For update-plan: partial Plan object. Fields present overwrite session.Plan; omitted fields untouched."`
	FragmentID       string      `json:"fragmentId,omitempty"       jsonschema:"For record-fragment: fragment identifier. Valid shapes: root/intro, env/<N>/intro (N=0..5), env/<N>/import-comments/<hostname>, env/<N>/import-comments/project, codebase/<hostname>/intro, codebase/<hostname>/integration-guide, codebase/<hostname>/integration-guide/<n> (slotted, n is the IG item index — engine pre-stamps n=1, agent authors 2+), codebase/<hostname>/knowledge-base, codebase/<hostname>/zerops-yaml (whole commented yaml — one per codebase), codebase/<hostname>/claude-md, codebase/<hostname>/claude-md/service-facts (legacy), codebase/<hostname>/claude-md/notes (legacy)."`
	Fragment         string      `json:"fragment,omitempty"         jsonschema:"For record-fragment: the fragment body. Overwrite for root/* and env/* ids; append-on-extend for codebase/*/integration-guide, knowledge-base, claude-md/* ids so a feature sub-agent extends scaffold's body rather than replacing it."`
	Mode             string      `json:"mode,omitempty"             jsonschema:"For record-fragment: 'append' (default for codebase IG/KB/claude-md ids; concatenates with prior body) or 'replace' (overwrites prior body). Use 'replace' to correct a fragment you authored earlier in the same recipe session, e.g. after a complete-phase validator violation."`
	DispatchedPrompt string      `json:"dispatchedPrompt,omitempty" jsonschema:"For verify-subagent-dispatch: the prompt the main agent intends to pass to Agent. Engine recomposes the brief and confirms its body appears byte-identical inside the dispatched prompt. Wrapper text around the brief (header lines before, context notes after) is allowed; only truncations and paraphrases are rejected."`
	// Classification (run-15 F.3) — optional spec classification for
	// record-fragment. When present, the engine refuses incompatible
	// (classification, fragmentId) pairs per the
	// docs/spec-content-surfaces.md compatibility table. Empty
	// classification keeps prior behavior (no refusal).
	Classification string `json:"classification,omitempty" jsonschema:"For record-fragment: optional fact classification — one of platform-invariant, intersection, framework-quirk, library-metadata, scaffold-decision, operational, self-inflicted. The engine refuses classifications that don't belong on the fragment's surface (e.g. self-inflicted on KB, scaffold-decision on CLAUDE.md); the refusal payload spells out the compatibility rule for the offending pair."`
	// FeaturePass — for build-brief / build-subagent-prompt with
	// briefKind=feature. Run-23 F-21 split the feature phase into a
	// sequential backend pass + frontend-integration pass; the brief
	// composer loads disjoint atom sets per pass.
	FeaturePass string `json:"featurePass,omitempty" jsonschema:"For build-brief / build-subagent-prompt with briefKind=feature: 'backend' (api + worker scope; routes/queue/contract authoring + curl smoke-tests; no design-system / Tailwind atoms) or 'frontend' (SPA / monolith UI scope; design-system + Tailwind componentry + integration validator + bounded cross-codebase edit authority). Required for feature briefs."`
	// Findings + FindingsJSON — input for action=enrich-findings. The
	// refinement-2 audit sub-agent emits a slim findings JSON; the main
	// agent passes the parsed envelope here OR the raw fenced JSON
	// string verbatim. Engine returns the enriched form with
	// fragmentId / classification / suggestedReplacement filled. Run-45
	// Pillar C — closes the run-40 → 44 monkey-patch cycle where the
	// brief demanded `classification` on findings and the agent didn't
	// emit it.
	Findings     *FindingsEnvelope `json:"findings,omitempty"     jsonschema:"For enrich-findings: the refinement-2 audit's slim findings envelope. Either this OR findingsJson must be set."`
	FindingsJSON string            `json:"findingsJson,omitempty" jsonschema:"For enrich-findings: the audit sub-agent's fenced JSON findings block, raw. Engine parses + strips the fence. Either this OR findings must be set."`
	// Walked — Run-46 Item 1 walked-ledger receipt. Sub-agent emits the
	// manifest IDKeys it evaluated alongside its findings; the engine
	// persists the ledger on the session via `enrich-findings` and the
	// refinement close-gate refuses when the ledger doesn't cover every
	// manifest entry.
	Walked []string `json:"walked,omitempty" jsonschema:"For enrich-findings: the refinement-2 audit's walked-ledger — every manifest idKey the sub-agent evaluated. The close-gate refuses refinement-phase close when this set doesn't cover the full manifest. Run-46 Item 1."`
	// CrossSurfaceUniquenessScanned + Duplicates — Run-46 Item 6 ledger
	// (data path lands now alongside Item 1; the close-gate consumer
	// lands in PR 5).
	CrossSurfaceUniquenessScanned int      `json:"crossSurfaceUniquenessScanned,omitempty" jsonschema:"For enrich-findings: count of manifest items the sub-agent compared in the cross-surface uniqueness pass. Run-46 Item 6."`
	Duplicates                    []string `json:"duplicates,omitempty"                    jsonschema:"For enrich-findings: pair references to duplicate teachings flagged in the cross-surface uniqueness pass. Run-46 Item 6."`
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
	// BriefPath is the absolute path to a disk-persisted sub-agent
	// dispatch prompt — populated by action=build-subagent-prompt when
	// the composed body exceeds BriefDiskFallbackThreshold. The main
	// agent dispatches the sub-agent with a thin wrapper telling it to
	// Read briefPath first thing. Either-or semantics with Prompt: when
	// BriefPath is set, Prompt is empty and vice versa. Run-29 Fix #1.
	BriefPath string `json:"briefPath,omitempty"`
	// BriefSize is the byte count of the disk-persisted brief body
	// (NOT the body itself). Sized as an int so the main agent can
	// sanity-check the pointer before dispatching the sub-agent.
	// Populated alongside BriefPath; zero on the inline path.
	// Run-29 Fix #1.
	BriefSize int `json:"briefSize,omitempty"`
	// EnrichedFindings is the enriched-shape response from
	// action=enrich-findings — slim findings PLUS deterministic
	// engine-derived fragmentId / classification / suggestedReplacement.
	// Run-45 Pillar C.
	EnrichedFindings *EnrichedFindingsEnvelope `json:"enrichedFindings,omitempty"`
	Error            string                    `json:"error,omitempty"`
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
		// enrich-findings reads the session's facts log for the
		// classification candidateClass lookup.
		"enrich-findings": true,
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
		r = startAction(store, in, r)
	case "enter-phase":
		r = enterPhaseAction(sess, in, r)
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
		r = recordFactAction(sess, in, r)
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
	case "enrich-findings":
		r = enrichFindingsAction(sess, in, r)
	default:
		r.Error = fmt.Sprintf("unknown action %q", in.Action)
	}
	return r
}

// flipDispatchFlags sets the per-kind "Dispatched" boolean on the
// session for kinds the engine tracks as mandatory pre-close
// dispatches. Run-23 F-26 added BriefRefinement; Run-41 added
// BriefRefinement2. Both flags are read by complete-phase to refuse
// the refinement-phase close until both audit sub-agents have been
// dispatched. Idempotent — single-flip semantics.
func flipDispatchFlags(sess *Session, kind BriefKind) {
	//exhaustive:ignore — only refinement1/refinement2 flip per-kind tracking; other kinds are no-op.
	switch kind {
	case BriefRefinement:
		sess.mu.Lock()
		sess.RefinementDispatched = true
		sess.mu.Unlock()
	case BriefRefinement2:
		sess.mu.Lock()
		sess.Refinement2Dispatched = true
		sess.mu.Unlock()
	}
}

// handleBuildSubagentPrompt implements the build-subagent-prompt
// dispatch branch. Extracted from dispatch's switch (run-16 §6.2/§7.1
// added engine-fact seeding + FactsLog threading, pushing the inline
// branch over the maintainability index threshold).
func handleBuildSubagentPrompt(sess *Session, in RecipeInput, r RecipeResult) RecipeResult {
	// Run-41 — refinement2 is a single cross-codebase audit pass; it
	// must not be dispatched with a codebase= scope. A misuse case
	// (e.g. an agent re-running the refinement pattern from refinement-
	// 1's per-codebase pre-validate path) would flip
	// Refinement2Dispatched on a build that only audited one codebase,
	// skipping the cross-codebase relationship checks the audit
	// exists to run. Reject the call before any side effect lands.
	if BriefKind(in.BriefKind) == BriefRefinement2 && in.Codebase != "" {
		r.Error = "build-subagent-prompt: briefKind=refinement2 does not accept a codebase scope; refinement-2 is a single cross-codebase audit pass over the full stitched deliverable. Drop the codebase parameter and re-dispatch."
		return r
	}
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
	// Run-31 Fix #1 closure — multi-file briefs. Codebase-content,
	// env-content, and refinement persist to disk as index.md + N
	// part-*.md (composer-side); the dispatch envelope returns
	// `briefPath` pointing at index.md. Single-file kinds (scaffold,
	// feature, finalize, claudemd-author) keep the legacy run-29 Fix #1
	// disk-fallback shape: inline below 40 KB, single-file disk pointer
	// above.
	//
	// Lazy parent load: brief composers are the load-bearing consumer
	// of parent content. Trigger LoadParent() here so the composer
	// receives a populated sess.Parent on first dispatch (cached after
	// that). Errors other than ErrNoParent abort the dispatch — a
	// definite "no parent" outcome (hello-world / minimal / unpublished
	// parent) flows through with sess.Parent == nil and the composer's
	// own appendEmbeddedParentBaseline / parent != nil checks handle it.
	if _, lpErr := sess.LoadParent(); lpErr != nil {
		r.Error = lpErr.Error()
		return r
	}
	prompt, indexPath, err := buildSubagentDispatchForPhase(sess.Plan, sess.Parent, in, sess.Current, sess.MountRoot, sess.OutputRoot, factsSnapshot)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	// Run-46 Item 1 — for refinement-2 dispatches, write the surface
	// manifest to disk so the sub-agent can Read it. The brief renders
	// the path; the manifest enumerates every in-scope item across S3 +
	// S4 + S5 + S7. Empty outputRoot (unit-test fixtures) skips silently
	// — the brief's manifest section only renders when runDir != "" too.
	if BriefKind(in.BriefKind) == BriefRefinement2 && sess.OutputRoot != "" {
		if _, mErr := WriteRefinement2Manifest(sess.OutputRoot, sess.Plan); mErr != nil {
			r.Error = "build-subagent-prompt: write refinement-2 manifest: " + mErr.Error()
			return r
		}
	}
	if indexPath != "" {
		// Multi-file path. The composer already wrote the index + parts
		// to disk; populate BriefPath with the index. BriefSize carries
		// the index file's byte count for sanity-check.
		stat, sErr := os.Stat(indexPath)
		if sErr == nil {
			r.BriefSize = int(stat.Size())
		}
		r.BriefPath = indexPath
		r.Notice = "brief written to disk as multi-file index; dispatch sub-agent with this path. Sub-agent must Read index.md, then Read each part file listed in its 'Read order' section in order."
		r.Notice += refinement2MainAgentTriageGuidance(BriefKind(in.BriefKind))
		flipDispatchFlags(sess, BriefKind(in.BriefKind))
		r.OK = true
		return r
	}
	// Run-29 Fix #1 — single-file disk-fallback for oversized briefs.
	// Above the threshold the engine writes the body to
	// `<sess.OutputRoot>/.briefs/<kind>-<codebase>-<unixnano>.md` and
	// returns a pointer instead of inlining. Either-or semantics with
	// Prompt: when BriefPath is set, Prompt is empty. OutputRoot is
	// populated before this handler runs (set at handlers.go NewSession
	// path / OpenOrCreate path). Defense-in-depth: an empty OutputRoot
	// falls through to the inline path, matching legacy in-memory test
	// fixtures that construct sessions without an on-disk root.
	if len(prompt) > BriefDiskFallbackThreshold && sess.OutputRoot != "" {
		path, wErr := writeBriefToDisk(sess.OutputRoot, BriefKind(in.BriefKind), in.Codebase, prompt)
		if wErr != nil {
			r.Error = wErr.Error()
			return r
		}
		r.BriefPath = path
		r.BriefSize = len(prompt)
		r.Notice = "brief written to disk; dispatch sub-agent with this path"
		r.Notice += refinement2MainAgentTriageGuidance(BriefKind(in.BriefKind))
		// Run-23 F-26 / Run-41 — flip the per-kind Dispatched flag only
		// after the brief is actually deliverable (inline or pointer).
		// Earlier flip would let the downstream close-gate pass even
		// when the brief failed to write to disk.
		flipDispatchFlags(sess, BriefKind(in.BriefKind))
		r.OK = true
		return r
	}
	// Run-23 F-26 / Run-41 — flip the per-kind Dispatched flag on a
	// successful brief build so the downstream close-gate can refuse
	// closure until the sub-agent has been dispatched at least once.
	// Single-flip is fine; the gate only reads the boolean.
	flipDispatchFlags(sess, BriefKind(in.BriefKind))
	r.Prompt, r.OK = prompt, true
	if guidance := refinement2MainAgentTriageGuidance(BriefKind(in.BriefKind)); guidance != "" {
		r.Notice += guidance
	}
	return r
}

// refinement2MainAgentTriageGuidance returns the per-finding triage
// contract reminder that surfaces on the MAIN AGENT's view of a
// successful refinement-2 dispatch response. Empty for all other
// brief kinds.
//
// Why this lives on the response.Notice channel: the per-finding
// triage instruction lives inside the refinement-2 SUB-AGENT brief
// (`phase_entry.md §"Per-finding triage is the contract"`), but the
// MAIN AGENT is the one who triages findings post-return. The main
// agent doesn't read the sub-agent's brief — it dispatches the
// sub-agent and reads the sub-agent's returned text. Run-41 dogfood
// ([plans/run-41-validation.md]) shipped with the main agent bulk-
// HOLDING all 10 advisory findings with one-line "ships acceptably"
// because the triage contract was invisible to it. Surface it on
// the dispatch response so the main agent sees the contract at
// dispatch time.
func refinement2MainAgentTriageGuidance(kind BriefKind) string {
	if kind != BriefRefinement2 {
		return ""
	}
	return "\n\nMAIN AGENT — refinement-2 triage contract: when the sub-agent returns its findings JSON block (the slim shape — surface + scope + itemReference + surfaceTestFailureMode + topic + rationale), call `zerops_recipe action=enrich-findings findings=<the parsed envelope>` (or `findingsJson=<the raw fenced block>`) FIRST. The engine returns the enriched form with `fragmentId`, `classification`, and `suggestedReplacement` filled deterministically — copy those fields verbatim onto your `record-fragment mode=replace` ACT calls. This closes the run-40 → 44 cycle where the sub-agent omitted `classification` and your ACTs got refused with `classification is required for fragments on surface \"CODEBASE_KB\"`. AFTER enrichment: record an ACT / HOLD / ACCEPT decision per finding, NOT a bulk dismissal. `advisory` severity does NOT mean ignore — it means YOU triage. Bulk-HOLD with one-line reasoning like `all advisory severity, recipe ships acceptably` is the documented failure pattern and violates the contract. For each finding: ACT (apply the fix via `record-fragment mode=replace` per the surfaceTestFailureMode), HOLD (record per-finding reasoning why the advisory is acceptable for ship — not bulk), or ACCEPT (record one sentence on why the audit fired on a borderline that doesn't actually violate the contract). Blocker-severity HOLD requires contract-anchored justification — name the surface, name the test, explain why this specific instance falls outside the test's scope. The contract exists because severity is a prior, not a verdict; the seven-surface content rules require per-finding judgment."
}

// startAction handles `action=start`. Bootstraps the session, returns
// the research-phase guidance + the parent-status prediction. Extracted
// from dispatch() for maintainability index headroom.
func startAction(store *Store, in RecipeInput, r RecipeResult) RecipeResult {
	if in.OutputRoot == "" {
		r.Error = "outputRoot is required"
		return r
	}
	sess, err := store.OpenOrCreate(in.Slug, in.OutputRoot)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	// Lazy parent shape: sess.Parent is nil here (resolution deferred
	// to scaffold-brief dispatch). parentStatus at start reports the
	// chain prediction — "embedded" / "absent" — from the cheap
	// parentSlugFor check + embed.FS existence probe, without
	// populating the full ParentRecipe body. The agent knows what's
	// coming; the body lands on the first build-subagent-prompt call
	// that needs it.
	snap := sess.Snapshot()
	r.Status = &snap
	r.ParentStatus = predictParentStatus(in.Slug)
	r.Guidance = loadPhaseEntry(sess.Current)
	r.OK = true
	return r
}

// enterPhaseAction handles `action=enter-phase`. Advances the session
// phase and runs scaffold-phase prep when entering scaffold.
func enterPhaseAction(sess *Session, in RecipeInput, r RecipeResult) RecipeResult {
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
	return r
}

// recordFactAction handles `action=record-fact`. Runs the voice-token
// validation, records the fact, then surfaces the V-1 self-inflicted
// override notice + the R3-C-3 surface-classification compatibility
// warning.
func recordFactAction(sess *Session, in RecipeInput, r RecipeResult) RecipeResult {
	if in.Fact == nil {
		r.Error = "record-fact: fact payload is required"
		return r
	}
	// Run-34 Fix 3 — engine-side runtime gate for forbidden recipe-
	// author voice tokens. Brief teaches REQUIRED → engine enforces
	// REQUIRED. Refusal is BLOCKING (not a notice) so the agent cannot
	// ignore the rule at runtime — empirically the notice signal was
	// insufficient (12.8% strict-token contamination rate UNCHANGED
	// from run-33 to run-34 even after the brief edits landed).
	// Validate BEFORE RecordFact so a rejected fact never lands on
	// facts.jsonl.
	if err := validateFactVoiceTokens(*in.Fact); err != nil {
		r.Error = err.Error()
		return r
	}
	if err := sess.RecordFact(*in.Fact); err != nil {
		r.Error = err.Error()
		return r
	}
	// V-1 — notice when the classifier auto-overrides the agent's
	// surfaceHint to self-inflicted. The fact is recorded either way
	// (the override only affects publish-time routing), but the notice
	// gives the author a chance to course-correct on the next call.
	if _, notice := ClassifyWithNotice(*in.Fact); notice != "" {
		r.Notice = notice
	}
	// Run-22 R3-C-3 — warn-on-record when the (candidateClass,
	// candidateSurface) pair violates the spec compatibility table.
	// Fragment-time refusal (validateRecordFragment) still applies
	// downstream; this is the earlier signal so the agent doesn't burn
	// 6+ tool-call hops between record-fact and the eventual fragment
	// refusal. DISCARD classes (framework-quirk, library-metadata,
	// self-inflicted) on any surface trip this; publishable classes on
	// a wrong surface trip this too.
	if r.Notice == "" && in.Fact.CandidateClass != "" && in.Fact.CandidateSurface != "" {
		if err := classificationCompatibleWithSurface(
			Classification(in.Fact.CandidateClass),
			Surface(in.Fact.CandidateSurface),
		); err != nil {
			r.Notice = "record-fact warn: " + err.Error()
		}
	}
	r.OK = true
	return r
}

// enrichFindingsAction handles `action=enrich-findings`. The
// refinement-2 audit sub-agent emits a SLIM findings envelope; this
// handler fills the three deterministic fields (fragmentId,
// classification, suggestedReplacement) by reading the session's
// facts.jsonl + the Citation Map. Run-45 Pillar C.
//
// Input contract: either `findings` (parsed envelope) OR `findingsJSON`
// (raw fenced JSON block). Both empty → error. Both set → `findings`
// wins; `findingsJSON` is ignored.
//
// Reads facts via `sess.FactsLog.Read()`. Missing facts.jsonl is not
// fatal — the engine falls back to per-surface defaults
// (`intersection` for S4/S5) when no fact records `candidateClass` for
// the topic + scope.
func enrichFindingsAction(sess *Session, in RecipeInput, r RecipeResult) RecipeResult {
	env := in.Findings
	if env == nil {
		if in.FindingsJSON == "" {
			r.Error = "enrich-findings: at least one of `findings` (parsed) or `findingsJson` (raw) must be set"
			return r
		}
		parsed, err := ParseFindingsJSON(in.FindingsJSON)
		if err != nil {
			r.Error = fmt.Sprintf("enrich-findings: %v", err)
			return r
		}
		env = parsed
	}
	var facts []FactRecord
	if sess.FactsLog != nil {
		if records, err := sess.FactsLog.Read(); err == nil {
			facts = records
		}
	}
	r.EnrichedFindings = EnrichFindings(env, facts)
	// Run-46 Item 1 — persist the walked-ledger receipt the main agent
	// forwarded alongside the findings. The refinement close-gate reads
	// sess.Refinement2Ledger to refuse close when the ledger doesn't
	// cover the full manifest. We persist on every enrich-findings call
	// (latest-wins) so a re-dispatch + re-enrich path replaces a
	// partial earlier ledger with the most recent emission.
	if len(in.Walked) > 0 || in.CrossSurfaceUniquenessScanned > 0 || len(in.Duplicates) > 0 {
		sess.mu.Lock()
		sess.Refinement2Ledger = &Refinement2Ledger{
			Walked:                        append([]string(nil), in.Walked...),
			CrossSurfaceUniquenessScanned: in.CrossSurfaceUniquenessScanned,
			Duplicates:                    append([]string(nil), in.Duplicates...),
		}
		sess.mu.Unlock()
	}
	r.OK = true
	return r
}

// writeBriefToDisk persists an oversized sub-agent dispatch prompt to
// `<outputRoot>/.briefs/<kind>-<codebase>-<unix>.md` atomically (temp
// file + Sync + rename). Returns the absolute destination path so the
// caller can populate RecipeResult.BriefPath. Run-29 Fix #1.
//
// Atomic-write pattern mirrors stitch_yaml.go's
// CreateTemp+Write+Sync+Close+Rename so a partial-write never leaves a
// half-written file at the target path; the rename is atomic on the
// same filesystem and concurrent readers (the dispatched sub-agent's
// Read call) never see the truncate-then-write window.
func writeBriefToDisk(outputRoot string, kind BriefKind, codebase, body string) (string, error) {
	dir := filepath.Join(outputRoot, ".briefs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create briefs dir: %w", err)
	}
	cb := codebase
	if cb == "" {
		cb = "phase"
	}
	// UnixNano (not Unix) so two large briefs with the same kind +
	// codebase in the same second resolve to distinct destinations;
	// otherwise os.Rename would overwrite the prior brief and the
	// caller's BriefPath pointer races with the next call's write.
	name := fmt.Sprintf("%s-%s-%d.md", kind, cb, time.Now().UnixNano())
	dst := filepath.Join(dir, name)
	tmp, err := os.CreateTemp(dir, ".brief.tmp.*")
	if err != nil {
		return "", fmt.Errorf("create temp brief: %w", err)
	}
	tmpPath := tmp.Name()
	if _, wErr := tmp.WriteString(body); wErr != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("write temp brief: %w", wErr)
	}
	if sErr := tmp.Sync(); sErr != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("sync temp brief: %w", sErr)
	}
	if cErr := tmp.Close(); cErr != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("close temp brief: %w", cErr)
	}
	if rErr := os.Rename(tmpPath, dst); rErr != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("rename temp brief: %w", rErr)
	}
	return dst, nil
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
	// (zerops-yaml whole-yaml, CLAUDE.md, intro) keep classification
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
	// Run-21 R2-5 — per-hostname leakage refusal removed. The
	// 4-letter-hostname matcher (db, cache, search, broker) collided
	// with English prose; brief teaching is the right surface for the
	// Zerops-free CLAUDE.md contract. See
	// `briefs/claudemd-author/zerops_free_prohibition.md`.

	// Run-46 Item 7 — self-referential-naming linter for codebase KB
	// fragments. Scoped to `codebase/<host>/knowledge-base` (KB is
	// where the run-45 violations landed). Refuses backticked tokens
	// that resolve to recipe-internal symbols (src/ filenames, class
	// exports, NestJS module-pattern suffixes). Closes the loop on
	// spec §"Self-referential decoration prohibition" — the principle
	// was in the audit substrate; refinement-2 didn't catch it across
	// run-45. Structural validator at authoring time is the same shape
	// as the named-constant-drift gate.
	if errMsg := selfReferentialKBRefusal(sess, in.FragmentID, in.Fragment); errMsg != "" {
		r.Error = errMsg
		return r
	}

	// Run-20 C1 — facts-attestation check for JetStream framing in
	// env-content import-comment fragments. Run-19 shipped fabricated
	// JetStream framing on a recipe that uses only core pub/sub,
	// because the env-content composer didn't see the NATS atom that
	// distinguishes the two shapes. The atom is now wired in
	// briefs_content_phase.go::BuildEnvContentBrief (C1 layer 1). This
	// refusal is the layer-2 backstop: if an env import-comment body
	// names JetStream / quorum-replicated streams / durable consumers
	// without an attesting `nats-jetstream-*` fact in the FactsLog,
	// refuse with a redirect to record the attestation first.
	if envImportCommentsRe.MatchString(in.FragmentID) {
		if msg := envCommentJetStreamRefusal(in.Fragment, sess.FactsLog); msg != "" {
			r.Error = "record-fragment: " + msg
			return r
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

	// Run-34 Fix 1 — refinement-time env-fragment persistence parity.
	// `record-fragment mode=replace` on `env/<N>/intro` or
	// `env/<N>/import-comments/<target>` lands in plan state via
	// recordFragment / ApplyEnvComment, but without re-stitching the
	// matching tier the published file on disk still holds the prior
	// body. Mirrors the codebase wrapper above; refinement contract for
	// env fragments is "best-effort" for revert (no snapshot/restore
	// gate yet), but persistence-to-disk MUST happen regardless.
	//
	// Diagnosed in plans/run-34-validation.md §"Top 5 surprises" #1.
	if wrapRefinement {
		if tierIdx := envTierIndexFromFragmentID(in.FragmentID); tierIdx >= 0 {
			if err := preStitchEnv(sess, tierIdx); err != nil {
				r.Notices = append(r.Notices, Violation{
					Code:     "refinement-env-stitch-failed",
					Path:     in.FragmentID,
					Severity: SeverityNotice,
					Message: fmt.Sprintf(
						"env-fragment Replace landed in plan state but on-disk stitch failed: %s. Re-run stitch-content to reconcile.",
						err.Error()),
				})
			}
		}
	}

	// Run-40 ENG-1 — refinement-phase plan.json write-back. Pre-fix the
	// Replace path updated sess.Plan + disk-stitched READMEs but never
	// persisted plan.json itself, making the refinement pass lossy at
	// the plan-of-record layer. Extracted to a helper so handleRecord-
	// Fragment stays under the maintainability index threshold.
	if wrapRefinement {
		if notice := persistPlanAfterRefinementReplace(sess, in.FragmentID); notice != nil {
			r.Notices = append(r.Notices, *notice)
		}
	}

	// Run-29 Fix #2 — IG scaffold-filename Notice. The legacy Blocking
	// gate at validators_codebase.go:81-93 banned `migrate.ts` /
	// `seed.ts` / `main.ts` / `api.ts` literals across ALL IG content,
	// including engine-stamped IG #1 yaml that legitimately names the
	// codebase's own initCommands sources; the agent's evasion (delete
	// the engine emit) became the shape that shipped (system.md §4
	// catalog-drift). Notice now scopes to IG fragment text OUTSIDE
	// any ```yaml fenced block so the porter-transferable signal still
	// surfaces but engine emits are never flagged.
	if isIGFragmentID(in.FragmentID) {
		if name, found := igScaffoldFilenameOutsideYamlBlock(in.Fragment); found {
			r.Notices = append(r.Notices, Violation{
				Code:     "codebase-ig-scaffold-filename",
				Path:     in.FragmentID,
				Severity: SeverityNotice,
				Message: fmt.Sprintf(
					"IG step prose mentions a scaffold-source filename %q — porters bringing their own code don't have these. Consider demoting the teaching to a code-comment in the codebase, or rewriting the IG step around the platform contract (init-commands key shape, the env-var alias rule) rather than the recipe's own helper file.",
					name),
			})
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

// persistPlanAfterRefinementReplace snapshots the session's Plan
// under sess.mu, releases the lock, and writes plan.json to disk.
// Returns a non-nil Violation when the disk write failed so the
// caller can attach it as a notice on the record-fragment response.
//
// CLAUDE.md "Hold mutexes during I/O — copy under lock, release,
// then I/O" — snapshot is a shallow copy of the Plan struct under
// the lock; WritePlan runs unlocked.
//
// Run-40 ENG-1 — refinement-phase plan.json write-back. Pre-fix the
// Replace path updated sess.Plan + disk-stitched READMEs but never
// persisted plan.json itself, making the refinement pass lossy at
// the plan-of-record layer. Diagnosed in plans/run-40-evidence-
// grounded-plan.md §"S1-5".
func persistPlanAfterRefinementReplace(sess *Session, fragmentID string) *Violation {
	sess.mu.Lock()
	var snapshot Plan
	var outputRoot string
	if sess.Plan != nil {
		snapshot = *sess.Plan
		outputRoot = sess.OutputRoot
	}
	sess.mu.Unlock()
	if outputRoot == "" {
		return nil
	}
	if err := WritePlan(outputRoot, &snapshot); err != nil {
		return &Violation{
			Code:     "refinement-plan-persist-failed",
			Path:     fragmentID,
			Severity: SeverityNotice,
			Message: fmt.Sprintf(
				"refinement Replace landed in plan-state + disk-stitch, but plan.json write failed: %s. Re-run update-plan to reconcile.",
				err.Error()),
		}
	}
	return nil
}

// checkRefinementCloseGates encapsulates the refinement-phase close
// preconditions (run-41 + run-46 Item 1). Returns the error message
// that should be reported on r.Error, or "" when all preconditions
// pass.
//
//  1. refinement-1 dispatched
//  2. refinement-2 dispatched
//  3. walked-ledger receipt covers every manifest entry (Run-46 Item 1).
func checkRefinementCloseGates(sess *Session) string {
	sess.mu.Lock()
	ref1 := sess.RefinementDispatched
	ref2 := sess.Refinement2Dispatched
	ledger := sess.Refinement2Ledger
	plan := sess.Plan
	sess.mu.Unlock()
	if !ref1 {
		return "complete-phase: phase=refinement requires the refinement-1 sub-agent (intra-fragment rule walk) to be dispatched first; call `zerops_recipe action=build-subagent-prompt briefKind=refinement` and dispatch the agent before closing refinement."
	}
	if !ref2 {
		return "complete-phase: phase=refinement requires the refinement-2 sub-agent (cross-surface audit) to be dispatched after refinement-1 closes; call `zerops_recipe action=build-subagent-prompt briefKind=refinement2` and dispatch the agent before closing refinement. The cross-surface audit catches KB↔IG duplication, surface-misplacement, aspirational-as-current prose, and yaml-comment ↔ yaml-content drift — defect classes refinement-1's per-fragment rule walk cannot see."
	}
	manifest, mErr := BuildRefinement2Manifest(plan)
	if mErr != nil {
		return "complete-phase: build refinement-2 manifest: " + mErr.Error()
	}
	if ledger == nil {
		return "complete-phase: phase=refinement requires the refinement-2 audit's walked-ledger receipt. The sub-agent emits a `walked` array listing the manifest idKey of every item it evaluated; forward it via `zerops_recipe action=enrich-findings walked=<array>` after the sub-agent returns. Run-46 Item 1 closes the run-41 → run-45 pattern where the sub-agent walked partial state and emitted 0 findings on un-walked surfaces."
	}
	if missing := ledger.Missing(manifest); len(missing) > 0 {
		return fmt.Sprintf(
			"complete-phase: phase=refinement walked-ledger covers %d of %d manifest entries; %d entries un-walked. Examples: %s. Re-dispatch refinement-2 and ensure the sub-agent evaluates every manifest entry — zero-finding is acceptable ONLY when the idKey is in `walked` (which proves the surface test ran). Run-46 Item 1.",
			len(ledger.Walked), len(manifest.AllKeys()), len(missing), strings.Join(missingHead(missing, 5), ", "),
		)
	}
	return ""
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
		cur = &Plan{Slug: sess.Slug, EngineVersion: sess.EngineVersion}
	}
	if cur.EngineVersion == "" {
		cur.EngineVersion = sess.EngineVersion
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
	// Run-40 A1 — NamedConstants merge. Records the cross-codebase
	// name-of-record for queue groups, cache prefixes, signing-key
	// aliases.
	//
	// Run-40 fix-up #7 — conflict detection. Plain maps.Copy
	// silently overwrites existing keys, which masks the case where
	// a later update-plan call mutates a canonical value the agent
	// recorded earlier (run-39's "showcase-workers" → "workers"
	// rename was exactly this shape — silent overwrite would leave
	// downstream surfaces racing the rename order). The merge now
	// detects conflicts (same key, different value) and refuses
	// with a surface-able error naming the offending keys. The
	// agent's recovery is to either acknowledge the rename via a
	// dedicated rename action (future) or accept the existing
	// value. New keys + idempotent same-value writes pass through.
	if len(incoming.NamedConstants) > 0 {
		var conflicts []string
		for k, v := range incoming.NamedConstants {
			if existing, ok := cur.NamedConstants[k]; ok && existing != v {
				conflicts = append(conflicts, fmt.Sprintf("%s: existing=%q incoming=%q", k, existing, v))
			}
		}
		if len(conflicts) > 0 {
			sort.Strings(conflicts)
			sess.mu.Unlock()
			return fmt.Errorf("update-plan: NamedConstants conflict — values diverge from prior canonical record:\n  - %s\nRename detection: if this is an intentional rename, drop the old key and add the new one (record both via separate update-plan calls); if accidental, restore the prior value", strings.Join(conflicts, "\n  - "))
		}
		if cur.NamedConstants == nil {
			cur.NamedConstants = map[string]string{}
		}
		maps.Copy(cur.NamedConstants, incoming.NamedConstants)
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
	// Run-43 Edit D / P6 — consolidated refinement state machine.
	// Pre-Edit-D the engine had TWO refinement-dispatch gates: one
	// at finalize-close (demanding RefinementDispatched) and one at
	// refinement-close (demanding BOTH RefinementDispatched +
	// Refinement2Dispatched). Run-42 dogfood produced "three
	// refinement passes, wrong order" runs: main agent dispatched
	// refinement-1 + refinement-2 during finalize-close demand
	// iteration (because the finalize gate refused), then redispatched
	// refinement-1 at refinement phase (because phase-8 entry guidance
	// led there). Run-43 Edit D drops the finalize-phase gate
	// entirely; refinement happens at the refinement phase, and the
	// refinement-close gate (below) enforces both refinement-1 +
	// refinement-2 dispatched AND re-runs surface validators on
	// any ACTs the main agent made on refinement-2 findings. The
	// finalize-phase gate this comment block formerly housed has been
	// removed; see TestCompletePhaseFinalize_DoesNotDemandRefinementDispatch
	// (the inversion pin against re-introduction).
	//
	// Refinement-phase closure refuses unless BOTH refinement
	// sub-agents have been dispatched. Refinement-1 walks per-fragment
	// rules (`derived_rules.md`); refinement-2 walks cross-surface
	// defect classes (KB↔IG duplication, surface-misplacement,
	// aspirational-as-current, yaml-comment-content-drift). Run-40
	// dogfood ([plans/run-40-validation.md]) shipped six cross-surface
	// defects post-refinement-1 close because refinement-1's intra-
	// fragment scope is structurally blind to those classes. The gate
	// fires on the no-codebase main-agent close (refinement-2 is a
	// single-pass main-agent dispatch, not a per-codebase sub-agent).
	if in.Codebase == "" && sess.Current == PhaseRefinement {
		if errMsg := checkRefinementCloseGates(sess); errMsg != "" {
			r.Error = errMsg
			return r
		}
	}
	// Run-21-prep — pre-stitch at codebase-content too so the
	// codebase-content sub-agent's whole-yaml fragment lands on disk
	// before validateCodebaseYAML / gateZeropsYamlSchema read it.
	// Pre-fix, those gates read the bare scaffold yaml and missed
	// regressions in the agent's authored body until finalize.
	//
	// When the call is scoped (in.Codebase != ""), pre-stitch only the
	// requested codebase so an unrelated codebase's missing fragment +
	// empty-yaml hardening doesn't abort the named codebase's self-
	// validate. Preserves the scoped-isolation contract.
	if sess.Current == PhaseScaffold || sess.Current == PhaseFeature || sess.Current == PhaseCodebaseContent {
		if err := preStitchCodebases(sess, in.Codebase); err != nil {
			r.Error = "complete-phase: pre-stitch codebases: " + err.Error()
			return r
		}
	}
	if in.Codebase != "" {
		// Sub-agent's pre-termination self-validate. Run-17 §8 — pick
		// the gate set matching the current phase so scaffold/feature
		// scoped close runs only the fact-quality gates and codebase-
		// content scoped close runs the surface validators.
		//
		// Run-40 fix-up #2 — PhaseFeature scoped close now runs the
		// full FeatureGates() set (was CodebaseScaffoldGates(),
		// missing ENG-5 + B1). The feature sub-agent calls
		// complete-phase with codebase= to self-validate; without the
		// scoped path running the new gates, ENG-5 (ghost-dependency)
		// and B1 (dead-env) never fired during sub-agent termination.
		// Codex code review caught this bypass: the gates were wired
		// to FeatureGates() but the scoped path used a different set.
		// Run-40 B1 also requires env-reads populate before the gate
		// runs; populate fires here too on the scoped path.
		var scopedGates []Gate
		//exhaustive:ignore — fall-through covers Research/Provision/Env/Finalize.
		switch sess.Current {
		case PhaseScaffold:
			scopedGates = CodebaseScaffoldGates()
		case PhaseFeature:
			scopedGates = FeatureGates()
			if pErr := populateEnvReadsFromSource(sess); pErr != nil {
				r.Error = "complete-phase scoped: populate env-reads: " + pErr.Error()
				return r
			}
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
	// Run-40 B1 — populate plan.ObservedFacts.EnvReads via source-grep
	// before feature gates run so gateEnvReadsDerivable can compare
	// declared run.envVariables against authoritative source-derived
	// reads. Runs at feature complete-phase only — scaffold leaves
	// codebases bare and the source-grep would find nothing.
	//
	// Run-40 fix-up #5 — error propagates (previously ignored via
	// `_ = ...`). A failed populate leaves the gate working against
	// stale or missing data; codex code review noted the silent-
	// swallow defeated the gate's defensive contract. The gate's
	// no-entry path (now Blocking per fix-up #6) handles the case
	// where populate ran but found no entries; a hard populate
	// error is an infrastructure-side failure the agent should see.
	if sess.Current == PhaseFeature {
		if pErr := populateEnvReadsFromSource(sess); pErr != nil {
			r.Error = "complete-phase: populate env-reads: " + pErr.Error()
			return r
		}
	}
	blocking, notices, err := sess.CompletePhase(gatesForPhase(sess.Current))
	if err != nil {
		r.Error = err.Error()
		return r
	}
	r.Violations, r.Notices = blocking, notices
	r.OK = len(blocking) == 0
	if r.OK && sess.Current == PhaseScaffold {
		// Run-21 R2-3 — bare scaffold yaml is on disk now (pre-stitch
		// above ran). Parse run.envVariables for `${<host>_*}` /
		// `${<host>}` references and record per-codebase
		// ConsumesServices so the codebase-content brief composer +
		// recipe-context Services block can filter precisely.
		if pErr := populateConsumesServicesFromYaml(sess); pErr != nil {
			r.Error = "complete-phase: populate consumes-services: " + pErr.Error()
			return r
		}
	}
	if r.OK {
		// Run-23 F-26 — flip RefinementClosed when refinement closes.
		// Used by the export gate (`zcp sync recipe export`) so an
		// agent that crashes mid-dispatch (RefinementDispatched=true
		// but the phase never closed) doesn't slip through.
		if sess.Current == PhaseRefinement {
			sess.mu.Lock()
			sess.RefinementClosed = true
			outputRoot := sess.OutputRoot
			sess.mu.Unlock()
			// Persist the close marker to disk so a separate-process
			// export caller can read it without reconstructing the
			// in-memory Session. If the marker write fails, the in-
			// memory state still says RefinementClosed=true but cross-
			// process state diverges (the export gate would refuse).
			// Surface a notice so the user can see the divergence and
			// re-attempt the close (or write the marker by hand).
			if outputRoot != "" {
				if err := writeRefinementCloseMarker(outputRoot); err != nil {
					r.Notices = append(r.Notices, Violation{
						Code:     "refinement-marker-write-failed",
						Path:     outputRoot,
						Message:  "session shows RefinementClosed=true but the on-disk marker did not persist (" + err.Error() + "). The export gate (zcp sync recipe export) reads the marker; without it, export would refuse. Retry complete-phase or write " + RefinementClosedMarkerName + " under outputRoot manually.",
						Severity: SeverityNotice,
					})
				}
			}
		}
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
//
// Run-31 review (HIGH) — multi-file kinds (codebase-content / env-content
// / refinement) compose to disk as index.md + N part files; the legacy
// single-file composers used here return an empty Body for those kinds.
// `strings.Contains(prompt, "")` is always-true in Go, so the previous
// implementation silently passed for ANY dispatched prompt on exactly
// the kinds with the highest paraphrase risk. Refuse multi-file kinds
// with a structured error pointing at the new contract until the
// multi-file verify path lands (assert prompt names the index path;
// assert index file lists each part by absolute path; assert each part
// exists and is non-empty). Pinned by TestVerifyDispatch_MultiFileKindRefused.
func verifyDispatch(sess *Session, in RecipeInput, r RecipeResult) RecipeResult {
	if in.BriefKind == "" {
		r.Error = "verify-subagent-dispatch: briefKind required"
		return r
	}
	if in.DispatchedPrompt == "" {
		r.Error = "verify-subagent-dispatch: dispatchedPrompt required"
		return r
	}
	if isMultiFileBriefKind(BriefKind(in.BriefKind)) {
		r.Error = fmt.Sprintf(
			"verify-subagent-dispatch: kind %q is multi-file (index.md + N part files); "+
				"the single-file Contains-the-body check is a no-op for this kind because the "+
				"composer returns an empty Body. The multi-file verify path is not yet wired — "+
				"the agent should assert (a) the dispatched prompt contains the absolute index "+
				"path, (b) the index file exists and lists each part by absolute path, and "+
				"(c) each part exists and is non-empty. TODO: add multi-file verify path; "+
				"refusing here is safer than silent-passing for exactly the kinds with the "+
				"highest paraphrase risk (run-31 review HIGH).",
			in.BriefKind,
		)
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
	return sess.BuildBrief(BriefKind(in.BriefKind), cb, FeaturePass(in.FeaturePass))
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

	// Resolve each codebase's prod-time runtime base from its on-disk
	// zerops.yaml before regenerating tier yamls. The deliverable
	// emitter reads Codebase.ProdRuntimeBase to set `services[].type`
	// for build/run-asymmetric codebases (Vite SPA: nodejs build,
	// static run); empty value falls back to BaseRuntime (symmetric
	// case). See prod_runtime_base.go.
	if err := populateProdRuntimeBaseFromYaml(sess); err != nil {
		return nil, fmt.Errorf("populate prod runtime base: %w", err)
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

// envTierIndexFromFragmentID returns the tier index encoded in an env
// fragment id (`env/<N>/intro`, `env/<N>/import-comments/<target>`).
// Returns -1 when id is not env-shaped or the index can't be parsed —
// callers fall through to the codebase / non-env path. Run-34 Fix 1.
func envTierIndexFromFragmentID(id string) int {
	rest, ok := strings.CutPrefix(id, "env/")
	if !ok {
		return -1
	}
	slash := strings.IndexByte(rest, '/')
	if slash <= 0 {
		return -1
	}
	idx, err := parseTierIndex(rest[:slash])
	if err != nil {
		return -1
	}
	if _, ok := TierAt(idx); !ok {
		return -1
	}
	return idx
}

// preStitchEnv re-renders ONE tier's env README + import.yaml from
// in-memory plan state and writes both back to disk. Mirror of
// preStitchCodebases for the env-fragment side: a refinement-time
// `record-fragment mode=replace` on `env/<N>/intro` or
// `env/<N>/import-comments/<target>` lands in plan state immediately,
// but without this re-stitch the published file on disk still holds
// the prior body. Pre-fix, every refinement env-fragment ACT was a
// silent no-op.
//
// Diagnosed in plans/run-34-validation.md §"Top 5 surprises" #1
// (six refinement ACTs on tier-prefix intros, none landed). Run-34 Fix 1.
//
// Scoped to one tier so a parallel-run refinement on tier 0 doesn't
// re-emit tiers 1-5 (which would also re-write neighboring import.yamls
// against an unrelated edit). Errors propagate to the caller; persist
// failure surfaces as a record-fragment failure rather than a silent
// disk/state divergence.
func preStitchEnv(sess *Session, tierIndex int) error {
	sess.mu.Lock()
	plan := sess.Plan
	outputRoot := sess.OutputRoot
	sess.mu.Unlock()
	if plan == nil {
		return errors.New("nil plan")
	}
	if outputRoot == "" {
		// Sessions without an output root (early bootstrap) have no disk
		// surface to re-stitch; the in-memory plan state is the only
		// truth and the caller carries it forward via stitch-content.
		return nil
	}
	tier, ok := TierAt(tierIndex)
	if !ok {
		return fmt.Errorf("preStitchEnv: unknown tier index %d", tierIndex)
	}
	envBody, _, err := AssembleEnvREADME(plan, tierIndex)
	if err != nil {
		return fmt.Errorf("assemble env/%d README: %w", tierIndex, err)
	}
	if err := writeSurfaceFile(filepath.Join(outputRoot, tier.Folder, "README.md"), envBody); err != nil {
		return err
	}
	// Re-emit the tier's import.yaml so per-host comments populated via
	// `env/<N>/import-comments/<target>` (which routes through
	// plan.EnvComments) land on disk. Goes through Session.EmitYAML so
	// the write path stays single-source with stitch-content's loop.
	if _, err := sess.EmitYAML(ShapeDeliverable, tierIndex); err != nil {
		return fmt.Errorf("regenerate tier %d import.yaml: %w", tierIndex, err)
	}
	return nil
}

// preStitchCodebases is the scoped pre-stitch path called by
// completePhase. When `only` is non-empty, the stitch loop runs ONLY
// for the matching hostname so an unrelated codebase's stitch failure
// doesn't abort a scoped self-validate (run-21-prep — the F2 empty-disk
// hardening would otherwise leak across codebases). When `only` is empty
// every codebase gets stitched.
func preStitchCodebases(sess *Session, only string) error {
	sess.mu.Lock()
	plan := sess.Plan
	sess.mu.Unlock()
	if plan == nil {
		return errors.New("nil plan")
	}
	if err := validateCodebaseSourceRoots(plan); err != nil {
		return err
	}
	if only == "" {
		_, err := writeCodebaseSurfaces(plan)
		return err
	}
	_, err := writeCodebaseSurfacesScoped(plan, only)
	return err
}

// writeCodebaseSurfacesScoped writes README + CLAUDE.md + zerops.yaml
// for ONE codebase only, mirroring writeCodebaseSurfaces' shape. Used by
// preStitchCodebases for scoped completePhase calls.
func writeCodebaseSurfacesScoped(plan *Plan, hostname string) ([]string, error) {
	single := *plan
	for _, cb := range plan.Codebases {
		if cb.Hostname == hostname {
			single.Codebases = []Codebase{cb}
			return writeCodebaseSurfaces(&single)
		}
	}
	return nil, fmt.Errorf("codebase %q not in plan", hostname)
}

// writeCodebaseSurfaces is the shared codebase-write loop used by both
// stitchContent and preStitchCodebases. Returns missing fragment ids
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
		// Run-23 fix-2 — refuse-to-wipe guard. The README template
		// always carries the codebase H1 + back-link + at least the
		// engine-stamped IG #1 yaml block, so an empty body indicates
		// upstream corruption (template-read fail, marker substitution
		// edge case). Refuse rather than silently writing 0 bytes over
		// a previous round's content. Run-20 prod hit a 0-byte README
		// wipe on appdev/workerdev (TIMELINE Issue 3); ZCP could not
		// reproduce locally — this guard makes the silent-wipe vector
		// closed by construction.
		if strings.TrimSpace(readmeBody) == "" {
			return nil, fmt.Errorf("refuse-to-wipe: assemble codebase %s README produced empty body", cb.Hostname)
		}
		if err := writeSurfaceFile(filepath.Join(cb.SourceRoot, "README.md"), readmeBody); err != nil {
			return nil, err
		}
		claudeBody, m, err := AssembleCodebaseClaudeMD(plan, cb.Hostname)
		if err != nil {
			return nil, fmt.Errorf("assemble codebase %s CLAUDE.md: %w", cb.Hostname, err)
		}
		missing = append(missing, m...)
		if strings.TrimSpace(claudeBody) == "" {
			return nil, fmt.Errorf("refuse-to-wipe: assemble codebase %s CLAUDE.md produced empty body", cb.Hostname)
		}
		if err := writeSurfaceFile(filepath.Join(cb.SourceRoot, "CLAUDE.md"), claudeBody); err != nil {
			return nil, err
		}
		// Run-19 prep — close the run-18 §1.3 bare-runtime-yaml gap.
		// codebase-content sub-agents record yaml-comments fragments;
		// the IG-#1 stitch path embedded them in the README, but no
		// engine step wrote them back to <SourceRoot>/zerops.yaml.
		// run-18 apidev + workerdev shipped bare on-disk yaml because
		// of that gap. WriteCodebaseYAMLWithComments closes it.
		if err := WriteCodebaseYAMLWithComments(plan, cb.Hostname); err != nil {
			return nil, fmt.Errorf("stitch codebase %s zerops.yaml: %w", cb.Hostname, err)
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

// parentStatus tag values. Single-source so the predict path + the
// post-load path (parentStatus()) emit byte-identical tags.
const (
	parentStatusAbsent   = "absent"
	parentStatusEmbedded = "embedded"
	parentStatusMounted  = "mounted"
)

// predictParentStatus reports the parentStatus value the lazy load
// would produce, without actually loading the parent body. Used by
// the `start` handler to give the research-phase agent an accurate
// signal at session open. Cheap: parentSlugFor() is a string-suffix
// check; the embed.FS probe is one fs.Stat against the in-memory
// embed tree. The full body load is deferred to LoadParent() on the
// first brief-composition call.
func predictParentStatus(slug string) string {
	parentSlug := parentSlugFor(slug)
	if parentSlug == "" {
		return parentStatusAbsent
	}
	if _, err := loadEmbeddedRecipeMD(parentSlug); err == nil {
		return parentStatusEmbedded
	}
	return parentStatusAbsent
}

// parentStatus returns a short tag telling the agent how the chain
// resolver found the parent recipe:
//   - "absent"   : no parent exists for this slug (hello-world,
//     *-minimal, or no published parent corpus).
//   - "embedded" : parent resolved from the embedded knowledge corpus
//     (`internal/knowledge/recipes/<slug>.md`). The
//     baseline body is in p.EmbeddedBody and downstream
//     brief composers surface it via the
//     appendEmbeddedParentBaseline path.
//   - "mounted"  : parent resolved from a filesystem-mounted tree
//     (~/recipes/<slug>/ with full codebases + tier
//     import.yamls). Legacy CDE shape.
//
// The research atom branches on this string to tell the agent how to
// read parent content. Run-40 post-ship: "embedded" replaced the
// "absent" misfire for *-showcase slugs whose parent ships in the
// binary but isn't mounted on the local fs.
func parentStatus(p *ParentRecipe) string {
	if p == nil {
		return parentStatusAbsent
	}
	if p.IsEmbedded() {
		return parentStatusEmbedded
	}
	return parentStatusMounted
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
