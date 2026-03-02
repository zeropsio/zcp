# Recipe Validation Report -- Agent 1 (PHP Recipes)

Validated: laravel-jetstream, filament, twill, nette, nette-contributte

---

# Validation: laravel-jetstream

## Repo: recipe-laravel-jetstream

## Findings

### F1: Missing MAIL_* env vars in recipe zerops.yml
- Severity: P2 (completeness)
- Category: yaml-mismatch
- Recipe says: (no MAIL_* variables)
- Repo says: `MAIL_FROM_ADDRESS: hello@example.com`, `MAIL_FROM_NAME: ZeropsLaravel`, `MAIL_HOST: mailpit`, `MAIL_MAILER: smtp`, `MAIL_PORT: 1025`
- Recommendation: Add the 5 MAIL_* env vars to the recipe zerops.yml. These reference the `mailpit` service which is also missing from the recipe import.yml.

### F2: Missing mailpit service in recipe import.yml
- Severity: P2 (completeness)
- Category: yaml-mismatch
- Recipe says: 4 services (app, db, redis, storage)
- Repo says: 5 services (app, db, redis, storage, mailpit as `go@1` with `buildFromGit`)
- Recommendation: Add the mailpit service to the recipe import.yml, or document its omission. The zerops.yml MAIL_HOST references `mailpit` so the service must exist.

### F3: Missing verticalAutoscaling in recipe import.yml
- Severity: P2 (completeness)
- Category: yaml-mismatch
- Recipe says: (no verticalAutoscaling on app service)
- Repo says: `verticalAutoscaling: { minRam: 0.25, minFreeRamGB: 0.125 }`
- Recommendation: Add verticalAutoscaling to recipe import.yml app service. This is a common pattern across all Zerops recipes.

### F4: Missing APP_NAME in recipe import.yml envSecrets
- Severity: P2 (completeness)
- Category: yaml-mismatch
- Recipe says: envSecrets has `APP_NAME: ZeropsLaravelJetstream`
- Repo says: envSecrets has `APP_NAME: ZeropsLaravelJetstreamDev`
- Recommendation: Values differ slightly (recipe omits "Dev" suffix). This is acceptable since recipe is a template, but noting the divergence.

### F5: Missing buildFromGit, project section, and tags in recipe import.yml
- Severity: P2 (cosmetic)
- Category: yaml-mismatch
- Recipe says: (no project section, no buildFromGit, no tags)
- Repo says: Has `project: { name: laravel-jetstream-devel, tags: [zerops-recipe, development] }` and `buildFromGit` on app/mailpit
- Recommendation: No change needed. Recipe import.yml is intended as a template for bootstrap, not a full project import. buildFromGit and project metadata are repo-specific.

## Verdict: PASS (0 P0/P1, 5 P2 issues)

---

# Validation: filament

## Repo: recipe-filament

## Findings

### F1: Build OS mismatch -- recipe omits os field (defaults differ)
- Severity: P1 (wrong behavior)
- Category: yaml-mismatch
- Recipe says: build section has no `os:` field (no explicit OS)
- Repo says: `os: alpine` on both build and run sections
- Recommendation: Add `os: alpine` to both build and run sections in the recipe zerops.yml. The repo explicitly uses alpine for both, and the recipe silently diverges.

### F2: Run OS mismatch -- recipe omits os field
- Severity: P1 (wrong behavior)
- Category: yaml-mismatch
- Recipe says: run section has no `os:` field
- Repo says: `os: alpine` on run section
- Recommendation: Add `os: alpine` to the recipe run section. Without this, the platform default may differ from what the repo intends.

### F3: Missing APP_NAME env var in recipe zerops.yml
- Severity: P2 (completeness)
- Category: yaml-mismatch
- Recipe says: (no APP_NAME in envVariables)
- Repo says: `APP_NAME: "Filament X Zerops"` in envVariables
- Recommendation: Add `APP_NAME: "Filament X Zerops"` to recipe zerops.yml envVariables.

