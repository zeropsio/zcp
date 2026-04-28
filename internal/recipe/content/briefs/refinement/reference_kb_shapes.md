# Reference: KB stem shapes

## Why this matters

KB bullets (Surface 5) exist for porters who hit a symptom and
search for it. Author-claim stems (`**Library X: setting Y**`) are
unsearchable — the porter doesn't know to search for the recipe's
directive. The dominant Run-17 quality lift on Criterion 1 comes
from reshaping these into symptom-first stems (or directive stems
tightly mapped to an observable failure named in the body opener).

Run-16 shipped roughly 5/15 KB bullets in author-claim shape. The
reference recipes consistently land at 8.5+ — symptom-first stems
or directive-tightly-mapped — because both authors thought about
the porter's search path before they wrote the stem.

## Pass examples (drawn from references)

### Pass 1 — symptom-first anchor (showcase apidev KB)

> *"**No `.env` file** — Zerops injects environment variables as OS"*
> *"env vars. Creating a `.env` file with empty values shadows the OS"*
> *"vars, causing `env()` to return `null` for every key that appears"*
> *"in `.env` even if the platform has a value set."*

**Source**: `laravel-showcase-app/README.md:349`.

**Why this works**: stem names the *thing porters do wrong* (creating
a `.env` file). Body's first sentence carries the platform mechanism
(env vars injected as OS env vars), the second sentence carries the
*observable wrong state* (`env()` returns null). The porter
searching "env() returns null on Zerops" or ".env file Zerops"
finds this in one search.

This is the canonical 8.5 anchor for symptom-first shape.

### Pass 2 — directive-tightly-mapped-to-symptom (showcase apidev KB)

> *"**Cache commands in `initCommands`, not `buildCommands`** —"*
> *"`config:cache`, `route:cache`, and `view:cache` bake absolute paths"*
> *"into their cached files. The build container runs at `/build/source/`"*
> *"while the runtime serves from `/var/www/`. Caching during build"*
> *"produces paths like `/build/source/storage/...` that crash at runtime"*
> *"with \"directory not found.\""*

**Source**: `laravel-showcase-app/README.md:350`.

