# Run-33 — full failure inventory + golden side-by-side

**Sibling to** [run-33-analysis.md](run-33-analysis.md). This doc digs the
two channels the analysis-doc skipped over: (1) every tool-call failure
classified by root cause, and (2) defect-by-defect comparison against
the user's run-32 defect list with golden references.

**Validation:** all numbers below independently verified against
artifacts; one F-54 PASS verdict in run-33-analysis.md was wrong and
is corrected here.

---

## Part 1 — Every tool-call failure, classified

Total `is_error=true` tool-results across 13 sub-agents: **30 failures**
(plus 1 harness-block, 1 Bash exit-2). Down from run-32's
~50+ across 11 sub-agents.

### Failure inventory by class

| # | Class | Count | Sub-agents | Root cause | Verdict |
|---|---|---|---|---|---|
| **F1** | scaffold close-phase `fact-rationale-missing` | 5 | scaffold-api×2, scaffold-app×2, scaffold-worker×1 | Engine emits `field_rationale` shells per yaml directive group; agent must record rationale facts to satisfy each. The retries are the F-48 batch-fix discipline gap — agents fix one violation, retry, see N-1, repeat. | F-48 brief teaching landed but discipline didn't fully internalize; retry rate is 2-3 round-trips per scaffold codebase (down from run-32's 4-5). PARTIAL improvement. |
| **F2** | codebase-content close-phase `kb-citation-missing` | 6+ | cc-api×2, cc-worker×3, refinement×1 | NEW validator class (not in run-32 baseline): when KB body mentions platform-feature words like `execOnce`, `appVersionId`, `minContainers`, `SIGTERM`, `subdomain`, validator demands an inline `[...](docs URL)` citation. Agent's first-pass KB authoring didn't cite; iterations added citations. | NEW class in run-33; validator fired predictably. Brief teaching for KB-citation requirement should land in cc-content brief. **Add to fix list.** |
| **F3** | record-fragment IG body cap (≤30) | 3 | cc-worker×2 (got 33, got 31), cc-app×1 (got 34) | F-50 surface-caps callout in env-content brief lists `intro` + `import-comments` caps but **not the codebase-content IG body 30-line cap**. Agents overshot by 1-4 lines. | Confirmed F-50 gap. **One-line fix.** |
| **F4** | record-fragment unknown fragmentId (slot names) | 6 | env-content×6 (one parallel batch) | F-49 hostname principle landed in 4 composers. env-content agent issued one batch of 6 calls with `env/0/import-comments/{apidev,apistage,appdev,appstage,workerdev,workerstage}` — all slot names. Errored, recovered with bare names + service names. Clean for env/1 from first call. | F-49 PARTIAL FAIL — principle didn't reach the env-content sub-agent's effective context for fragmentId path shape. Diagnose composer wiring. |
| **F5** | record-fragment "noun-phrase slug citation" (V3) | 1 | cc-worker | Agent authored `'rolling-deploys' reference covers` shape (V3 violation: slug name as link text). Validator caught at write time. | V3 teaching landed at refinement layer; cc-worker tried it at authoring time. Refinement re-fixed it. PARTIAL — wants brief-side V3 teaching for cc-content. |
| **F6** | record-fragment classification required (CODEBASE_KB) | 1 | refinement | Validator demands `classification: platform-invariant\|intersection` on CODEBASE_KB fragments; agent omitted. | One-off. NOT a pattern. |
| **F7** | env-content close-phase `env-import-missing` | 2 | env-content×2 | Agent called complete-phase before all 6 tier yamls were stitched. Env-content does its writes in parallel; the close-phase guard fired before the agent finished. | Discipline retry; not a brief gap. |
| **F8** | scaffold close-phase `worker-dev-server-not-started` | 1 | scaffold-api | Validator wants `worker_dev_server_started` fact when start uses `zsc noop --silent`. Agent missed on first try; recorded fact and re-validated. | One-off self-corrected. Not a regression. |
| **F9** | recipe-content lookup (wrong slug) | 2 | scaffold-app (`svelte-static-hello-world`), scaffold-worker (`managed-services-nats`) | Agent called `zerops_recipe action=recipe-content recipe=<bogus-slug>` to fetch reference recipes that don't exist. **Conflated knowledge-corpus lookup (`zerops_knowledge query=managed-services-nats`) with recipe-template fetch.** Engine returned the available-list to redirect. | NEW class in run-33. Brief teaching gap: `zerops_recipe action=recipe-content` is for live-published recipes, not for corpus lookup. |
| **F10** | preprocessor expansion failed | 2 | features-backend (`<@db_password>`, `<@db.password>`) | Agent tried to use `<@...>` preprocessor syntax (the `<@generateRandomString(<32>)>` shape) for cross-service variable references. Canonical is `${db_password}`. Confused two distinct platform-yaml mechanisms. | NEW class in run-33. Brief teaching gap: distinction between `${...}` env-refs and `<@...>` preprocessor functions. |
| **F11** | F-52 `nothing to commit` | 2 | features-backend, features-frontend | F-52 pre-check landed in feature briefs (line 186) AND scaffold composer wiring missed it for scaffold briefs at runtime. features-frontend issued 6 naked `git add ... && git commit` shells DESPITE the brief carrying the pre-check. | Both composer-wiring + discipline gaps. |
| **F12** | F-54 wrong-path `ls` (refinement) | 1 (Bash exit-2) | refinement | Refinement agent ran `ls /var/www/zcprecipator/nestjs-showcase/api 2>/dev/null; ... /app 2>/dev/null; ... /worker 2>/dev/null`. Each path uses (a) wrong slug-prefix prefix (`zcprecipator/nestjs-showcase` doesn't exist; mounts are at `/var/www/<slot>dev/`), and (b) bare codebase names instead of slot names. The `2>/dev/null` swallowed the `No such file or directory` stderr; only exit code 2 surfaced. **My run-33-analysis.md F-54 PASS verdict was WRONG — codex flagged it implicitly via PARTIAL-on-#9; the actual repro is here.** | F-54 PARTIAL FAIL. The refinement brief's hostname-vs-slot teaching didn't reach the agent. |
| **F13** | scaffold-worker harness-blocked `sleep 25` | 1 | scaffold-worker | Agent ran `sleep 25 && echo "waited"` to wait on stage cross-deploy. Claude harness blocks sleeps >300s and chained-shorter-sleeps; this triggered the block. | Brief should teach `until <cond>; do sleep N; done` or `Monitor` tool for waits. |

### Cumulative impact

| Severity | Class | Count | Brief vs engine vs discipline |
|---|---|---|---|
| HIGH | F2 (kb-citation-missing) | 6 | NEW validator → needs brief teaching |
| HIGH | F4 (env-content slot-name fragmentId) | 6 | F-49 partial → composer wiring |
| MED | F3 (IG body cap overrun) | 3 | F-50 gap → callout extension |
| MED | F1 (fact-rationale missing iterations) | 5 | F-48 partial → discipline |
| MED | F11 (nothing to commit) | 2 | F-52 two-gap regression → composer + discipline |
| MED | F12 (wrong-path ls in refinement) | 1 | F-54 partial → brief teaching didn't reach |
| MED | F9 (recipe-lookup slug confusion) | 2 | NEW → brief teaching gap |
| MED | F10 (preprocessor syntax confusion) | 2 | NEW → brief teaching gap |
| LOW | F7 (env-import-missing iterations) | 2 | discipline only |
| LOW | F8 (worker-dev-server-not-started) | 1 | one-off self-corrected |
| LOW | F5 (noun-phrase slug citation) | 1 | refinement caught |
| LOW | F6 (classification required on KB) | 1 | one-off |
| LOW | F13 (harness sleep block) | 1 | brief gap |

---

## Part 2 — Side-by-side defect comparison

For each user-named run-32 defect: golden anchor → run-32 state → run-33 state → verdict.

### D1 — Tiers mentioned on codebase surfaces

**Run-32:** "tier 0", "HA tier" referenced in apidev README + zerops.yaml.
**Golden (jetstream apps-repo):** zero tier name mentions in README/yaml.
**Run-33:** **1 hit.** [workerdev/README.md:249](docs/zcprecipator3/runs/33/workerdev/README.md#L249) — `"The recipe runs the worker at `minContainers: 2` from production tiers onward"`. One mention, in a KB body explaining cause-and-effect of `minContainers: 2`. Not a tier-name leak per se ("production tiers" as collective adjective, not "tier 4 specifically").

**Verdict:** **PARTIAL CLOSE.** Run-32 referenced specific tiers by name; run-33 has one collective-adjective reference ("production tiers"). Down from multi-mention to one. Acceptable threshold? User's stance was "tier names belong to env-content only" — strictly violated, but the mention is unavoidable when narrating multi-replica behavior. Could be re-phrased as `"at minContainers: 2 and above"` (substance preserved, tier reference dropped).

### D2 — Internal guide slug names leaking into porter content (×25 in run-32)

**Run-32:** `init-commands`, `env-var-model`, `managed-services-nats` appearing as bare slug names ×25 instances.
**Golden (jetstream):** zero corpus-slug-as-link-text references in 291-line README. Link text is `[Laravel Jetstream]`, `[step-by-step tutorial]`, `[multi-container setups]`.
**Run-33:** **7 hits in published markdown** but the SHAPE has shifted. All seven are now real markdown links with English-cased descriptive labels:
- `[Zerops managed NATS service](https://docs.zerops.io/services/managed-services/nats)` — link to the docs page; descriptive label, but the slug `managed-services/nats` survives in the URL path
- `[per-deploy init-commands reference](https://docs.zerops.io/zerops-yaml/specification#initcommands-)` — descriptive label preserves "init-commands" as fragment
- `[S3-compatible object storage on Zerops](https://docs.zerops.io/services/object-storage)` — descriptive
- `[Zerops rolling-deploys reference](https://docs.zerops.io/features/scaling-ha)` — descriptive label, "rolling-deploys" is the corpus slug verbalized

**Verdict:** **PARTIAL CLOSE.** Counter #2 reads zero on the strict run-32 regex (`[managed NATS service]` etc.) but the AGENT EVOLVED a new shape: `[Zerops <slug-derived-phrase> reference]`. This is semantically less bad than run-32 (URLs work; descriptions help) but the slug skeleton still surfaces in link text on `init-commands`, `rolling-deploys`. Aspirational reference uses `[Zerops scaling and HA reference]` for rolling-deploys to fully detach from slug.

**Counter #2 escape:** the regex set in [run-32-phase-1-baselines.md](run-32-phase-1-baselines.md#L75-L91) needs a follow-up pattern matching `Zerops .* reference` and slug-fragment-in-URL-path leakage to catch this evolved shape.

### D3 — Nothing in KB / gotchas; everything stuffed into IG

**Run-32:** KB ships as flat one-liner bullets; substantial mechanism stories (Authorization Violation trap, JWT self-shadow) crammed into IG yaml comments.
**Golden (jetstream):** KB has 2 H3 sub-headings + paragraphs + `> [!CAUTION]` callout for destructive ops. Showcase: 7 H3'd intersection-trap items.
**Run-33:** KB substance is **at-bar with showcase** but shape is **shallower than jetstream**:

| Codebase | KB items | Item shape | H3 sub-headings | CAUTION callout | Fenced code |
|---|---|---|---|---|---|
| apidev | 7 | flat bullet, full mechanism+effect+fix prose | NO | NO | NO |
| appdev | 4 | flat bullet, full mechanism+effect+fix prose | NO | NO | NO |
| workerdev | 5 | flat bullet, full mechanism+effect+fix prose | NO | NO | NO |
| jetstream (golden) | 2 | H3 + paragraphs + callout | YES | YES | YES (`zsc health-check disable` shell sequence) |
| showcase (golden) | 7 | H3 + paragraphs | YES | NO | NO |

Each run-33 KB bullet IS a fully-formed mechanism+effect+fix narrative — substance matches showcase. But the H3+paragraph+CAUTION shape is missing.

**Verdict:** **SUBSTANTIVE FIX, SHAPE PARTIAL.** Substance closed: KB bullets carry the heavy intersection-trap content the user wants. Shape didn't promote. Run-33's KB bullets are 4-7 lines of dense prose each — would benefit from H3+paragraph break-out for skimmability + CAUTION callout for destructive items (like the `synchronize: true` worker bullet — that IS destructive and warrants `> [!CAUTION]`).

### D4 — Excessive yaml comments, unfriendly for humans (run-32: 56-63%)

**Golden (jetstream):** 36% comment density.
**Run-33:** apidev **50.0%** / appdev **59.4%** / workerdev **51.8%**.

| Codebase | Run-32 | Run-33 | Δ | Vs golden 36% |
|---|---|---|---|---|
| apidev | 58% | 50% | -8pp | +14pp |
| appdev | 63% | 59% | -4pp | +23pp |
| workerdev | 57% | 52% | -5pp | +16pp |

**Verdict:** **MARGINAL.** All three codebases dropped 4-8 percentage points. None at golden bar. appdev still highest at 59%. The brief composer's `Y14 (MECHANISM+EFFECT+SO-WHAT)` rule pulls toward verbose comments; rule is principle-shaped but the agent applies it uniformly to every directive instead of selectively. Need a "comment density target" or "comment-when-non-obvious" cap rule.

### D5 — IG #4 "Alias cross-service env vars" (convention not platform-forced)

**Run-32:** All three codebases used "Alias cross-service env vars under your own keys" as IG #4 — a convention, not Zerops-forced.
**Golden (jetstream):** IG #2 = "Add Support For Object Storage" (composer require + config edit). IG #3 = "Utilize Environment Variables" (Zerops-forced). IG #4 = "Setup Production Mailer" (recipe-specific platform integration).
**Run-33:**

| Codebase | IG #4 topic | Verdict |
|---|---|---|
| apidev | "Drain on SIGTERM for rolling deploys" | **GOOD** — runtime-correctness, platform-relevant |
| appdev | "Strip the build-output prefix when shipping to `base: static`" | **GOOD** — Zerops-forced (specific to `base: static`) |
| workerdev | **"Alias cross-service env vars under your own keys"** | **BAD** — same defect as run-32; convention not platform-forced |

**Verdict:** **2 OF 3 CLOSED.** apidev + appdev moved off the bad topic. workerdev retained it, with full 30-line body that's mostly repeat of platform mechanics the porter learns elsewhere. The brief teaching landed unevenly across sub-agent codebase choices.

### D6 — Intro leaks recipe-internal wiring

**Run-32:** apidev intro carried "Mounts under /api; JWT-ready via JWT_SECRET" — deployment wiring in the standalone-app description.
**Golden (jetstream apps-repo):** "An app showcasing how to integrate Laravel Jetstream apps with the Zerops platform." Pure framework + integration. Zero deployment wiring.
**Run-33:**
- **apidev:** "NestJS REST API serving Items CRUD with Postgres persistence, a Valkey read-through cache, NATS job publishing, S3-compatible object storage, and Meilisearch full-text search."
- **appdev:** "Svelte 5 + Vite SPA serving the showcase dashboard — Items CRUD, cache probe, queue publisher, file upload, and search across one screen, talking exclusively to the api codebase over `fetch`."
- **workerdev:** "NestJS standalone-context background worker consuming NATS messages on subject `jobs.process` and persisting processed job records to Postgres via TypeORM."

**Verdict:** **CLOSED.** All three intros are clean porter-grade descriptions naming framework + feature set. Zero deployment-wiring leakage (no mount paths, no env-var-alias names, no port numbers). Best-in-class section of the run.

### D7 — Cross-language adapt-paths (Python uvicorn, Go http.ListenAndServe in NestJS recipe)

**Run-32:** apidev IG #2 listed Python `uvicorn` + Go `http.ListenAndServe` adapt-paths.
**Golden (jetstream):** zero cross-language references in 291-line README.
**Run-33:** **0 hits** on published codebase surfaces (`grep -nE '\b(uvicorn|gunicorn|RemoteIPHeader|http\.ListenAndServe|Lumen|Symfony|Django|Flask|Rails|Phoenix|fiber|gin-gonic|actix)\b'`).

**Verdict:** **CLOSED.** Same axis as Counter #3.

### D8 — Factuality bug: app declared as `nodejs@22` (Stage import.yaml)

**Run-32:** `3 — Stage/import.yaml` declared `app type: nodejs@22` while runtime base is `static`. Engine bug; service type didn't match runtime base.
**Run-33:** All five tiers that ship the app service declare:
```yaml
- hostname: app
  type: static
  zeropsSetup: prod
  buildFromGit: https://github.com/zerops-recipe-apps/nestjs-showcase-app
```
across `2 — Local`, `3 — Stage`, `4 — Small Production`, `5 — Highly-available Production`. Tiers `0 — AI Agent` and `1 — Remote (CDE)` don't include the app service (those are dev environments).

**Verdict:** **CLOSED.** Type matches runtime base (`static` everywhere).

### D9 — Root taxonomy drift ("Include Coding Agents" vs "AI agent")

**Run-32:** Tier names drifted to "Include Coding Agents" / "Include Cloud IDE".
**Run-33:** Tier directory names + root README links use canonical:
```
0 — AI Agent
1 — Remote (CDE)
2 — Local
3 — Stage
4 — Small Production
5 — Highly-available Production
```

**Verdict:** **CLOSED.**

### D10 — IG steps lack concrete repo-file anchors

**Run-32:** Generic IG prose; no links to `composer.json#L14` etc.
**Golden (jetstream IG #2):** `[league/flysystem-aws-s3-v3](https://github.com/zerops-recipe-apps/laravel-jetstream-app/blob/main/composer.json#L14)` — links to exact file + line.
**Run-33:**

| Codebase | Repo-file links (excluding docs URLs) | Status |
|---|---|---|
| apidev | 0 | **NOT FIXED** |
| appdev | 3 (`vite.config.js`, `zerops.yaml` ×2) | **PARTIAL** |
| workerdev | 0 | **NOT FIXED** |

appdev's IG body links to `[vite.config.js](vite.config.js)` and `[zerops.yaml](zerops.yaml)` — unanchored to specific lines but at least naming the file. apidev + workerdev IGs are pure prose with docs.zerops.io links only.

**Verdict:** **PARTIAL.** appdev took the file-link shape; apidev + workerdev did not. The codebase-content brief teaching for IG-3 ("link to specific apps-repo lines") didn't reach two of three codebases.

### D11 — Missing concept-bridge between IG and KB

**Golden (jetstream):** Between IG end and KB start lives `## Understand Zerops Core Concepts` linking to the framework's Zerops tutorial.
**Run-33:** **Zero text** between `<!-- #ZEROPS_EXTRACT_END:integration-guide# -->` and `<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->` markers across all three codebases.

**Verdict:** **NOT FIXED.** Same gap as run-32. Brief composer doesn't author this bridge section; engine doesn't synthesize it.

### Bonus — Recipe Features + Production-vs-Development sections (PD1, RF1)

**Golden (jetstream apps-repo):** has both `## Recipe features` (8-bullet) + `## Production vs. Development` (3-bullet HA upgrade map).
**Aspirational reference:** added "Production vs. Development" to root README.
**Run-33:** **0 of 4 surfaces** carry either section (root README + 3 codebases).

**Verdict:** **NOT FIXED.** Continues the run-32 Defect #2 ("No Production vs. Development map"). Aspirational reference closed it manually; run-33 didn't reproduce that fix.

---

## Part 3 — Summary verdict matrix

| Defect | Run-32 | Aspirational | Run-33 | Verdict |
|---|---|---|---|---|
| D1 tiers on codebase surfaces | multi | 0 | 1 ("production tiers" collective) | **PARTIAL** |
| D2 slug-name leakage | 25 | 0 | 7 (evolved shape: descriptive-with-slug-fragment) | **PARTIAL** |
| D3 KB shape | flat | H3+CAUTION | flat (substance at-bar) | **SUBSTANCE PASS, SHAPE PARTIAL** |
| D4 yaml comment density | 58/63/57% | <40% (impl) | 50/59/52% | **MARGINAL** (none at golden) |
| D5 IG #4 topic | bad ×3 | good ×3 | good ×2, bad ×1 (worker) | **2/3 PASS** |
| D6 intro leak | wiring leakage | clean | clean ×3 | **PASS** |
| D7 cross-language adapt-path | many | 0 | 0 | **PASS** |
| D8 app type vs base | mismatch | type=static | type=static | **PASS** |
| D9 root taxonomy | drifted | canonical | canonical | **PASS** |
| D10 IG repo-file anchors | none | many | partial (appdev only) | **1/3 PASS** |
| D11 IG-KB concept bridge | missing | present | missing | **NOT FIXED** |
| RF1 + PD1 sections (root) | missing | present | missing | **NOT FIXED** |

**Summary:** 5 full-PASS, 3 PARTIAL, 2 substance-PASS-shape-PARTIAL, 2 NOT FIXED. The pre-flight + iteration-cost fixes closed 5 defect classes outright, moved 5 to PARTIAL, and 2 are untouched (concept-bridge, RF/PD sections — both because no brief or engine actor authored them).

---

## Part 4 — Updated next-intervention list

Beyond the three brief edits in [run-33-analysis.md](run-33-analysis.md):

1. **(F-50 IG body cap)** — extend surface-caps callout to include `codebase/<host>/integration-guide/<n> body ≤30 lines`. ~10 min. Closes F3.
2. **(F-49 env-content composer)** — verify principle reaches env-content brief slice; add worked BAD/GOOD pair for fragmentId path shape. ~30 min. Closes F4.
3. **(F-52 two-gap)** — composer wiring for scaffold + discipline shell wrapper. ~50 min. Closes F11.
4. **(NEW) KB-citation teaching** — the `kb-citation-missing` validator is new and predictable. Teach the cc-content brief: "if KB body mentions `execOnce`, `appVersionId`, `minContainers`, `SIGTERM`, `subdomain`, `forcePathStyle`, etc., include a real markdown link `[label](docs URL)`." ~20 min. Closes F2.
5. **(NEW) F-9 + F-10 brief teachings** — distinguish `zerops_recipe action=recipe-content` (live recipe lookup) vs `zerops_knowledge query=<slug>` (corpus). Distinguish `${...}` env-refs vs `<@...>` preprocessor. ~30 min. Closes F9 + F10.
6. **(F-54 actually broken)** — refinement Bash hostname-vs-slot teaching. Already in `refinement/synthesis_workflow.md` per F-54 spec, but the agent ran `ls /var/www/zcprecipator/nestjs-showcase/<bare>` (extra slug-prefix on top of bare names). Either teach the path shape explicitly (`/var/www/<slot>dev/` is the mount root), or check for `zcprecipator/<slug>/` prefix as a separate trap. ~20 min.
7. **(D5 worker IG#4)** — worker IG#4 retained the bad "Alias cross-service env vars" topic. Move to a worker-relevant Zerops-forced topic: "Connect to managed NATS broker with separate credentials" (already IG #5 — could promote/swap), or "Disable HTTP probes for non-listening services" (worker has no `ports`/`healthCheck` — that IS Zerops-forced behavior worth IG-ing). ~15 min editorial decision.
8. **(D2 evolved slug shape)** — extend Counter #2 regex + V3 rule to forbid `Zerops <slug-derived-phrase> reference` shape; teach descriptive labels like `[Zerops scaling and HA reference]` (per aspirational CHANGES.md). ~10 min.
9. **(D11 + RF1 + PD1)** — these need an authoring actor. Currently the finalize sub-agent emits the root README without RF1/PD1 sections; codebase-content sub-agents don't author the IG-KB concept-bridge. Decide who owns these and add to the right phase. ~1 hour design + edit.
10. **(D4 yaml density)** — comment-density target rule (≤40%); brief teaches "comment when non-obvious; field-name self-doc means skip." ~30 min editorial.

Total ~4 hours of brief edits before next dogfood.

---

## Part 5 — Corrections to run-33-analysis.md

Three findings need updating in the sibling analysis doc:

1. **F-54 PASS → PARTIAL FAIL.** Refinement agent ran `ls /var/www/zcprecipator/nestjs-showcase/{api,app,worker} 2>/dev/null` and got Exit code 2. The `2>/dev/null` swallowed the `No such file or directory` stderr; my regex missed it. Same trap as run-32 F-54.

2. **Counter #2 = 0 understates D2.** Counter regex was tuned to run-32 specific shape (`[managed NATS service]`); run-33 evolved into `[Zerops <slug-phrase> reference]` shape that escapes the regex. Defect class isn't fully closed; counter needs a follow-up pattern set.

3. **NEW failure class F2 (kb-citation-missing) wasn't on the run-32 radar.** Added 6 errors and one cycle of refinement. Brief teaching gap for kb-citation requirement should be on the next intervention list.

These don't change the headline (pre-flight worked, brief edits over fresh dogfood) but tighten the residual list.
