# Reference: cite-by-name patterns

## Why this matters

The engine threads a `citations[]` field on every fragment manifest,
but porters reading the published markdown never see it — the
manifest is internal scaffolding. If a KB bullet or IG paragraph
covers a topic on the Citation Map (env-var-model, init-commands,
rolling-deploys, object-storage, http-support, etc.) but the body
doesn't *name* the relevant Zerops guide in prose, the porter reaches
the recipe author's framing without knowing the platform's deeper
resource exists.

Reference recipes use URL links to docs.zerops.io ("[zsc scale
command](https://docs.zerops.io/references/zsc#scale)") — that's a
weaker form of cite-by-name; it shows the link but doesn't name the
guide id in the prose. The 8.5 anchor for Criterion 3 is naming the
guide id directly ("The `init-commands` guide covers ..."). The 9.0
anchor adds the *application-specific corollary* — the line between
the guide's general teaching and this recipe's specific application.

Run-16 has a few cite-by-name PASS examples (apidev `Decompose
execOnce keys`, workerdev `Auto-sync corrupts schema`) and a long
list of FAIL examples (KB bullets touching Citation Map topics with
zero inline cite).

## Pass examples (drawn from references and run-16)

### Pass 1 — cite-by-name + what's in the guide (run-16 apidev KB)

> *"**Decompose execOnce keys into migrate + seed** — A single combined"*
> *"key marks the whole script succeeded even when the seed step"*
> *"crashed, leaving a half-migrated state. Use two per-deploy keys"*
> *"(\`${appVersionId}-migrate\` and \`${appVersionId}-seed\`) so a seed"*
> *"failure does not burn the migrate key — the next redeploy re-fires"*
> *"only the failing step. The Zerops `init-commands` reference covers"*
> *"per-deploy key shape and the in-script-guard pitfall."*

**Why this works**: final sentence names the guide (`init-commands`)
AND tells the porter what's in the guide (per-deploy key shape +
in-script-guard pitfall). The porter who reads the bullet and wants
to dig deeper knows where to go AND what to expect there. This is
the canonical 8.5 anchor for Criterion 3.

### Pass 2 — cite-by-name as parenthetical reference (run-16 workerdev KB)

> *"**Auto-sync corrupts schema under multi-container boot** — Leaving"*
> *"the ORM `synchronize: true` makes every fresh container race the"*
> *"others to create tables/indices on first boot. The losers throw"*
> *"`relation already exists` at random and the deploy goes red"*
> *"intermittently. Fix: hard-code `synchronize: false`, own the schema"*
> *"via the `zsc execOnce`-fired migrator (see the `init-commands`"*
> *"guide for \`${appVersionId}\` semantics)."*

**Why this works**: parenthetical "see the `init-commands` guide for
\`${appVersionId}\` semantics" names the guide AND the specific
mechanism (`${appVersionId}` semantics) the porter would learn there.
The cite is integrated into the fix sentence, not stamped at the end.
Same 8.5 tier as Pass 1, different prose shape.

### Pass 3 — cite-by-name pointing at recovery pattern (run-16 workerdev KB)

> *"**Authorization Violation on connect** — Embedding NATS credentials"*
> *"in the URL (`nats://user:pass@host:port`) makes most clients"*
> *"double-auth: they parse the URL AND attempt SASL CONNECT with the"*
> *"same values. The server rejects the first frame and the worker"*
> *"crashes at boot. Recovery: pass `user` and `pass` as `connect()`"*
> *"options against a credential-free `host:port` string (Pattern A"*
> *"from the NATS knowledge guide)."*

**Why this works**: parenthetical "(Pattern A from the NATS knowledge
guide)" names the guide AND the specific pattern within it. Porter
who hits this trap knows there's a named pattern, not just one-off
recipe advice. Tight integration — cite in 7 tokens.

### Pass 4 — URL-link variant from references (jetstream zerops.yaml)

> *"# Read more about it here: https://docs.zerops.io/php/how-to/customize-web-server#customize-nginx-configuration"*

**Why this works (8.0 — between 7.0 and 8.5)**: links to the docs
page AND the specific anchor. Doesn't name the guide ID in prose,
which is the 8.5 lift, but it does point the porter at a concrete
resource. Reference-acceptable on Surface 7 (yaml comments) where
guide-id naming feels stamped — URL-as-cite is the showcase pattern.

