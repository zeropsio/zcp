# Finalize phase — stitch + validate (run-16)

Phases 5 + 6 (codebase-content + env-content) authored every
documentation fragment. Finalize is **stitch + validate only**:

1. Call `zerops_recipe action=stitch-content slug=<slug>` to render
   every Surface from recorded fragments + engine-stamped IG #1.
2. Call `zerops_recipe action=complete-phase phase=finalize` to run
   the full validator set.
3. If a violation surfaces, fix the underlying fragment via
   `record-fragment mode=replace fragmentId=<id> fragment=<...>`,
   re-stitch, re-validate. The author of the violating fragment was
   the codebase-content / claudemd-author / env-content sub-agent at
   phases 5+6 — main agent owns finalize-time corrections.

There is no fragment authoring at finalize. The "main-agent root +
env fragments" pattern was retired at run-16 — the env-content
sub-agent at phase 6 owns those surfaces.

Run-16 §6.1 + §6.2 — finalize was 4 jobs (stitch, render, validate,
re-author) pre-run-16; now it is 2 (stitch, validate). The legacy
"finalize sub-agent dispatch" option below is back-compat for recipes
that ran the pre-run-16 pipeline; new recipes use phase-6
`env-content` for the same surfaces.

## Sub-agent dispatch option (high-volume mechanical authoring)

Finalize fragments may be authored directly by the main agent
(low fragment count, single-shot) OR via a finalize sub-agent
dispatch (high fragment count — root + env + import-comment
fragments, ~50+ on a 3-codebase recipe with 6 tiers). When you
choose the dispatch path, compose the FULL dispatch prompt from
`Plan` via:

```
zerops_recipe action=build-subagent-prompt slug=<slug> briefKind=finalize
```

The response carries the engine-owned recipe-level context block +
the finalize brief verbatim (correct codebase paths, fragment-count
math, validator tripwires, managed-service list — all Plan-derived) +
closing notes naming the stitch-then-complete-phase path. Pass
`response.prompt` verbatim to `Agent`. Hand-typed wrappers are out —
math errors and path drift compound (run-10 wrapper claimed 89
fragments when actual was 67; carried obsolete pre-§L paths). Run-11
gap S-1; run-13 §B2.

## The env-var template model (critical)

The 6 deliverable yamls are a **template**. Each end-user's click-deploy
creates their own project with their own subdomains and their own
secrets. That means:

- **Shared secrets** emit as `<@generateRandomString(<32>)>` — evaluated
  once per end-user at their import. Your workspace's real secret stays
  on your workspace; **do NOT copy it into the deliverable**.
- **URLs** use `${zeropsSubdomainHost}` as a literal — the platform
  substitutes the end-user's subdomain at their click-deploy.
- **Per-env shape differs**:
  - Envs 0-1 (AI Agent, Remote/CDE — dev-pair slots `apidev`/`apistage`
    exist): carry both `DEV_*` and `STAGE_*` URL constants.
  - Envs 2-5 (Local, Stage, Small-Prod, HA-Prod — single-slot `api`/`app`):
    carry `STAGE_*` only, with hostnames `api`/`app`.

## Fragment authoring

The main agent authors these fragment ids, one at a time, via
`zerops_recipe action=record-fragment slug=<slug> fragmentId=<id>
fragment=<body>`:

- `root/intro` — one sentence naming every managed service + tier count
- `env/0/intro` … `env/5/intro` — per-tier audience + outgrow signal +
  what changes at the next tier (spec-content-surfaces §Surface 2)
- `env/<N>/import-comments/project` — per-tier project-level comment in
  the import.yaml
- `env/<N>/import-comments/<hostname>` — per-tier per-service comment
  explaining why THIS service at THIS tier at THIS mode/scale (never
  the narration of what the field does)

### Single-question test per surface (spec-content-surfaces.md)

Apply ONE test per fragment before recording:

- **Root README intro** — "Can a reader decide in 30 seconds whether
  this recipe deploys what they need?"
- **Env README intro** — "Does this teach me when to outgrow this
  tier and what changes at the next one?"
- **Env import-comments** — "Does each service block explain a
  decision (why this scale / mode / presence), not narrate what the
  field does?"

Items that fail their surface's test are REMOVED, not rewritten —
failure means the content doesn't belong on that surface, not that
it's phrased wrong.

### Tone rules

- Env READMEs: porter-facing, never uses the word "agent". Tier
  promotion vocabulary present ("outgrow", "promote", "when you move
  to tier N+1").
- Import-comments: causal words (`because`, `so that`, `otherwise`,
  `trade-off`). First sentence must differ across runtime-service
  blocks (no templated opening).

### Citation-map attachment

When your fragment touches a topic in the engine's citation map
(env-var-model, init-commands, rolling-deploys, object-storage,
http-support, deploy-files, readiness-health-checks), fetch the guide
via `zerops_knowledge` FIRST and cite it by name. Writing new mental
models for topics the platform already documents is how folk-doctrine
ships (run 7 workerdev gotcha #1 class).

## Stitch + validate loop

1. **Author every fragment** listed above.
2. `zerops_recipe action=stitch-content slug=<slug>` — renders every
   surface into outputRoot. Missing fragments return as an error with
   the list of unset ids.
3. **Read validator output** from the complete-phase response. Each
   violation names the offending fragment id + the rule it broke.
4. **Iterate** — `record-fragment` again with a fixed body, re-stitch,
   re-validate. Codebase-scoped fragment failures may route back to a
   re-dispatch if the issue is causality only the scaffold sub-agent
   could know.

## Complete-phase gate

`zerops_recipe action=complete-phase slug=<slug>` runs default gates
(citation timestamps, fact validation) + finalize gates
(env-imports-present, per-surface validators). Phase is not done until
every validator passes on the assembled output.

## What NOT to do

- Do NOT re-run `emit-yaml shape=workspace` at finalize — that shape is
  provision-only.
- Do NOT pass your live workspace's secret as a `project_env_vars` value.
  Use `<@generateRandomString(<32>)>`.
- Do NOT resolve `${zeropsSubdomainHost}` to a literal URL. It stays
  a template for the end-user's platform to substitute.
- Do NOT hand-edit stitched files. `stitch-content` is the write path;
  iterate via `record-fragment` + re-stitch.
