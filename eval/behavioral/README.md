# eval/behavioral/ — interactive C4-layer behavioral findings eval

> POC complete (Phase 2 closed 2026-05-03). First live run logged at
> `audits/phase2-live-run.md`.

## What this is

A behavioral-findings eval primitive for the local Claude Code session.
The user invokes it from a normal Claude Code session ("spusť flow-eval"
or "spusť flow-eval co testuje bootstrap"). The orchestrator runs a
curated scenario through a real agent on the `zcp` container against the
`eval-zcp` Zerops project, captures the full transcript, then resumes
the same session with a retrospective prompt that asks the agent to brief
a future agent about the friction it hit. The local session reads the
retrospective + the full transcript locally and the user drives the
analysis interactively.

It fills the C4 (per-behavior) correctness layer that is not guarded by
existing per-atom lints (C1), per-render goldens (C2), or per-composition
goldens (C3). See `plans/eval-behavioral-findings-poc-2026-05-03.md`.

## What this is NOT

- **Not** the self-improving knowledge-eval loop in `eval/AGENT_PROMPT.md`.
  That loop is closed-loop (agent edits knowledge files itself across
  iterations). This is open-loop (agent observes, user decides what to
  edit and where).
- **Not** the substring-grader runner in `internal/eval/Runner.RunScenario`
  (the pre-existing single-shot scenario runner). Behavioral mode is a
  sibling method `RunBehavioralScenario` on the same `Runner` — same seed,
  init, preseed, and post-run cleanup envelope, two-shot resume in the
  middle.
- **Not** a CI gate. Interactive primitive only. No verdict, no pass/fail.
- **Not** auto-fix. Surfaces friction; user decides how to act.

## Architecture

The infrastructure is reused from `internal/eval/`. No duplicated runner,
no duplicated cleanup, no duplicated scenario parser.

- `internal/eval/Scenario` carries optional behavioral fields (`Tags`,
  `Area`, `Retrospective`, `NotableFriction`). Scenarios with
  `retrospective` set are routed to the behavioral runner.
- `internal/eval/Runner.RunBehavioralScenario` is the two-shot orchestrator:
  seed → init → preseed → spawn agent (persistence ON) → capture
  `session_id` → second `claude --resume` call with retrospective prompt
  → extract `self-review.md` → post-run cleanup. Side-by-side with the
  existing one-shot `RunScenario` method.
- Retrospective prompts live embedded in the binary at
  `internal/eval/retrospective_prompts/<style>.md`. The `briefing-future-agent`
  default ships in this POC.
- CLI: `zcp eval behavioral <list|run|all>` exposes the runner.
- Dev-side wrapper: `eval/behavioral/flow-eval.sh` does build-deploy →
  ssh → scp glue. Scenarios live in `eval/behavioral/scenarios/`,
  outputs land in `eval/behavioral/runs/<suiteId>/<scenarioId>/`
  (gitignored).

## Directory layout

```
eval/behavioral/
  scenarios/                        Scenario files (YAML frontmatter + markdown body)
    greenfield-node-postgres-dev-stage.md
    fixtures/                       Optional import YAML for non-empty seeds
    preseed/                        Optional scripts for state-specific fixtures
  audits/                           Committed audit notes per phase
    scenario-coverage-rationale.md
  runs/                             Per-run output (gitignored, scp'd back from zcp)
  flow-eval.sh                      Dev-side wrapper (build-deploy + ssh + scp)
  README.md                         This file
  .gitignore                        Excludes runs/

internal/eval/
  scenario.go                       Scenario struct (extended with behavioral fields)
  behavioral_run.go                 RunBehavioralScenario method + helpers
  behavioral_assets.go              go:embed retrospective_prompts/*.md
  behavioral_run_test.go            Unit tests for parser/extractors/load helpers
  retrospective_prompts/
    briefing-future-agent.md        Default retrospective prompt

cmd/zcp/
  eval.go                           eval subcommand dispatcher (added 'behavioral' branch)
  eval_behavioral.go                runEvalBehavioral list|run|all handlers
```

