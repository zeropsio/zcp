# Recipe Validation Report -- Agent 5 (alt-runtime recipes)

Recipes validated: bun, bun-hono (no repo), deno, gleam, rust

---

# Validation: bun

## Repo: recipe-bun

### F1: Hostname mismatch -- recipe uses `app`, repo uses `api`
- Severity: P1
- Category: yaml-mismatch
- Recipe says: `setup: app` in zerops.yml, `hostname: app` in import.yml
- Repo says: `setup: api` in zerops.yml, `hostname: api` in import.yml
- Recommendation: Change recipe to use `api` as the hostname to match the repo convention shared across all Zerops recipes

### F2: Build base version drift -- recipe says bun@1.2, repo says bun@1.1
- Severity: P1
- Category: version-drift
- Recipe says: `base: bun@1.2` (build), type `bun@1.2` (import)
- Repo says: `base: bun@1.1` (build+run), type `bun@1.1` (import)
- Recommendation: Verify which version Zerops currently supports and update recipe. The recipe is likely intentionally ahead of the repo; keep recipe at 1.2 if that is the latest supported version, but document the version difference

### F3: Recipe missing `run.base` field
- Severity: P2
- Category: yaml-mismatch
- Recipe says: no `base:` under `run:` section
- Repo says: `base: bun@1.1` under `run:` section
- Recommendation: Add `base: bun@1.2` to the run section in the recipe if the runtime base is required (some Zerops runtimes need it)

### F4: Build command differs -- `bun run build` vs explicit bun build
- Severity: P2
- Category: yaml-mismatch
- Recipe says: `bun build src/index.ts --outdir dist --target bun`
- Repo says: `bun run build` (which runs `bun build src/main.ts --outdir ./dist --target bun` via package.json)
- Recommendation: No functional difference since the npm script runs the same command. Recipe is more explicit which is fine for documentation. Note the different entrypoint filename: `src/index.ts` (recipe) vs `src/main.ts` (repo).

### F5: Start command differs
- Severity: P2
- Category: yaml-mismatch
- Recipe says: `start: bun run dist/index.js`
- Repo says: `start: bun run start:prod` (which runs `bun run dist/main.js`)
- Recommendation: Both are valid approaches. Recipe uses direct execution, repo uses npm script. The actual binary name differs (`index.js` vs `main.js`) consistent with source file naming.

### F6: Missing `PORT` env var in repo
- Severity: P2
- Category: yaml-mismatch
- Recipe says: `PORT: "3000"` in envVariables
- Repo says: no PORT env var; hardcodes port 3000 in `Bun.serve({ port: 3000 })`
- Recommendation: The recipe adds PORT as a best practice. No issue -- recipe is more configurable.

### F7: Recipe import.yml has Valkey service, repo does not
- Severity: P1
- Category: yaml-mismatch
- Recipe says: includes `hostname: cache, type: valkey@7.2` service
- Repo says: no Valkey/cache service at all
- Recommendation: The recipe adds a cache service that the repo never uses. Either remove Valkey from the recipe (to match the actual repo) or document that it is an enhancement. Since the recipe title says "Bun + PostgreSQL + Valkey", this is intentional but the repo code does not actually use Valkey.

### F8: Recipe import.yml has `envSecrets` block, repo does not
- Severity: P1
- Category: yaml-mismatch
- Recipe says: envSecrets with DATABASE_URL, REDIS_HOST, REDIS_PORT
- Repo says: no envSecrets at all; env vars are only in zerops.yml envVariables
- Recommendation: The envSecrets block references services (cache) that the repo does not use. Align with repo or clearly document this as an "enhanced" template.

### F9: Non-secret values in envSecrets
- Severity: P1
- Category: env-var-pattern
- Recipe says: `REDIS_HOST: ${cache_host}` and `REDIS_PORT: ${cache_port}` in envSecrets
- Repo says: N/A
- Recommendation: `REDIS_HOST` and `REDIS_PORT` are not secrets. They contain cross-service references which require import.yml placement, but `envSecrets` is semantically for secrets. However, Zerops requires cross-service refs (`${...}` syntax) to be in import.yml, and `envSecrets` is the only place to put them. This is acceptable per Zerops conventions -- the naming is misleading but functionally correct.

