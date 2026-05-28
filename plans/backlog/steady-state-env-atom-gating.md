# Steady-state env-atom gating gap

**Surfaced**: 2026-05-28, Stage-2 holistic env review (atom-composition agent
finding, confirmed against `internal/workflow/synthesize.go` axis gating).

**Why deferred**: `develop-env-var-model` (where values come from, self-shadow,
explicit `${host_var}`) and `develop-reserved-env-names` are gated
`envelopeDeployStates: [never-deployed]` — they fire only while authoring the
first `zerops.yaml`. On an already-DEPLOYED service the agent editing env (e.g.
adding a `run.envVariables` line) does NOT receive that guidance. But this is
**reduced proactive guidance, not a silent failure**: the safety-nets still
hold — `CheckEnvSelfShadow` catches a self-shadow at the next deploy-preflight,
the platform 400s a reserved-key collision (`userDataUseOfSystemKey`), and
`develop-env-var-channels` (no deploy-state axis) **still fires at steady-state**
carrying precedence + `shadowWarnings` + reload-vs-restart. So the highest-value
guidance survives; only the first-deploy scaffolding atoms drop out.

**Why not fix now**: the naive fix (broaden `envelopeDeployStates` to include
`deployed`) adds two sizeable atoms to the deployed-state payload, which is
already near the 32 KB MCP soft cap (28672 B — P2's atom edit tripped it at +7
bytes). A correct fix needs a design pass + cap measurement, not a one-line gate
change.

**Trigger to promote**: a flow-eval or real agent run shows an agent
re-introducing a self-shadow / reserved-name mistake at steady-state that the
deploy-preflight + platform nets didn't catch in time; OR develop-phase cap
headroom opens up (atom corpus shrinks elsewhere).

**Sketch**: author a SLIM `develop-env-var-steady-state` atom
(`phases:[develop-active] envelopeDeployStates:[deployed]`) carrying only the
steady-state-relevant reminders — a one-line self-shadow caution + a pointer to
reserved-name rules — rather than firing the full never-deployed atoms. Precedence
is already covered by `develop-env-var-channels`, so don't duplicate it. Measure
the deployed-state synthesized payload (corpus_coverage_test fixtures) before and
after to confirm it stays under the soft cap.

**Risks**: 32 KB cap; redundancy with `develop-env-var-channels`; axis-filter
semantics (keep the new atom's per-service axes minimal so it doesn't over-fire).

**Refs**: `internal/content/atoms/develop-env-var-model.md:5`,
`internal/content/atoms/develop-reserved-env-names.md`,
`internal/workflow/synthesize.go` (axis gating),
`internal/workflow/corpus_coverage_test.go` (cap test).
