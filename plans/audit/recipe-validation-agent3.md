# Recipe Validation Report -- Agent 3

Recipes validated: nestjs, nextjs-ssr, nextjs-static, nuxt, payload-cms

---

# Validation: nestjs

## Repo: recipe-nestjs

## Findings

### F1: Missing `run.base` in recipe zerops.yml
- Severity: P1
- Category: yaml-mismatch
- Recipe says: (no `run.base` specified)
- Repo says: `run.base: nodejs@20`
- Recommendation: Add `run.base: nodejs@20` under `run:` in the recipe zerops.yml. While Zerops may infer the runtime base from the service type, the repo explicitly sets it and the recipe should match.

### F2: Missing `cache` in recipe zerops.yml
- Severity: P2
- Category: yaml-mismatch
- Recipe says: `cache: node_modules`
- Repo says: (no `cache` key in zerops.yml)
- Recommendation: The recipe has `cache` but the repo does not. This is a recipe improvement over the repo -- the recipe is actually better here. No fix needed; note the discrepancy for documentation accuracy. ACTUALLY CORRECT in recipe.

### F3: DATABASE_HOST uses cross-service ref in recipe but hardcoded in repo
- Severity: P1
- Category: env-var-pattern
- Recipe says: `DATABASE_HOST: ${db_hostname}`
- Repo says: `DATABASE_HOST: db`
- Recommendation: Per decision rules, hardcoded `db` matching the import hostname `db` is CORRECT. The recipe should use `DATABASE_HOST: db` (hardcoded), not `${db_hostname}`. The cross-service ref works but is unnecessarily indirect when the hostname is known and static.

### F4: DATABASE_PORT uses cross-service ref in recipe but hardcoded in repo
- Severity: P1
- Category: env-var-pattern
- Recipe says: `DATABASE_PORT: ${db_port}`
- Repo says: `DATABASE_PORT: "5432"`
- Recommendation: Per decision rules, hardcoded well-known PostgreSQL port 5432 is CORRECT. The recipe should use `DATABASE_PORT: "5432"`, not `${db_port}`.

### F5: DATABASE_USERNAME uses cross-service ref in recipe but hardcoded in repo
- Severity: P1
- Category: env-var-pattern
- Recipe says: `DATABASE_USERNAME: ${db_user}`
- Repo says: `DATABASE_USERNAME: db`
- Recommendation: The repo hardcodes `db` which is the default PostgreSQL username matching the hostname. Per decision rules for hostnames matching import, this is acceptable. However, `${db_user}` is also correct as a cross-service reference. Lower concern but recipe diverges from repo. Change recipe to `DATABASE_USERNAME: ${db_user}` is acceptable (secret value). Keep as-is or match repo -- either way is defensible. Marking P1 for divergence from repo.

### F6: initCommands uses `${appVersionId}` but repo uses `$ZEROPS_appVersionId`
- Severity: P0
- Category: yaml-mismatch
- Recipe says: `zsc execOnce ${appVersionId} npm run typeorm:migrate`
- Repo says: `zsc execOnce $ZEROPS_appVersionId npm run typeorm:migrate`
- Recommendation: The correct Zerops built-in variable is `$ZEROPS_appVersionId` (env var). The recipe uses `${appVersionId}` which is zerops.yml interpolation syntax and may not resolve the same way. Fix recipe to use `$ZEROPS_appVersionId`.

### F7: Missing `run.base` field
- Severity: P1
- Category: yaml-mismatch
- Recipe says: (no `run.base`)
- Repo says: `run.base: nodejs@20`
- Recommendation: Add `run.base: nodejs@20` to recipe. (Same as F1, consolidated.)

### F8: Recipe import.yml omits services present in repo import
- Severity: P2
- Category: knowledge-gap
- Recipe says: 4 services (api, db, storage, mailpit)
- Repo says: 6 services (api, app/static, db, storage, adminer, mailpit) plus `buildFromGit`, `project:` block, `verticalAutoscaling`, `priority` values differ
- Recommendation: Recipe correctly simplifies by omitting `app` (static UI), `adminer` (dev tool), and `buildFromGit` refs. The `verticalAutoscaling` and different `priority` values in the repo import are minor operational differences. Recipe import is a clean minimal version. No fix needed, but mention adminer as optional in Gotchas (recipe already does this).

