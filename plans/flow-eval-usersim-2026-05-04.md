# Plan: Flow-eval user-sim — multi-turn realistic conversation via headless Claude

**Status**: Proposed.
**Surfaced**: 2026-05-04 — analysis of flow-eval suite `20260504-065807` (9 scenarios across classic / recipe / adopt / recovery / delivery routes). One scenario (`recipe-laravel-minimal-standard`) terminated at 27 s / 17 transcript events because the agent stopped at `"let me confirm the MariaDB substitution"` and never deployed. The retrospective was therefore hypothetical, not lived experience. Reframe: agent's confirm-question is **wanted** behavior — a real user would also expect to be asked before silent `mysql → mariadb` substitution. What's missing is the other half of the conversation: a simulated user who can answer.

This plan is self-contained — no external references beyond `CLAUDE.md`, `CLAUDE.local.md`, `MEMORY.md`, `eval/behavioral/README.md`, and the source tree.

---

## How an LLM implementer should approach this plan

1. **Read top-to-bottom before starting Phase 1.** Detection rules and termination conditions are load-bearing — implementing them loosely produces user-sim loops or false-success exits.
2. **Order is strict.** Phase 1 (scenario schema + classifier) → Phase 2 (user-sim spawn + loop wiring) → Phase 3 (pilot conversion + live verification). Each phase commits green before the next starts.
3. **TDD per `CLAUDE.md`.** Each step marked `RED` / `GREEN` / `(audit)` / `(doc)` / `(operational)`.
4. **Pause points are explicit.** Phase 1 last step (classifier table validates against five canned transcripts). Phase 3 last step (pilot live-run reproduces full Laravel deploy). DO NOT skip.
5. **No acceptance gate on retrospective content.** Same contract as `eval-behavioral-findings-poc-2026-05-03.md`: run → pull → read → discuss. The local session is the grader.
6. **No new orchestrator.** Existing `RunBehavioralScenario` is extended in place. No fork, no parallel runner.
7. **No silent decisions.** Any deviation from the rule table in Phase 1 (e.g. classifier decides "agent waiting" looser than the spec) STOPs and surfaces to the user.

---

## Why

The existing two-shot retrospective architecture (one fresh agent run + one resume call for self-review) treats the agent run as a single user-turn followed by autonomous work. That works when the agent has enough autonomy to drive to deploy without further input. It collapses when:

- The agent legitimately needs clarification (catalog substitutions, mode pick, ambiguous goal).
- The agent surfaces a confirm-question that a real user would answer in one sentence.
- The agent reaches a decision point with multiple acceptable paths and asks which to take.

In all three cases today's runner returns an early-stop transcript and a hypothetical retrospective. We don't get the lived friction we need for atom / handler / recipe-content fixes. Worse, **scaling scenarios will silently amplify this** — more scenarios, more confirm-points, more low-engagement runs, more useless retrospectives.

The fix is to **simulate the user's next turn**. When the agent ends with a question and no further tool work, spawn a separate headless Claude as the user, give it the original goal + persona context + recent conversation, and inject its reply into the agent's session via `claude --resume`. Loop until the agent declares done, the simulated user is satisfied, or a cap fires. Then run the existing retrospective.

One mechanism, one model call type, no Tier-1/Tier-2 split. Persona is a free-form string defaulting to a generic "developer who initiated this task" — overridable per scenario for special cases.

---

## What is NOT in scope

- Multi-stage trails (separate plan candidate; this plan keeps single-stage scenarios but unblocks them).
- Parallelization across multiple `eval-zcp-N` projects (separate plan candidate).
- Structured findings classification (separate; orthogonal to user-sim).
- Recipe-coverage matrix expansion past the existing 9 scenarios (separate; user-sim must work first).
- Replacing or removing the existing retrospective two-shot. User-sim runs **before** retrospective; retrospective stays as-is.
- LLM-as-classifier for "is agent waiting?". Heuristic-first; classifier as fallback only if heuristic accuracy proves insufficient.

