# Phase 6 — LAND

Entry: owner confirmed the retest pack (`## Run State` `phase:
awaiting-retest` → owner reports it passed). Never enter straight from
ASSEMBLE without that confirmation.

## 1. Code review

Run the `code-review` skill on the branch diff — the whole feature, not one
slice. Every finding: fix it, or disposition it with a one-line reason;
record the disposition in the plan. No finding is silently dropped.

## 2. Spec reconciliation

Compare shipped behavior against the spec §§ promoted at GATE 1 (listed
under `## Promotion`). Fix whichever side is wrong:
- Behavior drifted from the promoted contract → fix the code (it's a bug,
  not grounds for a spec change).
- The promoted contract itself was wrong/incomplete once real
  implementation landed → amend the spec section, not a workaround comment.

Never leave a delta unresolved: LAND is the last checkpoint before the plan
archives and the spec becomes the only surviving source (CLAUDE.md — plans
are never cited as a source).

## 3. Promote PROVE probes to permanent tests

For every `## Evidence Ledger` row with a `promote:` target, confirm that
test exists now, named as specified — cite `Test{Op}_{Scenario}_{Result}` +
its file. A probe whose promotion never landed is a gap: write the test now,
don't archive around it.

## 4. Promotion checklist (`## Promotion` section)

- [ ] Contracts → `docs/spec-*.md` §s (true since GATE 1 — confirm step 2
      found no drift)
- [ ] Every behavior invariant → a permanent test + spec §, both cited
- [ ] CLAUDE.md trap line: **at most 1**, and only if a genuinely new
      cross-cutting trap emerged (one an agent could violate at a new call
      site before a test catches it) — respect the ~30-line trap budget and
      its pruning rules; default to "none"
- [ ] Recipe-specific gotcha (framework/library/dev-workflow, not platform
      mechanics) → `internal/knowledge/recipes/<slug>.md`, never an atom

## 5. Commit and archive

Commit discipline per CLAUDE.md: atomic, each commit compiles/runs/makes
sense alone, no `Co-Authored-By`, no amend. Set `## Run State` `phase:
archived` first (the archived copy is the permanent record), then `git mv
plans/<slug>-<date>.md plans/archive/` and the same for any `.retest.md` /
`.handoff.md` siblings.

## Definition of done — all six, no partial credit

ACx trace green or dispositioned · battery green (`## Verify Trace`, phase
5) · code-review clean · owner retest confirmed · spec reconciled · plan
archived.

Exit: plan (+ siblings) under `plans/archive/`, working tree clean, no open
finding, unresolved spec delta, or unpromoted probe remains.
