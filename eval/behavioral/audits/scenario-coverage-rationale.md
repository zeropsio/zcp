# Scenario coverage rationale (POC, 2026-05-03)

## Why one scenario for the POC

The POC has one scenario: `greenfield-node-postgres-dev-stage`. That choice
is deliberate, not lazy. The plan
(`plans/eval-behavioral-findings-poc-2026-05-03.md`) closes the loop on two
known structural defects (Trap-1 — recipe-route discover plan retries;
Trap-2 — dev-mode dynamic-runtime first-deploy verify HTTP 502). Both are
reproducible from a single scenario that traverses the bootstrap recipe
route plus the develop first-deploy flow on a dev/stage pair. Adding more
scenarios up front would dilute the proof that the orchestrator + two-shot
resume + interactive grading loop actually surfaces these defects in the
agent's retrospective. Multi-scenario coverage is roadmap, gated on this
loop closing.

## What envelope shape this scenario traverses

- `bootstrap-active` phase, `routes: [recipe]`, `steps: [discover, provision, close]`.
  Plan-shape decision lives in `bootstrap-recipe-match.md` (priority 1,
  recipe-route discover step). This is the atom under test for Trap-1.
- `develop-active` phase with two runtimes (dev + stage standard pair) +
  one managed dependency (Postgres). `runtimes: [dynamic]` + `modes: [dev, stage]`.
  Both runtimes hit the never-deployed → first-deploy → verify sequence.
  `develop-first-deploy-verify.md` (priority 5, gate `deployStates: [never-deployed]`)
  is the atom under test for Trap-2; `develop-dev-server-triage.md` is the
  atom whose timing-locked gate (`deployStates: [deployed]`) is what causes
  the trap to surface.
- `develop-active` close: auto-close after both runtimes verify healthy.
  Implicit in the scenario flow but not explicitly under test in this POC.

## What scenarios are explicitly deferred to roadmap

- **Negative-control: `bootstrap-classic-route-discover`.** Same envelope
  axis content but classic route. The classic counterpart atom
  (`bootstrap-mode-prompt.md:22`) has the worked JSON example
  bootstrap-recipe-match lacks. Trap-1-equivalent friction should NOT
  appear in the retrospective; if it does, that's a different bug or our
  diagnosis was incomplete. Roadmap item 1.
- **Develop-iteration on already-deployed scope.** The agent receives a
  scope where everything is already deployed, asked to add a feature.
  `develop-dev-server-triage.md` fires correctly because `deployStates: [deployed]`
  is satisfied. Trap-2-equivalent friction should NOT appear. Validates
  Trap-2 is timing-locked specifically to the first-deploy moment. Roadmap
  item 2.
- **Multi-agent runs of the same scenario.** Vary `agent.model` across
  Opus 4.7, Sonnet 4.6, Codex, Gemini. Compare retrospectives
  conversationally. Surfaces model-specific friction (e.g. one model
  recovers from schema validation faster than another, useful signal for
  atom-rendering calibration). Roadmap item 3.
- **Phase × Mode × Environment matrix.** The full coverage product:
  classic vs recipe routes × dev / standard / simple / stage modes ×
  container / local environments. Tagged so `flow-eval.sh list` rolls up
  by category. Pruning policy at scenario count = 20. Roadmap item 4.

## Why this single scenario is sufficient for the POC

Three things must be true at the end of Phase 2 for the POC to count as
closed:

1. The orchestrator (cleanup → build-deploy → call 1 → resume → extract)
   produces a complete artifact set with no missing fields.
2. The agent's retrospective in `self-review.md` is non-trivial (not a
   recap, not bland, surfaces concrete friction the agent actually hit).
3. The local Claude Code session can read the retrospective + the
   transcript + the repo and have a useful conversation with the user
   about what to do next.

None of those three depends on having more than one scenario. They all
depend on the loop running end-to-end on one scenario that's known to
exercise both traps. Once the loop is proven, scenario expansion is
roadmap-governed and cheap.
