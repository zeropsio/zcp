# Decision recording — record `porter_change` + `field_rationale` facts

You write source code + zerops.yaml at scaffold; you do NOT author IG /
KB / yaml comments yet (a sibling content phase reads your facts +
on-disk artifacts later and synthesizes those surfaces).

For every non-obvious decision, record a structured fact at densest
context — the moment you make the change. Two subtypes cover the
codebase scope:

## FactRecord shape

Two orthogonal taxonomies — `Kind` and `Classification` — go on
every fact. Don't mix them.

### Kind (the discriminator — 4 values, picks the validation path)

| Kind | Required fields |
|---|---|
| `porter_change` | `topic`, `why`, `candidateClass`, `candidateSurface` |
| `field_rationale` | `topic`, `fieldPath`, `why` |
| `tier_decision` | `topic`, `tier` (0-5), `fieldPath`, `chosenValue` |
| `contract` | `topic`, `publishers`, `subscribers`, `subject`, `purpose` |

Unknown `kind` values reject. There is no `classification` field on
`FactRecord` — the classification of a `porter_change` fact lives on
the `candidateClass` slot (table below).

### Classification (porter_change.candidateClass — 7 values)

| Class | Surface routing |
|---|---|
| `platform-invariant` | record (CODEBASE_IG) |
| `intersection` | record (CODEBASE_KB) |
| `scaffold-decision` | record (CODEBASE_IG for config; CODEBASE_KB for code) |
| `framework-quirk` | skip — no porter-facing surface |
| `library-metadata` | skip — no porter-facing surface |
| `operational` | skip — sibling claudemd-author owns CLAUDE.md |
| `self-inflicted` | skip — discard |

Record only when `candidateClass` is one of the three surface-bearing
classes. Skip-classes have no landing point — recording them costs
brief budget for content the synthesizer will discard.

## `porter_change` — code or library decisions a porter would have to make

Record whenever you write code that's NOT framework-defaults: bind to
0.0.0.0, install a specific library, configure CORS exposed-headers,
mount a same-origin proxy, etc. Consult `zerops_knowledge
runtime=<svc-type>` before authoring the Why so the platform mechanism
prose is grounded in the live atom, not paraphrased from memory.

```
zerops_recipe action=record-fact slug=<slug>
  fact={
    topic: "<host>-<short-id>",
    kind: "porter_change",
    scope: "<host>/code/<file>",
    phase: "scaffold",
    changeKind: "code-addition",
    library: "<lib-name>",
    diff: "<the-actual-line-or-block>",
    why: "<symptom + mechanism + fix at the platform level>",
    candidateClass: "platform-invariant" | "intersection",
    candidateHeading: "<surface-shaped heading>",
    candidateSurface: "CODEBASE_IG" | "CODEBASE_KB",
    citationGuide: "<topic-id-from-citation-map>"
  }
```

## `field_rationale` — non-obvious zerops.yaml field decisions

Record whenever a yaml field carries reasoning that's not self-evident
from the value (e.g. S3_REGION=us-east-1 is the only region MinIO
accepts; two separate execOnce keys so a seed failure doesn't roll back
the schema migration).

```
zerops_recipe action=record-fact slug=<slug>
  fact={
    topic: "<host>-<short-id>",
    kind: "field_rationale",
    scope: "<host>/zerops.yaml/<field-path>",
    phase: "scaffold",
    fieldPath: "run.envVariables.S3_REGION",
    fieldValue: "us-east-1",
    why: "<reason>",
    alternatives: "<what-fails-if-changed>",
    compoundReasoning: "<optional, when reasoning spans multiple fields>"
  }
```

For compound decisions (e.g. two `initCommands` entries with paired
reasoning), record one `field_rationale` per field with a shared
`compoundReasoning` slot. The content sub-agent merges them into one
yaml comment block.

## Filter rule — when NOT to record

