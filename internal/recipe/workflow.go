package recipe

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Phase is one of the seven state-machine phases a recipe run passes
// through. Each phase has an entry guard (precondition) and exit guard
// (gate set) — see AdvancePhase. Run-16 §6.1 added codebase-content +
// env-content between feature and finalize so deploy phases stop
// authoring documentation surfaces; content sub-agents read recorded
// facts + on-disk artifacts and synthesize all surfaces.
type Phase string

const (
	PhaseResearch        Phase = "research"
	PhaseProvision       Phase = "provision"
	PhaseScaffold        Phase = "scaffold"
	PhaseFeature         Phase = "feature"
	PhaseCodebaseContent Phase = "codebase-content" // run-16 §6.1
	PhaseEnvContent      Phase = "env-content"      // run-16 §6.1
	PhaseFinalize        Phase = "finalize"
	PhaseRefinement      Phase = "refinement" // run-17 §9
)

// Phases returns the phases in execution order. Run-17 §9 — refinement
// runs post-finalize as a single transactional pass over the stitched
// output; the refinement sub-agent reshapes voice / KB stem / IG
// fusion against the rubric and reverts on validator violation.
func Phases() []Phase {
	return []Phase{
		PhaseResearch, PhaseProvision, PhaseScaffold, PhaseFeature,
		PhaseCodebaseContent, PhaseEnvContent, PhaseFinalize, PhaseRefinement,
	}
}

// phaseIndex returns the zero-based index of a phase, or -1 if unknown.
func phaseIndex(p Phase) int {
	for i, q := range Phases() {
		if q == p {
			return i
		}
	}
	return -1
}

// Session is one recipe run's live state. Thread-safe — handlers acquire
// the session mutex before mutating.
type Session struct {
	mu         sync.Mutex
	Slug       string
	Current    Phase
	Plan       *Plan
	FactsLog   *FactsLog
	Parent     *ParentRecipe
	OutputRoot string
	// EngineVersion is the running zcp binary version that authored this
	// session. It is threaded into GateContext so recipe gates never import
	// internal/server.
	EngineVersion string
	// MountRoot is the recipes-mount root (typically the
	// zeropsio/recipes clone directory) used by the chain Resolver and
	// the scaffold brief's reachable-recipe-slug enumeration. Empty
	// when the session was created without a Store-attached mount root.
	MountRoot string
	// Completed records phases whose exit gates passed.
	Completed map[Phase]bool
	// RefinementDispatched flips to true the first time
	// `build-subagent-prompt briefKind=refinement` returns ok. The
	// `complete-phase phase=finalize` handler refuses unless this flag
	// is set so the always-on quality gate (system.md §3 phase 8)
	// stops being silently skipped. Run-23 F-26.
	RefinementDispatched bool
	// Refinement2Dispatched flips to true the first time
	// `build-subagent-prompt briefKind=refinement2` returns ok. The
	// `complete-phase phase=refinement` handler refuses unless this
	// flag is set — refinement-1 walks per-fragment rules; refinement-
	// 2 is the cross-surface audit pass (KB↔IG duplication, surface-
	// misplacement, aspirational-as-current, yaml-comment-content-
	// drift). Run-40 dogfood ([plans/run-40-validation.md]) shipped
	// six cross-surface defects that refinement-1 ran clean over.
	// Run-41.
	Refinement2Dispatched bool
	// RefinementClosed flips to true when `complete-phase
	// phase=refinement` returns ok. The export gate
	// (`zcp sync recipe export`) refuses unless this flag is set —
	// dispatch-attempted is not the same as phase-completed; an agent
	// that crashes mid-dispatch leaves RefinementDispatched=true but
	// RefinementClosed=false. Run-23 F-26.
	RefinementClosed bool

	// Refinement2Ledger holds the walked-ledger receipt the sub-agent
	// emits alongside its findings. Run-46 Item 1 — the close-gate
	// refuses refinement-phase close when the ledger does not cover
	// every manifest entry the engine enumerated. nil when no ledger
	// has been received yet; set by `enrich-findings` action when the
	// caller forwards the sub-agent's `walked` array.
	Refinement2Ledger *Refinement2Ledger

	// BatchedRefinement2Ledger is the in-flight accumulator for Run-47
	// Item H's typed multi-batch enrich-findings API. Non-nil while at
	// least one but not all batches in {1..TotalBatches} have arrived;
	// the engine promotes its Walked union to Refinement2Ledger and
	// clears this field when the last batch lands. Close-gate refuses
	// refinement-phase close while this field is non-nil so an
	// incomplete batch set surfaces as a refusal naming the missing
	// batch ids.
	BatchedRefinement2Ledger *BatchedLedger

	// parentResolved guards lazy parent resolution. LoadParent runs at
	// most once per session — subsequent calls return the cached Parent.
	// True after the first LoadParent attempt regardless of outcome
	// (cache the "no parent" decision so repeat callers don't re-walk
	// the embed.FS + filesystem mount probe).
	parentResolved bool
}

