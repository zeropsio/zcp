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
does not spawn local processes — use your agent's background-task
primitive so it survives the ZCP call and stdio does not block.

**Start:** background-spawn `{start-command}` (returns immediately).
Use the framework dev command and bind to the app port.

**Check:** foreground call

```
curl -s -o /dev/null -w '%{http_code}' --max-time 2 http://localhost:{port}/
```

**Logs:** read stdout/stderr from the background task.

**Stop:** kill the background task. If task id is lost: `lsof -ti :{port} | xargs kill`.

**Managed-service env vars** come from Zerops. Generate `.env` for the
dev command:

```
zerops_env action=generate-dotenv serviceHostname="{stage-hostname}"
```

Add `.env` to `.gitignore`; it contains secrets. VPN must be up
(`zcli vpn up`) to reach managed services.

**Do NOT use `zerops_dev_server`** — that tool is container-only (it
SSHes into Zerops dev containers). In local env it is not registered.
