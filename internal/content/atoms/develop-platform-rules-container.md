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
- **Mount recovery.** If the SSHFS mount becomes stale after a deploy
  (symptoms: stat / ls returns empty, writes hang), remount via:

  ```
  zerops_mount action="mount"
  ```

- **Agent Browser.** `agent-browser.dev` is installed on the ZCP host
  container — use it to verify deployed web apps from the browser.
