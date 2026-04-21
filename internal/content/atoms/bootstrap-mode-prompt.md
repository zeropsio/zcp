---
id: bootstrap-mode-prompt
priority: 3
phases: [bootstrap-active]
routes: [classic, adopt]
steps: [discover]
title: "Confirm mode — dev / standard / simple per service"
---

### Confirm mode per service

Every runtime service needs a **mode**; confirm it with the user before
committing the plan:

- **dev** — single mutable container, SSHFS-mountable, no stage pair.
  Best for active development.
- **standard** — dev + stage pair. Stage is the production-shaped deploy
  target; dev is the iteration surface.
- **simple** — single container that starts real code on every deploy;
  no SSHFS-mutation lifecycle.
- **stage** — never bootstrapped on its own; created as the stage half
  of a standard pair.

Default suggestion when uncertain: **dev** for runtime services the user
is actively iterating on, **simple** for services treated as immutable
(cron workers, fire-and-forget processes). Record the confirmed choice in
the `ServiceMeta.Mode` field — it is immutable afterward, so only proceed
with explicit approval.