---

## Architecture sketch

```
┌────────────────────────────────────────────────────────────────────┐
│  RunBehavioralScenario (existing entrypoint)                       │
│    1. seed → init → preseed   (existing, unchanged)                │
│    2. spawnClaudeFresh(scenario.InitialPrompt) → captures session  │
│    3. ┌── NEW: user-sim loop ──────────────────────────────┐      │
│       │  loop while iteration < maxUserSimTurns:           │      │
│       │    state = classifyTranscriptTail(transcript)      │      │
│       │    switch state:                                   │      │
│       │      done       → break                            │      │
│       │      waiting    → reply = spawnUserSim(persona,    │      │
│       │                          recentConvo, lastAgentMsg)│      │
│       │                   if reply.satisfied → break       │      │
│       │                   if isLoopRepeat(prev, current)   │      │
│       │                          → mark stuck, break       │      │
│       │                   spawnClaudeResume(session, reply)│      │
│       │      error      → break                            │      │
│       │      maxTurns   → break                            │      │
│       └────────────────────────────────────────────────────┘      │
│    4. spawnClaudeResume(session, retrospectivePrompt)  (existing) │
│    5. extract self-review + write meta.json   (existing + new     │
│                                                userSim fields)    │
│    6. CleanupProject  (existing, unchanged)                       │
└────────────────────────────────────────────────────────────────────┘
```

User-sim is a **separate** `claude` process invocation per turn, not a sub-agent. No persistence (`--no-session-persistence`), no MCP config, no project work-dir context — pure stateless single-turn LLM call with a hand-built prompt.

---

## Detection rules — classifier

Goal: from the tail of the agent's stream-json transcript, decide which of `done` / `waiting` / `error` / `maxTurns` / `working` applies.

**Rules table** (evaluated in order, first match wins):

| Order | Signal in transcript tail | Verdict |
|------:|---|---|
| 1 | Last `result` event has `is_error: true` | `error` |
| 2 | Last `result` event has `subtype == "error_max_turns"` | `maxTurns` |
| 3 | Last `result.result` text matches `/(deployed|ready|set up|complete|✓|verified|live)/i` AND last assistant turn contains zero `?` characters | `done` |
| 4 | Last `tool_use` is `zerops_verify` AND last `tool_result` indicates success (`"status":"healthy"` or HTTP 2xx in result) | `done` |
| 5 | `stop_reason == "end_turn"` AND last assistant content is text-only (no `tool_use`) AND text matches one of: `?` in last 200 chars, `/\b(should I|do you want|would you prefer|let me know|please confirm|shall I|which would you like|either is fine)\b/i`, or contains `option` + `?` within 100 chars of each other | `waiting` |
| 6 | `stop_reason == "end_turn"` AND last content is text-only AND no waiting markers AND no done markers | `waiting` (conservative — text-only end usually means agent is awaiting input) |
| 7 | Otherwise (mid-tool roundtrip, no terminal event yet) | `working` (NOT a state we act on — main loop should not be entered until run terminates) |

**Why rule 6 errs toward `waiting`**: false-positive (user-sim sends an unnecessary "go ahead") is cheap — agent ignores it and continues. False-negative (we treat a question as `done`) terminates the stage prematurely with a hypothetical retrospective — that's the bug we're fixing.

**Edge cases**:
- Agent ends with assistant text + immediately follows with another assistant text (rare, multi-message turn): take the **last** message in the turn as the signal.
- Agent ends with a markdown bullet list of options + question mark in the last bullet: rule 5 matches via the `?`. Reply will need to pick one — user-sim does that naturally.
- Agent ends with `"I'll deploy now."` then `tool_use: zerops_deploy` → stream continues. Classifier never sees `end_turn` until tool roundtrip completes. Rule 7 (`working`) keeps loop dormant.

