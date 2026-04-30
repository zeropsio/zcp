---
id: develop-deploy-modes
priority: 2
phases: [develop-active]
title: "Deploy modes — self-deploy vs cross-deploy"
---

### Two deploy classes

| Class | Trigger | `deployFiles` constraint | Typical use |
|---|---|---|---|
| **Self-deploy** | `sourceService == targetService`, or omitted and inferred to target | MUST be `[.]` or `[./]`; narrower patterns destroy target source | dev/simple mutable workspace |
| **Cross-deploy** | `sourceService != targetService`, or `strategy=git-push` | Cherry-pick build output: `./out`, `./dist`, `./build` | dev→stage promotion; stage runs foreground binaries |

Self-deploy refreshes a **mutable workspace**; cross-deploy produces an
**immutable artifact** from build-container output after `buildCommands`.

### Picking deployFiles

| Setup block purpose | deployFiles | Why |
|---|---|---|
| Self-deploy (dev, simple modes) | `[.]` | Anything narrower destroys target on deploy. |
| Cross-deploy, preserve dir | `[./out]` | Lands at `/var/www/out/...`; use when `start` references that path or artifacts live in subdirs. |
| Cross-deploy, extract contents | `[./out/~]` | Tilde strips `out/`; use when runtime expects assets at `/var/www/`. |

### Why the source tree sometimes doesn't have `./out`

`deployFiles` is evaluated against the **build container filesystem
after `buildCommands`**, NOT the editor tree. `deployFiles: [./out]`
is correct even when `./out` is absent locally; the build creates it.
See guide `deployment-lifecycle`.

ZCP pre-flight does NOT check cross-deploy path existence; Zerops
builder emits `WARN: deployFiles paths not found: ...` in
`DeployResult.BuildLogs` only if the build produces no matches.

### Where pre-flight finds zerops.yaml

Pre-flight searches by **target hostname**, in this order:

1. **Per-target subdirectory** — `<projectRoot>/<targetService>/zerops.yaml` (`/var/www/<targetService>/zerops.yaml` in container env). <!-- axis-n-keep -->
2. **Project root fallback** — `<projectRoot>/zerops.yaml` (`/var/www/zerops.yaml` in container env). <!-- axis-n-keep -->

The **source mount is never searched**. Self-deploy works transparently
because source == target; cross-deploy
(`sourceService != targetService`) does NOT pick up the yaml you
scaffolded under the source mount.

Place a single shared `zerops.yaml` at the project root when you have
standard pairs. Every deploy — self or cross — finds it there without
per-service copies. If you already wrote it under the source mount,
copy (or symlink) it to the project root before the cross-deploy.