### F10: Repo source code missing `hostname: "0.0.0.0"` binding
- Severity: P0
- Category: knowledge-gap
- Recipe says: "CRITICAL: Bun.serve() must bind to 0.0.0.0" and shows code with `hostname: "0.0.0.0"`
- Repo says: `Bun.serve({ fetch: handler, port: 3000 })` -- NO hostname specified
- Recommendation: The repo has a critical bug per the recipe's own documentation. The app may work because Bun's default may be 0.0.0.0, but the recipe correctly warns about this. The recipe documentation is correct; the repo should be fixed. However, since Bun.serve() defaults to 0.0.0.0, this works in practice.

### F11: Recipe missing `#yamlPreprocessor=on` consistency note
- Severity: P2
- Category: yaml-mismatch
- Recipe says: import.yml has `#yamlPreprocessor=on`
- Repo says: import.yml has no preprocessor directive (but also no `${...}` in import.yml)
- Recommendation: The recipe needs the preprocessor because it uses `${...}` references in envSecrets. The repo does not need it because it has no cross-service references in import.yml. Consistent.

### F12: Priority values differ
- Severity: P2
- Category: yaml-mismatch
- Recipe says: `priority: 10` for db and cache, `priority: 5` for app
- Repo says: `priority: 1` for db only, no priority for api
- Recommendation: Minor difference. Recipe uses explicit priorities to ensure services start in order. Both approaches work.

## Verdict: NEEDS_FIX (4 issues: F1, F7, F8, F9 are P1; F10 is P0 but works in practice due to Bun defaults)

---

# Validation: bun-hono

## Repo: (none -- internal consistency check only)

### F1: `node_modules` in deployFiles may be unnecessary
- Severity: P2
- Category: knowledge-gap
- Recipe says: deployFiles includes `node_modules` with a gotcha explaining "required if migration scripts import ORM packages that cannot be fully bundled"
- Internal consistency: The recipe correctly documents the tradeoff and when to omit it. Consistent.

### F2: `DATABASE_HOST: db` is hardcoded hostname in envSecrets
- Severity: P2
- Category: env-var-pattern
- Recipe says: `DATABASE_HOST: db` in envSecrets
- Decision rule: Hardcoded hostname matching import.yml hostname (`hostname: db`) is CORRECT per validation rules
- Recommendation: No issue.

### F3: Non-secret values in envSecrets
- Severity: P1
- Category: env-var-pattern
- Recipe says: `AWS_USE_PATH_STYLE_ENDPOINT: "true"` in envSecrets
- Recommendation: This is a static boolean config, not a secret and not a cross-service reference. It should be in zerops.yml `envVariables` instead of import.yml `envSecrets`. However, it is placed in envSecrets alongside other S3 vars for organizational convenience. Functionally works but semantically incorrect.

### F4: `DATABASE_PORT: ${db_port}` uses cross-service ref
- Severity: P2
- Category: env-var-pattern
- Recipe says: `DATABASE_PORT: ${db_port}` in envSecrets
- Decision rule: Cross-service refs require import.yml placement. Correct.

### F5: Missing health check in zerops.yml
- Severity: P2
- Category: yaml-mismatch
- Recipe says: no healthCheck in zerops.yml run section
- Internal consistency: The Configuration section shows a `/status` endpoint but zerops.yml does not configure a health check for it. Inconsistency.
- Recommendation: Add healthCheck httpGet for /status on port 3000 to zerops.yml, or remove the /status example from Configuration section.

## Verdict: NEEDS_FIX (2 issues: F3 is P1, F5 is P2 internal inconsistency)

---

# Validation: deno

## Repo: recipe-deno

### F1: Major version drift -- recipe says deno@2, repo says deno@1
- Severity: P0
- Category: version-drift
- Recipe says: `base: deno@2` (build), `type: deno@2` (import)
- Repo says: `base: deno@1` (build+run), `type: deno@1` (import)
- Recommendation: This is a major version difference. Deno 2 has significant API changes from Deno 1. The recipe represents the forward-looking version; verify Zerops supports deno@2 in import.yml. The MEMORY.md confirms `deno@2` is the correct import type.

### F2: Hostname mismatch -- recipe uses `api`, repo uses `api`
- Severity: N/A
- Category: N/A
- Both recipe and repo use `api` as the hostname. No issue.

