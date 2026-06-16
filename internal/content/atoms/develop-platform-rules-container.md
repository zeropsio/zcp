---
id: develop-platform-rules-container
priority: 5
phases: [develop-active]
envelopeDeployStates: [never-deployed]
runtimes: [dynamic]
environments: [container]
title: "Platform rules — mount & SSH usage"
reference: true
references-fields: [ops.DevServerResult.Running, ops.DevServerResult.HealthStatus, ops.DevServerResult.StartMillis, ops.DevServerResult.Reason, ops.DevServerResult.LogTail]
references-atoms: [develop-dynamic-runtime-start-container, develop-dev-server-reason-codes]
---

### Platform rules — mount & SSH usage

- **Mount caveats.** Mount is the build source for each new container.
  Never `ssh <hostname> cat/ls/tail …` for mount files — SSH adds
  shell-escape bugs (nested quotes in `sed`/`awk` break). One-shot
  SSH is for runtime CLIs only.
- **One-shot vs long-running over SSH.** One-shot commands (framework
  CLIs, git ops, migrations, `curl localhost`) exit quickly — run them
  on the service over SSH per the run-on-service rule. Long-running dev
  processes are the exception: a backgrounded `ssh <hostname> "cmd &"`
  holds the channel until the 120 s bash timeout, so start them through
  `zerops_dev_server` instead; its response carries `running`,
  `healthStatus`, `startMillis`, and on failure a `reason` code — read
  it before another call.
- **Mount recovery.** If the SSHFS mount goes stale after a deploy
  (stat/ls returns empty, writes hang), remount: `zerops_mount action="mount"`.
- **Agent Browser** — `agent-browser.dev` is available on the ZCP host
  for browser-backed verify checks (`zerops_verify` selects the right
  route per service shape).
