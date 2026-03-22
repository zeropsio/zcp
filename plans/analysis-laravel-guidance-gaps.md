# Analysis: Laravel Bootstrap Guidance Gaps — Iteration 1
**Date**: 2026-03-22
**Scope**: Knowledge delivery chain for Laravel (php-nginx) simple-mode bootstrap
**Agents**: kb (zerops-knowledge), primary (architecture+correctness)
**Complexity**: Deep (ultrathink)
**Task**: Audit why an agent deploying Laravel on php-nginx@8.4 (simple mode) failed to use envSecrets for APP_KEY, used php artisan serve, created .env file, and never loaded the Laravel recipe

## Summary

The Laravel recipe (`internal/knowledge/recipes/laravel.md`) contains comprehensive, correct guidance for every mistake the agent made — APP_KEY via envSecrets, `--no-scripts` to prevent .env shadowing, "never use php artisan serve", scaffolding steps. But **none of this knowledge reached the agent** because:

1. Recipe loading is marked "optional" in bootstrap.md (line 67)
2. The briefing system shows recipe HINTS but doesn't inject recipe CONTENT
3. Bootstrap workflow (provision + generate steps) never mentions framework-specific secrets (envSecrets)
4. No conceptual model explaining generated secrets (import.yml) vs discovered secrets (zerops_discover)

The knowledge exists. The delivery pipeline doesn't push it through.

## Findings by Severity

### Critical

| # | Finding | Evidence | Source |
|---|---------|----------|--------|
| F1 | **Framework secrets (APP_KEY, etc.) not mentioned in any bootstrap step** — provision section guides import.yml creation but never mentions envSecrets for framework keys; generate sections only discuss zerops.yml envVariables | bootstrap.md:82-329 — zero mentions of envSecrets for framework keys across provision, generate, generate-simple, generate-standard, generate-dev sections | Both agents |
| F2 | **Recipe content never auto-injected** — briefing system shows "Available recipes... use `zerops_knowledge recipe=X` to load" but agent must actively choose to load. If ignored, all framework-specific guidance (APP_KEY, scaffolding, artisan serve warning) is lost | briefing.go:49-61 (matchingRecipes returns names only), guidance.go:85-97 (assembleKnowledge never calls GetRecipe) | Primary |
| F3 | **Recipe loading labeled "optional"** — bootstrap.md:67 says "This is optional — platform knowledge is delivered automatically. Recipes add framework-specific patterns on top." This is misleading — for Laravel, "framework-specific patterns" includes APP_KEY setup, which is not optional | bootstrap.md:59-67 (discover section) | KB |

### Major

| # | Finding | Evidence | Source |
|---|---------|----------|--------|
| F4 | **No distinction between generated secrets and discovered secrets** — provision guidance only covers discovering existing env vars from managed services (DB credentials). Never explains that framework secrets must be GENERATED via `<@generateRandomString(...)>` in import.yml envSecrets | bootstrap.md:122-152 (env var discovery protocol), core.md:272,275 (envSecrets rules exist but not referenced from bootstrap) | Both agents |
| F5 | **Provision validation checklist missing envSecrets check** — checklist at bootstrap.md:109-118 validates hostnames, types, duplicates, object-storage, preprocessor, mode — but NOT framework-specific envSecrets | bootstrap.md:109-118 | KB |
| F6 | **`.env` prohibition is generic, not framework-aware** — bootstrap.md says "Do NOT create .env files" (line 210, 671) but doesn't warn that `composer create-project` (without `--no-scripts`) creates one automatically that shadows envSecrets. The recipe covers this (laravel.md:123) but recipe wasn't loaded | bootstrap.md:210,671 vs laravel.md:115-123 | KB + orchestrator |

### Minor

| # | Finding | Evidence | Source |
|---|---------|----------|--------|
| F7 | **PHP runtime guide mentions "laravel" in keywords but has zero Laravel content** — Agent gets php.md at generate step but it's generic PHP only (build, documentRoot, TRUSTED_PROXIES). No bridge from "you're using PHP" → "load the Laravel recipe" | php.md:4 (keywords) vs php.md content (60 lines, no Laravel guidance) | Primary |
| F8 | **`zerops_env` tool exists but never referenced in bootstrap workflow** — tool supports `action="set"` for service/project-level secrets post-import, but bootstrap.md never mentions it as a fallback for setting framework secrets | env.go:12-62, bootstrap.md (no zerops_env reference) | Primary |

## Recommendations (evidence-backed)

