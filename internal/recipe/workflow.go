package recipe

import (
	"errors"
	"fmt"
	"sync"
)

// Phase is one of the five state-machine phases a recipe run passes
// through. Each phase has an entry guard (precondition) and exit guard
// (gate set) — see AdvancePhase.
type Phase string

const (
	PhaseResearch  Phase = "research"
	PhaseProvision Phase = "provision"
	PhaseScaffold  Phase = "scaffold"
	PhaseFeature   Phase = "feature"
	PhaseFinalize  Phase = "finalize"
)

// Phases returns the phases in execution order.
func Phases() []Phase {
	return []Phase{PhaseResearch, PhaseProvision, PhaseScaffold, PhaseFeature, PhaseFinalize}
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

// CompletePhase marks the current phase done after gate evaluation. If
// gates return any Violation the phase stays open; the caller sees the
// violations and iterates before retrying.
func (s *Session) CompletePhase(gates []Gate) ([]Violation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Completed[s.Current] {
		return nil, nil // already complete
	}
	ctx := GateContext{
		Plan:       s.Plan,
		OutputRoot: s.OutputRoot,
		FactsLog:   s.FactsLog,
		Parent:     s.Parent,
	}
	violations := RunGates(gates, ctx)
	if len(violations) > 0 {
		return violations, nil
	}
	s.Completed[s.Current] = true
	return nil, nil
}

// RecordFact appends a fact to the session's facts-log after validation.
// Returns an error if validation fails (required field missing).
func (s *Session) RecordFact(f FactRecord) error {
	if s.FactsLog == nil {
		return errors.New("session has no FactsLog")
	}
	return s.FactsLog.Append(f)
}

// BuildBrief composes a brief for a sub-agent dispatch. Kind picks the
// composer; caller supplies the codebase (scaffold only).
func (s *Session) BuildBrief(kind BriefKind, cb Codebase) (Brief, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	facts, err := s.readFacts()
	if err != nil {
		return Brief{}, err
	}

	switch kind {
	case BriefScaffold:
		return BuildScaffoldBrief(s.Plan, cb, s.Parent)
	case BriefFeature:
		return BuildFeatureBrief(s.Plan)
	case BriefWriter:
		return BuildWriterBrief(s.Plan, facts, s.Parent)
	default:
		return Brief{}, fmt.Errorf("unknown brief kind %q", kind)
	}
}

// EmitYAML renders the import.yaml for a tier. Thread-safe; safe to call
// during or after any phase.
func (s *Session) EmitYAML(tierIndex int) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return EmitImportYAML(s.Plan, tierIndex)
}

func (s *Session) readFacts() ([]FactRecord, error) {
	if s.FactsLog == nil {
		return nil, nil
	}
	return s.FactsLog.Read()
}

// Status returns a snapshot summary for handlers to return from
// zerops_recipe action=status.
type Status struct {
	Slug       string
	Current    Phase
	Completed  []Phase
	Codebases  int
	Services   int
	FactsCount int
}

// Snapshot returns the current session status.
func (s *Session) Snapshot() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	completed := make([]Phase, 0, len(s.Completed))
	for p := range s.Completed {
		if s.Completed[p] {
			completed = append(completed, p)
		}
	}
	factsCount := 0
	if s.FactsLog != nil {
		if records, err := s.FactsLog.Read(); err == nil {
			factsCount = len(records)
		}
	}
	cbs, svcs := 0, 0
	if s.Plan != nil {
		cbs = len(s.Plan.Codebases)
		svcs = len(s.Plan.Services)
	}
	return Status{
		Slug:       s.Slug,
		Current:    s.Current,
		Completed:  completed,
		Codebases:  cbs,
		Services:   svcs,
		FactsCount: factsCount,
	}
}
