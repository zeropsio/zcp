# Reference: tier import.yaml service-block comment shapes

## Why this matters

Surface 3 (tier `import.yaml` service-block comments) explains every
decision — service presence, scale, mode — at the deployed tier.
Both reference recipes settle within the spec's 3–5-lines-per-block
cap, but they choose two different shapes:

- **jetstream tier-4** — mechanism-first, tight. Service blocks open
  with what the service does and why this tier needs it; field values
  are not restated in prose because the yaml directly below carries
  them.
- **showcase tier-4** — service-name-first, looser. Blocks open with
  a short noun-phrase ("Production queue worker", "Small production")
  followed by mechanism prose. Field-value tokens occasionally appear
  inline but the comment leads with the choice, not the field.

Both shapes are reference-acceptable (they land at the 8.5 anchor on
Criterion 2). This atom teaches the refinement sub-agent when each
shape lands at 8.5, when each could be reshaped to 9.0, and what the
7.0 anti-pattern (run-16 field-restatement preamble) looks like
underneath.

The refinement sub-agent does NOT mechanically rewrite jetstream-
shape into showcase-shape or vice versa. They are equally valid;
the action is to pull a 7.0 anchor up to either of them, not to
homogenize.

## Pass examples (drawn from references)

### Pass 1 — jetstream tier-4 mechanism-first (app service)

> *"# Deploy the Laravel Jetstream app, running on"*
> *"# PHP 8.4 with Nginx (FastCGI) as webserver."*
> *"- hostname: app"*
> *"  type: php-nginx@8.4"*

**Why this works**: comment names what's deployed (Laravel Jetstream
app) and the runtime stack (PHP 8.4 + Nginx FastCGI). The yaml below
carries the runtime version — comment doesn't restate it. The porter
reads the comment and understands what's running BEFORE seeing the
field values.

### Pass 2 — jetstream tier-4 with friendly-authority adapt path

> *"    # Disabling the subdomain access is recommended,"*
> *"    # after you set up access through your own domain(s)."*
> *"    enableSubdomainAccess: true"*

**Why this works**: tight 2-line comment above a single boolean
field. Names the recommended adapt path (disable subdomain) and the
prerequisite trigger (after own domain configured). One friendly-
authority phrasing tied to a concrete porter signal. This pattern
lands at 8.5 on Criterion 2 by itself.

### Pass 3 — jetstream tier-4 mechanism + tier rationale (db service)

> *"  # Set higher priority for databases and storages,"*
> *"  # because the app depends on those services."*
> *""*
> *"  # Deploy the PostgreSQL database in highly-available mode,"*
> *"  # used by the Laravel app to store data. Automatic,"*
> *"  # encrypted backups are enabled by default too."*
> *"  - hostname: db"*
> *"    type: postgresql@16"*
> *"    mode: HA"*
> *"    priority: 10"*

**Why this works**: meta-comment explains the `priority: 10` choice
across multiple services (databases + storages); per-service comment
names the mode (`HA`) and one tier-specific feature (encrypted
backups). Field-value restatement is absent — `mode: HA` appears
once, in the yaml. Comment teaches the choice, never narrates the
field.

### Pass 4 — showcase tier-4 mechanism-with-light-field-token

> *"  # Small production — minContainers: 2 guarantees two app containers at all"*
> *"  # times, enabling rolling deploys with zero downtime (one container serves"*
> *"  # traffic while the other rebuilds). Zerops autoscales RAM within"*
> *"  # verticalAutoscaling bounds to absorb traffic spikes without manual"*
> *"  # intervention."*

**Why this works**: comment opens with "Small production —" (tier
identity, not field name), names `minContainers: 2` inline as part of
the mechanism explanation (not as preamble), and explains *why* the
field carries that value (rolling deploys with zero downtime). The
inline `minContainers: 2` is a teaching anchor — the porter reading
the comment can match the explanation to the literal field below.
This is showcase-shape: looser than jetstream, but the field token
serves the mechanism prose, not the other way around.

### Pass 5 — showcase tier-4 worker block

> *"  # Production queue worker — processes background jobs with --tries=3 retry"*
> *"  # policy. Single container sufficient for moderate job volumes; scale"*
> *"  # minContainers if queue depth grows."*

