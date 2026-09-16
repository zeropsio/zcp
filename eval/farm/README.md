# eval/farm — operator runbook for `zcp eval farm`

Everything below runs from a full checkout on any machine or session with
network access to the Zerops API and the farm bucket — you never need to be
on the farm host itself. Contracts live in `docs/spec-eval-farm.md`; this
file is only the copy-paste sequence. Verify flags against `zcp eval farm`
with no arguments (its usage text) before relying on this doc for anything
beyond the order of operations.

## Prerequisites

`ZCP_AUTHORING=1` gates every verb (§3.1 FM-17). Four env vars come from
the operator credential file; `ZCP_FARM_PROJECT_ID` is new and non-secret:

| Var | Source |
|---|---|
| `ZCP_FARM_ACCOUNT_TOKEN` | credential file — account-wide token, reads/creates farm state |
| `CLAUDE_CODE_OAUTH_TOKEN` | credential file — the run's sole model-request credential (§2.4) |
| `ZEROPS_API_KEY` | credential file — unused by `farm`, kept for other `zcp` commands |
| `GIT_FARM_REPO` | credential file — unused by `farm` directly |
| `ZCP_FARM_PROJECT_ID` | non-secret; the current farm project — `swY2yczpQlqVLlcz0fCyFA` |
| `ZCP_E2E_GITHUB_PAT` | credential file — optional; only needed for a scenario declaring `gitRepoReset` (setup-then-actions) |
| `ZCP_E2E_GITHUB_PAT_ADMIN` | credential file — optional; only needed for a scenario declaring `gitRepoCreate` (setup-empty-remote) and `farm gc`'s repo-cleanup pass |

With `ZCP_FARM_PROJECT_ID` set, any `ZCP_FARM_*` key a verb needs and the
environment doesn't already have — `ZCP_FARM_S3_URL/BUCKET/KEY/SECRET`,
`ZCP_FARM_CLIENT_ID` — resolves once from the `farm` service's env in that
project (`${os_*}` references follow to the `os` service), instead of
failing "missing env var(s)". No value is ever printed; a resolution
failure names only the key it failed on.

## Kickoff sequence

```sh
set -a; . ~/.zerops-dev/agent-creds/farm.env; set +a   # ZCP_FARM_ACCOUNT_TOKEN, CLAUDE_CODE_OAUTH_TOKEN, ...
export ZCP_AUTHORING=1 ZCP_FARM_PROJECT_ID=swY2yczpQlqVLlcz0fCyFA

# 1. Build the binary under test. Farm run-projects are always linux/amd64;
#    --evaluator wants the same shape (build it once per farm, not per batch).
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/zcp-linux ./cmd/zcp

# 2. Push each part; every push prints its digest. Pushing is always safe:
#    parts are content-addressed, and `run` resolves the evaluator/wrapper
#    pins once at kickoff, so a batch already running keeps the ones it started with.
go run ./cmd/zcp eval farm push --evaluator /tmp/zcp-linux   # writes evaluators/current — re-push whenever eval/oracle/runner code changed (the evaluator runs seed, preseed, gitRepoReset and every check; a new candidate alone does not carry them)
go run ./cmd/zcp eval farm push --candidate /tmp/zcp-linux   # per batch
go run ./cmd/zcp eval farm push --scenarios eval/behavioral/scenarios   # hashes .farm-gate-set.txt into the scenario tree; also writes a legacy sets/<digest>/gate.txt copy
go run ./cmd/zcp eval farm push --wrapper eval/farm/wrapper.sh          # writes farm/wrapper/current

# 3. Kick off a batch — --set gate|all|<id,id,...> (§3.3). --note (max 200
#    chars) records why the batch ran, for `farm status`/console readers.
go run ./cmd/zcp eval farm run --candidate <candidate-sha> --scenarios <scenarios-digest> \
  --set gate --batch <batch-id> --note "gate run before v0.2 release"

# 4. Watch it.
go run ./cmd/zcp eval farm status [<batch-id>]

# 5. Pull the bundle once it settles.
go run ./cmd/zcp eval farm pull --batch <batch-id> --out /tmp/farm-out

# 6. Verdicts and coverage — no network, work over the pulled dir.
go run ./cmd/zcp eval farm report /tmp/farm-out
go run ./cmd/zcp eval farm coverage /tmp/farm-out

# 7. Advisory per-run observation (§7) — writes <run-dir>/observer/<id>.json.
go run ./cmd/zcp eval farm observe /tmp/farm-out/<runId>
```

`run` also takes `[--evaluator <sha256>] [--wrapper <sha256>]` (default to
the `evaluators/current` / `farm/wrapper/current` pointers `push` wrote),
`[--run-budget 45m]`, `[--max-concurrent 8]` (env `ZCP_FARM_MAX_CONCURRENT`;
`0` = unlimited; §3.3 FM-65), `[--detach]` (re-execs in the background, logs
to `farm-<batch>.log` in the current directory — gitignored), and
`[--observer <model>|off]`.

