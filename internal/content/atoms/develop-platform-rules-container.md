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
  escaping bugs (nested quotes in `sed`/`awk` pipelines break). SSH is for
  **running processes**: starting the server, running framework CLIs
  (artisan/bun/npm), curling `localhost` to hit the server from inside the
  container.
- **Mount recovery.** If the SSHFS mount becomes stale after a deploy
  (symptoms: stat / ls returns empty, writes hang), remount via:

  ```
  zerops_mount action="mount"
  ```

- **Agent Browser.** `agent-browser.dev` is installed on the ZCP host
  container — use it to verify deployed web apps from the browser.
