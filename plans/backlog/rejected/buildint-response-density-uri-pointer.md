# REJECTED: build-integration response density → zerops_knowledge URI pointer

**Why rejected** (2026-06-17): the proposed fix — replace inline blocks with a
`zerops_knowledge uri="zerops://...setup-build-integration-actions"` pointer — targets
an INLINE atom (`phases:[strategy-setup], priority:1`, not `reference:true`), which
`resolveAtomURI` explicitly rejects ("delivered inline, not by URI"): a dead pointer.
The "carries everything twice in one turn" premise is also false — the atom renders
during strategy-setup, BEFORE the action; the build-integration confirm response is
the RESULT (sequential surfaces, not duplicated). The one real seam (PAT-scope drift)
is subsumed by the F7/F8 work (`TestGhPATScope_AtomsAgreeWithOwner` + the topology
single-owner const). Not real friction; root-cause incorrect.

**Refs**: plans/minor-findings-rootcause-2026-06-17.md (F2).
