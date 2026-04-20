---
id: bootstrap-generate-intro
priority: 3
phases: [bootstrap-active]
routes: [classic]
steps: [generate]
title: "Generate zerops.yaml — shared rules"
---

### Generate — shared rules

Infrastructure verification only — write a hello-world server exposing
`/`, `/health`, `/status`. **Not the user's application** — that comes
in the develop workflow.

Files live at `/var/www/{hostname}/` in container env, in the working
directory in local env.

Use `setup: <name>` in `zerops.yaml` with the canonical recipe name
(`dev` or `prod`) — never the hostname.

**Build vs runtime env scopes**: `build.envVariables` and
`run.envVariables` live in separate containers and are not shared.
Build-time secrets (private registry creds, API tokens fetched during
build) go in `build.envVariables`; runtime config (DB host, feature
flags, app keys) goes in `run.envVariables`. `process.env.FOO` at
runtime will not see a `build.envVariables.FOO`.
