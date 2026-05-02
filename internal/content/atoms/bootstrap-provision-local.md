---
id: bootstrap-provision-local
priority: 2
phases: [bootstrap-active]
routes: [classic]
environments: [local]
steps: [provision]
title: "Local provision addendum (classic route)"
---

### Local-mode provision

Import shape depends on mode:

| Mode | Runtime services | Managed services |
|------|-------------------------------|-----------------|
| Standard | `{name}stage` only; no dev on Zerops | Yes |
| Simple | `{name}` (single service) | Yes |
| Dev / managed-only | None — no runtime on Zerops | Yes |

**Stage properties (standard mode)**:

- Do NOT set `startWithoutCode` — stage waits for first deploy
  (READY_TO_DEPLOY).
- `enableSubdomainAccess: true`.
- No `maxContainers: 1` — use defaults.

**No SSHFS** — `zerops_mount` is unavailable in local mode; files live
on the user's machine.
