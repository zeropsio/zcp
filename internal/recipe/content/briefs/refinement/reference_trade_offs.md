# Reference: trade-off two-sidedness

## Why this matters

A KB bullet that names only the chosen path teaches porters *what to
do* but not *why this is the choice*. When the porter encounters a
context where the rejected alternative looks attractive — their own
preference, their own constraint, their own scale — they can't weigh
the trade-off without knowing what was rejected and what would push
them to revisit.

Both reference recipes consistently name rejected alternatives.
Run-16 KB bullets often name only the chosen path, leaving the
porter without the second side of the trade-off. This atom teaches
the refinement sub-agent when to expand a one-sided bullet to
two-sided and when to HOLD because the alternative would bloat the
bullet past the body cap.

## Pass examples (drawn from references)

### Pass 1 — chosen + rejected + why rejected (showcase apidev KB)

> *"**Predis over phpredis** — The `php-nginx` base image does not"*
> *"include the `phpredis` C extension. Use the `predis/predis` Composer"*
> *"package and set `REDIS_CLIENT=predis` to avoid \"class Redis not"*
> *"found\" errors."*

**Why this works**: names the chosen path (`predis`) and the
rejected alternative (`phpredis` C extension) and the reason rejected
(base image doesn't include it → "class Redis not found" at runtime).
The porter who knows phpredis exists and is faster understands why
this recipe didn't use it.

### Pass 2 — chosen + rejected via what-not-to-do (showcase apidev KB)

> *"**Cache commands in `initCommands`, not `buildCommands`** —"*
> *"`config:cache`, `route:cache`, and `view:cache` bake absolute paths"*
> *"into their cached files. The build container runs at `/build/source/`"*
> *"while the runtime serves from `/var/www/`. Caching during build"*
> *"produces paths like `/build/source/storage/...` that crash at runtime"*
> *"with \"directory not found.\""*

**Why this works**: stem itself names the chosen path
(`initCommands`) and the rejected alternative (`buildCommands`).
Body explains the mechanism (path baking + container path
divergence) AND the consequence of the rejected path ("crash at
runtime with 'directory not found'"). The porter who would
intuitively put cache commands in `buildCommands` (because that's
where most "build the app" steps go) reads this and understands
why their intuition is wrong on Zerops.

### Pass 3 — chosen + rejected with explicit "do not" (showcase apidev KB)

> *"**Vite manifest missing on dev after fresh deploy** — the `dev`"*
> *"setup intentionally omits `npm run build` from `buildCommands` so"*
> *"the HMR workflow (`npm run dev` via SSH) stays fast. Any view"*
> *"rendering `@vite(...)` therefore 500s with `Vite manifest not"*
> *"found at: /var/www/public/build/manifest.json` on the first"*
> *"request after a `zerops_deploy`. ... **Do NOT add `npm run build`"*
> *"to dev `buildCommands`** — it adds ~20–30 s to every `zcli push`"*
> *"and defeats the HMR-first design."*

**Why this works**: names the chosen path (HMR-first dev with
manual `npm run build`), names the porter's likely instinct
("just add `npm run build` to dev `buildCommands`"), and names the
cost of taking that instinct (~20–30 s per `zcli push` + defeats
the design). The trigger condition ("the porter's instinct") is
implicit but the rejection cost is concrete.

### Pass 4 — chosen + rejected via "this works but" (run-16 apidev KB, retained)

> *"**Internal traffic between Zerops services is plain HTTP** — SSL"*
> *"terminates at the L7 balancer; `https://search:7700` between"*
> *"services fails the TLS handshake. Wire `MEILI_HOST:"*
> *"http://search:${search_port}`. Reaching a sibling via its public"*
> *"`${search_zeropsSubdomain}` works but routes the call out and back"*
> *"through the public balancer for no reason."*

**Why this works**: chosen path (`http://` between services).
Rejected alternative #1 (`https://` between services) — fails the
TLS handshake. Rejected alternative #2 (public subdomain) — works
but routes externally for no reason. Two rejected alternatives,
each with the explicit cost. This is run-16 doing it RIGHT; cite it
to teach the sub-agent that the same author can hit both shapes.

### Pass 5 — chosen + rejected via "do not add" (showcase apidev KB)

> *"**`APP_KEY` is project-level** — Laravel's encryption key must be"*
> *"shared across all services that read the same database (app +"*
> *"worker both need the same key for sessions and encrypted columns)."*
> *"Set it once at project level in Zerops; do not add it per-service"*
> *"or in `zerops.yaml envVariables`."*

**Why this works**: names the chosen path (project level) and the
rejected alternative (per-service or per-zerops.yaml). Names *why*
the chosen wins ("must be shared across services" + concrete
consequence: sessions and encrypted columns). The porter who would
default to setting env vars per-service (because that's where most
service config lives) reads this and routes APP_KEY differently.

### 9.0 anchor — chosen + rejected + when-to-revisit trigger (hand-crafted)

> *"**Predis over phpredis** — The `php-nginx` base image does not"*
> *"include the `phpredis` C extension. Use `predis` (pure-PHP) to"*
> *"avoid `\"class Redis not found\"` at runtime. `phpredis` *is*"*
> *"marginally faster on hot paths and worth installing if your"*
> *"workload hammers Redis at >10k ops/sec — at that scale, rebuild"*
> *"the base image with the C extension. At showcase-tier traffic, the"*
> *"perf delta is noise."*

**(Hand-crafted; reference recipes don't hit 9.0 verbatim on this
criterion.)**

**Why this works**: names chosen + rejected + WHY rejected wins at
this scale + the *trigger condition for revisiting* (>10k ops/sec).
A porter on a higher-throughput app reads this and knows when to
switch decisions. The 9.0 lift over Pass 1 is the trigger condition.

## Fail examples (drawn from run-16)

### Fail 1 — chosen path only, no rejected (run-16 workerdev KB)

> *"**Liveness without HTTP** — Because the worker has no port, the"*
> *"project dashboard's \"is it up?\" answer comes from container logs"*
> *"and a SQL spot-check, not curl. The canonical probe is `SELECT"*
> *"count(*), max(received_at) FROM events;` against the project"*
> *"Postgres. Document this on the dashboard so on-call engineers"*
> *"don't reach for the missing health endpoint."*

**Why this fails**: names the chosen liveness probe (SQL count from
events). No rejected alternative — the bullet implies "you can't use
HTTP because there's no port" but doesn't name the obvious alternative
(adding a small `/healthz` listener on the worker for observability)
or why it loses (extra HTTP surface area, lifecycle complexity for a
process that has no other HTTP needs). The porter who'd default to
adding an HTTP listener doesn't learn why this recipe didn't.

**Refined to**:

> *"**Liveness without HTTP** — Because the worker has no port, the"*
> *"project dashboard's \"is it up?\" answer comes from container logs"*
> *"and a SQL spot-check (`SELECT count(*), max(received_at) FROM"*
> *"events;`), not curl. Adding a small `/healthz` listener works but"*
> *"changes the worker into a queue-AND-HTTP service — it pulls in"*
> *"port management, SIGTERM-aware HTTP shutdown, and another readiness"*
> *"surface. The SQL probe is sufficient at single-replica scale; revisit"*
> *"if you want platform-level health gating."*

The reshape adds the rejected alternative (`/healthz` listener) and
why it loses (queue-AND-HTTP cost) and a trigger to revisit
(platform-level health gating). Stem preserved.

### Fail 2 — chosen path only, no rejected (run-16 workerdev KB)

> *"**Subject typo silently stops delivery** — NATS has no schema"*
> *"enforcement, so a one-character typo in the subscribed subject"*
> *"pattern or queue-group name compiles fine and the worker just sits"*
> *"idle. Symptom: api logs successful publishes, the events table"*
> *"never grows. Cross-check the publisher and subscriber strings"*
> *"before assuming the broker is broken."*

**Why this fails**: names the chosen verification approach
("cross-check strings"). No rejected alternative — there's a
defensible alternative (defining shared subject/queue-group constants
in a TypeScript module both sides import) that prevents the typo
class entirely. The porter who'd reach for shared constants doesn't
learn why this recipe didn't.

**Refined to**:

> *"**Subject typo silently stops delivery** — NATS has no schema"*
> *"enforcement, so a one-character typo in the subscribed subject"*
> *"pattern or queue-group name compiles fine and the worker just sits"*
> *"idle. Symptom: api logs successful publishes, the events table"*
> *"never grows. Defining shared subject + queue-group constants in a"*
> *"TS module both sides import prevents the typo class but couples"*
> *"the codebases at the type level — at this recipe's two-codebase"*
> *"scale, cross-checking strings during code review is cheaper than"*
> *"the coupling. Revisit if the subject set grows past ~10 entries."*

The reshape adds the rejected alternative (shared TS constants) and
why it loses at this scale (cross-codebase coupling cost) and a
trigger to revisit (subject set growth past ~10).

## The heuristic

For every KB bullet, ask: **is there a defensible alternative the
porter could pick that would be wrong-by-default in this recipe?**

Per rubric Criterion 4 scoring:

| Body content | Score impact |
|---|---|
| Names the alternative AND why it loses | +1.0 |
| Names the alternative + names a trigger to revisit | +1.5 |
| Names only the chosen path | -1.0 |

A reshape from one-sided to two-sided is unambiguous when ALL of:

1. The chosen path is platform-conditional or scale-conditional —
   there's a real alternative the porter might pick.
2. The rejected alternative is namable from the recorded facts, the
   spec, or `zerops_knowledge` runtime queries.
3. Adding the rejected alternative + the rejection reason fits within
   the 4-sentence body cap (Surface 5 hard cap).

A reshape from two-sided (8.5) to two-sided + revisit-trigger (9.0)
is unambiguous when:

1. The trigger condition is concrete (numeric threshold, scale
   marker, observable porter signal).
2. The trigger is not a rationalization — there's a real porter
   context where the rejected alternative becomes correct.
3. The reshape stays within the body cap.

## When to HOLD (refinement does not act)

- **No alternative exists** — the platform offers one path (e.g.
  "Subdomain refs already carry `https://`" — there's no
  defensible alternative to "don't prepend a scheme"). Score `n/a`
  on this criterion; HOLD action.
- **Alternative would push body past 4-sentence cap** — better to
  add a separate IG note than bloat the bullet. HOLD; surface
  "trade-off too verbose for KB" notice.
- **Alternative is unfamiliar enough that naming it without
  explaining it leaves the porter more confused than informed** —
  the alternative needs its own teaching, not a name-drop. HOLD.
- **Alternative is the porter's framework default** — naming it as
  "rejected" risks reading as recipe-author opinion, not platform
  trade-off. HOLD if the rejection reason isn't clearly platform-
  side.
- **Body already names the alternative AND why** — bullet is at
  8.5+ tier. HOLD unless adding a revisit-trigger lifts to 9.0
  unambiguously.

The 100%-sure threshold: if the rejected alternative requires
inventing a porter context the recipe doesn't actually witness,
HOLD. Refinement is post-hoc shaping, not speculative balance-
adding.
