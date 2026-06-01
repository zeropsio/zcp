# Run-51 validation — first full Opus 4.8 run on the post-v9.102.0 hardened engine

> **Headline: ITERATE-TO-52.** *(Codex independently verified all findings and
> CONCURRED with ITERATE-TO-52, correcting the report in three places — it
> gave the RC1 substrate-attribution a "haircut" (the agent shared fault), it
> REFUTED the "from nodejs-template-not-parent" provenance detail (the stale
> token is in both), and it flagged that I'm somewhat too generous to Axis-A
> and that process-ergonomics is now the dominant systematic risk. See Codex
> validation.)*
> Engine lineage is clean *and ahead of the task premise*: the run-51 plan
> stamps `engineVersion = "v9.103.2"`, not v9.102.0 as the brief assumed.
> v9.102.0 (commit `53dbd9db`, "slug-drop + verify-first") is an ancestor of
> v9.103.2, so every piece of the dispatch hardening is present — the
> substrate is the *inverse* of run-49's stale-binary gap, this time
> overshooting expectation rather than lagging it. (Provenance nuance:
> v9.103.2 is untagged in this repo and HEAD sits at v9.102.0, so the exact
> v9.103.x source can't be inspected from this tree; v9.103-specific
> behaviour is inferred from the deliverable + logs.) **Axis A (YAML content
> + correctness): the strongest run yet.** All 9 YAMLs are structurally valid
> (zero `additionalProperties`/required/mode errors); their only divergence
> from the *current published* schema is the bare-form type/base tokens
> (`nodejs@22`, `postgresql@18`, …) which are **API-accepted legacy BC** per
> `internal/topology/type_equivalence.go` and pass the engine's own bundled
> schema — a corpus-wide representation choice, not a run-51 defect (run-50
> used identical tokens; the recipe deployed green). R49 Issues 1, 2, 4 are
> CONFIRMED-FIXED — Issues 1 and 4 materially
> *improved* over run-50, including the affirmative new-then-cutover teaching
> run-50 flagged as missing, and Issue-1's corrected mental model has now
> propagated all the way into `facts.jsonl` (the residual run-50 could not
> close). The one Axis-A blemish is **Issue 3: a CONTAINED regression** —
> `workerdev/zerops.yaml:84` (and `README.md:107`) ship `start: zsc noop
> --silent`, the exact stale pattern the owner asked to retire. Workflow
> verification *corrected my first read*: the root cause is **not** "inherited
> from parent" as the TIMELINE claims, but an **incomplete R49-I3 purge** —
> `zsc noop` still ships in 43 `internal/knowledge/recipes/` files (the
> `zerops_knowledge` corpus scaffold subagents read), and the 4.8 subagent
> wrote it despite its brief saying "omit `run.start`" five times. It is
> contained (api + app correctly omit `run.start`; the briefs all teach
> retirement), and refinement *detected it, attempted the fix, was correctly
> engine-blocked* by the yaml-shape ownership gate, then escalated.
> **Axis B (flow quality): it did NOT walk smoothly — it took a guidance-
> induced misordering and a forced hard workaround.** (The "36 subagents"
> alarm is itself a miscount — there are **18**, and none of the v9.102.0
> hardening failure modes (slug-drop, outputRoot, parentStatus) tripped — but
> that does not make the walk clean.) The first cause is the **engine's own
> guidance**: the `complete-phase feature` next-call instruction (L178) told
> the agent to `build-subagent-prompt` + dispatch the 6 codebase-content
> workers *without first sequencing `enter-phase codebase-content`*, and
> `build-subagent-prompt` succeeds with no enter-phase precondition. So the
> agent (following guidance) dispatched 6 workers into a not-yet-entered
> phase → their fact shells didn't exist and the leftover scaffold bare-yaml
> gate fired on their output (masking 7 independent citation defects) → the
> agent had to detect this, `enter-phase` late, and re-stitch → the citation
> defects then surfaced → and the natural fix (resume the warm authoring
> workers) was **impossible because `SendMessage` is unavailable in this
> environment**, forcing **3 fresh cold fix agents** — the "extra subagents"
> and the hard workaround the user flagged. This is a **substrate cascade**,
> not 4.8 eagerness: the model largely followed the engine's instructions.
> **Highest-leverage v9.103.x followups:** (A) **fix the phase-walk guidance +
> add an `enter-phase` precondition** — `complete-phase feature` must
> sequence `enter-phase codebase-content` *before* dispatch, and
> `build-subagent-prompt`/dispatch should refuse (or auto-enter) until the
> phase is active; (B) **make `SendMessage` available** to the recipe main
> agent so gate-rejection fixes resume the warm authors instead of cold
> re-dispatch; (C) scrub `zsc noop` from all 43 `internal/knowledge/recipes/`
> files (Issue 3 — the only Axis-A correctness item the owner cares about).

---

## Engine-lineage check (REQUIRED-FIRST per primer)

```
$ git -C /Users/fxck/www/zcp merge-base --is-ancestor v9.101.0 v9.102.0; echo $?
0  ← v9.101.0 IS an ancestor of v9.102.0

$ git -C /Users/fxck/www/zcp merge-base --is-ancestor v9.101.5 v9.102.0; echo $?
0  ← v9.101.5 IS an ancestor of v9.102.0

$ jq -r .engineVersion docs/zcprecipator3/runs/51/environments/plan.json
v9.103.2          ← AHEAD of v9.102.0, not equal to it

$ git tag --list 'v9.10[23]*' | sort -V
v9.102.0          ← latest tag in repo; no v9.103.x tag exists
$ git describe --tags HEAD
v9.102.0          ← HEAD == v9.102.0, 0 commits ahead
$ git log -1 --format='%H %s' v9.102.0
53dbd9db…  recipe: harden authoring flow against Opus 4.8 slug-drop + verify-first
```

**Verdict: lineage gate PASSES — and in the safe direction.** The brief
assumed run-51 ran against v9.102.0; the plan actually stamps **v9.103.2**.
Because v9.102.0 (the hardening commit) is an ancestor of v9.103.2, the
slug-less recovery, guidance interpolation, canonical-outputRoot gate, and
start-error/next-call headers are all *guaranteed present*. The run-engine
progression is monotonic across runs: run-49 = v9.100.1, run-50 = v9.101.0,
run-51 = v9.103.2. This is the structural inverse of run-49 (whose root
cause was a binary *behind* its expected tag). The only caveat is
provenance: v9.103.2 is not a tag in this checkout and HEAD is at v9.102.0,
so I cannot read the exact v9.103.x source — any v9.103-specific behaviour in
this report is inferred from the deliverable and session logs, not from code.

---

## Per-issue verdict