The classifier is **deterministic and pure** — given a transcript file, returns a verdict. Unit-tested against canned transcripts in `usersim_test.go`.

---

## User-sim prompt structure

Each user-sim invocation is a fresh `claude -p` headless call with no persistence and no MCP. Model: Haiku 4.5 by default (`claude-haiku-4-5-20251001`); override per scenario via `userSim.model:` (e.g. `sonnet`).

**Prompt body** (assembled in Go, single string passed via `-p`):

```
You are simulating a real user in a chat with a coding agent. The user
originally said:

  "{stage.InitialPrompt}"

Your persona: {scenario.UserPersona OR default persona}

The agent has been working on the task and has now turned to ask you
something. Reply as the user would:

- Brief (1-3 sentences max). Real users don't write essays.
- Don't pretend to be helpful or knowledgeable about the platform.
- If the agent suggests a substitution with a clear reason, accept it
  and ask them to mention it in the final summary.
- If the agent's question is unclear, say so plainly.
- If the agent asks permission for something you didn't ask for and
  it's not necessary, push back briefly.
- If the agent's question shows they've finished the work and just
  want a confirmation, say "thanks, looks good" or "that's all I
  needed" — these phrases signal task completion to the runner.
- Never ask the agent for code, configuration, or platform details.
  You're the user, not a developer pair.

Recent conversation (last 3 turns of agent text, oldest first):

[agent, turn N-2]: {summary or full text, capped at 800 chars}
[you, turn N-1]: {previous user message — if first user-sim turn,
                  this is "I want: {stage.InitialPrompt}"}
[agent, turn N — most recent]: {full last assistant text}

Reply now.
```

**Default persona** (string literal in `usersim.go`):

```
You are a developer who initiated this task. You want it done with sensible
defaults. Compatible substitutions are fine if the agent explains them.
You'll only push back if something contradicts your stated goal. You don't
know the Zerops platform's internal details, but you know your own goal.
```

**Per-scenario override** in scenario YAML:

```yaml
userPersona: |
  You are a senior dev with a strict policy: no redeploy unless a code
  change is involved. Subdomain-access toggling is infrastructure, not a
  deploy-class change. Push back on any redeploy suggestion that isn't
  triggered by a real source change.
```

**Recent conversation window**: last 3 agent text turns + last 1-2 user-sim replies, capped at 800 characters per agent turn. Full last assistant message (no cap) so user-sim sees exactly what was asked. This keeps prompt size bounded (~2-3 KB) and avoids halucinated detail recall from earlier turns.

**Output extraction**: user-sim's reply is the concatenation of all `assistant.text` content blocks in its single-turn stream-json output. Trim whitespace. No JSON parsing — pure text.

**Satisfaction signal**: parse the reply for the literal substrings `"thanks, looks good"`, `"that's all I needed"`, `"all set"`, `"perfect, done"`. Match → `satisfied: true` and runner breaks the user-sim loop. Other replies → continue the loop.

---

## Loop termination conditions

Aspoň jedna z těchto musí být true pro stage to terminate (in priority order):

1. **Agent declared done** — classifier verdict `done`.
2. **User-sim satisfied** — reply contains satisfaction marker.
3. **Loop detected** — same agent question hash twice in a row (Levenshtein < 30 chars on last 200 chars of last agent text). `meta.stuckOnQuestion` populated; `terminatedBy: "stuck_loop"`.
4. **Max user-sim turns** — cap of 10 user-sim invocations per stage. `terminatedBy: "max_iterations"`.
5. **Stage timeout** — wall-time budget per stage (default 15 min, override via scenario `userSim.stageTimeout:`). `terminatedBy: "timeout"`.
6. **Fatal error** — classifier verdict `error` or agent process exit non-zero. `terminatedBy: "agent_error"`.

