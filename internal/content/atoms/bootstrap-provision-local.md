---
id: bootstrap-provision-local
priority: 2
phases: [bootstrap-active]
environments: [local]
steps: [provision]
title: "Bootstrap — provision addendum"
---

### Local-mode provision

Import shape depends on mode:

| Mode | Runtime services | Managed services |
|------|-------------------------------|-----------------|
| Standard | `{name}stage` only (no dev on Zerops) | Yes — shared with container mode |
| Simple | `{name}` (single service) | Yes |
| Dev / managed-only | None — no runtime on Zerops | Yes |

**Stage service properties (standard mode)**:

- Do NOT set `startWithoutCode` — stage waits for first deploy
  (READY_TO_DEPLOY).
- `enableSubdomainAccess: true`.
- No `maxContainers: 1` — use defaults.

**No SSHFS** — `zerops_mount` is unavailable in local mode. Files
live on the user's machine.

**After services reach RUNNING:**

1. `zerops_discover includeEnvs=true` — keys only.
2. `zerops_env action="generate-dotenv" serviceHostname="{hostname}"` —
   writes `.env` resolved from live env vars.
3. Add `.env` to `.gitignore` — it contains secrets.
4. Guide the user to start VPN: `zcli vpn up <projectId>`. Needs
   sudo/admin; ZCP cannot start it. Guide `local-development`
   (via `zerops_knowledge`) covers VPN conventions.
