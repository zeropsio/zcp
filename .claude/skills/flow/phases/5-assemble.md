# Phase 5 — ASSEMBLE

Entry: `## Run State` `phase: assemble` — every BUILD wave landed, no
`blocked` Slice Register row. Runs as a **fresh verifier session**: read the
plan's `## Frame` / `## Slice Register` / `## Evidence Ledger` and the
integrated diff — never a BUILD transcript. Rubber-stamping here defeats the
gate.

## Battery — in order, fail-loud

Record each step as one row in `## Verify Trace` the moment it finishes
(don't batch the writes). A step's exact pass line is the evidence — paste
it, don't paraphrase.

| # | Step | Command | Pass line |
|---|---|---|---|
| 1 | Race | `make test-race` | `ok` on every package, no `FAIL`/`DATA RACE` |
| 2 | Lint | `make lint-local` | no output |
| 3 | Build-tag compile guard | `make vet-tags` | no output |
| 4 | Fast E2E | `make e2e-zcp-fast` | `--- PASS` lines, no `FAIL` |
| 5 | Deploy E2E | `make e2e-zcp-deploy` | `--- PASS` lines, no `FAIL` — **only if** the feature touches deploy/import/export/launch |
| 6 | Behavioral eval | `make flow-eval-local ID=<id>` | see preflight below — **only if** agent-behavior-facing |
| 7 | Real-binary drive | `make install`, then run the feature's headline path | observed output matches the AC |

`failed` on any applicable step blocks owner handoff — fix and re-run before
continuing; don't skip ahead to later steps on a red one.

## Step 6 preflight — CRITICAL, check before every behavioral-eval call

`internal/eval.CleanupProject` deletes every non-system service except `zcp`
in whatever project the eval run's `ZCP_API_KEY` resolves to. The documented
disposable eval project is **`eval-zcp`** (`eval/behavioral/README.md`) — a
project distinct from **`zcp-eval-clean`** (step 7's drive target and
PROVE's testbed, holding the live managed fleet: db, storage, docs, cache,
ch, search, queue, vectors, es, events). Before running `make
flow-eval-local`, confirm the identity the command will actually resolve —
NOT `zcp-eval-clean`. If you cannot confirm it, do not run the step; record
`## Verify Trace` as `blocked` with the reason. Never guess: getting this
wrong erases the managed fleet, not a disposable sandbox.

Consume `eval/behavioral/runs-local/<suite>/<id>/self-review.md` +
`verification.json` as OBSERVED signal, not a gate — behavioral eval is
warn-only by design (`docs/spec-testing-architecture.md`); a regression here
is a flag for owner judgment, never an auto-block. Skip step 6 entirely for
changes with no agent-facing behavior.

## Verify Trace

Fill `## Verify Trace` per `templates/plan.md`'s shape: one row per ACx
(`passed|failed|blocked|not-run` + evidence) plus the mandatory negative/
regression row. `not-run` on an in-scope ACx blocks handoff exactly like
`failed` — silence isn't a pass.

## Retest pack

Generate `plans/<slug>-<date>.retest.md` from `templates/retest.md`: every
Run command ties to a battery step above, every Drive step ties to an ACx,
Rollback reads `git revert <range>` from `## Run State` `integration:`, Docs
lists the spec §§ promoted at GATE 1. Zero-context bar: Karel runs it with
no other file open, in minutes.

Exit: `## Verify Trace` complete with no `failed`/`not-run` on an in-scope
ACx, retest pack written, `## Run State` `phase: awaiting-retest`. Notify
the owner with a compact summary + the retest file path.
