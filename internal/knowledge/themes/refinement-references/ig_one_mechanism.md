# Reference: IG one mechanism per H3

## Why this matters

Surface 4 (per-codebase Integration Guide) is the porter's
table-of-contents into the platform-forced changes they have to make
in their own code. Each H3 heading is a discoverable entry point;
fusing two or three independent mechanisms into a single H3 muddles
the porter's search. A porter scanning the TOC for "rolling deploys"
or "trust proxy" needs each topic at its own H3.

Both reference recipes consistently land at one mechanism per H3.
Run-16 apidev fused three mechanisms into IG #2 (`Bind 0.0.0.0,
trust the proxy, drain on SIGTERM`) under cap pressure from the
engine-emit shells (§4.1 of run-17-prep). With Tranche 1 retracting
those shells, refinement at phase 8 splits the fused H3 back into
three discoverable headings.

This atom teaches the refinement sub-agent when an H3 is fused (and
should split), when an H3 is genuinely one mechanism (and should
HOLD), and when a misrouted H3 should leave the IG entirely.

## Pass examples (drawn from references)

### Pass 1, 2, 3 — laravel-showcase sequential H3s, each one mechanism

**Why this works**: heading names exactly one platform-forced
change ("Trust the reverse proxy"). Body opens with the platform
mechanism (SSL termination + reverse proxy forwarding), names the
observable wrong state without trust (CSRF rejection + http://
URL generation), and ends with the concrete code diff. Porter
searching "trust proxy on Zerops" finds this H3 directly.

> *"### 3. Configure Redis client"*
>
> *"Laravel defaults to the `phpredis` C extension. On Zerops, the"*
> *"`predis` pure-PHP client avoids needing a compiled extension."*
> *"Install via Composer and set `REDIS_CLIENT=predis` in your"*
> *"environment:"*
>
> ```bash
> composer require predis/predis
> ```

**Why this works**: heading names one mechanism (Redis client
choice). Body names the framework default (`phpredis`), the
platform constraint (compiled extension not in base image), the
chosen alternative (`predis` pure-PHP), and the install step.
Trade-off two-sided. Porter searching "Redis on Zerops PHP" finds
this.

> *"### 4. Configure S3 object storage"*
>
> *"Install the S3 Flysystem adapter and set `FILESYSTEM_DISK=s3` with"*
> *"the Zerops object storage credentials. Path-style endpoints are"*
> *"mandatory for the MinIO-backed storage:"*
>
> ```bash
> composer require league/flysystem-aws-s3-v3
> ```

**Why this works**: heading names one mechanism (object storage
configuration). Body names the install step, the env var to set
(`FILESYSTEM_DISK=s3`), and the platform constraint (path-style
mandatory because MinIO-backed). Porter searching "S3 Zerops" or
"MinIO path-style" finds this.

The three H3s in sequence demonstrate the reference shape: each H3
is one porter edit, one mechanism, one search path. The IG cap
holds because the recipe only asks for these (plus `### 1. Adding
zerops.yaml` and `### 5. Configure Meilisearch search`).

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
Action 4 in `zerops://themes/refinement-references/refinement_thresholds`.

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

**Why this fails**: this is recipe *preference*, not platform-forced.
A porter can read `${db_hostname}` directly in code and the app
works fine. The recipe author chose to alias for portability — that's
a scaffold-decision visible in `zerops.yaml` field values, which per
spec routes to zerops.yaml block comments (Surface 7), not IG
(Surface 4).

Compounding: the env-var block is already shipped verbatim in IG #1
(the engine-stamped `Adding zerops.yaml`), so the porter sees the
aliasing pattern there. IG #3 duplicates the surface-area without
adding teaching.

**Refined to**: move to zerops.yaml block comment; remove from IG.

The refinement sub-agent emits two `record-fragment` calls:

1. Read the current `codebase/apidev/zerops-yaml` whole-yaml body
   (the codebase-content sub-agent already authored every block-level
   comment in it), splice the new comment block above the existing
   `run.envVariables:` block, then replace the whole-yaml fragment
   verbatim with the edited body:

   ```
   record-fragment mode=replace fragmentId=codebase/apidev/zerops-yaml fragment=<full edited yaml>
   ```

   The new block to splice in (above `run.envVariables:`):

   ```
   # Zerops injects cross-service refs project-wide as ${db_hostname},
   # ${cache_port}, ${storage_apiUrl}, ${search_masterKey}, etc. Reading
   # those directly from code couples the app to Zerops naming — the
   # aliasing below maps them once so your runtime reads its own names.
   # Feel free to drop this aliasing if you don't mind reading
   # ${db_hostname} directly.
   ```

