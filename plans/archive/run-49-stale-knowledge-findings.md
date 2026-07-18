# Run-49 stale-knowledge findings — four bullets the project owner flagged

> **Headline (after triple-verification).** Project owner reviewed run-49
> nestjs-showcase output and identified four pieces of stale or wrong
> guidance propagated by the recipe-engine knowledge corpus. Triple-
> verified (round 1 codex, round 2 fresh-opus with primary-source read,
> round 2 deeper-codex against public docs): three issues are
> CONFIRMED-WRONG, one is owner-authoritative-stale-but-replacement-
> uncertain (Issue 3). None are framework-specific — every one is a
> platform-mechanic error baked into atoms, briefs, or the canonical
> knowledge guides. Issue 2 (cross-service env-var auto-injection)
> remains the highest-stakes finding; deeper review actually
> *strengthened* the case (every shipped recipe demonstrates the owner
> is right by always writing the `run.envVariables` aliases — none ships
> bare auto-inject) but **the round-1 CONFIRMED-WRONG label was
> over-confident on the replacement model** — the corpus's `envIsolation`
> sub-claim needs platform-verifier ground-truth before rewrite. Issues
> 1 and 4 picked up nuance in round 2 (see "Verification refinements"
> below).

---

## Fresh-against-main check (2026-05-20)

Fetched origin/main (30+ commits since our branch base d26be103). Key
commit relevant to Issue 2: **`31d976b4 audit: env-var corpus +
preflight + diagnosis (Phase 0-4)` (krls2020, 2026-05-17)** — an
empirical audit of env-var corpus that landed exactly the same finding
**from the other direction**. Section B3 of
[`plans/audit-env-vars-20260515/AUDIT.md`](plans/audit-env-vars-20260515/AUDIT.md)
states verbatim:

> "Under default `service`: NO auto-inject; you must declare `${db_*}`
> in `run.envVariables` to pull a value. Under legacy `none` (eval-zcp's
> current setting): ALL service vars auto-inject across all
> containers."

And: *"LLM seeing eval-zcp's traces (which auto-share everything)
infers a pattern that breaks on production projects with default
isolation."* — exactly the "zcp project is envIsolation:none, recipes
must author for porter default" frame the user gave me.

**What changed on main that partially fixes Issue 2:**

| File | Status on main |
|---|---|
| `internal/knowledge/guides/environment-variables.md` | **DELETED** — the worst-offending file is gone |
| `internal/knowledge/guides/production-checklist.md` | **DELETED** — the "minContainers: 2 on all app services for zero-downtime deploys" claim from Issue 1 is gone with it |
| `internal/content/atoms/develop-env-var-model.md` | **NEW** — careful wording: project envs auto-inject (correct), cross-service refs go in `run.envVariables` as renames (correct). The atom is right but doesn't make the project-vs-cross-service distinction crystal-clear; doesn't reference isolation modes at all (good — purge already started). |
| `internal/content/atoms/develop-env-var-shell-usage.md` | **NEW** — single-quote SSH bodies, read-vs-use distinction |
| `internal/content/atoms/develop-reserved-env-names.md` | **NEW** — 3 reserved-name regimes (R1 zcli pre-flight, R2 deploy-stage, R3 accepted) — addresses a separate gap |
| `internal/content/atoms/develop-first-deploy-env-vars.md` | **REWRITTEN** — drops "prefer connectionString over assembling" misdirection; references new env-var-model atom |
| `internal/content/atoms/develop-platform-rules-common.md` | EDITED (minor) |
| `internal/ops/env_shadow.go` (mechanism) + `internal/ops/deploy_validate.go::CheckReservedEnvNames` | NEW tool-level preflight + reserved-name checks |

**What still stands wrong on main (Issues 1, 2-residual, 3, 4):**

