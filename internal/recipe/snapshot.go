package recipe

import "strings"

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
//
// Env fragments (`env/<N>/import-comments/<target>`) read through
// plan.EnvComments — Run-46 Item 3 extends the snapshot path to the
// env-tier store so the refinement wrapper can preserve env-comment
// bodies symmetrically with codebase fragment bodies.
func (s *Session) SnapshotFragment(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Plan == nil {
		return ""
	}
	if body, ok := snapshotEnvCommentLocked(s.Plan, id); ok {
		return body
	}
	if s.Plan.Fragments == nil {
		return ""
	}
	return s.Plan.Fragments[id]
}

// RestoreFragment writes body back as the fragment's recorded body,
// bypassing slot_shape + classification refusal. Used only by the
// refinement validator-revert path. Pinned by
// TestRestoreFragment_BypassesValidators.
//
// Env fragments restore through ApplyEnvComment (Run-46 Item 3) so the
// typed plan.EnvComments store stays the single source of truth.
func (s *Session) RestoreFragment(id, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Plan == nil {
		return
	}
	if strings.HasPrefix(id, "env/") && strings.Contains(id, "/import-comments/") {
		_ = ApplyEnvComment(s.Plan, id, body)
		return
	}
	if s.Plan.Fragments == nil {
		s.Plan.Fragments = map[string]string{}
	}
	s.Plan.Fragments[id] = body
}

// snapshotEnvCommentLocked reads an env-comment body from plan.EnvComments
// when id is an env/<N>/import-comments/<target> fragment. Caller MUST
// hold the session lock. Returns (body, true) on env-id match (even when
// body is empty — empty means "no prior comment", which is still the
// canonical pre-replace state); (false) otherwise.
func snapshotEnvCommentLocked(plan *Plan, id string) (string, bool) {
	if !strings.HasPrefix(id, "env/") || !strings.Contains(id, "/import-comments/") {
		return "", false
	}
	rest := strings.TrimPrefix(id, "env/")
	slash := strings.IndexByte(rest, '/')
	if slash <= 0 {
		return "", false
	}
	tierKey := rest[:slash]
	target := strings.TrimPrefix(rest[slash+1:], "import-comments/")
	ec, ok := plan.EnvComments[tierKey]
	if !ok {
		return "", true
	}
	if target == "project" {
		return ec.Project, true
	}
	return ec.Service[target], true
}
