---
id: bootstrap-adopt-mode-prompt
priority: 3
phases: [bootstrap-active]
routes: [adopt]
steps: [discover]
title: "Adopt — confirm mode with the user"
---

### Confirm mode per service

Ask the user, per service:

- **dev** — single mutable container, SSHFS-mountable, no stage pair.
  Best for active development.
- **standard** — dev + stage pair. Stage is the production-shaped deploy
  target; dev is the iteration surface.
- **simple** — single container that starts real code on every deploy;
  no SSHFS-mutation lifecycle.

Default suggestion when uncertain: **dev** for runtime services the user
is actively iterating on, **simple** for services treated as immutable
(cron workers, fire-and-forget processes). Record the confirmed choice in
the `ServiceMeta.Mode` field — it is immutable afterward, so only proceed
with explicit approval.