| File | Status | Issues affected |
|---|---|---|
| `internal/recipe/content/briefs/scaffold/platform_principles.md:21-69` | **UNCHANGED from our branch base.** Still asserts "Cross-service env vars auto-inject project-wide". The "Same-key shadow trap" architecture still encoded as a real failure mode. | Issue 2 |
| `internal/recipe/content/briefs/env-content/per_tier_authoring.md:301-317, 843-845` | **UNCHANGED.** Rolling-deploy PASS exemplar still teaches "minContainers: 2 keeps rolling deploys zero-downtime (one container serves traffic while the other rebuilds)" with the maxRam hallucination. | Issue 1 |
| `internal/recipe/content/briefs/codebase-content/synthesis_workflow.md:160-275` | **UNCHANGED.** Still encodes "same-key shadow trap" worked examples + the `zsc noop --silent` PASS exemplar at lines 260-275. | Issues 2, 3 |
| `internal/recipe/content/principles/dev-loop.md` | **UNCHANGED.** Universal `zsc noop --silent` convention still encoded. | Issue 3 |
| `internal/recipe/content/principles/init-commands-model.md` | **UNCHANGED.** Principle file itself is fine; the conflation lives elsewhere. | Issue 4 (reference only) |
| `internal/content/workflows/recipe.md:675, 946, 1600, 2344, 2366` | **UNCHANGED.** Still carries "project-level env vars auto-inject" plus the execOnce burn-recovery conflations. | Issues 2, 4 |
| `internal/knowledge/themes/refinement-references/voice_patterns.md:101` | **UNCHANGED.** "rolling deploys — one serves traffic" voice exemplar still present. | Issue 1 |
| `internal/recipe/gate_worker_dev_server.go` | **UNCHANGED.** Validator still enforces `zsc noop --silent` discipline. | Issue 3 |
| `internal/tools/workflow_checks_claude_md_test.go:49-50, 195` | **UNCHANGED.** Test fixtures still teach the per-deploy + burn-recovery conflation. | Issue 4 |
| `internal/content/workflows/recipe/briefs/writer/citation-map.md:13` + `editorial-review/citation-audit.md:16` + `docs/spec-content-surfaces.md:479` | **UNCHANGED.** Still cite `envIsolation semantics` as a porter-facing topic. | Issue 2 (purge) |
| `internal/recipe/citations.go:17` + `briefs_refinement2.go:307` + `classify.go:151` | **UNCHANGED.** Still reference `envIsolation` in the citation pipeline. | Issue 2 (purge) |

### Net assessment

- **All four findings still stand verbatim** at the recipe-engine /
  briefs / workflow layer — the May 17 audit hit the atoms + ops layer
  but never touched the recipe-engine briefs.
- **Issue 2's evidence is now overwhelming**: Karel's audit on main
  independently arrived at the same finding (under default `service`,
  no auto-inject); the user's correction today reinforces it; the
  `environment-variables.md` guide is already gone. **The platform
  verifier ground-truth test is mooted** — the audit already did it
  (live `printenv` on eval-zcp with `envIsolation: none`; cross-service
  vars auto-inject; documented that they would NOT under default
  `service`). Confidence is now near-certain; the rewrite can proceed
  without an additional verifier round.
