# zerops-yaml substep — write the complete zerops.yaml per codebase

This substep ends when every codebase has a complete `zerops.yaml` on its mount — all setups present in one file, a comment ratio at or above 30 percent (target 35 percent), no env-var self-shadows, ASCII `#` comments only. `zerops_workflow action=status` shows completion state.

## Schema reference

The injected chain recipe's zerops.yaml template is the primary shape source — it is written in the same framework family as the recipe in hand. For hello-world tiers with no chain predecessor, or for exotic fields not in the template (`buildFromGit`, cache layers, per-environment overrides), fetch the live schema on demand:

```
zerops_knowledge scope="theme" query="zerops.yaml Schema"
```

## One file, all setups

Write the complete `zerops.yaml` with every setup entry in a single file. Setup names are generic across recipes: `setup: dev` and `setup: prod`. Showcase recipes with a shared-codebase worker (a target whose `sharesCodebaseWith` is set to another target's hostname) also carry `setup: worker` in the host target's file. Writing one file is the source of truth for both the deploy step and the integration-guide fragment in the README later — it eliminates drift between what deploys and what the README documents.

## Per-codebase file count and setup shape

The file count and per-file setup count are driven by `sharesCodebaseWith` in `plan.Research.Targets`:

- **Dual-runtime + shared worker** (`worker.sharesCodebaseWith == "api"`):
  - `/var/www/apidev/zerops.yaml` — three setups (`dev`, `prod`, `worker`)
  - `/var/www/appdev/zerops.yaml` — two setups (`dev`, `prod`)
- **Dual-runtime + separate worker** (default; three repos):
  - `/var/www/apidev/zerops.yaml` — two setups (`dev`, `prod`)
  - `/var/www/appdev/zerops.yaml` — two setups (`dev`, `prod`)
  - `/var/www/workerdev/zerops.yaml` — two setups (`dev`, `prod`)
- **Single-app + shared worker** (Laravel, Rails, Django idiom):
  - `/var/www/appdev/zerops.yaml` — three setups (`dev`, `prod`, `worker`)
- **Single-app + separate worker**:
  - `/var/www/appdev/zerops.yaml` — two setups (`dev`, `prod`)
  - `/var/www/workerdev/zerops.yaml` — two setups (`dev`, `prod`)

## What to read next

Before writing, read `env-var-model.md` and — for dual-runtime recipes — `dual-runtime-consumption.md`. Then apply the setup-specific atoms for the relevant setups: `setup-rules-dev.md`, `setup-rules-prod.md`, `setup-rules-worker.md` (when a worker setup applies), `setup-rules-static-frontend.md` (when the prod runtime is serve-only). `seed-execonce-keys.md` covers `initCommands` key shapes; `comment-style-positive.md` covers comment form.
