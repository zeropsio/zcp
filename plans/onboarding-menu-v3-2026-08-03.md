# Plan: onboarding-menu-v3

## Run State
- `phase:` shape
- `base:` 4470e35212d91b2e0d937da90b75d1edbe34bf44
- `integration:` —
- `approved:` — (owner checkpoint 2026-08-03: mapping-in-playbook · sync persists categories+takeover-guide · taxonomy out of scope · GUI link app.zerops.io/recipes/<slug>)
- `codex:` round 1 approved-with-changes (/tmp/codex-out-1785758979-6567-1966.md); SHAPE plan gate approve-with-amendments, ALL amendments incorporated into register+briefs (/tmp/codex-out-1785779511-34141-13912.md)
- `next:` OWNER GATE 1: present register, on approval promote specs then start BUILD W1
<!-- PROVE closed 2026-08-03: P1 CONFIRMED (stage-URL correction absorbed), P2 REFUTED→Frame amended, P3 CONFIRMED. Owner cleanup pending in localflow db: table `greetings` (1 row) via psql over VPN. -->>

## Frame

**Outcome**: A first-time user who says "Onboard me to Zerops." in the container gets the v3 menu — **Build something** / **Try a ready-made recipe** / **What are Zerops & ZCP?** + example-rich escape line — where both recipe branches run on a deterministic authored language→slug mapping, the recipe branch ends in an ownership handoff (git-push-setup/export + GUI recipe page link + takeover-guide content), the third branch fetches an authored orientation playbook, and the whole thing is deployed and observable on the localflow container.

