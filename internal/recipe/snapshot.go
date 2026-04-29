package recipe

// Run-17 §9.5 — snapshot/restore primitive for refinement-phase
// transactional Replace. The refinement sub-agent's record-fragment
// mode=replace at PhaseRefinement is wrapped in a transaction: the
// engine snapshots the fragment body before applying the replacement,
// runs post-replace validators, and reverts to the snapshot when any
// new violation surfaces. Lets the sub-agent reshape voice / KB stem
// / IG fusion without risking a regression that would have to be
// caught downstream.

// SnapshotFragment returns the current body of a fragment-id, or "" if
// not recorded yet. Used by refinement to preserve original content
// before Replace, so post-Replace validators can revert when the
// refinement degrades quality.
func (s *Session) SnapshotFragment(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Plan == nil || s.Plan.Fragments == nil {
		return ""
	}
	return s.Plan.Fragments[id]
}

// RestoreFragment writes body back as the fragment's recorded body,
// bypassing slot_shape + classification refusal. Used only by the
// refinement validator-revert path. Pinned by
// TestRestoreFragment_BypassesValidators.
func (s *Session) RestoreFragment(id, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Plan == nil {
		return
	}
	if s.Plan.Fragments == nil {
		s.Plan.Fragments = map[string]string{}
	}
	s.Plan.Fragments[id] = body
}
