# Quality rules inferred from laravel-jetstream

**Method:** Read every published surface of `/Users/fxck/www/recipes/laravel-jetstream/` (recipe-repo) + `/Users/fxck/www/laravel-jetstream-app/` (apps-repo). Extract rules grounded in **what jetstream visibly does** — not what we wish it did. Each rule has a citation. Cross-checked against candidate `docs/zcprecipator3/simulations/32-pattern2-fix-1/` to surface violations.

**Scope:** Single-codebase Laravel-Jetstream golden. Quality rules are codebase-count-agnostic; codebase-count is structural shape, not a quality axis.

**Output:** A portable rule list. Refinement (or any judge) can score against it without needing the recipe in context.

---

## Universal voice rules (apply to all surfaces)

| # | Rule | Cited evidence | Candidate violation |
|---|---|---|---|
| V1 | **Audience is the porter who clones-and-runs.** Voice frames content as "this app uses Y for Z" or "this showcases how to integrate X with Zerops". Never "we chose", "during scaffold", "the agent". | jetstream apps-repo README:4 — "This showcases how to best integrate Jetstream apps with Zerops" | candidate yaml comments use "the recipe sets ... at provision time"; facts.jsonl carries "the agent owns" / "the recipe" verbatim |
| V2 | **Names what the recipe IS, not what it could be.** Names the specific framework + the specific feature set. Never "any Node HTTP framework", "alternative starter kits", "if you use Symfony instead". | jetstream apps-repo README:4 — "Laravel Jetstream is an advanced starter kit by Laravel". Zero alternatives mentioned in 291 lines. | candidate apidev/README.md:217-220, :241-245, :283-285, :327-332 — Express/Fastify/Plain-Express adapt-paths in 4 IG items |
| V3 | **Link text references real concepts the porter recognizes**, not internal corpus slugs verbalized. | jetstream uses `[Laravel Jetstream]`, `[step-by-step tutorial]`, `[zsc health-check]`, `[Laravel documentation]`, `[multi-container setups]`. Zero references to internal taxonomy. | candidate apidev/README.md:343,346 — `[managed NATS service]`, `[Zerops object-storage service]` (English-cased slugs); workerdev/README.md:312-314 — `[Zerops env-var model]` |
| V4 | **Porter-actionable phrasings tied to platform signal.** "Feel free to change this value", "Configure this to use real SMTP sinks", "If you want to try integrating from scratch, check our tutorial". Imperatives + customization invitations. | jetstream apps-repo zerops.yaml:56-58 — "Feel free to change this value to your own custom domain, after setting up the domain access" | candidate yaml comments mostly state facts ("npm ci for reproducible builds"); few invitations |
| V5 | **Defers to docs by URL when concept is broad.** Doesn't try to teach platform internals in-line. | jetstream apps-repo zerops.yaml:43-45 — "Read more about it here: https://docs.zerops.io/php/how-to/customize-web-server" (4 instances across the file) | candidate yaml inlines decision-rationale paragraphs (apidev/zerops.yaml:62-69 — execOnce decoupling explained in 8 lines of comment) |
| V6 | **No authoring vocabulary in any porter-facing surface.** No `zerops_dev_server`, `zsc noop`, "the agent", "scaffold", "feature phase", "recipe author". | jetstream zerops.yaml + README — zero hits across all 7 KB | candidate facts.jsonl — 17 records carry these tokens; leaks to environments/1/import.yaml:23 |

---

## Recipe-repo root README rules

The root README is a **6-tier entry point**, not a documentation surface.

| # | Rule | Cited evidence |
|---|---|---|
| R1 | **~28 lines total.** Single intro paragraph + deploy button + cover image + 6-tier link list + catalog punt + Discord. | jetstream README.md:1-28 |
| R2 | **Intro paragraph is one sentence + "with all the essentials" line.** Names the framework + names what it demonstrates. | "A showcase of running Laravel Jetstream — an advanced starter kit by Laravel — on Zerops, with all the essentials for modern development: login flow, 2FA, sessions, queues, mailing, and cloud-ready file storage." |
| R3 | **6 tier links have uniform shape.** Both `[info]` and `[deploy with one click]` for every tier. | README.md:16-21 |
| R4 | **Trailing punt to the recipe catalog**, not in-line teaching. | "For more advanced examples see all PHP recipes on Zerops." |
| R5 | **Trailing Discord invite.** | "Need help setting your project up? Join Zerops Discord community." |
| R6 | **What's NOT here:** no IG, no KB, no architecture description, no managed-services list, no codebase orientation. The root README is purely an ENTRY POINT. | jetstream README.md has zero `##` H2 sections. |

