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
)

// Phases returns the phases in execution order.
func Phases() []Phase {
	return []Phase{
		PhaseResearch, PhaseProvision, PhaseScaffold, PhaseFeature,
		PhaseCodebaseContent, PhaseEnvContent, PhaseFinalize,
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
	// MountRoot is the recipes-mount root (typically the
	// zeropsio/recipes clone directory) used by the chain Resolver and
	// the scaffold brief's reachable-recipe-slug enumeration. Empty
	// when the session was created without a Store-attached mount root.
	MountRoot string
	// Completed records phases whose exit gates passed.
	Completed map[Phase]bool
}

// NewSession bootstraps a session at PhaseResearch with an empty plan.
// FactsLog + OutputRoot are caller-supplied (handlers know the run dir).
func NewSession(slug string, factsLog *FactsLog, outputRoot string, parent *ParentRecipe) *Session {
	return &Session{
		Slug:       slug,
		Current:    PhaseResearch,
		Plan:       &Plan{Slug: slug},
		FactsLog:   factsLog,
		Parent:     parent,
		OutputRoot: outputRoot,
		Completed:  map[Phase]bool{},
	}
}

// EnterPhase transitions the session into the named phase. Returns an
// error if the transition is not adjacent-forward or if the previous
// phase hasn't completed.
func (s *Session) EnterPhase(p Phase) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := phaseIndex(s.Current)
	next := phaseIndex(p)
	if next < 0 {
		return fmt.Errorf("unknown phase %q", p)
	}
	if p == s.Current {
		return nil // idempotent
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
		Plan:       s.Plan,
		OutputRoot: s.OutputRoot,
		FactsLog:   s.FactsLog,
		Parent:     s.Parent,
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
		Plan:       &scopedPlan,
		OutputRoot: s.OutputRoot,
		FactsLog:   s.FactsLog,
		Parent:     s.Parent,
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
	case BriefFeature, BriefFinalize:
		// no engine-emit at these kinds
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
// composer; caller supplies the codebase (scaffold only).
func (s *Session) BuildBrief(kind BriefKind, cb Codebase) (Brief, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch kind {
	case BriefScaffold:
		var resolver *Resolver
		if s.MountRoot != "" {
			resolver = &Resolver{MountRoot: s.MountRoot}
		}
		return BuildScaffoldBriefWithResolver(s.Plan, cb, s.Parent, resolver)
	case BriefFeature:
		return BuildFeatureBrief(s.Plan)
	case BriefFinalize:
		return BuildFinalizeBrief(s.Plan)
	case BriefCodebaseContent, BriefEnvContent:
		// Run-16 §6.2 — content briefs read FactsLog so the codebase-
		// content sub-agent sees the deploy-phase agents' recorded
		// porter_change / field_rationale / tier_decision facts. The
		// FactsLog read happens here (Session-level) rather than inside
		// the composer so the package-level composer stays plan-pure.
		var factsSnapshot []FactRecord
		if s.FactsLog != nil {
			recs, err := s.FactsLog.Read()
			if err != nil {
				return Brief{}, fmt.Errorf("read facts log for %s brief: %w", kind, err)
			}
			factsSnapshot = recs
		}
		if kind == BriefCodebaseContent {
			return BuildCodebaseContentBrief(s.Plan, cb, s.Parent, factsSnapshot)
		}
		return BuildEnvContentBrief(s.Plan, s.Parent, factsSnapshot)
	case BriefClaudeMDAuthor:
		return BuildClaudeMDBrief(s.Plan, cb)
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
type Status struct {
	Slug       string  `json:"slug"`
	Current    Phase   `json:"current"`
	Completed  []Phase `json:"completed"`
	Codebases  int     `json:"codebases"`
	Services   int     `json:"services"`
	FactsCount int     `json:"factsCount"`
}

// Snapshot returns the current session status.
func (s *Session) Snapshot() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	return Status{
		Slug: s.Slug, Current: s.Current, Completed: completed,
		Codebases: cbs, Services: svcs, FactsCount: factsCount,
	}
}
