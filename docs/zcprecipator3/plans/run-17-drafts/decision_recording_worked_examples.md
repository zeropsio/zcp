# Drop-in: Worked examples section for `decision_recording.md`

Two append-targets:
- `internal/recipe/content/briefs/scaffold/decision_recording.md` — primary, carries 5 worked examples
- `internal/recipe/content/briefs/feature/decision_recording.md` — cross-references scaffold, carries 2 feature-specific examples

The verbatim Why prose below is canonical — it's the same prose the
engine-emit Class B/C facts carried in run-16 (verified against
`internal/recipe/engine_emitted_facts.go:36-87` lines as of 2026-04-28).
After Tranche 1 retracts engine-emit, agents have to record this Why
themselves; these worked examples teach the shape.

---

## Append to `briefs/scaffold/decision_recording.md` (after the existing content)

---

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
    why: "Zerops injects cross-service refs as ${db_hostname}, ${cache_port}, ${storage_apiUrl}, etc. Reading those directly couples the app to Zerops naming. Aliasing once at run.envVariables lets the runtime read its own names — DB_HOST, CACHE_PORT, S3_ENDPOINT — and a porter could swap the cluster's naming without code changes. This is recipe preference (porter can read ${db_hostname} directly), not platform-forced.",
    classification: "scaffold-decision (config)"
  }
```

**Why this Why is good**: explicitly names the classification
("scaffold-decision (config)") AND the reasoning ("recipe
preference, not platform-forced"). The codebase-content sub-agent
reads "recipe preference" and routes to zerops.yaml block comment
(Surface 7), not IG (Surface 4). The misroute that bit run-16 (R-17-C3)
gets prevented at recording time.

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

Plus an `initCommands` migrator entry in `zerops.yaml`:

```yaml
run:
  initCommands:
    - zsc execOnce ${appVersionId}-migrate --retryUntilSuccessful -- npm run migrate
```

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

---

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

---

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

---

## Append to `briefs/feature/decision_recording.md` (after the existing content)

---

## Worked examples — feature-phase porter_change shapes

Feature phase records typically look different from scaffold —
narrower scope, scenario-tied, often a Class D shape (framework ×
feature). Two canonical examples:

### Worked example F1 — cross-origin custom headers (Class D, cache feature)

**The change you'd make in `src/main.ts`**:

```typescript
app.enableCors({
  origin: [process.env.APP_URL, process.env.APP_DEV_URL],
  credentials: true,
  exposedHeaders: ['X-Cache', 'X-Cache-Elapsed-Ms'],
});
```

**The fact you record**:

```
zerops_recipe action=record-fact slug=<slug>
  fact={
    topic: "api-cors-exposed-headers",
    kind: "porter_change",
    scope: "api/code/main.ts",
    phase: "feature",
    changeKind: "code-addition",
    diff: "exposedHeaders: ['X-Cache', 'X-Cache-Elapsed-Ms']",
    why: "Browsers hide every non-CORS-safelisted response header from JS on cross-origin fetches. The cache panel's X-Cache: HIT|MISS and X-Cache-Elapsed-Ms headers are visible from curl but undefined from the SPA unless the api lists them in app.enableCors({ exposedHeaders: [...] }). Without exposedHeaders, the cache demo silently shows 'undefined' on every request — porter can't tell hit from miss. This is intersection — CORS spec + Zerops cross-origin-by-default subdomain shape.",
    candidateClass: "intersection",
    candidateHeading: "Cross-origin custom headers need exposedHeaders",
    candidateSurface: "CODEBASE_KB",
    citationGuide: ""
  }
```

**Why this Why is good**: feature-phase facts often surface to KB
(intersection class). The Why explicitly names the trigger (browsers
hide non-safelisted headers), the symptom (undefined / can't tell
hit from miss), the fix (exposedHeaders), and the classification
reason (CORS spec + Zerops subdomain shape). The synthesizer at
phase 5 has everything to author the KB bullet at 9.0 anchor shape.

### Worked example F2 — streamed proxy duplex (Class D, storage feature)

**The change you'd make in `src/storage/proxy.ts`**:

```typescript
const upstream = await fetch(s3Url, {
  method: 'PUT',
  body: req,           // streamed
  duplex: 'half',      // required when body is a stream
});
```

**The fact you record**:

```
zerops_recipe action=record-fact slug=<slug>
  fact={
    topic: "api-fetch-stream-duplex",
    kind: "porter_change",
    scope: "api/code/storage/proxy.ts",
    phase: "feature",
    changeKind: "code-addition",
    diff: "duplex: 'half'",
    why: "Node 18+ undici fetch rejects body=stream without duplex: 'half' — the request fails with 'TypeError: RequestInit: duplex option is required when sending a body.' This applies to any streamed-body proxy (storage upload, large-file forwarding). The error is at request-build time, not at runtime, so the proxy crashes on first call.",
    candidateClass: "library-metadata",
    candidateHeading: "",
    candidateSurface: "",
    citationGuide: ""
  }
```

**Why this Why is good**: notice the candidate fields are empty.
This is library-metadata classification (Node 18+ undici quirk) —
per spec, library-metadata routes to NO surface. Recording it
preserves the teaching for code comments at the call site, but the
synthesizer at phase 5 sees the classification and discards from IG/
KB candidate sets. This is a discard-class fact recorded for
internal teaching, not for porter-facing surfaces.

If you record this with `candidateSurface: "CODEBASE_KB"` and
`candidateClass: "platform-invariant"`, the synthesizer ships a KB
bullet that's actually about Node + undici, not about Zerops. R-17
classification routing closure depends on getting this distinction
right at recording time.

---

## Notes for the fresh instance

1. Verbatim Why prose for examples 1, 2, 3 was lifted from
   `internal/recipe/engine_emitted_facts.go` Class B/C hardcoded Why
   strings (lines 41-105 as of pre-Tranche-1). After Tranche 1
   retracts engine-emit, those Why values disappear from the engine —
   these worked examples are how the deploy-phase agents learn the
   shape.

2. The "What good vs bad Why looks like" section lives in the
   scaffold atom only; the feature atom cross-references it.

3. The five scaffold examples cover Class A (engine-stamped IG #1
   from yaml), Class B (bind, sigterm — universal-for-role), Class C
   umbrella (own-key-aliases) + per-service (db connect), Class D
   (framework × scenario, TypeORM synchronize). The two feature
   examples cover intersection (CORS exposed-headers) and library-
   metadata-discard (Node undici duplex). Together they span every
   classification × surface row in the spec compatibility table.

4. Cross-check the verbatim Why quotes against
   `internal/recipe/engine_emitted_facts.go` lines 41-105 before
   committing — if the source code has drifted, update the worked
   examples to match.
