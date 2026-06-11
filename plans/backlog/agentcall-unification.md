# AgentCall unification — one owner for "what to call next"

**Surfaced**: 2026-06-09/10, delivery-model foundations evaluation (16-agent workflow + fresh Codex
adversarial pass over the Stream B redesign; verdict doc `plans/delivery-model-foundations-verdict-2026-06-09.md`,
integrated plan `plans/delivery-model-final-plan-2026-06-09.md` Phase 2). Karel chose the surgical
path (point fixes within the current model) and deferred this structural piece.

**Why deferred**: the surgical fix list (executable nextCall on errors, deploy-order, Router-1/2,
live-gated tables, develop-wall trim, …) takes ~70–80% of the measured pain at ~40% of the cost
without it. This entry is the structural half: it doesn't fix any single measured bleeding by
itself — it removes the *generator* of the drift class so the point fixes stop being a fight with
a hydra.

**Trigger to promote**:
- A NEW next-call dialect or hand-authored call-string drift appears in review/eval (the signal the
  generator is still producing), OR
- the surgical batch's error-nextCall work (fix #1) starts duplicating call-construction logic per
  error site — at that point do THIS first and let errors ride on the unified type, OR
- Stream A LP-8 / J3 / F43 (structured args/pipelineSummary fields) is about to land — those should
  land ON the unified type, not add shape #9.

## Problem

One concept — "the executable next call" — is produced at many sites by hand in ~8 incompatible
shapes: typed `Plan/NextAction` (pure `BuildPlan`, self-declared single source — but its JSON tags
are dead on the wire), `topology.Recovery` (own dialect: separate `Action` field), import gate
`retryCall`, `NonRunningRecovery`, knowledge fetch directive, launch envelope free-string
`NextCall`, plus prose `nextActions`/`nextStep(s)`/`Next:` authored per handler/render. Corpus
(428 runs): the fully-structured form appeared in 2/428 runs' responses; the same recommendation
ships in six wire dialects. TELL (what we advise) and CHECK (what the gate requires) have no common
owner → recurring drift bug class (close-mode call historically emitted from 5 sites in 2 syntaxes).

## Sketch (full step detail in the 2026-06-10 conversation; design rules)

1. **One type**: `topology.AgentCall {Label, Tool, Args, Rationale}`; `Recovery` merges into it
   (the `Action` field dies — action is just an arg). Launch string `NextCall` → object.
2. **Producers only**: `BuildPlan` (lifecycle) + gate/check verdicts construct calls — derived from
   the same predicate that gates (tell==check by construction). Handlers only place produced calls.
3. **Render projects**: one `FormatAgentCall` formatter (pattern: `CloseModeCallExample`); no
   hand-written call strings in Go code. Atoms unaffected (separate, linted templating).
4. **Locked by tests, not discipline**: AST lint forbids `AgentCall` literals outside producer
   packages (pattern: `TestNoInlineManagedRuntimeIndex`) + lint forbids hand-composed call strings
   in `internal/tools` + goldens assert `nextCall == Plan.Primary` where applicable.

Phasing (each independently green): (1) type promotion + Recovery alias; (2) consumer sweep
(errwire, verify checks, import retryCall, NonRunningRecovery, knowledge fetch); (3) launch
string→object + golden rewrite; (4) formatter + AST lints; (5) merge duplicated predicates (render's
parallel pending loop + double-computed close-mode gate onto build_plan/envelope predicates —
equivalence-pinned before deleting the duplicate); (6) goldens + flow-eval smoke.

## Risks

- Effort ~3–5 days. Breakage potential low-medium; the only behavioral-risk step is the predicate
  merge (5) — both paths read the same metas, equivalence pinned first, step independently
  revertable. Launch wire-shape change is covered by the clean-swap policy (response shapes are
  ephemeral; tool names/schemas/disk untouched). Recipe engine (Aleš) unaffected — own pipeline (P9).
- The site catalog comes from the 2026-06-09 evaluation (workflow run `wf_7745d06e-6ec`, m1/m4
  mappers) — **re-verify against main at promotion time** (the repo has moved since, e.g. the
  2026-06-10 `DeriveDeliveryState` ladder).

## Refs

- `plans/delivery-model-foundations-verdict-2026-06-09.md` §3 (architecture rulings: nextCall
  ownership, the 8-shape catalog, drift-class precedents)
- `plans/delivery-model-final-plan-2026-06-09.md` Phase 2 (the keystone phase this entry extracts)
- `plans/zcp-goal-contracts-concept-2026-06-09.md` §3.1 design rules (nextCall always object form;
  Plan as single owner)
- CLAUDE.md "Information Contract" — one owner per concept: the TELL and the CHECK derive from a
  single source