### F4: Missing MAIL_* env vars in recipe zerops.yml
- Severity: P2 (completeness)
- Category: yaml-mismatch
- Recipe says: (no MAIL_* variables)
- Repo says: `MAIL_FROM_ADDRESS: hello@example.com`, `MAIL_FROM_NAME: FilamentZerops`, `MAIL_HOST: mailpit`, `MAIL_MAILER: smtp`, `MAIL_PORT: 1025`
- Recommendation: Add the 5 MAIL_* env vars to the recipe zerops.yml. These reference the `mailpit` service.

### F5: Missing mailpit service in recipe import.yml
- Severity: P2 (completeness)
- Category: yaml-mismatch
- Recipe says: 4 services (app, db, redis, storage)
- Repo says: 5 services (app, db, redis, mailpit, storage)
- Recommendation: Add the mailpit service or document its omission.

### F6: Missing verticalAutoscaling in recipe import.yml
- Severity: P2 (completeness)
- Category: yaml-mismatch
- Recipe says: (no verticalAutoscaling on app service)
- Repo says: `verticalAutoscaling: { minRam: 0.25, minFreeRamGB: 0.125 }`
- Recommendation: Add verticalAutoscaling to recipe import.yml app service.

### F7: TRUSTED_PROXIES missing from zerops.yml but commented out in repo code
- Severity: P2 (completeness)
- Category: knowledge-gap
- Recipe says: Configuration section notes "TRUSTED_PROXIES -- not set by default in Filament recipe; add TRUSTED_PROXIES: '*' if behind Zerops L7 balancer"
- Repo says: `bootstrap/app.php` has `// $middleware->trustProxies(at: env('TRUSTED_PROXIES', '*'));` (commented out)
- Recommendation: Recipe correctly notes this. Consider recommending uncommenting this line in the Gotchas section since Zerops always uses L7 balancer.

## Verdict: NEEDS_FIX (2 P1 issues: OS fields missing on both build and run)

---

# Validation: twill

## Repo: recipe-twill

## Findings

### F1: Missing APP_NAME env var in recipe zerops.yml
- Severity: P2 (completeness)
- Category: yaml-mismatch
- Recipe says: (no APP_NAME in envVariables)
- Repo says: `APP_NAME: ZeropsTwill` in envVariables
- Recommendation: Add `APP_NAME: ZeropsTwill` to recipe zerops.yml envVariables.

### F2: Missing MAIL_* env vars in recipe zerops.yml
- Severity: P2 (completeness)
- Category: yaml-mismatch
- Recipe says: (no MAIL_* variables)
- Repo says: `MAIL_FROM_ADDRESS: hello@example.com`, `MAIL_FROM_NAME: ZeropsLaravel`, `MAIL_HOST: mailpit`, `MAIL_MAILER: smtp`, `MAIL_PORT: 1025`
- Recommendation: Add the 5 MAIL_* env vars to the recipe zerops.yml.

### F3: Missing mailpit service in recipe import.yml
- Severity: P2 (completeness)
- Category: yaml-mismatch
- Recipe says: 4 services (app, db, redis, storage)
- Repo says: 5 services (app, db, redis, mailpit, storage)
- Recommendation: Add the mailpit service or document its omission.

### F4: twill:install command differs -- recipe omits --env=development flag
- Severity: P2 (completeness)
- Category: yaml-mismatch
- Recipe says: `php artisan twill:install -n`
- Repo says: `php artisan twill:install -n --env=development`
- Recommendation: Add `--env=development` flag to match repo. Without it, twill:install may behave differently.