### F3: Build command differs significantly
- Severity: P1
- Category: yaml-mismatch
- Recipe says: `buildCommands: deno cache main.ts`
- Repo says: `buildCommands: deno task build` (which runs esbuild to bundle into `dist/bundle.js`)
- Recommendation: The recipe uses a simple `deno cache` approach (no bundling, source-file deploy). The repo uses esbuild bundling into dist/. These are fundamentally different deploy strategies. Recipe deploys source files (`main.ts`, `deno.json`, `deno.lock`); repo deploys bundled output (`dist/`, `deno.jsonc`). The recipe approach is simpler and more idiomatic for Deno 2.

### F4: Deploy files differ
- Severity: P1
- Category: yaml-mismatch
- Recipe says: `deployFiles: [main.ts, deno.json, deno.lock]`
- Repo says: `deployFiles: [dist, deno.jsonc]`
- Recommendation: Consistent with the different build strategy (F3). Recipe deploys source; repo deploys bundle. Recipe approach is cleaner for Deno 2.

### F5: Start command differs
- Severity: P1
- Category: yaml-mismatch
- Recipe says: `start: deno run --allow-net --allow-env --allow-read main.ts`
- Repo says: `start: deno task start` (which runs `deno run --allow-net --allow-env --allow-read dist/bundle.js`)
- Recommendation: Consistent with different deploy strategy. Both use explicit permission flags.

### F6: Recipe missing `run.base` field
- Severity: P2
- Category: yaml-mismatch
- Recipe says: no `base:` under `run:` section
- Repo says: `base: deno@1` under `run:` section
- Recommendation: Consider adding `base: deno@2` to the run section if required by Zerops

### F7: Env vars use different patterns
- Severity: P1
- Category: env-var-pattern
- Recipe says: `DB_HOST: ${db_hostname}`, `DB_PORT: ${db_port}`, `DB_USER: ${db_user}`, `DB_PASS: ${db_password}` (cross-service refs in zerops.yml envVariables)
- Repo says: `DB_HOST: db`, `DB_USER: db`, `DB_NAME: db`, `DB_PASS: ${db_password}` (hardcoded hostnames)
- Recommendation: Recipe uses `${db_hostname}` cross-service ref in zerops.yml envVariables. Per Zerops platform rules, cross-service `${...}` references should be in import.yml `envSecrets`, NOT in zerops.yml `envVariables`. The repo's approach of hardcoding `db` (matching the import hostname) is actually correct. Recipe has an env-var placement error.

### F8: Recipe uses `DB_NAME: ${db_hostname}` pattern inconsistency
- Severity: P2
- Category: env-var-pattern
- Recipe says: `DB_NAME: db` (hardcoded, but this is for database name, not hostname)
- Repo says: `DB_NAME: db` (same)
- Recommendation: No issue. `DB_NAME` is `db` which is the default database name in Zerops PostgreSQL (matches the service hostname). Note: this is the database name, not a hostname reference.

### F9: Repo uses Oak framework, recipe shows raw Deno.serve
- Severity: P2
- Category: knowledge-gap
- Recipe says: Shows `Deno.serve()` API directly
- Repo says: Uses `oak` web framework (`Application`, `Router`)
- Recommendation: Recipe chose to show the simpler Deno.serve pattern which is framework-agnostic. The recipe keywords mention "oak, hono, fresh" which is helpful. No major issue.

### F10: Repo uses `app.listen({ port })` without explicit hostname binding
- Severity: P2
- Category: knowledge-gap
- Recipe says: "Bind 0.0.0.0" as a gotcha, shows `Deno.serve({ port: 8000, hostname: "0.0.0.0" }, handler)`
- Repo says: `await app.listen({ port })` -- no explicit 0.0.0.0 binding
- Recommendation: Oak's `app.listen()` defaults to 0.0.0.0, so this works. But the recipe correctly documents the explicit binding requirement for Deno.serve. No issue.

### F11: Recipe import.yml missing priority for services
- Severity: P2
- Category: yaml-mismatch
- Recipe says: no priority on `api` service, `priority: 10` on `db`
- Repo says: `priority: 1` on `db` only
- Recommendation: Both omit priority on the runtime service (which is fine). Priority value differs (10 vs 1) but both ensure db starts first. Minor.