After loop terminates, retrospective fires unconditionally (existing behavior — same contract as today). Termination reason is recorded in `meta.json` for downstream interpretation.

---

## Logging shape

Extend `BehavioralResult` with user-sim fields. New `meta.json` excerpt:

```json
{
  "scenarioId": "recipe-laravel-minimal-standard",
  "userSim": {
    "personaUsed": "default" | "scenario-override",
    "model": "claude-haiku-4-5-20251001",
    "turns": [
      {
        "iteration": 1,
        "trigger": "MariaDB substitution confirm",
        "agentTextExcerpt": "...the catalog has mariadb@10.6 but no mysql. Should I use MariaDB?",
        "reply": "MariaDB is fine — just mention the substitution in your final summary.",
        "wallTime": "2.1s"
      },
      {
        "iteration": 2,
        "trigger": "mode pick confirm",
        "agentTextExcerpt": "...do you want a single combined service or a dev/stage pair?",
        "reply": "Dev plus staging slot, like I said.",
        "wallTime": "1.8s"
      }
    ],
    "terminatedBy": "agent_declared_done",
    "stuckOnQuestion": null,
    "totalUserSimWallTime": "3.9s"
  }
}
```

This is the **single most important debugging surface**: when reading a self-review later, the user-sim turn log tells you exactly which questions the agent asked and how the simulated user answered. If `terminatedBy: "stuck_loop"`, the agent didn't understand the user's reply — that's a finding for `[atom-knowledge]` or `[handler-or-tool]`.

---

## Phases

### Phase 1 — Scenario schema + classifier (no live runs)

**Goal**: scenario YAML accepts `userPersona:` and `userSim:` fields; classifier deterministically maps canned transcripts to verdicts.

**Files**:
- `internal/eval/scenario.go` (+~30 LOC): `Scenario.UserPersona string`, `Scenario.UserSim *UserSimConfig`, `UserSimConfig{Model string, MaxTurns int, StageTimeout Duration}`.
- `internal/eval/usersim.go` (NEW, ~140 LOC): `ClassifyTranscriptTail(file string) (Verdict, error)`, the rule table from the Detection Rules section, helper to load last N events.
- `internal/eval/usersim_test.go` (NEW, ~150 LOC): table-driven tests against 6 canned transcript fixtures (one per verdict) under `internal/eval/testdata/usersim/*.jsonl`.

**Steps**:

1. `RED` (`internal/eval/usersim_test.go`) — table-driven test `TestClassifyTranscriptTail` with six cases: `done_via_verify`, `done_via_text`, `waiting_question_mark`, `waiting_modal_phrase`, `error_max_turns`, `error_is_error`. Each case loads a `.jsonl` fixture and asserts the verdict. Tests fail because `ClassifyTranscriptTail` doesn't exist. Commit RED with `internal/eval/testdata/usersim/*.jsonl` hand-authored to match each verdict shape.

2. `GREEN` (`internal/eval/usersim.go`) — implement `ClassifyTranscriptTail` exactly per rules table. Pure function. No I/O beyond reading the file. Commit GREEN.

3. `RED` (`internal/eval/scenario_test.go`) — extend existing scenario parser test with a fixture using `userPersona:` and `userSim:` sub-block. Assert parsed values. Tests fail because fields don't exist on Scenario.

4. `GREEN` (`internal/eval/scenario.go`) — add fields, parse them in `splitFrontmatter`'s yaml unmarshal step. Commit GREEN.

5. `(audit)` Run `go test ./internal/eval/... -run "TestClassify|TestParseScenario" -v`. Output goes to `eval/behavioral/audits/usersim-phase1-test-output.md`. Verify all six classifier cases + new scenario parsing pass green.

**Pause point**: classifier table validates against canned transcripts. No live run yet. Confirm with user that detection rules feel right before wiring the loop.

### Phase 2 — User-sim spawn + main loop wiring (no live runs)

