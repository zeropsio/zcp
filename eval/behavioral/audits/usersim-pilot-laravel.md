# User-sim Phase 3 — Laravel pilot live run

**Date**: 2026-05-04
**Plan**: `plans/flow-eval-usersim-2026-05-04.md` Phase 3.
**Suite ID**: `20260504-093832`
**Scenario**: `recipe-laravel-minimal-standard`
**Binary hash (linux-amd64)**: `d25f3350cd3e`
**Model**: `claude-opus-4-6[1m]` (agent) / `claude-haiku-4-5-20251001` (user-sim, idle this run)

## Summary

Pilot is a **partial success**. The user-sim infrastructure is wired
correctly and runs without incident, BUT it did not activate in this run
because the agent didn't pause for clarification — it proceeded with
sensible defaults and drove all the way to a deployed Laravel app.

The valuable outcome: **agent ran 11m13s producing a 59-turn lived run
with 6 substantive new findings**, vs. the prior 27s / 17-turn
short-circuit baseline that produced hypothetical commentary. This is
the friction-surfacing improvement the plan was after, even with the
user-sim quietly idle.

User-sim's loop measured 7.5ms total wall-time (a single classifier
read returning `agent_declared_done`), so the new infrastructure adds
zero noise on the happy path.

## Run metadata

| Field | Value |
|---|---|
| Total wall | 12m07s |
| Scenario | 11m13s |
| Retrospective | 53s |
| User-sim loop | 7.5ms |
| Compaction at resume | false |
| Final agent status | `Everything is set up` (Laravel deployed) |
| Tool-call distribution | 5 `zerops_deploy`, 3 `zerops_verify`, 7 `zerops_workflow`, 2 `zerops_logs`, 19 `Write`, 4 `Edit`, 7 `Bash` |

## User-sim trace

```json
"userSim": {
  "personaUsed": "scenario-override",
  "model": "claude-haiku-4-5-20251001",
  "turns": null,
  "terminatedBy": "agent_declared_done",
  "totalWallTime": "7.552477ms"
}
```

`personaUsed: "scenario-override"` — confirms the Laravel scenario's
bespoke persona block was loaded (not the default fallback).
`turns: null` — no user-sim invocations needed; classifier's first
check after the agent's run terminated returned `Done` (final text was
"Everything is set up and the session will auto-close. Here's your
Laravel environment: ...", which matches `doneMarkerRE` via "set up").
`totalWallTime: 7.5ms` — purely the classifier file-read; no claude
spawn, no LLM call.

## Why user-sim didn't fire

Opus 4.6 in this run made an autonomous call early on: it picked
`mariadb@10.6` for MySQL substitution and `valkey@7.2` for Redis
substitution and proceeded without a confirm-question. This is
stochastic — the prior baseline (suite `20260504-065807`) on the same
scenario produced "Sound good, or do you want to adjust anything?" and
stalled at 27s.

