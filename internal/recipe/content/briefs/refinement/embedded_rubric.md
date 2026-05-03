# Content quality rubric

The rubric grades each of the seven content surfaces on five
criteria. Each criterion has three hand-scored anchor examples
(7.0 = run-15 floor, 8.5 = reference floor, 9.0 = above golden) and
explicit "how to score" signals that let two graders converge to the
same number on the same artifact.

The rubric is the load-bearing authority for run-17 quality. The
refinement sub-agent at phase 8 reads it, scores every surface, and
acts on criteria below 8.5 only when the fix is unambiguous from the
reference distillation atoms. Post-dogfood ANALYSIS.md grades against
the same rubric so run-to-run lift is measurable.

This doc is single-source. The runtime brief carries
`internal/recipe/content/briefs/refinement/embedded_rubric.md`
which is byte-identical to this file (synced via `go:generate`;
drift caught by `TestEmbeddedRubric_MatchesSpec`).

---

## Criterion 1 — Stem shape (KB Surface 5; IG Surface 4 H3 headings)

**Why this matters**: KB bullets and IG H3 headings exist for porters
who hit a symptom and search for it. An author-claim stem (the
recipe author's directive) is unsearchable — the porter doesn't know
to search for the recipe's prescription. A symptom-first stem (or
directive tightly mapped to an observable failure) is the only shape
that works at scale.

### 7.0 anchor — author-claim, unsearchable

> **TypeORM `synchronize: false` everywhere** — Auto-sync mutates the
> schema on every container start; with two or more containers booting
> in parallel, two simultaneous `ALTER TABLE` calls can corrupt the
> schema. Pin `synchronize: false` and own DDL in an idempotent script…

The body is fine. The stem names the *recipe author's directive*
(`synchronize: false`) instead of the *thing porters do wrong*
(leaving auto-sync on under multi-replica). A porter searching
"schema corruption on deploy", "ALTER TABLE deadlock", "relation
already exists", or "two containers boot at once" never reaches
this bullet.

### 8.5 anchor — symptom-first OR directive-tightly-mapped

> **No `.env` file** — Zerops injects environment variables as OS env
> vars. Creating a `.env` file with empty values shadows the OS vars,
> causing `env()` to return null for every key…

— [laravel-showcase apidev README KB]

Stem names the *thing porters do wrong* + the body opens with the
*observable wrong state* (`env()` returns null). The porter searching
"env() returns null on Zerops" finds this in one search.

> **Cache commands in `initCommands`, not `buildCommands`** —
> config:cache bakes absolute paths… The build container runs at
> `/build/source/` while the runtime serves from `/var/www/`.
> Caching during build produces paths like
> `/build/source/storage/...` that crash at runtime with "directory
> not found."

— [laravel-showcase apidev README KB]

Stem is the directive, but body opens with the observable error
("directory not found"). The porter searching for that error finds
this. Acceptable directive-mapped shape because the failure mode is
named immediately.

### 9.0 anchor — symptom-first with quoted error string OR HTTP code OR observable wrong-state

> **ALTER TABLE deadlock under multi-container boot** — Leaving the
> ORM `synchronize: true` makes every fresh container race the others
> to create tables/indices on first boot. Postgres rejects the loser
> with `relation already exists` and the deploy goes red intermittently.
> Pin `synchronize: false`, own the schema via a `zsc execOnce`-fired
> migrator, and the failure mode disappears regardless of replica
> count.

Stem names *both* a symptom (deadlock under multi-replica) and the
mechanism class (ALTER TABLE concurrency). Body opens with the
quoted error string (`relation already exists`). Three search paths
all converge on this bullet: the porter searching for the deadlock,
the porter searching for the error string, the porter searching for
the multi-container failure shape.

### How to score

Walk every KB bullet stem (the text between `**...**`):

| Signal in stem | Score impact |
|---|---|
| HTTP status code (`\b[1-5]\d{2}\b`) | +1.5 |
| Quoted error string (`"..."` or `` `...` `` matching error syntax) | +1.5 |
| Verb-form failure phrase (fails, crashes, corrupts, deadlocks, silently exits, returns null, breaks, drops, rejects, hangs, times out, panics, missing) | +1.0 |
| Observable wrong-state phrase (empty body, wrong header, null where X expected, 404 on X, no rows, undefined) | +1.0 |
| Body opens with quoted error string OR observable wrong state | +0.5 |
| Stem is recipe author directive only (no symptom signal) AND body's first sentence does NOT carry observable error | -1.5 |