**Candidate state:** root README is 1.9 KB (within bar) and structurally close. Multi-codebase orientation gap (Defect #6) is real but doesn't require breaking R6 — fits inside the intro paragraph, not as new sections.

---

## Tier README rules

Tier READMEs are **8-line delta descriptions**.

| # | Rule | Cited evidence |
|---|---|---|
| T1 | **8 lines total** (jetstream Stage README is 8 lines). Title + one-sentence framing + 2-3 line intro extract. | recipes/laravel-jetstream/3 — Stage/README.md |
| T2 | **Intro extract names this tier's DELTA**, not its absolute spec. | "Stage environment uses the same configuration as production, but runs on the lowest scaling settings." |
| T3 | **Title links back to recipe deploy page.** | `[Laravel Jetstream (info + deploy)](https://app.zerops.io/recipes/laravel-jetstream?environment=stage)` |
| T4 | **No tier-promotion narrative.** Tier README does NOT say "promote to tier 5 when X" — see also rubric R1-RC-7 anchor. | Jetstream tier READMEs describe their own purpose; do not frame tier-N as a stepping-stone to tier-(N+1). |

**Candidate state:** Defect #13 (yaml-field jargon in tier intros — "minFreeRamGB: 0.25 adds a spike buffer") violates V4 — leaks engine vocabulary instead of porter-actionable framing.

---

## Tier import.yaml rules

Per-tier import.yaml carries comments that explain **service role in this recipe**, not field documentation.

| # | Rule | Cited evidence |
|---|---|---|
| TY1 | **Top-of-file 2-line comment** describes this tier's purpose. Same content as the tier README intro extract. | recipes/laravel-jetstream/3 — Stage/import.yaml:3-4 |
| TY2 | **Per-service comment is 1-2 sentences naming SERVICE ROLE in this recipe.** | ":24-25 — `Deploy single node PostgreSQL database, used by the Laravel app to store data.`" |
| TY3 | **Optional services are explicitly marked optional.** | ":47-50 — `Optionally, spin up the single-service email and SMTP testing tool. Feel free to remove this service, if you wish to stage-test your app with as-close-as-possible production setup.`" |
| TY4 | **Comments name framework-canonical effects.** "Used by the Laravel app" — not "consumed by the api codebase" or engine-internal vocabulary. | ":24-25 — "used by the Laravel app to store data" |
| TY5 | **Service ordering with `priority` justified in human terms when non-default.** | ":21-22 — `Set higher priority for databases and storages, because the app depends on those services.`" |

---

## Apps-repo README rules

Apps-repo README carries the IG and KB. Both extract via `<!-- #ZEROPS_EXTRACT_START:* -->` markers.

### Integration Guide rules

| # | Rule | Cited evidence | Candidate violation |
|---|---|---|---|
| IG1 | **IG #1 is "Add `zerops.yaml`" with verbatim yaml block.** Engine-emitted; agent does not author this slot. | jetstream apps-repo README:22-206 | n/a — engine-emitted |
| IG2 | **IG #2-#N are 1-4 line steps.** Jetstream's IG #2 is 4 lines (composer require + config edit). #3 is 2 lines. #4 is 1 paragraph. **Each step does one thing.** | apps-repo README:208-219 | candidate apidev/README.md IG #4 is **40 lines**, IG #5 is **53 lines**. Worker IG #2-#5: 34 / 49 / 56 / 70 lines. |
| IG3 | **IG body links to specific apps-repo GitHub lines.** Concrete, ungeneralized. | `[league/flysystem-aws-s3-v3](https://github.com/zerops-recipe-apps/laravel-jetstream-app/blob/main/composer.json#L14)` — links to `composer.json#L14` | candidate IG bodies link to internal corpus slugs verbalized |
| IG4 | **No cross-framework adapt-paths.** Stays Laravel-Jetstream-specific across all 4 IG items. Zero "if you use Symfony" / "any PHP framework". | All 291 lines of jetstream apps-repo README | candidate apidev/README.md:217-220 "Adapt path: any Node HTTP framework"; :241-245 "Fastify uses `trustProxy: true`"; :283-285 "dotenv-flow"; :327-332 "Express's `cors` middleware, Fastify's `@fastify/cors`" |
| IG5 | **IG step framing is "do this thing", not "understand this concept".** | "### 2. Add Support For Object Storage / - add `league/flysystem-aws-s3-v3` to your composer.json" — directly actionable | candidate IG #4 "Alias cross-service env vars under your own keys" — 40-line concept tutorial including same-key shadow trap explanation |

### "Recipe features" + "Production vs. Development" rules

| # | Rule | Cited evidence | Candidate violation |
|---|---|---|---|
| RF1 | **"Recipe features" is a bulleted list of ~8 bullets** framed by what the recipe SETS UP, not what porters can do. | apps-repo README:230-237 — 8 bullets, all "X service" or "Setup for Y" | candidate has no equivalent section |
| PD1 | **"Production vs. Development" is 3 bullets max.** Names HA migration paths the porter takes when scaling. | apps-repo README:241-247 — exactly 3 bullets (HA db, minContainers≥2, real SMTP) | candidate has no equivalent section (Defect #2). Scattered across 6 tier intros + import-comment blocks. |

### Knowledge Base rules

| # | Rule | Cited evidence | Candidate violation |
|---|---|---|---|
| KB1 | **KB items use H3 heading + paragraph(s) + optional callout/fenced shell.** | apps-repo README:253-282 — 2 KB items, both `### Heading` + paragraph(s) + (CAUTION callout / fenced shell where applicable) | candidate apidev/README.md:341 — multi-paragraph mechanism crammed into one flat bullet (Defect #3); workerdev = `## Knowledge base` H2; appdev = `### Gotchas`. KB shape inconsistent across siblings (Defect #11). |
| KB2 | **KB items address ad-hoc operational concerns**, not "how the recipe was built". | jetstream KB items: Maintenance Mode, Temporary Upscaling — both ops-time tasks the porter performs | candidate KB has stronger intersection-content (NATS URL-cred, MinIO 403, TypeORM race) but mixes mechanism explanations with self-inflicted teaching (Defect #15) |
| KB3 | **KB stem is symptom-first OR directive-tightly-mapped.** | Jetstream KB sub-headings are concept-named (`Maintenance Mode`, `Temporary Upscaling`) — directive-mapped to operational task | candidate KB stems are mostly symptom-first (good substance) but flat-bullet shape violates KB1 |
| KB4 | **`> [!CAUTION]` callouts are reserved for porter-destructive operations.** | apps-repo README:259-270 — CAUTION around `php artisan down` + health-check disable sequence | n/a |
| KB5 | **Fenced shell blocks show the porter's command sequence**, not platform-internal commands. | apps-repo README:264-269 — `zsc health-check disable` / `php artisan down` / `php artisan up` — porter's hands-on sequence | candidate yaml comments embed shell-fragment-level decisions in prose, not in fenced blocks |

---

## Apps-repo zerops.yaml rules

| # | Rule | Cited evidence | Candidate violation |
|---|---|---|---|
| Y1 | **Top-of-file block comment names the setup pattern.** ~5 lines. Names what each setup IS. | jetstream zerops.yaml:1-5 — "This example app uses two setups: 'prod' for ..., 'dev' for ..." | candidate apidev/zerops.yaml has top comment but immediately leaks engine vocabulary |
| Y2 | **Per-section comment names the section's PURPOSE in the recipe.** Not the field's documentation. | jetstream zerops.yaml:7-9 — "Define shared 'base' setup to prevent duplication. Both final setups will use the same subset of environment variables..." | candidate apidev/zerops.yaml:62-69 — execOnce comment is FACT-heavy ("Two `zsc execOnce` keys decouple migration and seed lifecycles") not PURPOSE-heavy |
| Y3 | **Comments explain framework requirements that motivate Zerops choices.** | jetstream zerops.yaml:14-17 — "PHP applications generally require a web server such as Nginx or Apache HTTP server" | candidate yaml comments lecture on Node ecosystem facts (npm ci vs npm install) without framing as "this recipe chose X because of Y" |
| Y4 | **URL references for broad concepts.** "Read more about it here:" pattern. | jetstream:43-45, :117, :139, :150 — 4 URL deferrals | candidate has zero URL deferrals; tries to teach concepts in-line |
| Y5 | **Comments name PORTER-customization opportunities.** | ":56-58 — "Feel free to change this value to your own custom domain, after setting up the domain access" | candidate has few "feel free" / customization invitations |
| Y6 | **Init-command comments explain WHEN they run + WHY**, not what they are. | ":140-143 — "Run these pre-heat and migration commands, right after creating every runtime container" | candidate explains the COMMANDS but less the WHY-IN-RECIPE |
| Y7 | **Comments use framework-canonical terms only.** APP_KEY, APP_URL, BCRYPT_ROUNDS — never authoring tokens. | jetstream zerops.yaml — zero hits for `zsc noop` / `zerops_dev_server` / `the agent` | candidate apidev/zerops.yaml line 199 — `zsc noop --silent` reference (intentional yaml content) but framed in authoring voice |
| Y8 | **No tier-promotion narrative inside comments.** | jetstream comments describe THIS recipe's choices; never "promote to tier 5 when X" | candidate yamls comments are clean of tier-promotion |
| Y9 | **No decision-tree-of-alternatives.** Comments name THIS choice, not "alternatives are X, Y, Z and we picked W because..." | jetstream zerops.yaml — zero alternatives enumerated | candidate apidev/zerops.yaml:62-69 — execOnce comment is acceptable per Y9 (no alternatives), but other comments lean toward enumerating non-chosen paths |

---

## What jetstream does NOT do (anti-rules)

These are absences worth naming explicitly because the candidate keeps drifting toward them:

| # | Anti-rule | Jetstream evidence | Candidate violation |
|---|---|---|---|
| A1 | **No KB section header in apps-repo README.** Section is named `## Tips and Others`. | apps-repo README:249 | candidate workerdev = `## Knowledge base` (engine-internal name leaked) |
| A2 | **No "Adapt path:" prose in IG.** Zero instances across 291 lines. | apps-repo README | candidate has 8+ "Adapt path:" instances (Defect #18) |
| A3 | **No internal-tooling vocabulary anywhere.** | apps-repo README + zerops.yaml — zero hits | candidate has multiple |
| A4 | **No tier-promotion narrative in tier READMEs.** | recipes/laravel-jetstream/*/README.md — none | candidate flagged in run-22 R1-RC-7 |
| A5 | **No multi-paragraph yaml comments justifying every field.** Comments cluster around setups + envVariables blocks; many fields have no comment at all (the field name is self-documenting). | jetstream zerops.yaml — comment density 33% | candidate apidev/zerops.yaml — 45% comment density; many comments are facts the field-name already tells |

---

## How to use this rule list

This list is the **substrate refinement should be scoring against**. Each rule has:
1. A name (V1, R1, IG2, KB1, Y2, A2, etc.).
2. A cited piece of evidence in jetstream — the rule was extracted from observable practice.
3. (Where applicable) a candidate violation — the rule's failure shape is concrete.

Scoring against this rule list is a check-each-rule walk, not a pattern match. The rules are **principle-shaped** ("name what the recipe IS, not what it could be") with **observable anchors** ("zero alternatives mentioned in 291 lines of jetstream README"). That's the form the diagnosis Layer 2 reshape should take.

**Mapping to the candidate's 19 head-to-head defects** (from `run-32-content-quality-handoff-iter-2.md`):

| Defect # | Violated rule(s) |
|---|---|
| #1 IG over-explains | IG2, IG5, V2 |
| #2 No "Production vs. Development" map | PD1 |
| #3 KB flat bullets where shape demands H3+callout | KB1 |
| #4 Slug verbalization in link text | V3 |
| #5 IG #4 + #5 are conventions not platform-forced | IG2, IG5 |
| #6 Root README missing 3-codebase orientation | R6 (within intro paragraph) |
| #7 Wrong URL on rolling-deploys link | (factuality — outside this rule set) |
| #8 Wrong concept-bridge for Svelte | (factuality) |
| #9 Dev-tier migration race | (factuality) |
| #10 Inconsistent S3 own-keys across siblings | (cross-codebase coherence — Layer 1) |
| #11 KB shape inconsistency across siblings | KB1, A1 |
| #12 zerops_dev_server leakage in tier yaml comment | V6, Y7 |
| #13 Tier intros leak yaml-field jargon | T2, V4 |
| #14 IG cap overruns | IG2 |
| #15 Self-inflicted KB bullet | KB2 |
| #16 CLAUDE.md ships in deliverable | (out of scope per Q2) |
| #17 Duplicate `#ZEROPS_EXTRACT_*` markers | (stitching — outside this rule set) |
| #18 Cross-framework adapt-path drift | IG4, V2, A2 |
| #19 Worker DB env wired but unused | (Layer 1 cross-codebase coherence) |

**16 of 19 head-to-head defects map to a rule extracted from jetstream.** The 3 that don't (factuality + stitching artifact + CLAUDE.md scope) are orthogonal classes.

---

## Implications for the diagnosis

This rule extraction concretely supports two of the proposed attack-order steps:

- **Step 5 (refinement reshape)** — anchors should be reframed as "score against this rule list" with `principle + observable anchor` shape. The rule list above is approximately what that looks like.
- **Step 6 was misframed.** The "handcraft a multi-codebase exemplar" task collapses to "extract rules from existing goldens" — done here for jetstream. Each additional golden adds rules at the margin, but jetstream alone already covers 16/19 head-to-head defects.

**What's NOT covered by jetstream alone:**
- Multi-codebase consistency rules (Layer 1 territory; needs cross-codebase coherence enforcer at engine, not refinement).
- Factuality (URL correctness, framework-bridge correctness) — needs an authoritative URL/framework allowlist, not a rule from a golden.
- Stitching artifacts (duplicate extract markers) — engine concern.

**Layer 0 reframed:** Not "no canonical exemplar exists" — *jetstream is a canonical exemplar*. It just hasn't been mined for portable rules until now. Layer 0 closure is this document.

---

# Upper-bound rules — where laravel-showcase exceeds jetstream

Source: `/Users/fxck/www/laravel-showcase-app/README.md` (291 lines authored, plus 285-line embedded yaml). This recipe was generated by an earlier run, lands above jetstream on KB substance and yaml comment depth, and reveals rules jetstream doesn't surface because jetstream's narrower scope doesn't need them.

The user's question driving this section: **what could we comment on in zerops.yaml + import.yaml that jetstream doesn't?** Answer: showcase already shows us. Catalog those decisions as rules.

## Where showcase EXCEEDS jetstream

### KB substance + density

Jetstream KB has **2 items** (Maintenance Mode, Temporary Upscaling) — both operational tasks. Showcase KB has **7 items**, all framework × platform intersection traps. Showcase items at apps-repo README:347-355:

- **No `.env` file** — env-shadow trap
- **Cache commands in `initCommands`, not `buildCommands`** — build/runtime path mismatch
- **`APP_KEY` is project-level** — cross-service shared secret
- **PDO PostgreSQL extension** — base image preinstalled (no prepareCommands)
- **Predis over phpredis** — base image absence (compiled extension missing)
- **Object storage requires path-style** — MinIO addressing
- **Vite manifest missing on dev after fresh deploy** — HMR-vs-build interaction

Each is symptom-first or directive-tightly-mapped (KB3 form). Each explains MECHANISM (what's happening platform-side) + EFFECT (what porter sees) + FIX (canonical resolution). This is the upper-bound shape of KB.

**Implication for rule KB2:** the rule "addresses ad-hoc operational concerns" was extracted from jetstream's narrower KB. Promote to: **KB primarily addresses framework × platform intersection traps**; ad-hoc ops items are a secondary class. Showcase shows that 6 of 7 KB items are intersection-traps; that's the bar.

### YAML comment shape

Jetstream comments are good. Showcase comments are *better* — and in a measurable way: they consistently follow a **MECHANISM + EFFECT + SO-WHAT** 3-part shape that jetstream uses inconsistently.

Examples from showcase apps-repo zerops.yaml (rendered in IG #1):

| Showcase comment | Mechanism | Effect | So-what |
|---|---|---|---|
| `Stderr logging sends output to Zerops runtime log viewer — no log files to manage or rotate.` | stderr → Zerops log viewer | log capture works | porter doesn't manage rotation |
| `Readiness check gates the traffic switch — new containers must answer HTTP 200 before the L7 balancer routes to them. This enables zero-downtime deploys.` | readinessCheck gates routing | container must answer 200 | zero-downtime deploys |
| `Health check restarts unresponsive containers after the 5-minute retry window expires — keeps production alive.` | healthCheck retry window | restarts on failure | production alive |
| `Cross-service references resolve at deploy time. Pattern: ${hostname_varname} maps to the db service's auto-generated credentials.` | deploy-time resolution | pattern shape | porter knows when to use |
| `--sleep=3 polls every 3s when idle, --tries=3 retries failed jobs before marking them as permanently failed.` | flag semantics | polling + retry | failure threshold |

**Promote rule Y2** to: comments explain MECHANISM + EFFECT + SO-WHAT in 2-3 sentences. Field-name documentation alone is insufficient.

### Per-setup intro line

Showcase opens every setup with a 1-2 line preamble that names what THIS setup is **and what it OPTIMIZES for**. Jetstream does this for prod (line 117-118) but not consistently. Showcase examples:

- `# Production — optimized build, compiled assets, framework caches, full service connectivity (DB, Redis, S3, Meilisearch).` (line 26-27)
- `# Dev — full source deployed for live editing via SSHFS. PHP-FPM serves requests immediately; edit files in /var/www and changes take effect on the next request — no restart.` (line 159-160)
- `# Worker — background job processor consuming from Redis queue. Same codebase as the app, different entry point. No HTTP traffic — no healthCheck, readinessCheck, or documentRoot.` (line 238-240)

**New rule Y10:** each setup section opens with a 1-2 line preamble naming the setup's PURPOSE + what it optimizes for + (when applicable) what it deliberately omits.

### Explicit absence-naming

Showcase's worker-setup comment names what's MISSING from the worker config and why: "No HTTP traffic — no healthCheck, readinessCheck, or documentRoot." This is honest porter communication: when a setup has fewer fields than its sibling, the porter is told why.

**New rule Y11:** when a setup has fewer fields than its sibling (worker vs prod, dev vs prod), comments name the deliberate absences. "No X — no Y, Z, W needed."

### Explicit duplication acknowledgment

Showcase's dev-setup envVariables block opens with: "Same service wiring as prod — only mode flags differ." Jetstream uses `extends: base` to avoid duplication mechanically; showcase repeats inline (since it doesn't use `extends`) but tells the porter what's happening. The honest framing wins; the mechanical-extension is a cleaner solution but if you don't use it, narrate.

**New rule Y12:** when a section repeats a previous section's content, name the dedup explicitly OR use `extends:` to mechanically share.

### Multi-line block comments for command groups

When `initCommands` has multiple lines, showcase puts ONE block comment ABOVE the group explaining all of them, rather than per-command inline comments. Example at line 82-92:

```yaml
# Config, route, and view caches MUST be built at runtime.
# Build runs at /build/source/ but the app serves from
# /var/www/ — caching during build bakes wrong paths.
#
# Migrations run exactly once per deploy via zsc execOnce,
# regardless of how many containers start in parallel.
# Seeder populates sample data on first deploy so the
# dashboard shows real records immediately.
# Scout import rebuilds the Meilisearch index from DB data
# after seeding — the safety net for when auto-indexing
# fires zero events (records already exist from prior deploy).
initCommands:
  - zsc execOnce ${appVersionId} --retryUntilSuccessful -- php artisan migrate --force
  ...
```

The block comment narrates the whole sequence in causal order. Jetstream tends toward shorter inline comments. The block-comment shape carries more pedagogical weight per line.

**New rule Y13:** when a list of commands forms a causal sequence (init/build/prepare commands), prefer ONE block comment above the list explaining the sequence over per-command inline comments.

## What showcase does WORSE than jetstream (don't elevate these)

For honesty:

| Showcase regression | Jetstream pattern |
|---|---|
| KB header is `### Gotchas` | Jetstream uses `## Tips and Others` (less engine-internal vocabulary in section name) |
| No deep links to apps-repo GitHub lines in IG | Jetstream IG body links to `composer.json#L14` etc. — porter sees exact change |
| Missing "Recipe features" bulleted list | Jetstream apps-repo README:230-237 has 8-bullet feature list |
| Missing "Production vs. Development" 3-bullet upgrade map | Jetstream apps-repo README:241-247 has 3-bullet map |
| No `> [!CAUTION]` callouts | Jetstream KB uses CAUTION callout for destructive ops (`php artisan down`) |
| Some IG/KB content duplicates (Predis client) | Jetstream IG and KB don't overlap |

Rules from jetstream NOT replaced by showcase: R23 (Recipe features section), PD1 (Production vs Development map), KB4 (CAUTION callouts).

## Yaml fields worth commenting on (consolidated catalog)

Per the user's question. List what gets commented across both goldens + the candidate, with the strongest exemplar.

### Always-comment fields (jetstream + showcase agree)

| Field | Why comment | Best exemplar |
|---|---|---|
| `setup: <name>` (per-section preamble) | What this setup IS + optimizes for | showcase line 26-27 / 159-160 / 238-240 |
| `build.base` (when multi-base) | Which runtimes are needed during build | showcase line 31-35 |
| `buildCommands` | Which step does what | showcase line 36-44 |
| `deployFiles` (when explicit list, not `./`) | Why these directories specifically | showcase line 45-58 |
| `cache` | What survives between builds | showcase line 60-64 |
| `readinessCheck` | What gates traffic | showcase line 67-68 |
| `initCommands` | Sequence-of-effects narrative | showcase line 82-92 (block comment) |
| `healthCheck` | What recovery semantics | showcase line 100-101 |
| Init/build commands with non-default flags | Flag semantics | showcase line 268-271 (`--sleep=3 --tries=3`) |
| `envVariables` block boundaries between services | Group purpose | showcase line 120-122 (cross-service pattern) |
| Specific cross-service env mappings (`AWS_USE_PATH_STYLE_ENDPOINT`) | Platform-mandated value, framework-mandated key | showcase line 139-141 |

### Conditional-comment fields (worth commenting when non-default)

| Field | When to comment |
|---|---|
| `documentRoot` | When framework-prescribed (showcase line 80-81 — Laravel `public/`) |
| `prepareCommands` | When installing tools the base image lacks (showcase line 188-192 — Node on dev) |
| `os: ubuntu` | When the slim default doesn't carry needed tools |
| `start: <cmd>` | When non-default OR when delegating to platform (`zsc noop --silent`) |
| `priority` | Only when non-default; explain dependency ordering (jetstream import.yaml:21-22) |
| `mode: HA / NON_HA` | Per-tier rationale; do NOT comment in the apps-repo yaml |
| `minContainers / maxContainers` | Per-tier; comment in import.yaml only |
| `verticalAutoscaling` | When tuning beyond defaults |
| `objectStoragePolicy` | When `public-read` (jetstream tier import.yaml:44 — `Create 'public-read' object storage bucket`) |
| `enableSubdomainAccess` | Worth a 1-line "L7 HTTPS proxy enabled for this service" |
| `envSecrets:` (vs envVariables) | When the secret is generated per end-user (jetstream tier import.yaml:18 — `APP_KEY: <@generateRandomString(<32>)>`) |
| `MEILI_HOST: http://...` (internal HTTP, not HTTPS) | Internal-vs-external traffic pattern (showcase line 149-151) |
| `siteConfigPath: site.conf.tmpl` | When a custom Nginx template ships (jetstream zerops.yaml:14-19) |

### Don't-comment fields (self-documenting)

| Field | Why skip |
|---|---|
| `hostname: <name>` | Self-evident |
| `type: postgresql@16` | Self-evident |
| Standard `${db_*}` / `${cache_*}` mappings | The pattern is comment-worthy ONCE, not per-mapping |
| `buildFromGit: <url>` | Self-evident |

### Worth-commenting that NEITHER current golden does well

These are gaps in BOTH jetstream and showcase — opportunities for the candidate to exceed both:

| Field | Why it deserves a comment | Sketch |
|---|---|---|
| `ports` (when `httpSupport: true`) | The signal that auto-enables L7 subdomain on first deploy. Candidate apidev/zerops.yaml:73-76 already comments this well. | "`httpSupport: true` is the signal that auto-enables the L7 subdomain on first deploy. The api binds 0.0.0.0:port inside the container; the platform terminates SSL at the edge and forwards plain HTTP to the container's VXLAN IP." |
| Project-level `envVariables` (in tier import.yaml) | Why these are project-scope vs service-scope (resolution-ordering, cross-service availability) | "Project-scope env vars resolve before any service deploys, so cross-service URL constants (FRONTEND_URL, API_URL) are available in build.envVariables of any service that depends on them." |
| `enableSubdomainAccess: true` paired with `httpSupport: true` | Two-step shape: enable + signal | "Pair: `enableSubdomainAccess` (import.yaml) declares intent; `httpSupport: true` (zerops.yaml ports) declares the entrypoint. Both required for the L7 subdomain to mint." |
| Tier-level `mode: HA` vs `mode: NON_HA` | What HA buys vs what NON_HA misses | "HA = automatic primary-replica failover for managed services. NON_HA = single node, no failover; cheaper but a service failure surfaces directly to the app." |
| `<@generateRandomString(<32>)>` preprocessor | Per-end-user generation timing | "`<@generateRandomString(<32>)>` is evaluated once per end-user at click-deploy. The author's workspace value never reaches the deliverable; each clone gets its own secret." |
| `${zeropsSubdomainHost}` literal in env values | Platform-substitution timing | "`${zeropsSubdomainHost}` stays as a literal in the deliverable yaml; the platform substitutes the end-user's specific subdomain at click-deploy." |

## Rules added or promoted from showcase

Net additions on top of the jetstream-derived list (38 rules → 47 rules):

- **Y10** — per-setup 1-2 line preamble naming purpose + optimization + (when applicable) deliberate omissions
- **Y11** — explicit absence-naming when a setup has fewer fields than its sibling
- **Y12** — explicit duplication acknowledgment OR mechanical `extends:`
- **Y13** — block comment above causal command sequences, not per-command inline
- **Y14** — yaml comments follow MECHANISM + EFFECT + SO-WHAT 3-part shape (promotion of Y2)
- **KB6** — KB primarily addresses framework × platform intersection traps; operational ad-hoc items are secondary class (promotion of KB2)
- **KB7** — each KB item explains MECHANISM (platform-side) + EFFECT (porter symptom) + FIX (canonical resolution) — same 3-part shape as Y14
- **PORTS-1** — comment `httpSupport: true` to name the L7 subdomain auto-enable signal (gap in both goldens)
- **TIER-1** — comment per-tier `mode: HA` / `mode: NON_HA` to name what HA buys vs misses (gap in both goldens)
- **PREPROC-1** — comment `<@generateRandomString>` preprocessor to name the per-end-user generation timing (gap in both goldens)
- **PREPROC-2** — comment `${zeropsSubdomainHost}` literal to name the click-deploy substitution timing (gap in both goldens)

## Implications

Net-net: the upper-bound bar — what we should aspire to and what refinement should anchor against — is **showcase's KB density + showcase's yaml comment depth + jetstream's IG discipline + jetstream's "Recipe features" + "Production vs Dev" sections + the four PORTS-1/TIER-1/PREPROC-1/PREPROC-2 gap-fillers**.

That's a synthesizable, observable, codebase-count-agnostic target. The candidate already exceeds both goldens on KB *substance* (real Zerops × NestJS intersections) but fails on KB *shape* (flat bullets where H3 + paragraph + 3-part body is the bar). The fix is shape, not substance.

---

# Verification pass — codex review (2026-05-08)

47 rules → **39 survive** after independent codex review. Verdict counts: 36 GROUNDED / 10 WEAK / 2 WRONG / 10 OVER-ENGINEERED.

## CUT — 6 rules

| Rule | Why cut |
|---|---|
| **A1** (KB header naming convention) | Section-title ban too brittle. Jetstream uses `## Tips and Others`, showcase uses `### Gotchas`. Fold into KB1/KB2. |
| **A2** (no "Adapt path:" prose in IG) | Pure negation of IG4. Redundant rule. |
| **A3** (no internal-tooling vocabulary) | Pure negation of V6/Y7. Redundant rule. |
| **A4** (no tier-promotion narrative) | Pure negation of T4/Y8. Redundant rule. |
| **PORTS-1** (comment `httpSupport: true`) | No cited evidence. "Nobody does this, therefore require it" is not a golden-derived rule. Move to platform-factual lint outside this set if needed. |
| **PREPROC-2** (`${zeropsSubdomainHost}` literal) | WRONG. Goldens use `${zeropsSubdomain}`, not `${zeropsSubdomainHost}` — citation was fabricated. Cut until grounded. |

## MERGE — 2 rules absorbed

| Rule | Merged into |
|---|---|
| **A5** (comment-density anti-rule) | Folded into Y2/Y14 — comment QUALITY rule, not density rule |
| **KB7** (3-part body shape duplicates Y14) | Merged into reworded Y14 |

## REWORD — 11 rules need new wording

| Rule | Issue | New wording |
|---|---|---|
| **V5** | Jetstream defers via URL; showcase has zero deferrals — universal claim refuted | "Defer broad platform concepts to docs when inline explanation would sprawl." Conditional, not mandatory. |
| **T1** | "8 lines total" — actual Stage README is 7 lines, other tiers vary 6-8 | Drop exact line count; keep "short delta description" principle |
| **TY1** | "2-line top comment" — AI Agent uses 3, Remote uses 4 | Drop line count; keep "mirror tier README intro extract" principle |
| **IG5** | "do this thing, not understand this concept" — slippery; showcase IG includes concept explanation | "Frame each IG step around a concrete change; concept explanation must serve that change." |
| **KB2** | Already better captured by KB6 | Fold; KB6 carries the framework × platform intersection framing |
| **Y4** | URL deferrals not portable; showcase yaml has zero | Make conditional, not scoring requirement |
| **Y12** | User's flag was correct. The rule prescribed two compatible options simultaneously. Then user position firmed: skip `extends:` entirely — recipe yaml is read top-to-bottom by a porter who clones-and-edits, flat yaml is more legible than DRY-with-indirection. | "Skip `extends:` — repeat envVariables inline per setup. When inline repetition spans many lines, lead with a one-line note naming what's intentionally repeated (showcase line 211-212 shape: `Same service wiring as prod — only mode flags differ.`)." |
| **Y13** | Grounded in single block — too thin for "prefer block comment universally" | "For causal command sequences, group the explanation above the list." |
| **Y14** | 3-part MECHANISM+EFFECT+SO-WHAT is ONE good shape, not THE shape; many good comments are not 3-part (showcase `documentRoot` at :80-81, `MAIL_MAILER` at :157) | "Comments are causal and porter-relevant" — merge with KB7 body-shape teaching. Drop the 3-slot template prescription. |
| **TIER-1** | Plan said "no cited evidence"; codex found citations I missed at `5 — Highly-available Production/import.yaml:47-53,:59-65,:79-84` and `4 — Small Production/import.yaml:42-47` | Rescue with citations |
| **PREPROC-1** | `<@generateRandomString>` partially supported by jetstream `0 — AI Agent/import.yaml:24-28` but not the "per-end-user timing" claim exactly | Reword to cited observed framing |

## KEEP UNCHANGED — 28 rules

V1, V2, V3, V4, V6, R1-R6, T2-T4, TY2-TY5, IG1-IG4, RF1, PD1, KB1, KB3-KB6, Y1-Y3, Y5-Y11.

## Y12 specific verdict — REWORD, then sharpen

Codex pass: showcase line 211-212 ("Same service wiring as prod — only mode flags differ") exists because showcase chose to repeat env wiring rather than use `extends:`. Jetstream uses mechanical `extends:` at zerops.yaml:95-96/:157-158.

User position: **skip `extends:` entirely.** Recipe yaml is read top-to-bottom by a porter who clones-and-edits; flat yaml is more legible than DRY-with-indirection. `extends:` adds resolve-the-base mental overhead with zero porter benefit. Showcase's "repeat inline + narrate the dedup when substantial" is the recommended shape.

This also clarifies the shape of similar conditional rules (V5/Y4 URL-deferral): when jetstream and showcase diverge on yaml-mechanism choice, prefer the flatter shape. Audience is reading top-to-bottom, not chasing references.

## Survivor count: 39 rules

39 = 47 − 6 cut − 2 merged. Of the 39, 11 are reworded vs original phrasing. Final composition:

- 6 voice (V1-V4, V6 + reworded V5)
- 6 root README (R1-R6)
- 4 tier README (T1 reworded, T2-T4)
- 5 tier import.yaml (TY1 reworded, TY2-TY5)
- 5 IG (IG1-IG4 + IG5 reworded)
- 2 sections (RF1, PD1)
- 6 KB (KB1, KB2 folded into KB6, KB3-KB6) — net 5
- 11 yaml (Y1-Y3, Y4 reworded, Y5-Y11)
- 2 gap-fillers (TIER-1 reworded with citations, PREPROC-1 reworded)

**This is the surviving rule list. Use this as the refinement scoring substrate, not the original 47.**
