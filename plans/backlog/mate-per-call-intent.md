# Per-call `intent` on the mutating Zerops tools — the reason for a call, for the person watching in Mate

**Surfaced**: 2026-09-04 — mate `feat/chat-output` (operation cards,
`plans/mate-chat-output-concept-2026-09-03.md` §4) and its zcp half
`feat/mate-intent` (two commits, dropped unmerged the same day). A Mate
operation card opens with a "voice" line: the agent's own sentence when there
is one, else an English phrase produced from the call's arguments. The only
agent sentence on the wire today is the `intent` of
`zerops_workflow action="start"`; the per-call tools (`zerops_deploy`,
`deploy_batch`, `import`, `verify`, `subdomain`, `delete`, `scale`, `manage`,
`env`, `mount`) carry none.

**Why deferred**: the dropped branch added an optional `intent` string to ten
tool schemas (+278 B each; no handler reads it, no result echoes it) plus a
phase-universal atom `mate-intent` telling the agent to fill it. Karel's call:
that is a divergence between zcp and zcp-under-Mate — a field and guidance that
exist for one client only — and the session intent already reaches Mate with
every call (`StateEnvelope.workSession.intent` / `bootstrap.intent`; the
`1 task = 1 session` invariant makes it the reason for every call inside).
Mate ships with the phrase producer for per-call voice and the bootstrap
intent for the session card.

Two review findings to carry into any retake:

1. The atom listed `zerops_workflow action="start"` among the intent carriers,
   but that `intent` is handler-read: recipe scoring
   (`recipe_corpus_store.go::FindRankedMatches` → `FindRecipeCandidates`),
   `render.go::intentSignalsProduction` (English keywords), the reflog entry,
   the develop "new intent differs" force check. Telling the agent to write it
   "as one sentence to the user, in their language" degrades routing. A retake
   excludes `zerops_workflow` or states that its intent keeps its
   task-description meaning.
2. Atoms select by phase only; `runtime.Info.MateEnabled` is invisible inside
   `internal/workflow`. The atom fired for every agent on every zcp, Mate or
   not (~700 B per workflow response). A retake needs a gate mechanism for
   atoms, or an explicit decision that the guidance is universal.

**Trigger to promote**: live Mate usage shows that the phrase-producer line
("Deploying appdev") under the session intent is not enough to follow the
agent — concretely, feedback that per-call cards inside one session need a
different "why" each; or a second consumer of a per-call reason appears in
zcp itself (stamping it into the process/event log or the reflog), which makes
the field zcp's own and removes the divergence objection.

## Sketch (what the dropped branch had)

- `internal/tools/intent.go`: `IntentDescription` const + `intentSchema()`.
  Explicit-schema tools (`deploy_local`, `deploy_ssh`, `import`, `env`) add
  `"intent": intentSchema()`; tag-inferred tools repeat the description in the
  struct tag; `TestMutatingToolSchemas_ExposeIntent` pins every published
  schema back to the constant. `schema_byte_budget_test.go` ceilings +278 per
  mutating tool.
- `internal/content/atoms/mate-intent.md`, priority 8, all eight phases:

  > ### Say what a call is for, in the user's own language
  >
  > When a person watches this session through Zerops Mate, these calls carry
  > an `intent`: `zerops_deploy`, `zerops_deploy_batch`, `zerops_import`,
  > `zerops_verify`, `zerops_subdomain`, `zerops_delete`, `zerops_scale`,
  > `zerops_manage`, `zerops_env`, `zerops_mount`, and
  > `zerops_workflow action="start"`. *(see finding 1 — drop the last one)*
  >
  > One sentence, in the language they are speaking to you, addressed to
  > them, saying what this call does to their project and why — e.g.
  > `intent="Nasazuju první verzi dashboardu na weatherdash."` Mate prints it
  > verbatim above the call's progress, so write the reason, not a
  > restatement of the arguments and not a status report.
  >
  > A call that only reads state carries none, and so does a repeat whose
  > sentence already stands above it.

- Mate side to add back: `packages/client-runtime/src/zerops/operations/reduce.ts`
  read `input.intent` for every per-call kind and returned it as
  `voiceSource: "agent"` (deleted on `feat/chat-output`, 2026-09-04, when the
  zcp half was dropped); the `types.ts` voice doc line and the
  `docs/internals/zerops/design-system.md` `ZeropsCard` row say the same.

**Refs**: `plans/mate-chat-output-concept-2026-09-03.md` §4, §10;
`docs/spec-mate.md` §5.4; `internal/workflow/render.go::intentSignalsProduction`;
`internal/workflow/recipe_corpus_store.go::FindRankedMatches`.