### 9.0 anchor — cite-by-name + application-specific corollary

> *"**Half-migrated state when the seeder crashes** — A single combined"*
> *"`execOnce` key marks the whole script succeeded even when the seed"*
> *"step crashed, leaving the database with migrations applied but seed"*
> *"data missing. Use two per-deploy keys (\`${appVersionId}-migrate\`"*
> *"and \`${appVersionId}-seed\`) so a seed failure does not burn the"*
> *"migrate key. The `init-commands` guide covers per-deploy key shape"*
> *"and the in-script-guard pitfall; the application-specific corollary"*
> *"here is that decomposing the keys across the migrator vs the seeder"*
> *"lets you re-fire the seed independently when its dataset changes —"*
> *"without re-applying migrations that have already settled."*

**(Reshape target; this is the canonical 9.0 anchor the refinement
sub-agent produces by extending Pass 1's body with the corollary.)**

**Why this works**: cite names the guide AND draws the line between
the guide's general teaching (per-deploy key shape + in-script-guard
pitfall) and this recipe's specific decomposition (migrator-vs-seeder
keys for independent seed re-fire). The corollary is load-bearing —
the porter learns *why this recipe* makes the choice the guide
describes.

## Fail examples (drawn from run-16)

### Fail 1 — Citation Map topic, zero inline cite (run-16 apidev KB)

> *"**TypeORM `synchronize: false` everywhere** — Auto-sync mutates the"*
> *"schema on every container start; with two or more containers booting"*
> *"in parallel, two simultaneous `ALTER TABLE` calls can corrupt the"*
> *"schema. Pin `synchronize: false` and own DDL in an idempotent script"*
> *"(`CREATE TABLE IF NOT EXISTS`, `CREATE INDEX IF NOT EXISTS`) fired"*
> *"once per deploy from `run.initCommands`."*

**Why this fails**: body covers `init-commands` territory
(`run.initCommands`, per-deploy schema setup). The Citation Map names
this topic. No inline cite. A porter reaching this bullet doesn't
know the `init-commands` guide exists, doesn't see the
`${appVersionId}` per-deploy-key idiom, doesn't learn what
`--retryUntilSuccessful` does.

**Refined to** (combines Action 1 stem reshape with Action 7 cite):

> *"**ALTER TABLE deadlock under multi-container boot** — Leaving the"*
> *"ORM `synchronize: true` makes every fresh container race the others"*
> *"to create tables/indices on first boot. Postgres rejects the loser"*
> *"with `relation already exists` and the deploy goes red intermittently."*
> *"Pin `synchronize: false`, own the schema via a `zsc execOnce`-fired"*
> *"migrator. The `init-commands` guide covers per-deploy key shape and"*
> *"the in-script-guard pitfall — the application-specific corollary"*
> *"here is that the migrator's idempotency primitive (`CREATE ... IF"*
> *"NOT EXISTS`) makes the deploy converge regardless of replica count."*

The cite names the guide AND the corollary distinguishes the recipe's
specific application from the guide's general teaching. 9.0 anchor
shape.

### Fail 2 — Citation Map topic, zero inline cite (run-16 apidev KB)

> *"**Internal traffic between Zerops services is plain HTTP** — SSL"*
> *"terminates at the L7 balancer; `https://search:7700` between"*
> *"services fails the TLS handshake. Wire `MEILI_HOST:"*
> *"http://search:${search_port}`. Reaching a sibling via its public"*
> *"`${search_zeropsSubdomain}` works but routes the call out and back"*
> *"through the public balancer for no reason."*

**Why this fails**: body covers `http-support` / `l7-balancer` and
`env-var-model` (cross-service refs) territory. Citation Map names
both. No inline cite. The bullet is otherwise excellent (symptom-
first stem, two-sided trade-off, mechanism prose) — citation is the
only criterion below 8.5.

**Refined to**:

> *"**Internal traffic between Zerops services is plain HTTP** — SSL"*
> *"terminates at the L7 balancer; `https://search:7700` between"*
> *"services fails the TLS handshake. Wire `MEILI_HOST:"*
> *"http://search:${search_port}`. Reaching a sibling via its public"*
> *"`${search_zeropsSubdomain}` works but routes the call out and back"*
> *"through the public balancer for no reason. The `http-support` guide"*
> *"covers the L7-to-VXLAN routing model that drives this asymmetry."*

