# Recipe Validation Report — Agent 2

Recipes validated: symfony, php-variants, django, rails, phoenix

---

# Validation: symfony

## Repo: recipe-symfony

## Findings

### F1: Preprocessor directive mismatch
- Severity: P0
- Category: yaml-mismatch
- Recipe says: `#yamlPreprocessor=on` in import.yml
- Repo says: `#zeropsPreprocessor=on` in zerops-project-import.yml
- Recommendation: Recipe is CORRECT per MEMORY.md verified rules. The repo uses the old/wrong preprocessor directive. No recipe change needed, but the repo itself is outdated.

### F2: Redis service type mismatch
- Severity: P1
- Category: yaml-mismatch
- Recipe says: `type: valkey@7.2` for hostname `redis`
- Repo says: `type: keydb@6` for hostname `redis`
- Recommendation: Recipe uses the modern Zerops stack name (valkey). The repo uses deprecated keydb@6. Recipe is CORRECT for new projects. Note the version/type drift for awareness.

### F3: Repo has extra services not in recipe
- Severity: P2
- Category: knowledge-gap
- Recipe says: 3 services (app + db + redis)
- Repo says: 5 services (app + db + redis + adminer + mailpit)
- Recommendation: This is expected -- recipe strips dev-only services. No change needed, but recipe could mention that adminer/mailpit are available in the full repo.

### F4: Migration --no-interaction flag missing in repo
- Severity: P1
- Category: yaml-mismatch
- Recipe says: `php bin/console doctrine:migrations:migrate --no-interaction`
- Repo says: `php bin/console doctrine:migrations:migrate` (no --no-interaction flag)
- Recommendation: The recipe is CORRECT. The `--no-interaction` flag is critical for automated deploys to prevent interactive prompts blocking migrations. The repo is missing this flag.

### F5: Repo has buildFromGit and project/tags fields
- Severity: P2
- Category: knowledge-gap
- Recipe says: no `buildFromGit`, no `project:` block
- Repo says: includes `buildFromGit: https://github.com/zeropsio/recipe-symfony@main` and `project: name: recipe-symfony`
- Recommendation: Expected. Recipe strips repo-specific deployment fields for reusability. No change needed.

### F6: Session handler_id uses /0 DB index suffix
- Severity: P2
- Category: knowledge-gap
- Recipe says: `handler_id: '%env(REDIS_URL)%'` (Configuration section)
- Repo says: `handler_id: '%env(REDIS_URL)%/0'` (config/packages/framework.yaml line 16)
- Recommendation: Update recipe Configuration section to include the `/0` DB index suffix to match the actual repo config. Recipe Gotchas says "Sessions use Redis DB index 0 via SESSION_HANDLER config" but the Configuration code example omits `/0`.

### F7: Monolog config in repo differs from recipe suggestion
- Severity: P2
- Category: knowledge-gap
- Recipe says: simple syslog handler at debug level (Configuration section)
- Repo says: production uses fingers_crossed handler + nested stderr + json formatter + syslog_handler (monolog.yaml when@prod)
- Recommendation: The recipe shows a simplified syslog config. The actual repo config is more sophisticated with fingers_crossed error buffering. Recipe could note this is a minimal example.

### F8: Repo import has priority: 10 on db and redis, recipe matches
- Severity: PASS
- Category: yaml-mismatch
- Verified: Both recipe and repo have `priority: 10` on db and redis services.

### F9: twbs/bootstrap is in `require` (not require-dev), recipe matches
- Severity: PASS
- Category: yaml-mismatch
- Verified: composer.json has `"twbs/bootstrap": "^5"` in `require`. Recipe correctly documents this.

### F10: marein/symfony-lock-doctrine-migrations-bundle present
- Severity: PASS
- Category: yaml-mismatch
- Verified: composer.json has `"marein/symfony-lock-doctrine-migrations-bundle": "^1.0"` in `require`. Recipe correctly documents this.

### F11: symfonycasts/sass-bundle is v0.7 in repo
- Severity: PASS
- Category: version-drift
- Verified: composer.json has `"symfonycasts/sass-bundle": "^0.7"` which is >= 0.5. Recipe correctly states "must be v0.5+".

## Verdict: NEEDS_FIX (3 issues: F1-P0 preprocessor repo-side, F2-P1 redis type drift, F4-P1 --no-interaction flag, F6-P2 session handler)

---

# Validation: php-variants

## Repo: recipe-php

## Findings

