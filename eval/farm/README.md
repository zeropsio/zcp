# eval/farm/wrapper.sh

Run-project supervisor + child driving one farm eval run
(`docs/spec-eval-farm.md` §2.3, FM-13). Started detached by the run
project's last init command (§2.1); reads its run descriptor entirely
from its own environment (§2.2 FM-12 — see the top-of-file comment in
`wrapper.sh` for the exact contract, including `ZCP_FARM_SCENARIOS_DIGEST`,
not yet in that spec's table).

The supervisor forks itself as a child (`"$0" --child`); a trap
(`EXIT INT TERM HUP`) redacts-then-uploads regardless of how the child
ended, so `kill -9 <child>` still produces a bundle. `kill -9 <supervisor>`
is the one case nothing runs after — no `done.json`.

POSIX `sh` only (`#!/bin/sh`, `set -eu`) — no bashisms.

## Offline test
```
go test ./internal/eval/farm -run TestWrapper_ -v   # from repo root
sh -n eval/farm/wrapper.sh
shellcheck -s sh eval/farm/wrapper.sh   # if installed
```
Drives the real script against real `curl` and an in-process fake S3
(`httptest`); skips itself if `sh` or `curl` is missing.

## Publishing a change
```
zcp eval farm push --wrapper eval/farm/wrapper.sh
```
