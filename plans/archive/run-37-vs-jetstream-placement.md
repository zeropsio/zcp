# Run-37 ↔ Laravel Jetstream — placement-correctness audit

**Date:** 2026-05-10
**Run-37:** [`docs/zcprecipator3/runs/37/`](../docs/zcprecipator3/runs/37/) — nestjs-showcase recipe (3 codebases: api / app / worker; 5 managed services + 1 worker; 6 tiers)
**Jetstream golden:**
- recipe-repo: `/Users/fxck/www/recipes/laravel-jetstream/` — 28-line root README + 6 tier dirs (each: 8-line tier README + 53-76-line tier import.yaml)
- apps-repo: `/Users/fxck/www/laravel-jetstream-app/` — 291-line README + 207-line zerops.yaml + 32-line CLAUDE.md
**Method:** content-by-content placement audit. For every artifact + every section + every yaml block, where does jetstream put it, and where does run-37 put it? **Voice is held aside; placement is the focus.** Voice was covered in [`run-35-vs-jetstream.md`](run-35-vs-jetstream.md) and [`run-37-validation.md`](run-37-validation.md) §"Side-by-side voice comparison vs jetstream".

This complements [`run-37-validation.md`](run-37-validation.md). Validation focused on whether v9.78.0 fixes landed; this doc compares placement of every load-bearing piece of content.

---

## TL;DR — placement scorecard

