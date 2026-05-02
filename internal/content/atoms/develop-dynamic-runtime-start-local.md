---
id: develop-dynamic-runtime-start-local
priority: 3
phases: [develop-active]
runtimes: [dynamic]
environments: [local]
modes: [dev, standard, local-stage, local-only]
title: "Dynamic runtime — local dev server"
coverageExempt: "local-mode dynamic-runtime dev server — 30 canonical scenarios are container-focused; covered by Phase 5 quarterly live-eval (eval-zcp local-mode runs)"
---

### Dynamic-runtime dev server

In local env the dev server runs **on your machine**, not in Zerops. ZCP
does not spawn local processes — use the harness background-task
primitive so it survives the ZCP call and stdio does not block.

**Claude Code: `Bash run_in_background=true`.**

**Start:**

```
Bash run_in_background=true  command="{start-command}"
```

Use the framework dev command and bind to the app port.

**Check:**

```
Bash command="curl -s -o /dev/null -w '%{http_code}' --max-time 2 http://localhost:{port}/"
```

**Logs:**

```
BashOutput bash_id={task-id}
```

**Stop:**

```
KillBash shell_id={task-id}
```

If task id is lost: `Bash command="lsof -ti :{port} | xargs kill"`.

**Managed-service env vars** come from Zerops. Generate `.env` for the
dev command:

```
zerops_env action=generate-dotenv serviceHostname="{stage-hostname}"
```

Add `.env` to `.gitignore`; it contains secrets. VPN must be up
(`zcli vpn up`) to reach managed services.

**Do NOT use `zerops_dev_server`** — that tool is container-only (it
SSHes into Zerops dev containers). In local env it is not registered.