| # | Issue | Verdict | Evidence (file:line / session-line) |
|---|-------|---------|-------------------------------------|
| 1 | Rolling-deploy mechanism + `maxRam` hallucination | **CONFIRMED-FIXED + IMPROVED** | Zero `maxRam` in shipped surfaces (7× only in engine briefs/logs). Tier-4/5 `minContainers` comments are capacity+crash-framed ([`4 — Small Production/import.yaml:14-20`](docs/zcprecipator3/runs/51/environments/4%20—%20Small%20Production/import.yaml#L14-L20)). Run-50's 3 residuals (appdev "rebuilds", "rolling deploy", workerdev Scaling "headroom") are GONE. NEW affirmative new-then-cutover teaching at [`apidev/README.md:229`](docs/zcprecipator3/runs/51/apidev/README.md#L229). Corrected model now in `facts.jsonl:85` ("not the deploy-cutover one"). |
| 2 | Cross-service env auto-inject + same-key self-shadow | **CONFIRMED-FIXED** (minor vocab nit) | Zero `envIsolation`, zero cross-service auto-inject claims. All `auto-inherit` hits are project-level (correct half of R49-I2): [`apidev/README.md:277`](docs/zcprecipator3/runs/51/apidev/README.md#L277). Cross-service "self-shadow" prose ([`apidev/zerops.yaml:70`](docs/zcprecipator3/runs/51/apidev/zerops.yaml#L70)) is outcome-correct per the engine's `ops/env_shadow.go:5-38`. |
| 3 | `zsc noop --silent` retirement | **CONTAINED REGRESSION (root cause: incomplete R49-I3 purge)** | Ships on TWO surfaces: [`workerdev/zerops.yaml:84`](docs/zcprecipator3/runs/51/workerdev/zerops.yaml#L84) + [`workerdev/README.md:107`](docs/zcprecipator3/runs/51/workerdev/README.md#L107). api + app dev correctly OMIT `run.start`. Root cause: `zsc noop` persists in **43 `internal/knowledge/recipes/` files** (the `zerops_knowledge` corpus scaffold subagents read; R49-I3 only scrubbed the briefs) + a 4.8 self-contradiction (brief said omit 5×; subagent wrote it anyway). Refinement detected + attempted + was engine-blocked ([`slot_shape.go:349-377`](internal/recipe/slot_shape.go#L349-L377)) + escalated. Regression from run-50's clean state. |
| 4 | execOnce burn-recovery on per-deploy keys | **CONFIRMED-FIXED + IMPROVED at surface; stranded at facts** | migrate = per-deploy `${appVersionId}`, seed = STATIC `nestjs-showcase.seed.v1` with correct `.v1→.v2` rotation framing ([`apidev/README.md:267-269`](docs/zcprecipator3/runs/51/apidev/README.md#L267-L269)). Zero burn-vocab in deliverable. `facts.jsonl:28-29` retain superseded scaffold-era burn framing (both-per-deploy plan). |
| S5 | Surface-5 dual-shape (run-50 calibration item) | **CONFIRMED STRUCTURAL** | apidev KB ships shape-(1) forward-looking topic H3s (`### Object Storage…`, `### Search key rotation`, `### Re-firing the seed…`) + shape-(2) `### Gotchas` bullets ([`apidev/README.md:259-278`](docs/zcprecipator3/runs/51/apidev/README.md#L259-L278)). Richer than run-50's single Scaling-H3, content-driven, and clean of the Issue-1 conflation run-50's Scaling-H3 carried. |
| G | `kb-self-inflicted-reversible` gate | **ARMED, NOTHING FIRED** (= run-50) | Violation code `kb-self-inflicted-reversible` fired 0× in `main-session.jsonl`; the 8 "self-inflicted" hits are guidance/litmus vocabulary. Preventive effect holding. |
| F | Flow quality | **DID NOT WALK SMOOTHLY — substrate-induced cascade + forced hard workaround** | First cause: `complete-phase feature` guidance (L178) sequenced dispatch *before* `enter-phase codebase-content`; `build-subagent-prompt` has no enter-phase precondition → 6 workers ran in the wrong phase → bare-yaml gate masked 7 citation defects → late enter-phase + re-stitch → fix needed resume but **`SendMessage` unavailable (L236)** → 3 cold fix agents. 18 subagents (NOT 36); zero slug-drop/outputRoot/parentStatus failures. |

---

## Axis A1 — Schema correctness (structurally valid; a published-vs-BC nuance)

I re-validated all 9 YAMLs against the live schemas the brief named
(`import-project-yml-json-schema.json`, `zerops-yml-json-schema.json`, both
fetched fresh, Draft 2020-12). The honest picture has two layers — and
independent verification corrected my first-pass "schema-clean" phrasing:

- **Structure is clean.** Once the type/base tokens are set aside, there are
  **zero** structural violations across all 9 files: no `envVariables` at a
  setup-entry top level (only `build.envVariables` / `run.envVariables` — the
  CLAUDE.md invariant holds), no `additionalProperties` violations, no missing
  required fields, `mode` is `HA`/`NON_HA` only, and `buildFromGit` sits only
  on runtime services (never on managed deps). My normalized-structure
  validation passes 9/9.

- **The bare-form type/base tokens fail the *current published* schema.** The
  published enum lists only composite forms (`alpine/nodejs@22`,
  `postgresql:single@18`, `valkey:ha@7.2`, …) after the 2026-05-18 release;
  the deliverable uses bare forms (`nodejs@22`, `postgresql@18`, `valkey@7.2`,
  `nats@2.12`, `meilisearch@1.20`). This gap hits **all 9 YAMLs across two
  field families** — `services[].type` in the 6 imports AND `build.base` /
  `run.base` in the 3 zerops.yaml files (a footprint my first draft
  under-counted to "6 imports / one field").

- **But the bare forms are API-accepted legacy BC, not an error.**
  [`internal/topology/type_equivalence.go:6-22`](internal/topology/type_equivalence.go#L6-L22) documents it directly:
  *"Sunday-release 2026-05-18 moved Zerops upstream identifiers to composite
  form… Legacy forms (`nodejs@22` + sibling `os:`, `postgresql@18` + sibling
  `mode:`) stay accepted by the Zerops API for BC; ZCP must accept both."* The
  engine's own bundled schema snapshot
  (`internal/schema/testdata/import_yml_schema.json`) is a **superset
  containing both** bare and composite forms, which is why the bare tokens
  passed the engine's validation gate at run time, and the recipe deployed
  live green (TIMELINE.md). Run-50 used the identical bare tokens.

**Verdict: structurally correct and API-valid; "schema-clean against the
current *published* schema" would be the overstatement my draft made.** The
accurate statement is: *structurally valid + API-accepted BC bare forms that
pass the engine's bundled schema but diverge from the current strict published
enum.* This is a **corpus-wide modernization question** (should the recipe
corpus migrate type/base tokens to composite form to match the published
schema, or is BC-acceptance the deliberate stance?), not a run-51-specific
regression — every recipe emits bare forms. Worth a separate decision; it does
not gate run-51 on its own because the API accepts the forms today.

---

## Issue 1 — Rolling-deploy mechanism + maxRam

### Status: CONFIRMED-FIXED and materially improved over run-50

**The `maxRam` hallucination is gone from every porter-facing surface.**
`grep maxRam` across all import.yaml / zerops.yaml / README / CLAUDE.md
returns zero hits; every adapt-knob points at the field the yaml actually
sets — `verticalAutoscaling.minRam`. (`maxRam` survives only in
engine-authored `.briefs/*.md` generic knob-lists and SESSION_LOGS, neither
of which ships to porters — so "zero in shipped surfaces," not "zero
anywhere.")

**The tier comments are clean capacity+crash framing.** Tier-4
([`4 — Small Production/import.yaml:14-20`](docs/zcprecipator3/runs/51/environments/4%20—%20Small%20Production/import.yaml#L14-L20)):

```
# Run two NestJS API containers — minContainers: 2 gives the
# service capacity for concurrent traffic and absorbs a single
# container crash without dropping requests, and the L7
# balancer spreads requests across both. Zero-downtime rolling
# deploys work the same at one or two containers. Bump
# verticalAutoscaling.minRam when monitoring shows steady-state
# RAM saturating the current floor.
```

- ✓ Capacity + crash-tolerance framing for `minContainers: 2`.
- ✓ "Zero-downtime rolling deploys work the same at one or two containers" —
  a **correct disjoint statement** (the task pre-flagged this), not a
  conflation. It explicitly decouples the deploy-cutover axis from
  minContainers.
- ✓ Period-separated thoughts; the em-dash is appositive (introducing the
  elaboration), not the R49 two-thought weld.

**The three run-50 residuals are closed.** Run-50 was ITERATE-TO-51 because
of three soft conflations (appdev tier-4 "rebuilds or restarts", appdev
tier-5 "container crash or rolling deploy", workerdev Scaling "rolling
deploys have headroom"). In run-51:
- appdev tier-4 ([`import.yaml:32-36`](docs/zcprecipator3/runs/51/environments/4%20—%20Small%20Production/import.yaml#L32-L36)):
  *"minContainers: 2 keeps it reachable through a single container crash and
  spreads load across both."* — capacity only, no "rebuilds".
- appdev tier-5 ([`import.yaml:34-38`](docs/zcprecipator3/runs/51/environments/5%20—%20Highly-available%20Production/import.yaml#L34-L38)):
  *"minContainers: 2 keeps it reachable through a container crash, and
  dedicated cores hold steady latency…"* — no "rolling deploy".
- workerdev README has **no `### Scaling` H3 at all** (so the "headroom"
  exemplar can't recur); scaling guidance moved into IG §5 (queue group) and
  the Gotchas bullets, all correctly framed.

### The affirmative teaching run-50 said was missing is now present

Run-50's report noted: *"No deliverable surface explicitly teaches the
rolling-deploy cutover mechanism… the corpus has dropped the wrong story
without giving the porter a replacement mental model."* Run-51 fixes exactly
this. [`apidev/README.md:227-236`](docs/zcprecipator3/runs/51/apidev/README.md#L227-L236) IG §4:

> *"A rolling deploy starts the new container, waits for its readiness
> check, then stops the old one with `SIGTERM`. … Graceful drain is what
> makes the readiness-gated switch a true zero-downtime deploy — the old
> container finishes its work while the new one already serves new
> traffic."*

This is the correct **new-then-cutover** mechanism, attributing zero-downtime
to readiness-gating + drain, not to `minContainers`. The worker README IG §4
teaches the same for the consumer side (drain-on-SIGTERM).

### The correction has reached the facts layer (run-50's open residual)

Run-50's cross-cutting concern #1 was that the corrected framing landed at
brief level but not in sub-agent priors / facts. Run-51 closes this:
[`facts.jsonl:85`](docs/zcprecipator3/runs/51/environments/facts.jsonl#L85) (tier-4 decision) states verbatim:

> *"Two always-on containers give capacity for concurrent traffic and absorb
> a single-container crash… zero-downtime rolling deploys work the same at 1
> or 2 containers via the platform default, so this floor is the
> capacity-and-crash-tolerance axis, not the deploy-cutover one."*

Every other rolling-deploy mention in `facts.jsonl` (lines 5, 26, 57, 72)
attributes zero-downtime to SIGTERM-drain + readinessCheck, never to
minContainers. The two-orthogonal-axes model is now internalized end to end.

---

## Issue 2 — Cross-service env model

### Status: CONFIRMED-FIXED (one minor vocabulary-precision nit)

**Sweep results (deliverable surfaces):**
```
envIsolation / isolation mode / legacy mode  → ZERO
cross-service auto-inject claim               → ZERO
auto-inherit hits                             → all project-level (correct)
```

**The project-level / cross-service split is taught correctly** — the exact
both-halves-precise model R49-I2 demanded.
[`apidev/README.md:277`](docs/zcprecipator3/runs/51/apidev/README.md#L277):

> *"project-scope constants (here `JWT_SECRET`, `FRONTEND_URL`,
> `DEV_FRONTEND_URL`) auto-inherit into every container… Leave project vars
> out of `run.envVariables`; only cross-service references (`${db_*}`,
> `${broker_*}`, …) belong there."*

[`appdev/README.md:105`](docs/zcprecipator3/runs/51/appdev/README.md#L105) reinforces it for the build phase: a project
variable auto-inherits into the build container; cross-service refs resolve
at the referenced container's start. The worker README IG §3 leads with the
isolated model: *"A cross-service value reaches the worker process only when
you alias it under your own key… Without the alias the broker host, port,
and password are simply absent from the environment."* This is precisely the
"declaration is the only way" framing R49-I2 asked for.

### The cross-service "self-shadow" prose — outcome-correct per the engine

[`apidev/zerops.yaml:65-70`](docs/zcprecipator3/runs/51/apidev/zerops.yaml#L65-L70) (echoed at README:88-93) carries:

> *"The own-key name must differ from the platform side; reusing the same
> name self-shadows to the literal token."*

Run-50's surfaces had **no** cross-service self-shadow claim, so this is new
prose. But it is **not** a re-introduction of the R49-I2 auto-inject error.
The engine's own `internal/ops/env_shadow.go:15-22` (`DetectSelfShadows`) is
authoritative:

> *"PROJECT-level vars auto-inherit… a same-key declaration in
> run.envVariables produces the literal-string shadow… CROSS-SERVICE vars do
> not auto-inject under the porter-default isolation — a same-key
> declaration is technically not a 'shadow' (there is no auto-injected value
> to shadow) but is still invalid because the right-hand-side template
> cannot resolve to anything useful. Either way the value becomes a literal
> at runtime; flag both shapes uniformly."*

So the prose's **outcome** claim ("reusing the same name → literal token at
runtime") is platform-true, and the operational guidance ("use a distinct
own-key name") is exactly what the engine enforces. The only imprecision is
calling the cross-service case a "shadow," which the engine reserves for the
project-level case — but since the engine flags both shapes uniformly and the
runtime result is identical, this is a defensible teaching, not a factual
error. The project-level self-shadow bullet (`apidev/README.md:277`) is
fully precise ("the interpolator does not recurse back to project scope").

**Adversarial verification (workflow) — refutation FAILED, verdict holds.**
An agent tasked to refute this found: the prose's outcome assertion
("self-shadows to the literal token") matches the engine verbatim ("Either
way the value becomes a literal at runtime"); `auto-inject` appears **zero**
times; the only `auto-inherit` use is correctly scoped to project vars
(matching `env_shadow.go:15-16`); and the prose never claims an injected
value is being overridden — which is what would make it the R49 auto-inject
error. The single nit (cross-service case is "technically not a shadow") is
one the engine itself flattens by naming the detector `DetectSelfShadows` and
directing to "flag both shapes uniformly." Not a recurrence of R49-I2.

---

## Issue 3 — `zsc noop --silent` retirement (the one Axis-A regression)

### Status: CONTAINED REGRESSION — root cause is an INCOMPLETE R49-I3 purge

This is the single Axis-A correctness blemish, and it is the issue the owner
explicitly cared most about retiring ("would be nice to not propagate it
further"). **My first-pass acceptance of the TIMELINE's "inherited from the
deployment-verified parent `nestjs-minimal`" framing was wrong** — workflow
transcript-level verification (and direct corpus grep) overturned it. This is
the run-50 codex-pattern repeating: an accepted framing corrected on
independent read.

**The regression is real and ships on TWO porter-facing surfaces** (my draft
undercounted at one). [`workerdev/zerops.yaml:81-84`](docs/zcprecipator3/runs/51/workerdev/zerops.yaml#L81-L84) AND
[`workerdev/README.md:104-107`](docs/zcprecipator3/runs/51/workerdev/README.md#L104-L107) (the IG-embedded yaml block) both ship:

```
# No-op placeholder so the dev container comes up idle; you start
# the watcher yourself over SSH, which is what lets edits rebuild
# without a redeploy.
start: zsc noop --silent
```

It is also a **regression from run-50's CONFIRMED-FIXED clean state** (run-50
had zero `zsc noop` strings in any shipped surface).

**But it is contained — the corpus briefs and 2 of 3 dev runtimes are
correct.** The api and app dev setups omit `run.start` with narrated intent:
- [`apidev/zerops.yaml:176-180`](docs/zcprecipator3/runs/51/apidev/zerops.yaml#L176-L180): *"No run.start, no healthCheck on purpose:
  with no process pinned, the container stays up as a workspace."*
- [`appdev/zerops.yaml:29-30`](docs/zcprecipator3/runs/51/appdev/zerops.yaml#L29-L30): *"No 'start': the Vite dev server is a
  long-running process you own."*

### The real root cause: R49-I3's purge missed the `zerops_knowledge` corpus

R49-I3 retired `zsc noop` from the recipe-engine **briefs**
(`internal/recipe/content/briefs/`, 5 files teaching "omit `run.start`; `zsc
noop` is now stale" — confirmed; the codebase-content brief
[`part-7-rules-from-goldens.md:16`](docs/zcprecipator3/runs/51/environments/.briefs/codebase-content-api/part-7-rules-from-goldens.md#L16) says it verbatim). But it
**left `zsc noop --silent` in 43 files of `internal/knowledge/recipes/`** —
the `zerops_knowledge`-served corpus that scaffold subagents query directly,
including the canonical [`recipe.md`], the generic
[`nodejs-hello-world.md:137`] (`start: zsc noop --silent`), AND the parent
[`nestjs-minimal.md`].

What is robustly established (and what is not):
- The token is endemic across the corpus — it ships in **both** the parent
  [`nestjs-minimal.md:145`](internal/knowledge/recipes/nestjs-minimal.md#L145)
  AND the generic [`nodejs-hello-world.md:137`](internal/knowledge/recipes/nodejs-hello-world.md#L137),
  plus 41 other `internal/knowledge/recipes/` files. The scaffold-worker
  subagent transcript shows it querying `zerops_knowledge` for the nodejs
  runtime (which surfaces the nodejs template), and the main agent read
  `nestjs-minimal` in research — so **the precise single source is not
  isolable, and it does not matter** (codex correctly flagged my first-draft
  "from nodejs-template, NOT parent" as over-claimed). The TIMELINE's
  "inherited from parent" is therefore *incomplete, not wrong* — the parent is
  one of 43 corpus files still carrying the token.
- The recipe **brief told the subagent to omit `run.start` FIVE times** (the
  brief contains neither "noop" nor "zsc"), and the subagent **paraphrased
  that correctly in its own pre-authoring plan** ("dev setup: `run.start`
  omitted") — then **wrote `start: zsc noop --silent` anyway**, a
  self-contradiction within one reason→author step.

The root cause that survives all of this — and the one that matters for the
fix — is that `zsc noop` is endemic across 43 `internal/knowledge/recipes/`
files that R49-I3 never scrubbed (it only purged the briefs). Whichever of
the 43 the subagent drew from, the fix is the same corpus scrub.

### Refinement detected it, ATTEMPTED the fix, and was correctly engine-blocked

This is stronger than a pre-emptive hold. Refinement-1
(`agent-a6f3085b8beb6f046`) flagged it as "FINDING 1 — V6/Y7 HARD violation,"
**attempted** a `record-fragment mode=replace` dropping the `start:` line, and
the engine **refused** (`ok:false`): *"structural changes to yaml refused —
agent owns comments, not the yaml shape."* This is the engine-enforced
ownership boundary in [`internal/recipe/slot_shape.go:349-377`](internal/recipe/slot_shape.go#L349-L377)
(`checkZeropsYamlStructurePreserved`, pinned by `slot_shape_test.go:241`).
Refinement-1 respected its 1-attempt cap and escalated: *"route to a phase
with yaml-shape authority."* Refinement-2 independently re-judged it (S7
test, severity advisory) and also flagged the fact-vs-output drift
(`facts.jsonl:1,18` narrate the slot as *"run.start omitted (zsc noop)"* —
internally contradictory). Both refinement agents are diagnosis-only, so the
item was correctly escalated, not silently shipped or wrongly edited. **The
slot_shape gate working as designed is a positive substrate signal** — it
prevented refinement from making an out-of-scope structural edit; the gap is
that no post-scaffold phase HAS yaml-shape authority to fix it.

**No remediation path existed in the pipeline (codex).** The token ships on
*both* worker surfaces — `zerops.yaml:84` and `README.md:107` (the
IG-embedded yaml block). The cold fix agents were dispatched for *citations*
and did not touch it; refinement *cannot* (yaml-shape gate); scaffold has no
lint for it. So once the scaffold subagent wrote it, nothing downstream could
remove it — it was structurally guaranteed to ship. (This also tightens the
"contained" framing: within the worker codebase it is not one stray line but
the dev-runtime story regressed on **both** its surfaces at once — codex's
point that this is a different quality signal than a single contained line.
It stays *contained to the worker* — api + app are clean — but it is not
"minor.")

**Classification: (a) substrate bug (primary) + (b) 4.8 authoring drift.**
Highest-value v9.103.x followup, now precisely scoped:
1. **Scrub `zsc noop --silent` from all 43 `internal/knowledge/recipes/`
   files** — this is the corpus scaffold subagents actually read, and the
   layer R49-I3's purge missed. Without this, every dynamic-runtime scaffold
   re-inherits the stale token regardless of how many times the brief says
   omit.
2. **Add a scaffold-phase lint** that rejects `zsc noop --silent` (or flags
   dev-runtime `run.start` divergence when sibling setups omit it), so the
   4.8 self-contradiction is caught at authoring time, not just diagnosed
   un-fixably in refinement.

---

## Issue 4 — execOnce burn-recovery scoping

### Status: CONFIRMED-FIXED and improved at surface; superseded framing stranded at facts

**The deliverable resolves Issue 4 the gold way** — split by key-shape
semantics, exactly the R49-I4 prescription.
[`apidev/zerops.yaml:44-55`](docs/zcprecipator3/runs/51/apidev/zerops.yaml#L44-L55):

```
# migrate uses a per-deploy ${appVersionId} key — applying the
# schema is idempotent, so re-running it every deploy is safe.
# seed uses a static key so it runs exactly once per service
# lifetime: a fresh deploy lands a populated dashboard, later
# redeploys skip it. Bump the suffix to .v2 to deliberately
# re-seed after you change the demo data.
initCommands:
  - zsc execOnce ${appVersionId}-migrate --retryUntilSuccessful -- node dist/scripts/migrate.js
  - zsc execOnce nestjs-showcase.seed.v1 --retryUntilSuccessful -- node dist/scripts/seed.js
```

- ✓ migrate = per-deploy `${appVersionId}` key for the idempotent schema sync.
- ✓ seed = STATIC key (`nestjs-showcase.seed.v1`) for the run-once-per-lifetime
  seed, with the `.v1→.v2` rotation lever — the burn-recovery/rotation story
  applied to the *static* key where it is correct.
- ✓ Zero "burn"/"burned"/"consumed key" vocabulary anywhere in the deliverable.
- ✓ The KB section [`apidev/README.md:267-269`](docs/zcprecipator3/runs/51/apidev/README.md#L267-L269) ("Re-firing the seed
  after its data changes") is a textbook PASS exemplar — it teaches the
  static-vs-per-deploy distinction affirmatively and cites the per-deploy
  initCommands reference.

This is **better than run-50**, where both keys were per-deploy
(`${appVersionId}-migrate` / `${appVersionId}-seed`) and the fix relied on a
synthesis-layer filter stripping burn-vocab. Run-51 changes the *key shape*
itself, so the static-key rotation story is now mechanically correct.

**Stranded at the facts layer.** [`facts.jsonl:28-29`](docs/zcprecipator3/runs/51/environments/facts.jsonl#L28-L29) (recorded 08:07,
scaffold-era) still describe both keys as per-deploy and carry the conflation:

> *"Splitting them also means a failed seed does not burn the migrate key, so
> the next deploy retries seed while already-applied schema is skipped."*

This is the R49-I4 per-deploy + cross-deploy-burn conflation — but it
describes the **superseded** scaffold plan (both per-deploy); the feature
pass later changed seed to a static key, and the deliverable shipped clean.
The early scaffold facts were never retracted when the key shape changed.
This is facts-layer staleness, not a deliverable defect — and it is *less*
severe than run-50's because the surface is actively correct rather than
filter-corrected.

---

## Surface-5 dual-shape contract — CONFIRMED STRUCTURAL

Run-50 flagged the dual-shape as a single data point (workerdev `### Scaling`
H3 + `### Gotchas`) that "future runs need to be monitored for whether shape
(1) adoption matches content topics or defaults back to single-shape
uniformity." Run-51 answers it: **the dual-shape is structural, not a
one-off — it recurred at a different codebase, content-driven, and clean.**

Per the spec ([`docs/spec-content-surfaces.md:195-220`](docs/spec-content-surfaces.md#L195-L220)), shape (1) is
"one or more `### <Topic>` H3 headers with prose" — `### Scaling` was run-50's
*instance*, not a mandated heading. Run-51's manifestation:

| Codebase | Shape (1): forward-looking topic H3s | Shape (2): `### Gotchas` bullets | Adoption |
|---|---|---|---|
| **apidev** | `### Object Storage runs on a MinIO backend`, `### Search key rotation`, `### Re-firing the seed after its data changes` | 3 bullets | **DUAL-SHAPE** (richer than run-50) |
| workerdev | — | 4 bullets | shape (2) only |
| appdev | — | 2 bullets | shape (2) only |

- apidev's three forward-looking H3s sit inside the `knowledge-base` KB
  markers, each citing its guide by descriptive link text (MinIO/object
  storage, Meilisearch, per-deploy initCommands) — satisfying both the
  Surface-5 citation rule (spec:246) and slug hygiene (no bare slugs).
- The dual-shape migrated from run-50's workerdev (where its Scaling-H3
  carried the Issue-1 "headroom" residual) to run-51's apidev, and is now
  **clean of any Issue-1 conflation** — the structural fix held without
  re-importing the defect.
- ItemCap retired: 3/4/2 bullets across codebases, content-driven salience.

---

## `kb-self-inflicted-reversible` gate — armed, nothing fired

Same outcome as run-50. The violation code `kb-self-inflicted-reversible`
fired 0× in `main-session.jsonl`. The 8 "self-inflicted" string hits are all
guidance/litmus vocabulary (`self-inflicted-as-gotcha`,
`self-inflicted-reversible litmus`) teaching sub-agents NOT to author
self-inflicted-reversible patterns as KB bullets. The preventive effect is
holding two runs in a row: sub-agents author correctly from the start, so the
runtime gate has nothing to refuse.

---

## Flow section (Axis B)

Ground truth from `main-session.jsonl` (389 lines, 38 `zerops_recipe` calls)
and 18 subagent transcripts.

### The "36 subagents" alarm is a miscount — there are 18

The brief flagged "36 subagent dispatches… potentially 2× the expected
count." The directory holds **18 `.meta.json` + 18 `.jsonl` = 18 unique
subagents** (36 = the meta+transcript file pairs). Tally by phase:

| Phase | Subagents | Canonical? |
|---|---|---|
| research / provision | 0 (inline by main agent) | ✓ |
| scaffold | 3 (api, app, worker) | ✓ |
| feature | 2 (backend over api+worker, frontend over app) | ✓ |
| codebase-content | 6 (3 codebase-content + 3 claudemd-author) | ✓ |
| codebase-content **fixes** | 3 (fix-codebase-content-{api,app,worker}) | +3 (citation gate) |
| env-content | 1 | ✓ |
| refinement | 3 (refinement-1, refinement-2, apply-refinement-acts) | ✓ |
| **total** | **18** | 15 canonical + 3 fix |

All 18 are `agentType: general-purpose` — **none** used `claude` (which would
break shared MCP state per the codebase-content phase doc). The 3 "extra" fix
subagents are legitimately triggered (api + worker by the citation gate, app
by `yaml-comment-missing-causal-word` quality notices — see the dispatch
audit below), not off-piste improvisation. 18 is within an order of magnitude
of the 12–15 target and fully accounted for.

### Tool-call error inventory — exactly one in-envelope rejection

`isError:true` at the transport level: **0**. In-envelope `ok:false`: **1**.

| Line | Action | Outcome | Classification |
|---|---|---|---|
| L224 | `complete-phase codebase-content` (1st) | **REJECTED** — `violations: kb-citation-display-mismatch` ("display text 'zero-downtime rolling deploy' does not match friendly name 'zero-downtime deploys with multi-container setups' for guide 'rolling-deploys'") + `kb-citation-missing` (7 hard citation violations total) | (c) acceptable — engine gate working; walked correctly |
| L249 | `complete-phase codebase-content` (2nd) | PASS (after 3 fix subagents) | (c) recovered cleanly |

The v9.102.0 hardening failure modes the brief asked me to watch for **did
not occur**:
- `slug is required` → **0 hits** (the 4.8 slug-drop pattern did not recur).
- `outputRoot must nest under…` → **0 hits** (canonical-base guard untripped;
  `outputRoot` appears 67× only as a normal response field).
- `parentStatus absent` → **0 hits** (the deferred prefixed-slug issue did
  not surface; `parentStatus` appears 5× only as a normal envelope field).
- `zerops-`-prefixed slug mismatch → **0 hits**.

A separate Low-severity schema miss happened *inside* the scaffold subagents
([`TIMELINE.md:129`](docs/zcprecipator3/runs/51/TIMELINE.md#L129)): the live schema validator rejected
`verticalAutoscaling` under the `run` block; the subagent removed it. The
final deliverable places `verticalAutoscaling` correctly (under the service
block in import.yaml), and my independent structural validation confirms all
9 YAMLs are structurally valid (see Axis A1 for the bare-form/BC nuance). The
live validator catching the `run`-block placement is the substrate working.

### Phase-gate walk audit — the FIRST-CAUSE cascade (this is why it didn't walk smoothly)

The user is right that the process did **not** walk smoothly and ended in a
hard workaround. My earlier "mostly clean" framing was too generous. The
honest account is a cascade with a single first cause — and that first cause
is the **engine's own guidance**, not 4.8 eagerness. Here is the chain, fully
grounded in the main-session log:

**0. First cause — the `complete-phase feature` guidance told the agent to
dispatch the next phase's workers WITHOUT first entering that phase.** The
phase pointer does not auto-advance (`TIMELINE.md:10-13`): every phase must be
`enter-phase`d before its work, because `enter-phase` mints the fact shells
and retires the prior phase's gate. But the next-call guidance returned by
`complete-phase feature` (main-session **L178**) reads:

> *"**Next call:** for each codebase, `build-subagent-prompt … briefKind=codebase-content` AND `briefKind=claudemd-author`; **dispatch all briefs in parallel via `Agent`, then `complete-phase … codebase-content` → `enter-phase …`**"*

It **leads with build-subagent-prompt + dispatch** and never sequences
`enter-phase codebase-content` *before* the dispatch (the only `enter-phase`
in the sentence is the tail-end one for the phase *after* codebase-content).
And `build-subagent-prompt` **returns briefs even when the phase isn't
entered** — no precondition check — so the agent got zero error signal. The
agent followed the guidance literally (L180: *"Now codebase-content… That's 6
sub-agents. Let me compose all six briefs."*) and dispatched.

**1. 6 workers ran in the wrong phase.** Dispatched at L183–204 while the
pointer was still `current: feature`. The workers correctly detected it —
L211: *"this resolves only when the main agent runs `complete-phase
feature` → `enter-phase codebase-content`… before content sub-agents
validate. I did not invoke those."* L214 even notes the worker left its
`zerops.yaml` *"in the correct bare state… so the engine stitches cleanly
once"* the phase is entered. (Smart, in-bounds behavior — they diagnosed the
engine's ordering bug for the main agent.)

**2. Late enter-phase + masked citation defects surface.** The agent
self-diagnosed (L217), ran `enter-phase codebase-content` (L218), re-stitched,
and `complete-phase` (L224) then surfaced **7 citation violations**. Those are
independent CONTENT defects (mismatched display text, missing guide
citations) the workers authored regardless of ordering — but the
dispatch-before-enter ordering had **masked** them: before `enter-phase`, the
leftover scaffold bare-yaml gate fired on the stitched comments, so the
workers' self-validate never saw their own citation defects. (This corrects
my first draft, which wrongly called the ordering the *cause* of the
violations; it is the *masker*.)

**3. The hard workaround.** The agent's correct instinct was to **resume the
warm authoring workers** to fix their own citations (L235: *"…they have full
context… I'll resume each to fix its own surfaces. Let me load SendMessage."*).
But **`SendMessage` is unavailable in the recipe-authoring environment**
(L236), so it was forced to spin up **3 fresh cold fix agents** — re-reading
each stitched file from scratch. That is the "hard workaround," and the
"extra subagents" the user flagged.

**Net classification: a (a) substrate cascade, substrate-leading but with
shared agent fault** (codex haircut, accepted). The engine (i) sequenced
dispatch before `enter-phase`, (ii) let `build-subagent-prompt` succeed with
no enter-phase precondition, (iii) masked the citation defects behind the
bare-yaml gate so the workers couldn't self-correct, and (iv) gave no resume
path (`SendMessage` absent) to repair via the warm authors. Each is an
independent substrate fix (below). **But the agent is not blameless:** the
**provision re-sync** earlier in the same run (L100 `complete` before L104
`enter` → L110 `complete`) had *already* taught it that `complete-phase` does
not auto-advance and that phases need an explicit `enter-phase`. By
codebase-content it had that signal and could have entered-first regardless of
what the guidance led with. So the honest split is: the engine guidance + the
missing precondition are the proximate trigger and the systematic defect (they
will mis-lead every agent), but a maximally-careful agent, already bitten once
this run, should have caught it — shared fault, substrate-leading.

No `complete-phase` was skipped and no phase was silently bypassed — but
"walked smoothly" is the wrong description. It was a guidance-induced
misordering that the agent had to detect, re-sync, and then repair through a
cold-agent workaround the harness forced on it.

### Subagent dispatch audit — and the REAL cause of the "extra subagents"

All 18 subagents are `general-purpose` (none `claude`); all trace to a
`build-subagent-prompt` brief. The fix subagents each re-recorded **only
their own codebase fragments** (no sibling-codebase edits) and exited without
thrash. One correction to my draft: the 3 fix subagents were **not** all
citation-triggered — **api and worker** were (the `kb-citation-*` gate;
worker's `complete-phase` returned the diagnostic rejection with
display-mismatch + 2 missing-citation codes), but **app** was triggered by
`yaml-comment-missing-causal-word` *quality notices only* (its brief states
"the phase gate reports only QUALITY NOTICES (no hard violations)"). The
worker fix subagent logged 7 tool errors, but workflow read confirms **none
are thrash**: one self-corrected param-shape error and five `fill-fact-slot`
"no fact with topic" probes — disciplined one-shot verification that
brief-named "if present" shells don't exist (the engine never minted them),
not retried failures. Both api and worker showed in-bounds rule reasoning
(e.g. correctly distinguishing the forbidden slug-stem `init-commands` from
the blessed field-name `initCommands`). No off-piste dispatches.

**The 3 fix subagents existed at all because `SendMessage` is unavailable in
the recipe-authoring environment — a substrate/harness gap, and the direct
cause of the "extra subagents" the user flagged.** When the citation gate
rejected at L224, the agent's *first* and correct instinct was to **resume
the warm authoring sub-agents** to fix their own surfaces — they hold full
context, the citations are their own output, and resume-in-place would have
added zero new dispatches. Main-session **L235→L236** records exactly this:

> *"…they have full context and the citation rules in their briefs, so I'll
> resume each to fix its own surfaces… **Let me load SendMessage.**"*
> *"**I can't resume the terminated sub-agents — SendMessage isn't available
> in this environment.** So I'll dispatch fresh, narrowly-scoped fix agents
> (one per affected codebase, in parallel)…"*

So the "extra" dispatches are not eager improvisation — they are the agent's
*fallback* after the efficient path (resume) was blocked by the harness. The
cost of the gap compounds three ways: (i) 3 fresh full dispatches instead of
3 resumptions (the agent-count inflation), (ii) lost warm authoring context
(each fix agent must re-read the on-disk stitched README/yaml from cold), and
(iii) the re-derivation risk of authoring-context loss the engine elsewhere
works hard to avoid (the codebase-content brief explicitly forbids
paraphrase wrappers for exactly this reason). Classification: **(a)
substrate/harness gap.** Fix: make `SendMessage` available to the
recipe-authoring main agent (or give the engine a first-class
"re-dispatch-with-warm-context fix pass" for gate rejections) so a
citation-gate rejection is repaired by the original author, not a cold fresh
agent.

---

## Cross-cutting observations

1. **Run-51 closes run-50's #1 cross-cutting concern.** Run-50 noted the
   corrected Issue-1 framing landed at brief level but not in sub-agent
   priors / facts. `facts.jsonl:85` now articulates the two-orthogonal-axes
   model verbatim. The priors caught up.

2. **R49-I3's "purge" was incomplete — the knowledge corpus was never
   scrubbed.** The retirement landed in the recipe-engine *briefs*
   (`internal/recipe/content/briefs/`, which correctly teach "omit
   `run.start`"), but `zsc noop` still ships in **43 files** of
   `internal/knowledge/recipes/` — including the canonical `recipe.md`, the
   generic `nodejs-hello-world.md`, and the parent `nestjs-minimal.md`. That
   corpus is what `zerops_knowledge` serves to scaffold subagents, so the
   stale token is one query away on every dynamic-runtime scaffold no matter
   how emphatically the brief says omit. This is the single highest-leverage
   substrate followup, and it generalizes beyond this recipe: any showcase
   fork re-inherits it. (See Issue 3.)

3. **The remaining facts-layer strands are about superseded plans, not wrong
   teaching.** Issue-4's `facts.jsonl:28-29` describe the scaffold-era
   both-per-deploy plan that the feature pass replaced with a static seed
   key; Issue-3's `facts.jsonl:1,18` muddle "omitted (zsc noop)." Both are
   stale-relative-to-deliverable, not defects shipped to porters. The
   pattern to watch: facts recorded early in a phase are not retracted when a
   later phase changes the decision.

4. **The engine's gate suite is comprehensive and exercised.** Violation
   codes that fired across the run: `yaml-comment-missing-causal-word` (the
   "why"-framing enforcer that makes the comments so consistent),
   `kb-citation-missing` / `kb-citation-display-mismatch` (citation hygiene),
   `meta-agent-voice` (anti-authoring-vocab — with the documented tier-0 "AI
   Agent" false positive at TIMELINE.md:138), `cross-surface-duplication`,
   `cross-codebase-env-coherence` (which caught the real `NATS_PASSWORD` vs
   `NATS_PASS` drift). The agent walked the hard ones and held/accepted the
   advisory ones.

5. **Voice (A3) is strong and gate-enforced.** Comments are period-terminated
   with "why" framing; em-dashes are appositive, not two-thought welds.
   Tier-internal coherence holds (api/app/worker prose share tone within a
   tier); cross-tier, tier-4 and tier-5 read as deltas (tier-5 adds dedicated
   CPU / HA / SERIOUS / 1 GB floor over tier-4's shape). The
   `yaml-comment-missing-causal-word` gate is the structural reason the voice
   is consistent.

6. **Subdomain handling is consistent with the run-50 baseline.** Production
   tiers 4/5 set `enableSubdomainAccess: true` on api+app (worker correctly
   omits it), matching run-50 and the `*.zerops.app` API_URL templates with
   prose to migrate to a custom domain. This is recipe-tier convention, not a
   P-PROD-2 violation (P-PROD-2 governs the launch-production *workflow*
   composer, a different scope).

---

## Codex validation

Codex (re-authenticated, agent `a3b6de2ec3beca00e`) independently verified
all findings against primary sources, with explicit scrutiny on root causes
and the big picture. It **concurred with ITERATE-TO-52** and reproduced the
run-50 codex value — it corrected the report rather than rubber-stamping it.
Two earlier independent passes (a 6-agent workflow + a fresh verifier) had
already corrected the first-draft claims below; codex then corrected *those*
corrections in two places. Net codex dispositions:

| Claim | Codex verdict | Resulting change |
|---|---|---|
| RC1 — flow first-cause is the engine guidance (dispatch sequenced before `enter-phase`; no precondition; gate masking; `SendMessage` absent) | **CONFIRM (a)–(d) line-by-line**, but **REFINE the attribution** — the agent had already learned from the provision re-sync that `complete-phase` doesn't auto-advance, so "primarily substrate, not 4.8" needs a haircut; responsibility is shared, substrate-leading | Reframed RC1 to shared/substrate-leading |
| RC2 — Issue 3 root cause is the 43-file `internal/knowledge/recipes/` purge gap | **CONFIRM the 43-file purge**; **REFUTE the "pulled from nodejs template, NOT parent" specificity** — `zsc noop` is in BOTH `nestjs-minimal.md:145` AND `nodejs-hello-world.md:137`; the precise source isn't isolable and the TIMELINE's "inherited from parent" is *incomplete, not wrong* | Softened provenance; kept 43-file purge as the robust root cause |
| RC3 — bare forms are API-accepted BC per `type_equivalence.go` + bundled superset schema | **CONFIRM** (verified `type_equivalence.go:8-12` + both forms in the bundled schema). The "published schema is composite-only" sub-claim it marked **[unverified — API host unreachable from its sandbox]**; my own fresh `curl` fetch confirms it (composite-only enum, no bare `nodejs@22`) | Noted the published-schema evidence is mine, not codex-reproducible |
| Issue 3 two-surface count + the README:107 leak **survived the cold fix-agent pass** | **CONFIRM + adds**: the cold fix agents (dispatched for citations) did not clean README:107, and refinement can't (yaml-shape) — so the worker zsc noop had **no remediation path** in the pipeline | Strengthened Issue 3 |
| Issues 1, 2, 4 fixed/improved; Surface-5 structural; Issue-1 model in facts.jsonl:85; refinement detected+engine-blocked+escalated | **CONFIRM** against primary sources | Stand |
| Big picture | **Verdict ITERATE-TO-52 correct, but report is somewhat too generous to Axis-A** (workerdev regressed on *both* its surfaces simultaneously — a different signal than "one contained line"), and the dominant systematic risk is now **process ergonomics**, not content — it deserves its own roadmap RC, not a flow-section note | Added the big-picture RC + tightened the worker framing |

Codex one-line verdict, verbatim: *"ITERATE-TO-52 is correct, but the
workerdev regression is two surfaces not one, the substrate ergonomics failure
is the primary systemic risk now, and the 'primarily substrate not agent' RC1
attribution needs a haircut — the agent had the information to enter-phase
first."* All three are folded in below.

---

## Big picture — the failure surface has shifted from CONTENT to PROCESS ERGONOMICS

This is the most important cross-run pattern, and codex pushed it from a
flow-section footnote to a first-class finding. Across the sequence:

- **run-49** failed on **content** — four pieces of stale platform knowledge
  (rolling-deploy/maxRam, cross-service auto-inject, `zsc noop`, execOnce
  burn) baked into the corpus.
- **run-50** was **content** again — three Issue-1 residuals left it
  ITERATE-TO-51.
- **run-51's content is the strongest yet.** Issues 1/2/4 are fixed (1 and 4
  improved), the corrected models reached the facts layer, Surface-5 is
  structural, schema is structurally valid. The *only* content residual is
  one contained (if two-surface) `zsc noop` regression — itself a corpus
  hygiene gap (incomplete purge), not a reasoning error.
- **What actually made run-51 rough was the engine's PROCESS ERGONOMICS:**
  (1) phase-walk guidance that sequences dispatch before `enter-phase`;
  (2) no `enter-phase` precondition on `build-subagent-prompt`/dispatch;
  (3) the bare-yaml gate *masking* citation defects so authors can't
  self-correct; (4) `SendMessage` absent, so a gate rejection can't be
  repaired by the warm author and forces cold fix agents; (5) the
  comments-only refinement boundary (correct) with **no** post-scaffold phase
  holding yaml-shape authority, so a stale token has no removal path; (6) the
  manual no-auto-advance phase pointer (provision re-sync).

**The implication for the roadmap:** content-quality work has largely paid
off; the dominant, *recurring-every-run* risk is now the authoring engine's
own ergonomics. These are not stochastic — they are structural and will
re-fire on the next recipe regardless of model or framework. Followups #1–#3
and #5 below target this surface, and it deserves to be tracked as its own
workstream, not as per-run flow notes. The good news mirrors it: because
these are structural, fixing them once fixes them for every recipe.

---

## Verdict — ITERATE-TO-52 (content strong; process ergonomics is the gating risk)

Run-51 is the best run in the 49→50→51 sequence. The substrate-fix loop that
run-50 left half-open closed cleanly:
- Issues 1, 2, 4 are CONFIRMED-FIXED, with 1 and 4 *improved* over run-50.
- Issue 1's three run-50 residuals are gone, the missing affirmative teaching
  is present, and the corrected model reached the facts layer.
- The Surface-5 dual-shape is confirmed structural (recurred clean at a new
  codebase).
- All 9 YAMLs are structurally valid; voice is strong and gate-enforced;
  slug hygiene is clean. (The bare-form type/base tokens diverge from the
  current published schema but are API-accepted BC — a corpus-wide
  modernization question, not a run-51 defect; see Axis A1.)
- The v9.102.0 dispatch hardening held (no slug-drop, outputRoot, or
  parentStatus failures).

But the **flow did not walk smoothly** — and that is the second reason it is
not ship-as-canonical. A first-cause guidance bug (the `complete-phase
feature` next-call sequenced dispatch before `enter-phase codebase-content`)
sent 6 workers into a not-yet-entered phase, the bare-yaml gate then masked 7
citation defects, and the natural repair (resume the warm authors) was
impossible because `SendMessage` is absent — forcing 3 cold fix agents. The
Axis-A *content* came out strong, but the *process* took a substrate-induced
detour and a hard workaround to get there (Axis B).

It is **not** ship-as-canonical for two reasons. (1) `workerdev/zerops.yaml:84`
(and `README.md:107`) still ship `start: zsc noop --silent`, the exact pattern
the owner asked to retire; shipping as canonical would propagate it. This is a
*contained, detected, root-caused* regression — 1 of 3 dev runtimes, the
briefs all still teach retirement, and refinement detected it but was
correctly engine-blocked from the structural edit. (2) The flow cascade above
is a real substrate-ergonomics defect that will recur on every run until the
phase-walk guidance + `enter-phase` precondition + `SendMessage` gaps are
closed.

Recommended actions, in priority order (the flow first-cause leads because it
is systematic — it recurs on every run, not just this recipe):

1. **Fix the phase-walk guidance + add an `enter-phase` precondition (the
   flow first-cause).** The `complete-phase feature` next-call guidance (L178)
   must sequence `enter-phase codebase-content` *before* the
   `build-subagent-prompt`/dispatch step, and `build-subagent-prompt` +
   `Agent` dispatch for a phase should **refuse (or auto-enter)** until that
   phase is active. Today the guidance leads with dispatch and the engine
   enforces no precondition, so an agent that follows the guidance literally
   sends workers into a not-yet-entered phase — the root of the whole
   codebase-content cascade.

2. **Make `SendMessage` available to the recipe main agent** (or give the
   engine a first-class "re-dispatch-with-warm-context fix pass" for gate
   rejections). This is what forced the cold 3-fix-agent hard workaround: the
   agent wanted to resume the warm authors and could not (L236). With resume,
   a gate rejection is repaired by the original author at near-zero extra
   cost; without it, every gate rejection spawns cold fix agents.

3. **Run the citation gate independently of the bare-yaml gate.** Before
   `enter-phase`, the scaffold bare-yaml gate masks citation defects so
   sub-agents can't self-correct them during their own validate. Decoupling
   the gates lets authors catch their citation mistakes before termination —
   which, combined with #2, would have avoided the fix pass entirely.

4. **Scrub `zsc noop --silent` from the `internal/knowledge/recipes/` corpus
   (Issue 3 — the one Axis-A correctness item the owner flagged).** 43 files
   (incl. `recipe.md`, `nodejs-hello-world.md`, `nestjs-minimal.md`) still
   ship it; that is the corpus `zerops_knowledge` serves scaffold subagents,
   so R49-I3's brief-only purge never reached the layer that feeds authoring.
   Pair with a scaffold-phase lint rejecting `zsc noop` / dev-runtime
   `run.start` divergence, since nothing catches it until refinement (which
   correctly cannot make the yaml-shape edit).

5. **Smooth the phase-pointer ergonomics broadly.** Beyond #1, the provision
   re-sync (L100 `complete` before L104 `enter` → L110 `complete`) shows the
   same no-auto-advance friction. Either auto-advance on `complete-phase`, or
   return a structured `enter-phase`-first recovery hint instead of requiring
   the manual dance.

6. **Retract superseded facts when a later phase changes a decision.**
   Issue-4 `facts.jsonl:28-29` (scaffold-era both-per-deploy + burn framing)
   and Issue-3 `facts.jsonl:1,18` ("omitted (zsc noop)") describe plans the
   deliverable superseded. Lower priority — the surface is clean — but the
   facts trail should not contradict the shipped yaml.

7. **Decide the bare-form-vs-composite type/base stance (corpus-wide).** The
   recipe corpus emits bare forms (`nodejs@22`, `postgresql@18`) the current
   *published* schema (post 2026-05-18) no longer lists, though the API
   accepts them as BC (`type_equivalence.go`). Not a run-51 defect — but the
   corpus should migrate to composite forms or explicitly record BC-acceptance
   so "validate against the published schema" stops surfacing as a false
   failure. Affects every recipe.

8. **Re-run as run-52 after #1–#4.** The worker `zsc noop` absence is the
   regression-shape to verify-gone; uniform `run.start` omission across all
   three dev runtimes is the affirmative shape to verify-present; a clean
   codebase-content phase walk (enter-before-dispatch, no fix agents) is the
   flow shape to verify-present.

### What this is NOT

- NOT a stale-binary failure like run-49 — the engine is v9.103.2, *ahead* of
  the expected hardening tag.
- NOT a regression on Issues 1, 2, or 4 — those are fixed (1 and 4 improved).
- NOT a stochastic-drift artifact — the worker `zsc noop` traces precisely to
  an un-scrubbed 43-file knowledge corpus + a 4.8 authoring self-contradiction
  + a missing scaffold gate, not random sub-agent variance.
- NOT a sub-agent-disobedience failure — the agent detected the one residual,
  attempted the fix, was correctly engine-blocked, and escalated; the gates
  fired and were walked.
- NOT the "36 subagents" overrun the brief feared — there are 18, all
  general-purpose, all brief-tied; the 3 extras are accounted-for fix
  subagents.