| Surface | Placement verdict |
|---|---|
| Root recipe README structure | **AT-BAR** (V3 title + porter-meta line both engine-fixed; ordering matches; intro-extract content style differs) |
| Tier README L1 banner | **BELOW** — bare `# {TierName}` vs jetstream's `# {RecipeName} — {TierName} Environment` + recipe-link sentence |
| Tier README structure (post-banner) | **AT-BAR** (intro extract markers + jetstream lead pattern landed); `_back-link_` substituted for jetstream's recipe-link sentence |
| Apps-repo README L1 title shape | **AT-BAR** (V3 title `# Zerops x NestJS Showcase {Role}` matches jetstream's `# Zerops x <markers>...<markers>` shape) |
| Apps-repo README intro extract | **AT-BAR** placement; content-style differs (services-list vs framework-name + audience-value) |
| Apps-repo README cover image vs deploy button order | **BELOW** — run-37 has deploy button INSIDE the title-block above the cover; jetstream has cover above deploy H2 |
| Apps-repo README `## Deploy to Zerops` H2 | **MISSING** — jetstream wraps deploy button + manual import link in a dedicated H2; run-37 has only the inline deploy button |
| Apps-repo README section ORDER (apidev) | **DIVERGENT** — run-37: IG → RF1 → PD1 → Understand → KB; jetstream: IG → Understand → RF1 → PD1 → KB |
| Apps-repo README RF1+PD1 placement | **REGRESSION** — duplicated to non-canonical apps-repo (appdev). Jetstream has one apps-repo so this question doesn't arise; substrate teaching says canonical-only; run-35 honored that, run-37 broke it |
| Apps-repo README KB H2 wrapper title | **DIVERGENT** — apidev `## Tips and gotchas`, appdev `## Knowledge base`, workerdev no H2 wrapper at all (`### Gotchas` H3); jetstream `## Tips and Others` |
| Apps-repo README KB body shape | **DIVERGENT** — bullet `**X** — Y` intersection-trap entries; jetstream narrative `### <Topic>` H3 with multi-paragraph bodies |
| Apps-repo zerops.yaml setup architecture | **DELIBERATELY DIVERGENT** — flat (`prod`+`dev`); jetstream uses `base`+`prod extends`+`dev extends`. Per user direction: keep flat. |
| Apps-repo zerops.yaml block-comment style | **AT-BAR** — `# explanation` comments above each block, similar density and substance |
| Apps-repo CLAUDE.md scope | **DELIBERATELY DIVERGENT** — pure framework `claude /init`-shape; jetstream conflates framework + Zerops |
| Tier import.yaml file location | **AT-BAR** — `environments/{tier}/import.yaml` |
| Tier import.yaml head-comment placement | **AT-BAR** — preprocessor directive L1, blank, head comment L3-7 mirroring README intro |
| Tier import.yaml `project:` block | **AT-BAR** placement; `envVariables` section differs (run-37 uses preprocessor-resolved cross-codebase URL composition; jetstream's app-only recipe doesn't need this) |
| Tier import.yaml service-ordering convention | **AT-BAR** — apps-repo services first, then managed services (db / cache / broker / storage / search) |
| Tier import.yaml priority-justification block | **MISSING** — jetstream has `# Set higher priority for databases and storages, because the app depends on those services.` between app and db blocks; run-37 just sets `priority: 10` without the human-language explanation |
| Tier import.yaml per-service comment placement | **AT-BAR** — `#` comments above each `- hostname:` block |
| Tier import.yaml per-service comment voice (substrate fix #5) | **PARTIAL** — see [`run-37-validation.md`](run-37-validation.md) §"Substrate fix verification matrix"; tier-0 jetstream-pure 8/8, tier-4 db+cache reproduce TY2 BAD lead |
| FIXME-class noise in published yaml | **AT-BAR or BETTER** — jetstream tier-5 has a `# FIXME(tikinang): Deploy Mailpit?` comment; run-37 has no FIXME-class porter-visible noise |

---

## 1. Root recipe README — placement-by-placement

### Jetstream (28 lines, [`/Users/fxck/www/recipes/laravel-jetstream/README.md`](file:///Users/fxck/www/recipes/laravel-jetstream/README.md))

```
L1   # Laravel Jetstream Recipe
L3-6 <intro extract — markers + 2-line body naming framework + audience-value>
L8   ⬇️ **Full recipe page and deploy with one-click**
L10  [deploy button SVG → small-production env]
L12  [cover image SVG]
L14  Offered in examples for the whole development lifecycle — from environments…
L16-21  - **<Tier>** [[info]](relative-path) — [[deploy]](recipe-page-with-tier)  ×6
L23  ---
L25  For more advanced examples see all [PHP recipes]…
L27  Need help… Discord
```

### Run-37 ([`environments/README.md`](../docs/zcprecipator3/runs/37/environments/README.md))

```
L1   # NestJS Showcase Recipe                ← V3 fix landed; matches jetstream shape
L3-5 <intro extract markers + 1-line body naming services + tier count>
L7   ⬇️ **Full recipe page and deploy with one-click**
L9   [deploy button SVG → small-production env]
L11  [cover image SVG]
L13  Offered in examples for the whole development lifecycle…   ← engine-fixed line
L15-20  - **<Tier>** [[info]](encoded-path) — [[deploy with one click]](recipe-page-with-tier)  ×6
L22  ---
L24  For more advanced examples see all [nestjs recipes]…
L26  Need help… Discord
```

### Placement diff

- **L1 title:** ✓ AT-BAR (`# {RecipeName} Recipe` shape matches; engine fix V3 + HumanName closed the run-35 slug-stem leak)
- **Intro extract markers:** ✓ AT-BAR (between cover image H1 and deploy CTA)
- **Deploy button:** ✓ AT-BAR
- **Cover image:** ✓ AT-BAR
- **Porter-meta lifecycle line:** ✓ AT-BAR (engine-hardcoded; same byte content as jetstream)
- **Tier list:** ✓ AT-BAR (6 bullets, info-link + deploy-link)
- **Catalog punt:** ✓ AT-BAR
- **Discord line:** ✓ AT-BAR

### Intro-extract content style (placement-adjacent, voice-axis)

| Aspect | Jetstream | Run-37 |
|---|---|---|
| Frames the framework | "running [Laravel Jetstream](https://jetstream.laravel.com/introduction.html) - an advanced starter kit by Laravel" | "A NestJS showcase recipe with a Vite SPA frontend and a background worker" |
| Audience value | "with all the essentials for modern development: login flow, 2FA, sessions, queues, mailing, and cloud-ready file storage" | "wired to PostgreSQL, Valkey, NATS, S3-compatible object storage, and Meilisearch — six deployable tiers from AI agent workspace through highly-available production" |
| Recipe-name link | inline link to framework's homepage | no link |

Run-37's intro is more service-list focused; jetstream's is more porter-value focused. **Placement is correct; content style differs.** Not a placement issue.

### Verdict for Section 1
**Root README placement is at-bar with jetstream golden.** Both engine fixes (V3 title + porter-meta line) land cleanly. The only sub-bar element is intro-extract content style — service-list vs framework + audience-value — but this is voice axis, not placement.

---

## 2. Tier README — placement-by-placement

### Jetstream tier-3 (8 lines, [`/Users/fxck/www/recipes/laravel-jetstream/3 — Stage/README.md`](file:///Users/fxck/www/recipes/laravel-jetstream/3%20%E2%80%94%20Stage/README.md))

```
L1   # Laravel Jetstream — Stage Environment           ← recipe + tier + "Environment" suffix
L2   This is a stage environment for [Laravel Jetstream (info + deploy)](https://app.zerops.io/recipes/laravel-jetstream?environment=stage) recipe on [Zerops](https://zerops.io).
                                                       ← banner sentence: recipe-page link + zerops.io link
L4-7 <intro extract — markers + jetstream lead pattern body>
```

### Run-37 tier-3 (10 lines, [`environments/3 — Stage/README.md`](../docs/zcprecipator3/runs/37/environments/3%20%E2%80%94%20Stage/README.md))

```
L1   # Stage                                           ← BARE tier name only
L2   (blank)
L3   _[← back to recipe root](../README.md)_           ← back-link, not deploy-link
L4   (blank)
L5-7 <intro extract — markers + jetstream lead pattern body>
L8   (blank)
L9   [deploy button SVG → tier env]                    ← run-37 ADDS a deploy button (jetstream tier README has none)
```

### Placement diff — three findings

**Finding A: tier README L1 title is below jetstream-bar.**
- Jetstream: `# Laravel Jetstream — Stage Environment` — full recipe context. A porter who lands on this page from a search engine immediately sees what recipe + what tier.
- Run-37: `# Stage` — bare tier name. A porter who lands here has zero context for what recipe this belongs to without scrolling/clicking back.

**Finding B: tier README L2 banner sentence missing.**
- Jetstream: `This is a stage environment for [Laravel Jetstream (info + deploy)](https://app.zerops.io/recipes/laravel-jetstream?environment=stage) recipe on [Zerops](https://zerops.io).`
- Run-37: substituted with `_[← back to recipe root](../README.md)_` italic back-link.

The jetstream banner sentence accomplishes three porter-deeplink-friendly things:
1. Names the recipe and the tier in one sentence
2. Links to the recipe page on app.zerops.io with the tier environment query param (= one-click deploy)
3. Links to zerops.io platform homepage

The run-37 back-link does ONE thing: navigates relative to the recipe root within the same repo. Loses the deploy CTA entirely.

**Finding C: run-37 tier README has a deploy button at the bottom; jetstream tier README has no deploy button.**

This is run-37's defensible compensation for missing the L2 banner sentence — the porter still gets a deploy CTA, just at the bottom rather than in the title-block. **Net deploy-CTA reachability: jetstream is more discoverable (above the fold); run-37 is below the intro.**

### Tier README placement-correctness recommendation

If the iteration goal is alignment with jetstream golden:

1. Engine fix: `tier_readme.md.tmpl` should render L1 as `# {RecipeName} — {TierName} Environment`. Plus `internal/recipe/human_name.go` already exposes `HumanName(plan)` so this is one template-line change.
2. Engine fix: render L2 as the banner sentence: `This is a {tierName} environment for [{RecipeName} (info + deploy)](https://app.zerops.io/recipes/{slug}?environment={tier-slug}) recipe on [Zerops](https://zerops.io).`
3. Drop the back-link or move it to a bottom footer.
4. Drop the standalone deploy button (the L2 banner sentence carries the deploy CTA).

Cost: 2 template lines + 2 tests pinning the rendered shape. Engine teeth, no substrate dependency. Same class as the V3 title fix that v9.78.0 just shipped.

### Tier README intro extract — substrate fix #3 landing

[`run-37-validation.md`](run-37-validation.md) §"Substrate fix verification matrix" already documented that all 6 tier intros use the jetstream lead pattern (`**<Tier name>** environment <verb> <audience purpose>`). **Placement of the intro extract itself (markers + body between L1/L2 banner and end-of-file) is at-bar with jetstream.**

---

## 3. Apps-repo README — placement-by-placement

### Jetstream apps-repo README structure (291 lines, [`/Users/fxck/www/laravel-jetstream-app/README.md`](file:///Users/fxck/www/laravel-jetstream-app/README.md))

```
L1   # Zerops x <!-- markers --> Laravel Jetstream <!-- markers -->
L3-5 <intro extract markers + body naming framework + integration scope>
L7   ![cover image]
L9   <br/>
L11  ## Deploy to Zerops
L12  You can either click the deploy button… [import yaml](github-link)
L14  [deploy button SVG → recipe page]
L16  <br/>
L18  ## Integration Guide
L20  <intro extract markers BEGIN>
L22  ### 1. Add `zerops.yaml`                                    ← imperative title
L23  Add the following… `zerops.yaml`. Feel free to customize…
L25  This example includes idempotent migrations, caching, optimized build process and support for remote or agent development.
L27-206  ```yaml … (179-line annotated yaml block with extends:) ``` 
L208 ### 2. Add Support For Object Storage                        ← 3 lines: bullet, code, bullet
L215 ### 3. Utilize Environment Variables                         ← 1 paragraph with 2 inline links to source
L218 ### 4. Setup Production Mailer                               ← 1 line
L221 <intro extract markers END>
L223 ## Understand Zerops Core Concepts                           ← H2 directly after IG
L224 If you want to try integrating Zerops from scratch on a new Laravel project, check our [step-by-step tutorial]…
L226 <br/>
L228 ## Recipe features                                           ← AFTER Understand
L230-237 - 8 bullets including Mailpit + Inertia.js
L239 <br/>
L241 ## Production vs. Development                                ← AFTER RF1
L243 Base of the recipe is ready for production, the difference comes down to:
L245-247 - 3 bullets ending with Mailpit-replacement
L249 ## Tips and Others                                            ← KB H2 wrapper
L251 <KB intro extract markers BEGIN>
L253 ### Maintenance Mode                                         ← narrative H3 entry, 3-paragraph body + > [!CAUTION] block
L272 ### Temporary Upscaling when Playing Around                  ← narrative H3 entry, 3-paragraph body
L285 <KB intro extract markers END>
L287-291 <br/><br/> + Discord line
```

### Run-37 apidev README structure (336 lines, [`apidev/README.md`](../docs/zcprecipator3/runs/37/apidev/README.md))

```
L1   # Zerops x NestJS Showcase API                              ← V3 fix landed
L3-5 <intro extract markers + 1-line body naming services + DOR>
L7   ⬇️ **Full recipe page and deploy with one-click**
L9   [deploy button SVG → small-production env]                  ← INLINE, no H2 wrapper
L11  ![cover image]                                              ← AFTER deploy button (jetstream has cover BEFORE deploy H2)
L13  ## Integration Guide                                        ← jumps directly into IG, no `## Deploy to Zerops` H2
L15  <IG intro extract markers BEGIN>
L16-205  ### 1. Adding `zerops.yaml`                             ← 190 lines incl. 178-line yaml block (no extends)
L207-220 ### 2. Bind the listener to `0.0.0.0`                   ← 12 lines body
L222-237 ### 3. Trust the reverse proxy                          ← 12 lines body, NestExpressApplication snippet
L239-258 ### 4. Drain on `SIGTERM` for rolling deploys           ← 18 lines body, NEW vs run-35 apidev (was workerdev-only)
L260-275 ### 5. Avoid the same-key shadow trap on env-var aliases ← 14 lines body, sharpened framing
L277 ## Recipe features                                           ← BEFORE Understand (jetstream is AFTER)
L279-296 - 8 bullets
L298 ## Production vs. Development                                ← AFTER RF1, BEFORE Understand
L300-309 - 3 bullets ending with APP_SECRET placeholder swap
L311 ## Understand Zerops Core Concepts                           ← BURIED below RF1+PD1
L313-315 If you want to try integrating Zerops from scratch on a new NestJS project, check our [step-by-step NestJS tutorial]…
L316 <IG intro extract markers END>
L318 <KB intro extract markers BEGIN>
L319 ## Tips and gotchas                                          ← KB H2 wrapper, NEW vs run-35 (was bare ### Gotchas H3)
L321-336 - 8 bullets, intersection-trap shape (`**X** — Y`)
L337 <KB intro extract markers END>
```

### Placement diff — five findings

**Finding D: cover image and deploy button are inverted.**

| Order | Jetstream | Run-37 apidev |
|---|---|---|
| L1 | title | title |
| L3-5 | intro extract | intro extract |
| L7 | **cover image** | "⬇️ Full recipe page and deploy with one-click" lead |
| L9 | (br) | **deploy button** |
| L11 | **`## Deploy to Zerops` H2** | **cover image** |
| L13 | (deploy button + import link) | `## Integration Guide` H2 |

Jetstream order: title → intro → cover → deploy H2 → IG H2.
Run-37 order: title → intro → deploy button → cover → IG H2.

Cover image and deploy button are swapped. Why does this matter? Jetstream's `## Deploy to Zerops` H2 wraps the deploy button AND adds a `manual zerops-project-import.yml` link reference, giving the porter two paths (one-click vs manual import). Run-37 has only the inline button.

**Finding E: `## Deploy to Zerops` H2 wrapper is missing.**

Jetstream:
```markdown
## Deploy to Zerops
You can either click the deploy button to deploy directly to Zerops, or manually copy the [import yaml](https://github.com/zerops-recipe-apps/laravel-jetstream-app/blob/main/zerops-project-import.yml) to the import dialog in the Zerops app.

[![Deploy on Zerops](…)](https://app.zerops.io/recipe/laravel)
```

Two things are accomplished:
1. The deploy button gets an H2 anchor (`#deploy-to-zerops`) that other docs / blog posts can deeplink to.
2. The H2 surfaces the manual-import path for porters who want to inspect the yaml before clicking.

Run-37 has only the inline button + "Full recipe page and deploy with one-click" lead. No deploy-anchor H2, no manual-import path mention.

**Finding F: section ORDER below IG diverges.**

| Position | Jetstream | Run-37 apidev |
|---|---|---|
| 1st post-IG H2 | `## Understand Zerops Core Concepts` (L223) | `## Recipe features` (L277) |
| 2nd post-IG H2 | `## Recipe features` (L228) | `## Production vs. Development` (L298) |
| 3rd post-IG H2 | `## Production vs. Development` (L241) | `## Understand Zerops Core Concepts` (L311) |
| 4th post-IG H2 | `## Tips and Others` (L249) | `## Tips and gotchas` (L319) |

Different storytelling:
- **Jetstream:** Hands-on flow first (deploy → IG → "if you want to learn the platform deeper, here's Understand"), then "what this recipe gives you" (RF1) + "how to scale to prod" (PD1) + "operational tips" (KB).
- **Run-37:** Hands-on flow first (deploy + IG), then "what this recipe gives you" (RF1) + "how to scale to prod" (PD1), then platform-knowledge bridge (Understand) as a footer, then KB.

Jetstream's order treats Understand as a load-bearing exit ramp from the IG ("done with IG? here's the platform tutorial"). Run-37's order treats Understand as a footer link near KB. Both work; jetstream is more aligned with porter-flow.

**Finding G: RF1+PD1 duplicated to non-canonical apps-repo (appdev) — REGRESSION.**

Documented in [`run-37-validation.md`](run-37-validation.md) §"Top 5 surprises #2". Jetstream has one apps-repo so this is not testable against jetstream golden. The substrate teaching (`briefs/refinement/derived_rules.md §RF1`) is "the api/central-app codebase owns this section" and the gate at [`internal/recipe/gate_canonical_apps_repo.go:48`](../internal/recipe/gate_canonical_apps_repo.go) enforces presence on canonical, NOT absence on others. Run-37 appdev has them duplicated (L163, L175); run-35 appdev did not.

**Finding H: KB H2 wrapper title and body shape diverge.**

Wrapper title:
| Sibling | Run-37 KB title | Heading level |
|---|---|---|
| apidev | `## Tips and gotchas` | H2 — closer to jetstream's `## Tips and Others` |
| appdev | `## Knowledge base` | H2 — different title |
| workerdev | `### Gotchas` | **H3 — no H2 wrapper** |

Jetstream uses `## Tips and Others` H2. Run-37 has 3 different titles + 1 different heading level across siblings.

Body shape:
- Jetstream: 2 narrative `### <Topic>` H3 entries with 3-paragraph bodies + `> [!CAUTION]` callouts + shell sequences (Maintenance Mode + Temporary Upscaling)
- Run-37 apidev: 8 bullet entries with `**X** — Y` intersection-trap shape (NATS double-auth, MinIO 403, Valkey AUTH, Meilisearch http-only, X-Cache cross-origin, 413 body cap, combined execOnce, ListObjectsV2 sort)
- Run-37 appdev: 3 bullet entries (Vite host allowlist, dev SPA 502, no shared state)
- Run-37 workerdev: 6 bullet entries (drain SIGTERM, NATS double-auth, Valkey AUTH, MinIO 403, Meilisearch http-only, dev workflow)

Different audiences:
- Jetstream KB serves a porter who's deployed and is now operating (maintenance, scaling).
- Run-37 KB serves a porter who's evaluating + deploying and may hit a deployment trap.

Both philosophies work. As [`run-35-vs-jetstream.md`](run-35-vs-jetstream.md) noted, this is a "real audience-model fork" — picking one to align onto is a product decision.

### Verdict for Section 3

**Apps-repo README placement is below jetstream-bar on five axes:**

1. Cover image and deploy button order inverted (D)
2. `## Deploy to Zerops` H2 wrapper missing (E)
3. Section order below IG diverges (Understand misplaced as footer rather than exit-ramp) (F)
4. RF1+PD1 duplicated to non-canonical apps-repo (G — REGRESSION)
5. KB H2 wrapper title + body shape diverge across siblings (H)

(D), (E), and (F) would each be cheap engine-side fixes (template ordering + an explicit `## Deploy to Zerops` H2 in the codebase README template). (G) is the absence-gate fix already pre-scoped in [`run-37-validation.md`](run-37-validation.md). (H) is a product decision (audience-model fork).

---

## 4. Apps-repo zerops.yaml — placement-by-placement

### Jetstream zerops.yaml structure (207 lines, [`/Users/fxck/www/laravel-jetstream-app/zerops.yaml`](file:///Users/fxck/www/laravel-jetstream-app/zerops.yaml))

```
L1-5    file head: `# This example app uses two setups…` (4-line preamble)
L6      zerops:
L7-9    `# Define shared 'base' setup to prevent duplication.`
L10-115 - setup: base                                                           ← shared envVariables block
          L11-13 run.os/base
          L14-19 inline comment about PHP 8.4 + nginx
          L20-115 envVariables (50+ vars: APP_*, DB_*, AWS_*, LOG_*, MAIL_*, BROADCAST/CACHE/REDIS/SESSION_*, BCRYPT/TRUSTED_PROXIES/FILESYSTEM)
                  with inline `#` comments explaining each block
L117-178 - setup: prod                                                          ← inherits via extends: base
          L118 extends: base
          L121-145 build (commands, deployFiles, cache)
          L146-156 deploy.readinessCheck + inline `#` comments
          L157-178 run (envVariables override + initCommands + healthCheck)
L179-206 - setup: dev                                                           ← inherits via extends: base
          L180 extends: base
          L184-186 build (deployFiles only)
          L187-206 run (prepareCommands installing Node 22 + envVariables override + initCommands)
```

### Run-37 apidev zerops.yaml structure (184 lines, [`apidev/zerops.yaml`](../docs/zcprecipator3/runs/37/apidev/zerops.yaml))

```
L1      zerops:                                                                  ← no file-head preamble
L2-7    inline comment about prod + dev setup
L8      - setup: prod
        L9-49 build (commands, deployFiles, cache)
        L50-58 deploy (readinessCheck)
        L59-138 run (envVariables, initCommands, start, healthCheck)
              with inline `#` comments explaining each block
              including IG#5-cross-reference comment block at L79-100
L140    - setup: dev                                                             ← parallel envVariables block (no extends)
        L142-159 build (deployFiles, cache)
        L160-184 run (envVariables, initCommands, start)
```

### Placement diff — three findings

**Finding I: file-head preamble missing.**

Jetstream L1-5:
```yaml
# This example app uses two setups:
# 'prod' for building, deploying, and running the app
# in production or staging environments.
# 'dev' for deploying the source code into a development
# environment with the required toolset.
```

A porter cracking open `zerops.yaml` for the first time gets a one-paragraph orientation BEFORE the structural keys. Run-37 starts with `zerops:` on L1 — porter has to read the inline comment at L2-7 INSIDE the structure to learn the same fact.

This is a small but real placement difference. **Cheap fix:** substrate teaching to author a top-of-file preamble before `zerops:`. Or engine teeth at scaffold-yaml-content gate: refuse zerops.yaml whose first non-blank line is `zerops:`.

**Finding J: setup architecture (`base` extends pattern) — DELIBERATELY DIVERGENT per user direction.**

Jetstream: `base` + `prod extends base` + `dev extends base`. Each setup defines only what it overrides; the shared envVariables list lives in `base` once.

Run-37: flat `prod` + `dev`. Every envVariables block is repeated; only `NODE_ENV` and `start` differ between them.

Per user instruction in the run-37 handoff: *"Skip `extends:` in yaml — flat yaml beats DRY-with-indirection."* This is a design choice, not a defect. Cost: ~+8pp comment density vs jetstream golden because comments compensate for the visual repetition.

**Finding K: per-block comment substance — at-bar with jetstream.**

Both use `# explanation` comments above each non-obvious block. Both pre-empt failure modes inline (jetstream: `# Laravel checks the 'Host' header against this value` at APP_URL; run-37: `# `S3_REGION: us-east-1` is required by every AWS S3 SDK because MinIO ignores the value but the SDK refuses to sign requests without one`).

Jetstream comments are denser per-block (often 3-line explanatory paragraphs); run-37 comments are slightly shorter but more numerous. Net density: jetstream ~38%, run-37 apidev 46.4% (counted via [`/tmp/yaml_density2.py`](file:///tmp/yaml_density2.py)).

### Verdict for Section 4

**Apps-repo zerops.yaml placement is at-bar with jetstream golden** in every dimension EXCEPT the deliberate `extends:` divergence (per user direction) and the missing file-head preamble (cheap fix candidate). Comment density is above golden as the cost of the flat architecture.

---

## 5. Apps-repo CLAUDE.md — placement-by-placement

### Jetstream apps-repo CLAUDE.md (32 lines, [`/Users/fxck/www/laravel-jetstream-app/CLAUDE.md`](file:///Users/fxck/www/laravel-jetstream-app/CLAUDE.md))

```
L1   # laravel-jetstream-app
L3   <1-line description: framework + Zerops sibling list + setup architecture>
L5   ## Zerops service facts
L7-9 - HTTP port + siblings + runtime base
L11  ## Zerops dev (hybrid)
L13-22 hybrid-dev workflow: Vite manifest 500 trap + 2-option recovery + DO NOT add `npm run build` to dev buildCommands
L24  All platform operations… via `zcp` MCP tools.
L26  ## Notes
L28-31 - 4 framework-specific operational notes
```

### Run-37 apidev CLAUDE.md (per system-reminder injection — 25-30 lines)

```
L1   # nestjs-showcase-api
L3   <1-line description: NestJS 10 framework + dependencies — pure framework, no Zerops>
L5   ## Build & run
L7-13 - 7 npm scripts catalog (build, start, start:dev, migrate, seed, etc.)
L15  ## Architecture
L17+ - per-file architecture map (src/main.ts, src/app.module.ts, src/items.controller.ts, ...)
```

### Placement diff — two findings

**Finding L: CLAUDE.md scope is split between framework + Zerops in jetstream; pure framework in run-37.**

Jetstream conflates framework + Zerops in one CLAUDE.md (port, siblings, runtime base, hybrid-dev workflow with Vite manifest trap, zcp MCP tool reference).

Run-37 splits the concerns: apidev/CLAUDE.md is `claude /init`-shape (pure framework); the Zerops-aware content lives in apidev/README.md (`## Recipe features`, `## Production vs. Development`, KB section).

This is a deliberate substrate-decided split, not a defect. Cost: a porter using `claude` to operate the codebase doesn't get framework-specific Zerops gotchas inline (e.g., NestJS file-watch lifecycle, TypeORM synchronize on rolling deploy, Meilisearch reindex on redeploy). They have to discover apidev/README.md.

But: NestJS doesn't have the Razor/Blade hot-reload trap that Vite manifest 500 captures. So the practical cost of the split is lower for NestJS than it would be for Laravel + Inertia.

**Finding M: framework-architecture map placement is at-bar.**

Both put framework architecture under `## Architecture` H2 with file-by-file bullets. Both treat npm scripts under `## Build & run` H2. Same shape, different volume.

### Verdict for Section 5

**Apps-repo CLAUDE.md placement is deliberately divergent (per substrate-decided split) but coherent.** Not a placement defect. The Zerops-aware content lives at the right layer (README), just split out from CLAUDE.md.

---

## 6. Tier import.yaml — placement-by-placement

### Jetstream tier-3 import.yaml structure (54 lines)

```
L1   # zeropsPreprocessor=on                    ← preprocessor directive
L2   (blank)
L3-4 # Stage environment uses the same configuration as production,
     # but runs on the lowest scaling settings.   ← head comment, README intro mirror
L5   (blank)
L6   project:
L7      name: laravel-jetstream-stage
L8   (blank)
L9   services:
L10-15  # Deploy the Laravel Jetstream stage app, running on
        # PHP 8.4 with Nginx (FastCGI) as webserver.
        - hostname: app
          ...                                  ← imperative + role + framework-bridge
L17-19  # Set higher priority for databases and storages,
        # because the app depends on those services.    ← TY5-class priority justification
L20-25  # Deploy single node PostgreSQL database,
        # used by the Laravel app to store data. Automatic,
        # encrypted backups are enabled by default.
        - hostname: db
        ...                                    ← imperative + role + relationship + porter-fact
L27-32  # Spin up Valkey in single-node mode.
        # Valkey is an open-source Redis-compatible
        # key/value storage for caching, sessions, queues, etc.
        - hostname: redis
        ...                                    ← imperative + framework-bridge for porters who know Redis
L34-39  # Create 'public-read' object storage bucket,
        # with minimal storage capacity.
        - hostname: storage
        ...
L41-44  # Optionally, spin up the single-service email and SMTP
        # testing tool.
        # Feel free to remove this service, if you wish to stage-test
        # your app with as-close-as-possible production setup.
        - hostname: mailpit
        ...                                    ← porter-instructional ("feel free to remove")
```

### Run-37 tier-3 import.yaml structure (112 lines)

```
L1    #zeropsPreprocessor=on                    ← preprocessor directive (note: no space before "zerops")
L2    (blank)
L3-6  # Stage environment uses the same configuration as production, but
      # runs on the lowest scaling settings with a small spike-buffer
      # headroom on every container — sized for QA rehearsal traffic
      # against snapshot-grade data.            ← head comment, README intro mirror
L7    project:
L8       name: nestjs-showcase-stage
L9       envVariables:
L10-12      APP_SECRET / API_URL / FRONTEND_URL ← project-scope cross-codebase URL composition
L13   (blank)
L14   services:
L15-20  # Single api container running the production NestJS setup —     ← MIX shape
        # stage rehearsal shape because QA load is bursty rather than
        # steady. `minFreeRamGB: 0.25` keeps spike headroom on the
        # container so that QA-traffic bursts don't tip into swap;
        # bump `verticalAutoscaling.maxRam` when monitoring shows
        # containers approaching the current ceiling.
        - hostname: api
        ...
L31-34  # Single app container shipping the built Vite SPA on            ← MIX shape, cross-tier delta
        # `base: static` — same shape as tier 2 with `minFreeRamGB: 0.25`
        # spike headroom. Replace the `${zeropsSubdomainHost}`-derived
        # URL with your own stage domain once you have one configured.
        - hostname: app
        ...
L44-47  # Single worker container running the built subscriber — stage   ← MIX shape
        ...
L56-60  # Single-node Postgres — used by the api codebase to store the   ← MIX shape (jetstream-MIX-pure)
        # items table. Snapshots-only durability at this rehearsal tier;
        ...
L69-72  # Single-node Valkey — same shape as tier 2 plus
        # `minFreeRamGB: 0.25` for stage spikes. Bump
        # `verticalAutoscaling.minRam` if cache hit rate drops as the
        # working set grows past 0.25 GB.
        - hostname: cache
        ...
L81-84  # Single-node NATS — core pub/sub keeps `items.indexed` flowing
        # from api to worker. `minFreeRamGB: 0.25` headroom soaks up
        # rehearsal-time fan-out spikes. Messages remain fire-and-forget;
        # rehearsal-grade tradeoff at stage.
        - hostname: broker
        ...
L93-95  # Object storage — same shape every tier; the only adapt path is
        # `objectStorageSize`. Bump it when stage uploads outgrow the
        # current quota.
        - hostname: storage
        ...
L101-103 # Single-node Meilisearch — bump
        # `verticalAutoscaling.minRam` if stage search latency correlates
        # with index growth past the 0.25 GB ceiling.
        - hostname: search
        ...
```

### Placement diff — three findings

**Finding N: preprocessor directive whitespace style.**

Jetstream: `# zeropsPreprocessor=on` (space after `#`).
Run-37: `#zeropsPreprocessor=on` (no space after `#`).

Both are valid for the preprocessor (the engine reads either form). Jetstream's space-style is more readable as a comment. Cosmetic; not a placement issue.

**Finding O: priority-justification block missing.**

Jetstream tier-3 + tier-5 + tier-0 import.yaml each have an inline TY5-class explanation between the apps-repo block and the first managed-service block:
```yaml
  # Set higher priority for databases and storages,
  # because the app depends on those services.
```

Run-37 doesn't carry this priority-justification block. Service blocks have `priority: 10` set on db/cache/broker/search, but the human-language explanation of WHY is missing.

This is a substrate-axis placement gap. Substrate's TY5 rule (per [`internal/recipe/content/briefs/env-content/per_tier_authoring.md`](../internal/recipe/content/briefs/env-content/per_tier_authoring.md) per the run-35-vs-jetstream side-by-side §"Per-service comment voice — the TY2 axis" but in reality TY5 specifically) teaches "service ordering with `priority` justified in human terms when non-default" — so the substrate teaches it but the agent didn't author it on any of the 6 tier yamls.

This is the SAME class of substrate-compliance failure as the TY2 BAD residual on tier-4 db+cache. Substrate teaches it; agent doesn't always author it. Engine teeth could close it (gate refuses tier yaml that has `priority: 10` on managed services without a corresponding `# Set higher priority…` comment block above the first managed-service block).

**Finding P: porter-instructional ("Feel free to remove this service") shape — partially landed.**

Jetstream tier-3 mailpit comment: *"Optionally, spin up the single-service email and SMTP testing tool. Feel free to remove this service, if you wish to stage-test your app with as-close-as-possible production setup."*

This is a porter-instructional shape — the comment empowers the porter to modify the recipe.

Run-37 tier-3 storage comment: *"Object storage — same shape every tier; the only adapt path is `objectStorageSize`. Bump it when stage uploads outgrow the current quota."*

This is more knob-instructional — bump this field if condition. Similar shape to "feel free to remove" but pointed at scaling rather than removal. **At-bar in spirit, partial match in form.**

### Verdict for Section 6

**Tier import.yaml placement is at-bar with jetstream golden on file-location, head-comment placement, service-ordering, and per-service comment placement.** Two cleanup findings:
- (O) priority-justification block missing on every tier — substrate-compliance gap, candidate for engine teeth
- (N) preprocessor directive whitespace style — cosmetic, sub-1-byte fix

Per-service comment voice is partial-landed (substrate fix #5; documented in [`run-37-validation.md`](run-37-validation.md)).

---

## 7. Summary — placement scorecard with prioritized fixes

| # | Finding | Surface | Cost | Impact | Recommend |
|---|---|---|---|---|---|
| A | Tier README L1 = bare `# {TierName}` | tier README | 2 template lines + 2 tests | High — porter deeplink + recipe-context discoverability | **Engine fix** — `tier_readme.md.tmpl` renders L1 as `# {RecipeName} — {TierName} Environment` |
| B | Tier README L2 banner sentence missing | tier README | 1 template line + 1 test | High — adds porter-deploy CTA above the fold | **Engine fix** — render L2 as `This is a {tierName} environment for [{RecipeName} (info + deploy)]({recipeUrl}) recipe on [Zerops](https://zerops.io).` |
| D | Cover image / deploy button order inverted | apps-repo README | template ordering swap | Medium — porter sees cover before CTA in jetstream's flow | **Engine fix** — swap order in `codebase_readme.md.tmpl` |
| E | `## Deploy to Zerops` H2 wrapper missing | apps-repo README | 3 template lines (H2 + sentence + button) + 1 test | Medium — adds H2 anchor + manual import path | **Engine fix** |
| F | Section order below IG diverges (Understand misplaced as footer) | apps-repo README | template ordering swap or substrate teaching | Low-medium — different storytelling, both work | **Hold** unless porter feedback says otherwise |
| G | RF1+PD1 duplicated to non-canonical apps-repo (appdev) | apps-repo README | 3 lines of Go + table-driven test in `gate_canonical_apps_repo.go` | High — silent regression, gate enforces presence not absence | **Engine teeth** (already pre-scoped in [`run-37-validation.md`](run-37-validation.md)) |
| H | KB H2 wrapper title + body shape diverge across siblings | apps-repo README | engine gate (~1-2h) + product decision on shape | Medium — sim-3 win still lost | **Engine teeth** in next iteration; product decision on shape required first |
| I | Apps-repo zerops.yaml file-head preamble missing | apps-repo zerops.yaml | substrate teaching or engine gate | Low — porter still gets the same fact via inline comment, just one click later | **Hold or substrate** |
| O | Tier yaml priority-justification block missing on every tier | tier import.yaml | substrate (already teaches TY5) — escalate to engine if substrate doesn't land in run-38 | Low-medium — porter sees `priority: 10` without explanation | **Hold** for sim-replay; escalate to engine teeth if persistent |
| N | Preprocessor directive whitespace style | tier import.yaml | one-byte template fix | Cosmetic | **Engine fix** alongside other tier-yaml engine fixes |

**Per-iteration priority for the next ship:**

1. (G) — RF1+PD1 absence-gate. Already pre-scoped. Highest priority; closes the silent regression.
2. (A) + (B) — tier README L1 + L2 engine fix. Same template, same change-set; closes the porter-deeplink discoverability gap. Cheap.
3. (D) + (E) — cover-image/deploy-button order + `## Deploy to Zerops` H2. Same template (`codebase_readme.md.tmpl`); closes two findings together.
4. (H) — KB sibling-shape engine gate. Requires product decision on shape first. Sim-3-win-lost residual.

(F), (I), (N), (O) are deferred until porter feedback or sim-replay reveals which are determinist regressions.

---

## 8. What this audit does NOT cover

| Out of scope | Why |
|---|---|
| Voice and tone within published artifacts | Covered by [`run-35-vs-jetstream.md`](run-35-vs-jetstream.md) and [`run-37-validation.md`](run-37-validation.md) §"Side-by-side voice comparison" |
| IG step content depth (jetstream IG#3 = 1 paragraph; run-37 IG#3 = 12 lines) | Deliberate divergence per user direction — flat-beats-DRY, density buys rigor |
| KB body philosophy (intersection-trap vs porter-workflow tip) | Audience-model fork — product decision required before alignment |
| Yaml `extends:` adoption | Deliberate divergence per user direction — flat-beats-DRY |
| CLAUDE.md scope (framework-only vs framework + Zerops) | Substrate-decided split; coherent |
| Counter measurements | Covered by [`run-37-validation.md`](run-37-validation.md) §"Counter table" |

---

## Verdict

**Run-37 placement is at-bar or above on root recipe README + apps-repo zerops.yaml + apps-repo CLAUDE.md + tier import.yaml file-location-and-structure.** The two engine fixes that v9.78.0 shipped (V3 title + porter-meta line) closed the most-visible run-35 placement gaps cleanly.

**Below-bar placement findings are concentrated at three surfaces:**

1. **Tier README L1+L2** — bare title + back-link instead of `# {RecipeName} — {TierName} Environment` + recipe-link sentence. Closes via two template lines.
2. **Apps-repo README cover/deploy + section order + RF1/PD1 duplication** — five findings, three of which close via template-ordering or H2-wrapper additions. RF1/PD1 is the absence-gate already pre-scoped.
3. **KB sibling-shape divergence** — same residual class as run-35; sim-3 win still lost; needs product decision on shape before engine teeth.

Tier import.yaml placement (head-comment + per-service comment placement) is at-bar; substrate-compliance residuals (priority-justification missing, tier-4 TY2 BAD on db+cache) are voice/substrate axis, not placement.

**Recommend a next-iteration "placement engine fix-pack"**: (A)+(B) tier README banner + (D)+(E) apps-repo README cover/deploy/H2 + (G) RF1/PD1 absence gate. Estimated cost: ~10 template lines + ~5 tests + ~3 lines of Go gate code. All engine teeth, no substrate dependency. Same class of fix as the v9.78.0 V3-title + porter-meta-line engine teeth that just landed cleanly.
