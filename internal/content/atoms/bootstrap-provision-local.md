---
id: bootstrap-provision-local
priority: 2
phases: [bootstrap-active]
environments: [local]
routes: [classic]
steps: [provision]
title: "Bootstrap — local-mode provision addendum"
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

1. `zerops_discover includeEnvs=true` — same protocol as container
   mode (keys only).
2. `zerops_env action="generate-dotenv" serviceHostname="{hostname}"`
   — resolves `${hostname_varName}` references against live data
   and writes a `.env` file. The local app reads `.env` via
   dotenv or framework auto-loading.
3. Add `.env` to `.gitignore` — it contains secrets.
4. Guide the user: `zcli vpn up <projectId>` to reach managed
   services. All hostnames (`db`, `cache`, etc.) resolve over VPN.
   One project at a time — switching disconnects the current.