### F12: Gotcha claims "DB env vars use cross-service references"
- Severity: P1
- Category: env-var-pattern
- Recipe says (Gotchas): "DB env vars use cross-service references -- use `${db_hostname}`, `${db_port}`, `${db_user}`, `${db_password}` syntax, not hardcoded service names"
- Repo says: Uses hardcoded `db` for DB_HOST, DB_USER, DB_NAME (which is correct per decision rules)
- Recommendation: The gotcha actively tells users NOT to hardcode service names, contradicting the repo's (correct) approach. Cross-service `${...}` refs in zerops.yml envVariables are problematic. The gotcha should say: hardcode hostnames in zerops.yml when they match the import hostname; use `${...}` refs only in import.yml envSecrets.

## Verdict: NEEDS_FIX (7 issues: F1 is P0; F3, F4, F5, F7, F12 are P1)

---

# Validation: gleam

## Repo: recipe-gleam

### F1: Recipe missing `run.base` field
- Severity: P2
- Category: yaml-mismatch
- Recipe says: no `base:` under `run:` section
- Repo says: `base: gleam@1.5` under `run:` section
- Recommendation: Add `base: gleam@1.5` to recipe zerops.yml run section

### F2: Recipe missing cache configuration
- Severity: P2
- Category: yaml-mismatch
- Recipe says: `cache: [build, _gleam_artefacts]` in zerops.yml
- Repo says: no cache section in zerops.yml
- Recommendation: Recipe adds caching as a best practice. No issue -- recipe is enhanced.

### F3: Gleam app name in gleam.toml is `app`, recipe uses `api` as hostname
- Severity: P2
- Category: knowledge-gap
- Recipe says: `setup: api` (hostname)
- Repo says: `gleam.toml` has `name = "app"`, zerops.yml has `setup: api`
- Recommendation: The gleam package name (`app`) is different from the Zerops service hostname (`api`). This is fine -- they are independent. No issue.

### F4: Recipe missing health check in zerops.yml
- Severity: P1
- Category: yaml-mismatch
- Recipe says: has `healthCheck: httpGet: port: 3000, path: /status`
- Repo says: no healthCheck in zerops.yml
- Recommendation: Recipe adds health check; repo omits it. Recipe is better -- the repo should add it. No recipe fix needed.

Wait -- re-reading: The recipe DOES have healthCheck. The repo does NOT. This means the recipe is more complete than the repo. No recipe fix needed.

### F5: Recipe shows `wisp` code example that matches repo
- Severity: N/A
- Recipe Configuration section shows Wisp-based router. Repo uses Wisp + Mist. Consistent.

### F6: Repo binds on `0.0.0.0` correctly
- Severity: N/A
- Repo says: `mist.bind("0.0.0.0")` in app.gleam line 46
- Recipe says: "configure Wisp/Mist to bind all interfaces"
- Consistent. No issue.

### F7: Recipe shows `gleam_pgo` for DB, repo uses `gleam/pgo` (same library)
- Severity: N/A
- Both reference the same library. Consistent.

### F8: Repo has `glenvy` dotenv dependency not mentioned in recipe
- Severity: P2
- Category: knowledge-gap
- Recipe says: nothing about dotenv loading
- Repo says: uses `glenvy/dotenv` to load .env file
- Recommendation: Minor. The dotenv loading is a development convenience and does not affect production deployment on Zerops.

## Verdict: PASS (no P0/P1 issues affecting recipe correctness; F1 is P2 missing run.base)

---

# Validation: rust

## Repo: recipe-rust

### F1: Recipe zerops.yml matches repo closely
- Severity: N/A
- Recipe and repo zerops.yml are nearly identical for: base, buildCommands, deployFiles, ports, envVariables, healthCheck, start
- Both use `base: rust@stable`, `cargo build --release`, `./target/release/~/rust`, port 8080, `./rust` start
- Env vars match exactly: DB_NAME=db, DB_HOST=${db_hostname}, DB_PORT=${db_port}, DB_USER=${db_user}, DB_PASS=${db_password}

