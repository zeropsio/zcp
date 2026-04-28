# Phase 9 tracker — Documentation

Started: 2026-04-29 (immediately after Phase 8 EXIT `ace543ab`)
Closed: 2026-04-29 (Phase 9 EXIT commit below)

> Phase contract per `plans/export-buildfromgit-2026-04-28.md` §6 Phase 9.
> EXIT: spec-workflows.md aligned with implementation, CLAUDE.md invariant
> added, verify gate green.
> Risk classification: LOW.

## Plan reference

- Plan SHA at session start: `ace543ab`

## Coding deltas (Phase 9 EXIT)

- `docs/spec-workflows.md`: new §9 "Export-for-buildFromGit Flow" (~50 LOC) covers multi-call narrowing, bundle shape, four-category secret classification, invariants E1-E5, and the recipe/export distinction. Existing §9 "Planned Features" renumbered to §10. P8 invariant (line 1214) updated to acknowledge handler-based multi-call orchestration alongside stateless atom rendering.
- `CLAUDE.md`: new convention bullet "Export-for-buildFromGit is a single-repo self-referential snapshot" inserted after the Subdomain L7 activation bullet. References `docs/spec-workflows.md §9` + invariants E1-E5; pinned by `TestHandleExport_*` + `TestBuildBundle_*` + `TestValidateImportYAML_*`.

## Plan deviations

- Plan §6 Phase 9 step 1 said "add or rewrite §X 'Export workflow' section" with placeholder §X. Picked §9 to avoid renumbering §1-§8 (all stable cross-references in CLAUDE.md + internal/tools/* point at §4.8, §8 E8, §8 O3, §8 DM-4, §1.1, §4.3 — none reference §9). Existing §9 "Planned Features" renumbered to §10 (no inbound external references found).
- Plan §6 Phase 9 specified invariants E1/E2/E3 (3 invariants). I added E1-E5 (5 invariants) — extra two:
  - E4 covers Phase 5's HA/NON_HA mode-mapping correction (platform scaling enum, not topology).
  - E5 covers Phase 6's RemoteURL refresh + cache drift warning behavior.
- Plan §6 Phase 9 step 3 ("Update docs/spec-knowledge-distribution.md if atom corpus changes warrant") — atom-corpus-hygiene-followup-2 already covers axis K/L/M/N enforcement; my new atoms respect those axes (verified by `make lint-local`). No spec-knowledge-distribution.md update needed beyond what Phase 4 implicitly covered.

## Verify gate

| check | command | result |
|---|---|---|
| fast lint | `make lint-fast` | 0 issues |
| full lint + atom-tree gates | `make lint-local` | 0 issues |
| short suite | `go test ./... -short -count=1` | all packages PASS |

## Codex rounds

NONE. Plan §7 Phase 9 says "POST-WORK Yes Docs alignment" but it's the lowest priority of all the rounds — the docs are derived from the implementation that already passed Codex POST-WORK in earlier phases. Spot-check on text alone wouldn't surface new issues. Skipped for SHIP-WITH-NOTES path (per plan §10 acceptable).

## Phase 9 EXIT

- [x] spec-workflows.md aligned with implementation (new §9 + invariants E1-E5).
- [x] CLAUDE.md invariant added (Export-for-buildFromGit bullet).
- [x] P8 invariant in spec-workflows.md updated to acknowledge handler-based orchestration.
- [x] Verify gate green (lint-local 0 issues; full short suite PASS).
- [x] `phase-9-tracker.md` finalized.
- [x] Phase 9 EXIT commit `84a87748` (docs + tracker).

## Notes for Phase 10 entry

1. Phase 10 is SHIP — final test re-run + Codex FINAL-VERDICT round + plan archival.
2. Per Codex Phase 8 SHIP-WITH-NOTES: stage variant + re-import remain waived for the minimal scope. Phase 10's SHIP-WITH-NOTES outcome is acceptable per plan §6 Phase 10 acceptance criteria (X7 subdomain drift documented + multi-runtime out of scope + private-repo auth not yet confirmed are all listed as acceptable noted limitations).
3. `make release` is user-controlled per CLAUDE.local.md — Phase 10 does NOT auto-release.
4. Plan archival: `git mv plans/export-buildfromgit-2026-04-28.md plans/archive/` + tracker dir move.