### F9: Recipe claims trust proxy is required but repo main.ts does NOT set it
- Severity: P1
- Category: knowledge-gap
- Recipe says: `app.set('trust proxy', true)` required in main.ts; Configuration section shows it
- Repo says: `main.ts` only does `app.enableCors(); await app.listen(3000);` -- no trust proxy
- Recommendation: The recipe's Configuration section and Gotchas emphasize trust proxy as required, but the actual repo code does NOT implement it. Either: (a) the repo is missing this and should add it, or (b) the recipe overstates the requirement. For recipe accuracy, note that the repo does not include trust proxy. The recipe guidance is correct best practice, but should note the repo omits it.

### F10: Recipe Configuration section shows direct DataSource creation but repo uses ConfigService pattern
- Severity: P2
- Category: knowledge-gap
- Recipe says: `export const dataSource = new DataSource({...})` with direct `process.env.*` access
- Repo says: Uses `getDbConfig(configService)` via NestJS ConfigService pattern, with a separate `typeorm-data-source.ts` that wraps it
- Recommendation: The recipe's code snippet is a simplified illustration. The actual repo pattern is more idiomatic NestJS (using ConfigService DI). Minor documentation improvement -- could show the actual pattern.

## Verdict: NEEDS_FIX (5 issues)
- P0: 1 (F6: appVersionId syntax)
- P1: 3 (F1/F7: missing run.base, F3: DATABASE_HOST pattern, F4: DATABASE_PORT pattern)
- P2: 2 (F8: import simplification, F10: code snippet style)

Note: F5 (DATABASE_USERNAME) is borderline -- `${db_user}` is a valid cross-service ref for a credential. F9 is a knowledge gap where the recipe recommends best practice the repo doesn't implement.

---

# Validation: nextjs-ssr

## Repo: recipe-nextjs-nodejs

## Findings

### F1: Missing `run.base` in recipe zerops.yml
- Severity: P1
- Category: yaml-mismatch
- Recipe says: (no `run.base` specified)
- Repo says: `run.base: nodejs@20`
- Recommendation: Add `run.base: nodejs@20` under `run:` in the recipe zerops.yml.

### F2: Recipe includes `cache` and `envVariables` not present in repo
- Severity: P2
- Category: yaml-mismatch
- Recipe says: `cache: node_modules` and `envVariables: NODE_ENV: production`
- Repo says: (neither `cache` nor `envVariables` present in zerops.yml)
- Recommendation: These are recipe improvements over the bare-minimum repo. `cache: node_modules` speeds rebuilds and `NODE_ENV: production` is best practice. The recipe is better here. No fix needed -- these are correct additions.

### F3: Import files match (minimal)
- Severity: PASS
- Category: n/a
- Recipe says: single `nodejs@20` service with `enableSubdomainAccess: true`
- Repo says: same, plus `project:` block and `buildFromGit`
- Recommendation: Recipe correctly strips repo-specific `buildFromGit` and `project:` metadata. Match confirmed.

## Verdict: NEEDS_FIX (1 issue)
- P1: 1 (F1: missing run.base)
- P2: 1 (F2: recipe has extras vs repo, but these are improvements)

---

# Validation: nextjs-static

## Repo: recipe-nextjs-static

## Findings

### F1: zerops.yml matches perfectly
- Severity: PASS
- Category: n/a
- Recipe says: build base `nodejs@20`, buildCommands `pnpm i` + `pnpm build`, deployFiles `out/~`, run base `static`, cache `node_modules`
- Repo says: build base `nodejs@20`, buildCommands `pnpm i` + `pnpm build`, deployFiles `out/~` (as array), run base `static`
- Recommendation: Near-perfect match. Recipe adds `cache: node_modules` which the repo omits -- this is a recipe improvement.

### F2: Import files match
- Severity: PASS
- Category: n/a
- Recipe says: single `static` service with `enableSubdomainAccess: true`
- Repo says: same, plus `project:` block and `buildFromGit`
- Recommendation: Match confirmed.

### F3: next.config.mjs has `output: 'export'` as recipe requires
- Severity: PASS
- Category: n/a
- Recipe says: Must have `output: 'export'` in next.config
- Repo says: `next.config.mjs` contains `output: 'export'`
- Recommendation: Confirmed.