// NewSession bootstraps a session at PhaseResearch with an empty plan.
// FactsLog + OutputRoot are caller-supplied (handlers know the run dir).
func NewSession(slug, engineVersion string, factsLog *FactsLog, outputRoot string, parent *ParentRecipe) *Session {
	if engineVersion == "" {
		engineVersion = devEngineVersion
	}
	return &Session{
		Slug:          slug,
		Current:       PhaseResearch,
		Plan:          &Plan{Slug: slug, EngineVersion: engineVersion},
		FactsLog:      factsLog,
		Parent:        parent,
		OutputRoot:    outputRoot,
		EngineVersion: engineVersion,
		Completed:     map[Phase]bool{},
	}
}

// LoadParent resolves the parent recipe lazily and caches the result
// on the session. First caller triggers ResolveChain; subsequent
// callers read the cache. Errors other than ErrNoParent abort and
// surface to the caller; ErrNoParent is the legitimate "no parent
// for this slug" path and leaves sess.Parent nil with parentResolved
// true so the cache short-circuits subsequent calls.
//
// Used by every brief composer that needs the parent body
// (scaffold/codebase-content/env-content/refinement) and by the
// surface validator that takes ctx.Parent. Research/provision/feature
// gates don't call LoadParent and the resolution is skipped for those
// paths — the prior eager-load at session start did wasted work for
// every phase whose consumers don't read parent content.
func (s *Session) LoadParent() (*ParentRecipe, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.parentResolved {
		return s.Parent, nil
	}
	s.parentResolved = true
	parent, err := ResolveChain(Resolver{MountRoot: s.MountRoot}, s.Slug)
	if err != nil && !errors.Is(err, ErrNoParent) {
		// Reset parentResolved so a future call can retry — only
		// definite outcomes (parent found, or ErrNoParent) cache.
		s.parentResolved = false
		return nil, fmt.Errorf("load parent: %w", err)
	}
	s.Parent = parent
	return s.Parent, nil
}

// EnterPhase transitions the session into the named phase. Returns an
// error if the transition is not adjacent-forward or if the previous
// phase hasn't completed.
//
// Run-47 Item I — accept the call as a silent no-op when the requested
// phase is ALREADY in s.Completed. Sub-agents (env-content, refinement)
// self-close their phase via CompletePhase; the main agent's subsequent
// stale `enter-phase` call would otherwise refuse as non-adjacent. The
// carve-out is narrow: only completed phases are accepted; arbitrary
// "in the past" jumps still refuse so genuine skipped-phase errors
// remain caught. Handler attaches a Notice (handlers.go::enterPhaseAction)
// so the main agent sees that a sub-agent self-closed the phase.
func (s *Session) EnterPhase(p Phase) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := phaseIndex(s.Current)
	next := phaseIndex(p)
	if next < 0 {
		return fmt.Errorf("unknown phase %q", p)
	}
	if p == s.Current {
		return nil // idempotent on current phase
	}
	if s.Completed[p] {
		return nil // idempotent on already-completed phase (run-47 Item I)
	}
	if next != cur+1 {
		return fmt.Errorf("phase transition %q → %q not adjacent-forward", s.Current, p)
	}
	if !s.Completed[s.Current] {
		return fmt.Errorf("cannot enter %q: current phase %q not completed", p, s.Current)
	}
	s.Current = p
	return nil
}

// PhaseCompleted reports whether the named phase is in the session's
// Completed set. Thread-safe; takes the session mutex. Used by
// handlers.go::enterPhaseAction to attach a structured Notice on the
// completed-phase idempotency path so a sub-agent self-close is visible
// to the main agent (run-47 Item I).
func (s *Session) PhaseCompleted(p Phase) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Completed[p]
}

