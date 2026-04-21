# dev-server-contract

Long-running dev servers start, stop, probe status, and tail logs via the `zerops_dev_server` MCP tool. The tool detaches the dev-server process from your SSH channel correctly (via `ssh -T -n` plus `setsid` with redirected stdio) so your call returns in seconds, not at the SSH timeout. It also bounds every phase (spawn, probe, tail) under a tight budget so any future regression costs seconds, not minutes.

## The four actions

- **action=start** — spawn the dev server as a detached process, wait `waitSeconds` for the health endpoint to respond, return structured status.
  ```
  zerops_dev_server action=start hostname={host} command="{start-cmd}" port={port} healthPath="{path}" waitSeconds={n}
  ```
- **action=status** — probe the health endpoint once; return whether the server is up and what it returned.
  ```
  zerops_dev_server action=status hostname={host} port={port} healthPath="{path}"
  ```
- **action=logs** — tail the last N lines of the dev-server log ring.
  ```
  zerops_dev_server action=logs hostname={host} lines={n}
  ```
- **action=stop** — terminate the process by port (or by process-match string). Tolerates "nothing to kill" as success.
  ```
  zerops_dev_server action=stop hostname={host} port={port}
  ```

There is also `action=restart`, which is a `stop` followed by a `start` with the same parameters.

## Error-class taxonomy

Every call returns a structured result. When it reports failure, the reason field falls into one of three classes — interpret the class before you decide next step:

- **start-failed** — the process started but crashed immediately. Reason codes include `spawn_error` (the exec itself failed: missing binary, wrong path, permission) and `spawn_timeout` (the process never printed anything readable within the spawn window). Next step: read the log tail, fix the command or the code, re-start.

- **bind-failed** — the process started but the health probe could not reach it. Reason codes include `health_probe_connection_refused` (port is free but the process did not bind), `health_probe_timeout` (process bound the port but does not respond within the probe window), and `health_probe_http_<code>` (the process responded but with an error status). Next step: verify the bind is `0.0.0.0` not localhost (see principles/platform-principles/02-routable-bind.md), verify the health path matches, verify the framework is actually up.

- **log-overflow** — the log ring filled faster than you tailed it, and the early part of the output has rotated out. Reason code: `log_overflow`. Next step: you have the recent tail; if you need earlier context, re-start the process with a quieter config (reduce log verbosity) or tail immediately after start.

## What the tool replaces

Raw `ssh host "cmd > log 2>&1 &"`. Backgrounding via `&` holds the SSH channel open until timeout fires because the backgrounded child still owns stdout and stderr. You lose minutes per start. Use the tool for every dev-server lifecycle operation.
