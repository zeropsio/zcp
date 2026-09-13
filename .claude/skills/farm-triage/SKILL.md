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

## 1. Resolve scope, then one `digest.md` call

`/api/digest.md` is the one call for "what did the farm find" (§8.4) — it
carries the scope header, the ranked problems, the failed/blocked runs and the
finished-not-yet-assessed count, all in one ≤8 KB read. Make it your first
substantive read, scoped by what the owner asked for:

- A batch id named → `digest.md?batch=<id>`. Archived batches (bring-up,
  mutation, negative checks — `kind=archived`, spec §3.7/§8.8) are not
  baselines: triage them only when the owner names one explicitly.
- **"today"** → **not** `since=24h`: a rolling 24h window drifts across UTC
  midnight and can silently miss or include the wrong batches. Instead, GET
  `/api/batches.md` first, keep the rows whose `createdAt` (UTC) falls on
  today's UTC date, then call `digest.md?batch=<id>` once per one of those
  batches.
- A genuine rolling window ("last 3 days", "this week") → `digest.md?since=<n>d`.
- **"farm run `<runId>`"** → single-run mode: skip straight to step 4 for that
  run alone; no `digest.md` call.

Every markdown list, `digest.md` included, opens with a one-line legend of the
terms it uses (§8.8) — read it once per session rather than re-deriving what a
term means from context.

`digest.md` is capped at 8 KB and says so when it cuts something ("… N more
problems/runs not shown, see …") — it never drops content silently. When it
truncates, follow the note to the named endpoint (`/api/problems.md`,
`/api/runs.md`), scoped the same way, for the rest.

## 2. Work the ranked problems, not raw findings

The console already clusters findings across runs into problems (§8.6) —
same key, same run, same root cause — server-side and deterministically; do
not re-cluster by hand. `digest.md`'s problems (and `/api/problems.md`, the
same list with its full member set) are pre-ranked: live status
(`new`/`first-seen`/`recurring`) before `gone`/`unconfirmed`, then highest
severity, then runs hit on the newest build. Work the list top to bottom.
Identical batches flip 4-8 of 29 cells on agent variance (spec-scenarios §9.4):
rank `recurring` (≥2 baseline batches) above anything seen once.

Each problem row already carries severity, cause, **surface**, **anchor**,
status, `hit <a>/<b> runs on <newest build>`, and one run link with its
finding's anchor (`/r/<runId>#f<n>`) — read these before fetching anything
else.

Also read `digest.md`'s failed/blocked runs section (run id, failed checks,
headline) and its finished-not-yet-assessed count.

## 3. Assess unassessed runs — gated, never open-ended

A run **needs an assessment** when it has `done.json`, is not queued or
running, and carries no current `ok` observation (§8.5). Request one only
when all of these hold:

- the run is inside the batch(es) step 1 resolved for this triage (never a
  run from an unrelated batch),
- the run has `done.json` (never a run still in flight),
- the run's batch is not empty (an evaluation batch, not one with zero
  finished runs),
- the running total for this triage is **five or fewer**.

At five, stop and ask the owner before requesting more — name the count and
its rough cost (**count × about $0.25**).

```
curl -sf -X POST -H "Authorization: Bearer $ZCP_FARM_CONSOLE_TOKEN" \
  -d model=claude-sonnet-5 "$ZCP_FARM_CONSOLE_URL/r/<runId>/observe"
```

Re-read `/api/runs.md?batch=<id>` (or `digest.md` again) once queued jobs
settle — a queued/running job shows in the `observerState`. Triage a run that
never finishes in time from its checks alone (the `verdict`/failed-checks
fields `digest.md`/`runs.md` already carry).

## 4. Drill into steps — top three problems only

For the **three** highest-ranked problems from step 2, and only those, pull
the full picture:

```
curl -sf -H "Authorization: Bearer $ZCP_FARM_CONSOLE_TOKEN" \
  "$ZCP_FARM_CONSOLE_URL/api/runs/<runId>.md"
curl -sf -H "Authorization: Bearer $ZCP_FARM_CONSOLE_TOKEN" \
  "$ZCP_FARM_CONSOLE_URL/api/runs/<runId>/steps.md?n=<step>"   # per cited step
curl -sf -H "Authorization: Bearer $ZCP_FARM_CONSOLE_TOKEN" \
  "$ZCP_FARM_CONSOLE_URL/api/runs/<runId>/self-review.md"
```

The full run detail carries fields `digest.md`/`problems.md` don't: the
**story** (task/expected/did/stuck/ending), the **judged checks** (§7.5 — did
the observer agree with each failed/blocked check, and why, not just whether
the run passed), and each finding's full **surface**/**anchor**/**span**, not
just its title. Read all of it before writing the problem up — a title and a
severity are not a diagnosis.

An evidence quote the observer did not mark `verified` (FM-46) is a lead, not
a fact — re-check it against the step text you just pulled before citing it.

For every **other** problem in scope, report straight from `digest.md`'s/
`problems.md`'s own fields (title, severity, cause, surface, anchor, status,
fix) — do **not** fetch its run detail or steps.

For a top-three finding that needs a stronger read, ask for a second opinion:

```
curl -sf -X POST -H "Authorization: Bearer $ZCP_FARM_CONSOLE_TOKEN" \
  -d model=claude-opus-5 "$ZCP_FARM_CONSOLE_URL/r/<runId>/observe"
```

(allowlist: `claude-sonnet-5`, `claude-opus-5`, `claude-fable-5-1`; anything
else 400s.) This counts against step 3's five-run/$0.25-each gate too.

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

`plans/farm-triage-<YYYY-MM-DD>.md` (UTC date). Per problem: title, cause,
severity, status (new/first-seen/recurring/gone/unconfirmed), surface +
anchor, runs affected (link each as
`<$ZCP_FARM_CONSOLE_URL>/r/<runId>#f<n>`), what happened (≤3 sentences),
evidence (step number + quote), code location(s) `file:line`, root cause
(`VERIFIED`/`HYPOTHESIS`), fix proposal (direction, rough size, the test that
would pin it). Then a "not a ZCP problem" section for findings whose cause is
Agent mistake / Test scenario / Test check, and runs `blocked: preparation`
(spec §4.5 — the harness, never zcp or the agent). Then a ranked recommendation list,
highest-impact first. Plain, short sentences — no essay.

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

Read the pulled bundle files directly and continue from step 2 (there is no
`digest.md` locally — read each run's own findings and cluster by hand).