2. `record-fragment fragmentId=codebase/apidev/integration-guide/3 mode=replace` with body that REMOVES the H3 entirely. (Refinement renumbers: original IG #4 becomes #3, #5 becomes #4. Cap holds at 5 ≤ 5; if cap was tight, this drop frees a slot.)

This is the **two-surface refinement** — Action 4 in
`zerops://themes/refinement-references/refinement_thresholds` is the only refinement action that touches
two fragments in one logical operation. Atom-side teaching for both
surfaces lives here.

### Fail 3 — H3 names a fix without naming the platform-forced cause (HOLD case)

```
### 5. Talk to Zerops object storage with `forcePathStyle` + `apiUrl`

Zerops's object storage is S3-compatible at the protocol level but
runs against MinIO behind the scenes. Two AWS-SDK options matter:

- `forcePathStyle: true` — MinIO doesn't accept virtual-hosted-style
  bucket URLs.
- `endpoint: storage_apiUrl` — point the SDK at the project-internal
  apiUrl, not the public AWS-region endpoint.
```

**Why this is borderline (HOLD case)**: H3 heading IS one mechanism
(talking to Zerops object storage) and the body covers two specific
SDK options under that one mechanism. This is acceptable shape — the
two options are tied to the same call site (the S3 client constructor).

Show as a HOLD case to teach the refinement sub-agent that not every
multi-bullet H3 is a fusion candidate. The test: do the items belong
to the same call site / mechanism class? If yes, HOLD. If no, ACT
(split).

## The heuristic

For every IG H3 heading, ask: **does the heading name one
porter-edit, or two or more independent edits?**

A heading is **one mechanism** when:
- All items in the body are at the same call site (one constructor,
  one config block, one `app.set`-style invocation).
- The items share a single failure mode (the porter who skips ONE
  item produces the same observable failure as the porter who skips
  ALL of them).
- A porter searching the TOC for any of the items finds the same H3.

A heading is **fused** (and should split) when:
- The body uses multiple sub-items (numbered list, multiple code
  blocks) AND each sub-item names a *different* platform mechanism
  (HTTP routability ≠ header trust ≠ graceful exit).
- The sub-items have *different observable failure modes* (binding
  127.0.0.1 → balancer can't reach; missing trust proxy → wrong
  request.ip; missing SIGTERM drain → in-flight requests fail).
- A porter searching the TOC for any one of the sub-items might or
  might not find the H3 depending on which token the heading
  prioritized.

A heading is **misrouted** (and should leave IG) when:
- The change is recipe *preference*, not platform-forced (the
  porter could skip it and the app still works on Zerops).
- The corresponding zerops.yaml block-comment slot is empty or could
  absorb the explanation (Action 4 pairing).

A reshape from fused to split is unambiguous when:

1. The body already separates the mechanisms into sub-items — the
   split mostly promotes sub-items to H3s.
2. The split would not push IG count above the 5-item cap (or
   compensating Action 4 frees a slot).
3. Each new H3 has its own load-bearing failure mode + its own fix.

A reshape from misrouted to moved (Action 4) is unambiguous when:

1. The H3 covers a recipe choice, not a platform forcing (the porter
   could read raw `${db_hostname}` and the app works).
2. The corresponding yaml block-comment slot is empty or absorbable.
3. The IG count after removal stays at or above the 3-item floor
   (IG #1 yaml + ≥ 2 platform-forced).

## When to HOLD (refinement does not act)

- **The H3 covers ONE mechanism with multiple SDK options at the
  same call site** — Pass 3 above (object storage `forcePathStyle`
  + `apiUrl`). One H3, two options, one call site, one porter
  edit. HOLD.
- **Splitting would push IG count above 5** — refinement does NOT
  add new H3s past the cap. Instead, evaluate Action 4 (route a
  recipe-preference H3 to yaml comment) for any H3 that could
  leave. If no Action 4 candidate exists, HOLD; surface
  "IG cap pressure" notice.
- **The fused changes are genuinely tied** (same call site, same
  mechanism class). Rare; check by asking "does the porter make
  this change as one edit or two?"
- **The fragment is in a slotted form** (`integration-guide/2`,
  `integration-guide/3`) AND the slot count is already at 5 — same
  cap consideration. HOLD or trigger Action 4 first.
- **The H3 names a platform-forced change but the body's mechanisms
  are at the SAME call site** — even if the heading is multi-token
  ("Bind, trust proxy, drain on SIGTERM"), if `app.listen` +
  `app.set('trust proxy')` + `app.enableShutdownHooks()` are all at
  app-bootstrap, the porter makes them as one edit. **Note**: this
  is the run-16 fused case (Fail 1); the call-site argument doesn't
  hold there because the three calls have three different observable
  failure modes. Use observable-failure-mode-distinct as the
  tie-breaker, not call-site proximity alone.

The edit threshold: if the split would produce H3s that share
an observable failure mode, the split is wrong. HOLD. Refinement is
a pull-toward-reference shape, not a structural rewrite.
