# Run-52 validation — first full run on the v9.104.0 five-fix engine

> **Headline: SMOOTH-WITH-NITS.** *(Codex independently verified every claim
> against primary sources and CONCURRED with the substance, correcting the
> report in two places — it haircut my "prod blocks all carry `start`" to
> "process runtimes do; the static frontend prod correctly omits it too," and
> — the one that matters — it REFUSED my "Issue-4 seed key is a design
> divergence, not a regression," ruling it a **mild design regression** vs
> run-51: the per-deploy seed key is duplicate-safe today only by an
> application count-guard, but silently stops inserting new seed rows if the
> seed data ever changes, and run-52 also dropped run-51's "### Re-firing the
> seed" KB teaching. See Codex validation.)*
>
> **The two reasons run-51 was NOT ship-as-canonical are both GONE, verified.**
> The worker `zsc noop --silent` regression is **absent on every porter-facing
> surface**, and `run.start` is **uniformly omitted across all three dev
> runtimes** (api/app/worker). The dispatch-before-enter cascade is **gone by
> construction**: the agent called `enter-phase codebase-content` *before* the
> first `build-subagent-prompt` for that phase (Fix 1 guidance followed), the
> Fix-2 keystone net was **never tripped**, `complete-phase codebase-content`
> **passed on its first call with zero citation violations**, and the run used
> **15 subagents — all canonical, ZERO cold fix agents** (run-51 had 18 = 15 +
> 3 cold fix; delta **−3**). Axis-A content is the strongest in the
> 49→50→51→52 sequence: Issues 1, 2, 3 are CONFIRMED-FIXED, the facts layer is
> clean (run-51's `omitted (zsc noop)` and burn-framing residuals did **not**
> recur), schema is structurally valid.
>
> **Why SMOOTH-WITH-NITS and not SHIP-AS-CANONICAL.** A single soft content
> nit holds it back from being stamped *the canonical pattern*: Issue 4's seed
> key regressed from run-51's static `nestjs-showcase.seed.v1` (run-once per
> service lifetime, with a documented `.v1→.v2` re-seed lever) back to a
> per-deploy `${appVersionId}-seed` key whose only protection against
> re-seeding is a `count() > 0` guard in `seed.ts`. That is duplicate-safe for
> the *current* seed data, but it is strictly weaker than run-51: if a future
> author adds an item to `SEED_ITEMS`, the count-guard blocks the new row from
> ever landing on an existing deployment, and the run-51 KB section that taught
> the porter how to force a re-seed is gone. Shipping this as the canonical
> reference would propagate the weaker seed pattern. This is **not** a
> correctness defect today (the showcase deploys green; TIMELINE verifies "both
> fire (verified idempotent)"), and it is emphatically **not** ITERATE-TO-53 —
> every structural blocker run-51 carried is fixed. It is one revert (static
> seed key + restore the re-seed teaching) away from clean canonical.

---

## Engine-lineage check (REQUIRED-FIRST — HARD BLOCKER) — PASSES, on premise

```
$ jq -r .engineVersion docs/zcprecipator3/runs/52/environments/plan.json
v9.104.0            ← matches the brief premise exactly (no run-51 overshoot)

$ git tag --list 'v9.10[34]*' | sort -V
v9.103.0  v9.103.1  v9.103.2  v9.104.0   ← v9.104.0 exists and is the latest

$ git log -1 --format='%H %s' v9.104.0
7ce8231f  fix(knowledge): exempt dev setups from start-required recipe lint (run-52)

$ git merge-base --is-ancestor <each fix commit> v9.104.0; echo $?  → 0 for all six
60758b3b  Fix 2 (KEYSTONE) — gate build-subagent-prompt on matching enter-phase
60e8e6fe  Fix 1          — sequence enter-phase before dispatch in phase-walk guidance
989faf91  Fix 4          — scaffold lint: dynamic dev runtime must omit run.start
6a80eb43  Fix 3          — run citation validators independent of bare-yaml gate
49c6c809  refactor       — extract scopedGatesForCurrentPhase from completePhase
7ce8231f  Fix 5 backstop — exempt dev setups from start-required recipe lint
```

**Verdict: lineage gate PASSES, and this time on-premise (unlike run-51's
v9.103.2 overshoot).** The plan stamps exactly v9.104.0; all five fix commits
(plus the supporting refactor) are ancestors of the v9.104.0 tag. The fixes
are **present** — this is the inverse of the run-49 stale-binary trap and of
run-51's ahead-of-premise overshoot: the substrate is exactly what the brief
assumed. The fix-regression axis is **live and gradeable**, not moot.

---

## Per-issue verdict

| # | Issue | Verdict | Evidence |
|---|-------|---------|----------|
| **Fix 1** | Phase-entry guidance sequences enter-phase before dispatch | **HELD** | `main-session.jsonl` action order: `complete-phase feature` → **`enter-phase codebase-content`** → 6× `build-subagent-prompt` → 6 `Agent` dispatches → `complete-phase codebase-content` (`ok:true`). Enter precedes first dispatch. |
| **Fix 2** | KEYSTONE — `build-subagent-prompt` refuses on phase mismatch | **HELD (net never tripped)** | Zero `"belongs to phase"` / `"the session is at"` in any tool result. Because Fix-1 guidance was followed, the net had nothing to catch — the correct "clean by construction" shape, not a trip-and-recover. |
| **Fix 3** | Citation validators decoupled from bare-yaml gate | **HELD (not load-bearing this run)** | No out-of-phase self-validate occurred (Fix 2 prevents it), so the defense-in-depth path was never exercised. `complete-phase codebase-content` passed first call with **zero** `kb-citation-*` violations. |
| **Fix 4** | Scaffold lint `dev-runtime-run-start-present` (SeverityBlocking) | **HELD (verify-present, gate silent)** | All 3 dev runtimes omit `run.start`; no stray `start:` was authored, so the gate (`internal/recipe/gate_dev_runtime_no_run_start.go:63`) had nothing to fire. The affirmative shape, not the reproducer. |
| **Fix 5** | Corpus scrub of the two scaffold-read parents | **HELD (clean inheritance)** | Zero `zsc noop` in any deliverable surface; the only corpus-served hits left are the engine briefs, which TEACH retirement. Scaffold inherited a clean parent. |
| **Flow metric** | ~15 canonical, ZERO cold fix agents | **HELD** | 15 `.meta.json` subagents (3 scaffold + 2 features + 3 codebase-content + 3 claudemd-author + 1 env-content + 3 refinement). **0 cold fix agents.** Δ vs run-51: **−3**. |
| **1** | Rolling-deploy mechanism + `maxRam` hallucination | **CONFIRMED-FIXED** | Zero `maxRam` in deliverable. Tier-4/5 `minContainers` comments are capacity+crash framed; `verticalAutoscaling.minRam` is the only adapt-knob. Rolling-deploy mentions are all SIGTERM-drain mechanism, never minContainers-conflation. |
| **2** | Cross-service env auto-inject + same-key self-shadow | **CONFIRMED-FIXED** | Zero `envIsolation`/`auto-inject`. Project vars taught as auto-inherit ([`apidev/zerops.yaml:54-58`](../docs/zcprecipator3/runs/52/apidev/zerops.yaml#L54-L58)); cross-service refs renamed under own keys; self-shadow-to-literal taught correctly. |
| **3** | `zsc noop --silent` retirement (owner's top concern) | **CONFIRMED-FIXED / GONE** | **Zero hits on every porter-facing surface** (apidev/appdev/workerdev/environments). The only corpus hits remaining are the V6 forbidden-token lists in the briefs, which teach its retirement. |
| **4** | execOnce burn-recovery key-shape | **CONFIRMED-FIXED (R49 defect) + MILD DESIGN REGRESSION vs run-51** | Burn vocabulary entirely absent; distinct-key-or-collision pitfall taught correctly. BUT seed reverted from run-51's static key to per-deploy `${appVersionId}-seed` ([`apidev/zerops.yaml:108-109`](../docs/zcprecipator3/runs/52/apidev/zerops.yaml#L108-L109)); duplicate-safe only via `seed.ts:54-58` count-guard, which silently blocks new rows if seed data changes. See Issue 4 below. |
| **S5** | Surface-5 dual-shape | **CONFIRMED STRUCTURAL** | apidev KB ships forward-looking topic H3s (`### Object storage…`, `### Search is single-node…`, `### Custom domain…`) — content-driven, slug-clean. |
| **Facts** | run-51 facts-staleness residuals | **DID NOT RECUR (improved)** | `facts.jsonl` says *"Dev omits run.start entirely"* with no `(zsc noop)` muddle and no burn-framing. The run-51 residual is closed. |
| **Flow (Axis B)** | Smooth walk? | **YES — smooth, by construction** | enter-before-dispatch; zero cascade; zero cold fix agents; zero in-envelope `ok:false`; v9.102.0 hardening held (0 slug-drop/outputRoot/parentStatus). One in-stride param-shape self-correction (feature `pass`). |

---

## Axis 0 — Fix regression (the reason run-52 exists)

This is the headline axis: run-51 was not ship-as-canonical for two reasons —
(1) the worker `zsc noop --silent` regression, (2) the dispatch-before-enter
cascade with 3 forced cold fix agents. v9.104.0 targeted both. **Both are
resolved, verified against `main-session.jsonl` + the deliverable.**

### Fix 1 + Fix 2 — the cascade is gone *by construction*

The exact ordered `zerops_recipe`/`Agent` sequence from `main-session.jsonl`,
codebase-content phase:

```
complete-phase feature
enter-phase   codebase-content     ← ENTER BEFORE DISPATCH (run-51's missing step)
build-subagent-prompt codebase-content   (×3 codebases)
build-subagent-prompt claudemd-author    (×3 codebases)
Agent ×6                                  (dispatched into the ENTERED phase)
complete-phase codebase-content          → ok:true, ZERO citation violations
enter-phase   env-content
```

- **Fix 1 held.** The agent sequenced `enter-phase codebase-content` *before*
  the first `build-subagent-prompt`. The run-51 first-cause (guidance led with
  dispatch, never sequenced enter-phase) is eliminated.
- **Fix 2 (keystone) held — the net was never tripped.** Zero `"belongs to
  phase"` / `"the session is at"` strings in any tool result. This is the
  *correct* outcome of a keystone: because Fix-1 guidance routed the agent
  correctly, the precondition had nothing to refuse. This is "clean by
  construction," **distinct from** a trip-then-recover-in-stride (which the
  brief said would also be GOOD). Neither shape — there was simply no mismatch.
- **The downstream cascade is absent.** `complete-phase codebase-content`
  passed on its **first** call with **zero** `kb-citation-*` violations — no
  rejection, no late re-stitch. In run-51 this call was rejected (7 citation
  defects masked by the bare-yaml gate), forcing a late enter-phase + 3 cold
  fix agents. None of that recurs.
- **Fix 3 (defense-in-depth) was not load-bearing this run** — exactly as the
  fix spec predicted ("not load-bearing once Fix 2 lands"). No content was
  self-validated outside its phase, so the citation-decoupling path was never
  exercised. It remains armed for any future out-of-phase path.

### Flow-health metric — 15 subagents, ZERO cold fix agents

| Phase | Subagents | Canonical? |
|---|---|---|
| scaffold | 3 (api, app, worker) | ✓ |
| feature | 2 (backend, frontend) | ✓ |
| codebase-content | 3 | ✓ |
| claudemd-author | 3 | ✓ |
| env-content | 1 | ✓ |
| refinement | 3 (rulewalk, audit, apply) | ✓ |
| **total** | **15** | **15 canonical, 0 cold fix** |

All 15 are `general-purpose` (none `claude`). The 3 cold fix agents run-51 was
forced into (because `SendMessage` was unavailable to repair gate rejections)
are **gone** — not because `SendMessage` was added (it was deliberately dropped
as moot per the fix spec), but because the root cause was removed: there is no
gate rejection to repair, so no fix pass is needed. Δ = **−3 subagents**, and
the −3 are precisely the substrate-forced ones.

### Fix 4 + Fix 5 — the worker `zsc noop` regression is gone

- **Zero `zsc noop` on any deliverable surface.** The only repository hits are
  in `environments/.briefs/*/part-*-rules-from-goldens.md`, where the V6
  forbidden-token rule *teaches its retirement* ("`zsc noop` is now stale — dev
  setups omit `run.start` entirely").
- **`run.start` is uniformly omitted across all three dev runtimes** — the
  affirmative verify-present shape the brief demanded:
  - apidev: `start: node dist/main.js` appears **only** in the `prod` setup
    ([`apidev/zerops.yaml:113`](../docs/zcprecipator3/runs/52/apidev/zerops.yaml#L113)); the `dev` setup
    (lines 122+) has no start, only `initCommands`.
  - workerdev: `start` only in `prod` ([`workerdev/zerops.yaml:65`](../docs/zcprecipator3/runs/52/workerdev/zerops.yaml#L65)); the
    `dev` setup explicitly narrates *"No `start` here on purpose — the platform
    leaves the container idle"* ([line 96](../docs/zcprecipator3/runs/52/workerdev/zerops.yaml#L96)).
  - appdev: the `prod` setup is `base: static` (Nginx — **correctly carries no
    `start` either**, codex's precision correction); the `dev` setup
    ([line 57](../docs/zcprecipator3/runs/52/appdev/zerops.yaml#L57)) narrates *"No start command, so the Node container
    stays alive and idle."*
- **Fix 4's lint held silently.** Because no stray `run.start` was authored,
  the `dev-runtime-run-start-present` gate had nothing to fire on — the
  verify-present shape (the reproducer path is the verify-fired shape, which
  did not occur and did not need to).
- **Fix 5's scrub gave a clean parent.** The scaffold drew the dynamic-dev
  pattern with no `zsc noop` to re-inherit.

---

## Axis A — content + correctness

### A1 — Schema (structurally valid; the bare-form BC question carries forward)

Re-validated all YAMLs (the six tier `import.yaml` + the three `zerops.yaml`),
normalizing bare-form type/base tokens first per
`internal/topology/type_equivalence.go` (and memory
`reference_schema_bareform_bc.md`):

- **Structure is clean.** No `envVariables` at any setup-entry top level — all
  seven `envVariables` blocks are nested under `build:` or `run:` (the CLAUDE.md
  invariant holds). No `additionalProperties` violations. `mode` is `HA`/`NON_HA`
  only — tier-5 sets `HA` on db/cache/broker and `NON_HA` on `search`
  (correct — Meilisearch is single-node only, as the recipe itself teaches).
  `buildFromGit` sits only on runtime services, never on managed deps.
- **Bare-form tokens persist** (`nodejs@22`, `postgresql@18`, `valkey@7.2`,
  `nats@2.12`, `meilisearch@1.20` — 6 occurrences of each across the 6 tiers).
  These are **API-accepted legacy BC**, not schema failures — do not false-flag
  them. They diverge only from the current *published* composite-only enum.
  This is the unchanged corpus-wide modernization question (deferred per the
  fix-spec decision log), not a run-52 defect.

### Issue 1 — rolling-deploy + `maxRam`: CONFIRMED-FIXED

Zero `maxRam` in the deliverable. Production tiers frame `minContainers: 2` as
capacity + crash-absorption with `verticalAutoscaling.minRam` as the adapt-knob
([`4 — Small Production/import.yaml:19-23`](<../docs/zcprecipator3/runs/52/environments/4 — Small Production/import.yaml#L19-L23>)):
*"minContainers: 2 gives capacity for concurrent traffic and absorbs a
single-container crash without dropping requests, because the L7 balancer keeps
routing to the survivor."* Every rolling-deploy mention (apidev IG §4,
workerdev IG, both `src/` comments) attributes zero-downtime to SIGTERM-drain +
readiness, never to `minContainers`. No conflation.

### Issue 2 — cross-service env model: CONFIRMED-FIXED

Zero `envIsolation`/`auto-inject`. The project-level vs cross-service split is
taught precisely: project-scope constants (`JWT_SECRET`, `FRONTEND_URL`,
`DEV_FRONTEND_URL`) auto-inherit and are *deliberately not re-declared* —
*"re-declaring one under its own name shadows the inherited value with the
literal unresolved token"* ([`apidev/zerops.yaml:54-58`](../docs/zcprecipator3/runs/52/apidev/zerops.yaml#L54-L58)); cross-service refs
(`${db_*}`, `${broker_*}`, …) are renamed under the keys the app code reads.
The IG §5 "Never re-declare a project-level variable under its own name" section
teaches the self-shadow-to-literal outcome correctly.

### Issue 3 — `zsc noop --silent`: CONFIRMED-FIXED / GONE (the owner's top item)

The issue the owner explicitly cared most about retiring is **fully resolved**.
Zero `zsc noop` on every deliverable surface — apidev, appdev, workerdev, all
six environment tiers, both README and YAML. This is the verify-gone shape from
run-51 followup #8, achieved. The remaining corpus occurrences are confined to
the engine briefs' V6 forbidden-token lists, which *teach* retirement.

### Issue 4 — execOnce seed key: CONFIRMED-FIXED (R49 defect) + MILD DESIGN REGRESSION vs run-51

Two layers, and codex corrected my first-pass framing here:

- **The R49-I4 defect is fixed.** Burn-recovery vocabulary
  (`burn`/`burned`/`consumed key`) is **entirely absent**. The execOnce comment
  ([`apidev/zerops.yaml:100-106`](../docs/zcprecipator3/runs/52/apidev/zerops.yaml#L100-L106)) teaches the genuine pitfall correctly:
  *"zsc execOnce dedupes on the key alone — the command after `--` is not part
  of the key. Give migrate and seed distinct keys, otherwise the second one
  sees the first's key already satisfied and is skipped as a no-op."* That is a
  real, valuable lesson (the TIMELINE Issues table records it as a Medium bug
  caught and fixed during scaffold).

- **But the seed key-shape regressed from run-51's better design.** Run-51's
  *improvement* was a STATIC seed key (`nestjs-showcase.seed.v1`) that runs the
  seed exactly once per service lifetime, with a documented `.v1→.v2` rotation
  lever and a dedicated KB section ("### Re-firing the seed after its data
  changes"). Run-52 reverts to a per-deploy `${appVersionId}-seed` key
  ([`apidev/zerops.yaml:109`](../docs/zcprecipator3/runs/52/apidev/zerops.yaml#L109)), relying solely on an application-level
  count-guard in `seed.ts` ([lines 54-58](../docs/zcprecipator3/runs/52/apidev/src/seed.ts#L54-L58): `if (existing > 0) skip DB insert`)
  to avoid duplicate rows. Codex's correction stands: this is duplicate-safe
  for the *current* seed data, but **strictly weaker** — if a future author
  adds an item to `SEED_ITEMS`, the `count() > 0` guard blocks the new row from
  ever landing on an existing deployment, and run-52 **dropped run-51's
  re-seed KB teaching**, leaving the porter no documented escape hatch. It is
  not a correctness defect today (the showcase deploys green; the index is
  re-asserted every deploy via `indexItems`), but it is a robustness regression
  and a latent authoring trap that shipping-as-canonical would propagate. This
  is the single nit that gates the verdict.

### Voice + comments (A3): strong, causal, gate-enforced

Comments are period-terminated, "why"-framed, with appositive em-dashes (not
two-thought welds). The `yaml-comment-missing-causal-word` enforcer drives the
consistency. No regression.

### Slug-citation hygiene (A4): clean

Markdown links use friendly display text (e.g. "Zerops L7 balancer + subdomain
access", "zerops.yaml") — no bare slugs, no display-text-vs-friendly-name
mismatch. The codebase-content phase passed citation validation on first call.

### Surface-5 (A5): structural again, clean

apidev KB ships forward-looking topic H3s (`### Object storage runs on a MinIO
backend`, `### Search is single-node — plan around no HA`, `### Custom domain
swaps the CORS allow-list source`) — content-driven, each citing its guide by
descriptive link text. Clean of any Issue-1 conflation. The dual-shape contract
holds across runs.

### Facts layer (A6): clean — run-51 residuals did NOT recur

`facts.jsonl` describes the dev runtime as *"Dev omits run.start entirely"*
(e.g. the `api-yaml-run-start` fact) with **no** `(zsc noop)` muddle and **no**
burn-framing. The new `execonce-distinct-key-per-init-command` facts are
accurate. Run-51's two facts-staleness residuals (`omitted (zsc noop)`,
both-per-deploy burn framing) are both absent. The one carry-forward: facts are
still write-only (no retraction mechanism), so the *general* staleness risk
remains for future runs where a later phase supersedes an early decision — but
it did not bite this run.

---

## Axis B — flow quality (graded against the fix)

**The walk was smooth — the direct contrast to run-51's documented cascade.**

- **Phase-gate walk:** every phase closed `ok:true` on its first/only
  `complete-phase`. No phase was entered late, no re-stitch loop, no masked
  defects. The enter-before-dispatch ordering held at the one place run-51
  broke (codebase-content) *and* throughout.
- **Tool-call error inventory:** transport `is_error:true` = 2, in-envelope
  `ok:false` = 0.
  - Error 1 — `build-subagent-prompt briefKind=feature` called without the
    `pass` param → engine returned *"unknown pass (want backend or frontend)"*.
    The agent self-corrected in one step (supplied `pass=backend` then
    `pass=frontend`); both feature subagents completed. A one-shot param-shape
    self-correction, **in stride** — the disciplined-recovery shape, not a
    cascade. (Mild NEW friction; see followups.)
  - Error 2 — a Bash close-step note (`TIMELINE.md is missing…`) with no session
    context. Benign tooling prompt, not a recipe-gate error.
- **v9.102.0 hardening still holds:** zero `slug is required`, zero `outputRoot
  must nest under…`, zero `parentStatus absent`, zero `zerops-`-prefixed slug
  mismatch.
- **Refinement was normal:** refinement-1 (0 edits, flagged 1 misroute),
  refinement-2 (16 advisory findings — 8 citations, 5 cross-surface dedups, 3
  misc), all ACTed by a single `refinement-apply` agent with no reverts. This
  is healthy refinement, not churn.

---

## Codex validation

Codex (agent `a0f371eef6c7c71ff`) independently verified all nine claim-groups
against primary sources, with explicit instruction to be adversarial on root
causes and the verdict. It CONFIRMED the structural findings and corrected the
report in two places — reproducing the run-50/51 codex value (it haircut rather
than rubber-stamped).

| Claim | Codex verdict | Resulting change |
|---|---|---|
| 1 — Engine lineage v9.104.0, all 6 fix commits ancestors → fixes present | **CONFIRM** (`plan.json:3`; merge-base 0 for all six) | Stand |
| 2 — Fix 1+2 held; enter precedes dispatch; net never tripped; complete-phase passed first call | **CONFIRM** (`main-session.jsonl` enter-phase before first build-subagent-prompt; zero "belongs to phase"; complete-phase `ok:true`) | Stand |
| 3 — 15 subagents, 0 cold fix; Δ −3 vs run-51 | **CONFIRM** (count + descriptions) | Stand |
| 4 — `zsc noop` gone on all deliverable surfaces | **CONFIRM** (only brief-list hits, teaching retirement) | Stand |
| 5 — `run.start` uniformly omitted across 3 dev runtimes | **REFINE** — my "prod blocks carry start" was over-broad; the static `appdev` prod *correctly omits it too* (only process runtimes carry prod `start`) | Tightened the Fix-4 wording |
| 6 — Issue-4 seed key is "design divergence, not regression" | **REFINE → effectively REFUTE the 'not a regression' framing** — the per-deploy key + count-guard is duplicate-safe *only for current data*; it silently blocks new seed rows if `SEED_ITEMS` changes, and run-51's static key was the safer design. "Mild design regression," not equivalence | Downgraded Issue 4 to CONFIRMED-FIXED + mild regression; **this became the gating nit** |
| 7 — Issues 1 & 2 stay fixed | **CONFIRM** (zero maxRam/envIsolation/auto-inject; capacity+crash framing) | Stand |
| 8 — facts layer clean, run-51 residual did not recur | **CONFIRM** (zero `zsc noop`/`burn` in facts.jsonl) | Stand |
| 9 — verdict | **REFINE** — structurally clean, all three gating conditions confirmed; bare-form tokens correctly non-gating; but the seed-key regression should be a *noted design debt*, "SHIP-WITH-CAVEAT" not full canonical | Set verdict to **SMOOTH-WITH-NITS** with the seed key as the named nit |

Codex one-line verdict, verbatim: *"Run-52 is structurally clean — all three
gating conditions confirmed — but two refinements are warranted: the 'prod
always has start' claim is over-broad (static runtimes correctly omit it), and
the per-deploy seed key is duplicate-safe only under current data shape, making
it weaker than run-51's static key rather than equivalent."* Both folded in.

---

## Big picture — the process-ergonomics surface that gated run-51 is closed

Run-51's central cross-run finding was that the failure surface had shifted
from CONTENT (runs 49–50) to PROCESS ERGONOMICS — the dispatch-before-enter
cascade, the gate-masking, the absent `SendMessage`, the cold fix agents.
**v9.104.0's five fixes targeted exactly that surface, and run-52 proves they
landed:**

- The phase walk is enter-then-dispatch *by construction* (Fix 1 guidance +
  Fix 2 precondition), so the cascade cannot re-form.
- No gate-masking (Fix 3 armed; not even needed because Fix 2 prevents the
  out-of-phase self-validate).
- No cold fix agents — and `SendMessage` was correctly *not* needed, validating
  the fix-spec's decision to drop it as moot (the root fix removed the fix pass
  that needed it).
- The content regression that had no removal path in run-51 (worker `zsc noop`)
  is structurally prevented (Fix 4 lint + Fix 5 parent scrub).

This is the strongest run in the 49→50→51→52 sequence on *both* axes
simultaneously — the first run where content is strong AND the process walked
smoothly. The residual is no longer structural: it is a single content-design
choice (per-deploy vs static seed key) that is correct-but-weaker, plus the
standing deferred decisions (corpus sweep, bare-vs-composite, facts retraction).
The systematic, recurring-every-run process risk that dominated run-51 is
**retired**.

---

## Verdict — SMOOTH-WITH-NITS (one revert from canonical)

Run-52 walked smoothly and cleanly. Both reasons run-51 was not ship-as-
canonical are gone and verified: the worker `zsc noop --silent` is absent on
every surface, `run.start` is uniformly omitted across all three dev runtimes,
the codebase-content phase entered-before-dispatch with no cascade and **zero**
cold fix agents (15 subagents, all canonical), and the keystone Fix-2 net was
never tripped because the Fix-1 guidance routed the agent correctly. Issues 1,
2, and 3 are CONFIRMED-FIXED; the facts layer is clean; schema is structurally
valid; voice and citations are gate-clean.

It falls one nit short of SHIP-AS-CANONICAL: **Issue 4's seed key regressed
from run-51's static `nestjs-showcase.seed.v1` (run-once-per-lifetime + a
documented re-seed lever) to a per-deploy `${appVersionId}-seed` key guarded
only by `seed.ts`'s `count() > 0` check.** It is duplicate-safe today, but it
silently stops inserting new seed rows if the seed data ever changes, and
run-52 dropped run-51's "### Re-firing the seed" teaching — so canonizing it
would propagate the weaker pattern. This is a soft, contained content nit, not
a structural blocker, which is why this is **not** ITERATE-TO-53.

### Prioritized followups

1. **Revert Issue-4 seed to run-51's static-key design** (the gating nit).
   Use a static `nestjs-showcase.seed.v1` key with a `.v1→.v2` rotation lever
   for the seed (keep `${appVersionId}-migrate` per-deploy), and restore the
   "### Re-firing the seed after its data changes" KB section. This closes the
   only thing between run-52 and SHIP-AS-CANONICAL. *(Carries forward as a new
   item; run-51 had this RIGHT and run-52 lost it.)*

2. **Add the required `pass` param hint to `build-subagent-prompt
   briefKind=feature`** (new minor friction). The first feature dispatch errored
   with "unknown pass (want backend or frontend)"; surfacing the required param
   in the feature phase-entry next-call guidance would remove the one-shot
   self-correction. Low priority — recovered in stride.

3. **Complete the corpus `zsc noop` scrub beyond the 2 parents** (carried from
   run-51 #4). Fix 5 scrubbed `nestjs-minimal.md` + `nodejs-hello-world.md`;
   the other ~41 `internal/knowledge/recipes/` files still carry the token.
   Fix 4's lint backstops the deliverable, but a forked recipe still re-inherits
   it from the corpus. Finish the sweep.

4. **Decide the bare-form-vs-composite type/base stance** (carried from run-51
   #7). The corpus still emits `nodejs@22`/`postgresql@18`; the published schema
   is composite-only but the API accepts bare as BC. Migrate the corpus or
   record BC-acceptance as the stance so "validate against the published schema"
   stops surfacing as a false failure. Affects every recipe.

5. **Add a facts-retraction mechanism** (carried from run-51 #6, lower
   priority). Facts did not go stale this run, but the write-only facts trail
   still has no way to retract an early decision a later phase supersedes — a
   latent risk for future runs.

### What this is NOT

- **NOT a stale-binary / fix-absent failure.** The engine stamps v9.104.0
  on-premise; all five fix commits are ancestors. The fix-regression axis was
  live and the fixes held.
- **NOT ITERATE-TO-53.** Every structural blocker run-51 carried (the cascade,
  the cold fix agents, the worker `zsc noop`, the masked citation defects) is
  fixed and verified. The only residual is one soft content-design nit.
- **NOT a flow regression.** The walk was the smoothest in the sequence:
  enter-before-dispatch, zero cascade, zero cold fix agents, zero in-envelope
  rejections, v9.102.0 hardening intact.
- **NOT a recurrence of Issues 1, 2, or 3.** All three are CONFIRMED-FIXED;
  Issue 3 (the owner's top concern) is fully gone from every surface.
- **NOT a subagent-overrun.** 15 subagents, all canonical, all general-purpose
  — the −3 delta vs run-51 is exactly the substrate-forced cold fix agents,
  now eliminated.
- **NOT a stochastic-drift artifact.** The seed-key regression traces precisely
  to the scaffold author choosing a per-deploy key + count-guard (TIMELINE
  Issues row: "Distinct keys… both fire (verified idempotent)") rather than
  run-51's static key — a reproducible design choice, not random variance.
