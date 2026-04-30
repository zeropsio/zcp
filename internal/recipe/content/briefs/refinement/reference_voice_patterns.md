# Reference: friendly-authority voice patterns

## Why this matters

Surface 7 (`zerops.yaml` block comments) and Surface 3 (tier
`import.yaml` service-block comments) are read by porters who want to
adapt the recipe to their own needs. Engineering-spec voice ("api in
zeropsSetup: prod, 0.5 GB shared CPU, minContainers: 2") is correct
but it doesn't tell the porter what to change or when. Friendly-
authority voice ("Feel free to change this value to your own custom
domain") gives them permission AND points at the adapt path.

Run-16 shipped zero friendly-authority phrasings across all checked
surfaces — that's the dominant Criterion 2 lift for run-17. Both
reference recipes use these phrasings consistently; the patterns are
not invented, they are extracted verbatim.

The criterion does NOT apply to KB (gotchas are imperative — "Feel
free to" weakens the warning), CLAUDE.md, root README, or codebase
intro. Refinement HOLDS on those surfaces.

## Pass examples (drawn from references)

### Pass 1 — declarative fact + named adapt path (jetstream zerops.yaml)

> *"Laravel checks the 'Host' header against this value."*
> *"Feel free to change this value to your own custom domain,"*
> *"after setting up the domain access."*

**Why this works**: states the platform mechanism (Laravel checks the
Host header against APP_URL), names the adapt path (your own custom
domain), and ties to a concrete porter signal (after setting up the
domain access). The phrase "Feel free to change" gives permission;
the trailing condition tells the porter when they're ready to act.

### Pass 2 — invitation tied to environment shift (jetstream zerops.yaml)

> *"Configure this to use real SMTP sinks"*
> *"in true production setups. This default configuration"*
> *"expects 'mailpit' to be deployed along the app."*

**Why this works**: names the default ("mailpit"), names the porter-
adapt path ("real SMTP sinks"), and ties to a concrete porter signal
("true production setups"). "Configure this to" is imperative-as-
permission — the porter knows the recipe expects them to override.

### Pass 3 — replace-with directive (showcase zerops.yaml)

> *"Mail set to log driver — no external SMTP configured."*
> *"Replace with real SMTP credentials for production use."*

**Why this works**: declarative state ("set to log driver"),
explicit signal of incompleteness ("no external SMTP configured"),
and a verbed adapt path ("Replace with real SMTP credentials"). The
trailing trigger ("for production use") tells the porter when to act.
Same pattern as Pass 2, tighter prose.

### Pass 4 — recommendation conditional on porter readiness (jetstream tier-4)

> *"Disabling the subdomain access is recommended,"*
> *"after you set up access through your own domain(s)."*

**Why this works**: names the adapt path ("disable subdomain access")
and the prerequisite trigger ("after you set up access through your
own domain(s)"). Recommendation, not directive — leaves the choice
with the porter. The conditional is load-bearing; without "after you
set up", the porter might disable the subdomain and lose access.

### Pass 5 — service-removal invitation (jetstream tier-3)

> *"Optionally, spin up the single-service email and SMTP"*
> *"testing tool."*
> *"Feel free to remove this service, if you wish to stage-test"*
> *"your app with as-close-as-possible production setup."*

**Why this works**: "Optionally" signals the service is non-load-
bearing; "Feel free to remove" gives explicit permission; "if you
wish to stage-test ... production setup" names the porter's actual
goal as the trigger condition. The porter learns *why* removing
mailpit is reasonable, not just that it's allowed.

## Fail examples (drawn from run-16)

### Fail 1 — field-restatement preamble, no adapt path (tier-4 appdev)

> *"# Svelte frontend in zeropsSetup: prod, 0.5 GB shared CPU, minContainers: 2."*
> *"# Static bundle served from two replicas behind the public subdomain — a"*
> *"# rolling deploy doesn't drop the dashboard while it ships."*

**Why this fails**: the comment opens with a restatement of the
yaml fields (`zeropsSetup: prod`, `0.5 GB shared CPU`, `minContainers:
2`). The body that follows DOES name a mechanism, but the porter
reading top-to-bottom hits five tokens of field echo before the
teaching starts. Zero friendly-authority phrasings; the porter
reading "I want to scale this differently" finds no permission to do
so and no signal of what would change their mind.

**Refined to**:

> *"# Two Svelte replicas behind the public subdomain keep the"*
> *"# dashboard available during rolling deploys — one serves traffic"*
> *"# while the other rebuilds. Feel free to bump minContainers when"*
> *"# your dashboard usage outgrows the two-replica fan-out."*

The reshape opens with the mechanism, names the adapt path
(`minContainers`), and ties to a concrete porter signal (dashboard
usage growth). Field echoes deleted; mechanism + invitation kept.

### Fail 2 — field-restatement preamble, mechanism-first body (tier-4 worker)

> *"# NestJS worker in zeropsSetup: prod, 0.5 GB shared CPU, minContainers: 2."*
> *"# Both replicas join the same NATS queue group — only one of them"*
> *"# processes"*
> *"# each message, so doubling the count actually doubles throughput rather"*
> *"# than duplicating work."*

**Why this fails**: same shape as Fail 1 — leading field echo, then
a mechanism. The mechanism prose is genuinely good (queue group
semantics, throughput vs duplication trade-off explained). One
friendly-authority phrasing would push it to 8.5; without one it
sits at 7.5–8.0.

**Refined to**:

> *"# Both worker replicas join the same NATS queue group — only one"*
> *"# processes each message, so doubling the count doubles throughput"*
> *"# rather than duplicating work. Bump minContainers when your queue"*
> *"# depth backlog grows past one-replica drain rate."*

Field echo dropped; "Bump minContainers when" added with a concrete
porter signal (queue depth backlog).

## The heuristic

Count friendly-authority phrasings across all in-scope comments
(zerops.yaml block comments + tier import.yaml service-block
comments).

**Phrasings to count** (rubric Criterion 2):

1. `Feel free to ...`
2. `Configure this to ...`
3. `Replace ... with ...`
4. `Disabling ... is recommended ...` / `Enabling ... is recommended ...`
5. `Adapt this ...` / `Adjust this ...`
6. `Bump ... if ...` / `Switch ... when ...`
7. `... once you ...` (conditional adapt)

**Named-signal requirement**: each phrasing must be tied to a
concrete porter signal — a numeric threshold (`when traffic exceeds
N`), a configuration state (`once you have a custom domain
configured`), or a named external condition (`for production use`,
`if your queue depth grows`). A phrasing without a signal is hedge
phrasing, not friendly authority.

**Do NOT count**:
- "you might want to consider ..." — hedge, not authority.
- "perhaps this could be ..." — hedge.
- "Feel free to ..." with no trigger — opens the adapt path but
  doesn't tell the porter when to walk it.

**Where it applies**:
- `zerops.yaml` comments (Surface 7) — primary site.
- Tier `import.yaml` comments (Surface 3) — secondary site.
- IG prose (Surface 4) — sparingly, where a config has multiple
  valid shapes.

**Where it does NOT apply**:
- KB bullets (Surface 5) — gotchas are imperative.
- CLAUDE.md (Surface 6) — operational guide.
- Codebase intro / Root README — factual catalogs.

## When to HOLD (refinement does not act)

- The yaml field has only one valid value (e.g. `httpSupport: true`
  on a public-facing port) — there's no adapt path. HOLD.
- The comment is on a Surface 5 / Surface 6 / Surface 1 fragment —
  voice criterion is `n/a`. HOLD.
- The comment ALREADY carries one or more friendly-authority
  phrasings with named signals — surface is at the 8.5 anchor.
  HOLD.
- No porter-adapt path is namable from the recorded facts or the
  yaml field semantics — adding "Feel free to ..." with nothing to
  point at is hedge phrasing, not voice. HOLD; surface "fact-
  recording teaching gap" notice.
- The comment is in a tier-2 or tier-3 (Local / Stage) where the
  shape is intentionally pedagogical and the porter is expected to
  use the recipe as-is until they're ready to ship a higher tier.
  Voice-insertion is welcome but the bar is lower; HOLD if the
  reshape would feel forced.

The 100%-sure threshold: if you can't name a concrete signal the
porter would act on, you can't add the friendly-authority phrasing.
HOLD and surface the gap.