- **The audit confirms the "zcp project is envIsolation: none" frame**
  — eval-zcp (Karel's verification project) is also envIsolation: none,
  and the audit explicitly identifies this as why the corpus drifted.
- **Issue 1 lost one citation file** (production-checklist.md is
  deleted) but kept the load-bearing one (per_tier_authoring.md PASS
  exemplar).
- **Issues 3 and 4 entirely unaffected** by the May 17 audit — its
  scope was env-vars only.

### Implication for the rewrite scope

The rewrite plan stays the same shape but with two adjustments:

1. **Don't recreate the deleted guide**. The `environment-variables.md`
   knowledge guide was rightly deleted — the new atom-based teaching is
   the load-bearing path now. The brief-layer rewrite should align with
   the new atoms (`develop-env-var-model`, `develop-env-var-shell-usage`,
   `develop-reserved-env-names`), not regenerate the old guide.
2. **The audit doc itself is a strong piece of evidence to reference**
   when explaining the rewrite — quote `plans/audit-env-vars-20260515/AUDIT.md`
   Section B3 as the authoritative platform-truth for the new wording.

The 11-file purge list in "Corpus purge" section above is fully
unchanged — every file on it is still wrong on main exactly as we
documented.

---

## Verification refinements (round 2)

| Issue | Round-1 verdict | Final verdict | Refinement |
|---|---|---|---|
| 1 | CONFIRMED-WRONG | CONFIRMED-WRONG | Add precision: `minContainers≥2` is for *capacity / failure tolerance*, NOT the zero-downtime guarantee (which is `temporaryShutdown: false` default, independent of minContainers) |
| 2 | CONFIRMED-WRONG-HIGH | OWNER-CORRECT-HIGH (corpus internally consistent but contradicted by every shipped recipe + by atoms `develop-first-deploy-env-vars` + `bootstrap-env-var-discovery` + by current public docs) + **root cause identified**: zcp's own dev project runs `envIsolation: none` so engineers see auto-inject working locally; porters see default isolation. Recipe engine must author for isolation. | The corpus's "auto-inject in every mode" claim at `environment-variables.md:121-139` is the outlier; the atoms layer already aligns with the owner's claim. Critical analytic move that round 1 missed: **no shipped recipe demonstrates bare auto-inject** — every one writes the `DB_HOST: ${db_hostname}` block, which is unnecessary if auto-inject were really true. Recipe behavior is the strongest internal evidence the corpus theory was always wrong. Also distinguish: **project vars DO auto-inherit; cross-service SERVICE vars do NOT** — the rewrite needs both halves precise. **Purge directive**: remove every reference to legacy/non-isolated mode from the corpus output (knowledge guides, briefs, citation maps, spec tables). Porters should not learn legacy mode exists. |
| 3 | UNCERTAIN | OWNER-AUTHORITATIVE-STALE | No external evidence found in any round; owner's claim stands. Replacement pattern unspecified — platform-verifier test of `run.start: ""` omission is the right next step. |
| 4 | CONFIRMED-WRONG | REFINED — WRONG ONLY ON CROSS-DEPLOY FRAMING | The "two distinct per-deploy keys" advice IS valid for *within-deploy* lock-collision avoidance (per `init-commands-model.md:44-60`). The conflation is ONLY the cross-deploy "next deploy retries seed while migrate is skipped" framing — that part applies to static keys, not per-deploy. Fix: keep distinct-keys rationale, drop cross-deploy burn-recovery framing. |

---

## Issue 1 — Rolling-deploy mechanism is mis-described + maxRam hallucination

### Owner feedback

```
# Run two NestJS API replicas on shared CPU — minContainers: 2
# keeps rolling deploys zero-downtime (one container serves traffic
# while the other rebuilds). The L7 balancer distributes requests
# across replicas. Bump verticalAutoscaling.maxRam when monitoring
# shows containers approaching the current ceiling.
```

> "The em-dash to separate 2 different thoughts makes no sense, should be
> a dot. Our rolling deploy doesn't work the way it describes. I don't
> understand what it's hallucinating about maxRam bumping, when it set no
> ceiling. It keeps re-hashing the minContainers and rolling deploys over
> and over (makes no sense)."

### Root cause — three vectors

1. **Canonical PASS template models the mechanism error.**
   `internal/recipe/content/briefs/env-content/per_tier_authoring.md:301-317`
   — the tier-4 small-prod PASS example shipped to every env-content
   sub-agent literally teaches *"minContainers: 2 keeps rolling deploys
   zero-downtime (one container serves traffic while the other rebuilds)"*.
   The mechanism is wrong: per the canonical Zerops behavior documented at
   `internal/knowledge/guides/zerops-yaml-advanced.md:51` (and public docs
   at `docs.zerops.io/features/pipeline`), the rolling-deploy mechanism is
   **new-then-cutover** — new containers spin up, readiness check passes,
   old containers are removed. The build artifact is finished BEFORE the
   runtime deploy begins; the old container does NOT "rebuild." This is
   the platform default (`temporaryShutdown: false`) and works
   independently of `minContainers`.

2. **A second copy of the same wrong mechanism lives at line 843-845** of
   the same brief, packaged as the "Replace with mechanism + reason"
   anti-pattern-fix example: *"Two containers always running enables
   rolling deploys — one serves traffic while the other rebuilds, no
   downtime."* Same misdescription, taught as the GOOD alternative.

3. **Voice-pattern reference reinforces it.**
   `internal/knowledge/themes/refinement-references/voice_patterns.md:101`
   carries *"# dashboard available during rolling deploys — one serves
   traffic"* as a voice exemplar.

4. **Adapt-knob is hallucinated.** The PASS template instructs *"Bump
   verticalAutoscaling.maxRam when monitoring shows containers
   approaching the current ceiling"* — but the example yaml does not set
   `maxRam` at all. The brief's friendly-authority phrasing contract
   (per_tier_authoring.md:319-358) demands a "named external condition"
   for every adapt phrasing; the template models pointing at a field that
   the yaml does not set, which is what sub-agents then reproduce.

5. **Repetition pressure is structural.** The brief allocates one
   PASS-shaped block per service per tier, and tier-4 is the canonical
   small-prod tier. Sub-agents reproduce the same minContainers /
   rolling-deploys block on every runtime service in the tier yaml.
   The brief has an explicit anti-repetition rule ("Repetition across
   services… write it once at the project level or first service block")
   at line 847, but the PASS exemplar at line 301 — which is what
   sub-agents copy from — models the per-service form.

### Codex verdict (round 1 + 2)

CONFIRMED-WRONG, with a precision refinement from round 2. Evidence:
docs.zerops.io/features/pipeline + docs.zerops.io/features/scaling.
*"The build artifact is already stored before runtime deploy begins —
the old container never 'rebuilds,' it is replaced. `temporaryShutdown:
false` (the default) is what drives zero-downtime, not `minContainers:
2`. The maxRam advice is additionally hallucinated because the yaml
sets no ceiling."*

**Round-2 refinement.** `minContainers≥2` is for capacity and
failure-tolerance (a single container can crash mid-handle without
dropping all traffic; multiple containers absorb concurrent load), NOT
for the zero-downtime rolling-deploy mechanism. The rewrite must say
something like: *"default new-before-old replacement IS the
zero-downtime mechanism (per `temporaryShutdown: false` default);
`minContainers: 2` is for capacity and crash-tolerance, not the deploy
cutover itself."* The mental model is two orthogonal axes — the
existing `feedback_horizontal_scaling_vs_ha.md` memory already
captures the throughput-vs-HA distinction; round 2 added: rolling-deploy
cutover is its own third axis, owned by `temporaryShutdown`, not
`minContainers`.

### Affected files

- `internal/recipe/content/briefs/env-content/per_tier_authoring.md:301-317` (PASS exemplar)
- `internal/recipe/content/briefs/env-content/per_tier_authoring.md:843-845` (anti-pattern-fix exemplar)
- `internal/knowledge/themes/refinement-references/voice_patterns.md:101`
- Knowledge-guide `production-checklist.md:127` says *"`minContainers: 2`
  on all app services for zero-downtime deploys"* — this is also wrong as
  stated (zero-downtime is the default at minContainers=1; the reason for
  minContainers≥2 is the *availability-during-crash* and *capacity* axes,
  not the rolling-deploy-cutover axis).
- Memory `feedback_horizontal_scaling_vs_ha.md` already captures the
  "two independent axes" distinction (throughput vs HA-during-crash); the
  brief and the production-checklist guide both predate that correction.

---

## Issue 2 — Cross-service env vars do NOT auto-inject (HIGH-STAKES)

### Owner feedback

```
5. Alias platform env vars under your own keys
Zerops auto-injects cross-service references as ${db_hostname},
${broker_port}, ${storage_apiUrl}, etc. — platform-specific names you
don't want hard-coded in application code. Re-export each one under your
own stable key in zerops.yaml run.envVariables, and have the app read
only the own-key names. Swapping a managed service later becomes a
yaml-only edit.
```

> "This isn't true. By default if you do not alias them in zerops.yaml
> (or in UI), your app will not see them, not even under the original
> names (${service_envName})."

### Root cause — corpus-wide architectural error

The *primary* source of this claim is the authoritative knowledge guide
itself: `internal/knowledge/guides/environment-variables.md`. Line 3:

> "Both project-level vars AND cross-service vars (`${hostname_varname}`)
> auto-inject as OS env vars into every container in the project — no
> declaration required."

Line 48:

> "Every service's variables are automatically injected as OS environment
> variables into every other service's containers — both runtime and
> build. A worker container sees `db_hostname`, `db_password`,
> `queue_user`, `storage_apiUrl`, etc. as real OS env vars at container
> start. Zero declaration in zerops.yml required."

This is the foundational claim. Everything else in the corpus that uses
the phrase "auto-inject" / "auto-propagates" / "self-shadow" reasons FROM
this premise:

- `internal/recipe/content/briefs/scaffold/platform_principles.md:21-23,
  52-69` — the "Managed services" + "Same-key shadow trap" architecture
- `internal/recipe/content/briefs/codebase-content/synthesis_workflow.md:160-188`
  — worked examples teach "DB_HOST aliases ${db_hostname} — same-key
  would self-shadow the auto-inject"
- `internal/content/atoms/develop-platform-rules-common.md:20`
- `internal/content/atoms/bootstrap-env-var-discovery.md:12,18`
- `internal/content/workflows/recipe.md:675` — *"project-level env vars
  auto-inject into both runtime AND build containers"*
- `internal/recipe/content/briefs/scaffold/decision_recording.md:252` —
  *"Zerops injects cross-service refs as ${db_hostname}…"*
- `internal/recipe/refinement_suspects_run29_test.go:29` — refinement
  validator looking for *"envVariables write runs after the auto-inject"*

### Verdicts across all three rounds

- **Round 1 (codex)**: CONFIRMED-WRONG-HIGH. Cited default
  `envIsolation: service` semantics from public docs.
- **Round 2 (fresh-opus, primary-source read)**: CONFIRM-WRONG with
  refinement — the round-1 label was over-confident on the replacement
  model. The corpus is *unusually* internally consistent here (multiple
  load-bearing files, gates, pin tests all reason from the auto-inject
  premise) — so labeling it CONFIRMED-WRONG based on codex's inference
  about `envIsolation: service` is shaky inference. The right verdict
  is "owner-authoritative + corpus is contradicted by its own recipe
  output." **The strongest internal evidence the corpus is wrong**:
  every shipped recipe (`solidstart`, `gleam-hello-world`,
  `nodejs-hello-world`, `react-router-ssr-hello-world`,
  `laravel-showcase`) writes `DB_HOST: ${db_hostname}` etc. in
  `run.envVariables`. If bare auto-inject worked as the corpus claims,
  none of them would need to. The recipe behavior demonstrates the
  owner's claim is right, regardless of what the guide says.
- **Round 2 deeper-codex**: CONFIRM-PRIOR with crucial distinction —
  **project-level vars DO auto-inherit to every container in the
  project; cross-service SERVICE vars do NOT under default
  `envIsolation: service`** — only legacy `envIsolation: none` made
  service vars cross-service-visible without declaration. Current
  `docs.zerops.io/features/env-variables` confirms this split. The
  corpus atoms (`develop-first-deploy-env-vars`,
  `bootstrap-env-var-discovery`) are already aligned with the
  docs/owner; the guide at `environment-variables.md:121-139` is the
  outlier asserting "envIsolation does NOT control whether
  cross-service vars auto-inject — they do, in every mode" — that
  specific sub-claim is the false-fact at the load-bearing center of
  the corpus's mental model.

### Implication (final, post-triple-verify)

The "same-key self-shadow trap" architecture is built on a wrong
premise. If cross-service service vars don't auto-inject, then
declaring `db_hostname: ${db_hostname}` in `run.envVariables` is NOT a
self-shadow; **it's the canonical and only way** to get `db_hostname`
into the process env. The interpolator chain described at
`environment-variables.md:97-110` (literal-string fallback,
"interpolator can't recurse back") is also wrong as stated — the
question of "what does `${db_hostname}` resolve to in
`run.envVariables`" is the same in every-key case (it resolves to the
peer service's value at container start) regardless of whether the
key on the left matches.

**Distinct from**: project-level vars (set on the *project*, not on a
service) — those DO auto-inherit to every container, both build and
runtime. So `${API_URL}` declared at project level shows up in every
service's process env without any `run.envVariables` declaration. The
rewrite needs both halves precise:

- Project-level vars: auto-inherit ✓ (corpus claim correct)
- Cross-service service vars: explicit reference required ✗ (corpus
  claim wrong)

### Root cause of the corpus's mistake — zcp's own project is non-representative

**zcp's own development project has `envIsolation: none` (legacy mode).**
Under that mode, cross-service service vars DO auto-inject into every
container's OS env under the original `${service_envName}` names —
zero declaration required. zcp engineers see this work end-to-end
during recipe-engine dev/testing, and the corpus generalized that
observation as the universal platform behavior. But porters get the
platform default (`envIsolation: service`), under which cross-service
service vars do NOT auto-inject.

**The recipe engine must author yamls as if it were itself running
under isolated mode**, because that's what porters get. Validating
recipe output by running it against zcp's own project will continue to
mask the bug — broken recipes will look correct in dev because the
local env auto-injects the values regardless.

### Corpus purge — remove every reference to legacy/non-isolated mode

The user wants the porter-facing world to look as if isolation is
universal — don't teach the legacy mode, don't reference it, don't
contrast against it. The full audit surface (run on the branch state
2026-05-20):

| File | What to do |
|---|---|
| `internal/knowledge/guides/environment-variables.md:3,48,109,121-139,167-178` | **Major rewrite.** Drop the "Isolation Modes (envIsolation)" section entirely (lines 121-139). Rewrite line 3, 48, 109, 167-178 to teach: project-level vars auto-inherit; cross-service service vars require explicit aliasing in `run.envVariables` (NOT framed as an "isolation mode" choice — framed as platform behavior period). |
| `internal/recipe/content/briefs/scaffold/platform_principles.md:21-79` | **Full rewrite of "Managed services" section.** Drop the auto-inject premise. The "same-key shadow trap" architecture goes — under isolation, the failure mode doesn't exist. Reference at line 79 ("scopes, and isolation modes") gets the "isolation modes" half deleted. |
| `internal/content/workflows/recipe/briefs/writer/citation-map.md:13` | Remove `envIsolation semantics` from the citation-map row for `env-var-model`. |
| `internal/content/workflows/recipe/briefs/editorial-review/citation-audit.md:16` | Remove the `isolation modes` half from the topic descriptor. |
| `docs/spec-content-surfaces.md:479` | Remove `envIsolation semantics` from the citation-table topic descriptor. |
| `internal/recipe/citations.go:17` | Remove the `"envIsolation": "env-var-model"` mapping entry — porter-facing prose should never cite envIsolation. |
| `internal/recipe/classify.go:151` | Verify context; if it's gating on `envIsolation` as a porter-facing topic, drop. Operational classification (e.g., for zcp's own provisioning) is allowed. |
| `internal/recipe/briefs_refinement2.go:307` | Remove `envIsolation` from the env-var-model citation-trigger pattern list. |
| `internal/recipe/content/briefs/codebase-content/synthesis_workflow.md:160-200` | Rewrite the "same-key shadow trap" worked example — it teaches a failure mode that doesn't exist under isolation. Replace with a cleaner positive teaching: "declare each cross-service alias under your own key in `run.envVariables`; the app reads only its own keys." |
| `internal/content/atoms/develop-platform-rules-common.md:20-23` | Already aligns with isolation, but re-verify the wording doesn't imply auto-inject. |
| `internal/content/atoms/bootstrap-env-var-discovery.md:12-18` | Already aligns with isolation (says discovery returns templates, agent uses them when wiring `run.envVariables`) — preserve. |
| `internal/content/workflows/recipe.md:675` | Rewrite to remove the "auto-inject into both runtime AND build" claim for project-level vars unless that half is verified-correct (the platform-verifier test plan covers this). The wording must distinguish project-level from cross-service. |
| `internal/tools/launch_platform_envs.go:19,62` + `launch_platform_envs_test.go` + `internal/ops/env_*` | **Out of scope.** These handle zcp's own provisioning — managing the `envIsolation` key for zcp's own dev project. Operational concern, not porter-facing corpus output. Leave. |

The `docs/zcprecipator3/simulations/*/briefs/*-prompt.md` files all
carry the phrase "scopes, and isolation modes" as a frozen artifact
from past `platform_principles.md` snapshots. These are historical run
records — once `platform_principles.md` is rewritten, future
simulations won't have the phrase. Old snapshots can be left as
historical artifacts.

This is the platform-verifier test plan, sharpened from round 1
(needed BEFORE the rewrite ships, so the new wording is grounded in
ground-truth not just owner-statement):

- (a) Two-service project with `db` (managed postgres) + `app` (nodejs);
  `app/zerops.yaml` has empty `run.envVariables`; project has default
  isolation (`envIsolation: service`). SSH into app, run `env | grep
  db_` — should be EMPTY (confirms cross-service vars don't
  auto-inject)
- (b) Same project but declare `DB_HOST: ${db_hostname}` in app's
  `run.envVariables`. SSH in, `echo $DB_HOST` — should resolve to
  postgres's real hostname (confirms the alias is the working path)
- (c) Declare a project-level `API_URL: https://example.com`; SSH into
  app with NO `run.envVariables` for it; `echo $API_URL` — should be
  the value (confirms project-level vars DO auto-inherit)
- (d) Same-key form `db_hostname: ${db_hostname}` in `run.envVariables`;
  SSH in, `echo $db_hostname` — should resolve to the postgres
  hostname (since without the alias, the var isn't in the env at all,
  there's nothing to "shadow"). If it produces literal `${db_hostname}`,
  the self-shadow trap is real and needs different handling.

**Critical**: run the platform-verifier test against a **fresh
non-zcp-internal Zerops project** (porter-default isolation), not
against zcp's own project. zcp's project is `envIsolation: none` and
will produce misleading auto-inject behavior.

When the verifier returns results, the env-var-model rewrite can ship.

### Affected files (rough scope of corpus rewrite)

- `internal/knowledge/guides/environment-variables.md` — full rewrite
- `internal/recipe/content/briefs/scaffold/platform_principles.md` — full rewrite of "Managed services" section
- `internal/recipe/content/briefs/codebase-content/synthesis_workflow.md` — rewrite the worked examples that teach "same-key shadow"
- `internal/recipe/content/briefs/scaffold/decision_recording.md:252` — rewrite the "why" string
- `internal/content/atoms/develop-platform-rules-common.md` — update the cross-service env-var bullet
- `internal/content/atoms/bootstrap-env-var-discovery.md` — clarify discovery returns templates, not auto-injected names
- `internal/content/workflows/recipe.md:675` — rewrite project-vars auto-inject claim
- `internal/recipe/refinement_suspects_run29_test.go` — gate is looking for the wrong pattern; rule and test both need rewrite
- Existing pin tests across `internal/recipe/` that bake "self-shadow"
  language as a positive teaching example will need updating

---

## Issue 3 — `start: zsc noop --silent` on dev runtimes is stale

### Owner feedback

```
# `zsc noop --silent` keeps the container alive without binding
# the runtime to a foreground process — the dev container is a
# remote-development workspace. SSH in and run `npm run start:dev`
# (Nest's watcher) by hand; source edits over SSHFS rebuild in
# place, no redeploy.
start: zsc noop --silent
```

> "This isn't needed anymore for quite some time already. Would be nice
> to not propagate it further."

### Root cause — universal dev-runtime convention

This isn't a single bad atom — it's the universal scaffold convention
that every dev-mode dynamic runtime in every recipe gets `start: zsc noop
--silent`. The pattern is encoded across:

- `internal/recipe/content/principles/dev-loop.md:9-11` (the canonical
  principle file): *"**dev** — `start: zsc noop --silent`, NO
  `healthCheck`, `buildCommands` installs deps only."*
- `internal/content/atoms/develop-checklist-dev-mode.md:20-22`
- `internal/content/atoms/develop-first-deploy-verify.md:15`
- `internal/content/atoms/develop-dynamic-runtime-start-container.md:16`
- `internal/content/atoms/develop-dev-server-triage.md:22`
- `internal/content/workflows/recipe.md:709`
- `internal/content/workflows/recipe/phases/generate/zerops-yaml/setup-rules-dev.md:12`
- `internal/recipe/content/briefs/scaffold/content_authoring.md:170, 216`
- `internal/recipe/content/briefs/codebase-content/synthesis_workflow.md:260-275`
  (PASS exemplar for the comment)
- `internal/recipe/gate_worker_dev_server.go:36, 102` — VALIDATOR that
  refuses scaffold close-phase if dev codebase has `start: zsc noop
  --silent` without a paired `worker_dev_server_started` fact attestation

The `worker_dev_server` gate is the load-bearing one — if the start
pattern is stale, the entire "agent owns the dev process via
`zerops_dev_server`" architecture needs re-grounding.

### Codex verdict

UNCERTAIN from public-doc evidence. *"Reachable Zerops docs confirm `zsc
noop` is a valid keepalive mechanism, but no external evidence was found
proving or disproving an automatic dev-container keepalive that would
obsolete it. The project owner's 'no longer needed' claim cannot be
confirmed or denied from available sources."*

### Recommendation

Project owner is authoritative on platform behavior, so treat as
confirmed-stale. Recommend a focused `platform-verifier` test BEFORE
rewriting the dev-loop principle:

- Provision a fresh dev-mode `nodejs@22` runtime
- Deploy a zerops.yaml with `run.start` OMITTED entirely (no `zsc noop`)
- Observe container state — does it stay running, or shut down?
- If it stays running, the `zsc noop` pattern is fully obsolete and the
  whole `dev-loop.md` + worker-dev-server-gate architecture wants
  simplification (omit `start` on dev, sub-agents own the watcher via
  `zerops_dev_server` against an idle container that doesn't need
  keepalive)
- If it doesn't stay running, get from the owner what the new pattern is

### Affected files

Same list as the bullets above — broad rewrite of the dev-runtime contract.

---

## Issue 4 — execOnce per-deploy + split-keys burn-recovery is self-contradicting

### Owner feedback

```
# Migrate and seed each gate on `zsc execOnce` with a per-deploy key
# (`${appVersionId}` resolves to a fresh string every deploy), so
# each script runs exactly once per deploy across all replicas.
# `--retryUntilSuccessful` rides out the brief window where Postgres
# isn't yet accepting connections. Splitting migrate and seed into
# two keys means a failed seed doesn't burn the migrate key — the
# next deploy retries the seed but already-applied schema is skipped.
initCommands:
  - zsc execOnce ${appVersionId}-migrate --retryUntilSuccessful -- node dist/scripts/migrate.js
  - zsc execOnce ${appVersionId}-seed --retryUntilSuccessful -- node dist/scripts/seed.js
```

> "Here it's contradicting itself by saying it uses appVersionId it runs
> only once per deploy, and then right at the end it says it split the
> keys so failed seed doesn't burn the migrate key for the next
> deploy..."

### Root cause — two distinct execOnce stories cross-contaminated

The canonical principle at `internal/recipe/content/principles/init-commands-model.md:7-20`
cleanly distinguishes two key-shape models:

- `${appVersionId}` — re-runs every deploy. Fresh key every time. Only
  for idempotent work.
- Static (`bootstrap-seed`, `<slug>.<op>.v1`) — runs once per service
  lifetime. For non-idempotent work.

The burn-recovery story (`touch a file to rotate appVersionId`) appears
in TWO places in the corpus, and the two stories conflate:

- **Per-deploy keys**: a failure within ONE deploy can leave the
  `${appVersionId}-X` lock in a failed state, blocking siblings within
  THAT deploy. But the next deploy gets a fresh `${appVersionId}` — so
  "burn across deploys" is conceptually impossible.
- **Static keys**: a non-idempotent seed crashing mid-run leaves the
  static lock burned, blocking re-runs until the operator rotates the
  key (e.g., `bootstrap-seed` → `bootstrap-seed-v2`). THIS is where the
  "burn-recovery" story applies.

The bullet user objected to assembles BOTH stories into one paragraph:
the keys are `${appVersionId}-migrate` / `${appVersionId}-seed`
(per-deploy), but the rationale ("split so failed seed doesn't burn
migrate key") is the static-key story. Either:

- The keys should be static (`<slug>.migrate.v1` / `<slug>.seed.v1`),
  and the rationale stays as-is
- Or the keys are per-deploy, and the rationale should drop the
  burn-recovery framing entirely (split-keys point becomes "two
  per-deploy keys avoid the within-deploy collision the canonical
  principle warns about at lines 50-60")

The cross-contamination likely happens at the synthesis brief level —
some narrative pattern teaches the migrate/seed split alongside the
burn-recovery story without checking key-shape consistency.

### Verdicts across all three rounds

- **Round 1 codex**: CONFIRMED-WRONG. Per-deploy keys can't be "burned"
  cross-deploy because they regenerate each deploy.
- **Round 2 fresh-opus**: CONFIRM-WRONG, same reasoning. The principle
  file `init-commands-model.md` is internally consistent; the conflation
  lives in fixtures and worked examples.
- **Round 2 deeper-codex**: REFINE-PRIOR — round 1 was too sweeping.
  The "split into distinct per-deploy keys" advice IS valid for
  *within-deploy* lock-collision avoidance (per
  `init-commands-model.md:44-60` — two commands sharing one
  `${appVersionId}` collapse to a single lock; the second silently
  skips). The problematic framing is ONLY the cross-deploy claim *"next
  deploy retries the seed but already-applied schema is skipped."* That
  cross-deploy "burn-recovery" framing applies to static keys, not
  per-deploy keys. **Fix**: keep the distinct-keys rationale, retire
  the cross-deploy burn-recovery framing for per-deploy keys, retain
  burn-recovery teaching ONLY where keys are actually static.

### Affected files

- `internal/content/workflows/recipe.md:946, 1600, 2344, 2366` — burn-recovery teaching, currently bundled with mixed key-shape examples
- `internal/content/workflows/recipe/phases/deploy/init-commands.md:31-33`
  (entire "Recovering a burned execOnce key" section)
- `internal/tools/workflow_checks_claude_md_test.go:49-50, 195` —
  `If seed fails mid-insert the execOnce key is burned; touch any source
  file and redeploy to rotate appVersionId` — the test fixture teaches
  the conflation as the GOOD pattern. The pattern conflates because
  rotating `appVersionId` solves the per-deploy story but the
  "burn-recovery" framing is static-key vocab.
- `internal/tools/workflow_checks_worker_correctness_test.go:30, 41, 53`
  — same fixture conflation
- The refinement validator at
  `internal/recipe/content/briefs/refinement/derived_rules.md:92`
  (F-EXECONCE-SEMANTICS) catches `${appVersionId}` + non-idempotent seed
  command tail with prose claiming once-only — but does NOT catch the
  failure shape user flagged: per-deploy keys + burn-recovery prose with
  idempotent commands. Recommend extending the validator's prose-pattern
  set to include `burn` / `burned` / `consumed key` paired with
  `${appVersionId}` keys.

---

## Cross-cutting observations

1. **Three of four issues live in briefs, not atoms.** The atom corpus
   stays framework-neutral (per memory `feedback_no_framework_specific_atoms`)
   and is well-disciplined; the briefs are where mechanism-level
   teaching errors accrete. Synthesis briefs (`per_tier_authoring.md`,
   `synthesis_workflow.md`, `platform_principles.md`) carry worked
   examples that sub-agents copy near-verbatim, so an error in a PASS
   exemplar propagates to every run.

2. **The knowledge guides are also wrong** (Issue 2 specifically). The
   recipe-engine treats `internal/knowledge/guides/*.md` as authority
   for platform behavior — but the env-var guide carries the same
   foundational error as the briefs. The guides need ground-truth
   verification against current platform behavior, not just internal
   consistency review.

3. **Issue 1's repetition complaint is structural, not stylistic.**
   *"It keeps re-hashing the minContainers and rolling deploys over and
   over"* — the env-content brief itself has explicit anti-repetition
   rules (per_tier_authoring.md:847), but the PASS exemplar at line 301
   models a per-service form that contradicts the anti-repetition rule.
   Sub-agents copy the exemplar; the rule is invisible to them under
   exemplar pressure.

4. **The em-dash style complaint** (Issue 1, item 1) is downstream of
   the friendly-authority phrasing contract at per_tier_authoring.md:319-358
   — the contract teaches em-dash-led mechanism+reason connectives
   ("Replace with mechanism + reason: # Two containers always running
   enables rolling deploys — one serves..."). Style rule worth pinning
   when the mechanism story gets rewritten anyway.

5. **`feedback_horizontal_scaling_vs_ha.md` memory predates this run**
   and already captured the throughput-vs-HA distinction (the project
   owner's prior correction in run 19). Issue 1's recurrence suggests
   the memory wasn't applied at brief-rewrite time — the bullet
   propagated despite the memory existing. Suggests the briefing-pass
   process needs to grep memory for relevant corrections before
   shipping new PASS exemplars.

---

## Next-step recommendation

Order by stakes:

1. **Issue 2 first.** Platform-verifier test of cross-service env-var
   behavior under default `envIsolation: service`. If confirmed, this is
   the biggest rewrite. Affects: 8+ files in the corpus, several pin
   tests, and the whole "self-shadow trap" mental model.
2. **Issue 3 second.** Platform-verifier test of `run.start` omission on
   a fresh dev-mode dynamic runtime. If `zsc noop` is fully obsolete,
   the `dev-loop` principle + worker-dev-server gate need
   simplification.
3. **Issue 1 third.** Rewrite the tier-4 PASS exemplar to use the
   correct rolling-deploy mechanism (new-then-cutover, `temporaryShutdown:
   false` default), drop the `maxRam` hallucination, and apply the
   anti-repetition rule to the exemplar itself.
4. **Issue 4 fourth.** Decide per-deploy vs static key shape, then keep
   exactly ONE rationale per key shape. Extend the F-EXECONCE-SEMANTICS
   validator's prose-pattern set to catch the burn-recovery + per-deploy
   conflation.

Codex agent IDs for follow-up:
- Round-1 codex: `a07cc683b3e1c7c1b`
- Round-2 deeper-codex: `ab9835fa3b0525a47`
- Round-2 fresh-opus: `a0b6a92016b41880e`

All three are still warm (SendMessage to continue with full context).