**Goal**: `RunBehavioralScenario` runs the user-sim loop between stage spawn and retrospective. Mockable user-sim via interface so we can unit-test the loop without real `claude` calls.

**Files**:
- `internal/eval/usersim.go` (extend, +~120 LOC): `UserSimRunner interface { Reply(ctx, prompt string) (string, error) }`, default `claudeUserSimRunner` impl that exec's `claude -p ... --no-session-persistence --max-turns 1 --model haiku`. Helper `BuildUserSimPrompt(persona, initialPrompt, lastAssistantMsg, recentConvo []string) string`.
- `internal/eval/behavioral_run.go` (modify, +~60 LOC): insert user-sim loop between line ~130 (after `spawnClaudeFresh`) and line ~142 (`extractSessionID`). Loop uses `ClassifyTranscriptTail` after each `spawnClaudeResume`. Result struct extended with `UserSimTurns []UserSimTurn`, `UserSimTerminatedBy string`, `StuckOnQuestion string`.
- `internal/eval/behavioral_run_test.go` (extend, +~150 LOC): unit-test loop with stub `UserSimRunner` returning canned replies. Stub `spawnClaudeResume` via injection (refactor to take a function pointer for testability).

**Steps**:

1. `RED` — add `TestRunUserSimLoop_*` cases: `terminates_on_done`, `terminates_on_satisfaction_marker`, `terminates_on_max_iterations`, `terminates_on_stuck_loop`, `injects_reply_correctly`. Stub the resume call to return pre-canned next transcript states. Tests fail because loop doesn't exist.

2. `GREEN` — implement `runUserSimLoop` helper called from `RunBehavioralScenario`. Pseudocode:

   ```go
   func (r *Runner) runUserSimLoop(ctx context.Context, sc *Scenario, sessionID, transcriptFile string, simRunner UserSimRunner, result *BehavioralResult) error {
       maxTurns := defaultMaxUserSimTurns
       if sc.UserSim != nil && sc.UserSim.MaxTurns > 0 {
           maxTurns = sc.UserSim.MaxTurns
       }
       persona := sc.UserPersona
       if persona == "" {
           persona = defaultPersona
       }
       var prevAgentTail string
       for i := 0; i < maxTurns; i++ {
           verdict, err := ClassifyTranscriptTail(transcriptFile)
           if err != nil { return err }
           switch verdict.Kind {
           case VerdictDone:
               result.UserSimTerminatedBy = "agent_declared_done"
               return nil
           case VerdictError, VerdictMaxTurns:
               result.UserSimTerminatedBy = "agent_" + verdict.Kind.String()
               return nil
           case VerdictWaiting:
               if isLoopRepeat(prevAgentTail, verdict.LastAssistantText) {
                   result.UserSimTerminatedBy = "stuck_loop"
                   result.StuckOnQuestion = verdict.LastAssistantText
                   return nil
               }
               recent := tailWindow(transcriptFile, 3)
               prompt := BuildUserSimPrompt(persona, sc.Prompt, verdict.LastAssistantText, recent)
               start := time.Now()
               reply, err := simRunner.Reply(ctx, prompt)
               if err != nil { return fmt.Errorf("user-sim: %w", err) }
               turn := UserSimTurn{Iteration: i + 1, Reply: reply, AgentTextExcerpt: trunc(verdict.LastAssistantText, 200), WallTime: Duration(time.Since(start))}
               result.UserSimTurns = append(result.UserSimTurns, turn)
               if isSatisfied(reply) {
                   result.UserSimTerminatedBy = "user_sim_satisfied"
                   return nil
               }
               if err := r.spawnClaudeResume(ctx, sessionID, reply, transcriptFile); err != nil {
                   return fmt.Errorf("resume after user-sim: %w", err)
               }
               prevAgentTail = verdict.LastAssistantText
           }
       }
       result.UserSimTerminatedBy = "max_iterations"
       return nil
   }
   ```

   Note: `spawnClaudeResume` currently writes to a separate `retroFile`. Refactor minimally so it can append to the existing `transcriptFile` for the user-sim loop, then write retrospective to its own file. The `--resume` mechanism appends to the same session regardless; only the local log file path changes.