### F1: Build base syntax differs (array vs scalar)
- Severity: P1
- Category: yaml-mismatch
- Recipe says: `base: php@8.3` (scalar string)
- Repo says: `base:\n  - php@8.3` (array syntax)
- Recommendation: Both are valid Zerops YAML but the recipe should match the repo pattern. The scalar form is the more common convention in Zerops docs. Low priority since both work.

### F2: Repo import has priority: 1 on db, recipe has no priority
- Severity: P2
- Category: yaml-mismatch
- Recipe says: no `priority:` on db service in import.yml
- Repo says: `priority: 1` on db service
- Recommendation: Add `priority: 10` (not 1) to recipe import.yml db service. Using `priority: 10` ensures db is created before runtime services. The repo uses `priority: 1` which is unusual (10 is the conventional value).

### F3: Repo import has buildFromGit and project/tags fields
- Severity: P2
- Category: knowledge-gap
- Recipe says: no `buildFromGit`, no `project:` block
- Repo says: includes `buildFromGit` URLs and project block
- Recommendation: Expected. Recipe strips repo-specific deployment fields.

### F4: DB_NAME hardcoded to "db" matches import hostname
- Severity: PASS
- Category: env-var-pattern
- Verified: `DB_NAME: db` and `DB_HOST: db` match the import hostname `db`. Hardcoded hostname is CORRECT per decision rules.

### F5: DB_PORT hardcoded to 5432
- Severity: PASS
- Category: env-var-pattern
- Verified: `DB_PORT: 5432` is the well-known PostgreSQL port. CORRECT per decision rules.

### F6: Health check /status endpoint implemented in source
- Severity: PASS
- Category: knowledge-gap
- Verified: `index.php` line 12 implements `/status` returning `{"status":"UP"}`. Recipe correctly documents this requirement.

### F7: SQL injection in repo source code
- Severity: P2
- Category: knowledge-gap
- Recipe says: nothing about SQL injection
- Repo says: `pg_query($dbconn, "INSERT INTO entries (data) VALUES ('$data');");` (line 39 of index.php)
- Recommendation: This is a known pattern in the demo recipe (UUID data only, no user input). Not a recipe issue, but worth noting.

## Verdict: NEEDS_FIX (2 issues: F1-P1 base syntax minor, F2-P2 priority missing)

---

# Validation: django

## Repo: recipe-django

## Findings

### F1: Env var reference syntax differs ($var vs ${var})
- Severity: P2
- Category: yaml-mismatch
- Recipe says: `${db_hostname}`, `${db_port}`, `${storage_accessKeyId}`, etc. (curly-brace syntax)
- Repo says: `$db_hostname`, `$db_port`, `$storage_accessKeyId`, etc. (bare-dollar syntax)
- Recommendation: Both `$var` and `${var}` are valid Zerops env var reference syntax. Recipe uses the more explicit/recommended form. No fix required, but note the difference.

### F2: deployFiles differs significantly
- Severity: P1
- Category: yaml-mismatch
- Recipe says: `deployFiles: ./` (entire directory)
- Repo says: `deployFiles:\n  - files/\n  - recipe/\n  - manage.py\n  - gunicorn.conf.py` (selective files)
- Recommendation: Update recipe to match repo's selective deployFiles pattern. Deploying `./` includes unnecessary files (requirements.txt, .git, etc.) and increases deploy size. The repo correctly deploys only what's needed.

### F3: Missing buildCommands in repo
- Severity: P1
- Category: yaml-mismatch
- Recipe says: `buildCommands:\n  - pip install --no-cache-dir -r requirements.txt`
- Repo says: no `buildCommands:` section at all
- Recommendation: The repo relies on `prepareCommands` at runtime to install dependencies. The recipe adds a build-phase pip install which is wasteful since the build output isn't used (Python has no compilation step for deps). However, the recipe pattern is still valid -- it ensures deps are checked at build time. Consider aligning with repo approach.

### F4: Missing os: alpine in recipe
- Severity: P1
- Category: yaml-mismatch
- Recipe says: no `os:` field in build or run sections
- Repo says: `os: alpine` in both build and run sections
- Recommendation: Add `os: alpine` to recipe zerops.yml. The `os` field specifies the container OS. Without it, the default (ubuntu) is used, which may differ from what the repo actually runs.

### F5: Start command WSGI module name
- Severity: P1
- Category: yaml-mismatch
- Recipe says: `start: gunicorn myproject.wsgi` (placeholder)
- Repo says: `start: gunicorn recipe.wsgi` (actual module)
- Recommendation: Recipe correctly uses `myproject.wsgi` as a placeholder and documents "Replace the myproject.wsgi start command..." in Gotchas. This is acceptable for a template recipe.