| # | Recommendation | Priority | Evidence | Effort |
|---|---------------|----------|----------|--------|
| R1 | **Auto-inject recipe content at generate step** — when `matchingRecipes()` returns results AND the user's intent mentions a framework name, inject the recipe markdown into the briefing (not just hints). Modify `guidance.go:assembleKnowledge()` to call `GetRecipe()` for the matched recipe | P0 | briefing.go:49-61 shows current hint-only behavior; incident proves agents ignore hints | Medium — add recipe injection to assembleKnowledge() |
| R2 | **Add framework-secrets section to provision guidance** — after "Env var discovery protocol" in bootstrap.md, add guidance: "If framework needs generated secrets (APP_KEY, SECRET_KEY_BASE), add `envSecrets` with `<@generateRandomString(...)>` and `#yamlPreprocessor=on` to import.yml" | P0 | bootstrap.md:122-152 covers discovered vars but not generated secrets; laravel.md:89 shows correct pattern | Low — add ~10 lines to bootstrap.md provision section |
| R3 | **Upgrade recipe loading from "optional" to "recommended"** — change bootstrap.md:67 from "This is optional" to "Strongly recommended for known frameworks — recipes contain critical setup patterns (secrets, scaffolding, gotchas) not covered by generic runtime guides" | P0 | bootstrap.md:67 current wording; incident proves "optional" leads to skipping | Low — edit 2 lines |
| R4 | **Add provision checklist item for envSecrets** — add to bootstrap.md:109-118: "Framework secrets \| Does framework need envSecrets? (APP_KEY, SECRET_KEY_BASE, etc.)" | P1 | bootstrap.md:109-118 current checklist; laravel.md:89 shows pattern | Low — add 1 line |
| R5 | **Add framework-aware .env warning to generate sections** — current "Do NOT create .env files" is too generic. Add: "Tools like `composer create-project` create .env files automatically. Use `--no-scripts` to prevent this. Empty values in .env shadow valid envSecrets." | P1 | bootstrap.md:210 (generic warning), laravel.md:123 (specific warning), incident (.env created by composer) | Low — add 2 lines |
| R6 | **PHP runtime guide: add recipe bridge** — add line to php.md: "For frameworks (Laravel, Symfony, Nette): load the framework recipe — `zerops_knowledge recipe=\"laravel\"` — for complete setup patterns" | P2 | php.md has zero framework-specific content despite listing "laravel" in keywords | Low — add 3 lines |
| R7 | **Document zerops_env as post-import fallback in provision** — mention that `zerops_env action="set"` can set secrets after import, but envSecrets in import.yml is preferred (write-once, preprocessor-generated) | P2 | env.go:12-62 exists, bootstrap.md never references it | Low — add 2 lines |

## Evidence Map

| Finding | Confidence | Basis |
|---------|-----------|-------|
| F1 | VERIFIED | Read bootstrap.md:82-329, confirmed zero envSecrets mentions |
| F2 | VERIFIED | Read briefing.go:49-61, guidance.go:85-97, confirmed hint-only behavior |
| F3 | VERIFIED | Read bootstrap.md:67, confirmed "optional" wording |
| F4 | VERIFIED | Read bootstrap.md:122-152, confirmed discovery-only focus |
| F5 | VERIFIED | Read bootstrap.md:109-118, confirmed missing checklist item |
| F6 | VERIFIED | Read bootstrap.md:210,671 vs laravel.md:115-123, confirmed gap |
| F7 | VERIFIED | Read php.md:4 vs full content, confirmed no Laravel guidance |
| F8 | VERIFIED | Read env.go:12-62, grep bootstrap.md for zerops_env — zero hits |

## Self-Challenge Results

All CRITICAL and MAJOR findings verified against source code with file paths and line numbers. No downgrades needed. The evidence chain is:

1. Knowledge EXISTS: laravel.md has APP_KEY (line 89), --no-scripts (line 123), artisan serve warning (line 151)
2. Knowledge NOT DELIVERED: guidance.go never calls GetRecipe() (line 85-97), only GetBriefing() which shows hints
3. Workflow DOESN'T BRIDGE: bootstrap.md provision/generate sections never mention envSecrets for frameworks
4. Agent IGNORED hints: "optional" label in bootstrap.md:67 gave permission to skip

## Incident Replay — What Should Have Happened

| Step | What agent DID | What agent SHOULD have done | Knowledge source |
|------|---------------|---------------------------|-----------------|
| Discover | Identified php-nginx@8.4, simple mode | Same, PLUS load recipe: `zerops_knowledge recipe="laravel"` | bootstrap.md:63 (currently "optional") |
| Provision | import.yml without envSecrets | import.yml WITH `envSecrets: APP_KEY: <@generateRandomString(<32>)>` and `#yamlPreprocessor=on` | laravel.md:84-90 (recipe not loaded) |
| Generate | `composer create-project laravel/laravel .` | `composer create-project laravel/laravel . --no-scripts` + `composer run post-autoload-dump` | laravel.md:115-124 (recipe not loaded) |
| Generate | APP_KEY hardcoded in zerops.yml envVariables | APP_KEY already set via import.yml envSecrets — no zerops.yml entry needed | laravel.md:139 (recipe not loaded) |
| Deploy | Tested with `php artisan serve` on port 8080 | Used built-in php-nginx on port 80 | laravel.md:151, bootstrap.md:687 (deploy recovery table) |
| Debug | Tried invalid key lengths, manually generated keys | N/A — APP_KEY would have been correct from import.yml envSecrets | laravel.md:89 |