| obs | evidence |
|---|---|
| Authored onboarding = 1 playbook (~8 lines of copy) + AGENTS.md trigger; greeting/descriptions are model improvisation; test pins only bold labels + escape line | explore round 1: internal/knowledge/playbooks/onboarding.md:18-29; internal/tools/knowledge_playbook_content_test.go:39-43 |
| Recipe demo path: 39 import ymls, all `enableSubdomainAccess: true`; eval wall-time 3m04s–4m29s to live URL | explore round 1: internal/knowledge/recipes/*.import.yml; eval/behavioral/runs/ |
| Matcher gaps: generic "demo"/"hello world" → 0 candidates; Angular recipes carry `frameworks: [framework]` placeholder; ties break alphabetically (node → nestjs first) | explore round 1+Codex: internal/knowledge/recipe_matcher.go:24-82 |
| `route="recipe"` commit validates slug against the whole corpus, not routeOptions | [SELF-VERIFIED:internal/workflow/engine.go:329-334,367-384] |
| `recipeNarrow="dev-only"` exists (skip paid stage), never a silent default | [SELF-VERIFIED:internal/tools/workflow.go:77-80]; internal/workflow/recipe_shape.go:36-41 |
| Collision machinery exists: recipe shape confirm flips CREATE→EXISTS for existing managed deps | internal/workflow/recipe_shape.go:201; bootstrap_guide_assembly.go:295 |
| `zerops_import` blocks until processes complete → narration must precede import | Codex review: internal/tools/import.go:79 |
| Recipe close guidance already requires URL discovery + zerops_verify; gap = URL not structured into result; eval URL prober dead-stubbed | Codex review: internal/workflow/bootstrap_guide_assembly.go:347; internal/eval/verification.go:234,243 |
| Sync pull: one GET, no `fields[]` projection — every Strapi scalar arrives; recipeCategories + icon + source + recipe-level extracts (incl. `takeover-guide`) are fetched and DROPPED | frame-sync: internal/sync/pull_recipes.go:15-60,64 (live-verified, 44 recipes) |
| `.md` regenerated wholesale each pull + gitignored → new frontmatter keys must be emitted by `buildRecipeMarkdown`; import ymls committed separately | frame-sync: pull_recipes.go:106-127,135-211; .gitignore:53-59 |
| parseDocument consumes exactly 4 frontmatter keys (description/repo/languages/frameworks); extractFrontmatter is permissive, unknown keys silently dropped, no lint on taxonomy | frame-sync+KB: internal/knowledge/documents.go:87-176; recipe_lint_test.go |
| Strapi has NO curation field (featured/order/weight/popularity — nothing; list unsorted). FE curation = service-utility exclusion + `internalType` + `shape` ('app'/'software') + magic env key `ai-agent` | frame-fe: libs/models/src/recipes/recipes.model.ts:133-155; core/zerops-services/zerops-services.constant.ts:7 |
| `takeover-guide` is a recipe-level extract the GUI renders on the detail page ("app" shape: platform tier guide + takeover-guide; "software": takeover-guide or FE fallback const) | frame-fe: recipe-detail.page.ts:372-377,790,794-805 |
| GUI recipe routes: `/recipes` list, `/recipes/:slug` detail (slug-matched) | frame-fe: apps/zerops/src/modules/pages/+recipe/recipe.routes.ts:7-35 |
| FE agent-first wizard references NO recipes (pool claim + agent picker only) — onboarding recipe choice is 100% agent-side | frame-fe: core/zcp-pool-claim-base/zcp-onboard-wizard.component.ts |
| spec-onboarding preamble: "exactly two artifacts"; O1–O7 invariant table + exact pin needles; playbooks family = committed+embedded, direct-fetch-only, search-excluded | frame-kb: docs/spec-onboarding.md §preamble,§3,§5,§7; knowledge_playbook_content_test.go |
| Local corpus stale vs Strapi (upstream has recipe/tanstack-start/topcoat not present locally); elixir-minimal + wordpress have no import.yml → never offerable | frame-sync + explore round 1 |

**AC1** — Playbook opens with menu v3 verbatim (one-sentence Zerops orientation, three bold options, example-rich escape line); full bullet lines pinned. Planned evidence: extended `knowledge_playbook_content_test.go` needles + live container run transcript.
**AC2** — "Build something" branch: authored language→slug mapping (Node.js/Python/PHP/Laravel/Go/Rust/Bun/… → `*-hello-world`/`laravel-minimal`, post-remap corpus slugs), fixed default for "choose for me"; two-step consent (branch pick ≠ provisioning consent; footprint named incl. dev-only offer via `recipeNarrow` AND, when a managed dep flips to EXISTS, the fact that the recipe's migration will write into the existing database); concepts explained before the blocking import; the URL handed over is the STAGE service URL (dev is an idle `zsc noop` container answering 502 by design); after RUNNING the build continues into the develop loop toward the user's idea. Planned evidence: content pins + behavioral scenario parse (O7) + live run.
**AC3** — "Try a ready-made recipe" branch: same mapping; ends with verified URL handoff + ownership offer executed by the agent on request (git-push-setup / export) + GUI recipe page link (`https://app.zerops.io/recipes/<slug>` — publicly reachable; for 'app'-shape recipes the GUI renders the generic platform tier guide there, so the link carries value even where `takeover-guide` is unauthored). The playbook must NOT promise recipe-specific takeover content from the corpus. Planned evidence: content pins + scenario + live run.
**AC4** — Orientation playbook `zerops://playbooks/orientation` exists (committed, embedded, fetchable unsynced, search-excluded); "What are Zerops & ZCP?" branch fetches it; content covers project/services/containers, hostname networking, build→deploy→run, subdomain URLs, what ZCP is, consent boundaries. Planned evidence: URI-fetch + search-exclusion unit tests (O5 pattern) + content lint.
**AC5** — `docs/spec-onboarding.md` reconciled: artifact count, new labels, branch contracts, escape line, O-table; onboarding behavioral scenarios updated and parsing (O7). Planned evidence: spec diff + `TestOnboardingScenarios_ExistAndParse` green + full pin test green.
**AC6** — Sync pull persists `categories:` frontmatter and the recipe-level `takeover-guide` extract as a `## Take ownership` body section EMITTED ONLY WHEN NON-EMPTY (live data 2026-08-03: only wordpress + zerops-k8s-showcase carry it — future-proofing, not an onboarding dependency); `parseDocument` reads categories; `docs/recipes/README.md` frontmatter table updated (incl. the already-missing languages/frameworks rows); pull stays idempotent. Planned evidence: sync unit tests + one real `zcp sync pull recipes` run diff.
**AC7** — Recipe provision UX fixes (verifier-confirmed defects gating the URL promise): (a) the rendered provision YAML in the bootstrap guide is services-only — no `project:` block contradicting the "services section ONLY" instruction; (b) the composed stage subdomain URL (project-level `zeropsSubdomainHost` + service + port) is surfaced to the agent at/after provision so the handoff needs no manual API read. Planned evidence: unit tests on guide assembly rendering + URL composition; e2e `TestBootstrapRecipeRoute_AuthoredSlug_LiveURL` (stage 200 + dev 502 pins) per verifier test spec.
**AC8** — Deployed to localflow `zcp` service; live "Onboard me to Zerops." run shows menu v3 and a working Build-something happy path. Planned evidence: `./eval/scripts/build-deploy.sh` + transcript/screenshot.

**Non-goals**: local-machine file transfer; PaaS migration tooling; FE changes; matcher scoring rewrite (playbook mapping routes around it); Strapi schema additions (`featured` column) AND Strapi taxonomy data fixes + zcp taxonomy lint (owner decided 2026-08-03: not now); viability-gate dead-code resolution; new MCP tools/actions/state (O6 holds).
**Constraints**: playbooks family stays committed+embedded+search-excluded (O5); no mutating directive before `## 3. Branches` (O3); spec + pin needles move in the same commit; new frontmatter keys must be emitted by `buildRecipeMarkdown` or they're erased next pull; recipe `.md` bodies gitignored — corpus refresh via `zcp sync pull recipes` precedes build.
**Risk class**: medium — trigger: spec contract change (spec-onboarding O2–O7 + preamble) + multi-slice scope + owner asked.

**Assumptions**:
- [VERIFIED] `resolveRecipeMatch` accepts any corpus slug at commit — internal/workflow/engine.go:329-334,367-384.
- [VERIFIED] playbooks family mechanics for a second document (embed glob `all:playbooks`, URI fetch, search exclusion) — internal/knowledge/documents.go:10; internal/knowledge/engine.go:119.
- [VERIFIED] `takeover-guide` is a real recipe-level extract key in both FE contract and live Strapi response — recipe-detail.page.ts:790; frame-sync live GET.
- [VERIFIED] ownership mechanics exist: `git-push-setup` (container-capable, GIT_TOKEN user-held) + `zerops_export` — spec-workflows §4.3/§9.
- [VERIFIED] P1 CONFIRMED (ledger row, live run 2026-08-03): authored-slug commit ungated; full chain to HTTP 200 in 2m29s — with corrections: the 200 is the STAGE service (dev is 502 by design — `zsc noop` idle dev container); EXISTS flip is NOT read-only (recipe migration writes schema into the reused db); composed subdomain URL requires a project-level read (nothing surfaces it); rendered provision YAML contradicts the services-only instruction.
- [VERIFIED] P2 REFUTED as originally framed: `takeover-guide` is EMPTY for every mapped slug — only wordpress (1537 chars) + zerops-k8s-showcase (998) carry it (live GET 2026-08-03, 44 recipes). Frame amended: AC3 drops corpus takeover content; AC6 emits the section conditionally. Also: API slug is `node-js-hello-world`, remapped to `nodejs-hello-world` by .sync.yaml — the playbook mapping uses post-remap corpus slugs.
- [VERIFIED] P3: `/recipes` + `/recipes/:slug` carry NO canActivate guard (app.routes.ts:82-103 — neighboring routes have guards, recipe block has none); `https://app.zerops.io/recipes/nodejs-hello-world` → HTTP 200; Strapi data calls are unauthenticated (`zefSkipAuthHeader`).
- [ASSUMED] corpus staleness (missing tanstack/topcoat/recipe slugs) is resolved by a routine pre-build `zcp sync pull recipes`, not a code change.

## Evidence Ledger
| claim | gates | surface | command | observed | verdict | promote |
|---|---|---|---|---|---|---|
| P1 recipe-route commit w/ authored slug → live URL | AC2, AC3 | verifier | bootstrap start route=recipe recipeSlug=nodejs-hello-world on localflow (zcp-eval-clean unreachable from this token); container env via env vars; import; poll; curl | `route-menu for generic intent had ZERO recipe options; commit → session-active; import "All 6 processes completed"; stage ACTIVE+healthy, https://tmponb3astage-…-3000.prg1.zerops.app → 200 in 2m29s; dev ACTIVE but 502 BY DESIGN (zsc noop idle); db EXISTS flip dropped db from YAML, recipe migration WROTE table `greetings` into pre-existing db (residue listed to owner); URL must be composed from project-level zeropsSubdomainHost (service-level null, discover has no URL); rendered provision YAML carries project: block while guide says strip it` | CONFIRMED | e2e `TestBootstrapRecipeRoute_AuthoredSlug_LiveURL` per verifier test spec (stage 200 + dev 502 pins) |
| P2 takeover-guide populated for recommended slugs | AC3, AC6 | spike | GET api.zerops.io/api/recipes (44 rows) → count non-empty `sourceData.extracts["takeover-guide"]` | `total 44, non-empty 2: wordpress (1537c, shape=app), zerops-k8s-showcase (998c); ALL mapped hello-world/minimal slugs = 0` | REFUTED → Frame amended (AC3/AC6) | sync test: conditional `## Take ownership` emission |
| P3 GUI recipe page URL + auth reach | AC3 | spike | grep canActivate app.routes.ts:75-110; curl -w %{http_code} https://app.zerops.io/recipes{,/nodejs-hello-world} | `no guard on recipe routes; 200 + 200; Strapi calls zefSkipAuthHeader` | CONFIRMED | playbook link copy `https://app.zerops.io/recipes/<slug>` |

## Slice Register
| ID | Title | Depends | Files | Layers | Gate | State |
|---|---|---|---|---|---|---|
| S1 | Playbooks vertical: onboarding v3 rewrite + orientation doc + pins/fetch/exclusion + 14-slug corpus guard | — | internal/knowledge/playbooks/{onboarding,orientation}.md, internal/tools/knowledge_playbook_content_test.go, internal/tools/knowledge_playbooks_test.go, internal/knowledge/engine_playbooks_test.go | unit+tool | review | pending |
| S3 | Scenarios → v3 + retired-label drift guard in content test | S1 | eval/behavioral/scenarios/onboard-*.md + preseed/onboard-guided-on.sh, internal/eval/onboarding_scenarios_test.go, internal/content/eval_scenario_drift_test.go | unit | autonomous | pending |
| S4 | Sync: categories + conditional Take-ownership section, push-parser fragment boundaries, byte idempotency, README prose+diagram | — | internal/sync/{pull_recipes,transform}.go(+tests), internal/knowledge/documents.go(+test), docs/recipes/README.md | unit | autonomous | pending |
| S5a | Provision rendering: services-only YAML + executable env pre-steps (K+V, preprocess note) + bootstrap-recipe-import atom truth fix + golden | — | internal/workflow/bootstrap_guide_assembly.go(+test), internal/content/atoms/bootstrap-recipe-import.md + golden | unit+tool+integration | review | pending |
| S5b | Structured runtime URLs on BootstrapResponse (hostname/role/url/handoff), populated at L4 tools via ops.ResolveSubdomainURL, guidance derived from data, best-effort on resolve failure; present on post-provision/status/close | S5a | internal/workflow/bootstrap.go, internal/workflow/bootstrap_guide_assembly.go(+test), internal/tools/workflow_bootstrap.go(+tool tests), one integration test | unit+tool+integration | review | pending |
| S6 | e2e in DEDICATED disposable project (env ZCP_E2E_RECIPE_PROJECT_ID, emptiness preflight, allowlisted prefix, direct-read teardown): authored slug → confirm → services-only import (fresh db) → structured stage URL 200 / dev 502 | S1, S5b | e2e/bootstrap_recipe_route_test.go (+ e2e/safety_test.go only if allowlist extended) | e2e | review | pending |
Gate ∈ autonomous\|review\|owner · State ∈ pending\|building\|landed\|blocked. S2 merged into S1 (Codex gate: no dangling orientation URI). Waves: W1 = S1+S4+S5a (disjoint write-sets) · W2 = S3+S5b · W3 = S6.
Briefs: plans/briefs-onboard-v3/{S1,S3,S4,S5a,S5b,S6}.md. Deploy to localflow + live menu run (AC8) and the live-catalog check for the 14 mapped slugs (pull does not delete retired locals) are ASSEMBLE work, not slices.

## Verify Trace
| ACx | check | result | evidence |
|---|---|---|---|
| AC1 | `go test ./internal/tools -run TestPlaybookOnboarding_ContentPins_CoreContract -short -count=1 -v` (full-line pins) + live fetch of `zerops://playbooks/onboarding` on localflow shows v3 menu verbatim | not-run | |
| AC2 | pin needles (mapping slugs, recipeNarrow, stage-URL rule, EXISTS-write disclosure, pre-import concept ordering) in the same test + behavioral scenario `onboard-guided-on` parse | not-run | |
| AC3 | pin needles (git-push-setup/export offer, app.zerops.io/recipes/<slug> link, no corpus-takeover promise) + scenario parse | not-run | |
| AC4 | `go test ./internal/tools -run 'TestPlaybookOrientation|TestSearch_ExcludesPlaybooks' -short -count=1 -v` + `go test ./internal/content/... -short` | not-run | |
| AC5 | spec-onboarding.md diff reviewed at GATE 1; `go test ./internal/eval -run TestOnboardingScenarios -short -count=1 -v` | not-run | |
| AC6 | `go test ./internal/sync ./internal/knowledge -run 'TestBuildRecipeMarkdown|TestParseDocument' -short -count=1 -v` + one real `zcp sync pull recipes` in ASSEMBLE (diff: categories on all, Take-ownership only on wordpress) | not-run | |
| AC7 | `go test ./internal/workflow -run TestBootstrapGuide -short -count=1 -v` + integration suite + e2e `TestBootstrapRecipeRoute_AuthoredSlug_LiveURL` (stage 200, dev 502) | not-run | |
| AC8 | `./eval/scripts/build-deploy.sh` then live "Onboard me to Zerops." in the localflow container; transcript shows v3 menu + Build-something happy path to a verified stage URL | not-run | |
| — | negative/regression: full battery `go test ./... -short` + `make lint-local` + `TestNoBareZeropsURIInAgentContent` + `TestTemplatesContent_NoHardcodedVersions` + retired-label drift guard | not-run | |
| — | ASSEMBLE live-catalog check: all 14 mapped slugs present in the CURRENT Strapi catalog after `zcp sync pull recipes` (pull does not delete retired locals — embedded guard alone can miss retirement) | not-run | |

## Promotion
- Contracts → `docs/spec-onboarding.md`: §preamble (two → THREE artifacts: trigger block + onboarding playbook + orientation playbook), §3 (v3 fork copy verbatim + verbatim-render rule + new escape line), §4 (branch contracts: mapping table, two-step consent + EXISTS-write disclosure + dev-only offer, stage-URL handoff, ownership handoff + GUI link, orientation branch replaces themes/model tour, freeform bring lane, populated policy), §5 (family covers both playbooks), §7 (O2–O7 needle updates) — written at GATE 1.
- Contracts → `docs/spec-workflows.md` §8 RCO amendment: provision-step YAML rendered services-only (project.envVariables surfaced as explicit pre-step); post-provision guidance carries composed subdomain URLs (project-level zeropsSubdomainHost via ops.ResolveSubdomainURL) with stage URL as handoff — written at GATE 1.
- Invariants → tests: `TestPlaybookOnboarding_ContentPins_CoreContract` (extended), `TestPlaybookOrientation_ContentPins_CoreContract`, `TestOnboardingScenarios_ExistAndParse` (v3 guard), `TestBuildRecipeMarkdown_EmitsCategories`, `TestBuildRecipeMarkdown_TakeoverGuide_EmittedOnlyWhenNonEmpty`, `TestParseDocument_ReadsCategories`, `TestBootstrapGuide_ProvisionYAML_ServicesOnly`, `TestBootstrapGuide_CloseGuidance_ComposedStageURL`, e2e `TestBootstrapRecipeRoute_AuthoredSlug_LiveURL`.
- `docs/recipes/README.md` frontmatter table: + categories, + the already-shipping languages/frameworks rows.
- CLAUDE.md trap line (≤1): none (the EXISTS-flip-writes-to-db fact lands in spec-workflows RCO wording; revisit at LAND if a second call site appears).
- This plan → `plans/archive/` on LAND close; briefs dir with it.