## Verdict: PASS

---

# Validation: nuxt

## Repo: recipe-nuxt-nodejs

## Findings

### F1: Missing `run.base` in recipe zerops.yml
- Severity: P1
- Category: yaml-mismatch
- Recipe says: (no `run.base` specified)
- Repo says: `run.base: nodejs@20`
- Recommendation: Add `run.base: nodejs@20` under `run:` in the recipe zerops.yml.

### F2: Repo has `prepareCommands` not in recipe
- Severity: P2
- Category: yaml-mismatch
- Recipe says: (no `prepareCommands`)
- Repo says: `prepareCommands: - node -v`
- Recommendation: The `prepareCommands: - node -v` in the repo is a diagnostic/no-op command. Recipe correctly omits it as non-functional. No fix needed.

### F3: Recipe has `envVariables` not in repo
- Severity: P2
- Category: yaml-mismatch
- Recipe says: `envVariables: NODE_ENV: production`
- Repo says: (no `envVariables` in zerops.yml)
- Recommendation: Recipe improvement over repo. `NODE_ENV: production` is correct best practice for SSR Node.js. No fix needed.

### F4: Import files match
- Severity: PASS
- Category: n/a
- Recipe says: single `nodejs@20` service with `enableSubdomainAccess: true`
- Repo says: same plus `project:` block and `buildFromGit`
- Recommendation: Match confirmed.

### F5: Nuxt config has no NITRO_PRESET but recipe says it auto-detects
- Severity: PASS
- Category: n/a
- Recipe says: "No explicit preset configuration is needed in `nuxt.config.ts` -- Nuxt auto-detects the correct preset during build"
- Repo says: `nuxt.config.ts` has no preset configuration
- Recommendation: Consistent. Recipe correctly documents auto-detection.

### F6: Package manager is yarn in repo, recipe shows yarn too
- Severity: PASS
- Category: n/a
- Recipe says: `yarn` / `yarn build`
- Repo says: `packageManager: yarn@4.5.0`, buildCommands `yarn` / `yarn build`
- Recommendation: Match confirmed.

## Verdict: NEEDS_FIX (1 issue)
- P1: 1 (F1: missing run.base)
- P2: 2 (F2: prepareCommands omitted, F3: recipe adds NODE_ENV)

---

# Validation: payload-cms

## Repo: recipe-payload

## Findings

### F1: Repo has additional buildCommand `zsc test tcp -6 mailpit:1025`
- Severity: P2
- Category: yaml-mismatch
- Recipe says: buildCommands do NOT include mailpit TCP test
- Repo says: `zsc test tcp -6 mailpit:1025 --timeout 30s` (after the db TCP test)
- Recommendation: The repo waits for the mailpit service during build. Recipe omits this. Since the recipe also omits mailpit entirely from its import.yml, this is consistent omission. However, if someone adds mailpit, they should know about this. Add a note in Gotchas that the full repo also waits for mailpit.

### F2: Repo import has mailpit service not in recipe
- Severity: P2
- Category: knowledge-gap
- Recipe says: 3 services (db, storage, api)
- Repo says: 4 services (db, storage, mailpit, api)
- Recommendation: Recipe correctly simplifies by omitting mailpit (a dev-only email catcher). The `payload.config.ts` does have hardcoded mailpit config (`host: 'mailpit'`, `port: 1025`), so anyone deploying without mailpit would see email failures in logs but the app would still run. Mention in Gotchas.

### F3: Repo import uses `priority: 1` for all services; recipe uses `priority: 10`
- Severity: P2
- Category: yaml-mismatch
- Recipe says: `priority: 10` for db and storage
- Repo says: `priority: 1` for all services
- Recommendation: Both values ensure these services start before the app. The actual number difference is cosmetic (lower = higher priority in Zerops, or they all start together regardless). Minor discrepancy, no functional impact.

### F4: Repo import objectStorageSize is 1; recipe says 2
- Severity: P2
- Category: yaml-mismatch
- Recipe says: `objectStorageSize: 2`
- Repo says: `objectStorageSize: 1`
- Recommendation: Recipe allocates 2 GB, repo allocates 1 GB. Both are valid. The recipe is slightly more generous. Minor discrepancy. Match repo or document the choice.