3. `GREEN` — wire `runUserSimLoop` into `RunBehavioralScenario` between scenario spawn and `extractSessionID` → retrospective spawn block. Adjust `extractSessionID` ordering: capture session_id immediately after first spawn (before user-sim loop), reuse for both user-sim resumes and final retrospective.

4. `(audit)` `make lint-local` + `go test ./internal/eval/... -race`. Output to `eval/behavioral/audits/usersim-phase2-test-output.md`.

**Pause point**: loop logic green in unit tests with stubbed simulator. No live run yet.

### Phase 3 — Pilot live-run on Laravel scenario

**Goal**: `recipe-laravel-minimal-standard` runs end-to-end with user-sim, deploys successfully (or fails with substantive friction), produces a self-review reflecting lived experience, and `meta.json.userSim.turns` shows the catalog substitution Q&A captured.

**Files**:
- `eval/behavioral/scenarios/recipe-laravel-minimal-standard.md` (edit, no schema break): add `userPersona:` block describing a developer who's OK with sensible substitutions. Keep `prompt:` body unchanged.
- `eval/behavioral/audits/usersim-pilot-laravel.md` (NEW): committed audit of the pilot run.

**Steps**:

1. `(doc)` Edit `recipe-laravel-minimal-standard.md` to add:

   ```yaml
   userPersona: |
     You are a developer who wants a Laravel app on Zerops with dev + staging.
     Compatible catalog substitutions (MariaDB for MySQL, Valkey/KeyDB for Redis)
     are fine — accept them and ask the agent to mention what was substituted in
     the final summary. You don't know the Zerops catalog internals; trust the
     agent's recommendation when it has a clear reason. Push back only if the
     agent suggests something outside your stated goal (e.g. proposing HA tier
     when you didn't ask for it).
   ```

2. `(operational)` Run `./eval/behavioral/flow-eval.sh recipe-laravel-minimal-standard` from a clean local Claude session (per `CLAUDE.local.md` — `eval-zcp` is the project, `zcp` container hosts the runner). Wait for completion (notification on background bash).

3. `(audit)` Read `runs/<suiteId>/recipe-laravel-minimal-standard/meta.json` + `self-review.md` + `transcript.jsonl`. Write `eval/behavioral/audits/usersim-pilot-laravel.md` with:
   - Total wall-time vs prior 27 s short-circuit run
   - `userSim.turns` count + each turn's trigger + reply
   - `terminatedBy` value
   - Whether final agent state is `deployed` (subdomain reachable) or stuck somewhere downstream
   - Three lifted excerpts from `self-review.md` showing whether retrospective is **lived** vs hypothetical
   - One-paragraph verdict: pilot success / failure / partial

4. `(audit)` If pilot run fails (agent stuck despite user-sim, classifier misfires, simulator misunderstands persona): STOP, file finding to user, do not proceed to broader rollout. The fix path is iteration on the rule table or persona spec, not bulk scenario conversion.

5. `(operational, conditional on success)` Re-run two more existing scenarios (`classic-go-simple`, `existing-standard-appdev-only-reminders`) WITHOUT adding `userPersona:` (so they use the default persona). Verify default persona doesn't break scenarios that previously worked. Append delta to the audit file.

**Pause point**: pilot audit committed. User reads audit. Decision on broader rollout (all 9 existing scenarios) is a follow-up plan, not part of this one.