**Why this works**: stem is a directive ("put cache in initCommands,
not buildCommands"), but body opens with the platform mechanism
(absolute path baking + container path divergence) and lands on the
*observable error string* in the final sentence (`"directory not
found"`). The porter searching for the error string finds this.
Acceptable directive-mapped shape because the failure mode is named
explicitly in the body.

### Pass 3 — directive + observable opener (showcase apidev KB)

> *"**Object storage requires path-style** — Zerops object storage uses"*
> *"MinIO, which requires `AWS_USE_PATH_STYLE_ENDPOINT=true`. Without"*
> *"it, the SDK attempts virtual-hosted bucket URLs that MinIO cannot"*
> *"resolve."*

**Source**: `laravel-showcase-app/README.md:354`.

**Why this works**: stem is a directive ("object storage requires
path-style"), but body opens with the platform mechanism (MinIO
backing) and the rejected alternative's failure ("MinIO cannot
resolve" the virtual-hosted URLs). Porter searching for "MinIO bucket
URL" or "S3 path-style on Zerops" finds this. Tight 3-sentence body.

### Pass 4 — symptom-first with quoted error (showcase apidev KB)

> *"**Vite manifest missing on dev after fresh deploy** — the `dev`"*
> *"setup intentionally omits `npm run build` from `buildCommands` so"*
> *"the HMR workflow (`npm run dev` via SSH) stays fast. Any view"*
> *"rendering `@vite(...)` therefore 500s with `Vite manifest not"*
> *"found at: /var/www/public/build/manifest.json` on the first"*
> *"request after a `zerops_deploy`."*

**Source**: `laravel-showcase-app/README.md:355`.

**Why this works**: stem names the symptom ("Vite manifest missing
on dev after fresh deploy"). Body explains the platform-side cause
(intentional omit of `npm run build` from dev `buildCommands`), the
observable HTTP status (500), and the *quoted error string* (`Vite
manifest not found at: ...`). Three search paths converge on this
bullet — symptom phrase, status code, error string.

This is the 9.0 anchor shape (symptom-first stem + quoted error +
HTTP status all present).

### 9.0 anchor — symptom-first + quoted error + revisit-trigger (reshape target)

> *"**ALTER TABLE deadlock under multi-container boot** — Leaving the"*
> *"ORM `synchronize: true` makes every fresh container race the"*
> *"others to create tables/indices on first boot. Postgres rejects"*
> *"the loser with `relation already exists` and the deploy goes red"*
> *"intermittently. Pin `synchronize: false`, own the schema via a"*
> *"`zsc execOnce`-fired migrator, and the failure mode disappears"*
> *"regardless of replica count."*

**(Reshape target; this is the canonical above-golden answer the
refinement sub-agent produces from Fail 1 below.)**

**Why this works**: stem names *both* a symptom (deadlock under
multi-replica) and the mechanism class (ALTER TABLE concurrency).
Body opens with the trigger (`synchronize: true` under multi-replica),
carries the *quoted error string* (`relation already exists`), the
observable (deploy goes red intermittently), and the fix (pin +
execOnce migrator). Three search paths all converge on this bullet.

## Fail examples (drawn from run-16 misses)

### Fail 1 — author-claim stem, no symptom signal

> **TypeORM `synchronize: false` everywhere** — Auto-sync mutates the
> schema on every container start; with two or more containers
> booting in parallel, two simultaneous `ALTER TABLE` calls can
> corrupt the schema. Pin `synchronize: false` and own DDL in an
> idempotent script (`CREATE TABLE IF NOT EXISTS`, `CREATE INDEX IF
> NOT EXISTS`) fired once per deploy from `run.initCommands`.

**Source**: run-16 apidev README KB.

**Why this fails**: stem is the recipe author's directive
(`synchronize: false`). A porter who hits the symptom would search
for "schema corruption on deploy", "ALTER TABLE deadlock", "relation
already exists", or "Postgres concurrent DDL". None match.

**Refined to**:

> **ALTER TABLE deadlock under multi-container boot** — Leaving the
> ORM `synchronize: true` makes every fresh container race the
> others to create tables/indices on first boot. Postgres rejects
> the loser with `relation already exists` and the deploy goes red
> intermittently. Pin `synchronize: false`, own the schema via a
> `zsc execOnce`-fired migrator (`CREATE TABLE IF NOT EXISTS`,
> `CREATE INDEX IF NOT EXISTS`) so the DDL fires once per deploy
> regardless of replica count.

The reshape names the symptom (deadlock + multi-container boot),
opens with the quoted error string (`relation already exists`),
keeps the mechanism + fix prose intact.

### Fail 2 — directive-as-summary, no observable in body opener

> **Decompose execOnce keys into migrate + seed** — A single combined
> key marks the whole script succeeded even when the seed step
> crashed, leaving a half-migrated state. Use two per-deploy keys
> (`${appVersionId}-migrate` and `${appVersionId}-seed`) so a seed
> failure does not burn the migrate key — the next redeploy re-fires
> only the failing step. The Zerops `init-commands` reference covers
> per-deploy key shape and the in-script-guard pitfall.

**Source**: run-16 apidev README KB.

**Why this fails**: stem is the recipe author's directive ("decompose
execOnce keys"). Body's first sentence carries the observable wrong
state ("half-migrated state"), so this is borderline 8.0 — not a
9.0 because the porter searching "half-migrated state on Zerops" or
"seed crashed but migrate succeeded" doesn't reach the bullet via
the stem.

The citation is good (final sentence names the `init-commands` guide
+ what's in it). Stem reshape is the only lift.

**Refined to**:

> **Half-migrated state when the seeder crashes** — A single combined
> `execOnce` key (`${appVersionId}-migrate-and-seed`) marks the whole
> script succeeded even when the seed step crashed, leaving the
> database with migrations applied but seed data missing. The next
> redeploy sees the key set and skips the entire script — the
> seed never runs. Use two per-deploy keys
> (`${appVersionId}-migrate` and `${appVersionId}-seed`) so a seed
> failure does not burn the migrate key; the next redeploy re-fires
> only the failing step. The `init-commands` guide covers per-deploy
> key shape and the in-script-guard pitfall.

Stem now names the symptom ("half-migrated state") + the trigger
("when the seeder crashes"). Body keeps the mechanism + fix +
citation.

### Fail 3 — symptom-named in stem already (HOLD case for refinement)

> **Internal traffic between Zerops services is plain HTTP** — SSL
> terminates at the L7 balancer; `https://search:7700` between
> services fails the TLS handshake. Wire `MEILI_HOST:
> http://search:${search_port}`. Reaching a sibling via its public
> `${search_zeropsSubdomain}` works but routes the call out and back
> through the public balancer for no reason.

**Source**: run-16 apidev README KB.

**Why this is shown as a HOLD case (not a FAIL)**: stem names the
mechanism (internal traffic = plain HTTP) which is itself a directive,
but body opens with "SSL terminates at the L7 balancer; `https://...`
fails the TLS handshake" — observable failure at sentence one.
Trade-off two-sided (names the public-subdomain alternative + why it
loses).

This bullet would score 8.5+ on Criterion 1. **Refinement HOLDS.**

Show as a HOLD example to teach the refinement sub-agent the
boundary between "directive stem with observable opener" (8.5,
acceptable) vs "directive stem without observable opener" (7.0,
ACT).

### Fail 4 — author-claim stem on workerdev

> **`MaxReconnectAttempts: -1` is mandatory for long-lived consumers**
> — Default NATS clients give up after a handful of reconnects. A
> broker restart during a rolling upgrade then leaves the worker
> disconnected forever, even though the process is still healthy. Set
> the option to `-1` (unlimited) on every long-running subscriber.

**Source**: run-16 workerdev README KB.

**Why this fails**: stem names the *fix* (the option value), not the
*symptom*. Body's first sentence is the rejected alternative's
behavior ("default clients give up"), not an observable wrong state
the porter would search for.

**Refined to**:

> **Worker stays disconnected after a broker restart** — Default NATS
> clients (`nats.connect(...)` with stock options) give up after a
> handful of reconnect attempts. A broker restart during a rolling
> upgrade then leaves the worker silently disconnected — the
> process is still healthy from the supervisor's view but no
> messages flow. Set `MaxReconnectAttempts: -1` (unlimited) on every
> long-running subscriber so the worker keeps trying until the broker
> comes back.

Stem names the symptom ("disconnected after a broker restart"). Body
opens with the cause + observable ("silently disconnected — process
healthy but no messages flow"). Fix preserved.

### Fail 5 — author-claim stem on workerdev (start command)

> **`run.start` must stay foreground** — The platform's runtime
> supervisor restarts the container when the start command exits. A
> worker that backgrounds itself (`&`, `disown`, `nohup`) returns
> control to the shell, the supervisor sees exit code 0, and restarts
> in a tight loop. Run the Nest standalone process directly (`node
> dist/main.js`) so the supervisor's view matches the lifecycle.

**Source**: run-16 workerdev README KB.

**Why this fails**: stem names the *rule* (`run.start` foreground),
not the *symptom*. Body's first sentence is the platform's behavior,
not the observable wrong state.

**Refined to**:

> **Container restarts in a tight loop after a backgrounded `run.start`**
> — The platform's runtime supervisor restarts the container when the
> start command exits. A worker that backgrounds itself (`&`,
> `disown`, `nohup`) returns control to the shell, the supervisor
> sees exit code 0, and restarts the container — repeatedly, in
> a tight loop. Run the Nest standalone process directly (`node
> dist/main.js`) so the supervisor's view matches the lifecycle.

Stem names the observable ("restarts in a tight loop") + the trigger
("after a backgrounded `run.start`"). Body keeps mechanism + fix.

## The heuristic

Walk every KB bullet stem (the text between `**...**`). The stem is
symptom-first or directive-tightly-mapped when it contains at least
one of:

| Signal in stem | Pattern |
|---|---|
| HTTP status code | `\b[1-5]\d{2}\b` (e.g. `403`, `502`) |
| Quoted error string | `` `...` `` or `"..."` matching error syntax (e.g. `relation already exists`, `"directory not found"`) |
| Verb-form failure phrase | `fails`, `crashes`, `corrupts`, `deadlocks`, `silently exits`, `silently stops`, `returns null`, `breaks`, `drops`, `rejects`, `missing`, `hangs`, `times out`, `panics` |
| Observable wrong-state phrase | `empty body`, `wrong header`, `null where X expected`, `404 on X`, `502 on X`, `empty response`, `stale data`, `no rows`, `disconnected`, `restarts in a tight loop` |

If NONE of the above match the stem AND the body's first sentence
does NOT carry an observable error string OR HTTP status, the bullet
is in author-claim shape. Refinement ACTS if the source fact's Why
names the symptom explicitly (so the reshape isn't speculative).

**Backtick-quoted config keys do NOT count as quoted error strings.**
A stem like `**TypeORM \`synchronize: false\` everywhere**` contains
a backtick token, but `synchronize` is the directive, not the
symptom. Disambiguate by asking: "Would a porter type this token into
a search bar after hitting a problem?" Config keys: no. Error
strings: yes.

The 9.0 lift over 8.5 is the addition of a *quoted error string* OR
*HTTP status code* in the stem itself (not just the body). Three
search paths converge: symptom phrase + error string + status code.

## When to HOLD (refinement does not act)

- **Stem already names a symptom** (HTTP code, quoted error,
  failure verb, observable phrase) — current stem is at the 8.5+
  tier. HOLD.
- **Stem is directive-tightly-mapped AND body opens with the
  observable error in the first sentence** — showcase pattern,
  acceptable. HOLD.
- **The source fact's Why doesn't name a symptom** — refinement
  would have to invent one. HOLD AND surface "fact-recording
  teaching gap" notice for run-18 rubric tuning.
- **Reshape would change the bullet's classification** (e.g. moving
  from platform-invariant to intersection) — that's a routing
  decision, not a stem decision. HOLD.
- **Reshape would push the body past the 4-sentence Surface 5 cap**
  — rare, but possible when the symptom phrase requires extra
  context. HOLD; reshape is not a refactor.
- **Stem is a deliberate directive on a single-valued mechanism**
  (e.g. "Subdomain refs already carry `https://`") — there's no
  symptom to name; the directive IS the teaching. HOLD.

The 100%-sure threshold: if you couldn't argue the symptom phrase in
a code review against the recorded fact's Why, you can't add it to
the stem. HOLD.
