# Laravel recipes — verify APP_KEY failure source + standardize syslog logging

**Surfaced**: 2026-04-30 — Phase 1.5 / Tier-2 eval signal (B3) plus operator follow-up.

**2026-05-01 rescan**: the original APP_KEY hypothesis was too broad. ZCP and `../recipe-repos` do **not** currently contain `APP_KEY: base64:<@...>` in Laravel import files. The Laravel recipe repos (`recipe-laravel-jetstream`, `recipe-laravel-minimal`, `recipe-filament`, `recipe-twill`) all use `APP_KEY: <@generateRandomString(<32>)>`. Laravel 11-13 accepts a raw 32-byte key when the value has no `base64:` prefix; the failing shape is `base64:<@generateRandomString(<32>)>` because Laravel decodes the 32 generated chars to about 24 bytes. zParser v2.1.2 has no base64 encode modifier, so the preprocessor still cannot emit the exact `php artisan key:generate` shape.

Two separate follow-ups remain:

1. **APP_KEY failure attribution**: if `Unsupported cipher or incorrect key length` recurs, capture the exact runtime value source before changing recipe imports. Likely candidates are a missing key, a deployed `.env` shadowing platform envs, or a `zerops.yaml` line like `APP_KEY: ${APP_KEY}` leaving a literal unresolved reference.
2. **Logging**: recipes should use `LOG_CHANNEL: syslog` + `LOG_SYSLOG_FACILITY: local0` so the platform log viewer captures Laravel logs via syslog facility metadata instead of plain stderr.

**Why deferred**: APP_KEY no longer has a known recipe-file fix from this scan. Dropping APP_KEY from imports would make first boot fail loudly and is not justified without a fresh reproducible failure. Syslog is still recipe content work and should ship through the recipe sync flow.

**Trigger to promote**: any of —
- Another eval scenario fires `Unsupported cipher or incorrect key length` against a shipped Laravel recipe.
- We are cutting a Laravel recipe revision (`zcp sync push recipes laravel-*`) and want the syslog change in the same PR.
- Operator wants syslog-facility logging in place before a demo / live customer deploy.

## Sketch

### APP_KEY verification

On the next failure, record:

```
zerops_env action="get" scope="project" key="APP_KEY"
zerops_discover hostname="app..." includeEnvs=true includeEnvValues=true
ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null app... "cd /var/www && php -r 'var_dump(getenv(\"APP_KEY\"), strlen((string) getenv(\"APP_KEY\")));'"
ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null app... "cd /var/www && test -f .env && sed -n '1,20p' .env || true"
```

If the value is raw 32 ASCII chars and Laravel still rejects it, capture the exact Laravel version and stack trace before changing recipes. If the value is `base64:<@...>`, `${APP_KEY}`, empty, or shadowed by `.env`, fix that source.

Do **not** add a literal `base64:` prefix to preprocessor output. Valid options are either raw `<@generateRandomString(<32>)>` or a pre-encoded literal `base64:<44-char-artisan-key>`.

### Syslog logging

Wherever Laravel recipes still use stderr logging, replace with:

```yaml
LOG_CHANNEL: syslog
LOG_SYSLOG_FACILITY: local0
```

Laravel's default `config/logging.php` (>= 9.x) carries the `syslog` channel; `LOG_SYSLOG_FACILITY` lands as the second arg to `openlog()`. `local0` is the canonical user-app facility code on Alpine/Debian.

Recipe markdown `Gotchas` should mention: Zerops log viewer captures via syslog; stderr loses facility metadata that platform log filters can key off.

### Push pipeline

After recipe edits land in `internal/knowledge/recipes/laravel-{minimal,showcase}.{md,import.yml}`:

```
zcp sync push recipes laravel-minimal
zcp sync push recipes laravel-showcase
```

Each opens a GitHub PR in `zeropsio/recipes`. Merge, then `zcp sync cache-clear laravel-{minimal,showcase}` + `zcp sync pull recipes` so the embedded store picks up the new content.

## Risks

- Leaving APP_KEY as raw preprocessor output is correct for Laravel runtime key-length validation, but it is not the exact `key:generate` display shape. Tooling that assumes `base64:` form may still warn; distinguish tooling preference from runtime failure.
- Syslog facility `local0` collides if another service in the project also routes its app logs to the same facility. Unlikely in standard Zerops projects but worth one line in Gotchas.
- `LOG_SYSLOG_FACILITY` is Laravel-specific (consumed by `config/logging.php` syslog channel). Other PHP frameworks will not read it.

## Refs

- Tier-2 eval triage: `/Users/macbook/Documents/Zerops-MCP-evals/2026-04-30/TIER2-TRIAGE.md` §B3.
- Recipe import templates: `internal/knowledge/recipes/laravel-minimal.import.yml:14`, `internal/knowledge/recipes/laravel-showcase.import.yml:16`.
- Current external recipe repos: `../recipe-repos/recipe-laravel-jetstream`, `../recipe-repos/recipe-laravel-minimal`, `../recipe-repos/recipe-filament`, `../recipe-repos/recipe-twill`.
- zParser preprocessor entry point: `internal/preprocess/preprocess.go`, `internal/preprocess/preprocess_test.go`.