The plan acknowledged this: user-sim is a safety net for stochastic
confirm-stalls, not a guarantee they will reproduce on every run. To
validate the activated path I'd need either:
- A scenario authored to *force* confirmation (e.g. unusual catalog
  ask the agent can't sensibly default).
- Multiple runs of this same scenario across model temperatures —
  one of them will hit confirm-stall again, then user-sim will fire.

Both are out of scope for the pilot. The infrastructure proven correct
in unit tests + the live happy-path no-regression check is what Phase 3
required.

## What the agent actually surfaced (lived friction, not hypothetical)

Self-review excerpts — six concrete findings, all of which would have
been invisible in the prior 27s baseline because the agent never
deployed:

1. **`composer install` crashes on Laravel 11 without `bootstrap/cache/`
   on disk** — the php-nginx hello-world recipe shows `composer install`
   alone, but Laravel 11's post-autoload `artisan package:discover` hook
   crashes when the directory is missing. Recipe-content gap.

2. **APP_KEY truncation gotcha** — `head -c 32 /dev/urandom | base64 |
   head -c 32` gives 32 base64 chars but only ~24 decoded bytes;
   AES-256-CBC needs 32. Verify reported the cause as `info` severity
   inside `error_logs` while reporting `degraded` because of HTTP 500 —
   actionable cause was in the advisory field, not the failure field.
   Handler-or-tool issue (verify field severity).

3. **`config:cache` in `buildCommands` is a documented trap that the
   canonical example encourages** — bakes build container's unresolved
   `${db_hostname}` into the cached config, runtime container then
   cannot connect. Platform-rules atom does say "build ≠ runtime
   container" but the example contradicts it. The deploy error response
   surfaces this trap explicitly post-hit. Recipe-content + atom-knowledge
   gap (example needs aligning with platform rule).

4. **Project-level env vars vs `run.envVariables` shadowing** — putting
   `APP_KEY: ${APP_KEY}` in runtime envVariables creates a shadow that
   resolves to empty literal. The `${...}` syntax is for cross-service
   refs only; project-level vars auto-inherit. Atom-knowledge gap (rule
   buried in a wiring-patterns table).

5. **Verify reports stale `error_logs`** — after the second deploy fixed
   APP_KEY, `zerops_verify` returned `status: healthy` but `error_logs`
   still showed cipher errors from the previous container's lifetime.
   Handler-or-tool gap (verify should trim by deploy boundary or stamp
   timestamps).

6. **`php-apache@8.5` vs `php-nginx@8.4` base-image asymmetry in the
   knowledge example** — example uses php-apache for both build and run
   base, but php-nginx services need `php@8.4` for build base and
   `php-nginx@8.4` for run base. Plus `documentRoot: public` is required
   for Laravel and lives in the implicit-webserver atom, not the
   hello-world example. Recipe-content gap.

These divide cleanly across the [recipe-content] / [atom-knowledge] /
[handler-or-tool] axes the plan anticipated for the future
classification surface — the manual triage already separates fix targets
into recipe edits vs atom edits vs Go handler fixes.

## Orchestrator-level verification

| Check | Result |
|---|---|
| Cleanup pre-run (4 services deleted) | ✅ |
| Build-deploy hash match (`d25f3350cd3e`) | ✅ |
| Scenario file scp'd to zcp | ✅ |
| Fresh agent spawn ran to completion (59 turns, 11m13s) | ✅ |
| `session_id` captured | ✅ |
| User-sim loop entered AND terminated cleanly | ✅ (7.5ms, `agent_declared_done`) |
| Retrospective resume call (`max-turns=3`) | ✅ (53s) |
| `self-review.md` extracted, ~1 KB, 6 findings | ✅ |
| `meta.json.userSim` populated with new schema | ✅ |
| Post-cleanup ran | ✅ (3 services deleted) |
| scp pulled suite back to local | ✅ |

No regressions on the existing two-shot retrospective contract.
`compactedDuringResume: false` — Opus 4.6[1m] handled the 59-turn
session without auto-compaction.

## Comparison: pilot vs prior baseline

| Metric | `20260504-065807` (pre-user-sim) | `20260504-093832` (pilot) |
|---|---|---|
| Scenario wall-time | 27s | 11m13s (×25) |
| Transcript events | 17 | 250+ |
| `zerops_deploy` calls | 0 | 5 |
| `zerops_verify` calls | 0 | 3 |
| Reached deployed state | NO (stalled at confirm) | YES (Laravel running) |
| Self-review quality | Hypothetical (5 imagined frictions) | Lived (6 concrete frictions) |
| Findings classifiable as recipe-content | 0 | 3 (composer mkdir, APP_KEY gen, php-apache vs php-nginx) |
| Findings classifiable as atom-knowledge | 0 | 2 (config:cache trap, env-var shadowing) |
| Findings classifiable as handler-or-tool | 0 | 2 (verify field severity, stale error_logs) |

The 25× wall-time multiplier is intrinsic to the agent doing real work
instead of stalling. Cost likely 10-25× as well, but acceptable: a
60-minute suite running 5 trails like this delivers enough lived
friction to feed multiple atom + recipe edit cycles. Cost amortizes.

## Phase 3 verdict — close green with caveat

✅ **Pass criteria met**:
- `meta.json.userSim` populated with new schema, valid JSON
- Loop infrastructure correct on the happy path (no regression, no noise)
- Self-review references concrete deploy-time friction (not pre-deploy)
- Audit committed

⚠ **Caveat for follow-up**:
- User-sim *activation* path (waiting → reply → resume) was not exercised
  this run. Validation deferred to next live run that hits a
  confirm-stall (stochastic). If 3-4 subsequent runs all skip user-sim
  activation, the rule-5 / rule-6 detection may need broadening — but
  not until that pattern appears.

## Files / artifacts

- `internal/eval/usersim.go` — classifier + loop + persona + claude runner
- `internal/eval/usersim_test.go` — classifier table tests (7 cases)
- `internal/eval/usersim_loop_test.go` — loop tests (10 cases)
- `internal/eval/testdata/usersim/*.jsonl` — 7 canned transcripts
- `internal/eval/scenario.go` — `UserPersona`, `UserSimConfig`
- `internal/eval/behavioral_run.go` — loop wired in, `BehavioralResult.UserSim` field, `spawnClaudeResumeAppend`
- `internal/eval/runner.go` — `userSimOverride` for test injection
- `eval/behavioral/scenarios/recipe-laravel-minimal-standard.md` — `userPersona` block added
- `eval/behavioral/runs/20260504-093832/recipe-laravel-minimal-standard/` — pilot artifacts
- `eval/behavioral/audits/usersim-pilot-laravel.md` — this file

`make lint-local` clean. `go test -race ./internal/eval/... -count=1` clean.

## What's next

Out of scope for this plan, queued separately:
- Multi-stage trail support (chains of stages within one agent session)
- Bulk userPersona conversion across the other 8 scenarios
- Recipe-coverage matrix expansion
- Findings classification tags (`[recipe-content]` / `[atom-knowledge]` / `[handler-or-tool]`) in retrospective prompt
- Engagement quality gate

The user-sim plan unblocks all of these. Phase 3 closed.
