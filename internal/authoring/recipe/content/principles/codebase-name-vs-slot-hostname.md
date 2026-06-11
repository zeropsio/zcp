# Codebase name vs slot hostname

`Plan.Codebases[].Hostname` is the bare codebase name
(`api`, `app`, `worker`) — that's what these recipe-MCP parameters
consume:

- `complete-phase codebase=<bare>`
- `fragmentId=codebase/<bare>/{integration-guide,knowledge-base,zerops-yaml,claude-md,intro}`
- `fragmentId=env/<N>/import-comments/<bare>`

The slot hostnames you see in `zcli`, SSHFS mount paths
(`/var/www/<slot>`), and cross-service refs (`${<peer>_*}`) —
`apidev`/`apistage`, `appdev`/`appstage`, `workerdev`/`workerstage`
— are deploy-slot identifiers. Two slots map onto one codebase
(`apidev`+`apistage` → `api`).

Filesystem paths use the slot hostname (`ls /var/www/apidev/src`).
Recipe-MCP parameters use the bare codebase name. When you see
"unknown codebase 'workerdev' (Plan codebases: [api app worker])",
drop the `dev`/`stage` suffix and retry.
