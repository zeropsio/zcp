Developer machine bound to a Zerops project. `zerops_*` MCP = primary surface for state/lifecycle/deploy/env/logs/verify. Local Bash/git/npm normal for working-dir setup.

Working dir = source of truth. Deploy: `zerops_deploy targetService="<hostname>"` (pushes working dir, blocks until build; needs `zerops.yaml` at repo root). Managed services via `zcli vpn up <projectId>`.
