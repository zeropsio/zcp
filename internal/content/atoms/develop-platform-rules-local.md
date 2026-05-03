---
id: develop-platform-rules-local
priority: 5
phases: [develop-active]
runtimes: [dynamic]
environments: [local]
title: "Platform rules"
coverageExempt: "local-mode platform rules — 30 canonical scenarios are container-focused; covered by Phase 5 quarterly live-eval"
---

### Platform rules — local additions

### Dev server — always background

**ZCP does not spawn processes on your machine; `zerops_dev_server`
runs inside Zerops containers and never reaches your laptop.** You
start the dev server yourself, but it MUST run through the harness
background-task primitive — a foreground `Bash` call to `npm run dev`
/ `php artisan serve` / `bun --watch` blocks your turn until the
per-call bash timeout fires (2 minutes in Claude Code, then the
harness kills it). You're then stuck mid-step; the runner-level
scenario timeout eventually clears the seat. This is the dominant
local-mode failure mode — the anti-pattern below fires it every time.

In Claude Code, the canonical pattern is `run_in_background=true`:

```
Bash run_in_background=true  command="npm run dev"            ← spawns + returns
Bash                         command="curl -s -o /dev/null -w '%{http_code}' http://localhost:5173/"
BashOutput                   bash_id={task-id}                ← read stdout/stderr
KillBash                     shell_id={task-id}               ← cleanup
```

Anti-pattern (will hang the loop):

```
Bash command="npm run dev"          ← foreground, never returns
Bash command="php artisan serve"    ← same
```

Use the framework's normal dev command — bun, vite, npm run dev,
artisan serve, rails server, mix phx.server, etc. — wrapped in
`run_in_background=true`. Probe via `curl` from a separate
foreground `Bash` call once the background task is up.

| Topic | Local rule |
|---|---|
| Code | Edit the working directory. No SSHFS, no `/var/www/{hostname}` mount — that shape is container-only. |
| VPN | For managed services, use `zcli vpn up <projectId>` from `claude_local.md`; it needs sudo/admin and ZCP cannot start it. |
| `.env` bridge | Generate live vars with `zerops_env action="generate-dotenv" serviceHostname="{stage-hostname}"`; add `.env` to `.gitignore` because it contains secrets. |
| Health checks | Probe localhost directly: `curl -s localhost:{port}/health`. Port comes from `zerops.yaml` `run.ports` or the user's command. |
| Deploy source | `zerops_deploy` deploys from the working directory. Check `git status` before deploying uncommitted edits: `strategy=git-push` needs commits; the default zcli-push path ships the tree. |
| Git-push setup | Before `zerops_deploy strategy=git-push`, verify `git status` + `git log`. If there is no work tree or commit, ask the user to run `git init && git add -A && git commit -m 'initial'`; ZCP does NOT initialize git in the user's working directory. The default zcli-push path needs no git state. |