### F5: Recipe includes view:cache, config:cache, route:cache, optimize but repo has them commented out
- Severity: P1 (wrong behavior)
- Category: yaml-mismatch
- Recipe says: initCommands include `php artisan view:cache`, `php artisan config:cache`, `php artisan route:cache` (after the execOnce commands, see recipe line 81-86 area -- these are NOT present in recipe, but recipe has no caching commands at all)
- Repo says: Lines 84-87 are commented out: `#        - php artisan view:cache`, `#        - php artisan config:cache`, `#        - php artisan route:cache`, `#        - php artisan optimize`
- Recommendation: CRITICAL -- the recipe zerops.yml does NOT include these caching commands either, so this is actually correct. However, the Gotchas section should mention that Twill is incompatible with artisan caching commands (view:cache, config:cache, route:cache, optimize) which is why they are commented out in the repo.

### F6: Missing verticalAutoscaling in recipe import.yml
- Severity: P2 (completeness)
- Category: yaml-mismatch
- Recipe says: (no verticalAutoscaling on app service)
- Repo says: `verticalAutoscaling: { minRam: 0.25, minFreeRamGB: 0.125 }`
- Recommendation: Add verticalAutoscaling to recipe import.yml app service.

### F7: league/flysystem-aws-s3-v3 NOT in composer.json
- Severity: P1 (wrong behavior)
- Category: knowledge-gap
- Recipe says: Gotchas mention "league/flysystem-aws-s3-v3 must be in composer.json"
- Repo says: `league/flysystem-aws-s3-v3` is NOT in `composer.json` require section (only in composer.lock via transitive dependencies from `area17/twill`)
- Recommendation: Update the Gotchas to clarify that `area17/twill` bundles S3 support via its own dependencies. The flysystem package is a transitive dependency, not a direct requirement. The current advice to "add league/flysystem-aws-s3-v3 to composer.json" is misleading for Twill.

### F8: Missing TRUSTED_PROXIES in recipe zerops.yml
- Severity: P2 (completeness)
- Category: knowledge-gap
- Recipe says: (no TRUSTED_PROXIES variable)
- Repo says: (no TRUSTED_PROXIES variable either)
- Recommendation: Consider adding `TRUSTED_PROXIES: "*"` since Twill runs behind Zerops L7 balancer. The laravel-jetstream recipe includes it.

## Verdict: NEEDS_FIX (2 P1 issues: commented-out caching gotcha undocumented, flysystem advice misleading)

---

# Validation: nette

## Repo: recipe-nette

## Findings

### F1: Preprocessor directive mismatch
- Severity: P0 (breaks deploy)
- Category: yaml-mismatch
- Recipe says: `#yamlPreprocessor=on`
- Repo says: `#zeropsPreprocessor=on`
- Recommendation: The recipe uses `#yamlPreprocessor=on` which is the CORRECT modern syntax per MEMORY.md. The repo uses the legacy `#zeropsPreprocessor=on`. The recipe is correct here. However, this means the repo itself has the wrong preprocessor tag. No recipe change needed, but flagging the discrepancy.

### F2: Redis service type mismatch -- valkey@7.2 vs keydb@6
- Severity: P0 (breaks deploy)
- Category: version-drift
- Recipe says: `type: valkey@7.2` for redis service
- Repo says: `type: keydb@6` for redis service
- Recommendation: CRITICAL. The repo uses `keydb@6` which is a different service type entirely. Zerops may have migrated from KeyDB to Valkey, but this is a major divergence. If the platform still supports `keydb@6`, the recipe is wrong. If KeyDB was deprecated in favor of Valkey, the repo is outdated. Verify which is currently valid on the Zerops platform.

### F3: Missing os: field in recipe zerops.yml
- Severity: P1 (wrong behavior)
- Category: yaml-mismatch
- Recipe says: no `os:` field on build or run sections
- Repo says: `os: alpine` on both build and run sections
- Recommendation: Add `os: alpine` to both build and run sections in the recipe.

