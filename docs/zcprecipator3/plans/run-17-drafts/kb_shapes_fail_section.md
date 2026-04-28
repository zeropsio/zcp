# Drop-in: FAIL section for `reference_kb_shapes.md`

This file is the partial content for the FAIL examples section of
`internal/recipe/content/briefs/refinement/reference_kb_shapes.md`.
The fresh instance integrates this with their PASS section
(extracted verbatim from laravel-jetstream + laravel-showcase) and
the surrounding atom shape (Why/Pass/Fail/Heuristic/Hold sections).

Each FAIL example is drawn verbatim from
`docs/zcprecipator3/runs/16/<codebase>/README.md`. Cross-checked
2026-04-28; integrate without reword.

The "Refined to" suggestion under each FAIL is a reference reshape
the refinement sub-agent should produce — it's a target, not a
bound. Implementer may tighten the suggested phrasing during
distillation.

---

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

---

## Notes for the fresh instance integrating these

1. The Refined-to suggestions above are reshape *targets* the
   refinement sub-agent should aim for. The atom (`reference_kb_shapes.md`)
   should present them as the canonical answers the sub-agent
   produces; do not present them as drafts.

2. The Fail 3 case (internal traffic = plain HTTP) is intentionally
   NOT a refinement candidate. It belongs in the "When to HOLD"
   section of the atom, not the FAIL examples. Keep the example
   verbatim; reframe the framing prose around it.

3. After integrating, cross-check each FAIL quote against
   `docs/zcprecipator3/runs/16/<codebase>/README.md` byte-for-byte
   to ensure verbatim. The verbatim check is the load-bearing
   verification work for this atom.

4. The PASS section pairs each fail-shape with a reference recipe
   bullet at the 8.5 anchor (showcase apidev / showcase workerdev /
   jetstream apps repo) and at the 9.0 anchor (a hand-crafted
   "above-golden" reshape — the implementer can adapt the Refined-to
   suggestions above into 9.0 anchors if appropriate).