Every command above assumes the checkout root as the current directory.
From anywhere else, `go -C <checkout> run ./cmd/zcp eval farm …` does the
same (Go's `-C` flag): the command then runs in the checkout, so relative
paths and the `--detach` log resolve there too.

## Console

Hosted read model + agent API over the bucket (§8). URL + bearer token
live in `~/.zerops-dev/agent-creds/farm-console.env`
(`ZCP_FARM_CONSOLE_URL`, `ZCP_FARM_CONSOLE_TOKEN`) — never echo either.
Missing or stale → redeploy: `eval/farm/console/deploy.sh` (idempotent:
imports the console service only if absent, otherwise reuses it and its
recorded token). For a triage pass over the console's data, use the
`farm-triage` skill rather than curling it by hand — it also documents the
credential file and every read route.

## Cleanup

```sh
go run ./cmd/zcp eval farm gc --older-than 24h --yes
```
Prints every `zcp-farm-*` project and why it is or isn't eligible; `--yes`
deletes the eligible ones and revokes launch tokens orphaned by an earlier
no-bundle exemption (§3.6). Without `--yes` it only reports.

To hide bring-up/noise batches from the console without losing their evidence
(§3.7): `go run ./cmd/zcp eval farm archive <batch…> [--note "<why>"]` —
re-archiving is a no-op, and `farm archive --list` prints every archived batch.

## Wrapper (`eval/farm/wrapper.sh`)

Run-project supervisor + child (§2.3 FM-13) — its contract, redaction
behavior, and credential rule live in the top-of-file comment in
`wrapper.sh` itself, not here. To publish a change:

```sh
go run ./cmd/zcp eval farm push --wrapper eval/farm/wrapper.sh
```
Content-addressed (`farm/wrapper/<sha256>.sh`); updates the
`farm/wrapper/current` pointer (§1.1, R5) — every run project verifies the
fetched script's digest against its own run descriptor before executing
it. Offline test: `go test ./internal/eval/farm -run TestWrapper_ -v`.

## Mutation batch

Before a batch verdict is trusted to change zcp, `docs/spec-scenarios.md §9.4`
requires that a **mutation batch** has passed once per planted regression:
a candidate built with exactly one regression applied, run against the gate
set, must fail the ONE cell that regression breaks and nothing else. The
four patches live in `eval/farm/mutations/*.patch`
(`eval/farm/mutations/README.md` is the table of patch → regression → must-fail
cell → files touched); `internal/eval/farm/mutations_test.go` keeps each one
applying cleanly to the current tree.

This is an **owner action** (`docs/spec-eval-farm.md §3.1` maintainer gate:
the farm host holds an account-wide key) and each of the four mutations costs
roughly one gate batch — run them one at a time, not concurrently, so a
batch's own runs don't compete with another batch's runs for the same
scenario set.

For each patch, from a full checkout on the integration branch/sha you want
to certify:

```sh
# 1. Build the candidate WITH the patch applied, in a throwaway worktree —
#    never on the branch itself. Farm run-projects are always linux/amd64.
git worktree add /tmp/mut-01 <integration sha>
git -C /tmp/mut-01 apply eval/farm/mutations/01-env-set-no-restart.patch
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go -C /tmp/mut-01 build -o /tmp/mut-01/zcp-linux ./cmd/zcp

# 2. Push the mutated candidate — prints its sha256.
go run ./cmd/zcp eval farm push --candidate /tmp/mut-01/zcp-linux

# 3. Run the gate set against it (exact flags: cmd/zcp/eval_farm.go usage /
#    docs/spec-eval-farm.md §3.3 — quote them, never invent new ones).
go run ./cmd/zcp eval farm run --candidate <sha256 from step 2> \
  --scenarios <scenarios-digest, from an earlier `push --scenarios`> \
  --set gate --batch mut-01-<date>

# 4. Pull + report once it settles (§5.2).
go run ./cmd/zcp eval farm pull --batch mut-01-<date> --out /tmp/mut-01-out
go run ./cmd/zcp eval farm report /tmp/mut-01-out

# 5. Clean up the throwaway worktree.
git worktree remove /tmp/mut-01
```

Repeat for `02-subdomain-auto-enable-off.patch` (worktree `/tmp/mut-02`,
batch `mut-02-<date>`), `03-mount-from-cwd.patch` (`/tmp/mut-03`,
`mut-03-<date>`), and `04-adopt-wrong-stage-hostname.patch` (`/tmp/mut-04`,
`mut-04-<date>`) — four batches, one per patch, never combined into one
candidate.

**Pass criterion**: the named cell in `eval/farm/mutations/README.md`'s table
is `failed` on the mutated batch's report, and every other gate cell in that
report carries the same verdict (`passed`/`blocked`/etc.) as the unmutated
baseline gate batch it is compared against. A cell other than the named one
flipping to `failed` means the mutation's isolation claim in the table is
wrong — fix the table (or the patch) before trusting any future gate verdict
against it. `02-subdomain-auto-enable-off.patch` is a known exception today:
every subdomain-GET `liveness` cell also fails until those cells move to the
`internalLiveness` oracle family — see the note in the mutations table.
