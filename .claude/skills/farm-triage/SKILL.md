---
name: farm-triage
description: Turn the eval farm console's data into a ranked, evidence-backed triage report with code locations and fix proposals, then stop. Triggers on "farm runs", "farm run <id>", "evaluate the farm", "what failed in the farm", "observer findings".
---

# farm-triage

Reads the eval farm console (spec `docs/spec-eval-farm.md` §7-§8). Produces one
triage report. Never edits code — fixes go through `/flow`.

## 0. Credentials

```
set -a; . ~/.zerops-dev/agent-creds/farm-console.env; set +a
```

Gives `ZCP_FARM_CONSOLE_URL` and `ZCP_FARM_CONSOLE_TOKEN`. Never echo either.
Every read is:

```
curl -sf -H "Authorization: Bearer $ZCP_FARM_CONSOLE_TOKEN" "$ZCP_FARM_CONSOLE_URL/api/…"
```

If the file is missing: say so and point the owner at
`eval/farm/console/deploy.sh` (spec §8.1) — that script mints the token. Stop;
do not guess a token or a URL.

## 1. Pick a mode

- "today" / "this week" / a batch id → **overview mode**.
- "farm run `<runId>`" → **single-run mode**, skip to step 4 for that one run.

## 2. Overview: pull runs and findings

```
curl -sf -H "Authorization: Bearer $ZCP_FARM_CONSOLE_TOKEN" \
  "$ZCP_FARM_CONSOLE_URL/api/runs.md?since=24h"        # or ?batch=<id>
curl -sf -H "Authorization: Bearer $ZCP_FARM_CONSOLE_TOKEN" \
  "$ZCP_FARM_CONSOLE_URL/api/findings.md?since=24h"
```

A run listed with no observation (`observerState` not `observed`): request one
and move on — do not block the triage waiting for it:

```
curl -sf -X POST -H "Authorization: Bearer $ZCP_FARM_CONSOLE_TOKEN" \
  -d model=claude-sonnet-5 "$ZCP_FARM_CONSOLE_URL/r/<runId>/observe"
```

Re-read `/api/runs.md` later, or triage that run from checks alone if it never
finishes in time.

## 3. Cluster

Group findings across runs by root cause, not by run: the same quoted ZCP
text, the same tool + error code, the same check id. Rank clusters by
severity (`high` > `medium` > `low`), then by number of runs affected within a
severity. `owner` on a finding is the observer's guess (`zcp-guidance`,
`zcp-tool`, `platform`, `agent`, `scenario`, `evaluator`) — confirm or correct
it once you've read the evidence; state your own owner label in the report.

## 4. Per cluster (and per run in single-run mode)

```
curl -sf -H "Authorization: Bearer $ZCP_FARM_CONSOLE_TOKEN" \
  "$ZCP_FARM_CONSOLE_URL/api/runs/<runId>.md"
curl -sf -H "Authorization: Bearer $ZCP_FARM_CONSOLE_TOKEN" \
  "$ZCP_FARM_CONSOLE_URL/api/runs/<runId>/steps.md?from=<n-3>&to=<n+3>"   # per cited step
curl -sf -H "Authorization: Bearer $ZCP_FARM_CONSOLE_TOKEN" \
  "$ZCP_FARM_CONSOLE_URL/api/runs/<runId>/self-review.md"
```

An evidence quote the observer did not mark `verified` (FM-46) is a lead, not
a fact — re-check it against the step text you just pulled before citing it.
For a finding that needs a stronger read, ask for a second opinion:

```
curl -sf -X POST -H "Authorization: Bearer $ZCP_FARM_CONSOLE_TOKEN" \
  -d model=claude-opus-5 "$ZCP_FARM_CONSOLE_URL/r/<runId>/observe"
```

(allowlist: `claude-sonnet-5`, `claude-opus-5`, `claude-fable-5-1`; anything
else 400s).

## 5. Map to code

Search the repo for the quoted ZCP text or error code — not the observer's
paraphrase:

- guidance atoms: `internal/content/atoms/`
- tool handlers: `internal/tools/`
- workflow: `internal/workflow/`
- operations: `internal/ops/`
- error codes: `internal/platform/errors.go`
- recipes: `internal/knowledge/recipes/`
- evaluator findings: `internal/eval/`
- scenario findings: `eval/behavioral/scenarios/`

Read the mechanism, don't stop at the grep hit. Label each root cause
`VERIFIED` (cite `file:line`) or `HYPOTHESIS` (named mechanism, not yet
confirmed by reading it).

## 6. Write the report

`plans/farm-triage-<YYYY-MM-DD>.md` (UTC date). Per problem: title, owner,
severity, runs affected (link each as `<$ZCP_FARM_CONSOLE_URL>/r/<runId>#s<n>`),
what happened (≤3 sentences), evidence (step number + quote), code
location(s) `file:line`, root cause (`VERIFIED`/`HYPOTHESIS`), fix proposal
(direction, rough size, the test that would pin it). Then a "not a ZCP
problem" section for findings owned `evaluator`/`scenario`/`platform`. Then a
ranked recommendation list, highest-impact first. Plain, short sentences — no
essay.

## 7. Stop

Do not implement any fix. Hand the report to the owner; a chosen fix goes
through `/flow`.

## 8. Console down — local fallback (spec §7.7)

Run a zcp built from this repo (the released binary has no farm verbs), from
the repo root, with `ZCP_AUTHORING=1`, the sink env `ZCP_FARM_S3_*` (spec
§3.1) for `pull`, and `CLAUDE_CODE_OAUTH_TOKEN` for `observe`:

```
ZCP_AUTHORING=1 go run ./cmd/zcp eval farm pull --batch <b> --out <dir>
ZCP_AUTHORING=1 go run ./cmd/zcp eval farm observe <dir>/<runId> [--model <m>]
```

Read the pulled bundle files directly and continue from step 3.
