# Marker line-anchoring — residual edge cases

**Surfaced**: 2026-07-04, adversarial multi-agent review (3 lenses × refute-verify,
9 findings) of the `content.IndexMarkerLine` line-anchored marker-matching fix
(managed-section / REFLOG markers no longer match a mid-line prose mention). The
one HIGH finding (AGENTS.md data loss via the reflog-drop branch) was fixed and
pinned in the shipping commit; the items below are the confirmed-but-deferred
remainder.

**Why deferred**: none is a data-loss through a realistic user flow after the
reflog-drop branch was deleted. They are either a separate subsystem, a
hand-mutated file ZCP never writes, a pre-existing (non-regression) arithmetic
quirk, or cosmetic duplication on a nonstandard-but-clean file. The core fix +
its data-loss hardening + the `TestNoRawMarkerMatching` drift guard cover the
real user exposure.

**Trigger to promote**: a real report of any symptom below on a live install, or
a maintainer hitting the sync `ZEROPS_EXTRACT` corruption during a recipe push.

## Deferred items (all verify-confirmed real, severity as noted)

1. **[parallel path] `internal/sync/transform.go` `InjectFragment` has the SAME
   defect class unfixed.** The `ZEROPS_EXTRACT_START/END:<name>` markers match via
   raw `strings.Contains` (looser than the emitted marker — no comment wrapper, no
   trailing `#`), so a curated recipe README that MENTIONS its own sync markers in
   prose above the real marker pair has every maintainer line between the mention
   and the real START marker swallowed on `zcp sync push recipes <slug>`. Live at
   `push_recipes.go:273-281`. Maintainer-only + mitigated by the mandated
   preview-diff discipline (corruption shows in the dry-run / PR diff) → low. Fix:
   line-anchor that scanner too (its own matcher — it is line-based skip logic, not
   a literal reuse of `IndexMarkerLine`). This is the check-parallel-paths item —
   do NOT silently drop.

2. **[low] Trailing-whitespace / indented / BOM marker line → duplicate managed
   block.** `markerLineAnchored` requires the marker to be the whole line (no
   trailing space, no leading indent/BOM). A clean block whose marker line carries
   stray horizontal whitespace (editor/agent rewrite) or a leading UTF-8 BOM
   (legacy Notepad / PowerShell 5.1 `Out-File`) no longer matches → init prepends a
   SECOND block; the stale one is frozen below forever (no self-heal). No data loss.
   ZCP never writes these shapes; needs an external editor. Fix option: tolerate
   leading BOM + surrounding horizontal whitespace in the anchor (careful: keep the
   front-of-line anchor so a mid-sentence mention still can't match), or a one-time
   dedupe migration.

3. **[low] `upsertManagedSection` append branch + dangling anchored BEGIN deletes
   user content on the NEXT run** — ssh-config path only, and only for an
   END-before-BEGIN ordering ZCP never writes (hand-mutated file). The new
   end-after-begin search made deletion newly reachable for that specific input
   (old code duplicated instead). Guard: when an anchored BEGIN exists but no
   anchored END follows, refuse/neutralize rather than append.

4. **[low] CRLF arithmetic mismatch (pre-existing, NOT a regression of this diff).**
   `markerLineAnchored` now blesses CRLF marker lines, but the caller byte-arithmetic
   (`removeReflogSections` sectionEnd bump, `refreshManagedFile` / `upsertManagedSection`
   endLineEnd bump, the leading-newline drop) is still LF-only, so a CRLF file leaves
   a stray `\r` / a leftover blank `\r\n` line after a rewrite. Byte-identical to
   pre-fix behavior. Fix: make the caller bump CRLF-aware.

## Refs
- Review transcript: workflow run `wf_ea9a7222-fd2` (journal.jsonl in the session
  subagents dir).
- Fix commit: the `content.IndexMarkerLine` introduction + reflog-drop-branch
  deletion + `TestNoRawMarkerMatching` guard.