### F6: objectStorageSize differs
- Severity: P2
- Category: yaml-mismatch
- Recipe says: `objectStorageSize: 2`
- Repo says: `objectStorageSize: 5`
- Recommendation: Minor difference. Both are valid. Recipe uses a smaller default.

### F7: Missing MAIL_HOST and MAIL_PORT env vars in recipe
- Severity: P2
- Category: knowledge-gap
- Recipe says: no mail env vars in zerops.yml
- Repo says: `MAIL_HOST: mailpit` and `MAIL_PORT: "1025"` in zerops.yml envVariables
- Recommendation: Add MAIL_HOST and MAIL_PORT to recipe if mailpit is documented. Currently recipe strips mailpit service but the env vars referencing it are also stripped -- this is consistent.

### F8: Missing prepareCommands sudo apk add tzdata
- Severity: P2
- Category: knowledge-gap
- Recipe says: `prepareCommands:\n  - pip install --no-cache-dir -r requirements.txt`
- Repo says: `prepareCommands:\n  - sudo apk add tzdata\n  - pip install --no-cache-dir -r requirements.txt`
- Recommendation: The tzdata package is needed for timezone support on Alpine. If recipe adds `os: alpine`, this prepareCommand becomes important.

### F9: Repo has mailpit and adminer services
- Severity: P2
- Category: knowledge-gap
- Recipe says: 3 services (app + db + storage)
- Repo says: 5 services (app + db + storage + mailpit + adminer)
- Recommendation: Expected. Recipe strips dev-only services.

### F10: DB engine differs
- Severity: P2
- Category: knowledge-gap
- Recipe says (Configuration section): `"ENGINE": "django.db.backends.postgresql"`
- Repo says: `"ENGINE": "django.db.backends.postgresql_psycopg2"` (settings.py line 99)
- Recommendation: Update recipe Configuration section to use `postgresql_psycopg2` to match repo. Both work but `postgresql_psycopg2` is explicit about the driver used (psycopg2-binary in requirements.txt).

### F11: Recipe Configuration shows DB_NAME env var but zerops.yml does not set it
- Severity: P2
- Category: env-var-pattern
- Recipe says: Configuration section shows `"NAME": os.environ.get("DB_NAME", "db")`
- Recipe zerops.yml says: no DB_NAME in envVariables
- Repo says: hardcodes `"NAME": "db"` in settings.py (no DB_NAME env var)
- Recommendation: The recipe Configuration section suggests a DB_NAME env var that is never set. Either add `DB_NAME: db` to zerops.yml envVariables or update the Configuration example to hardcode `"db"` like the repo does.

### F12: S3 storage backend path differs
- Severity: P2
- Category: knowledge-gap
- Recipe says: `DEFAULT_FILE_STORAGE = "storages.backends.s3boto3.S3Boto3Storage"`
- Repo says: `DEFAULT_FILE_STORAGE = "storages.backends.s3.S3Storage"` and `STATICFILES_STORAGE = "storages.backends.s3.S3Storage"`
- Recommendation: The repo uses the newer `storages.backends.s3.S3Storage` path from django-storages >= 1.14. Recipe uses the older `s3boto3.S3Boto3Storage` path. Update recipe to match repo.

### F13: Repo also configures STATICFILES_STORAGE for S3, recipe does not
- Severity: P2
- Category: knowledge-gap
- Recipe says: only configures `DEFAULT_FILE_STORAGE` for S3
- Repo says: configures both `STATICFILES_STORAGE` and `DEFAULT_FILE_STORAGE` with `STATIC_URL` pointing to S3
- Recommendation: Add STATICFILES_STORAGE S3 configuration to recipe. This is important for serving static files from S3 in production.

### F14: AWS_S3_FILE_OVERWRITE differs
- Severity: P2
- Category: knowledge-gap
- Recipe says: `AWS_S3_FILE_OVERWRITE = False`
- Repo says: `AWS_S3_FILE_OVERWRITE = True`
- Recommendation: Minor. Different design choices. Recipe prevents overwrites by default.

### F15: Gunicorn bind address correctly configured
- Severity: PASS
- Category: knowledge-gap
- Verified: `gunicorn.conf.py` in repo has `bind = "0.0.0.0:8000"`. Recipe correctly documents the requirement for 0.0.0.0 binding.