### F4: Missing adminer service in recipe import.yml
- Severity: P2 (completeness)
- Category: yaml-mismatch
- Recipe says: 3 services (app, db, redis)
- Repo says: 4 services (app, db, redis, adminer as `php-apache@8.3` with `buildFromGit`)
- Recommendation: Add adminer service or document its omission.

### F5: Missing verticalAutoscaling in recipe import.yml
- Severity: P2 (completeness)
- Category: yaml-mismatch
- Recipe says: (no verticalAutoscaling on app service)
- Repo says: `verticalAutoscaling: { minRam: 0.25, minFreeRamGB: 0.125 }`
- Recommendation: Add verticalAutoscaling to recipe import.yml app service.

### F6: $appVersionId quoting difference
- Severity: P2 (cosmetic)
- Category: yaml-mismatch
- Recipe says: `zsc execOnce ${appVersionId} -- php /var/www/bin/console migrations:continue`
- Repo says: `zsc execOnce $appVersionId -- php /var/www/bin/console migrations:continue` (no curly braces)
- Recommendation: Both forms work in shell, but the recipe uses `${appVersionId}` (with braces) while repo uses `$appVersionId` (without). Standardize on `${appVersionId}` for consistency with other recipes. Recipe is correct.

### F7: envVariables placement differs
- Severity: P2 (cosmetic)
- Category: yaml-mismatch
- Recipe says: envVariables appears before initCommands in run section
- Repo says: envVariables appears after healthCheck (at the bottom of run section)
- Recommendation: Ordering within YAML mapping is not semantically significant. No change needed.

### F8: Recipe describes sessions stored in Redis but repo config shows Redis sessions
- Severity: P2 (completeness)
- Category: knowledge-gap
- Recipe says: "Sessions stored in Redis (Valkey) -- not file-based"
- Repo says: `common.neon` has Redis configured with two connections: default (storage=true, sessions=false) and session (storage=false, sessions with TTL). This is actually a sophisticated dual-connection Redis setup.
- Recommendation: The recipe correctly notes Redis sessions, but could mention the dual-connection pattern (separate Redis databases for cache vs sessions).

## Verdict: NEEDS_FIX (1 P0: keydb@6 vs valkey@7.2 type mismatch; 1 P1: missing os: alpine)

---

# Validation: nette-contributte

## Repo: recipe-nette-contributte

## Findings

### F1: Preprocessor directive mismatch
- Severity: P0 (breaks deploy)
- Category: yaml-mismatch
- Recipe says: `#yamlPreprocessor=on`
- Repo says: `#zeropsPreprocessor=on`
- Recommendation: Same as nette -- recipe uses the correct modern syntax. Repo uses legacy tag. No recipe change needed.

### F2: Redis service type mismatch -- valkey@7.2 vs keydb@6
- Severity: P0 (breaks deploy)
- Category: version-drift
- Recipe says: `type: valkey@7.2` for redis service
- Repo says: `type: keydb@6` for redis service
- Recommendation: CRITICAL. Same issue as nette recipe. Major type divergence between recipe and repo.

### F3: Missing os: field in recipe zerops.yml
- Severity: P1 (wrong behavior)
- Category: yaml-mismatch
- Recipe says: no `os:` field on build or run sections
- Repo says: `os: alpine` on both build and run sections
- Recommendation: Add `os: alpine` to both build and run sections in the recipe.

### F4: Missing adminer service in recipe import.yml
- Severity: P2 (completeness)
- Category: yaml-mismatch
- Recipe says: 3 services (app, db, redis)
- Repo says: 5 services (app, db, redis, adminer, mailpit)
- Recommendation: Add adminer and/or mailpit services or document their omission.

### F5: Missing mailpit service in recipe import.yml
- Severity: P2 (completeness)
- Category: yaml-mismatch
- Recipe says: 3 services
- Repo says: includes `mailpit` as `go@1` with `buildFromGit`
- Recommendation: Add mailpit service or document its omission.

