# Laravel APP_KEY needs `base64:` prefix; preprocessor produces raw bytes

**Surfaced**: 2026-04-30 — Tier-2 eval `bootstrap-user-forces-classic` (Laravel + Postgres). Agent referenced `${APP_KEY}` from project env (set by `<@generateRandomString(<32>)>` preprocessor pattern shown in php-hello-world / Laravel knowledge). Result: Laravel boot crashed with `RuntimeException: Unsupported cipher or incorrect key length`. Recovered by running `php artisan key:generate --show` on the container and hardcoding the resulting `base64:XXXX` value into `zerops.yaml`.

**Why deferred**: The fix touches preprocessor semantics (Zerops-side?) AND/OR Laravel-recipe-specific knowledge — different scope from the immediate Tier-2 cleanup. Need to confirm whether a `<@generateBase64Key>`-style preprocessor exists in Zerops (or could be added), or whether the right answer is per-recipe documentation. Either way the wrong outcome is silent: agent ships a broken APP_KEY, app crashes only at runtime, agent has to debug from runtime logs. Worth a focused look once we understand which side owns the contract.

**Trigger to promote**:
- Repeats in another Tier-2/Tier-3 / Phase 1.5 scenario (any Laravel scenario will hit this)
- A user reports the same friction outside an eval
- We need a Laravel-specific recipe with realistic `APP_KEY` wiring

## Sketch

Three possible fixes, ordered by structural goodness:

1. **Zerops preprocessor adds `<@generateBase64Key()>`** (or extends `<@generateRandomString(N, true, base64=true)>`) — emits `base64:` + base64-encoded random bytes. Recipe templates use it for Laravel keys + similar (Symfony `APP_SECRET` accepts raw, Rails `secret_key_base` accepts raw — Laravel is the odd one out). Single source of truth.
2. **Laravel-specific recipe documents the workaround** in `internal/knowledge/recipes/laravel-minimal.md` + `laravel-showcase.md`: include a one-shot `php artisan key:generate --show` step before first deploy, set the result via `zerops_env action="set" scope="project" key="APP_KEY"`. Atoms / generic php-hello-world stay as-is.
3. **Recipe import YAML for Laravel hardcodes** a `base64:`-prefixed placeholder (`APP_KEY: "base64:REPLACE_ME_WITH_REAL_32_BYTE_KEY"`) — agent rotates after first deploy. Worst — silent placeholder rotation rituals invite "forgot to rotate" production leaks.

Recommend option 1 if Zerops preprocessor extension is feasible (it's a tiny additional preprocessor token); fall back to option 2 if the platform team doesn't want to touch the preprocessor.

## Risks

- Option 1 = cross-team coordination with Zerops platform — non-trivial timeline.
- Option 2 = recipe-specific; doesn't help users who write classic-route Laravel from scratch (they'd need the same workaround).
- All options leave the silent-runtime-crash failure mode in place until the fix lands. Worth a defensive note in `php-hello-world` / Laravel knowledge meanwhile: "If you're using Laravel and APP_KEY ends up `${APP_KEY}` from a `<@generateRandomString>` preprocessor, the runtime will crash. Set it via `key:generate` instead."

## Refs

- Tier-2 triage: `/Users/macbook/Documents/Zerops-MCP-evals/2026-04-30/TIER2-TRIAGE.md` §B3
- Per-scenario report: `tier2/bootstrap-user-forces-classic/result.json`
- Atom guidance currently demonstrating the (wrong-for-Laravel) pattern: `internal/content/atoms/bootstrap-recipe-import.md` step 1
