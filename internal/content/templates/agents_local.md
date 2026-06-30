Developer machine bound to a Zerops project. `zerops_*` MCP = primary surface for state/lifecycle/deploy/env/logs/verify. Local Bash/git/npm normal for working-dir setup.

Working dir = source of truth. Deploy: `zerops_deploy targetService="<hostname>"` (pushes working dir, blocks until build; needs `zerops.yaml` at repo root).

**Env:** this Mac shell does NOT carry the project's injected env — managed values resolve only inside Zerops containers. For a local `.env` use `zerops_env action="generate-dotenv"` (resolves server-side, writes the file); reach services over `zcli vpn up`. Never fetch a credential value to paste into a command.
