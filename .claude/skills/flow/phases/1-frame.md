# FRAME

Entry: SKILL.md routed **FULL** and named the trigger, or a LITE
`problem-solving` fix escalated because its root cause needs an architecture
change, or `/flow resume` finds `phase: frame`. Revalidate on resume: `##
Frame` is absent or incomplete.

## 1. Create the plan file

Copy `templates/plan.md` to `plans/<slug>-<YYYY-MM-DD>.md` (slug = short
kebab-case of the request). Fill `## Run State`: `phase: frame`, `base:` =
`git rev-parse HEAD`, `next:` = this step.

## 2. Task-type orientation (steers exploration, not a Frame field)

Classify the request — feature (new capability) / behavior change (existing
path, different output) / multi-system (crosses ≥2 of platform/topology/
workflow/ops/tools) / unknown-cause (escalated from a LITE root-cause that
named an architecture-level fix). This decides which specs and packages
Explore reads first; it is never written to the plan file itself.

## 3. Read-only exploration — spawn it, never do it yourself

Two lanes, always both, in parallel:
- **KB lane**: one `zerops-knowledge` subagent — read-only by construction
  (its `disallowedTools` bars Edit/Write/Bash/NotebookEdit/worktree tools) —
  for platform facts, existing specs, recipes.
- **Codebase lane**: `Explore` subagent(s) with `mode="plan"` — mandatory,
  never a plain `general-purpose`/`claude` agent for this lane. `mode="plan"`
  IS the read-only enforcement: an `Explore` agent without it still has Bash
  and can write files via heredoc/echo/tee. Scale count by complexity: a
  small bounded area → 1 explorer; cross-system, or the request/files carry
  "platform"/"security"/"architecture" → 2-3 explorers in parallel + one
  sequential adversarial pass (a further `Explore`+`mode="plan"` agent fed
  the others' findings, must cite file:line for every challenge it raises).

Grade every finding per `dna.md`: **VERIFIED** (cite
`[SELF-VERIFIED:file:line]`) / **LOGICAL** (derived from a VERIFIED fact —
state the inference) / **UNVERIFIED** (tag `[UNVERIFIED]`); KB facts cite
`[KB:<source>]`. Do not invent a different scale for this phase.

## 4. Fill `## Frame`

- **Outcome** — one observable sentence.
- **obs | evidence** table — only KB/Explore-sourced rows, each cites its
  source.
- **AC1..n** — each with its planned evidence (what will prove it later, not
  a restatement of the criterion).
- **Non-goals**, **Constraints**.
- **Risk class** — `low|medium|high` by blast radius + reversibility (any
  slice you can already foresee needing an `owner` gate → at least medium;
  destructive/credential/irreversible → high) — plus the FULL trigger the
  router already named. Don't re-derive the trigger list; SKILL.md owns it.
- **Assumptions** — every material claim tagged `[VERIFIED]` (cite
  `path:line`), `[PROBE]` (uncertain AND load-bearing), or `[ASSUMED]`
  (uncertain, not load-bearing — stated, not probed). This is a different
  axis from §3's VERIFIED/LOGICAL/UNVERIFIED grades (probe-worthiness, not
  confidence) — do not merge the two tag sets.

## 5. Owner checkpoint

Any material open question — ambiguous AC, conflicting constraint, a risk
call only Karel can make — ask NOW, in this dialogue. FRAME is the one
owner-present phase where a question is cheap; the same question surfacing
mid-BUILD stalls an AFK run.

## 6. Tripwire (at end, before exit)

`git status` must be clean apart from the plan file itself; `git log
--oneline` since this session's start must show no unexpected commits. A
violation means a subagent wrote outside its lane — flag it, don't silently
proceed past it.

## 7. Set state and exit

Set `phase: prove`, `next:` = the first `[PROBE]` claim to probe (or, if
none exist, "no PROBE claims — PROVE will skip").

Exit: `## Frame` carries Outcome, the obs/evidence table, AC1..n each with
planned evidence, Non-goals, Constraints, a Risk class with its trigger, and
every Assumption tagged — PROVE reads this file next, never a transcript.