## Invocation surface

### From a local Claude Code session

The user types natural language; the assistant maps it to `flow-eval.sh` invocations:

| User says | Assistant does |
|---|---|
| "spusť flow-eval" | `flow-eval.sh list` → if one scenario, asks to confirm; if many, surfaces list and asks which |
| "spusť flow-eval co testuje bootstrap" | `flow-eval.sh list` → reads description+tags+area, picks matching scenario; if ambiguous, asks |
| "spusť flow-eval greenfield-node-postgres-dev-stage" | `flow-eval.sh greenfield-node-postgres-dev-stage` |
| "spusť všechny flow-evaly" | `flow-eval.sh all` |

The matching logic lives in the assistant — the script does no fuzzy match.

### Direct from terminal

```
./eval/behavioral/flow-eval.sh             # list scenarios
./eval/behavioral/flow-eval.sh list        # list scenarios
./eval/behavioral/flow-eval.sh <id>        # run one scenario
./eval/behavioral/flow-eval.sh all         # run every scenario
```

The wrapper:

1. Runs `go run ./cmd/zcp eval behavioral list ...` locally for `list` (no remote needed).
2. For run/all: `eval/scripts/build-deploy.sh` (build + scp + hash + kill stale `zcp serve`).
3. SCPs the scenario directory to `zcp:/tmp/zcp-behavioral-scenarios/`.
4. SSH-invokes `zcp eval behavioral run|all --scenarios-dir /tmp/...`.
5. Captures the suite ID printed on stderr.
6. SCPs results back from `zcp:/var/www/.zcp/eval/results/<suiteId>` to `eval/behavioral/runs/<suiteId>`.
7. Prints paths to each `self-review.md` for the assistant to read.

### Direct from zcp container (no wrapper)

```
zcp eval behavioral list --scenarios-dir <dir>
zcp eval behavioral run  --scenarios-dir <dir> --id <id>
zcp eval behavioral run  --file <path-to-scenario.md>
zcp eval behavioral all  --scenarios-dir <dir>
```

## Operational contract

Every `flow-eval.sh <id>` invocation runs unconditionally, in this order,
fail-loud on any error:

1. `eval/scripts/build-deploy.sh` — builds `linux-amd64` from current source,
   scp + sudo install on `zcp`, hash verify, kills stale `zcp serve`.
2. SCP scenarios from `eval/behavioral/scenarios/` to
   `zcp:/tmp/zcp-behavioral-scenarios/`.
3. SSH `zcp eval behavioral …`. The Go runner inside zcp:
   - **Cleanup project** (`CleanupProject`) — deletes all non-zcp services,
     unmounts stale SSHFS, cleans workdir (keeps `.claude`, `.mcp.json`,
     `.zcp`), resets workflow state. This is the existing pre-scenario
     cleanup; runs as part of the scenario seed.
   - **Seed** (`SeedEmpty` for greenfield).
   - **Init** (regenerate CLAUDE.md from current atom corpus).
   - **Spawn agent (call 1)** — claude headless with persistence ON,
     scenario prompt only (no eval awareness, no follow-up Q&A, no
     assessment instructions appended).
   - **Capture `session_id`** from the first stream-json system event.
   - **Spawn retrospective (call 2)** — `claude --resume <session_id>`
     with the retrospective prompt loaded from the embedded
     `retrospective_prompts/<style>.md`. Tight 5-minute timeout, max-turns 3.
   - **Extract self-review** from call 2's assistant text → write
     `<outDir>/self-review.md`.
   - **Detect compaction** at resume → record in `meta.json`.
   - **Post-scenario cleanup** (`CleanupProject` again).
4. SCP `<outDir>` back to `eval/behavioral/runs/<suiteId>/<scenarioId>/`.

There are no skip flags. Each run starts from a clean eval-zcp project
and a freshly-built binary on zcp. If you want to inspect state from a
prior run, do it BEFORE invoking flow-eval.