Aggregate per-bullet score: clamp to [7.0, 9.0]; surface score is the
mean across all bullets.

Apply the same scoring to IG H3 headings (Surface 4) — fewer signals
typically present (IG H3s often name the platform mechanism rather
than the symptom), so the 8.5 anchor for IG is "names one platform-
forced change unambiguously" and the 9.0 anchor is "names one change
+ body opens with what fails without it."

### Examples that should NOT score 9.0 despite signals

A stem that contains a backtick-quoted *config key* (`synchronize`,
`buildCommands`) but no actual error string is NOT a 9.0 — the
backtick token is the directive, not the symptom. Disambiguate by
asking: "Would a porter type this token into a search bar after
hitting a problem?" Config keys: no. Error strings: yes.

---

## Criterion 2 — Voice (Surface 7 zerops.yaml comments; Surface 3 tier import.yaml comments)

**Why this matters**: zerops.yaml comments and tier import.yaml
comments are read by porters who want to *adapt the recipe to their
needs*. Engineering-spec voice ("api in zeropsSetup: prod, 0.5 GB
shared CPU, minContainers: 2") is correct but it doesn't tell the
porter what to change. Friendly-authority voice ("Feel free to change
this value to your own custom domain") gives them permission AND
points at the adapt path.

The voice criterion does NOT apply to KB (gotchas are imperative —
"Feel free to" weakens the warning), CLAUDE.md (operational guide,
declarative), or root README (factual catalog). Score those criteria
as `n/a`.

### 7.0 anchor — engineering-spec, no adapt path

```yaml
# api in zeropsSetup: prod, 0.5 GB shared CPU, minContainers: 2.
- hostname: api
  type: nodejs@22
  zeropsSetup: prod
  minContainers: 2
```

— [run-16 nestjs-showcase tier-4 import.yaml]

Comment restates the field values. A porter reading this learns
nothing they couldn't read from the yaml itself. No adapt path
named.

### 8.5 anchor — 1–2 friendly-authority phrasings, mechanism + invitation

```yaml
# Two NestJS containers behind a queue group keep the deploy
# zero-downtime — the balancer fans requests across both replicas.
# Feel free to bump minContainers to 3 if your traffic spikes need
# a deeper buffer.
- hostname: api
  minContainers: 2
```

Comment names the mechanism (queue group + balancer fan-out) AND
invites the adapt path (bump to 3 for deeper buffer). One
friendly-authority phrase ("Feel free to bump"); one named
adaptation; concrete numeric trigger.

### 9.0 anchor — ≥3 friendly-authority phrasings, each tied to a real porter-adapt path

```yaml
# Two NestJS containers behind a queue group keep the deploy
# zero-downtime — the balancer fans requests across both replicas;
# minFreeRamGB: 0.25 keeps the autoscaler honest about adding a
# third under load. Feel free to bump minContainers to 3 if your
# baseline traffic warrants it; switch verticalAutoscaling.maxRam
# upward when steady-state usage approaches the current ceiling.
# Subdomain access is on by default — disable it once you have a
# custom domain configured.
- hostname: api
  minContainers: 2
```

Comment names the mechanism, three distinct adapt paths
(minContainers, maxRam, subdomain access), and ties each to a
concrete porter signal (baseline traffic, steady-state usage,
custom domain configured). Three friendly-authority phrasings, each
load-bearing.

### How to score

Count friendly-authority phrasings across all in-scope comments
(zerops.yaml block comments + tier import.yaml service-block
comments).

Phrasings to count:
- "Feel free to ..."
- "Configure this to ..."
- "Replace ... with ..."
- "Disabling ... is recommended ..." / "Enabling ... is recommended ..."
- "Adapt this ..." / "Adjust this ..."
- "Bump ... if ..." / "Switch ... when ..."
- "... once you ..." (conditional adapt)

For each phrasing, also assert:
- Names a concrete porter signal (numeric threshold, configuration
  state, or named external condition like "custom domain
  configured") — without a signal, it's hedge phrasing, not
  friendly authority. Do NOT count "you might want to consider ..."
  or "perhaps this could be ...".

| Phrasings per surface | Score |
|---|---|
| 0 | 7.0 |
| 1–2 | 8.0 |
| 3+ with named signals | 8.5 |
| ≥3 with named signals AND each tied to a real adapt path the porter would actually take | 9.0 |

Surfaces where voice is inapplicable (KB, CLAUDE.md, root README,
codebase intro) are scored `n/a` and excluded from aggregate.

---

## Criterion 3 — Citation prose-level (KB Surface 5; IG Surface 4 prose)

**Why this matters**: the engine's `citations[]` field on a fragment
manifest is invisible to the published reader. Porters read the
markdown. If a KB bullet covers a topic on the Citation Map but
doesn't *name* the relevant Zerops guide in the prose, the porter
doesn't know there's a deeper resource to consult.

### 7.0 anchor — zero inline guide refs

> **Decompose execOnce keys into migrate + seed** — A single combined
> key marks the whole script succeeded even when the seed step
> crashed, leaving a half-migrated state. Use two per-deploy keys
> (`${appVersionId}-migrate` and `${appVersionId}-seed`) so a seed
> failure does not burn the migrate key.

Topic ("init-commands per-deploy key shape") is on the Citation Map.
Body never names the `init-commands` guide. Porter doesn't know there's
a guide.

### 8.5 anchor — inline cite present, cite-by-name pattern

> **Decompose execOnce keys into migrate + seed** — A single combined
> key marks the whole script succeeded even when the seed step
> crashed, leaving a half-migrated state. Use two per-deploy keys
> (`${appVersionId}-migrate` and `${appVersionId}-seed`) so a seed
> failure does not burn the migrate key — the next redeploy re-fires
> only the failing step. The Zerops `init-commands` reference covers
> per-deploy key shape and the in-script-guard pitfall.

Final sentence names the guide (`init-commands`) and tells the porter
what's in it (per-deploy key shape + in-script-guard pitfall). Cite
is natural prose, not a stamped tag.

### 9.0 anchor — every Citation Map topic in body has a guide name + the cite tells the porter the *application-specific corollary*

> **Decompose execOnce keys into migrate + seed** — A single combined
> key marks the whole script succeeded even when the seed step
> crashed, leaving a half-migrated state. Use two per-deploy keys
> (`${appVersionId}-migrate` and `${appVersionId}-seed`) so a seed
> failure does not burn the migrate key. The `init-commands` guide
> covers per-deploy key shape and the in-script-guard pitfall; the
> application-specific corollary here is that decomposing the keys
> across the migrator vs the seeder lets you re-fire the seed
> independently when its dataset changes — without re-applying
> migrations that have already settled.

Cite names the guide AND draws the line between the guide's general
teaching and this recipe's specific decomposition pattern. Cite-by-
name AND application-specific corollary AND the corollary is
load-bearing (the porter learns *why this recipe* makes the choice
the guide describes).

### How to score

Build the topic set: every KB bullet's stem + body opening sentence
gets cross-referenced against `CitationMap` (defined in
[`internal/recipe/citations.go`](../internal/recipe/citations.go) or
equivalent). For every (bullet, citation-map-topic) match, check
whether the bullet's body names the guide id.

| Topic-on-map matches with inline cite | Score |
|---|---|
| 0% | 7.0 |
| 1–49% | 7.5 |
| 50–99% | 8.5 |
| 100% AND ≥30% of cites carry the application-specific corollary phrasing | 9.0 |

The "application-specific corollary" pattern: *"The X guide covers Y;
the application-specific corollary is …"* (verbatim phrasing varies;
the structure must be: name guide → name guide's general topic →
distinguish recipe's specific application). Heuristic match:
sentence containing the guide id AND containing one of "corollary",
"specific to", "in this recipe", "applied here", or "the application-
specific X is".

Surfaces where citation is inapplicable (root README, intros, yaml
comments, CLAUDE.md) are scored `n/a`.

---

## Criterion 4 — Trade-off two-sidedness (KB Surface 5; IG Surface 4 prose)

**Why this matters**: a KB bullet that names only the chosen path
teaches porters *what to do* but not *why this is the choice*. When
the porter encounters a context where the rejected alternative looks
attractive (their own preference, their own constraint), they can't
weigh the trade-off without knowing what was rejected. Reference
recipes consistently name the alternative and why it lost.

### 7.0 anchor — chosen path only

> **`MaxReconnectAttempts: -1` is mandatory for long-lived consumers**
> — Default NATS clients give up after a handful of reconnects. A
> broker restart during a rolling upgrade then leaves the worker
> disconnected forever, even though the process is still healthy. Set
> the option to `-1` (unlimited) on every long-running subscriber.

Names the chosen path (-1 unlimited). Doesn't name the rejected
alternative (the default's failure mode is implied but not made
explicit as a *choice* the porter could make differently).

### 8.5 anchor — chosen path + rejected alternative named

> **Predis over phpredis** — The php-nginx base image does not
> include the phpredis C extension. Use predis (pure-PHP) to avoid
> "class Redis not found" at runtime. The php-redis option *would*
> be marginally faster on hot paths, but installing it requires
> rebuilding the base image — the perf delta isn't worth the build
> complexity at the showcase tier.

— [laravel-showcase apidev README KB]

Names predis (chosen) AND phpredis (rejected) AND why phpredis lost
(base image rebuild cost > perf delta). Porter who would pick
phpredis on their own learns the cost.

### 9.0 anchor — chosen path + rejected alternative + when to revisit

> **Predis over phpredis** — The php-nginx base image does not
> include the phpredis C extension. Use predis (pure-PHP) to avoid
> "class Redis not found" at runtime. phpredis *is* marginally
> faster on hot paths and worth installing if your workload hammers
> Redis at >10k ops/sec — at that scale, rebuild the base image with
> the C extension. At showcase-tier traffic, the perf delta is
> noise.

Names chosen + rejected + the *trigger condition* for revisiting
(>10k ops/sec). Porter learns when to switch decisions.

### How to score

For every KB bullet, ask: is there a defensible alternative the porter
could pick that would be wrong-by-default in this recipe?

If YES (most bullets — most platform decisions have alternatives):
- Body names the alternative AND why it loses → +1.0
- Body names the alternative + names a trigger to revisit → +1.5
- Body names only the chosen path → -1.0

If NO (rare — sometimes there's only one path, e.g. "Subdomain refs
already carry `https://`"): score `n/a` for this bullet.

Aggregate per-bullet; surface score is the mean.

| Mean | Score |
|---|---|
| -0.5 to 0 | 7.0 |
| 0 to +0.5 | 8.0 |
| +0.5 to +1.0 | 8.5 |
| +1.0 to +1.5 | 9.0 |

---

## Criterion 5 — Classification × surface routing

**Why this matters**: every recorded fact has a classification
(platform-invariant, intersection, scaffold-decision, operational,
framework-quirk, library-metadata, self-inflicted) and the spec's
Classification × surface compatibility table defines which surfaces
each classification can land on. A scaffold-decision routed to the KB
is misrouted (config flavor belongs on the zerops.yaml comment, code
flavor on the IG diff); an operational fact on the IG is misrouted; a
platform-invariant on the CLAUDE.md is misrouted (should be on KB).
Misrouting sends the porter to the wrong surface for the question
they have.

### 7.0 anchor — ≥1 misrouted item per codebase

apidev IG #3 [run-16]:

> ### 3. Alias platform env refs to your own names in `zerops.yaml`

This is *recipe preference* — the recipe chose to alias
`${db_hostname}` → `DB_HOST`. Per spec, recipe preference routes to
zerops.yaml block comment (Surface 7), not IG (Surface 4).
Misrouted.

### 8.5 anchor — zero misrouted items; every routing passes the spec table

Every KB bullet is platform-invariant or intersection. Every IG H3
is platform-invariant or scaffold-decision (code flavor — porter
copies the diff). Every zerops.yaml block comment is scaffold-decision
(config flavor — visible in field values). Every CLAUDE.md item is
operational. No item violates the spec table.

### 9.0 anchor — zero misrouted items + every routing decision is *visibly* intentional from facts.jsonl

Every published item traces back to a recorded fact whose
`candidateClass` and `candidateSurface` match the published surface.
Routing is auditable — a fresh reviewer comparing facts.jsonl to the
published surfaces finds zero "where did this come from?" gaps.

### How to score

For each codebase, enumerate every item across IG, KB, zerops.yaml
comments, CLAUDE.md. For each item:

1. Classify it against the spec table (platform-invariant,
   intersection, framework-quirk, library-metadata, scaffold-decision
   (config/code/recipe-internal), operational, self-inflicted).
2. Check whether the surface accepts that classification.
3. If misrouted, count.

| Misrouted count per codebase | Score |
|---|---|
| ≥2 | 7.0 |
| 1 | 7.5 |
| 0 | 8.5 |
| 0 + every routing traceable to facts.jsonl record | 9.0 |

The traceability check at 9.0 is mechanical: every published
content item must correspond to a fact record (porter_change /
field_rationale / tier_decision) in facts.jsonl whose `topic` or
`candidateHeading` matches the item.

---

## Aggregate scoring

Per surface (when criterion is in scope):

```
surface_score = mean(applicable_criterion_scores)
```

Per recipe:

```
recipe_score = weighted_mean({
    "Codebase IG":         0.20,
    "Codebase KB":         0.25,
    "Codebase intro":      0.05,
    "zerops.yaml":         0.15,
    "Tier import.yaml":    0.15,
    "Tier README extract": 0.05,
    "Root README":         0.05,
    "CLAUDE.md":           0.10,
})
```

Weights reflect the surfaces where porter decisions concentrate
(KB + IG + yaml comments = 60% of the weight).

A run is "above golden" when `recipe_score ≥ 8.5` AND no individual
surface scores below 8.0.

---

## Anchors are not exhaustive

A surface may exhibit shape signals that aren't covered by an anchor.
Use the "How to score" tables as the binding signal list; the anchors
are calibration examples for graders to converge on.

When an artifact lands between two anchors, the score is whichever
anchor it shares more shape signals with (not the midpoint).

When an artifact exhibits one criterion's signal at 9.0 and another
criterion's signal at 7.0 simultaneously (e.g. perfect stem shape +
zero citation), the criteria are scored independently — that's the
point of having five criteria.

---

### Tier-promotion narrative (forbidden per spec §108)

Tier README extracts must NOT include narratives that frame the
current tier as a stepping-stone to a higher tier. Concretely,
fail any extract matching (case-insensitive):
- `\bpromote\b.*\btier\b`
- `\boutgrow\w*`
- `\bupgrade from tier\b`
- `\bgraduate (to|out of)\b`
- `\bmove (up|to) tier\b`

Each tier stands on its own merits. Trade-offs go in the intro;
upgrade narratives don't.

When refinement finds a match, hold the surface and rewrite to
remove the promotion framing — describe what THIS tier is for,
not what tier it leads to.

---

### Unicode box-drawing separators (forbidden)

Yaml comments must be ASCII-only. Box-drawing characters (codepoints
U+2500..U+257F and block elements U+2580..U+259F) render as visual
separators in some terminals but ship mojibake in others. Recipe
yamls go through copy-paste-share lifecycles; ASCII survives,
box-drawing doesn't.

Flag any fragment whose body contains characters in the Unicode
ranges U+2500..U+257F or U+2580..U+259F (the `─━─━┃┏┓┗┛` /
`▀▁▂▃▄▅▆▇█` glyph families). Replace with a single blank line or a
`# section ---` ASCII line if the author wants visual grouping.

Per-recipe TEACH-channel reference: `principles/yaml-comment-style.md`
(loaded at codebase-content + env-content; the positive-shape atom
at the authoring phase). This rubric entry is the refinement-pass
backstop for fragments that slipped past the authoring phases (parent
absorption, copy from a prior recipe, etc.). If the rule's rationale
matters at refinement time, query `zerops_knowledge query=yaml-comment-style`
on demand.

---

### Subdomain rotation overclaim (factual)

Platform-issued Zerops subdomains are stable per service identity —
the `<host>-${zeropsSubdomainHost}.prg1.zerops.app` URL doesn't
change for the lifetime of the service. They do not rotate. Fail
any prose that claims subdomains rotate, are randomized after
deploy, change between deploys, or are otherwise unstable. Common
overclaim phrases (case-insensitive):
- `\bdomain[s]? rotate\b`
- `\bsubdomain[s]? (rotate|change|randomize)\b`
- `\b(rotate|rotates|rotated|rotation) (the|each|every|after) (subdomain|domain|deploy|build)\b`
- `\bunstable\b.*\bsubdomain\b`

Subdomains are stable per service; rewrites should describe the URL
as a porter-controlled value (custom domain swaps it; `enableSubdomainAccess: false`
turns it off; otherwise it's stable). Do not rotate is the correct
framing.

---

## Updates

This rubric ships in run-17. Run-18 dogfood findings inform whether
criteria need addition (e.g. if cross-surface duplication becomes a
load-bearing axis) or removal (e.g. if classification routing
becomes mechanical and not worth grading).

Update protocol: edit this file → `go generate` regenerates
`embedded_rubric.md` → `TestEmbeddedRubric_MatchesSpec` confirms
sync → ship in the next minor.