Final sentence adds cite-by-name with what's in the guide (L7-to-VXLAN
routing model). One-sentence addition; bullet stays within body cap.

### Fail 3 — Citation Map topic, zero inline cite (run-16 apidev IG)

> *"### 5. Talk to Zerops object storage with `forcePathStyle` + `apiUrl`"*
>
> *"Zerops object storage is MinIO-backed, so the AWS S3 SDK has to be"*
> *"configured for path-style addressing (`https://endpoint/bucket/key`)"*
> *"— virtual-hosted-style requests fail. Region must be set to"*
> *"`us-east-1` even though MinIO ignores the value, because every S3"*
> *"client refuses to sign without one."*

**Why this fails**: IG body covers `object-storage` territory.
Citation Map names this topic. No inline cite. IG H3s often skip
inline cites because the H3 itself names the platform mechanism, but
Criterion 3 applies — the porter reading the H3 doesn't learn about
the broader `object-storage` guide.

**Refined to**: append a final sentence — `"The \`object-storage\`
guide covers MinIO-backed addressing semantics and the path-style
requirement."` — then evaluate whether the recipe's specific
\`storage_apiUrl\` vs \`storage_apiHost\` distinction warrants an
application-specific corollary.

## The heuristic

For every KB bullet (Surface 5) AND every IG paragraph (Surface 4):

1. Build the topic set: extract noun phrases from the stem + body
   opening sentence.
2. Cross-reference against the Citation Map topics:
   - `env-var-model` — cross-service env vars, self-shadow, aliasing
   - `init-commands` — `zsc execOnce`, `${appVersionId}`, init commands
   - `rolling-deploys` / `minContainers-semantics` — SIGTERM, HA
     replicas, two-axis `minContainers`
   - `object-storage` — MinIO, `forcePathStyle`, `storage_*` vars
   - `http-support` / `l7-balancer` — bind 0.0.0.0, TLS termination,
     `trust proxy`
   - `deploy-files` / `static-runtime` — `./dist/~`, `base: static`
   - `readiness-health-checks` — readiness/health gates
3. For each (bullet, citation-map-topic) match, check whether the
   body names the guide id in prose.

A reshape from no-cite to cite-by-name (Action 7) is unambiguous
when ALL of:

1. The bullet's body covers a topic on the Citation Map.
2. The body does not name the guide id in any sentence.
3. The recipe is not the SOLE source of teaching for this topic
   (i.e. the guide actually exists and is reachable via
   `zerops_knowledge`).
4. The reshape adds ≤ 1 sentence — body stays within cap.

A reshape from cite-by-name (8.5) to cite + application-specific
corollary (9.0) is unambiguous when:

1. The recorded facts make the corollary derivable — the recipe's
   specific application is genuinely distinct from the guide's
   general teaching.
2. The corollary is a single observation: "the guide covers X; the
   corollary here is Y." Not a multi-clause expansion.
3. Adding the corollary keeps the bullet within the body cap.

**Cite-by-name pattern variants** (all reference-acceptable):

- `The X guide covers Y.`
- `(see the X guide for Y)` — parenthetical
- `(Pattern A from the X guide)` — named pattern within guide
- `The Zerops X reference covers Y.` — equivalent

## When to HOLD (refinement does not act)

- **Stem doesn't actually match a Citation Map topic** — false-
  positive heuristic match. HOLD.
- **The application-specific corollary isn't derivable from facts** —
  forcing a generic "see X" cite reads like a stamped tag. HOLD;
  surface "citation match without corollary" notice for run-18
  rubric tuning.
- **The cite is already present in a sibling KB bullet** that's the
  authoritative source for this topic — duplicating doesn't add
  signal. HOLD.
- **The bullet is on a Surface where citation is `n/a`** — root
  README, codebase intro, yaml comments (URL-link variant is
  showcase pattern), CLAUDE.md. HOLD.
- **The reshape would push body past the 4-sentence Surface 5 cap**
  — citation insertion is not worth bloating the bullet. HOLD;
  surface "citation needs separate IG note" notice.
- **The Citation Map topic is single-touch in the body** — one
  passing reference, no load-bearing teaching. Adding the cite would
  promote an incidental mention to a load-bearing claim. HOLD.

The edit threshold: if you can't write the corollary clause
without inventing a recipe-side claim that isn't in the recorded
facts, HOLD. Citation refinement extends what's already there; it
doesn't author new platform claims.