See the Classification table above. The 4 skip-classes
(`framework-quirk`, `library-metadata`, `operational`,
`self-inflicted`) have no landing point. Record only when
`candidateClass` ∈ {`platform-invariant`, `intersection`,
`scaffold-decision`}.

## Examples (run-15 grounded)

- `S3_REGION=us-east-1` because MinIO requires it → `field_rationale`,
  `scope: apidev/zerops.yaml/run.envVariables.S3_REGION`.
- `app.enableCors({ exposedHeaders: ['X-Cache'] })` because cross-origin
  fetch strips the header → `porter_change`,
  `candidateClass: intersection`, `candidateSurface: CODEBASE_KB`.
- `$middleware->trustProxies(at: '*')` because L7 forwards X-Forwarded-*
  → `porter_change`, `candidateClass: platform-invariant`,
  `candidateSurface: CODEBASE_IG`.

If a porter would ask "why?", record it.

## Git hygiene (carried forward from pre-run-16)

Before the first deploy in any codebase, ensure git identity is set on
the dev container:

```
ssh <hostname>dev "git config --global user.name 'zerops-recipe-agent' \
  && git config --global user.email 'recipe-agent@zerops.io'"
```

Then for the scaffold commit:

```
git init
git add -A
git commit -m 'scaffold: initial structure + zerops.yaml'
```

The scaffold sub-agent records git ops in commits, not in fragments —
the apps-repo publish path needs a clean history precondition. (The
phase_entry atom names the recovery path when a deploy commit already
exists from prior runs.)

## Worked examples — what good fact-recording looks like

The codebase-content sub-agent at phase 5 reads your facts + on-disk
zerops.yaml + spec and synthesizes the documentation surfaces. Quality
of synthesis depends on quality of recording — a fact whose Why names
the platform mechanism + the observable failure mode + the fix gives
the synthesizer everything it needs. A fact whose Why says "made it
work" gives the synthesizer nothing.

These five examples cover the canonical Class A through Class D
shapes. Mirror the Why prose density; mirror the
`candidateClass` / `candidateSurface` routing.

### Worked example 1 — bind 0.0.0.0 + trust the L7 proxy (Class B, API role)

**The change you'd make in `src/main.ts`**:

```typescript
const app = await NestFactory.create(AppModule);
app.set('trust proxy', true);
await app.listen(parseInt(process.env.PORT, 10), '0.0.0.0');
```

**The fact you record**:

```
zerops_recipe action=record-fact slug=<slug>
  fact={
    topic: "api-bind-and-trust-proxy",
    kind: "porter_change",
    scope: "api/code/main.ts",
    phase: "scaffold",
    changeKind: "code-addition",
    diff: "app.set('trust proxy', true); await app.listen(port, '0.0.0.0');",
    why: "Default Node.js binds to 127.0.0.1, which is unreachable from the L7 balancer (which routes to the container's VXLAN IP). Trust the X-Forwarded-* headers so request.ip and request.protocol reflect the real caller, not the proxy. Both touches happen at app bootstrap.",
    candidateClass: "platform-invariant",
    candidateHeading: "Bind 0.0.0.0 and trust the L7 proxy",
    candidateSurface: "CODEBASE_IG",
    citationGuide: "http-support"
  }
```

**Why this Why is good**: names the platform mechanism (L7 balancer
routes to VXLAN IP), the observable wrong state (unreachable / wrong
request.ip / wrong request.protocol), and the fix (bind + trust). The
codebase-content sub-agent has everything to author IG H3 + cite the
http-support guide.

### Worked example 2 — SIGTERM drain (Class B, runtime-conditional)

**The change you'd make in `src/main.ts`**:

```typescript
const app = await NestFactory.create(AppModule);
app.enableShutdownHooks();
// ... rest of bootstrap
await app.listen(port, '0.0.0.0');
```

**The fact you record**:

```
zerops_recipe action=record-fact slug=<slug>
  fact={
    topic: "api-sigterm-drain",
    kind: "porter_change",
    scope: "api/code/main.ts",
    phase: "scaffold",
    changeKind: "code-addition",
    diff: "app.enableShutdownHooks();",
    why: "Rolling deploys send SIGTERM to the old container while the new one warms up. Without explicit shutdown handling, in-flight requests fail mid-response. NestJS exposes enableShutdownHooks() for this; wire it before app.listen(). Without it, every deploy under load drops some requests.",
    candidateClass: "platform-invariant",
    candidateHeading: "Drain in-flight requests on SIGTERM",
    candidateSurface: "CODEBASE_IG",
    citationGuide: "rolling-deploys"
  }
```

**Why this Why is good**: names the trigger (rolling deploy SIGTERM),
the observable (in-flight requests fail mid-response), the fix
(enableShutdownHooks), and the cost of skipping (every deploy drops
some requests). All four are load-bearing; cutting any one weakens
the synthesizer's prose.

### Worked example 3 — own-key aliases (Class C umbrella)

**The change you'd make in `zerops.yaml`**:

```yaml
run:
  envVariables:
    DB_HOST: ${db_hostname}
    DB_PORT: ${db_port}
    DB_NAME: ${db_dbName}
    CACHE_HOST: ${cache_hostname}
    NATS_HOST: ${broker_hostname}
    # ... etc
```

**The fact you record** (one umbrella fact, NOT per-managed-service):

```
zerops_recipe action=record-fact slug=<slug>
  fact={
    topic: "api-own-key-aliases",
    kind: "field_rationale",
    scope: "api/zerops.yaml/run.envVariables",
    phase: "scaffold",
    fieldPath: "run.envVariables",
    fieldValue: "<see zerops.yaml>",
    why: "Zerops injects cross-service refs as ${db_hostname}, ${cache_port}, ${storage_apiUrl}, etc. Reading those directly couples the app to Zerops naming. Aliasing once at run.envVariables lets the runtime read its own names — DB_HOST, CACHE_PORT, S3_ENDPOINT — and a porter could swap the cluster's naming without code changes. This is recipe preference (porter can read ${db_hostname} directly), not platform-forced."
  }
```

**Why this Why is good**: the prose itself names the routing signal
("recipe preference, not platform-forced") so the codebase-content
sub-agent reads it and routes to zerops.yaml block comment
(Surface 7), not IG (Surface 4). `field_rationale` carries no
`candidateClass` slot — the routing teaching lives in the Why prose.
The misroute that bit run-16 (R-17-C3) gets prevented at recording
time by the "recipe preference" anchor in Why.

### Worked example 4 — per-managed-service connect (Class C per-service)

**The change you'd make in `src/database.module.ts` (NestJS+TypeORM)**:

```typescript
TypeOrmModule.forRoot({
  type: 'postgres',
  host: process.env.DB_HOST,
  port: parseInt(process.env.DB_PORT, 10),
  username: process.env.DB_USER,
  password: process.env.DB_PASSWORD,
  database: process.env.DB_NAME,
  synchronize: false,  // see worked example 5
  // ... entities, migrations
})
```

**The fact you record**:

```
zerops_recipe action=record-fact slug=<slug>
  fact={
    topic: "api-connect-db",
    kind: "porter_change",
    scope: "api/code/database.module.ts",
    phase: "scaffold",
    changeKind: "code-addition",
    diff: "TypeOrmModule.forRoot({ type: 'postgres', host: process.env.DB_HOST, ... })",
    why: "Postgres connection wires through the aliased env vars (DB_HOST → ${db_hostname}). Direct ref reads (${db_hostname} in code) work but couple the runtime to Zerops naming. The aliasing pattern (worked example 3) makes this idiomatic.",
    candidateClass: "intersection",
    candidateHeading: "Connect to Postgres",
    candidateSurface: "CODEBASE_IG",
    citationGuide: "env-var-model"
  }
```