## Verdict: NEEDS_FIX (10 issues: F2-P1, F3-P1, F4-P1 os:alpine, F5-P1 placeholder OK, F6-P2, F8-P2, F10-P2, F11-P2, F12-P2, F13-P2)

---

# Validation: rails

## Repo: (no repo -- skipping source cross-check)

## Findings

Internal consistency validation only.

### F1: run.base missing in zerops.yml
- Severity: P1
- Category: yaml-mismatch
- Recipe says: no `base:` under `run:` section
- Expected: `base: ruby@3.4` or similar in run section
- Recommendation: Add `base: ruby@3.4` to the run section. Without it, the runtime base is undefined. The build section has `base: ruby@3.4` but the run section is missing it entirely.

### F2: DATABASE_URL uses cross-service refs correctly
- Severity: PASS
- Category: env-var-pattern
- Verified: `DATABASE_URL: postgresql://${db_user}:${db_password}@${db_hostname}:${db_port}/${db_dbName}` correctly uses cross-service references.

### F3: SECRET_KEY_BASE properly in envSecrets
- Severity: PASS
- Category: env-var-pattern
- Verified: `SECRET_KEY_BASE: <@generateRandomString(<64>)>` is correctly placed in envSecrets block.

### F4: Puma binding includes 0.0.0.0
- Severity: PASS
- Category: yaml-mismatch
- Verified: `start: bundle exec puma -b tcp://0.0.0.0:3000` correctly binds to all interfaces.

### F5: bundle install --deployment flag documented
- Severity: PASS
- Category: knowledge-gap
- Verified: Both zerops.yml and Gotchas document `--deployment` flag.

### F6: zsc execOnce migration pattern correct
- Severity: PASS
- Category: yaml-mismatch
- Verified: `zsc execOnce migrate-${ZEROPS_appVersionId} -- bin/rails db:migrate` uses correct per-version guard.

### F7: Internal consistency of Configuration section
- Severity: PASS
- Category: missing-section
- Verified: Configuration section documents `config.hosts.clear`, `public_file_server.enabled`, logger, and trusted_proxies. All match the env vars in zerops.yml.

## Verdict: NEEDS_FIX (1 issue: F1-P1 missing run.base)

---

# Validation: phoenix

## Repo: recipe-phoenix

## Findings

### F1: Elixir version mismatch
- Severity: P1
- Category: version-drift
- Recipe says: `base: elixir@1.17` (build), `type: elixir@1.17` (import)
- Repo says: `base: elixir@1.16` (build), `type: elixir@1.16` (import)
- Recommendation: Align recipe and repo versions. Recipe upgraded to 1.17 but repo is on 1.16. Either is valid -- decide which is canonical. For new projects, 1.17 is appropriate.

### F2: Build commands differ -- repo includes ecto.create and ecto.migrate
- Severity: P1
- Category: yaml-mismatch
- Recipe says: buildCommands includes `mix local.hex --force`, `mix local.rebar --force`, `mix deps.get --only prod`, `mix compile`, `mix assets.deploy`, `mix phx.digest`, `mix release --overwrite`
- Repo says: buildCommands includes `mix deps.get --only prod`, `mix ecto.create`, `mix ecto.migrate`, `mix compile`, `mix assets.deploy`, `mix phx.digest`, `mix release --overwrite`
- Recommendation: Recipe ADDS `mix local.hex --force` and `mix local.rebar --force` (correct for clean builds). Recipe REMOVES `mix ecto.create` and `mix ecto.migrate` from build phase and moves to initCommands (correct for production -- db operations should happen at runtime, not build time). Recipe pattern is BETTER than repo. However, recipe should note this design difference.

### F3: Recipe has initCommands, repo does not
- Severity: P1
- Category: yaml-mismatch
- Recipe says: `initCommands:\n  - zsc execOnce migrate-${ZEROPS_appVersionId} -- _build/prod/rel/myapp/bin/myapp eval "MyApp.Release.migrate()"`
- Repo says: no initCommands section (migrations run during build via mix ecto.migrate)
- Recommendation: Recipe pattern is BETTER. Running migrations at runtime with `zsc execOnce` is the correct multi-container pattern. But this requires a Release.migrate() module that doesn't exist in the repo (see F4).

### F4: Release.migrate() module does not exist in repo
- Severity: P1
- Category: knowledge-gap
- Recipe says: references `MyApp.Release.migrate()` in initCommands and provides a sample `lib/myapp/release.ex`
- Repo says: no `release.ex` file exists anywhere in the repo
- Recommendation: The recipe documents a pattern that the repo doesn't implement. This is by design -- the recipe is a guide for building new projects. But it means the repo itself cannot be deployed using the recipe's zerops.yml without adding the release module.