### Per-run artifacts under `runs/<suiteId>/<scenarioId>/`

```
task-prompt.txt              The exact prompt the agent received
retrospective-prompt.txt     The retrospective question (call 2)
transcript.jsonl             Stream-json from call 1 (full scenario run)
retrospective.jsonl          Stream-json from call 2 (resume)
self-review.md               Extracted assistant text from call 2 — what you read first
meta.json                    Run metadata: scenarioId, sessionId, model, wall times,
                              compaction flag, paths
```

## Interactive playbook (after `self-review.md` lands)

The local assistant reads `runs/<suiteId>/<scenarioId>/self-review.md` and
surfaces it to the user verbatim. Then the user drives:

| User intent | Assistant uses |
|---|---|
| "najdi root cause toho 502 v atomech" | `Read` self-review + transcript.jsonl turns nearby + relevant atoms; map friction → atom path |
| "vyextrahuj všechny retries" | `jq` over transcript.jsonl; group by tool; surface table |
| "porovnej s předchozím runem" | `Read` previous self-review.md + diff conversationally |
| "navrhni atom edit pro Trap-2" | Read offending atom + its sibling, propose edit + RED test sketch |
| "dej mi second opinion na navržený fix" | spawn Codex agent for independent review |

There is no automated grader, no findings.yaml, no taxonomy enforcement.
The cognitive bottleneck is reading the self-review and deciding what to
discuss; the assistant is the grader, with full project context.

## Round-trip protocol for fix verification

1. Land Trap-1 / Trap-2 fix in a separate plan + commit.
2. Re-run `flow-eval.sh greenfield-node-postgres-dev-stage`.
3. Compare new `self-review.md` to the prior one (assistant does this
   ad-hoc on request — no baseline file). Friction that was previously
   mentioned should be absent or downgraded.

## Failure modes

- **SSH drop mid-run** — transcript stays in `/tmp/...` on zcp; rerun the
  scp manually, then `zcp eval behavioral run --file <path-to-pulled>`
  with the local result dir. Or just re-run flow-eval (cleanup wipes state).
- **Agent wedge** — `--max-turns 80` cap bounds wall time; resume still
  works on partial session.
- **Compaction at resume** — `meta.json.compactedDuringResume = true` —
  retrospective may reflect compacted summary, not lived work. Mitigate
  by lowering scenario size or switching to one-shot self-eval (out of
  scope for POC).
- **eval-zcp auth expired** — `ssh zcp 'zcli login <token>'` (token in
  `.mcp.json` per `CLAUDE.local.md`).
- **Transcript huge** — `jq` filter before grep, e.g.
  `jq -c 'select(.type=="tool_use" or .type=="tool_result")' transcript.jsonl`.

## Cost / time budget

| Component | Wall time | Token cost (estimated) |
|---|---|---|
| Cleanup eval-zcp services | ~30 sec | — |
| Build + deploy ZCP | ~25 sec | — |
| Agent run (Opus, 70-80 turns, fullstack scenario) | 12-15 min | ~$0.50-$1.20 |
| Transcript pull + extract | ~10 sec | — |
| Retrospective resume call | ~30-45 sec | ~$0.10 |
| **Total per scenario** | **~14-17 min** | **~$0.65-$1.45** |

`flow-eval.sh all` is sequential — multiply by scenario count.

## Prerequisites

- Go toolchain (matches root `go.mod`).
- `jq` in PATH (used by `flow-eval.sh` for suite-id extraction).
- SSH access to `zcp` host (per `CLAUDE.local.md`).
- `zcli` authenticated on `zcp`, scope visible to `eval-zcp` project
  (per `CLAUDE.local.md` — login uses token from `.mcp.json`).

## Status

POC complete. First live run captured in `audits/phase2-live-run.md`.
Two-shot resume verified end-to-end; retrospective surfaces lived-experience
friction including Trap-2 (dev-mode 502 messaging) and four previously-
uncatalogued frictions on a single greenfield run.
