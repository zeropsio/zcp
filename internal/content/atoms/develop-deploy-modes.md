---
id: develop-deploy-modes
priority: 2
phases: [develop-active]
title: "Deploy modes — self-deploy vs cross-deploy"
---

### Two deploy classes, one tool

`zerops_deploy` has two classes determined by source vs target:

- **Self-deploy** — `sourceService == targetService` (or `sourceService`
  omitted, which auto-infers to target). Refreshes a **mutable
  workspace**. Runtime receives the working tree as-is; the setup
  block's `deployFiles` MUST be `[.]` or `[./]` — narrower patterns
  destroy the target's source (DM-2). Typical for dev services running
  `start: zsc noop --silent` where the agent SSHes in and iterates on
  the code manually.
- **Cross-deploy** — `sourceService != targetService`, or
  `strategy=git-push`. Produces an **immutable artifact**. Runtime
  receives the build container's post-`buildCommands` output selected
  by `deployFiles` (typically cherry-picked: `./out`, `./dist`,
  `./build`). Typical for dev→stage promotion; stage services run
  foreground binaries (`start: dotnet App.dll`, `start: node dist/server.js`).

### Picking deployFiles

| Setup block purpose | deployFiles | Why |
|---|---|---|
| Self-deploy (dev, simple modes) | `[.]` | DM-2; anything narrower destroys target on deploy. |
| Cross-deploy, preserve dir | `[./out]` | Artifact lands at `/var/www/out/...`. Pick when `start` references an explicit path (e.g. `./out/app/App.dll`) or multiple artifacts live in subdirs. |
| Cross-deploy, extract contents | `[./out/~]` | Tilde strips the `out/` prefix; artifact lands at `/var/www/...`. Pick when the runtime expects assets at root (ASP.NET's `wwwroot/` at ContentRootPath = `/var/www/`). |

### Why the source tree sometimes doesn't have `./out`

`deployFiles` is defined over the **build container's filesystem after
`buildCommands` runs**. A cross-deploy `deployFiles: [./out]` is
correct even when `./out` doesn't exist in your editor — the build
container creates it from `dotnet publish -o out`, `vite build`,
`go build -o out/server`, etc.

ZCP client-side pre-flight does NOT check path existence for
cross-deploy (DM-3 / DM-4). The Zerops builder validates existence
at build time and emits `WARN: deployFiles paths not found: ...` in
the build log (surfaced via `DeployResult.BuildLogs`) only when the
build genuinely produced no matching files.

**Reference**: `docs/spec-workflows.md` §8 Deploy Modes (DM-1…DM-5).
