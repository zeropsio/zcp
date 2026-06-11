# Decision recording — record `porter_change` + `field_rationale` facts

For every non-obvious decision you make at scaffold, record a
structured fact via `record-fact`. Codebase-content phase reads these
to synthesize IG/KB/yaml comments. Recording at densest context
(the moment of the decision) keeps the why preserved.

## Two kinds at scaffold scope

| Kind | Required fields |
|---|---|
| `porter_change` | `topic`, `why`, `candidateClass`, `candidateSurface` |
| `field_rationale` | `topic`, `fieldPath`, `why` |

The full `Fact` schema (every kind, every field, every accepted enum
value) is in the `zerops_recipe` action description for `record-fact`.

## `topic` (freeform) vs `kind` (enum)

These two slots are not interchangeable. The validator catches the
typical drift but the failure mode is fast to avoid:

- **`kind`** is a fixed enum: one of `porter_change`,
  `field_rationale`, `tier_decision`, `contract`, or empty (legacy
  platform-trap shape). Pick from this list. Anything else fails
  fact-shape validation.
- **`topic`** is a freeform identifier you author. Pick a token
  unique to this fact's specific purpose so two facts in the same
  scope can't collide. Reuse across scopes is fine when the
  underlying observation is the same; reuse across DIFFERENT
  observations (worker startup vs API health-check vs UI render)
  is the bug — the run-22 evidence pattern was
  `worker_dev_server_started` reused for 3 different processes,
  rendering join-by-(topic, scope) tooling useless.

Worked example — same observation, two different facts:

```yaml
# Good: distinct topic per distinct observation.
- topic: nestjs-listen-host-binding
  kind: porter_change
  scope: api
  ...

- topic: nestjs-listen-host-binding-worker
  kind: porter_change
  scope: worker
  ...

# Bad: same topic for two distinct observations.
- topic: server_started
  kind: porter_change
  scope: api
  ...
- topic: server_started   # ← collides; the dev server starting in api
  kind: porter_change     #   is not the same fact as the worker NATS
  scope: worker           #   subscriber starting.
  ...
```

## `citationGuide` — populate when the fact paraphrases a guide

When the fact paraphrases or directly applies content from a
`zerops_knowledge` guide, populate `citationGuide` with the guide id
so the codebase-content phase can render the cite-by-name reference
in the published KB or IG prose. Empty is fine when the fact stands
alone (recipe-specific, no guide cite); populate it when the fact's
why-it-matters is grounded in a Zerops mechanism documented in a
guide.

```yaml
- topic: nestjs-listen-host-binding
  kind: porter_change
  why: NestJS app.listen(port) binds 127.0.0.1; L7 needs 0.0.0.0
  candidateClass: platform-invariant
  candidateSurface: CODEBASE_KB   # canonical UPPER_SNAKE form
  citationGuide: http-support   # ← maps to a published guide id
```

`candidateSurface` must be one of: `ROOT_README`, `ENV_README`,
`ENV_IMPORT_COMMENTS`, `CODEBASE_IG`, `CODEBASE_KB`,
`CODEBASE_CLAUDE`, `CODEBASE_ZEROPS_COMMENTS`. `record-fact` refuses
other spellings.

The list of valid `citationGuide` ids is in the scaffold brief's
"Citation map" section (built from `CitationMap` in
`internal/recipe/citations.go`); `record-fact` accepts any
non-empty string but only known ids surface as cite-by-name in the
published prose.

## Env-token references in fact text

When a `porter_change` fact describes managed-service wiring (NATS
connection, S3 endpoint, DB credentials), at least one text field
(`why`, `fixApplied`, `surfaceHint`, `evidence`) MUST cite the
`${<host>_*}` env-token form for every managed service the fact
touches.

Why: cross-codebase fact propagation
(`CrossCodebaseManagedServiceFacts`) matches on env-token references
to decide which sister facts are relevant to this codebase. A fact
that cites only the alias-name (`connectionString`) or literal URLs
(`nats://user:pass@host:4222`) won't propagate to consumer codebases
even when the underlying decision is platform-invariant.

Worked example — BAD vs GOOD:

```yaml
# BAD — propagation predicate misses; the NATS-broken-shape finding
# stays scoped to worker even though the decision is platform-wide.
- topic: nats-pattern-a-separate-credentials
  kind: porter_change
  why: |
    Pattern B (single connectionString) crashes at boot — nats@2.29
    hostPort() IPv6-misidentifies embedded credentials by colon-count.
    Use Pattern A: separate servers/user/pass.

# GOOD — env-token cite makes propagation predicate match.
- topic: nats-pattern-a-separate-credentials
  kind: porter_change
  why: |
    Pattern B (single ${broker_connectionString}) crashes at boot —
    nats@2.29 hostPort() IPv6-misidentifies the auto-generated
    password by colon-count. Use Pattern A: ${broker_hostname} +
    ${broker_port} + ${broker_user} + ${broker_password} as
    separate servers/user/pass options.
```

## Forbidden tokens in fact text fields

Recipe-author voice in fact text propagates to porter prose at
synthesis. Forbidden in `why` / `mechanism` / `fixApplied` /
`surfaceHint` / `evidence`: `the agent`, `recipe author`,
`scaffold sub-agent`, `feature sub-agent`, `during scaffold`,
`during feature`, `we chose`, `we use`, `we set`, `record-fact`,
`zerops_dev_server`. Record what's true about the porter's runtime,
not about the recipe-construction process.

```yaml
# BAD — "we set" + "during scaffold" leak recipe-author voice.
why: During scaffold we set app.listen('0.0.0.0', port).

# GOOD — porter-runtime framing.
why: NestJS app.listen(port) defaults to 127.0.0.1; Zerops's L7
     balancer needs 0.0.0.0 so VXLAN-routed traffic reaches the
     container's listener.
```

## Skip rule

Skip recording if `candidateClass ∈ {framework-quirk, library-metadata,
operational, self-inflicted}` — those have no porter-facing surface.
Record only when `candidateClass ∈ {platform-invariant, intersection,
scaffold-decision}`.

## Close-out batch-fix discipline

`complete-phase phase=scaffold codebase=<host>` returns the FULL
violation list every call. If `ok:false`, fix EVERY violation the
response named (record-fact for each `fact-rationale-missing`,
record-fragment mode=replace for each surface-shape failure,
ssh-edit for yaml-comment violations) BEFORE re-calling
`complete-phase`. Steady state is two calls: first surfaces
everything; second confirms. Don't queue fixes one-at-a-time —
that's what burned 4-5 round-trips per codebase on run-32.

## Git hygiene

Before first deploy:

```
ssh <hostname>dev "git config --global user.name 'zerops-recipe-agent' && git config --global user.email 'recipe-agent@zerops.io'"
```

Then `git init && git add -A && git commit -m 'scaffold'`.
