# Drop-in: FAIL section for `reference_ig_one_mechanism.md`

This file is the partial content for the FAIL examples section of
`internal/recipe/content/briefs/refinement/reference_ig_one_mechanism.md`.
The fresh instance integrates this with PASS examples extracted
verbatim from laravel-jetstream + laravel-showcase IG sequences.

Each FAIL example is drawn verbatim from
`docs/zcprecipator3/runs/16/apidev/README.md`. Cross-checked
2026-04-28.

---

## Fail examples (drawn from run-16 misses)

### Fail 1 — three platform-forced changes fused into one H3

```
### 2. Bind `0.0.0.0`, trust the proxy, drain on SIGTERM

Zerops terminates SSL at its L7 balancer and forwards over the
project VXLAN. Three touches at bootstrap make the api safe behind
that hop and during rolling deploys:

1. **Bind to 0.0.0.0** — the default `127.0.0.1` is unreachable from
   the balancer's container-side IP. Pass `'0.0.0.0'` to
   `app.listen(...)` so the bind address matches the route target.
2. **Trust the proxy** — without this, NestJS sees the balancer's
   internal IP as `request.ip` and treats every request as
   not-HTTPS (`request.protocol === 'http'`). Call `app.set('trust
   proxy', true)` so the X-Forwarded-* headers are honored.
3. **Drain on SIGTERM** — rolling deploys send SIGTERM to the
   outgoing container. Without explicit shutdown handling, in-flight
   requests fail mid-response. Wire NestJS's
   `enableShutdownHooks()` so the framework drains before exit.
```

**Source**: run-16 apidev README IG #2.

**Why this fails**: H3 heading bundles three independent platform
mechanisms into one. The body actually does the right thing — three
numbered sub-items, each with its own mechanism + fix — but the H3
heading is the porter's table-of-contents entry and it muddles three
distinct platform forcings.

A porter searching the README's TOC for "rolling deploys" doesn't
find an H3. A porter searching for "trust proxy" finds the same H3 as
the porter searching for "bind 0.0.0.0". Three failure modes need
three discoverable headings.

**Refined to** (three separate H3s, ordered by load-bearing-ness):

```
### 2. Bind `0.0.0.0` for L7 balancer reachability

Zerops's L7 balancer routes to the container's VXLAN IP. The default
NestJS bind to `127.0.0.1` is unreachable from that route — the
balancer hands traffic at the container address but no listener is
there. Pass `'0.0.0.0'` to `app.listen(...)` so the bind matches the
route target.

```diff
- await app.listen(port);
+ await app.listen(port, '0.0.0.0');
```

The `http-support` guide covers the L7-to-VXLAN routing model.

### 3. Trust the L7 proxy for `request.ip` + `request.protocol`

Without `trust proxy` set, NestJS sees the balancer's internal IP as
`request.ip` and treats every request as plain HTTP
(`request.protocol === 'http'`) even when the porter's CDN has TLS
on. Call `app.set('trust proxy', true)` so X-Forwarded-* headers are
honored.

```diff
+ app.set('trust proxy', true);
```

### 4. Drain in-flight requests on SIGTERM for rolling deploys

Zerops rolling deploys send SIGTERM to the outgoing container while
the new one warms up. Without explicit shutdown handling, in-flight
requests fail mid-response. NestJS exposes
`enableShutdownHooks()` for this; wire it before `await app.listen()`.

```diff
+ app.enableShutdownHooks();
  await app.listen(port, '0.0.0.0');
```

The `rolling-deploys` guide covers the SIGTERM lifecycle.
```

The split adds three discoverable H3s. The cap stays at 5 because IG
#3 (own-key-aliases) is moved to a zerops.yaml block comment via
Action 4 in `refinement_thresholds.md`.

### Fail 2 — recipe-preference H3 in IG (misroute)

```
### 3. Alias platform env refs to your own names in `zerops.yaml`

Zerops injects cross-service refs project-wide as `${db_hostname}`,
`${cache_port}`, `${storage_apiUrl}`, `${search_masterKey}`, etc.
Reading those directly from your code couples the app to Zerops
naming. Map them once in `run.envVariables`; the runtime reads its
own names:

```yaml
envVariables:
  DB_HOST: ${db_hostname}
  DB_PORT: ${db_port}
  CACHE_HOST: ${cache_hostname}
  ...
```
```

**Source**: run-16 apidev README IG #3.

**Why this fails**: this is recipe *preference*, not platform-forced.
A porter can read `${db_hostname}` directly in code and the app
works fine. The recipe author chose to alias for portability — that's
a scaffold-decision (config), which per spec routes to zerops.yaml
block comments (Surface 7), not IG (Surface 4).

Compounding: the env-var block is already shipped verbatim in IG #1
(the engine-stamped `Adding zerops.yaml`), so the porter sees the
aliasing pattern there. IG #3 duplicates the surface-area without
adding teaching.

**Refined to**: move to zerops.yaml block comment; remove from IG.

The refinement sub-agent emits two `record-fragment` calls:

1. `record-fragment fragmentId=codebase/apidev/zerops-yaml-comments/run-envvars-aliasing` with body:

```
# Zerops injects cross-service refs project-wide as ${db_hostname},
# ${cache_port}, ${storage_apiUrl}, ${search_masterKey}, etc. Reading
# those directly from code couples the app to Zerops naming — the
# aliasing below maps them once so your runtime reads its own names.
# Feel free to drop this aliasing if you don't mind reading
# ${db_hostname} directly.
```

2. `record-fragment fragmentId=codebase/apidev/integration-guide/3 mode=replace` with body that REMOVES the H3 entirely. (Refinement renumbers: original IG #4 becomes #3, #5 becomes #4. Cap holds at 5 ≤ 5; if cap was tight, this drop frees a slot.)

### Fail 3 — H3 names a fix without naming the platform-forced cause

```
### 5. Talk to Zerops object storage with `forcePathStyle` + `apiUrl`

Zerops's object storage is S3-compatible at the protocol level but
runs against MinIO behind the scenes. Two AWS-SDK options matter:

- `forcePathStyle: true` — MinIO doesn't accept virtual-hosted-style
  bucket URLs.
- `endpoint: storage_apiUrl` — point the SDK at the project-internal
  apiUrl, not the public AWS-region endpoint.
```

**Source**: run-16 apidev README IG #5.

**Why this is borderline (HOLD case)**: H3 heading IS one mechanism
(talking to Zerops object storage) and the body covers two specific
SDK options under that one mechanism. This is acceptable shape — the
two options are tied to the same call site (the S3 client constructor).

Show as a HOLD case to teach the refinement sub-agent that not every
multi-bullet H3 is a fusion candidate. The test: do the items belong
to the same call site / mechanism class? If yes, HOLD. If no, ACT
(split).

---

## Notes for the fresh instance integrating these

1. The Fail 1 reshape is the canonical "split fused H3" example. The
   atom (`reference_ig_one_mechanism.md`) should present it as the
   target shape; the three H3s collectively replace IG #2 in run-16.

2. The Fail 2 reshape is unique among refinement actions in that it
   touches TWO surfaces (yaml comment + IG) in one logical
   operation. Document this pairing in the atom — Action 4 in
   `refinement_thresholds.md` describes it but the atom should give
   the worked example.

3. The Fail 3 case is intentionally a HOLD example. Frame in the
   atom's "When to HOLD" section.

4. PASS examples come from laravel-showcase apps repo IG (sequential
   H3s each one platform mechanism: see
   `/Users/fxck/www/laravel-showcase-app/README.md` IG section). Cross-
   check verbatim during integration.

5. After integrating, verify the FAIL quotes match
   `docs/zcprecipator3/runs/16/apidev/README.md` byte-for-byte.
