# Slice brief: S3 — Skill-pack port (code + tests only)

Self-contained. Cite spec §s, never the plan. Repo: /Users/macbook/Documents/Zerops-MCP/zcp.
Depends: none (independent of the template chain).

**Outcome** (observable): `internal/skillpacks/` gains the granularity axes
(`ReviewSkillLevel` / `SelectionSubset`), the revision-gated declarative `pack-set` apply,
and `fetchCommit` (fetch-without-full-clone); `zcp skills pack-set` is a wired CLI verb with
a JSON contract; `zcp init` creates `.agents/skills/` + `.claude/skills/` unconditionally; a fresh
Matt whole-repo→subset detach-migration test passes on THIS branch.
`docs/spec-skill-packs.md` ALREADY EXISTS (promoted at OWNER GATE 1) — you implement
against it; if the code you land contradicts it, that is a stop condition (spec fix goes
through the orchestrator), never a silent divergence.

**Allowed scope**
- Files (write-set): `internal/skillpacks/**`, `cmd/zcp/skills.go`, `cmd/zcp/main.go`,
  `internal/init/init.go` + its test file(s).
- Explicitly excluded: `internal/content/templates/**` (welcome surface — S1/S2/S5 own it),
  `internal/content/welcomejs/**` (the picker UI + its tests are S5), `internal/init/adapters/`
  (S1/S2 own claude.go).

**Spec citations**: `docs/spec-welcome-mode.md` §7 (the surface contract this package must
satisfy: axes semantics, revision-gated declarative apply refusing on mismatch with zero
writes, preflight-then-atomic, skill roots before any agent session, broken-refuses-picker,
retired-removable-via-pack-set); `docs/spec-skill-packs.md` (the mechanics contract —
promoted at GATE 1, cite its §s in tests).

**Donor discipline** (map-pinned): `archived/welcome-ux-redesign` is a parts donor —
functional seams only, read via `git show archived/welcome-ux-redesign:<path>`, NEVER checked
out, never its plans. Donor commits (verified to exist, archive-only): `eaa8f73d`
"refactor(skills): give selection granularity its own catalog axis", `36d920d2`
"feat(skills): declarative revision-gated pack-set with atomic reconciliation".

**Load-bearing code facts** (verified 2026-07-28):
- The port surface is a bounded diff:
  `git diff HEAD archived/welcome-ux-redesign -- internal/skillpacks/` = exactly two
  additions (`set.go`, `set_test.go`) + ~10 modifications. Read the diff first; port from it.
- Archive-only content: axes in archived `internal/skillpacks/catalog.go:13-81` (current
  `catalog.go:17` `Pack` has no Review/Selection/Skills fields); `PackSet` in archived
  `set.go`; `fetchCommit`/`fetchCommitArgs`/`checkoutCommitArgs` in archived `git.go:51-105`
  (current `git.go` has only `cloneArgs`/`gitEnv`/`cloneRepo`/`headCommit` :16-45).
- `internal/content/welcomejs/pack_install.test.js` (31 cases) is ALREADY on this branch —
  do not port or modify it. `pack_picker.test.js` is archive-only and belongs to S5, not you.
- Current CLI: `cmd/zcp/skills.go` dispatch `runSkills` :59-80 — verbs `pack-add` (:69),
  `pack-remove` (:71), `pack-status` (:73); arg parsing `parseSkillsArgs` :85-107 (single
  positional + `--json`); wired at `cmd/zcp/main.go:85`. Add `pack-set` in the same style;
  ALL FOUR verbs survive (`pack-add`/`pack-remove`/`pack-status`/`pack-set` — spec §7 names
  all four; pack-set is additive, not a replacement).
