---
id: bootstrap-discover-local
priority: 1
phases: [bootstrap-active]
environments: [local]
steps: [discover]
title: "Bootstrap — local-mode discovery addendum"
---

### Local-mode discovery

Local-mode topology:

| Mode | Created on Zerops | Stays local |
|------|---------------------------|-----------------|
| Standard | `{name}stage` + managed services | Dev server |
| Simple | `{name}` + managed services | Push directly to the single service |
| Dev / managed-only | Managed services only | Everything else |

**Key rule** — no `{name}dev` service on Zerops in local mode. The
user's machine replaces the dev service.

The plan format is unchanged: submit `devHostname` in the plan. The
engine routes hostnames internally (standard mode creates stage, not
dev).

**VPN required** — the user runs `zcli vpn up <projectId>` to reach
managed services from their machine. Env vars are not active over
VPN as OS env vars; a `.env` bridge file is generated at provision.

If the user only wants managed services (DB, cache, storage) with
no runtime service on Zerops, submit an empty plan: `plan=[]`.
