package recipe

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