### F2: Recipe missing `run.base` field
- Severity: P2
- Category: yaml-mismatch
- Recipe says: no `base:` under `run:` section
- Repo says: `base: rust@stable` under `run:` section
- Recommendation: Add `base: rust@stable` to recipe zerops.yml run section

### F3: Recipe missing `cache: target` in zerops.yml (it IS there)
- Severity: N/A
- Recipe says: `cache: target` -- present
- Repo says: no cache section
- Recipe is enhanced with caching. No issue.

### F4: Repo uses `[::]:8080` bind, recipe says `0.0.0.0:8080`
- Severity: P2
- Category: yaml-mismatch
- Recipe says: `.bind("0.0.0.0:8080")` in Configuration code example
- Repo says: `.bind("[::]:8080")` in main.rs line 106
- Recommendation: `[::]:8080` binds to all IPv6 interfaces which on dual-stack systems also accepts IPv4. This works on Zerops. The recipe's `0.0.0.0` is more explicit for IPv4. Both work but they are different. Recipe could mention `[::]` as an alternative.

### F5: Recipe import.yml matches repo
- Severity: N/A
- Both have: hostname: api (rust@stable), hostname: db (postgresql@16, NON_HA)
- Recipe uses `priority: 10` for db; repo uses `priority: 1`. Minor difference.

### F6: Repo uses `dotenv` crate not mentioned in recipe
- Severity: P2
- Category: knowledge-gap
- Recipe says: nothing about dotenv
- Repo says: `use dotenv::dotenv;` and `dotenv().ok();` in main.rs
- Recommendation: Minor. Dotenv is a development convenience. Not relevant for Zerops deployment.

### F7: Binary name consistency
- Severity: N/A
- Cargo.toml: `name = "rust"`, deployFiles: `./target/release/~/rust`, start: `./rust`
- Recipe: `deployFiles: ./target/release/~/rust`, start: `./rust`
- All consistent.

### F8: Recipe env vars use cross-service refs in zerops.yml envVariables
- Severity: P1
- Category: env-var-pattern
- Recipe says: `DB_HOST: ${db_hostname}`, `DB_PORT: ${db_port}`, `DB_USER: ${db_user}`, `DB_PASS: ${db_password}` in zerops.yml envVariables
- Repo says: identical pattern
- Decision rule context: Per Zerops platform rules, cross-service `${...}` references should technically be in import.yml envSecrets, not zerops.yml envVariables. However, the repo itself uses this pattern and it appears to work. This may be a valid Zerops feature where zerops.yml envVariables also resolves cross-service refs.
- Recommendation: Verify with Zerops docs whether `${...}` cross-service refs work in zerops.yml envVariables. If they do, both recipe and repo are correct. If not, both need to move these to import.yml envSecrets. Marking as P1 pending verification.

## Verdict: NEEDS_FIX (1 issue: F8 is P1 pending verification; F2, F4 are P2)

---

# Summary

| Recipe | Verdict | P0 | P1 | P2 |
|--------|---------|----|----|-----|
| bun | NEEDS_FIX | 1* | 4 | 5 |
| bun-hono | NEEDS_FIX | 0 | 1 | 2 |
| deno | NEEDS_FIX | 1 | 5 | 4 |
| gleam | PASS | 0 | 0 | 3 |
| rust | NEEDS_FIX | 0 | 1 | 3 |

*bun P0 (F10) works in practice due to Bun.serve() defaulting to 0.0.0.0

## Cross-cutting issues

1. **`run.base` missing from all recipes**: bun, deno, gleam, rust recipes all omit the `base:` field under `run:`. All repos include it. This may or may not be required by Zerops -- needs verification.

2. **Cross-service `${...}` refs in zerops.yml envVariables**: The deno and rust recipes (and repos) use `${db_hostname}` etc. in zerops.yml envVariables. The bun recipe correctly uses hardcoded hostnames in zerops.yml and puts cross-service refs in import.yml envSecrets. Need to verify which approach Zerops actually supports. If zerops.yml envVariables does NOT resolve cross-service refs, this is a P0 across deno and rust recipes.

3. **Hostname convention**: Most repos use `api` as the runtime hostname. The bun recipe uses `app` instead. Standardize to `api` or `app` across all recipes.

4. **Priority values**: Repos use `priority: 1`, recipes use `priority: 10` or `priority: 5`. Both work but are inconsistent.