**Why this Why is good**: cross-references worked example 3 (the
aliasing umbrella). The synthesizer sees the chain and understands the
per-service connect is one application of the umbrella pattern, not a
standalone H3 candidate. If the synthesizer cap-decides to fuse all
managed-service connects into one IG H3 ("Connect to all services
through aliased env vars"), this Why supports that decision; if it
keeps them separate, this Why supports that too. Either is rubric-
acceptable; the recording shape doesn't force the synthesizer's hand.

### Worked example 5 — TypeORM synchronize false (Class D, framework × scenario)

**The change you'd make in the same `src/database.module.ts`**:

```typescript
TypeOrmModule.forRoot({
  // ... above fields
  synchronize: false,
  migrations: [...],
})
```

Plus an `initCommands` migrator entry in `zerops.yaml` (see the
`init-commands-model` atom for the per-deploy-key shape — included
in this brief only when the plan declares `HasInitCommands: true`).

**The fact you record**:

```
zerops_recipe action=record-fact slug=<slug>
  fact={
    topic: "api-orm-no-autosync",
    kind: "porter_change",
    scope: "api/code/database.module.ts",
    phase: "scaffold",
    changeKind: "code-addition",
    diff: "synchronize: false",
    why: "Leaving TypeORM synchronize: true makes every fresh container race the others to create tables/indices on first boot. Postgres rejects the loser with 'relation already exists' and the deploy goes red intermittently. Pinning synchronize: false and owning DDL via a zsc execOnce-fired migrator (see field_rationale on run.initCommands) makes the schema converge once per deploy regardless of replica count.",
    candidateClass: "intersection",
    candidateHeading: "ALTER TABLE deadlock under multi-container boot",
    candidateSurface: "CODEBASE_KB",
    citationGuide: "init-commands"
  }
```

**Why this Why is good**: notice the `candidateHeading` is already in
symptom-first shape ("ALTER TABLE deadlock under multi-container
boot"). The Why names the trigger (synchronize: true under multi-
replica), the quoted error string (`relation already exists`), the
observable (deploy goes red intermittently), and the fix (synchronize:
false + execOnce migrator). The synthesizer at phase 5 lifts the
heading + the quoted error verbatim; refinement at phase 8 has nothing
to do because the symptom-first shape was authored at recording.

If you record the heading as `**TypeORM synchronize: false everywhere**`
(the run-16 shape), refinement has to reshape it. Saving the work at
recording time is the discipline.

## What "good" Why content looks like across these examples

Each Why names some subset of:

1. **The trigger** — what condition causes the failure (rolling
   deploy, multi-container boot, default config, etc.).
2. **The platform mechanism** — what Zerops does that creates the
   forcing function (L7 balancer, VXLAN routing, container
   supervisor, app version id keys).
3. **The observable wrong state** — what the porter sees when they
   miss this (504, schema corruption, restart loop, no messages
   flowing).
4. **The fix** — concrete code or config change.
5. **(For recipe-preference items)** — explicit "this is recipe
   preference, not platform-forced" so the synthesizer routes to
   yaml comment, not IG.
6. **(For intersection / Class D)** — the framework × scenario tie
   so the synthesizer knows whether the topic belongs in KB
   (multi-tenant gotcha) or IG (one-time setup).

Cutting any of (1) through (4) weakens the synthesizer's prose. (5)
is the route-correction; (6) is the surface-correction.

## What "bad" Why content looks like

Avoid:

- "Made it work" / "Standard pattern" / "Best practice" — names
  nothing the synthesizer can use.
- Single-sentence Why with only the fix ("Set synchronize: false") —
  no trigger, no observable, no mechanism.
- Why that paraphrases the diff ("Added trust proxy line") — the
  synthesizer can read the diff; the Why is for what the diff
  doesn't carry.
- Why that names the framework version or library version as the
  reason ("TypeORM 0.3 requires this") without naming the actual
  platform-side mechanism — that's library-metadata classification,
  not platform-invariant; usually means the fact shouldn't surface.

