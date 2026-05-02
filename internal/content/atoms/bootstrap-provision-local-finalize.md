---
id: bootstrap-provision-local-finalize
priority: 4
phases: [bootstrap-active]
routes: [classic]
environments: [local]
steps: [provision]
title: "Local provision — post-RUNNING finalize (dotenv + VPN)"
---

### After services reach RUNNING

1. `zerops_discover includeEnvs=true` — keys only.
2. `zerops_env action="generate-dotenv" serviceHostname="{hostname}"` —
   writes `.env` resolved from live env vars.
3. Add `.env` to `.gitignore` — it contains secrets.
4. Guide the user to start VPN: `zcli vpn up <projectId>`. Needs
   sudo/admin; ZCP cannot start it. The `local-development` guide
   covers VPN.
