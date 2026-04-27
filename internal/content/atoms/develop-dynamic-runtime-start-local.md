---
id: develop-dynamic-runtime-start-local
priority: 3
phases: [develop-active]
runtimes: [dynamic]
environments: [local]
modes: [dev, standard]
title: "Dynamic runtime — start dev server on your machine"
---

### Dynamic-runtime dev server

In local env the dev server runs **on your machine**, not on a Zerops
runtime container. ZCP does not spawn local processes — use your harness's
background task primitive so the process survives the ZCP call and
stdio does not block the caller.

**In Claude Code: `Bash run_in_background=true`.**

**Start:**

```
Bash run_in_background=true  command="{start-command}"
```

Use whatever dev command your framework offers — bind to the port
your app listens on.

**Check if already running:**

```
Bash command="curl -s -o /dev/null -w '%{http_code}' --max-time 2 http://localhost:{port}/"
```

**Tail recent logs:**

```
BashOutput bash_id={task-id}
```

**Stop:**

```
KillBash shell_id={task-id}
```

Or kill by port if the task id is lost: `Bash command="lsof -ti :{port} | xargs kill"`.

**Managed-service env vars** (DATABASE_URL, REDIS_URL, …) come from
Zerops. Generate `.env` in your working directory so your dev command
reads them at startup:

```
zerops_env action=generate-dotenv serviceHostname="{stage-hostname}"
```

Add `.env` to `.gitignore` — it contains secrets. VPN must be up
(`zcli vpn up`) for your local dev server to reach managed services.

**Do NOT use `zerops_dev_server`** — that tool is container-only (it
SSHes into Zerops dev containers). In local env it is not registered
and would not make sense if it were.
