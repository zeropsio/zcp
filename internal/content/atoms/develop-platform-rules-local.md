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
start the dev server yourself, but it MUST run through your agent's
background-task primitive — a foreground shell call to `npm run dev`
/ `php artisan serve` / `bun --watch` blocks your turn until the
per-call shell timeout fires (agent-specific, typically 60-120s).
You're then stuck mid-step; the scenario timeout eventually clears
the seat. This is the dominant local-mode failure mode — the
anti-pattern below fires it every time.

Use your agent's background-task primitive:

- **Start:** background-spawn the framework dev command (returns immediately)
- **Probe:** foreground `curl` to confirm the port is up
- **Logs:** read stdout/stderr from the background task
- **Stop:** kill the background task when done

Anti-pattern (will hang the loop):

```
foreground npm run dev          ← never returns
foreground php artisan serve    ← same
```

Use the framework's normal dev command — bun, vite, npm run dev,
artisan serve, rails server, mix phx.server, etc. — wrapped in your
agent's backgrounding API. Probe via `curl` from a separate
foreground call once the background task is up.

| Topic | Local rule |
|---|---|
| Code | Edit the working directory. No SSHFS, no `/var/www/{hostname}` mount — that shape is container-only. |
| VPN | For managed services, use `zcli vpn up <projectId>`; it needs sudo/admin and ZCP cannot start it. |
| `.env` bridge | Generate live vars with `zerops_env action="generate-dotenv" serviceHostname="{stage-hostname}"`; add `.env` to `.gitignore` because it contains secrets. |
| Health checks | Probe localhost directly: `curl -s localhost:{port}/health`. Port comes from `zerops.yaml` `run.ports` or the user's command. |
| Deploy source | `zerops_deploy` deploys from the working directory. Check `git status` before deploying uncommitted edits: `strategy=git-push` needs commits; the default zcli-push path ships the tree. |
| Git-push setup | Before `zerops_deploy strategy=git-push`, verify `git status` + `git log`. If there is no work tree or commit, ask the user to run `git init && git add -A && git commit -m 'initial'`; ZCP does NOT initialize git in the user's working directory. The default zcli-push path needs no git state. |
