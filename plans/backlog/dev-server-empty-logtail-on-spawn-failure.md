# dev_server failure with empty `logTail` provides zero diagnostic signal

**Surfaced:** 2026-05-04, eval suite `20260504-065807` `classic-go-simple` retro.

**Why deferred:** four-phase fix targeted response noise reduction; this
is the opposite class — response is too quiet on a real failure.

## What

When `zerops_dev_server action=start` fails to spawn the process (bad
command, missing binary, exec error from non-shell wrapper), the
response comes back with:

- `running: false`
- `reason: health_probe_connection_refused`
- `logTail: ""` (empty)
- log file empty

Agent quote: "no diagnostic signal whatsoever. I had to SSH in manually
to figure out what was happening."

Two distinct problems compound:

1. The `reason` enum collapses "process never started" and "process
   started but isn't listening" into one code (`connection_refused`).
   These are very different failure modes.
2. `logTail` is empty because the spawn error never reached the log
   file — it's a stderr emission from the wrapper, not the user
   process.

## Trigger to promote

Promote when ops-layer dev_server work is on the table. Also worth
promoting if any further eval flags "I had to SSH in to debug a failed
dev_server".

## Sketch

Two changes in `internal/tools/dev_server.go` (or `internal/ops/dev_server.go`):

- Distinguish `reason` codes:
  - `spawn_failed` — wrapper reported exec error before user process
    started (capture stderr from the wrapper here).
  - `health_probe_connection_refused` — process is alive but not
    listening on the expected port.
  - `health_probe_timeout` — probe hit the wait window.
- Always populate `logTail` from the wrapper's combined stderr/stdout
  when no user-process log lines exist yet, so a spawn error surfaces.

## Risks

- Wrapper boundaries in the dev_server implementation may not
  currently capture pre-exec stderr. Real fix may require restructuring
  the wrapper, not just relabeling.
