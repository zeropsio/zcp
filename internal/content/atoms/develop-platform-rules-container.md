---
id: develop-platform-rules-container
priority: 5
phases: [develop-active]
environments: [container]
title: "Platform rules — container extras"
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
- **Long-running dev processes → `zerops_dev_server`.** Starting, stopping,
  restarting, probing status, and tailing the dev server all go through
  the MCP tool. It detaches the process correctly, bounds every phase
  with a tight budget, and returns structured `{running, healthStatus,
  reason, logTail}` — never hand-roll `ssh {hostname} "cmd &"` for a
  long-running process (the SSH channel holds open until the 120 s
  timeout).
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
