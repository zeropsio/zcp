---
name: flow
description: End-to-end zcp dev flow for any non-trivial change — analyze, live-verified plan, sliced AFK implementation, full verification, owner retest pack, spec reconciliation. Routes small safe fixes to a LITE path.
---

# /flow — router

Read `.claude/skills/flow/dna.md` once when a flow starts. Never load all
`phases/*.md` at once — read only the file for the phase you are entering.

## Step 1: route LITE or FULL

**LITE** only when ALL hold: bounded single seam · existing test oracle covers
the area · no public wire-contract / security / credential / lifecycle-state /
concurrency / destructive-action implications · no live-platform mutation ·
reversible in one session. → invoke the `problem-solving` skill, one vertical
slice, RED(reproduce)→GREEN→REFACTOR, existing hooks gate. No plan file.

**FULL** on any hard trigger: load-bearing platform-API assumption · new MCP
tool · public wire contract or schema change · security/credential surface ·
destructive/irreversible op or migration · cross-process lifecycle/state ·
concurrency primitives · multi-session scope · owner asks. Layer count alone
is not a trigger (an ops+tools one-line fix stays LITE; a single-layer
credential change is FULL). Unknown-cause bug: root-cause with a deterministic
regression test first (still LITE); re-route to FULL once the cause names a
hard-trigger risk class.

**Announce the verdict and the trigger out loud before proceeding.** Skipping
FRAME/PROVE/SHAPE is justified by name, never silent.

## Step 2 (FULL only): six phases, two owner gates

| # | Phase | Runs it | Exit gate |
|---|-------|---------|-----------|
| 1 | FRAME | Fable + owner dialogue | Outcome, AC1..n w/ planned evidence, non-goals, risk class, tagged assumptions |
| 2 | PROVE | platform-verifier + zerops MCP reads | every load-bearing assumption VERIFIED or CONFIRMED; REFUTED → back to FRAME |
| 3 | SHAPE | Fable; `codex-brief` gate | slice register + briefs + verify plan; Codex clean |
| — | **OWNER GATE 1** | Karel | approves the register; spec-shaped contracts promoted to `docs/spec-*.md` now — BUILD briefs cite the spec §, never the plan |
| 4 | BUILD | Sonnet subagents, AFK, worktree waves | per slice: RED replay passed, layer tests, lint; landed on integration branch |
| 5 | ASSEMBLE | fresh verifier session | whole-feature battery green; Verify Trace complete; retest pack written |
| — | **OWNER GATE 2** | Karel | runs the retest pack, confirms |
| 6 | LAND | Fable; `/code-review` on the diff | findings dispositioned; spec reconciled with shipped behavior; plan archived |

FRAME+PROVE+SHAPE run in one owner-present block. BUILD+ASSEMBLE (incl.
retest-pack generation) is the AFK run. The Codex and code-review gates are
automated, not owner stops.

## Load discipline

- `dna.md` — once, at flow start.
- `phases/<n>-<name>.md` — only the file for the phase you are entering (e.g.
  `phases/3-shape.md` on entering SHAPE). Never all six at once.
- `templates/*.md` — only when producing that artifact.

## Invocation forms

- `/flow <request>` — routes LITE/FULL, starts FRAME (FULL) or the
  `problem-solving` skill (LITE).
- `/flow resume plans/<file>` — read `## Run State`, re-enter at its `phase:`.
- `/flow build|assemble|land plans/<file>` — revalidates that phase's entry
  conditions; never a gate bypass.

## Plan file

One file at `plans/<slug>-<YYYY-MM-DD>.md`, built from `templates/plan.md`.