### F6: Missing verticalAutoscaling in recipe import.yml
- Severity: P2 (completeness)
- Category: yaml-mismatch
- Recipe says: (no verticalAutoscaling on app service)
- Repo says: `verticalAutoscaling: { minRam: 0.25, minFreeRamGB: 0.125 }`
- Recommendation: Add verticalAutoscaling to recipe import.yml app service.

### F7: Recipe correctly documents Bootstrap.php env injection
- Severity: (informational -- PASS)
- Category: knowledge-gap
- Recipe says: `$configurator->addDynamicParameters(['env' => getenv()]);`
- Repo says: `$configurator->addDynamicParameters(['env' => getenv()]);` in `app/Bootstrap.php:19`
- Recommendation: None. Recipe accurately documents this.

### F8: Recipe correctly documents Monolog SyslogHandler
- Severity: (informational -- PASS)
- Category: knowledge-gap
- Recipe says: `Monolog\Handler\SyslogHandler` in contributte.neon monolog section
- Repo says: `contributte.neon` line 31: `- Monolog\Handler\SyslogHandler(app)` plus additional processors (WebProcessor, IntrospectionProcessor, MemoryPeakUsageProcessor, ProcessIdProcessor)
- Recommendation: Recipe could mention the additional Monolog processors for completeness, but this is minor.

### F9: Recipe correctly documents admin credentials
- Severity: (informational -- PASS)
- Category: knowledge-gap
- Recipe says: "Admin login: admin@admin.cz, password from ADMIN_PASSWORD env var"
- Repo says: ADMIN_PASSWORD generated in envSecrets
- Recommendation: None. Recipe is accurate.

### F10: Database connection config verified correct
- Severity: (informational -- PASS)
- Category: knowledge-gap
- Recipe says: `DATABASE_HOSTNAME`, `DATABASE_USER`, `DATABASE_PASSWORD`, `DATABASE_NAME`, `DATABASE_PORT` as separate env vars
- Repo says: `parameters.neon` maps these to `%env.DATABASE_HOSTNAME%`, `%env.DATABASE_USER%`, etc., which feed into `nettrine.neon` DBAL connection config
- Recommendation: None. Recipe accurately reflects the env var to config mapping.

## Verdict: NEEDS_FIX (1 P0: keydb@6 vs valkey@7.2 type mismatch; 1 P1: missing os: alpine)

---

# Summary

| Recipe | Verdict | P0 | P1 | P2 |
|--------|---------|----|----|-----|
| laravel-jetstream | PASS | 0 | 0 | 5 |
| filament | NEEDS_FIX | 0 | 2 | 5 |
| twill | NEEDS_FIX | 0 | 2 | 6 |
| nette | NEEDS_FIX | 1 | 1 | 5 |
| nette-contributte | NEEDS_FIX | 1 | 1 | 4 |

## Cross-cutting issues

1. **keydb@6 vs valkey@7.2** (nette, nette-contributte): Both repos use `keydb@6` while recipes say `valkey@7.2`. This needs platform verification -- if Zerops deprecated KeyDB in favor of Valkey, the repos are outdated (recipe is correct). If keydb@6 is still valid, recipes must match.

2. **Missing `os:` fields** (filament, nette, nette-contributte): All three repos explicitly set `os: alpine` but recipes omit it. This should be added to avoid platform default mismatches.

3. **Missing mailpit/adminer services**: All Laravel-based repos (jetstream, filament, twill) include a `mailpit` service; Nette repos include `adminer`. Recipes omit these auxiliary services. This is acceptable for a minimal template but should be documented.

4. **Missing verticalAutoscaling**: All repos include `verticalAutoscaling` on the app service; all recipes omit it. Consider adding as a standard pattern.

5. **Preprocessor tag**: Nette repos use legacy `#zeropsPreprocessor=on`; recipes correctly use `#yamlPreprocessor=on`. Repos should be updated, not recipes.