- Init step: the step table is `internal/init/init.go:54`; `generateGuidedSkill` :407-432 is
  the pattern (it writes `.claude/skills/guided` conditionally). The NEW step creates
  `.agents/skills/` + `.claude/skills/` UNCONDITIONALLY, before any agent session (§7 —
  agents' native watchers must see the roots at session start). Today `.agents/skills/` is
  created only lazily by `skillpacks.Add` via `os.Root` guards (`targets.go:93-103`,
  `workspaceGuardedPaths` :73-81) — keep those guards for pack operations.
- State/locking context you must not regress: manifest schema v2 (`manifest.go:27`, state dir
  `.zcp/state/skill-packs` :19), marker `.zcp-skillpack.json` v2 (`marker.go:16-18`), lock
  `.zcp/state/skill-packs.lock` (`lock.go:15`), targets `.agents`/`.claude`
  (`targets.go:16-40`), catalog drift guard
  `TestCatalogIDs_MatchWelcomeExtensionAllowlist` (`catalog_test.go:73`) — if your catalog
  change alters the ID surface, that test tells you; the JS side (`PACKS`
  vscode-bootstrap-welcome.js:206-207) is S5's file — if the guard breaks, STOP (scope drift)
  rather than edit the template.
- Revision-gate semantics (spec §7): `pack-set` is declarative (caller states the full
  desired set per pack), refuses on revision mismatch with ZERO writes; `conflict` response
  tells the caller to re-read status and re-render — never silently retry with a stale
  revision. Preflight-then-atomic: full reconciliation lands or the workspace is
  byte-identical. A `broken` pack refuses picker interaction and points at `pack-remove`; a
  `retired` `ReviewSkillLevel` pack must still be removable via `pack-set`.
- RED discipline for the port (additive-seam rule): a missing-symbol compile RED alone
  does NOT clear the BUILD replay gate — after the archived tests fail on the missing seam,
  add the MINIMAL skeleton (types/functions returning zero values) and capture a SECOND,
  assertion-level RED before porting behavior. Both REDs go in the report. The Matt
  detach-migration test is written FRESH on this branch (whole-repo install → `pack-set`
  subset → detach semantics; do NOT trust the archived fixture's starting state — build the
  starting state with this branch's actual `Add`).
- The GATE-1 spec was edited against main's actual `internal/skillpacks/` — still verify
  each claim you implement against; mismatches are stop conditions.

**RED test list** (representative; port the archived suite + these):
- `TestPackSet_RevisionMismatch_RefusesZeroWrites` — unit
- `TestPackSet_DeclarativeSubset_ReconcilesAtomically` — unit
- `TestPackSet_RetiredPack_RemovableViaSet` — unit
- `TestPackSet_BrokenPack_RefusedWithCode` — unit
- `TestFetchCommit_PinnedRevision_NoFullClone` — unit
- `TestMattDetach_WholeRepoInstallThenSubset_MigratesCleanly` — unit (fresh, this branch)
- `TestInit_SkillRoots_CreatedUnconditionally` — unit (init)
- `TestSkillsCLI_PackSet_JSONContract` — tool layer (`cmd/zcp`)

**Protocol**: RED → GREEN → REFACTOR, one named test at a time.
`go test ./internal/skillpacks/... -run <Name> -short -count=1 -v`;
tool layer `go test ./cmd/... -run <Name> -short -count=1 -v`. Then `make lint-fast`.

**BUILD addendum** (embedded verbatim):
- Never batch-write tests: RED → GREEN → REFACTOR one named test at a time.
- Independent oracle: expected values come from the spec §/a known-good literal, never
  recomputed the implementation's own way.
- Assert on public seams only (`ops.*` / tool output) — never an internal
  `platform`/`workflow` helper.
- Table-driven, `Test{Op}_{Scenario}_{Result}` naming; one layer-matrix pass line per
  CLAUDE.md-touched layer.
- `make lint-fast` clean before the slice reports done.

**Report contract**: RED output + exit code · GREEN output + exit code · files touched ·
layer-matrix pass lines (unit + tool) · independent-oracle note.

**Stop conditions**: scope drift · a material unknown · an acceptance-criteria change · a
repeated unexplained check failure. Specifically: needing to edit any file in
`internal/content/` is scope drift — halt.

**Definition of Done**
- [ ] RED replay: archived-test replay fails at slice base SHA, passes at slice head
- [ ] Named tests pass with `-count=1 -v`
- [ ] `go test ./internal/skillpacks/... ./cmd/... ./internal/init/... -short` green
- [ ] `make lint-fast` clean
- [ ] No file outside Allowed scope touched
- [ ] Report contract filled in full