// CompletePhase marks the current phase done after gate evaluation.
// Violations are partitioned by severity: blocking findings hold the
// phase open and surface in the first return; notice findings flow
// through as the second return without affecting completion. The
// blocking-vs-notice split exists so DISCOVER-side lessons can reach
// the agent without the engine pre-encoding them as publish-blocking
// gates (system.md §4).
func (s *Session) CompletePhase(gates []Gate) (blocking, notices []Violation, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Completed[s.Current] {
		return nil, nil, nil // already complete
	}
	ctx := GateContext{
		Plan:          s.Plan,
		OutputRoot:    s.OutputRoot,
		FactsLog:      s.FactsLog,
		Parent:        s.Parent,
		EngineVersion: s.EngineVersion,
	}
	blocking, notices = PartitionBySeverity(RunGates(gates, ctx))
	if len(blocking) > 0 {
		return blocking, notices, nil
	}
	s.Completed[s.Current] = true
	return nil, notices, nil
}

// CompletePhaseScoped runs the given gate set against a Plan whose
// Codebases slice is filtered to just `codebase`. Used by the sub-
// agent's pre-termination self-validate path — surface validators
// fire only against the named codebase's content. Phase state is NOT
// mutated; this is a self-validate, not a transition. Run-13 §G2.
//
// Returns an error when the codebase is not in s.Plan.Codebases. The
// caller (completePhase) typically pre-validates via
// validateCodebaseHostname for a richer error; this guard keeps the
// helper safe for direct callers.
func (s *Session) CompletePhaseScoped(gates []Gate, codebase string) (blocking, notices []Violation, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Plan == nil {
		return nil, nil, errors.New("CompletePhaseScoped: nil plan")
	}
	scopedPlan := *s.Plan
	scopedPlan.Codebases = nil
	for _, cb := range s.Plan.Codebases {
		if cb.Hostname == codebase {
			scopedPlan.Codebases = append(scopedPlan.Codebases, cb)
			break
		}
	}
	if len(scopedPlan.Codebases) == 0 {
		return nil, nil, fmt.Errorf("codebase %q not in plan", codebase)
	}
	ctx := GateContext{
		Plan:          &scopedPlan,
		OutputRoot:    s.OutputRoot,
		FactsLog:      s.FactsLog,
		Parent:        s.Parent,
		EngineVersion: s.EngineVersion,
	}
	blocking, notices = PartitionBySeverity(RunGates(gates, ctx))
	return blocking, notices, nil
}

// RecordFact appends a fact to the session's facts-log after validation.
// Returns an error if validation fails (required field missing).
func (s *Session) RecordFact(f FactRecord) error {
	if s.FactsLog == nil {
		return errors.New("session has no FactsLog")
	}
	return s.FactsLog.Append(f)
}

// seedEngineEmittedFacts appends engine-emitted fact shells + tier_decision
// facts to the session's FactsLog at brief-dispatch time. Idempotent: if
// the topic already exists in the log, the fact is skipped (the agent may
// have filled it via fill-fact-slot already).
//
// Run-16 §7.1 / §5.3 — engine emits at build-subagent-prompt rather than
// at update-plan to keep the timing tight (agent sees freshly-emitted
// shells in the dispatched brief). Codebase-bound kinds emit per-codebase
// shells; env-content emits tier_decision facts; other kinds no-op.
func seedEngineEmittedFacts(sess *Session, kind BriefKind, codebaseHostname string) error {
	if sess == nil || sess.FactsLog == nil || sess.Plan == nil {
		return nil
	}

	existing, err := sess.FactsLog.Read()
	if err != nil {
		return err
	}
	exists := make(map[string]bool, len(existing))
	for _, f := range existing {
		exists[f.Topic] = true
	}

	var toEmit []FactRecord

	switch kind {
	case BriefScaffold, BriefCodebaseContent, BriefClaudeMDAuthor:
		if codebaseHostname == "" {
			return nil
		}
		var cb Codebase
		found := false
		for _, c := range sess.Plan.Codebases {
			if c.Hostname == codebaseHostname {
				cb, found = c, true
				break
			}
		}
		if !found {
			return nil
		}
		toEmit = EmittedFactsForCodebase(sess.Plan, cb)
	case BriefEnvContent:
		toEmit = EmittedTierDecisionFacts(sess.Plan)
	case BriefFeature, BriefFinalize, BriefRefinement, BriefRefinement2:
		// no engine-emit at these kinds — refinement (run-17 §9) +
		// refinement2 (run-41) read the recorded facts log; engine-
		// emit is research-phase only.
		return nil
	}

	for _, f := range toEmit {
		if exists[f.Topic] {
			continue
		}
		if err := sess.FactsLog.Append(f); err != nil {
			return err
		}
	}
	return nil
}

