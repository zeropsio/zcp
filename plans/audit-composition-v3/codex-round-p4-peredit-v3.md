# Codex round P4 PER-EDIT — Axis N priority 1-3 + SPLIT-CANDIDATE

Date: 2026-04-27
Round type: PER-EDIT (per plan §5 Phase 4 step 5; mandatory for priority 1-3 broad-atom DROP-LEAKs)
Plan reviewed: `plans/atom-corpus-hygiene-followup-2-2026-04-27.md` §5 Phase 4
Reviewer: Codex
Reviewer brief: verify proposed Axis N edits preserve load-bearing signals + cross-link to platform-rules-{local,container} fills the dropped per-env detail.

---

## Round 1 — 2026-04-27 (NEEDS-REVISION)

### Per-edit verification

- **Edit 1** (develop-first-deploy-intro L31-32): APPROVE — load-bearing do-not preserved at L31; HTTP-probe rationale is universal-true; SSHFS recovery is in `develop-platform-rules-container.md:35` and CWD shape in `develop-platform-rules-local.md:13`. Drop is safe.
- **Edit 2** (develop-http-diagnostic L25-27): **NEEDS-REVISION** — body leak removal is correct, but proposed text only cross-links `develop-platform-rules-container`. Per spec §11.6 (`docs/spec-knowledge-distribution.md:659`), Axis N drop must cross-link BOTH platform-rules atoms. A local-env agent with only the container cross-link gets no routing to their CWD-relative log path. Fix: add `develop-platform-rules-local` to `references-atoms` (currently only `[develop-platform-rules-container]` at L6) AND to the body cross-link.
- **Edit 3** (develop-push-git-deploy SPLIT): **NEEDS-REVISION (two concerns)**:
  1. Proposed `(container)` title suffix conflicts with Axis L spec §11.5 (`docs/spec-knowledge-distribution.md:582`) — env-only title tokens drop when axis implies them. Even with planned local-env sibling, the suffix violates Axis L.
  2. Live test dependency: `scenarios_test.go:810` includes local-env deployed push-git envelope; `scenarios_test.go:948` pins atom in union; `corpus_coverage_test.go:624-639` has `develop_local_push_git` fixture with `MustContain: ["git-push", "GIT_TOKEN"]`. Currently the only atom firing on this envelope supplying "GIT_TOKEN" is `develop-push-git-deploy.md`. After tightening, MustContain FAILS.
  Codex framing: "Deferring the local-env gap is acceptable for cycle 3 only if the test fixtures are explicitly updated to reflect it — silently breaking the test is not acceptable."
- **Edit 4** (develop-implicit-webserver L24): APPROVE — `develop-implicit-webserver.md:1` has no env axis; container mount root is in `claude_container.md:5`; local CWD in `develop-platform-rules-local.md:13`. No load-bearing signal lost.
- **Edit 5** (develop-strategy-awareness L13): APPROVE — `develop-strategy-awareness.md:1` has no env axis; "(SSH self-deploy from the dev container)" is Axis N leak (spec §11.6 flags `dev container`). Replacement "direct deploy from your workspace" preserves taxonomy + rendered values.

### Round 1 VERDICT

`VERDICT: NEEDS-REVISION` (edits 2, 3)

### Plan revisions applied (round 1 → round 2)

1. **Edit 2 revised**: add `develop-platform-rules-local` to atom references-atoms + body cross-link.
   - Frontmatter `references-atoms: [develop-platform-rules-container]` → `[develop-platform-rules-container, develop-platform-rules-local]`.
   - Body L26-27 cross-link: "See `develop-platform-rules-container` for the mount-vs-SSH split." → "Per-env access detail in `develop-platform-rules-{container,local}`."

2. **Edit 3 DEFERRED ENTIRELY** to follow-up cycle. Tightening axis without authoring the local-env atom would break tests AND leave a gap. The proper fix needs new atom authoring (out of cycle 3 scope). Status quo (atom fires on local with wrong content) is pre-existing; cycle 3 does not introduce or worsen it. Deferral documented at `plans/audit-composition-v3/deferred-followups-v3.md` DF-1 + plan §5 Phase 4 work-scope step 4.

---

## Round 2 — 2026-04-27

Dispatched: 2026-04-27 (`acfa4b3da903f8d93`).
Status: COMPLETE.

Round 2 scope: verify Edit 2 revision (cross-link added correctly) + confirm Edit 3 deferral acceptance.

### Per-revision validation

- **Revision 2 (Edit 3 deferral)**: APPROVE — DF-1 captures proper-fix path (author local-env atom + tighten axis + update tests); plan §5 Phase 4 step 4 has deferral note pointing to DF-1; `axis-n-candidates.md` SPLIT-CANDIDATE marked DEFERRED with rationale; `develop-push-git-deploy.md` frontmatter unchanged (status quo preserved); deferral preserves pre-existing wrong-content state — cycle 3 doesn't worsen it.

- **Revision 1 (Edit 2 cross-link)**: prompt-confusion artifact — Codex round 2 checked DISK state instead of PROPOSED text. The proposed text in the round-2 prompt verbatim addresses the round-1 concern (adds `develop-platform-rules-local` to references-atoms + body cross-link). Substantive verification deferred to POST-WORK round (which validated the FINAL applied state).

### Round 2 VERDICT

`VERDICT: APPROVE` on Edit 3 deferral; Edit 2 verification carried over to POST-WORK round (`a9a90b863cedaa944` → APPROVE on final applied state). Net: round 2 cleared the deferral decision; POST-WORK closed the Edit 2 loop.