---

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| Classifier false-positive `done` (agent asked a question, runner thought it was done) | Medium | Rule 6 errs conservatively toward `waiting`. Pilot validates. |
| Classifier false-positive `waiting` (agent mid-thought) | Low | Rule 7 (`working`) keeps loop dormant during tool roundtrips. False-positive `waiting` after agent declares done is cheap — user-sim sends "thanks, all good" → loop terminates. |
| User-sim halucinates user details not in persona | Medium | Persona spec capped recent-window prompt. Haiku at temperature default. If hallucination becomes a pattern, lower temperature or switch to Sonnet override. |
| Loop detection misfires on legitimately-similar follow-up questions | Low-Medium | Levenshtein < 30 on last 200 chars is conservative. `terminatedBy: "stuck_loop"` is logged as a finding for analysis, not silent failure. |
| Cost overrun (user-sim turns × 10 stages × per-call cost) | Low | Haiku ~$0.001/call × 10 max × ~12 scenarios = ~$0.12 per full suite. Budget noise. |
| Wall-time overrun | Low | Each user-sim call ~2-5 s. Cap of 10 turns adds at most ~50 s per stage. Within existing 15-min stage budget. |
| `claude` headless can't be invoked twice in parallel from same machine | Low | User-sim and agent share the host but are sequential per loop iteration. No concurrent `claude` processes. |
| User-sim invokes MCP tools by accident | Medium | User-sim spawn explicitly omits `--mcp-config`. Verified in Phase 2 unit test. |
| Persona-override scenarios break default-persona scenarios | Low | Default persona is a constant string. Override is a different field. No coupling. Verified by Phase 3 step 5 (re-run 2 default-persona scenarios). |
| Compaction during long user-sim loop | Low-Medium | Haiku has small context. If compaction at user-sim level: switch to Sonnet via `userSim.model: sonnet`. Recent-window prompt keeps size bounded so this is unlikely. |

---

## Success criteria

1. **Phase 1**: All six classifier test cases pass. Scenario parser accepts new fields.
2. **Phase 2**: Loop unit tests pass with stubbed simulator. `make lint-local` clean. `go test -race ./internal/eval/...` clean.
3. **Phase 3 pilot**:
   - `meta.json.userSim.turns` non-empty (at least 1 turn captured).
   - Final agent state is "deployed" (transcript shows successful `zerops_deploy` and `zerops_verify`) OR substantively further than 17 events / 27 s of the prior baseline.
   - `self-review.md` references concrete deploy-time friction (build error, schema validation, runtime config), not pre-deploy confirmation friction.
   - Audit file committed under `eval/behavioral/audits/usersim-pilot-laravel.md`.
4. **No regression on default-persona scenarios** — `classic-go-simple` and `existing-standard-appdev-only-reminders` still complete with retrospectives of similar quality to their pre-user-sim baselines (pre-user-sim runs in `runs/20260504-065807/`). Acceptable variation: minor wall-time changes; user-sim turn count may be 0-2 (these scenarios may not trigger waiting state at all).

---

## After this plan

Out of scope, queued as separate plans (do not start until this one closes):

- **Multi-stage trail support** — `Stage []Stage` on Scenario, per-stage retrospective. Re-uses the user-sim loop unchanged.
- **Bulk userPersona conversion** — adding personas to all 9 existing scenarios.
- **Recipe-coverage matrix expansion** — new trails covering frontend SSR / static / multi-runtime / export-workflow.
- **Findings classification surface** — `[recipe-content]` / `[atom-knowledge]` / `[handler-or-tool]` / `[platform-vocab]` / `[framework-specific]` tags in retrospective prompt.
- **Engagement quality gate** — `engagement: low` flag in `meta.json` based on turn count + tool-call counts (orthogonal to user-sim; complements it for the residual cases where user-sim doesn't fix engagement).
- **Multi-project parallelization** — `eval-zcp-1..N` for true horizontal scaling. Only worth pursuing once the sequential trail throughput hits a real ceiling.

These are explicitly deferred. The user-sim mechanism unblocks them — without it, multi-stage trails would amplify the same low-engagement problem this plan fixes.