### F5: Repo zerops.yml has commented-out `pnpm payload migrate:create initial`
- Severity: PASS
- Category: n/a
- Recipe says: (not mentioned)
- Repo says: `# pnpm payload migrate:create initial` (commented out)
- Recommendation: Recipe correctly omits commented-out code. No action needed.

### F6: Repo payload.config.ts hardcodes mailpit but recipe doesn't mention email config
- Severity: P2
- Category: knowledge-gap
- Recipe says: (no mention of email/nodemailer configuration)
- Repo says: `payload.config.ts` uses `nodemailerAdapter` with hardcoded `host: 'mailpit'`, `port: 1025`
- Recommendation: Add a Gotchas entry noting that the repo has email configured via nodemailer to a mailpit service, and users should update email settings for production.

### F7: Repo payload.config.ts hardcodes S3 region `us-east-1`
- Severity: PASS
- Category: n/a
- Recipe says: (implicitly uses S3 with storage_ cross-service refs)
- Repo says: `region: 'us-east-1'` hardcoded in s3Storage config
- Recommendation: Consistent with Zerops object storage convention. No issue.

### F8: Recipe RUNTIME_ prefix env vars match repo exactly
- Severity: PASS
- Category: n/a
- Recipe says: Build env vars use `${RUNTIME_*}` prefix
- Repo says: Same set of `RUNTIME_*` build env vars
- Recommendation: Match confirmed.

### F9: Run envVariables match between recipe and repo
- Severity: PASS
- Category: n/a
- Recipe says: `DATABASE_URI: ${db_connectionString}/${db_dbName}`, `NEXT_PUBLIC_SERVER_URL: ${zeropsSubdomain}`, S3 vars
- Repo says: identical
- Recommendation: Match confirmed.

### F10: Recipe import has no `cache` in zerops.yml but recipe does
- Severity: PASS
- Category: n/a
- Recipe says: `cache: node_modules`
- Repo says: (no explicit `cache`)
- Note: Checking repo zerops.yml again -- repo does NOT have `cache`. Recipe adds it as improvement.

### F11: Recipe does not mention PAYLOAD_SECRET in run envVariables
- Severity: P1
- Category: yaml-mismatch
- Recipe says: run envVariables has no PAYLOAD_SECRET
- Repo says: run envVariables has no PAYLOAD_SECRET either; it comes from envSecrets in import.yml
- Recommendation: Both recipe and repo rely on envSecrets for PAYLOAD_SECRET at runtime. The recipe import.yml correctly includes `envSecrets: PAYLOAD_SECRET: <@generateRandomString(<24>)>`. The recipe Configuration section correctly explains the RUNTIME_ prefix mechanism. PASS -- no issue.

## Verdict: NEEDS_FIX (4 issues, all P2)
- P0: 0
- P1: 0
- P2: 4 (F1: missing mailpit buildCommand, F2: missing mailpit service, F4: objectStorageSize differs, F6: email config not documented)

---

# Summary

| Recipe | Verdict | P0 | P1 | P2 |
|--------|---------|----|----|-----|
| nestjs | NEEDS_FIX | 1 | 3 | 2 |
| nextjs-ssr | NEEDS_FIX | 0 | 1 | 1 |
| nextjs-static | PASS | 0 | 0 | 0 |
| nuxt | NEEDS_FIX | 0 | 1 | 2 |
| payload-cms | NEEDS_FIX | 0 | 0 | 4 |

## Critical issues requiring immediate attention

1. **nestjs F6 (P0)**: `${appVersionId}` must be `$ZEROPS_appVersionId` in initCommands
2. **nestjs F1/F7 (P1)**: Missing `run.base: nodejs@20`
3. **nestjs F3 (P1)**: DATABASE_HOST should be hardcoded `db`, not `${db_hostname}`
4. **nestjs F4 (P1)**: DATABASE_PORT should be hardcoded `"5432"`, not `${db_port}`
5. **nextjs-ssr F1 (P1)**: Missing `run.base: nodejs@20`
6. **nuxt F1 (P1)**: Missing `run.base: nodejs@20`

## Common pattern: missing `run.base`

Three of five recipes (nestjs, nextjs-ssr, nuxt) are missing `run.base: nodejs@20` in their zerops.yml. All three corresponding repos explicitly set it. This appears to be a systematic omission during recipe creation.
