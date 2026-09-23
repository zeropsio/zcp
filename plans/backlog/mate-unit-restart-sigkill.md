# `systemctl restart zerops@mate` SIGKILLs mate, so Mate never announces its restart

**Surfaced**: 2026-09-24 — the Mate client state-model programme's gate I (S.6 live check on `sm-birth-1`, a
dev build of mate with the C14 server state). The journal shows `exited: signal: killed` on a unit restart;
mate's SIGTERM handler (it publishes `draining: true` on `subscribeZeropsServerState`, spec-mate §2.5) never runs,
so the client shows "booting" instead of an announced restart. A direct SIGTERM to mate works: the client
shows "restarting (announced)" in 210 ms.

**Why deferred**: the client still recovers (the new `bootId` ends the restart); the cost is the wrong notice for
a few seconds on every unit restart (update, Enable, our Restart verb).

**Trigger to promote**: shipping S.6 (Mate > 0.11.42) — the announcement is its point.

## Sketch
- The zcp supervisor stops mate with SIGTERM and a grace period (e.g. 10 s) before SIGKILL, as systemd's default
  `KillMode`/`TimeoutStopSec` would; check `internal/init/init_mate.go` unit and whatever wraps the process.

## Also found (dev loop, same run)
- `eval/scripts/mate-dev-push.sh` packs `node-pty` from macOS without linux prebuilds; the container's
  `npm install` fails in node-gyp (`node-addon-api` missing), yet the chain reports exit 0. Workaround used:
  `npm install --ignore-scripts`, then copy `node-pty` from `versions/0.11.42`. The script should build/pack for
  linux (or install with the prebuilt binary) and fail loudly when the install fails.
