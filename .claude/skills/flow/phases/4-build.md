# Phase 4 — BUILD

Entry: `## Run State` `phase: build`, reached only via OWNER GATE 1 (register
approved, spec-shaped contracts already promoted to `docs/spec-*.md`). Read
`templates/slice-brief.md` before writing the first brief.

## BUILD addendum (embedded verbatim into every brief, per `3-shape.md` §4)

- Never batch-write tests: RED → GREEN → REFACTOR one named test at a time.
- Independent oracle: expected values come from the spec §/a known-good
  literal, never recomputed the implementation's own way.
- Assert on public seams only (`ops.*` / tool output) — never an internal
  `platform`/`workflow` helper.
- Table-driven, `Test{Op}_{Scenario}_{Result}` naming; one layer-matrix pass
  line per CLAUDE.md-touched layer.
- `make lint-fast` clean before the slice reports done.

## Wave algorithm

A wave = every Slice Register row whose `Depends` are all `landed` AND whose
`Files` write-set is disjoint from every other row in the wave — SHAPE
enforced disjointness at register time; BUILD re-checks it before spawning.
Before spawning a wave, confirm the main tree's checked-out branch IS the
integration branch at `## Run State` `integration:` — every slice in the
wave branches from there via `isolation: "worktree"`, never from `base:`.

## Spawn

One `Agent` call per slice: `subagent_type: "builder"` (sonnet, high
effort — pinned in `~/.claude/agents/builder.md`), `description` =
`<Sn> <title>`, `prompt` = the filled `templates/slice-brief.md` plus the
BUILD addendum above, `isolation: "worktree"`. The brief carries paths and
spec §s, never pasted file contents — the builder shares no cache with the
orchestrator and reads the files into its own. Send every slice in the wave as
ONE message with multiple `Agent` calls — that's what makes them run
concurrently. The tool returns a branch/path only when the agent made
changes; read that diff from the main tree by branch name (worktrees share
refs) — never `cd` into a subagent's own worktree yourself.

## RED replay acceptance — replay, not transcript trust

A slice's self-reported RED/GREEN transcript is a claim, not evidence.
Before integrating, reproduce it independently, per slice:
1. `EnterWorktree` (scratch) → `git checkout <slice base SHA>` (detached).
2. `git checkout <slice-branch> -- <test files named in the brief>` — pulls
   ONLY the new/changed test files onto the base tree (diff-restricted).
3. `go test ./<pkg> -run '<Names>' -short -count=1 -v` — MUST FAIL: an
   assertion diff, or (additive-seam exception) the exact missing-symbol/
   type error of the new seam only. Any other compile error (syntax, bad
   import) is an invalid RED — reject the slice, re-brief. On the
   additive-seam path, also confirm the brief's own transcript shows a
   *second*, minimal-skeleton assertion-level RED before behavior landed —
   a bare compile RED alone never clears this gate.
4. `git checkout -f <slice-branch>` (clean, full) → same command MUST PASS.
5. `ExitWorktree(action: "remove", discard_changes: true)`.
6. Note both exit codes under the Slice Register table (e.g. `S3: RED=1,
   GREEN=0`) — evidence, not decoration; the table's own columns stay
   canonical (no new column for this).

Any replay failure: row → `blocked`, re-brief from the current integration
SHA, do not integrate.

## Integration

Per accepted slice, in Slice Register order, from the main tree:
1. `git merge --no-ff <slice-branch>`.
2. Clean merge → re-run the slice's named tests + `make lint-fast`; both
   clean → row → `landed`; update `## Run State` `integration:` (new SHA +
   landed commit range — this pins LAND's rollback command).
3. Conflict → **halt the wave.** Re-brief every un-landed slice in it from
   the new integration SHA (their base moved) and replay each again before
   retrying — never resolve a conflict silently.
4. `git worktree remove <path>` + `git branch -d <slice-branch>` for every
   landed slice. NOT `ExitWorktree` — that tool only manages worktrees this
   session opened via `EnterWorktree`; a subagent's own `isolation:
   "worktree"` worktree is a different one and needs plain `git worktree
   remove`.

## AFK stop conditions

Halt the run and write `templates/handoff.md` on: scope drift · a material
unknown · an acceptance-criteria change · a repeated unexplained check
failure — per `dna.md`. `task-completed.sh` (tests+vet, exit 2 on red) keeps
hard-gating every subagent's own completion; unchanged.

Exit: every wave's slices `landed` on the integration branch, `##
Run State` `integration:` current, no `blocked` row remains (or the run
halted with a handoff) → ASSEMBLE.
