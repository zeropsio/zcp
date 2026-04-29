---
id: develop-platform-rules-local
priority: 5
phases: [develop-active]
environments: [local]
title: "Platform rules"
---

### Platform rules

| Topic | Local rule |
|---|---|
| Code | Edit the working directory. No SSHFS, no `/var/www/{hostname}` mount — that shape is container-only. |
| Dev server | Run it on your machine with the harness background-task primitive so the process survives the ZCP call and stdio does not block. In Claude Code: |

  ```
  Bash run_in_background=true  command="npm run dev"
  Bash                         command="curl -s -o /dev/null -w '%{http_code}' http://localhost:5173/"
  BashOutput                   bash_id={task-id}
  KillBash                     shell_id={task-id}
  ```

ZCP does not spawn processes on your machine; `zerops_dev_server` is
container-only. Use the framework's normal dev command.

| Topic | Local rule |
|---|---|
| VPN | For managed services, use `zcli vpn up <projectId>` from `claude_local.md`; it needs sudo/admin and ZCP cannot start it. |
| `.env` bridge | Generate live vars with `zerops_env action="generate-dotenv" serviceHostname="{stage-hostname}"`; add `.env` to `.gitignore` because it contains secrets. |
| Health checks | Probe localhost directly: `curl -s localhost:{port}/health`. Port comes from `zerops.yaml` `run.ports` or the user's command. |
| Deploy source | `zerops_deploy` deploys from the working directory. Check `git status` before deploying uncommitted edits: `strategy=git-push` needs commits; the default zcli-push path ships the tree. |
| Git-push setup | Before `zerops_deploy strategy=git-push`, verify `git status` + `git log`. If there is no work tree or commit, ask the user to run `git init && git add -A && git commit -m 'initial'`; ZCP does NOT initialize git in the user's working directory. The default zcli-push path needs no git state. |
