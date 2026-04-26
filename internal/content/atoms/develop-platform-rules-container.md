---
id: develop-platform-rules-container
priority: 5
phases: [develop-active]
environments: [container]
title: "Platform rules — container extras"
references-fields: [ops.DevServerResult.Running, ops.DevServerResult.HealthStatus, ops.DevServerResult.StartMillis, ops.DevServerResult.Reason, ops.DevServerResult.LogTail]
references-atoms: [develop-dynamic-runtime-start-container, develop-dev-server-reason-codes]
---

### Platform rules (container environment)

- **Code lives on an SSHFS mount** at `/var/www/<hostname>/` (one path
  per dev/simple service). A deploy rebuilds the container from mounted
  files; no transfer at deploy time.
- **Read and edit directly on the mount.** Use Read/Edit/Write/Glob/Grep
  tools against `/var/www/<hostname>/` — they work through SSHFS. Never
  `ssh <hostname> cat/ls/tail …` for mount files; SSH adds setup cost
  and shell-escaping bugs (nested quotes in `sed`/`awk` pipelines break).
- **Long-running dev processes → `zerops_dev_server`.** Start/stop/
  restart/probe/tail all go through the MCP tool. Response carries
  `running`, `healthStatus`, `startMillis`, `reason`, `logTail` — full
  diagnosis without follow-up. Don't hand-roll `ssh <hostname> "cmd &"`
  — backgrounded SSH holds the channel until the 120 s bash timeout.
  See `develop-dynamic-runtime-start-container` for the canonical start
  recipe; `develop-dev-server-reason-codes` for `reason` triage.
- **One-shot commands over SSH.** Framework CLIs, git ops,
  `curl localhost` exit quickly — no channel-lifetime concern:

  ```
  ssh <hostname> "cd /var/www && npm install"
  ssh <hostname> "cd /var/www && php artisan migrate"
  ssh <hostname> "curl -s http://localhost:{port}/api/health"
  ```

- **Mount recovery.** If the SSHFS mount goes stale after a deploy
  (stat/ls returns empty, writes hang), remount: `zerops_mount action="mount"`.
- **Agent Browser** — `agent-browser.dev` is on the ZCP host; use it to
  verify deployed web apps from the browser.