### F5: Deploy files path differs
- Severity: P1
- Category: yaml-mismatch
- Recipe says: `deployFiles: ./`
- Repo says: `deployFiles: /`
- Recommendation: `./` (relative current dir) vs `/` (root) -- this is a significant difference. The repo uses `/` which deploys the entire filesystem root (likely wrong or platform-interpreted). Recipe uses `./` which is the standard convention. Recipe is CORRECT.

### F6: Start command and release name differs
- Severity: P2
- Category: yaml-mismatch
- Recipe says: `start: _build/prod/rel/myapp/bin/myapp start` (placeholder)
- Repo says: `start: _build/prod/rel/recipe_phoenix/bin/recipe_phoenix start`
- Recommendation: Recipe correctly uses `myapp` as a placeholder and documents "Replace myapp with the actual application name." This is acceptable.

### F7: PORT and POOL_SIZE type difference
- Severity: P2
- Category: yaml-mismatch
- Recipe says: `PORT: "4000"` and `POOL_SIZE: "10"` (quoted strings)
- Repo says: `PORT: 4000` and `POOL_SIZE: 10` (unquoted, parsed as integers by YAML)
- Recommendation: Recipe is MORE CORRECT. Zerops env vars are always strings internally. Quoting ensures correct type. Minor cosmetic difference.

### F8: Missing priority on db service in repo import
- Severity: P2
- Category: yaml-mismatch
- Recipe says: `priority: 10` on db service
- Repo says: no `priority:` on db service
- Recommendation: Recipe is CORRECT. Without priority, the database might not be ready when the app build tries to connect. Recipe correctly adds priority.

### F9: Repo has verticalAutoscaling in import, recipe does not
- Severity: P2
- Category: knowledge-gap
- Recipe says: no verticalAutoscaling configuration
- Repo says: `verticalAutoscaling:\n  minRam: 0.25\n  minFreeRamGB: 0.125`
- Recommendation: Consider adding verticalAutoscaling to recipe import.yml for BEAM applications which have specific memory characteristics.

### F10: Runtime IP binding differs
- Severity: P2
- Category: knowledge-gap
- Recipe says (Configuration section): `http: [ip: {0, 0, 0, 0}, port: port]` (IPv4 only)
- Repo says: `ip: {0, 0, 0, 0, 0, 0, 0, 0}` (IPv6 dual-stack, binds all interfaces including IPv4)
- Recommendation: Minor difference. Both work on Zerops. Repo uses IPv6 dual-stack which is the Phoenix 1.7+ default. Recipe could match.

### F11: Preprocessor directive correct
- Severity: PASS
- Category: yaml-mismatch
- Verified: Both recipe and repo use `#yamlPreprocessor=on`. Correct.

### F12: SECRET_KEY_BASE envSecret generation matches
- Severity: PASS
- Category: env-var-pattern
- Verified: Both use `<@generateRandomString(<64>)>`.

### F13: PHX_SERVER=true correctly set
- Severity: PASS
- Category: env-var-pattern
- Verified: Both recipe and repo set `PHX_SERVER: true`. Recipe correctly documents this as mandatory.

## Verdict: NEEDS_FIX (5 issues: F1-P1 version, F2-P1 build commands, F3-P1 initCommands, F4-P1 release module, F5-P1 deployFiles)

---

# Summary

| Recipe | Verdict | P0 | P1 | P2 |
|--------|---------|----|----|-----|
| symfony | NEEDS_FIX | 1 (repo-side) | 2 | 3 |
| php-variants | NEEDS_FIX | 0 | 1 | 2 |
| django | NEEDS_FIX | 0 | 3 | 7 |
| rails | NEEDS_FIX | 0 | 1 | 0 |
| phoenix | NEEDS_FIX | 0 | 5 | 5 |

## Key Themes

1. **Preprocessor directive**: symfony repo uses `#zeropsPreprocessor=on` (wrong), recipes correctly use `#yamlPreprocessor=on`
2. **Version drift**: phoenix recipe upgraded to 1.17 but repo is 1.16; symfony repo uses keydb@6 but recipe uses valkey@7.2
3. **Deploy pattern improvements**: recipes generally have BETTER patterns than repos (initCommands migrations, priority on db services, quoted env var values)
4. **Django alignment**: recipe has several differences from repo (deployFiles, os:alpine, S3 backend paths) that should be aligned
5. **Rails missing run.base**: the only purely internal consistency issue found
