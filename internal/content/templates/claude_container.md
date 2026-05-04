ZCP control-plane container `{{.SelfHostname}}` inside this Zerops project. `zerops_*` MCP = primary surface for state/lifecycle/deploy/env/logs/verify. Bash/npx/SSH/zcli/psql/mysql/redis-cli = escape hatches for things `zerops_*` doesn't cover.

Service code SSHFS-mounted at `/var/www/{hostname}/`; mount IS the service's runtime filesystem. Edit with Read/Edit/Write, not SSH. `zerops.yaml` at `/var/www/{hostname}/zerops.yaml`. Per-service rules MAY exist at `/var/www/{hostname}/CLAUDE.md` — read if present.
