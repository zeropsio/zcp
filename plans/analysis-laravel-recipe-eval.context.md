# Context: analysis-laravel-recipe-eval

**Last updated**: 2026-03-24
**Iterations**: 1
**Task type**: codebase-analysis

## Decision Log

| # | Decision | Evidence | Iteration | Rationale |
|---|----------|----------|-----------|-----------|
| D1 | Remove `--isolated` from Laravel recipe (and all recipes) | Deploy failed with QueryException; second deploy without it succeeded | 1 | `zsc execOnce` already handles concurrency; `--isolated` creates chicken-and-egg with CACHE_STORE=database |
| D2 | `DB_HOST: db` is correct, not a bug | Zerops DNS resolves service hostnames within project | 1 | Using `${db_hostname}` would resolve to internal address, not the DNS-friendly hostname |
| D3 | Recipe needs dev-mode awareness markers | Recipe is production-only; bootstrap creates dev services | 1 | LLM copies recipe verbatim, resulting in production config on dev services |

## Resolved Concerns

| # | Concern | Evidence | Raised In | Resolved In | Resolution |
|---|---------|----------|-----------|-------------|------------|
| 1 | Missing sudo on command | zerops.yml had `sudo -E -u zerops` present | 1 | 1 | User hypothesis disproven — actual cause was QueryException from --isolated |
| 2 | Static DB credentials | Generated config used `${db_dbName}`, `${db_password}`, etc. (dynamic) | 1 | 1 | LLM was more correct than recipe; only DB_HOST=db is static (by design) |

## Open Questions (Unverified)

- Should all recipes have dev/prod variant sections? Or should the workflow strip prod-only items?
- Does `--isolated` also fail with `CACHE_STORE=redis` when no Redis is available? (likely yes)
- Should we add a recipe lint test that detects `--isolated` + `CACHE_STORE=database` conflict?

## Confidence Map

| Section/Area | Confidence | Evidence Basis |
|-------------|-----------|---------------|
| --isolated root cause | VERIFIED | Two deploys (fail/success), runtime logs |
| APP_ENV mismatch | VERIFIED | Recipe source + generated config |
| DB_HOST correctness | VERIFIED | Zerops DNS + discover response |
| sudo presence | VERIFIED | Generated zerops.yml content |
| Dev-mode recipe gap | LOGICAL | Recipe has prod-only config, workflow creates dev services |
