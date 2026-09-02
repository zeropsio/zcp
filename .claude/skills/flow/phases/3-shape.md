# SHAPE

Entry: `phase: shape`, reached from PROVE's exit or `/flow resume`.
Revalidate on resume: every `[PROBE]` Assumption in Frame is `[VERIFIED]`
(or PROVE's skip note is logged) — re-entering without that is a bypass.

## 1. Design step — only if a material trade-off exists

Most changes in this layered Go repo have one obvious, reversible approach —
skip to §2. A genuine trade-off (more than one defensible shape) gets 3
lines in `## Frame`: decision + rejected alternative. Don't manufacture a
design step for its own sake.

## 2. Derive slices — vertical, dependency-ordered

Every slice lands OBSERVABLE behavior plus its own tests — never a
layer-only slice (no "just the ops layer" slice with no caller). Order by
dependency, sized to one healthy session each. Grep the live tree before
placing any slice — never assume a file's location from memory or another
plan; the codebase has moved since anything you remember reading.

**S1 is the tracer bullet**: the thinnest real end-to-end path through the
change — for a feature, a real tool call against `zcp-eval-clean` where the
feature has a live surface. Proves the route before fan-out; a broken
tracer bullet reshapes the register, not just S1.

## 3. Fill `## Slice Register`

Columns: `ID | Title | Depends | Files | Layers | Gate | State`.
- `Files` — the slice's declared WRITE-set (creates/modifies), not reads.
  **Overlapping write-sets must not share a wave**: BUILD groups a wave by
  landed `Depends` + disjoint `Files` alone — give any two Files-overlapping
  slices a `Depends` edge, or BUILD has no ordering and the wave is ambiguous.
- `Layers` — TEST layers exercised (`unit/tool/integration/e2e`, CLAUDE.md's
  change-impact rule), not architecture layers; feeds §5 and each brief.
- `Gate` ∈ `autonomous` (default) / `review` (real risk — security,
  credential, destructive, public contract, but not taste) / `owner` (taste
  or an irreversible call only Karel can make).
- `State` starts `pending` for every row.

## 4. Compose per-slice briefs

One brief per slice from `templates/slice-brief.md` — self-contained (a
fresh BUILD subagent gets ONLY its brief + the plan pointer). Embed
`dna.md`'s core plus the BUILD addendum from `phases/4-build.md`. Cite spec
`§`s for load-bearing rules, never the plan (plans aren't a source,
CLAUDE.md) — specs aren't promoted yet, so cite the section each brief WILL
land in and flag it pending GATE 1.

## 5. Write Verify Trace, draft Promotion

One `## Verify Trace` row per ACx: the planned check/scenario, `result` left
`not-run` until ASSEMBLE — every AC needs ≥1 check naming a real command or
drive step, no "verify it works" placeholders.

Draft `## Promotion` now (destinations only, not yet executed): which
contract lands in which `docs/spec-<name>.md §<n>`, which invariant becomes
a permanent `Test<Op>_<Scenario>_<Result>`, at most one CLAUDE.md trap line
(or "none"). GATE 1 executes the spec writes; LAND executes the archive.

## 6. Judge gate

Hand the plan summary + register + open trade-offs to a `judge` subagent
(fable, low effort, read-only — see `/route`) with the plan path and the
spec §s it cites; ask for holes in the register: missing slices, a
write-set overlap, an unverified load-bearing assumption, a contract the
spec does not state. Incorporate findings that reshape the register or
briefs (re-run this step if they do). Record the verdict in Run State's
`review:` field. Codex is added to this step only when Karel asks for it by
name in the message — then load `codex-brief` and record its verdict too.

## 7. OWNER GATE 1

Present the Slice Register compactly (the table, not the full briefs); ask
Karel to approve. On approval, in order:
1. Execute `## Promotion`'s spec destinations — write the approved
   spec-shaped contracts to `docs/spec-*.md` now. BUILD briefs cite these
   `§`s from this point on.
2. Record `approved: Rev-N, <date>` in Run State; set `phase: build`,
   `next:` = the tracer-bullet slice's first action.

**Any material edit to `## Frame` or `## Slice Register` after this point**
resets `phase: awaiting-approval` — GATE 1 re-runs even for a small change;
there is no quiet re-approval.

Exit: Slice Register + briefs + Verify Trace + drafted Promotion exist;
the judge review is clean or incorporated; Karel has approved; approved
contracts are promoted to spec. `phase: build` only once all hold.
