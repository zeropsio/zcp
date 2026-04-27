---
id: bootstrap-mode-prompt
priority: 3
phases: [bootstrap-active]
routes: [classic, adopt]
steps: [discover]
title: "Confirm mode — dev / standard / simple per service"
references-fields: [workflow.ServiceSnapshot.Mode, workflow.ServiceSnapshot.StageHostname]
---

### Confirm mode per service

Every runtime service needs a **mode**; confirm with the user before
submitting the plan.

- **dev** — single mutable dev container, SSHFS-mountable, no stage pair.
  Best for active iteration.
- **standard** — dev + stage pair. The envelope reports `stageHostname`
  on the dev snapshot and a separate snapshot with `mode: stage` for
  the stage service.
- **simple** — single runtime container that starts real code on every redeploy;
  no SSHFS mutation lifecycle.
- **stage** — never bootstrapped alone; it is the stage half of a
  standard pair.

Default to **dev** for services under active iteration, **simple** for
immutable workers. The plan commits the mode when you submit it; after
bootstrap closes, the envelope exposes the chosen mode as
`ServiceSnapshot.Mode`. Changing mode later requires the
mode-expansion flow (see `develop-mode-expansion`).
