---
id: develop-platform-rules-local
priority: 5
phases: [develop-active]
environments: [local]
title: "Platform rules — local env extras"
---

### Platform rules (local environment)

- **Code lives in your working directory.** Edit normally with your
  editor/IDE. No SSHFS, no `/var/www/{hostname}` mount — that shape is
  container-only.
- **Dev server runs on your machine.** Use your harness's background
  task primitive so the process survives the tool call and stdio does
  not block. In Claude Code that is `Bash run_in_background=true`:

  ```
  Bash run_in_background=true  command="npm run dev"
  Bash                         command="curl -s -o /dev/null -w '%{http_code}' http://localhost:5173/"
  BashOutput                   bash_id={task-id}
  KillBash                     shell_id={task-id}
  ```

  ZCP does not spawn processes on your machine — `zerops_dev_server` is
  container-only. Whatever dev command your framework gives you works:
  `npm run dev`, `bun --hot`, `vite`, `artisan serve`, `rails s`,
  `uvicorn main:app --reload`.
- **Managed services live on Zerops.** Access them from the local dev
  server requires VPN:

  ```
  zcli vpn up <projectId>
  ```

  VPN needs sudo/admin — guide the user to run it manually; ZCP cannot
  start it for them.
- **`.env` bridge.** Generate a dotenv file from live Zerops env vars:

  ```
  zerops_env action="generate-dotenv" serviceHostname="{stage-hostname}"
  ```

  Add `.env` to `.gitignore`; it contains secrets.
- **Health checks use localhost.** Probe your local dev server directly:

  ```
  curl -s localhost:{port}/health
  ```

  Port comes from `zerops.yaml` `run.ports` — substitute from the plan
  or from the user's command.
- **Stage deploys ride the user's filesystem.** `zerops_deploy` runs
  `zcli push` from the working directory; committed state only. Check
  `git status` before a deploy if you want to ship un-committed edits
  (they need committing first for strategy=git-push; strategy=push-dev
  ships whatever is in the tree).