// FillFactSlot merges agent-supplied slot values into a previously
// engine-emitted fact identified by topic. Run-16 §6.4 — used at
// codebase-content phase to fill empty Why / CandidateHeading / Library
// on per-managed-service shells (§7.2), the worker no-HTTP fact's
// CandidateHeading (§7.1), or to extend tier_decision TierContext.
//
// The merge preserves the original Topic, Kind, CandidateClass,
// CandidateSurface, CitationGuide; the agent overrides Why,
// CandidateHeading, Library, Diff, TierContext when those are non-empty
// in the input. EngineEmitted flips to false on merge so the validator
// in Validate runs the full per-Kind required-field check on the now-
// agent-owned record.
func (s *Session) FillFactSlot(in FactRecord) error {
	if s.FactsLog == nil {
		return errors.New("session has no FactsLog")
	}
	if in.Topic == "" {
		return errors.New("fill-fact-slot: factTopic is required")
	}
	existing, err := s.FactsLog.Read()
	if err != nil {
		return err
	}
	var prior *FactRecord
	for i := range existing {
		if existing[i].Topic == in.Topic {
			prior = &existing[i]
			break
		}
	}
	if prior == nil {
		return fmt.Errorf("fill-fact-slot: no fact with topic %q", in.Topic)
	}
	if !prior.EngineEmitted {
		return fmt.Errorf("fill-fact-slot: fact %q is not engine-emitted (only engine shells accept slot fills)", in.Topic)
	}

	merged := *prior
	merged.EngineEmitted = false
	if in.Why != "" {
		merged.Why = in.Why
	}
	if in.CandidateHeading != "" {
		merged.CandidateHeading = in.CandidateHeading
	}
	if in.Library != "" {
		merged.Library = in.Library
	}
	if in.Diff != "" {
		merged.Diff = in.Diff
	}
	if in.TierContext != "" {
		merged.TierContext = in.TierContext
	}
	return s.FactsLog.ReplaceByTopic(merged)
}

// BuildBrief composes a brief for a sub-agent dispatch. Kind picks the
// composer; caller supplies the codebase (scaffold only) and optional
// featurePass (BriefFeature only). For non-feature kinds the pass
// argument is ignored.
//
// CLAUDE.md convention — copy under lock, release, then I/O.
// FactsLog.Read() touches disk; holding s.mu across that read serialized
// every concurrent BuildBrief on a single I/O. The function snapshots
// the per-Session inputs the composers need (Plan, Parent, MountRoot,
// OutputRoot, FactsLog handle) under the lock, releases, then runs the
// composer + any FactsLog read. The composers receive plain values that
// were copied / are themselves immutable from the call site's
// perspective.
func (s *Session) BuildBrief(kind BriefKind, cb Codebase, featurePass FeaturePass) (Brief, error) {
	s.mu.Lock()
	plan := s.Plan
	parent := s.Parent
	mountRoot := s.MountRoot
	outputRoot := s.OutputRoot
	factsLog := s.FactsLog
	s.mu.Unlock()

	switch kind {
	case BriefScaffold:
		var resolver *Resolver
		if mountRoot != "" {
			resolver = &Resolver{MountRoot: mountRoot}
		}
		return BuildScaffoldBriefWithResolver(plan, cb, parent, resolver)
	case BriefFeature:
		return BuildFeatureBrief(plan, featurePass)
	case BriefFinalize:
		return BuildFinalizeBrief(plan)
	case BriefCodebaseContent, BriefEnvContent:
		// Run-16 §6.2 — content briefs read FactsLog so the codebase-
		// content sub-agent sees the deploy-phase agents' recorded
		// porter_change / field_rationale / tier_decision facts. The
		// FactsLog read happens here (Session-level) rather than inside
		// the composer so the package-level composer stays plan-pure.
		var factsSnapshot []FactRecord
		if factsLog != nil {
			recs, err := factsLog.Read()
			if err != nil {
				return Brief{}, fmt.Errorf("read facts log for %s brief: %w", kind, err)
			}
			factsSnapshot = recs
		}
		if kind == BriefCodebaseContent {
			return BuildCodebaseContentBrief(plan, cb, parent, factsSnapshot)
		}
		return BuildEnvContentBrief(plan, parent, factsSnapshot)
	case BriefClaudeMDAuthor:
		return BuildClaudeMDBrief(plan, cb)
	case BriefRefinement:
		var factsSnapshot []FactRecord
		if factsLog != nil {
			recs, err := factsLog.Read()
			if err != nil {
				return Brief{}, fmt.Errorf("read facts log for refinement brief: %w", err)
			}
			factsSnapshot = recs
		}
		return BuildRefinementBrief(plan, parent, outputRoot, factsSnapshot)
	case BriefRefinement2:
		var factsSnapshot []FactRecord
		if factsLog != nil {
			recs, err := factsLog.Read()
			if err != nil {
				return Brief{}, fmt.Errorf("read facts log for refinement2 brief: %w", err)
			}
			factsSnapshot = recs
		}
		return BuildRefinement2Brief(plan, parent, outputRoot, factsSnapshot)
	default:
		return Brief{}, fmt.Errorf("unknown brief kind %q", kind)
	}
}

