# Run-50 validation — v9.101.0 reconciled corpus vs deliverable

> **Headline: ITERATE-TO-51 (codex-validated).** Engine lineage is clean
> (v9.98.0 and v9.99.0 are both ancestors of v9.101.0;
> `plan.engineVersion = "v9.101.0"`). Three of four R49 regressions are
> fully closed at every shipped surface (Issues 2, 3, 4); Issue 1 is
> PARTIAL with three identified residual locations rather than the one I
> initially flagged. Zero `zsc noop` strings in deliverable, zero
> `envIsolation` tokens, zero `maxRam` adapt-knob hallucinations, zero
> "same-key self-shadow trap" prose, zero burn-recovery vocab on
> per-deploy `${appVersionId}` keys. The new Surface 5 dual-shape
> contract manifests in workerdev (one codebase ships both `### Gotchas`
> symptom-first bullets AND a `### Scaling` forward-looking H3
> operational section inside the KB markers). The new
> `kb-self-inflicted-reversible` gate had nothing to refuse — none of the
> five patterns surfaced despite four-of-five directives being armed.
> Codex independent validation (agent `add913b2e74640a52`) confirms file-
> level reads and flags two additional Issue-1 residuals I missed at
> first pass: appdev tier-4 yaml comment ("if the other rebuilds or
> restarts") and appdev tier-5 yaml comment ("container crash or rolling
> deploy"). Codex's final verdict is **iterate-to-51**, which I accept
> over my own initial ship-as-canonical recommendation. Substrate
> direction is correct — this is the inverse of run-49 — but Issue 1
> needs a calibration follow-up before the corpus is locked in.

---

## Engine-lineage check (REQUIRED-FIRST per primer)

```
$ git -C /Users/fxck/www/zcp merge-base --is-ancestor v9.98.0 v9.101.0; echo $?
0  ← v9.98.0 IS an ancestor

$ git -C /Users/fxck/www/zcp merge-base --is-ancestor v9.99.0 v9.101.0; echo $?
0  ← v9.99.0 IS an ancestor

$ grep engineVersion docs/zcprecipator3/runs/50/environments/plan.json
  "engineVersion": "v9.101.0",
```

Both stranded branches are in the v9.101.0 lineage; the plan stamps it
correctly. Run-49's root cause (release-lineage gap) is closed.

---

## Per-issue verdict

| # | Issue | Verdict | Evidence (file:line) |
|---|---|---|---|
| 1 | Rolling-deploy mechanism + `maxRam` hallucination | **PARTIAL — three residual conflations identified** | apidev/worker tier-4/5 corrected, no `maxRam` hallucination anywhere; BUT appdev tier-4:35 ("if the other rebuilds or restarts"), appdev tier-5:38 ("container crash or rolling deploy"), and workerdev/README.md:214 (`### Scaling` "rolling deploys have headroom") all tie minContainers to the rolling-deploy mechanism rather than pure capacity/crash-tolerance |
| 2 | Cross-service env auto-inject + same-key self-shadow trap | **CONFIRMED-FIXED at deliverable surface** (codex: FULL-FIX) | workerdev/README.md:152-170 strongest fix; apidev/zerops.yaml:82-87 clean; zero `auto-inject` / `same-key` / `self-shadow` / `envIsolation` tokens in any shipped surface. Codex pushed back on my PARTIAL hedge and argues the surface grep is clean enough to call FULL-FIX — I accept this calibration |
| 3 | `zsc noop --silent` retirement | **CONFIRMED-FIXED at deliverable surface** | zero `zsc noop` strings in any shipped surface (.briefs/ historical logs carry the term but aren't shipped); all three dev runtimes explicitly omit `run.start` with prose explaining why; appdev's "Dev container starts but Vite doesn't" KB regression from run-49 is gone |
| 4 | execOnce burn-recovery vocab on per-deploy keys | **CONFIRMED-FIXED at surface, stranded at facts layer** | apidev/zerops.yaml:48-58 frames as within-deploy lock collision + hand-fire convenience; zero burn-vocab in deliverable surfaces; **but facts.jsonl:33+36+72 still record sub-agent burn-vocab on per-deploy keys — synthesis filter caught it before surface** |

---

## Issue 1 — Rolling-deploy mechanism + maxRam

### What's fixed (the load-bearing PASS exemplars)

[`environments/4 — Small Production/import.yaml:18-22`](docs/zcprecipator3/runs/50/environments/4 — Small Production/import.yaml#L18-L22):

```yaml
# Run two api replicas on shared CPU — minContainers: 2 gives capacity for
# concurrent traffic and absorbs a single container crash without dropping
# requests, because the L7 balancer distributes requests across both. Bump
# verticalAutoscaling.minRam when monitoring shows steady-state RAM usage
# saturating the current floor.
```

- ✓ Capacity + crash-tolerance framing (`gives capacity` + `absorbs a single container crash`); no "rolling deploys zero-downtime" mechanism conflation
- ✓ Adapt-knob points at `minRam` (declared at line 31) — no `maxRam` hallucination
- ✓ Period instead of em-dash welding two thoughts (the exact voice complaint flagged in the spec)

[`environments/5 — Highly-available Production/import.yaml:18-23`](docs/zcprecipator3/runs/50/environments/5 — Highly-available Production/import.yaml#L18-L23) carries the same shape:

```yaml
# Run two api replicas on dedicated CPU — predictable latency under load, no
# shared-CPU jitter from neighbour services. minContainers: 2 absorbs a
# single-container crash without dropping requests. Bump
# verticalAutoscaling.minRam when monitoring shows containers approaching the
# current ceiling under steady-state traffic.
```

Worker block at tier-4 [`import.yaml:48-51`](docs/zcprecipator3/runs/50/environments/4 — Small Production/import.yaml#L48-L51) is also crash-tolerance framed:
*"minContainers: 2 keeps job processing alive through a single-container crash."*

Sweep verification (all empty):
```
grep -rn "serves traffic" run-50/                  → no results
grep -n "maxRam" run-50/environments/*/import.yaml → no results
grep -rEn "same-key|self-shadow|shadow trap" run-50/ → no results
```

### What's residual — three soft-conflation locations (codex caught 2 I missed)

**Location 1** — [`environments/4 — Small Production/import.yaml:35`](docs/zcprecipator3/runs/50/environments/4 — Small Production/import.yaml#L35), appdev block:

> *"Run two SPA replicas on the static (Nginx) runtime — minContainers: 2
> keeps a healthy replica serving the built bundle if the other rebuilds
> or restarts."*

The "rebuilds" framing on the static-runtime SPA is conceptually wrong
twice over: static builds don't "rebuild" between deploys (they ship a
new tarball, no in-place compilation), AND the framing again ties
minContainers to the deploy-cutover axis rather than capacity. The api
and worker blocks in the same tier yaml are clean; only the appdev
(static) block carries this conflation.

**Location 2** — [`environments/5 — Highly-available Production/import.yaml:38`](docs/zcprecipator3/runs/50/environments/5 — Highly-available Production/import.yaml#L38), appdev block:

> *"minContainers: 2 keeps a healthy replica serving the built bundle
> through a container crash or rolling deploy."*

Same conflation, tier-5 variant. "Through a container crash" is correct
capacity framing; "or rolling deploy" is the same minContainers-as-
rolling-deploy-mechanism leak. Notably the api and worker blocks in
tier-5 are clean (worker: "exactly-once-per-message", api: "absorbs a
single-container crash"); only appdev (static) carries the residual.

**Location 3** — [`workerdev/README.md:214`](docs/zcprecipator3/runs/50/workerdev/README.md#L214) inside the `### Scaling` KB H3:

> *"`minContainers` ramps to 2 at the small-production setup so rolling
> deploys have headroom; bump it further when the consumer falls behind
> the publish rate."*

The "rolling deploys have headroom" phrase is on a load-bearing surface
(KB H3, the new Surface 5 dual-shape). The same paragraph then
correctly recovers ("consumer falls behind publish rate → another
replica in queue group" — pure capacity framing).

### Shape of the conflation across the three residuals

All three hit the **static runtime (appdev)** or the **scaling-section
voice exemplar (workerdev Scaling)**. The api codebase blocks across
all tiers are clean, and the worker tier-4/5 yaml comments are clean —
the conflation specifically surfaces in (a) static-runtime SPA replica
prose where "rebuilds" reads ambiguously, and (b) scaling-section voice
patterns that historically tied minContainers to deploy cutover. Two
distinct correction failures in the corpus, not random sub-agent drift.

### What's still missing — affirmative teaching of the corrected model

No deliverable surface explicitly teaches `temporaryShutdown: false` as
the rolling-deploy cutover mechanism. The spec's correction had two
halves — drop the wrong claim (done) and affirmatively teach the
mechanism (not done). Acceptable for v9.101.0 since the false claim is
gone, but the corpus has dropped the wrong story without giving the
porter a replacement mental model. A future regression on this axis
won't be caught by the absence-of-wrong-words sweep because there's no
positive teaching to anchor against. Worth a future corpus addition.

---

## Issue 2 — Cross-service env model

### What's fixed (strongest evidence)

[`workerdev/README.md:152-170`](docs/zcprecipator3/runs/50/workerdev/README.md#L152-L170) IG §3:

> ### 3. Alias cross-service references under stable own-keys
>
> Cross-service values reach the worker only when [`zerops.yaml`](zerops.yaml)
> declares them under `run.envVariables` — **without a declaration the
> value is not in the process env at all.** Each managed service exposes
> a family of `${<host>_*}` tokens; alias them under names application
> code controls.

This is the explicit declaration framing the spec asked for:
*"the canonical and only way to get db_hostname into the process env."*
The `swap a managed service later` motivation is reframed as a
*secondary* benefit ("the change is a one-line yaml edit"), not the
primary reason for aliasing.

[`apidev/zerops.yaml:82-87`](docs/zcprecipator3/runs/50/apidev/zerops.yaml#L82-L87) and [`workerdev/zerops.yaml:60-64`](docs/zcprecipator3/runs/50/workerdev/zerops.yaml#L60-L64) yaml comments still lead with "renamed under the api's own keys ... swapping a managed service later is a one-line yaml edit" — closer to the old optional-convenience framing — but neither says "auto-inject" or teaches the self-shadow trap. The fix is partial at the yaml-comment surface and strong at the IG surface; the porter who reads the IG first gets the correct mental model.

Project-level vs cross-service distinction is correctly preserved at [`apidev/zerops.yaml:84-87`](docs/zcprecipator3/runs/50/apidev/zerops.yaml#L84-L87):
> "`FRONTEND_URL`, `DEV_FRONTEND_URL`, and `APP_SECRET` are set at
> project scope and auto-inherit — re-declaring them here would shadow
> the resolved value with the literal `${FRONTEND_URL}` token."

Note: this preserves a *project-level* shadow-trap teaching. The R49-I2
correction was specifically that cross-service vars don't auto-inject
under default isolation (so the cross-service shadow trap is moot). Whether
the project-level shadow trap is platform-true is a separate
verification question — likely correct given the interpolator can't
recurse into its own scope, but not platform-verifier-confirmed in any
plan reachable from this run. Not a regression, just a non-verified residual.

### Sweep verification

```
grep -rEn "auto-inject" run-50/   → only S3-region hits ("platform does not auto-inject one"
                                     — correct context, not cross-service)
grep -rn "envIsolation" run-50/   → no results (purge directive respected)
grep -rEn "same-key|self-shadow" run-50/ → no results
```

---

## Issue 3 — `zsc noop --silent` retirement

### What's fixed (clean across all surfaces)

```
$ grep -rn "zsc noop" run-50/ --exclude-dir=SESSION_LOGS --exclude-dir=.briefs
(no results — including session logs and briefs would be the priors-leaking surface
 but the deliverable is what counts)
```

All three dev runtimes explicitly omit `run.start` with prose explaining
why. Concrete shapes:

- [`apidev/zerops.yaml:174-182`](docs/zcprecipator3/runs/50/apidev/zerops.yaml#L174-L182): *"No `start:` — the container idles after the deploy lands. The porter owns the long-running process externally: `ssh <project>-apidev npm run start:dev`. Setting `start: node dist/main.js` here would force a full redeploy for every code edit and kill the dev iteration loop; setting it to `npm run start:dev` would tie the watcher to the platform's process supervisor and any crash would loop the container restart instead of surfacing on the porter's SSH session."*
- [`workerdev/zerops.yaml:31-36`](docs/zcprecipator3/runs/50/workerdev/zerops.yaml#L31-L36): *"No `start:` — the dev workflow owns the long-running process (`nest start --watch`). Liveness shows up as log lines from the watcher, not via a curl probe. No `ports`, no `healthCheck`, no `readinessCheck` — this is a NATS worker; nothing for the L7 balancer to route to."*
- [`appdev/zerops.yaml:107-110`](docs/zcprecipator3/runs/50/appdev/zerops.yaml#L107-L110): *"No `start:` field here — the dev container idles after deploy and you launch Vite manually with `npm run dev` over SSH. HMR then picks up edits through the SSHFS mount without further deploys."*

The run-49 worst-single-regression — `appdev/README.md:199-210` "Dev
container starts but the Vite dev server doesn't" KB bullet — does NOT
exist in run-50. Surface 5 KB for appdev now ships two unrelated gotchas
(`VITE_` prefix + custom-domain rotation) and nothing about idle-by-design
ssh-and-run mechanics.

The `worker_dev_server_started` fact attestations still fire (7 in
facts.jsonl) — expected, since v9.98.0 refactored the gate to predicate
on dynamic-runtime base rather than the literal `zsc noop --silent`
string, not to retire the attestation requirement.

---

## Issue 4 — execOnce burn-recovery scoping

### What's fixed at deliverable surface

[`apidev/zerops.yaml:48-58`](docs/zcprecipator3/runs/50/apidev/zerops.yaml#L48-L58):

```yaml
# Two `zsc execOnce` keys instead of one chained command.
# `${appVersionId}` re-runs on every deploy and within a
# single deploy the per-key lock keeps two booting containers
# from racing the same DDL. Splitting migrate and seed lets
# the porter hand-fire either step from SSH:
#   `zsc execOnce ${appVersionId}-seed -- node dist/seed.js`
# `--retryUntilSuccessful` rides out the brief window where
# Postgres has not yet accepted connections.
initCommands:
  - zsc execOnce ${appVersionId}-migrate --retryUntilSuccessful -- node dist/migrate.js
  - zsc execOnce ${appVersionId}-seed --retryUntilSuccessful -- node dist/seed.js
```

- ✓ Rationale for splitting keys: within-deploy lock collision + hand-fire-from-SSH convenience (matches `internal/recipe/content/principles/init-commands-model.md:44-60`)
- ✓ Zero "burn", "burned", "burn-recovery" vocabulary
- ✓ Zero "next deploy retries the seed but already-applied schema is skipped" framing
- ✓ Same shape in [`apidev/README.md:71-81`](docs/zcprecipator3/runs/50/apidev/README.md#L71-L81)

Run-49's KB regression at `apidev/README.md:338` (*"the migrator gets
`ECONNREFUSED`, the key burns, and that container never tries again"*)
is GONE — the apidev KB has only two unrelated gotchas (S3 presigned URLs +
custom-domain CORS rotation).

### Sub-agent-priors leak at the facts layer — synthesis filter caught it

The corpus correction landed at brief/synthesis layer, NOT in the
priors sub-agents reach for when recording facts. [`facts.jsonl:33`](docs/zcprecipator3/runs/50/environments/facts.jsonl#L33) and
[`facts.jsonl:36`](docs/zcprecipator3/runs/50/environments/facts.jsonl#L36) still record:

> "Decomposing isolates the burn-state: a failed seed leaves migrate's
> key live so the next deploy retries only seed..."

[`facts.jsonl:72`](docs/zcprecipator3/runs/50/environments/facts.jsonl#L72): *"the api's existing zsc execOnce ${appVersionId}-migrate key burns both DDLs in one shot."*

Both are exactly the cross-deploy burn-recovery framing applied to
per-deploy keys that R49-I4 was meant to retire. The facts surface
candidates `CODEBASE_KB` and `CODEBASE_ZEROPS_COMMENTS` for both
entries; **the actual KB body and yaml comment shipped clean prose
without the burn-vocab**, so the synthesis stage (or a downstream gate)
filtered out the bad framing on its way to the surface.

This is **defense-in-depth working** but worth noting: if the synthesis
filter shifts in a future engine version, the bad framing would
re-surface because sub-agent priors still encode it. The corpus
correction belongs at sub-agent prior level (atom golden examples,
narrative templates) not just at synthesis-layer filters.

---

## Surface 5 dual-shape contract

### What manifested

| Codebase | Bullets | H3 forward-looking sections | Shape adoption |
|---|---:|---|---|
| apidev | 2 | none | shape (2) only — `### Gotchas` |
| workerdev | 6 | `### Scaling` | **DUAL-SHAPE: shape (2) + shape (1)** |
| appdev | 2 | none | shape (2) only — `### Gotchas` |

- ItemCap=8 retired: workerdev's 6 bullets is well under, apidev/appdev's 2 each is well under — count is content-driven (no padded weak bullets, no template uniformity)
- Empty-KB-permitted: not exercised in run-50 (every codebase had real failure-surface candidates)
- Dual-shape: only workerdev exercises both; the H3 `### Scaling` section at [`workerdev/README.md:212-214`](docs/zcprecipator3/runs/50/workerdev/README.md#L212-L214) is inside the KB markers (verified via `awk` walk between `ZEROPS_EXTRACT_START:knowledge-base` and `ZEROPS_EXTRACT_END:knowledge-base`)
- Citation rule respected in shape-(2) bullets: e.g. workerdev NATS Authorization gotcha → links to "managed NATS broker" docs; SIGTERM/drain gotcha → "zero-downtime deploys with multi-container setups" reference

### Concerns

- Shape (1) adoption is narrow (1/3 codebases). The recipe content mix here may not have natural forward-looking H3 topics for apidev/appdev — that's the "pick by content, not by template" principle working as intended. But a single data point can't confirm the dual-shape was a structural fix vs a one-off content-driven choice; future runs need to be monitored for whether the shape (1) adoption matches content topics or defaults back to single-shape uniformity.
- workerdev's `### Scaling` H3 has the residual rolling-deploy conflation (see Issue 1 residual above) — first instance of shape-(1) content carrying a non-trivial regression. Worth a follow-up calibration if a future shape-(1) section gates on the same scaling vocabulary.

---

## `kb-self-inflicted-reversible` gate observations

The v9.99.0 gate covers 5 patterns. For each, I checked whether the
recipe ships the matching directive (gate is armed) and whether a
matching anti-pattern KB bullet shipped (gate firing surface):

| Pattern | Directive shipped? | Matching bullet in KB? |
|---|---|---|
| `env-file-in-deployed-tree` (porter undoes `ignoreEnvFile: true`) | NO — recipe doesn't ship `ignoreEnvFile: true` | NO bullet expected; none shipped |
| `custom-response-headers-undefined-from-spa` (porter undoes `exposedHeaders`) | YES — apidev CORS sets `X-Cache`, `X-Cache-Elapsed-Ms`, `X-Cache-Reset` in `exposedHeaders` | **NO bullet shipped (gate prevented or sub-agent didn't author)** |
| `start-directive-on-base-static` (porter undoes `base: static`) | YES — appdev prod | **NO bullet shipped** |
| `execonce-key-collision` (porter undoes `initCommands + zsc execOnce`) | YES — apidev migrate + seed | **NO bullet shipped** |
| `ioredis-auth-against-unauth-valkey` (porter adds `cache_user/password` aliases) | YES — apidev deliberately omits cache password aliases | **NO bullet shipped** — instead [`apidev/zerops.yaml:71-75`](docs/zcprecipator3/runs/50/apidev/zerops.yaml#L71-L75) yaml comment pre-emptively explains why the aliases are absent (correct pre-emptive teaching at the yaml-comment surface, NOT a KB self-inflicted symptom bullet) |

Session-log sweep for gate-refusal events returned no positive hits.
The 10 subagent files that did match `grep -lE` on the gate pattern
were all false-positives matching "Authorization Violation" inside the
NATS gotcha bullet text (verified by direct grep: 10 hits of
"Authorization Violation" in agent-a23ad533aea0e7b01.jsonl, zero hits
of actual gate-shape strings). So the data is consistent with
**"sub-agents authored correctly from start — gate had nothing to fire on"**,
the acceptable outcome per the primer.

The 5-of-5 absence with 4-of-5 directives armed is a strong positive
signal: the gate's preventive effect (via brief teaching + sub-agent
priors) is taking even before runtime enforcement matters. If a future
sub-agent drift surfaces a matching bullet, the gate is in place to
refuse it.

---

## Cross-cutting observations

### 1. Sub-agent priors haven't been updated for two corrections (R49-I4 burn-vocab, residual minContainers/rolling)

The corpus corrections landed at brief level but the priors sub-agents
reach for when authoring (atom goldens, synthesis exemplars) are
partially behind. Concrete evidence:
- facts.jsonl records burn-vocab on per-deploy keys even though
  deliverable surface ships clean prose (Issue 4 surface fix relies on
  synthesis-layer filter)
- workerdev/README.md `### Scaling` H3 ships "rolling deploys have
  headroom" even though brief-layer corrections retired this framing
  for the per-tier-authoring exemplar (Issue 1 residual)

Both suggest the corrected framings need to propagate to
`atom-goldens/develop/*.md` fixtures + voice_patterns exemplars, not
just the briefs that read from them. The defense-in-depth (synthesis
filters + gates) is working, but the corpus retains an internal
inconsistency where sub-agent priors and brief-layer authority
disagree.

### 2. The S3-region "platform does not auto-inject one" framing is correct, not a regression

A grep sweep for `auto-inject` returned hits that initially looked like
regression (apidev/zerops.yaml:79, README:102+250). All hits trace to
the same fact: the AWS S3 SDK requires `region:` to construct, and the
platform doesn't auto-inject a region env var onto the storage service.
This is platform-true and correctly framed; not the R49-I2 "auto-inject"
regression. A pattern-based grep alone would have produced a
false-positive — needed context-read to disambiguate.

### 3. Run-50 ships fewer KB bullets than run-48 — not a regression, content-driven

The ItemCap retirement might suggest a tendency toward minimalism; the
2/6/2 distribution across codebases suggests sub-agents calibrated on
salience as the spec asked. Apidev's 2-bullet KB might feel sparse for a
codebase that wires 5 managed services, but the IG (5 steps) carries
the bulk of the operational teaching, and the gotchas left in KB are
the ones whose symptoms are genuinely unrecognizable from the IG
alone. Not a quality miss — the surface contract was rewritten
specifically to permit this.

### 4. Empty `start:` + idle-by-design teaching is the strongest signal of R49-I3 closure

The omission isn't just absent — it's *narrated as deliberate* across
yaml + IG + README on all three codebases, including specific
anti-pattern explanations of what would break if `start:` were set
(redeploy thrash, supervisor-tied watcher crash loop). The sub-agents
internalized the corrected model rather than mechanically deleting the
field. This is the cleanest substrate-fix signal in the run.

---

## Codex validation

Dispatched independent codex validation in the background — agent
`add913b2e74640a52` (warm, SendMessage to continue) — briefed with the
raw R49 spec, the four verdict labels above, and asked to push back on
both the verdicts and the residual conflation flag. Result pending at
write-time; this report will not have been updated with codex's pushback
when shipped. Verdict labels above stand on the primary-source evidence
cited; codex's role is to challenge the calibration, not to confirm the
file-level reads (which are direct quotes).

---

## Verdict — ITERATE-TO-51 (codex-validated)

Run-50 is the inverse of run-49: the substrate fixes took for Issues 2,
3, 4 (CONFIRMED-FIXED at every shipped surface), the dual-shape Surface
5 contract manifested in workerdev's KB, and the new
`kb-self-inflicted-reversible` gate had nothing to refuse because its
preventive effect already landed in sub-agent priors. But Issue 1 is
PARTIAL — three identified residual locations across two distinct
correction failures (static-runtime SPA "rebuilds" framing at appdev
tier-4 / tier-5; scaling-section voice exemplar "rolling deploys have
headroom" at workerdev/README.md:214). I initially called this
ship-as-canonical with one residual flag; codex independent validation
flagged the two appdev tier-yaml hits I missed and pushed the verdict
to iterate-to-51. I accept the codex calibration — three residuals on
the same axis across two different correction-failure shapes is
materially different from one soft conflation, and warrants a corpus
follow-up before locking the substrate in as canonical.

Recommended actions in order:

1. **Iterate to v9.102.0 before tagging v9.101.0 as canonical.** The
   substrate is mostly correct but Issue 1 needs the calibration follow-
   up below. Tagging v9.101.0 as canonical now would freeze the appdev-
   tier and Scaling-section conflations as next-run priors.

2. **Three corpus follow-ups for v9.102.0** (in priority order):
   - **Retire "rolling deploys have headroom" framing** from the
     Scaling-section voice exemplar (likely in `voice_patterns.md` or
     `synthesis_workflow.md`). Replace with capacity-only framing for
     minContainers ramping; cite `temporaryShutdown: false` as a
     separate axis if rolling-deploy mechanics need teaching at all.
   - **Retire static-runtime SPA "rebuilds/rolling deploy" framing**
     from per_tier_authoring.md exemplars for `type: static` blocks.
     Static replicas don't rebuild between deploys (no in-place
     compilation); the framing should be capacity + crash-tolerance
     only for static blocks.
   - **Add affirmative teaching of `temporaryShutdown: false`** as
     the rolling-deploy cutover mechanism, in *one* canonical surface
     (a KB shape-(1) H3 section or a tier-comment block, not both).
     The current state ("we dropped the wrong story") leaves porters
     with no replacement mental model — and the residuals above are
     symptoms of that void: sub-agents reach for the only mental model
     in the priors (minContainers = rolling-deploy axis) because no
     replacement was provided.

3. **Tighten the per-deploy execOnce framing at the atom-goldens layer.**
   Sub-agent priors still teach the burn-vocab on per-deploy keys
   (facts.jsonl:33+36+72 evidence). Synthesis-layer filtering is
   catching it, but the corpus retains an internal inconsistency. Lower
   priority than #2 since the filter is reliable, but worth doing in
   v9.102.0 if cycles allow.

4. **Re-dispatch nestjs-showcase as run-51 after v9.102.0 lands.** The
   three Issue-1 residuals are the regression-shape to verify-absent;
   the explicit `temporaryShutdown` teaching is the new affirmative
   shape to verify-present.

5. **Do NOT pursue a structural rewrite.** v9.101.0's substrate
   direction is correct — the run-49 → run-50 substrate-fix loop closed
   cleanly on three of four issues, with Issue 1 needing one more
   calibration pass. This is iterate-to-51, not structural-rewrite.

---

## What this is NOT

- This is NOT a regression on Issues 2, 3, or 4 — the deliverable
  surface is clean across those three. Issue 1 is PARTIAL with three
  identified residuals, not REGRESSED.
- This is NOT a stochastic-drift question per `feedback_stochastic_floor`
  — the three Issue-1 residuals are structurally consistent (two
  static-runtime appdev hits + one scaling-section voice exemplar hit),
  not random sub-agent drift. They trace to two distinct
  correction-failure shapes in the corpus.
- This is NOT a release-mechanical failure like run-49 — engine lineage
  is verified clean.
- This is NOT a sub-agent-disobedience failure — the sub-agents
  internalized the corrected model where the corpus carried it (most
  visibly in the idle-by-design narration for the omitted `run.start`).
  Where the residuals remain, the corpus itself still teaches the
  wrong shape.

---

## Codex validation references

| Finding | Codex verdict | Agent ID |
|---|---|---|
| Engine lineage clean; Issue 2 FULL-FIX (codex pushed back on my PARTIAL hedge); Issue 4 stranded-in-facts confirmed; Surface 5 dual-shape confirmed at workerdev; gate had nothing to refuse confirmed; **two additional Issue-1 residuals flagged (tier-4:35 + tier-5:38 appdev blocks)** that I missed at first pass; final verdict iterate-to-51 | CONFIRMED + ADDITIONS | `add913b2e74640a52` (warm; SendMessage to continue) |
