---
id: develop-platform-rules-container
priority: 5
phases: [develop-active]
environments: [container]
title: "Platform rules — container extras"
references-fields: [ops.DevServerResult.Running, ops.DevServerResult.HealthStatus, ops.DevServerResult.StartMillis, ops.DevServerResult.Reason, ops.DevServerResult.LogTail]
---

### Platform rules (container environment)

- **Code lives on an SSHFS mount** at `/var/www/{hostname}/` (one path per
  dev/simple service). A deploy rebuilds the container from mounted files;
  it does not transfer them at deploy time.
- **Read and edit directly on the mount.** Use the local `Read`, `Edit`,
  `Write`, `Glob`, `Grep` tools against `/var/www/{hostname}/` — they work
  transparently through SSHFS. **Never `ssh {hostname} cat …`, `ssh
  {hostname} ls …`, `ssh {hostname} tail …` or similar for files that exist
  on the mount** — every such call pays SSH setup cost and adds shell-
  escaping bugs (nested quotes in `sed`/`awk` pipelines break).
- **Long-running dev processes → `zerops_dev_server`.** Starting,
  stopping, restarting, probing status, and tailing the dev server
  all go through the MCP tool. The response has `running`,
  `healthStatus`, `startMillis`, `reason`, and `logTail` — all
  needed for diagnosing start failures without a follow-up call. Do
  not hand-roll `ssh {hostname} "cmd &"` — backgrounded SSH commands
  hold the channel open until the 120 s bash timeout. See
  `develop-dynamic-runtime-start-container` for the canonical start
  recipe and `develop-dev-server-reason-codes` for `reason` value
  triage.
- **One-shot commands over SSH.** Framework CLIs (`artisan`, `bun`,
  `npm install`, `composer`), git ops, and `curl localhost` stay on raw
  SSH — they exit quickly, no channel-lifetime concern:

  ```
  ssh {hostname} "cd /var/www && npm install"
  ssh {hostname} "cd /var/www && php artisan migrate"
  ssh {hostname} "curl -s http://localhost:{port}/api/health"
  ```

- **Mount recovery.** If the SSHFS mount becomes stale after a deploy
  (symptoms: stat / ls returns empty, writes hang), remount via:

  ```
  zerops_mount action="mount"
  ```

- **Agent Browser.** `agent-browser.dev` is installed on the ZCP host
  container — use it to verify deployed web apps from the browser.
