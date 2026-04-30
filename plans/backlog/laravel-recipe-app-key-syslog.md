# Laravel recipes — drop preprocessor APP_KEY + standardize on syslog logging

**Surfaced**: 2026-04-30 — Phase 1.5 / Tier-2 eval signal (B3) plus operator follow-up. Laravel recipes ship two `envVariables:` defaults that bite at runtime:

1. **APP_KEY**: `internal/knowledge/recipes/laravel-{minimal,showcase}.import.yml` set `APP_KEY: <@generateRandomString(<32>)>` at project scope. The preprocessor produces 32 raw ASCII chars; newer Laravel rejects with `RuntimeException: Unsupported cipher or incorrect key length`. Adding a literal `base64:` prefix doesn't help — the suffix has to be valid base64-encoded 32 random bytes (~44 chars), which the current preprocessor doesn't produce.
2. **Logging**: Recipes set `LOG_CHANNEL: stderr` in both `dev` and `prod` setups (and project-level envVariables in some templates). We want `LOG_CHANNEL: syslog` + new `LOG_SYSLOG_FACILITY: local0` so the platform's log viewer captures via syslog facility, preserving level/facility metadata that the platform's log filters key off.

**Why deferred**: B3 round-1 fix (`83a5d820`) was atom/recipe-prose only — covered the APP_KEY workaround in the recipe's `Gotchas`. Codex adversarial review correctly flagged that the shipped `.import.yml` defaults are unchanged, so agents still hit the broken APP_KEY on first import. The clean fix is two recipe edits + a confirmed `php artisan key:generate` workflow; bundling with the syslog change is natural since both touch the same `envVariables:` blocks and ship through one round of `zcp sync push recipes laravel-*`.

**Trigger to promote**: any of —
- Another eval scenario fires `Unsupported cipher or incorrect key length` against the shipped recipe.
- We're cutting a Laravel recipe revision (`zcp sync push recipes laravel-*`) and want both fixes in the same PR.
- Operator wants the syslog-facility logging in place before a demo / live customer deploy.

## Sketch

### APP_KEY drop

Both `.import.yml` files: remove the `project.envVariables.APP_KEY` block entirely. The recipe markdown `Gotchas` already documents the post-deploy recovery (B3 round-1):

```
ssh appdev "cd /var/www && php artisan key:generate --show"
# → outputs: base64:<base64-of-32-random-bytes>

zerops_env action="set" scope="project" key="APP_KEY" value="base64:..."
zerops_manage action="restart" service="appdev"   # PHP-FPM rereads env
zerops_manage action="restart" service="appstage"
```

**Open question**: does zParser expose a preprocessor that emits `base64:<base64-of-32-random-bytes>` directly? Something like `<@generateBase64Key(<32>)>` or a `| base64encode` modifier on `generateRandomString`. If yes, we keep `APP_KEY` in `.import.yml` and the manual step disappears. If no, drop is the shipping default and the manual step stays.

To answer: search zParser source (`github.com/zeropsio/zParser/v2`) or `internal/preprocess/preprocess_test.go` for any base64-aware modifier.

### Syslog logging

Wherever `LOG_CHANNEL: stderr` appears in either Laravel recipe (`zerops.yaml` `prod` setup, `dev` setup, project-level envVariables in `.import.yml`), replace with:

```yaml
envVariables:
  LOG_CHANNEL: syslog
  LOG_SYSLOG_FACILITY: local0
```

Laravel's default `config/logging.php` (>= 9.x) carries the `syslog` channel; `LOG_SYSLOG_FACILITY` lands as the second arg to `openlog()`. `local0` is the canonical user-app facility code on Alpine/Debian.

Recipe markdown `Gotchas` should mention: Zerops log viewer captures via syslog — using stderr loses level/facility metadata that the platform's log filters key off.

### Push pipeline

After both edits land in `internal/knowledge/recipes/laravel-{minimal,showcase}.{md,import.yml}`:

```
zcp sync push recipes laravel-minimal
zcp sync push recipes laravel-showcase
```

Each opens a GitHub PR in `zeropsio/recipes`. Merge, then `zcp sync cache-clear laravel-{minimal,showcase}` + `zcp sync pull recipes` so the embedded store picks up the new content.

## Risks

- Dropping APP_KEY means the **first agent run boots Laravel without a key set**. App may 500 on session/encryption paths until the agent runs `key:generate` and propagates. Acceptable: failure is loud, recovery is one tool call. Shipping a placeholder agent-must-rotate invites "forgot to rotate" leaks.
- Syslog facility `local0` collides if another service in the project also routes its app logs to the same facility. Unlikely in standard Zerops projects but worth one line in Gotchas.
- `LOG_SYSLOG_FACILITY` is a Laravel-specific env var (consumed by `config/logging.php` syslog channel). Other PHP frameworks won't read it — keep guidance in Laravel recipes only.
- The B3 round-1 commit (`83a5d820`) didn't actually ship the Laravel recipe markdown edits because `.md` files are gitignored. Promoting this entry to a real plan-doc means accepting that the *recipe* edit ships through `zcp sync push` (Strapi → GitHub PR), separate from the source-tree commit cycle.

## Refs

- **Supersedes** `plans/backlog/laravel-app-key-base64-prefix.md` (be8b4846, Phase 1 closeout B3 entry). Same APP_KEY observation; this entry replaces it with the resolved fix direction (drop preprocessor APP_KEY + manual `key:generate`) plus the syslog logging follow-up.
- Tier-2 eval triage: `/Users/macbook/Documents/Zerops-MCP-evals/2026-04-30/TIER2-TRIAGE.md` §B3 — sole eval signal grounding the APP_KEY portion. The syslog portion is operator follow-up, NOT eval-grounded; bundling is for shipping convenience (same recipes, same `zcp sync push` round).
- B3 round-1 attempt removed: the previous local commit `83a5d820 fix(recipe-laravel): flag APP_KEY base64 prefix requirement (B3)` was dropped during cleanup because its `.md` recipe edits were gitignored and didn't ship; the test pin without behavior change was misleading. The real fix lives here as a deferred plan.
- Recipe import templates: `internal/knowledge/recipes/laravel-minimal.import.yml:14`, `internal/knowledge/recipes/laravel-showcase.import.yml:16`.
- Recipe knowledge (gitignored): `internal/knowledge/recipes/laravel-minimal.md`, `internal/knowledge/recipes/laravel-showcase.md`.
- Sync workflow: `zcp sync push recipes <slug>`, `zcp sync cache-clear`, `zcp sync pull recipes`.
- zParser preprocessor entry-point (for the modifier-search question): `internal/preprocess/preprocess.go`, `internal/preprocess/preprocess_test.go`.