// EmitYAML renders an import.yaml for the given shape.
//
//   - ShapeWorkspace: services-only yaml for `zerops_import content=<yaml>`
//     at provision. tierIndex is ignored. Not written to disk — the agent
//     hands the string directly to zerops_import.
//
//   - ShapeDeliverable: published-template yaml for tier tierIndex, written
//     to <outputRoot>/<tier.Folder>/import.yaml so the finalize gate can
//     verify presence.
//
// Thread-safe; mutex released before disk I/O.
func (s *Session) EmitYAML(shape Shape, tierIndex int) (string, error) {
	s.mu.Lock()
	plan := s.Plan
	outputRoot := s.OutputRoot
	s.mu.Unlock()

	switch shape {
	case ShapeWorkspace:
		return EmitWorkspaceYAML(plan)
	case ShapeDeliverable:
		yaml, err := EmitDeliverableYAML(plan, tierIndex)
		if err != nil {
			return "", err
		}
		if outputRoot != "" {
			tier, _ := TierAt(tierIndex)
			dir := filepath.Join(outputRoot, tier.Folder)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return "", fmt.Errorf("create tier dir: %w", err)
			}
			path := filepath.Join(dir, "import.yaml")
			if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
				return "", fmt.Errorf("write import.yaml: %w", err)
			}
		}
		return yaml, nil
	default:
		return "", fmt.Errorf("unknown yaml shape %q (want %q or %q)", shape, ShapeWorkspace, ShapeDeliverable)
	}
}

// Status returns a snapshot summary for handlers to return from
// zerops_recipe action=status.
//
// GateRefusals (Run-47 Item D) summarizes counts per gate per phase
// read from <outputRoot>/.gate-refusals.jsonl. Surfaces visibility
// into engine-side gates that fire at sub-agent boundaries (Item 3
// snapshot/restore wrapper, Item 4 friendly-auth floor, Item 7 self-
// referential KB linter, Item B citation validator revert). Nil when
// no refusals have landed for this session.
type Status struct {
	Slug         string                    `json:"slug"`
	Current      Phase                     `json:"current"`
	Completed    []Phase                   `json:"completed"`
	Codebases    int                       `json:"codebases"`
	Services     int                       `json:"services"`
	FactsCount   int                       `json:"factsCount"`
	GateRefusals map[string]map[string]int `json:"gateRefusals,omitempty"`
}

// Snapshot returns the current session status.
//
// Run-47 Item D — `GateRefusals` summarizes counts per gate per phase
// from the on-disk ledger. The session lock is released before the
// ledger read so the I/O happens unlocked (CLAUDE.md "Hold mutexes
// during I/O").
func (s *Session) Snapshot() Status {
	s.mu.Lock()
	completed := make([]Phase, 0, len(s.Completed))
	for p, done := range s.Completed {
		if done {
			completed = append(completed, p)
		}
	}
	var factsCount, cbs, svcs int
	if s.FactsLog != nil {
		if r, err := s.FactsLog.Read(); err == nil {
			factsCount = len(r)
		}
	}
	if s.Plan != nil {
		cbs, svcs = len(s.Plan.Codebases), len(s.Plan.Services)
	}
	slug, current, outputRoot := s.Slug, s.Current, s.OutputRoot
	s.mu.Unlock()
	var gateRefusals map[string]map[string]int
	if outputRoot != "" {
		if entries, err := ReadGateRefusalLedger(outputRoot); err == nil {
			gateRefusals = SummarizeGateRefusals(entries)
		} else {
			fmt.Fprintf(os.Stderr, "snapshot: read gate-refusal ledger: %v\n", err)
		}
	}
	return Status{
		Slug: slug, Current: current, Completed: completed,
		Codebases: cbs, Services: svcs, FactsCount: factsCount,
		GateRefusals: gateRefusals,
	}
}
