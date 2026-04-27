---
id: develop-platform-rules-container
priority: 5
phases: [develop-active]
environments: [container]
title: "Platform rules"
references-fields: [ops.DevServerResult.Running, ops.DevServerResult.HealthStatus, ops.DevServerResult.StartMillis, ops.DevServerResult.Reason, ops.DevServerResult.LogTail]
references-atoms: [develop-dynamic-runtime-start-container, develop-dev-server-reason-codes]
---

### Platform rules

Mount basics in `claude_container.md` (boot shim). Container-only
cautions on top:

- **Mount caveats.** Deploy creates a new container from the mount; no
  transfer at deploy time. Never `ssh <hostname> cat/ls/tail …`
  for mount files — SSH adds shell-escape bugs (nested quotes in
  `sed`/`awk` break). One-shot SSH is for runtime CLIs only.
- **Long-running dev processes → `zerops_dev_server`.** Don't
  hand-roll `ssh <hostname> "cmd &"` — backgrounded SSH holds the
  channel until the 120 s bash timeout. See
  `develop-dynamic-runtime-start-container` for actions, parameters,
  and response shape; `develop-dev-server-reason-codes` for `reason`
  triage.
- **One-shot commands over SSH.** Framework CLIs, git ops,
  `curl localhost` exit quickly — no channel-lifetime concern:

  ```
  ssh <hostname> "cd /var/www && npm install"
  ssh <hostname> "cd /var/www && php artisan migrate"
  ssh <hostname> "curl -s http://localhost:{port}/api/health"
  ```

- **Mount recovery.** If the SSHFS mount goes stale after a deploy
  (stat/ls returns empty, writes hang), remount: `zerops_mount action="mount"`.
- **Agent Browser** — `agent-browser.dev` is available on the ZCP host;
  see `develop-verify-matrix` for the web verification path.