**Why this works**: 3-line tight comment. Names the worker's job
("processes background jobs"), the chosen retry policy
("--tries=3"), the current scale rationale ("Single container
sufficient for moderate job volumes"), and a porter-adapt path
("scale minContainers if queue depth grows"). One friendly-authority
phrasing ("scale ... if") tied to a concrete signal (queue depth
growth). This is the showcase shape at its cleanest.

## Fail examples (drawn from run-16)

### Fail 1 — field-restatement preamble (run-16 tier-4 appdev)

> *"  # Svelte frontend in zeropsSetup: prod, 0.5 GB shared CPU, minContainers: 2."*
> *"  # Static bundle served from two replicas behind the public subdomain — a"*
> *"  # rolling deploy doesn't drop the dashboard while it ships."*

**Why this fails**: opens with three field-value tokens
(`zeropsSetup: prod`, `0.5 GB shared CPU`, `minContainers: 2`)
before any mechanism. The body that follows IS mechanism-driven, but
the porter reading top-to-bottom sees field echo first. The
showcase pattern ("Small production — minContainers: 2 guarantees
...") names ONE field token and uses it as the teaching anchor; the
run-16 pattern names THREE field tokens as a literal restatement of
the yaml.

**Refined to**:

> *"  # Two Svelte replicas behind the public subdomain keep the dashboard"*
> *"  # available during rolling deploys — one serves traffic while the other"*
> *"  # rebuilds. Bump minContainers when dashboard usage outgrows the"*
> *"  # two-replica fan-out."*

Field echo dropped. The `minContainers: 2` claim is now embedded in
the mechanism prose ("Two Svelte replicas") rather than restated as
a preamble. Friendly-authority "Bump ... when ..." added with a
named porter signal.

### Fail 2 — field-restatement preamble (run-16 tier-4 worker)

> *"  # NestJS worker in zeropsSetup: prod, 0.5 GB shared CPU, minContainers: 2."*
> *"  # Both replicas join the same NATS queue group — only one of them"*
> *"  # processes"*
> *"  # each message, so doubling the count actually doubles throughput rather"*
> *"  # than duplicating work."*

**Why this fails**: same field-restatement preamble shape as Fail 1.
The mechanism prose afterward is genuinely good — explains the queue
group + throughput-not-duplication trade-off — but it sits behind the
preamble. The line break in "processes / each message" is also
awkward; that's a render artifact, not a content issue, but it
compounds the readability cost of the preamble.

**Refined to**:

> *"  # Both worker replicas join the same NATS queue group — only one"*
> *"  # processes each message, so doubling the count doubles throughput"*
> *"  # rather than duplicating work. Bump minContainers when queue depth"*
> *"  # backlog grows past one-replica drain rate."*

Preamble removed; mechanism leads. Adapter line break fixed. One
friendly-authority phrasing added.

## The heuristic

A reshape from 7.0 (field-restatement) to 8.5 (mechanism-first or
showcase-shape) is unambiguous when ALL of:

1. The opening line restates two or more yaml field values verbatim
   (`zeropsSetup: prod`, `0.5 GB shared CPU`, `minContainers: 2`)
   without using them as a teaching anchor for adjacent mechanism
   prose.
2. A mechanism-first version is shorter or no longer than the
   original AND at least as informative — the rewrite trades the
   preamble for the mechanism, doesn't add new claims.
3. The reshape preserves every load-bearing claim: the choice of
   mode, the choice of scale, any porter-adapt path already present.

A reshape from 8.5 (jetstream or showcase shape, no friendly-
authority) to 9.0 is unambiguous when:

1. A porter-adapt path is namable from the recorded facts or the
   yaml field semantics (typically `minContainers`, `mode`,
   `verticalAutoscaling.maxRam`, `enableSubdomainAccess`, named env
   vars).
2. The trigger condition is concrete (a numeric threshold, a
   porter-side state change like "custom domain configured", or a
   named external condition like "production use").
3. Adding the friendly-authority phrasing keeps the comment within
   the 5-lines-per-block soft cap.

## When to HOLD (refinement does not act)

- The comment ALREADY names the mechanism in addition to the field
  restatement (e.g. "minContainers: 2 — two replicas behind a queue
  group keep deploys zero-downtime"). Field restatement is redundant
  but the mechanism is present; reshape would be cosmetic. HOLD.
- The comment is short (≤ 2 lines) and the field restatement IS the
  mechanism explanation — common in tier-3 (Stage) where the config
  is the teaching. HOLD.
- The reshape would push the comment over the 5-lines-per-block soft
  cap. HOLD; surface "voice-insertion would bloat block" notice.
- The yaml field is single-valued at this tier (e.g. `mode: HA` on
  a tier-5 service that's HA-only) — no adapt path namable. Voice
  insertion is hedge phrasing without a signal. HOLD.
- The comment is in tier-0 (AI Agent) or tier-2 (Local) where the
  shape is intentionally minimal and the porter is expected to
  graduate before wanting adapt-paths. Reshape is welcome but the
  bar is lower; HOLD if the rewrite feels forced.

The 100%-sure threshold: if the reshape's mechanism prose isn't
clearly equivalent to the original's claims, HOLD. Refinement is
not a rewrite of the recipe author's choices.
