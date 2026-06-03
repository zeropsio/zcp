---
id: develop-deploy-modes
priority: 2
phases: [develop-active]
envelopeDeployStates: [never-deployed]
title: "Deploy modes — self-deploy vs cross-deploy"
reference: true
---

### Two deploy classes

| Class | Trigger | `deployFiles` constraint | Typical use |
|---|---|---|---|
| **Self-deploy** | `sourceService == targetService`, or omitted and inferred to target | MUST be `[.]` or `[./]`; narrower patterns destroy target source | dev/simple mutable workspace |
| **Cross-deploy** | `sourceService != targetService`, or `strategy=git-push` | Cherry-pick build output: `./out`, `./dist`, `./build` | dev→stage promotion; stage runs foreground binaries |

### Picking deployFiles

| Setup block purpose | deployFiles | Why |
|---|---|---|
| Self-deploy (dev, simple modes) | `[.]` | Anything narrower destroys target on deploy. |
| Cross-deploy, preserve dir | `[./out]` | Lands at `/var/www/out/...`; use when `start` references that path or artifacts live in subdirs. |
| Cross-deploy, extract contents | `[./out/~]` | Tilde strips `out/`; use when runtime expects assets at `/var/www/`. |

`deployFiles` is evaluated against the **build-container filesystem after `buildCommands`**, not the editor tree — `[./out]` is correct even when `./out` is absent from the source checkout (the build creates it). ZCP doesn't pre-check the path; the builder emits `WARN: deployFiles paths not found` in `DeployResult.BuildLogs` if it produces no matches.
