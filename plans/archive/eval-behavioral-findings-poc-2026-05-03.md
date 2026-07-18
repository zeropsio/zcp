# Plan: Behavioral findings eval — POC for the missing C4 correctness layer (interactive-loop architecture)

**Status**: Proposed.
**Surfaced**: 2026-05-03 — two eval sessions on the current release (`Build me a team-notes-dashboard scenario` + redesign-hipster follow-up) ran on the `zcp` container against `eval-zcp`. Self-eval at the end of session 2 surfaced three pain points; independent code-side verification (parallel Explore + targeted Codex `--fresh --effort low` rescues) confirmed two as ZCP defects (Trap-1 — bootstrap recipe-route discover plan stringification + missing JSON example; Trap-2 — dev-mode dynamic-runtime first-deploy verify HTTP 502 because the dev-server precondition isn't surfaced before agent commits to verify) and one as out-of-scope scaffold (`@types/express ^5` typing in `nodejs-hello-world-app` recipe-app repo). Both ZCP traps are structurally guaranteed for every greenfield node-recipe-route session and are not caught by any existing test, lint, golden, or live-eval mechanism.

This plan is self-contained — no external references required beyond `CLAUDE.md`, `CLAUDE.local.md`, `MEMORY.md` (auto-loaded), and the source tree.

---

## How an LLM implementer should approach this plan

1. **Read top-to-bottom before starting any phase.** Architecture decisions (separate `eval/behavioral/` directory; observation-only contract; two-shot resume as orchestrator default; interactive grading by the local Claude session, not by an automated grader) are load-bearing.
2. **Order is strict.** Phase 1 → Phase 2. Each phase commits and is verifiable green before the next starts.
3. **TDD per `CLAUDE.md`.** Every step is marked `RED` (failing test first), `GREEN` (implementation makes test pass), `(audit)` (no test, produces committed artifact), `(doc)` (documentation-only), or `(operational)` (runs against live infra).
4. **Pause points are explicit.** Phase 1 final step (scenario + retrospective prompt blessed). Phase 2 final step (live run produced artifacts, user has read self-review). DO NOT skip.
5. **No acceptance gate on retrospective content.** The flow is: run → pull → display → discuss. Whether the retrospective surfaces Trap-1 / Trap-2 / something else / nothing useful is a topic for conversation in the local session, not a phase gate.
6. **No Trap-1 / Trap-2 fixes inside this plan.** Reproducing them is a topic for the post-POC conversation. Fixes ship in a separate plan.
7. **No silent decisions.** Each `(audit)` step produces a committed artifact under `eval/behavioral/audits/`. If criteria don't match observed reality, STOP and surface to the user — do not improvise.

---

## Why

### What the existing layers cover

The atom corpus is composed at runtime: envelope → `Synthesize` (axis filter + priority sort) → rendered guidance → MCP response → agent. Today's correctness mechanisms target five layers, of which three are guarded:

| Layer | Verifies | Status |
|---|---|---|
| **C1 Per-atom** | Single atom obeys axis frontmatter rules, anti-phrase lints, references-fields integrity | ✅ `internal/content/atoms_lint*.go` |
| **C2 Per-render** | A composed render is deterministic and pinned | ⏳ Goldens infrastructure landing per `plans/archive/atom-corpus-verification-2026-05-02.md` |
| **C3 Per-composition** | No two atoms contradict each other in the same render | ⏳ Goldens partial — explicit by-design gap for atom pairs not co-firing in any of the 30 canonical scenarios |
| **C4 Per-behavior** | After receiving the render, agent takes the expected next action — first call, no retries, correct schema, ordered preconditions | ❌ Nothing |
| **C5 Per-trajectory** | Sequence of renders across a multi-turn session leads agent to the goal without backtracking | ❌ Nothing |

### Why the existing layers can't catch Trap-1 and Trap-2

**Trap-1 — recipe-route discover plan retries.** Two compound causes:

- `internal/tools/workflow.go:37` (`Plan` field jsonschema) lacks the stringification warning that its sibling `recipePlan` (`workflow.go:46`) has. The MCP SDK input validator reports `validating /properties/plan: type: ... has type "string", want one of "null, array"` — technically correct, but reads like a deep-nested type error rather than a JSON-encoding mistake.
- `internal/content/atoms/bootstrap-recipe-match.md` describes the plan in prose only — *Per runtime pair: `devHostname`/`stageHostname` from recipe's `zeropsSetup: dev`/`prod` services; `type` + `bootstrapMode` verbatim* — without showing the required `runtime: { ... }` wrapper. Its sibling `bootstrap-mode-prompt.md:22` has a worked JSON example.

Per-atom lint sees nothing wrong. Goldens (when shipped) will pin atom IDs and rendered body but not which body the LLM actually ingests usefully. Pin-density tests verify selection, not action quality. There is no rendered-text → action contract in the test surface.

**Trap-2 — dev-mode dynamic-runtime first-deploy verify HTTP 502.** `internal/content/atoms/develop-first-deploy-verify.md` (priority 5, gate `deployStates: [never-deployed]`) tells the agent to run verify for each runtime that hasn't been deployed. It does not mention that for `runtimeClass: dynamic + mode: dev`, the deployed `zerops.yaml` rendered `run.start: zsc noop --silent` and the dev process is not running until `zerops_dev_server action=start` is called. The companion atom that does carry that triage — `develop-dev-server-triage.md` — gates on `deployStates: [deployed]` and therefore fires only AFTER the first deploy, by which point the agent already ran verify and got 502.

Atom axes today (`phases`, `routes`, `runtimes`, `modes`, `deployStates`, `environment`, `serviceStatus`, `closeDeployModes`, `gitPushStates`, `buildIntegrations`) describe WHO/WHAT but have no axis for WHEN-within-workflow-timing. Two atoms can be individually correct, jointly contradictory only via timing, and pass every existing guard.

**Third structural gap.** The bootstrap-classic and bootstrap-recipe discover atoms describe the same decision (plan submission shape) for two different routes. One has a worked JSON example, the other has prose. There is no atom-family parity contract. Codex confirmed atom engine has no include / snippet / shared-fragment mechanism — duplication is the only path, and there's no enforcement that the duplication stays in sync.

### What this POC adds

A behavioral findings eval — observation-only, slow, cheap-to-iterate, **interactive** — that runs a curated scenario through a real agent on the real `zcp` container against `eval-zcp`, captures the full transcript, and obtains the agent's own retrospective by **resuming the same session** with a post-hoc prompt. The local Claude Code session (the one the user is in) reads the retrospective + the full transcript locally; the user drives the analysis interactively. There is no automated grader, no LLM-as-judge subprocess, no taxonomy file, no baseline file, no acceptance verdict. The flow is **run → pull → display self-review → discuss**.

It does NOT:

- Self-fix knowledge or atoms — that pattern lives in `eval/AGENT_PROMPT.md`'s self-improving loop.
- Run on CI — interactive primitive only.
- Replace existing layers — C1-C3 stay; this fills C4 narrowly.
- Use a separate grader subprocess — the local Claude Code session IS the grader, with full repo + transcript context and per-run user intent.
- Assert pass/fail on retrospective content — what comes back is data for conversation, not a verdict.

### Why two-shot resume, not one-shot self-eval

A baked-in self-eval instruction in the original prompt creates an **observer effect**: the agent knows from turn 0 it will be evaluated, narrates decisions more carefully, occasionally adds false self-correction. Empirically visible in eval-session 2 — the curated tone reads like a review paper, not lived friction.

Two-shot resume mirrors how a real user works: agent runs the scenario without any eval awareness (`--no-session-persistence` dropped, default persistence kept, session ID captured from first stream-json event), then a second `claude --resume <session-id>` call asks a retrospective question. Same agent, same context window (Opus 4.7's 1M is comfortable for typical 70-90 turn fullstack scenarios), but post-hoc framing.

Cost: ~30-60 sec extra wall time + one extra ssh+claude call. Quality: noticeably more authentic friction signal in the retrospective.

### Why we believe this is worth the cost

The behavioral eval gives us two things neither C1-C3 nor live-evals provide:

1. **A reproducible read on what the agent actually struggles with**, captured in the agent's own words from a real run. Discussion in the local session maps that to atom / schema / spec edits.
2. **A round-trip primitive for fix verification.** Land a fix in a separate plan, re-run the scenario, compare old vs new self-review conversationally. No baseline files, no grading rubric — just two markdown blobs the local Claude session can diff and discuss.

Cost: per-run ~14-17 min wall time + ~$0.65-$1.45 in Claude API + ~30-60 sec for retrospective. User has explicitly accepted: drahé OK, pouštíme občas, ne na CI gate, stačí pár scénářů.

---

## Architecture — vlastní podložka under `eval/behavioral/`

```
┌──────────────────────────────────────────────────────────────────┐
│  LOCAL Claude Code session (user + assistant)                     │
│                                                                    │
│  User: "spusť flow-eval"                                           │
│        / "spusť flow-eval co testuje bootstrap"                    │
│        / "spusť flow-eval greenfield-node-postgres-dev-stage"      │
│        / "spusť všechny flow-evaly"                                │
│                                                                    │
│  Assistant:                                                        │
│   1. Bash: `eval/behavioral/scripts/flow-eval.sh list`             │
│   2. Read description + tags + area; map user intent to scenario   │
│      id. If unambiguous, run. If ambiguous, list candidates back   │
│      and ask which.                                                │
│   3. Bash run_in_background `flow-eval.sh <id>` (or `all`).        │
│   4. On notification: Read runs/<ts>/self-review.md, surface to    │
│      user verbatim. Wait for user intent.                          │
│   5. User intent → Read/Grep transcript.jsonl + timeline.json,     │
│      Read atoms / spec / source, possibly Agent or Codex for       │
│      second opinion. Conversation continues.                       │
└──────────────────────────────────────────────────────────────────┘
                                  ↑
                                  │ scp transcript + retrospective
                                  │
┌──────────────────────────────────────────────────────────────────┐
│  REMOTE — claude headless on zcp container (two-shot)             │
│                                                                    │
│  Call 1 (scenario run, persistence ON, no eval awareness):         │
│    cd /var/www && claude -p "<scenario.prompt>" \                  │
│      --model <agent.model> --max-turns <agent.max_turns> \         │
│      --output-format stream-json --verbose                         │
│    First stream-json event has `session_id` — extracted.           │
│                                                                    │
│  Call 2 (retrospective, --resume, ~30-60 sec):                     │
│    cd /var/www && claude --resume <session_id> \                   │
│      -p "<retrospective_prompt>" --max-turns 3 \                   │
│      --output-format stream-json --verbose                         │
│    Same session, same context window, post-hoc framing.            │
└──────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────┐
│  eval/behavioral/   (vlastní podložka, separate from              │
│                      eval/AGENT_PROMPT.md self-improving loop      │
│                      and from internal/eval/ Go runner)            │
│                                                                    │
│  scenarios/<id>.md           One file per scenario:                │
│                                YAML frontmatter (id, description,  │
│                                tags, area, agent config,           │
│                                notable_friction, retrospective     │
│                                style override)                     │
│                                + markdown body = scenario prompt.  │
│                                Cleanup + build-deploy are NOT      │
│                                gated per scenario — they run       │
│                                unconditionally on every invocation.│
│                                                                    │
│  scripts/                                                          │
│   flow-eval.sh              Single entry point.                    │
│                              Subcommands: list / all / <id>.       │
│                              Internal two-shot orchestrator:       │
│                              cleanup → build-deploy → call 1 →     │
│                              capture session_id → call 2 →         │
│                              extract self-review → meta.json.      │
│   cleanup-eval-zcp.sh       Idempotent allowlist cleanup           │
│                              ({zcp, agent-browser} kept).          │
│   _lib.sh                   Shared shell helpers                   │
│                              (yq, ssh opts, log prefix).           │
│   retrospective_prompts/    Closed enum of retrospective styles.   │
│     briefing-future-agent.md  Default — action-oriented friction.  │
│                              (More styles added when scenarios     │
│                               in the roadmap demand them.)         │
│                                                                    │
│  audits/                    Committed audit artifacts per phase    │
│  runs/<timestamp>/          Per-run output (gitignored)            │
│    transcript.jsonl           full stream-json (call 1 + call 2)   │
│    retrospective.jsonl        call 2 only (separate copy)          │
│    self-review.md             extracted assistant text from call 2 │
│    timeline.json              tool-call summary (existing parser)  │
│    meta.json                  scenarioId, runTs, model, sessionId, │
│                                mode, binaryHash, wallTimes,        │
│                                tokenCost, compactedDuringResume    │
│  README.md                  How to invoke (both natural-language   │
│                              and direct script paths) + interactive│
│                              playbook for the local Claude session │
└──────────────────────────────────────────────────────────────────┘
                                  ↑
                                  │ reuses (read-only)
                                  │
┌──────────────────────────────────────────────────────────────────┐
│  Reused infra (do NOT modify):                                    │
│  eval/scripts/build-deploy.sh    Binary deploy + hash + symlink   │
│                                   + stale-process kill            │
│  eval/scripts/extract-tool-calls.py  jsonl → tool-call timeline   │
│                                   (treat as utility; don't fork)  │
│  eval-zcp project (i6HLVWoiQeeLv8tV0ZZ0EQ, org Muad)              │
│                                   Platform fixture                │
│  zcp container                    Agent host (claude headless,    │
│                                   zcli authed, MCP zerops server) │
└──────────────────────────────────────────────────────────────────┘
```

### Boundary contract

`eval/behavioral/` does NOT import or call any code from:

- `eval/AGENT_PROMPT.md` self-improving loop (different intent: closed-loop fix vs. open-loop observe).
- `internal/eval/scenario.go` / `runner.go` / `grade.go` (different intent: substring grader on Go-typed expectations vs. interactive grading by local Claude session).
- `internal/eval/scenarios/*.md` (different scenario format).

`eval/behavioral/` DOES:

- Invoke `eval/scripts/build-deploy.sh` directly so each run uses the latest binary.
- Use `eval/scripts/extract-tool-calls.py` as a read-only utility for transcript timeline.
- Target the same `zcp` SSH host and the same `eval-zcp` Zerops project; cleanup script keeps `zcp` and `agent-browser`.

### Tooling — extend internal/eval/, don't duplicate it

The repo already has a Go-side scenario runner at `internal/eval/Runner.RunScenario` that handles parse → seed → init → preseed → spawn claude → cleanup → grade. It also has `cmd/zcp eval scenario|scenario-suite|cleanup|...` CLI subcommands. Building a separate orchestrator under `eval/behavioral/cmd/` would duplicate ~80% of this and drift over time.

Decision: extend the existing infrastructure instead of duplicating it.

- **Scenario struct** in `internal/eval/scenario.go` gains optional fields: `Tags`, `Area`, `Retrospective`, `NotableFriction`. Existing scenarios are unchanged (all fields optional; behavioral mode is detected by `IsBehavioral()` = `Retrospective != nil`).
- **`Runner.RunBehavioralScenario`** in `internal/eval/behavioral_run.go` is the parallel of the existing `RunScenario` for two-shot resume mode: same seed/init/preseed/cleanup envelope, two-shot in the middle (call 1 with persistence ON, capture session_id, call 2 `claude --resume` with retrospective prompt, extract self-review).
- **Retrospective prompts** are embedded via `go:embed` in `internal/eval/behavioral_assets.go` from `internal/eval/retrospective_prompts/<style>.md` — ship with the binary, no out-of-band sync.
- **CLI** adds `zcp eval behavioral <list|run|all>` subcommand in `cmd/zcp/eval_behavioral.go`. Reuses `initEvalRunner()` / `initPlatformClient()` / cleanup helpers.
- **Dev-side wrapper** `eval/behavioral/flow-eval.sh` is ~80 lines of bash that does build-deploy → scp scenarios → ssh `zcp eval behavioral …` → scp results back. Pure shell glue; all logic is in Go.
- **"Vlastní podložka"** stays at the filesystem level — scenarios in `eval/behavioral/scenarios/`, outputs in `eval/behavioral/runs/`, audits in `eval/behavioral/audits/` — without forcing Go-package duplication.

The grading layer differs (interactive in local Claude session vs. substring grader in `internal/eval/grade.go`), but the orchestration plumbing is shared. Net delta: ~3 small files added (`behavioral_run.go`, `behavioral_assets.go`, `eval_behavioral.go`), 2 existing files extended (`scenario.go`, `eval.go`), 1 dev-side bash wrapper.

---

## Invocation surface — `flow-eval.sh`

Three commands. The assistant in the local session reads the `list` output and maps user intent (descriptive or specific) to one of them.

```
flow-eval.sh             # equivalent to `list`
flow-eval.sh list        # show all scenarios with description + tags + area
flow-eval.sh <id>        # run that scenario (two-shot orchestrator)
flow-eval.sh all         # run every scenario sequentially
```

`list` output is plain text, one block per scenario — designed for the assistant (or a human) to read and pick:

```
greenfield-node-postgres-dev-stage
  Greenfield Node + Postgres dashboard via bootstrap recipe-route, with
  develop first-deploy on a dev/stage pair. Reproduces eval-session 1.
  tags:  bootstrap, recipe-route, develop, dev-stage, fullstack, node, postgres
  area:  bootstrap-and-develop
```

How the assistant routes user intent:

| User says | Assistant does |
|---|---|
| "spusť flow-eval" | `flow-eval.sh list` → if exactly one scenario, ask "spustit `<id>`?". If multiple, surface the list and ask which. |
| "spusť flow-eval co testuje bootstrap" | `flow-eval.sh list` → read description + tags + area, find matching scenario(s). If one, `flow-eval.sh <id>` (background). If multiple, list back and ask. |
| "spusť flow-eval greenfield-node-postgres-dev-stage" | `flow-eval.sh greenfield-node-postgres-dev-stage` (background). |
| "spusť všechny flow-evaly" | `flow-eval.sh all` (background). |
| "co máš za eval scénáře?" | `flow-eval.sh list` (foreground, render output). |

The matching logic lives entirely in the assistant — the script does no fuzzy match. User never has to remember exact IDs; tags and area in `list` output are the descriptive surface.

---

## The first scenario

`eval/behavioral/scenarios/greenfield-node-postgres-dev-stage.md` (Phase 1 deliverable; full draft below pending bless):

```markdown
---
id: greenfield-node-postgres-dev-stage
description: |
  Greenfield Node + Postgres dashboard via bootstrap recipe-route, with
  develop first-deploy on a dev/stage pair. Reproduces eval-session 1.
tags: [bootstrap, recipe-route, develop, dev-stage, fullstack, node, postgres, first-deploy]
area: bootstrap-and-develop
agent:
  model: claude-opus-4-7
  max_turns: 80
  cwd: /var/www
# Cleanup + build-deploy run UNCONDITIONALLY before every scenario invocation —
# they are not gated by scenario frontmatter. Allowlist hardcoded in
# cleanup-eval-zcp.sh = [zcp, agent-browser]. Every flow-eval run starts from a
# clean eval-zcp project + a freshly-built binary on zcp.
notable_friction:
  # Informational only — does NOT gate anything. Helps the assistant in
  # the local session know what to look for in the retrospective.
  - id: trap-1
    description: |
      bootstrap recipe-route discover plan retries — agent stringifies
      array OR submits flat shape without `runtime: { ... }` wrapper
    suspected_causes:
      - internal/tools/workflow.go:37 (Plan jsonschema, no string warning)
      - internal/content/atoms/bootstrap-recipe-match.md (no JSON example)
  - id: trap-2
    description: |
      dev-mode dynamic-runtime first-deploy verify returns HTTP 502 because
      dev process never started — agent must call zerops_dev_server first
    suspected_causes:
      - internal/content/atoms/develop-first-deploy-verify.md (no precondition)
      - internal/content/atoms/develop-dev-server-triage.md (timing-locked gate)
retrospective:
  prompt_style: briefing-future-agent  # default — see scripts/retrospective_prompts/
---

Build me a small team-notes dashboard with a Node backend and Postgres.
I want both a dev and a stage service.
```

The body after the second `---` is the verbatim user prompt to the agent. **It contains no eval awareness, no self-eval instruction, no mention of being graded.** The retrospective comes from the second resume call, not from the prompt body.

---

## The retrospective prompt (default style)

`eval/behavioral/scripts/retrospective_prompts/briefing-future-agent.md`:

```
That was the end of the task. Step back from the work for a moment.

If you were briefing a future agent who is about to do this same scenario
from scratch in a fresh session, what would you actually tell them?
Focus on friction you hit, not on a recap of what you did.

Specifically, in plain language:

1. Where did you have to retry a tool call because the first try was
   wrong? What was the error response and what made the correct shape
   non-obvious?
2. Where did you guess at something — a JSON shape, a precondition, an
   ordering — because the guidance you got didn't say it directly?
3. Where did a tool response confuse you, even briefly? What field or
   error code did you have to read twice?
4. Where was the guidance you got actively misleading — told you to do
   one thing when a different thing was needed?

Write this as a plain-language briefing, not a structured report. If you
hit nothing in one of the four categories, skip it. Don't pad. Don't
apologize. Don't recap what you successfully did. Three to eight
paragraphs is normal.
```

Additional styles (`transcript-audit.md`, `surprise-focus.md`, etc.) are added when scenarios in the roadmap demand them. POC ships only `briefing-future-agent`.

---

## Phases

### Phase 1 — Scenario file + retrospective prompt + audit (declarative)

Pure declarative deliverable. No live runs. Ends at a pause point.

| Step | Mode | File | Change |
|---|---|---|---|
| 1.1 | (audit) | `eval/behavioral/audits/scenario-coverage-rationale.md` | One-page note: why one scenario for POC (greenfield Node + Postgres dev/stage), what envelope shape it traverses (bootstrap-recipe-route → develop-first-deploy + standard-pair → auto-close), and what scenarios are explicitly deferred to roadmap (negative-control classic-route bootstrap; develop-iteration on already-deployed scope; multi-agent variants). |
| 1.2 | RED | `eval/behavioral/scenario/scenario.go` + `scenario_test.go`, `eval/behavioral/cmd/scenariotool/main.go` | Go package + tests + CLI (uses `gopkg.in/yaml.v3`, already in `go.mod`). Package exports `ParseFile`, `Validate`, types. Test asserts every `eval/behavioral/scenarios/*.md` has frontmatter with required keys: `id` (kebab-case), `description` (non-empty), `tags` (non-empty array), `area` (non-empty), `agent.model` (non-empty), `agent.max_turns` (positive int), `agent.cwd` (absolute path), `retrospective.prompt_style` (must reference an existing file under `scripts/retrospective_prompts/<style>.md`); markdown body after frontmatter non-empty; **`setup.cleanup` MUST NOT be present** (cleanup is unconditional). A second test exercises every assertion branch on a synthetic bad scenario. Runs as `go test ./eval/behavioral/...` — picked up by `go test ./...` automatically. CLI tool (`go run ./eval/behavioral/cmd/scenariotool validate <file>`) gives a shell-friendly entry point Phase 2 orchestrator uses for extraction (`scenariotool show <file>` emits JSON). RED state: tests fail before scenario file exists. |
| 1.3 | GREEN | `eval/behavioral/scenarios/greenfield-node-postgres-dev-stage.md` | Write per the draft above. Test from 1.2 passes. |
| 1.4 | GREEN | `eval/behavioral/scripts/retrospective_prompts/briefing-future-agent.md` | Write per the draft above (only file in this directory for POC). |
| 1.5 | (doc) | `eval/behavioral/.gitignore` | Ignore `runs/`. Audits, scenarios, scripts, prompts, README all stay tracked. |
| 1.6 | (doc) | `eval/behavioral/README.md` (skeleton) | Quick orient: what this is (interactive C4-layer eval), what it isn't (no overlap with `eval/AGENT_PROMPT.md` self-improving loop, no overlap with `internal/eval/` Go runner). Phase 1 README is skeleton; Phase 2 expands with full interactive playbook after live run shows what the flow actually looks like. |
| **Step 1.7 — PAUSE** | — | — | **Pause point**: present scenario file + retrospective prompt + audit rationale to user. Wait for explicit bless before Phase 2. User may add tags, edit prompt body, edit retrospective prompt, edit notable_friction. Do NOT proceed without approval. |

**Acceptance**: shell test passes; all files present; user has blessed the scenario shape and retrospective prompt.

### Phase 2 — Orchestrator + live run + README playbook

Wire all scripts, validate dry-run, do the first real run end-to-end, expand README with interactive playbook informed by what the live run looked like.

| Step | Mode | File | Change |
|---|---|---|---|
| 2.1 | GREEN | `eval/behavioral/scripts/_lib.sh` | Shared helpers: `yq` wrapper with version check, SSH options block (`StrictHostKeyChecking=no`, `ServerAliveInterval=30`, `ServerAliveCountMax=60`), error-on-empty extract helper, log prefix function, `resolve_scenarios_dir` returning absolute path. Sourced by `flow-eval.sh`. |
| 2.2 | GREEN | `eval/behavioral/scripts/cleanup-eval-zcp.sh` | Clears managed services on eval-zcp via `zcli` over `ssh zcp 'zcli ...'`, excluding hardcoded allowlist `[zcp, agent-browser]`. Idempotent. Output one line per service deleted. Exit 0 even if no managed services exist. Logs to stderr. |
| 2.3 | RED | `eval/behavioral/scripts/_test_flow_eval.sh` | Failing shell test for `flow-eval.sh`: <br/>- `flow-eval.sh` (no args) and `flow-eval.sh list` produce identical output: human-readable block per scenario containing id + description + tags line + area line. <br/>- `flow-eval.sh greenfield-node-postgres-dev-stage` (with `RUN_DRYRUN=1`) emits planned cleanup invocation, planned build-deploy invocation, planned ssh+claude call 1 (with prompt extracted from scenario body, model + max_turns from frontmatter), planned session-id capture step, planned ssh+claude call 2 (with `--resume <SESSION_ID>` placeholder + retrospective prompt loaded from `briefing-future-agent.md`), planned scp pull-back paths, planned self-review extract step, planned meta.json write. <br/>- `flow-eval.sh all` (with `RUN_DRYRUN=1`) loops over each scenario and emits the per-scenario plan in sequence. <br/>- `flow-eval.sh nonexistent-id` exits non-zero with helpful "no such scenario" message. <br/>Fails because flow-eval.sh doesn't exist. |
| 2.4 | GREEN | `eval/behavioral/scripts/flow-eval.sh` | Single entry point. Subcommand dispatch: <br/>- `list` (or no args): print the human-readable list. <br/>- `<id>`: run two-shot orchestrator for that scenario. <br/>- `all`: loop over scenarios, run each in sequence (each gets its own clean cycle). <br/>**Two-shot orchestrator (internal function `run_one`)** — every step is unconditional, no opt-out flags: <br/>1. Source `_lib.sh`; resolve `eval/behavioral/scenarios/<id>.md`; extract `agent.model`, `agent.max_turns`, `agent.cwd`, `retrospective.prompt_style` via `yq`; extract markdown body after second `---` as scenario prompt. <br/>2. Resolve retrospective prompt: `scripts/retrospective_prompts/<style>.md`. Fail-loud if missing. <br/>3. **Cleanup (always)**: run `cleanup-eval-zcp.sh`. Fail-loud (exit non-zero, abort run) on any error — must not proceed against contaminated state. <br/>4. **Build + deploy (always)**: run `eval/scripts/build-deploy.sh`; capture binary hash. Fail-loud on build error or hash mismatch — must not proceed with stale binary. <br/>5. `RUN_TS=$(date +%Y%m%d_%H%M%S)`; create `eval/behavioral/runs/$RUN_TS/`; write extracted prompt to `runs/$RUN_TS/prompt.txt`; scp to `zcp:/tmp/zcp-eval-$RUN_TS.prompt`. <br/>6. **Call 1 — scenario run, persistence ON**: `ssh zcp 'cd <cwd> && claude --dangerously-skip-permissions -p "$(cat /tmp/zcp-eval-$RUN_TS.prompt)" --model <model> --max-turns <max_turns> --output-format stream-json --verbose > /tmp/zcp-eval-$RUN_TS.jsonl 2>&1'`. <br/>7. `scp zcp:/tmp/zcp-eval-$RUN_TS.jsonl runs/$RUN_TS/transcript.jsonl`. <br/>8. **Capture session_id**: `SESSION_ID=$(jq -r 'select(.type=="system" and .subtype=="init") | .session_id' < runs/$RUN_TS/transcript.jsonl | head -1)`. Fail-loud if empty (print first 5 events for debug). <br/>9. **Call 2 — retrospective**: scp the retrospective prompt to `zcp:/tmp/zcp-eval-$RUN_TS.retro.prompt`. Then `ssh zcp 'cd <cwd> && claude --dangerously-skip-permissions --resume $SESSION_ID -p "$(cat /tmp/zcp-eval-$RUN_TS.retro.prompt)" --max-turns 3 --output-format stream-json --verbose > /tmp/zcp-eval-$RUN_TS.retro.jsonl 2>&1'`. <br/>10. `scp zcp:/tmp/zcp-eval-$RUN_TS.retro.jsonl runs/$RUN_TS/retrospective.jsonl`. <br/>11. **Extract self-review**: `jq -r 'select(.type=="assistant") | .message.content[]? | select(.type=="text") | .text' runs/$RUN_TS/retrospective.jsonl > runs/$RUN_TS/self-review.md`. Fail-loud if empty or < 100 chars. <br/>12. **Compaction detection**: scan `retrospective.jsonl` for events flagging auto-compaction at resume; record `compactedDuringResume: true|false` in meta.json. <br/>13. Run `python3 eval/scripts/extract-tool-calls.py runs/$RUN_TS/transcript.jsonl > runs/$RUN_TS/timeline.json` (best-effort; missing timeline does not fail the run). <br/>14. Write `runs/$RUN_TS/meta.json` (scenarioId, runTs, model, sessionId, mode=two-shot-resume, binaryHash, scenarioWallTime, retroWallTime, agentInputTokens, agentOutputTokens, retroInputTokens, retroOutputTokens, compactedDuringResume, finalOutcome). <br/>15. `RUN_DRYRUN=1` (test-only) replaces each ssh/scp/build/cleanup invocation with an echo of the planned command — used by the shell test at 2.3 only. Not a user-facing skip flag. Test from 2.3 passes. |
| 2.5 | (operational) | `eval/behavioral/audits/phase2-live-run.md` | **First live run**: `./eval/behavioral/scripts/flow-eval.sh greenfield-node-postgres-dev-stage` for real. Verify (orchestrator-level, not retrospective-content): cleanup deletes managed services on eval-zcp; build-deploy ships latest binary to `zcp` with hash match; call 1 runs to completion (or hits max_turns); session_id captured; call 2 runs and `self-review.md` is non-empty + non-trivial; `meta.json` validates; `transcript.jsonl` + `retrospective.jsonl` + `timeline.json` all present. Commit audit doc with: run timestamp, binary hash, transcript line count, retrospective line count, self-review.md char count, scenarioWallTime, retroWallTime, total token cost, `compactedDuringResume` value, agent's final outcome (1-line), full self-review.md content quoted. **No verdict on retrospective content** — that's a topic for live-session conversation, not for this audit. |
| 2.6 | GREEN | `eval/behavioral/README.md` (full) | Expand the Phase 1 skeleton, informed by the actual artifacts from 2.5. Sections: <br/>- **What this is**: interactive C4-layer eval; observation only; two-shot resume to mirror real user flow without observer effect. <br/>- **What this isn't**: not `eval/AGENT_PROMPT.md` (self-improving loop); not `internal/eval/` (Go substring grader); no CI gate; no auto-fix; no acceptance verdict on retrospective content. <br/>- **Invocation surface**: `flow-eval.sh` / `flow-eval.sh list` / `flow-eval.sh <id>` / `flow-eval.sh all`. List output format example. <br/>- **How to invoke from local Claude Code session**: examples per "How the assistant routes user intent" table above. Make explicit that the assistant calls `flow-eval.sh list` first, reads description + tags + area, and routes; if ambiguous, lists candidates back. <br/>- **How to invoke directly from terminal**: `cd /Users/macbook/Documents/Zerops-MCP/zcp && ./eval/behavioral/scripts/flow-eval.sh list` then `./eval/behavioral/scripts/flow-eval.sh <id>`. <br/>- **Interactive playbook**: after self-review surfaces, what kinds of intent the user can give and what the assistant does (read transcript / map to atoms / propose fix / spawn Codex for second opinion / generate plan doc). Concrete example intents. <br/>- **Round-trip protocol for fix verification**: user lands a fix in a separate plan, re-runs `flow-eval.sh <id>`, the local session diffs old vs new self-review conversationally. No baseline file, no automated diff. <br/>- **Failure modes**: SSH drop mid-run (transcript stays in /tmp on remote, manual scp), agent wedge (max_turns hit; resume still works on partial session), compaction at resume (meta.json flag; fall back to one-shot variant for that scenario by appending retrospective prompt to scenario body), eval-zcp auth expired (`zcli login` on zcp), transcript huge (jq filter before grep). <br/>- **Cost / time budget**: per-run wall time ~14-17 min, per-run cost ~$0.65-$1.45 (model-dependent), retrospective adds ~30-60 sec + ~$0.10. |
| **Step 2.7 — PAUSE** | — | — | **Pause point**: present `phase2-live-run.md` audit + the actual `runs/<ts>/self-review.md` to user. POC done — user reads self-review, decides what to talk about next (separate plan for Trap-1/Trap-2 fixes; second scenario from roadmap; multi-agent extension; nothing). |

**Acceptance**: dry-run shell test passes; live run produced full artifact set; README documents what the flow looks like in practice; user has read self-review.

---

## Acceptance criteria (cross-phase)

- Both phases land as separate atomic commits, each verifiable green at boundary.
- `eval/behavioral/scenarios/_test_scenario_yaml.sh` passes — scenario YAML schema enforced.
- `eval/behavioral/scripts/_test_flow_eval.sh` passes — list output shape, dry-run orchestrator plan, all-loop, error on unknown id.
- `eval/behavioral/audits/scenario-coverage-rationale.md` committed.
- `eval/behavioral/audits/phase2-live-run.md` committed with artifact shapes verified and full self-review.md quoted.
- `eval/behavioral/README.md` documents what / what-not / invocation surface / interactive playbook / round-trip protocol / failure modes / cost.
- `eval/behavioral/.gitignore` excludes `runs/`.
- No code in `internal/eval/`, `eval/AGENT_PROMPT.md`, or `eval/scripts/` modified.
- POC is observation-only — no Trap-1, Trap-2, or atom edits land in this plan; no acceptance verdict on retrospective content.

---

## Roadmap (post-POC)

These extensions are explicitly out of scope for this plan but documented so the framework converges instead of forking:

1. **Negative-control scenario.** `bootstrap-classic-route-discover.md` (tags `[bootstrap, classic-route, single, node]`) — same envelope axis content as POC scenario but classic route. Trap-1-equivalent friction should NOT appear in retrospective because `bootstrap-mode-prompt.md:22` has the worked example.
2. **Develop-iteration scenario.** Already-deployed scope, agent asked for code change (tags `[develop, iteration, deployed]`). Trap-2-equivalent friction should NOT appear because `develop-dev-server-triage` fires correctly.
3. **Multi-agent runner.** Same scenario file; vary `agent.model` via env var or new flag. Compare retrospectives cross-model conversationally.
4. **Phase × Mode × Environment matrix.** Gradually fill scenarios. Each tagged so the assistant can roll up category coverage from `flow-eval.sh list`. Pruning policy: when scenario count crosses 20, audit for redundancy.
5. **Alternative retrospective prompts** as `scripts/retrospective_prompts/<style>.md`. Add when default proves insufficient for specific scenario classes.
6. **Lint / CI integration (deferred).** A subset of patterns surfaced repeatedly in retrospectives could lift to commit-time gates. Today: explicitly out of scope.
7. **Atom-family parity contract.** If multi-scenario eval shows Trap-1-class friction on multiple route×phase atom pairs, that's empirical evidence to add a per-family parity lint.

---

## Risks and mitigations

| Risk | Mitigation |
|---|---|
| **Resume context auto-compaction** — Claude Code auto-compacts when resuming a long session, retrospective is about the summary not the lived work | Phase 2 step 2.4.12 detects compaction events in stream-json and records `compactedDuringResume: true` in meta.json. README documents the fallback: switch to one-shot self-eval prompt for that specific scenario by appending the retrospective prompt to the original scenario body (accept observer effect for that case). Empirically Opus 4.7 with 1M context handles 70-90 turn fullstack scenarios without compaction. |
| **Session ID extraction fragile** — claude headless changes stream-json schema and `session_id` field moves | Phase 2 step 2.4.8 uses `jq` filter on `type=="system" and subtype=="init"`; if extraction fails, fail-loud with the first 5 events of the JSONL printed for debugging. README documents how to manually extract and pass `SESSION_ID` env var as escape hatch. |
| **Retrospective is bland or off-topic** — agent gives a recap rather than friction | This is data, not a failure. Surface to user, discuss in session. If repeatedly bland across scenarios, iterate the prompt manually (cheap — `--resume`-only iteration is ~30-60 sec, no full scenario re-run needed). |
| **Agent variation across runs** — same scenario different run yields different retrospective | Accepted as intrinsic to LLM nondeterminism. Multiple runs over time average out; for regression: if a previously-mentioned friction stops being mentioned across consecutive re-runs after an alleged fix, treat as evidence the fix worked (judgment call in session). |
| **Live agent run wedges** — claude headless gets stuck, scenario transcript truncated | `--max-turns 80` cap bounds wall time; SSH `ServerAliveInterval=30` keeps tunnel alive. Wedge mid-session means call 1 transcript is partial — call 2 resume still works, retrospective surfaces "I got stuck doing X". Manual cleanup `cleanup-eval-zcp.sh` is idempotent. |
| **eval-zcp state drift** — services from prior runs interfere | `cleanup-eval-zcp.sh` runs unconditionally before every flow-eval invocation (no skip flag). Allowlist (`zcp`, `agent-browser`) keeps required infra. If a prior run left state worth inspecting, do it BEFORE invoking flow-eval — the next invocation will wipe it. |
| **Stale binary on `zcp`** — agent runs against an older build than the source HEAD | `eval/scripts/build-deploy.sh` runs unconditionally before every flow-eval invocation (no skip flag). Build error or hash mismatch aborts the run before any ssh+claude call. Every transcript is bound to a known binary hash recorded in `meta.json.binaryHash`. |
| **Transcript format change in claude headless** | `eval/scripts/extract-tool-calls.py` is the canonical timeline consumer; if it breaks, `flow-eval.sh` doesn't depend on its output for the critical path (timeline.json is best-effort). The retrospective extraction uses raw `jq` on the well-defined `assistant.message.content[].text` shape, which is stable. |
| **Cost** — every run = scenario invocation + retrospective invocation | Documented as accepted cost. meta.json records token counts so cost drift surfaces over time. |
| **AGENT_PROMPT.md self-improving loop confused with this** — user runs the wrong one | README explicitly contrasts the two; `flow-eval.sh` and `eval/run.sh` have visibly different argv shapes (subcommand vs no-args). Different cleanup contracts. Operators run one OR the other, not both concurrently. |
| **Ambiguous descriptive invocation** ("spusť flow-eval co testuje X" matches multiple scenarios) | Phase 2 step 2.4 keeps `flow-eval.sh` agnostic — no fuzzy match in the script. Assistant calls `list`, reads, decides; if ambiguous, lists candidates back to user and asks. Direct id invocation always works as escape. |
| **`zerops_dev_server` MCP tool name drift** | Today: `mcp__zcp__zerops_dev_server`. If renamed, scenario file unchanged (no tool name pinned) — only the assistant's grep heuristics during interactive analysis. |

---

## Out of scope (explicit non-goals)

- **No Trap-1 / Trap-2 fixes inside this plan.** Reproducing them is a topic for the post-POC conversation. Fixes ship in `plans/<slug>-trap-fixes-<date>.md`.
- **No CI auto-trigger.** Interactive spawn from local Claude Code session OR direct terminal only.
- **No multi-tenant infra.** One agent host (`zcp`), one platform fixture (`eval-zcp`), one scenario at a time. `flow-eval.sh all` is sequential.
- **No production-code change in `internal/` or `cmd/zcp/`.** Phase 1 + 2 add a small Go package + CLI under `eval/behavioral/scenario/` + `eval/behavioral/cmd/scenariotool/` for YAML schema + extraction (validator runs via `go test ./eval/behavioral/...`); orchestrator (`flow-eval.sh`) is bash. No changes to the ZCP binary's production code path.
- **No reuse of `eval/AGENT_PROMPT.md` or `internal/eval/` Go runner.** Different intents.
- **No standalone LLM-as-judge grader subprocess.** The local Claude Code session IS the grader. No `grader_prompt.md`, no `taxonomy.yaml`, no `testdata/<fixture>.jsonl`, no `baselines/<id>.yaml`.
- **No acceptance verdict on retrospective content.** Whether retrospective surfaces specific findings is a topic for live-session conversation, not a phase gate.
- **No fuzzy match in `flow-eval.sh`.** Three commands only. Descriptive invocation is the assistant's job, not the script's.
- **No skip flags for cleanup or build-deploy.** Both run unconditionally on every flow-eval invocation. The contract is: each run starts from a clean eval-zcp project + a freshly-built binary on zcp. `RUN_DRYRUN=1` exists only for the shell test in step 2.3 and never for user-facing skip behavior. If you want to inspect state from a prior run, do it BEFORE invoking flow-eval.
- **No multi-scenario sweep in POC.** One scenario; multi-scenario is roadmap.
- **No atom-family parity lint, no timing axis, no replacement of `internal/eval/scenarios/*.md`.** All roadmap-deferred.
- **No slash-command skill.** Natural-language → `flow-eval.sh` invocation suffices.

---

## How to launch in a fresh session

In a new Claude Code session at `/Users/macbook/Documents/Zerops-MCP/zcp`, paste this as the first user message:

```
Execute the plan at plans/eval-behavioral-findings-poc-2026-05-03.md.

Read CLAUDE.md, CLAUDE.local.md, and the plan first. Pay special attention
to the "How an LLM implementer should approach this plan" section near
the top — pause points and the no-acceptance-verdict rule are load-bearing.

Work the phases in order: Phase 1 → Phase 2. Each phase is one or more
atomic commits. Follow TDD per CLAUDE.md: RED tests first (failing), then
GREEN implementation. Audit steps produce committed artifacts in
eval/behavioral/audits/ — if criteria don't match observed reality, STOP
and ask me, do not invent a path.

At every phase boundary: run the phase's shell tests + verify acceptance
gate before continuing.

Phase 1 ends at a PAUSE POINT after the scenario file + retrospective
prompt + audit rationale are blessed. Wait for my explicit approval
before Phase 2.

Phase 2 ends at a PAUSE POINT after the live run produces full artifacts
(transcript + retrospective + self-review.md + meta.json) and README
documents the interactive playbook. The audit at 2.5 records the live
run + quotes self-review verbatim — it does NOT verdict on retrospective
content. Surface artifacts to me; we decide next steps in conversation.

Throughout: do NOT modify code under internal/eval/, eval/AGENT_PROMPT.md,
or eval/scripts/. Do NOT fix Trap-1 or Trap-2 inside this plan. Both
are out of scope.

Confirm the plan, then start Phase 1 with the scenario-coverage audit.
```
