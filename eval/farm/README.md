# eval/farm/wrapper.sh

Run-project supervisor + child driving one farm eval run
(`docs/spec-eval-farm.md` §2.3, FM-13). Started detached by the run
project's last init command (§2.1); reads its run descriptor entirely
from its own environment (§2.2 FM-12 — see the top-of-file comment in
`wrapper.sh` for the exact contract, including `ZCP_FARM_SCENARIOS_DIGEST`,
not yet in that spec's table).

The supervisor forks itself as a child (`"$0" --child`), in its own session
(`setsid`, falling back to a one-line perl `setsid()`+`exec` where the
`setsid` binary is missing) so the child's whole process group can be
signaled independently of the supervisor's own; a trap
(`EXIT INT TERM HUP`) kills that group, then redacts-then-uploads,
regardless of how the child ended — so `kill -9 <child>` still produces a
bundle, with no orphaned grandchild left running past it. `kill -9
<supervisor>` is the one case nothing runs after — no `done.json`.

Runs under `$HOME/.zcp-farm/<runId>/` by default (a fixed, discoverable
root — not a bare `mktemp -d`); `ZCP_FARM_RUNDIR` overrides it and exists
only as a test seam, not part of the run descriptor.

Credentials are OAuth-only (owner decision 2026-09-10): the wrapper reads
`CLAUDE_CODE_OAUTH_TOKEN` and refuses to start — `started.json` never PUT,
`done.json` carries a `"refused: ..."` execution dimension — if that token
is empty or if `ANTHROPIC_API_KEY` is present in the environment at all
(docs/spec-eval-farm.md §2.4).

FM-7 redaction rewrites any credential value it finds in place, including
inside `capture/`; when that changes a file's bytes, `update_capture_manifest`
(D8) patches the matching `sizeBytes`/`sha256` entry in every
`manifest.json` it finds under `$CAPTURE_DIR` — both the evaluator's capture
window at `capture/capture-<id>/manifest.json` (`files[].path` relative to
that window's own directory) and the legacy flat `capture/manifest.json`
layout, kept as a fallback. Without this, a redacted file's recorded size or
digest goes stale and `zcp capture` / `farm report` refuses the bundle as
corrupt (docs/spec-eval-farm.md §1.3 FM-7).

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
Uploads content-addressed to `farm/wrapper/<sha256>.sh` and writes the
plain-text pointer `farm/wrapper/current` (R5, same pattern as
`evaluators/current`) — there is no unpinned `farm/wrapper.sh` key. Every
run project's init line fetches the content-addressed key and
`sha256sum`-verifies it against the pinned digest in its own run descriptor
before it is ever `chmod +x`'d and executed, so a run's write-capable
bucket key can never make a later run boot an attacker-modified wrapper.

## Kickoff on the farm host

The farm host (`docs/spec-eval-farm.md` §3.1 FM-17/FM-18) needs only the
`zcp` binary and its service envs — no checkout of this repo. Everything
`farm run` needs (the `--set gate`/`--set all` scenario list, each
scenario's front matter, and the evaluator pin absent `--evaluator`) is
read from the bucket, not from disk:

```
zcp eval farm run --candidate <sha256> --scenarios <tree-digest> --set gate [--evaluator <sha256>]
```

That bucket state is produced by `farm push`, run once from a full
checkout, before a kickoff:

```
zcp eval farm push --evaluator <path>    # also writes evaluators/current
zcp eval farm push --scenarios eval/behavioral/scenarios
                                          # also writes sets/<digest>/gate.txt
zcp eval farm push --candidate <path>
zcp eval farm push --wrapper eval/farm/wrapper.sh
                                          # also writes farm/wrapper/current
```

`--gate-set <path>` names the local gate scenario list `--scenarios` also
uploads to `sets/<scenariosDigest>/gate.txt`; it is always resolved relative
to the `--scenarios` argument, never to the shell's cwd, so `farm push` works
from anywhere a full checkout is available. Omitting it defaults to
`<scenariosDir>/../../farm/gate-set.txt`, which is this repo's own
`eval/behavioral/scenarios` + `eval/farm/gate-set.txt` layout; pass it
explicitly only when the gate set lives elsewhere.
