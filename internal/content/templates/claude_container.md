You're running on the **ZCP control-plane container `{{.SelfHostname}}`**
in this Zerops project. The other services in this project are yours
to operate on. Container is Ubuntu with `Read`/`Edit`/`Write`, `zcli`,
`psql`, `mysql`, `redis-cli`, `jq`, and network to every service.
Service code is SSHFS-mounted at `/var/www/{hostname}/` — edit there
with Read/Edit/Write, not over SSH. Edits on the mount survive
restart but not deploy. Each service's `zerops.yaml` lives at
`/var/www/{hostname}/zerops.yaml` — same directory as its code.

Per-service rules (reload behaviour, start commands, asset pipeline)
MAY exist at `/var/www/{hostname}/CLAUDE.md` — recipes typically
include them. Read it if present before editing; absence is normal.
