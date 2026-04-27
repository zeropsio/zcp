---
id: develop-deploy-modes
priority: 2
phases: [develop-active]
title: "Deploy modes — self-deploy vs cross-deploy"
---

### Two deploy classes, one tool

`zerops_deploy` has two classes determined by source vs target:

| Class | Trigger | `deployFiles` constraint | Typical use |
|---|---|---|---|
| **Self-deploy** | `sourceService == targetService` (or `sourceService` omitted, auto-inferred to target) | MUST be `[.]` or `[./]` — narrower patterns destroy the target's source | dev services running `start: zsc noop --silent`; agent SSHes in and iterates on the code |
| **Cross-deploy** | `sourceService != targetService`, or `strategy=git-push` | Cherry-picked from build output: `./out`, `./dist`, `./build` | dev→stage promotion; stage runs foreground binaries (`start: dotnet App.dll`, `start: node dist/server.js`) |

Self-deploy refreshes a **mutable workspace**; cross-deploy produces an
**immutable artifact** from the build container's post-`buildCommands`
output.

### Picking deployFiles

| Setup block purpose | deployFiles | Why |
|---|---|---|
| Self-deploy (dev, simple modes) | `[.]` | Anything narrower destroys target on deploy. |
| Cross-deploy, preserve dir | `[./out]` | Artifact lands at `/var/www/out/...`. Pick when `start` references an explicit path (e.g. `./out/app/App.dll`) or multiple artifacts live in subdirs. |
| Cross-deploy, extract contents | `[./out/~]` | Tilde strips the `out/` prefix; artifact lands at `/var/www/...`. Pick when the runtime expects assets at root (ASP.NET's `wwwroot/` at ContentRootPath = `/var/www/`). |

### Why the source tree sometimes doesn't have `./out`

`deployFiles` is evaluated against the **build container's filesystem
after `buildCommands` runs** — NOT your editor's working tree. So
`deployFiles: [./out]` is correct even when `./out` doesn't exist
locally; the build creates it. See guide `deployment-lifecycle` for
the full pipeline.

ZCP pre-flight does NOT check path existence for cross-deploy; the
Zerops builder emits `WARN: deployFiles paths not found: ...` in
`DeployResult.BuildLogs` only when the build produces no matching files.
