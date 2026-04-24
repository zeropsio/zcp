# Finalize phase — main-agent fragments, stitch, validate

Scaffold + feature built a working deploy and authored codebase-scoped
fragments in-phase. Finalize is assembly + validation: the main agent
fills the root + env fragments, stitches into the 6-tier deliverable,
then iterates on any validator failures.

There is no writer sub-agent. Fragments are authored by whoever holds
the densest context — scaffold/feature sub-agents for codebase-scoped
content (already recorded during their phases), main agent for
platform-narrative content (authored here). See run-8-readiness §2.0
for the authorship map.

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
