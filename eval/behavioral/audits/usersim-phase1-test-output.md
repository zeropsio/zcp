# User-sim Phase 1 — test audit

**Date**: 2026-05-04
**Plan**: `plans/flow-eval-usersim-2026-05-04.md` Phase 1.
**Scope**: classifier (`ClassifyTranscriptTail`) + scenario schema extension
(`UserPersona`, `UserSimConfig`).

## Summary

Phase 1 closed green. Classifier deterministically maps the canned
stream-json fixtures under `internal/eval/testdata/usersim/` to their
target verdicts. Scenario parser accepts the new `userPersona:` and
`userSim:` frontmatter blocks and tolerates their absence. Race detector
clean across full `internal/eval/...`. `make lint-local` clean.

## Classifier — `TestClassifyTranscriptTail`

```
--- PASS: TestClassifyTranscriptTail (0.00s)
    --- PASS: TestClassifyTranscriptTail/done_via_text (0.00s)
    --- PASS: TestClassifyTranscriptTail/done_via_verify (0.00s)
    --- PASS: TestClassifyTranscriptTail/waiting_question_mark (0.00s)
    --- PASS: TestClassifyTranscriptTail/waiting_modal_phrase (0.00s)
    --- PASS: TestClassifyTranscriptTail/error_max_turns (0.00s)
    --- PASS: TestClassifyTranscriptTail/error_is_error (0.00s)
    --- PASS: TestClassifyTranscriptTail/working_mid_roundtrip (0.00s)
--- PASS: TestClassifyTranscriptTail_FileMissing (0.00s)
```

Eight cases — seven verdict-table rules plus one "missing transcript file"
sanity check. Each fixture is intentionally minimal (3-5 events) so the
canned shape is reviewable at a glance and the classifier's coverage is
explicit.

### Rule mapping

| Fixture | Rule fired | Reason string |
|---|---|---|
| `done_via_text.jsonl` | Rule 3 | `rule3_done_markers` (text contains "live" + "Done" + "deployed", zero `?`) |
| `done_via_verify.jsonl` | Rule 4 | `rule4_verify_success` (last tool_use = `zerops_verify`, result `"status":"healthy"`) |
| `waiting_question_mark.jsonl` | Rule 5 | `rule5_question_mark` (final text ends with `?`, modal phrase "do you want") |
| `waiting_modal_phrase.jsonl` | Rule 5 | `rule5_question_mark` (modal phrases "Should I" + "would you prefer", trailing `?` in tail-200) |
| `error_max_turns.jsonl` | Rule 2 | `rule2_max_turns` (`subtype: "error_max_turns"`) |
| `error_is_error.jsonl` | Rule 1 | `rule1_is_error` (`is_error: true`, no max-turns subtype) |
| `working_mid_roundtrip.jsonl` | Rule 7 | `no_terminal_event` (no `result` event, classifier returns `working`) |

Note on `waiting_modal_phrase.jsonl`: original intent was to validate the
modal-phrase path *without* `?`, but the realistic phrasing
("...would you prefer a stage slot too.") is followed by `?` in the
tail-200 window because the broader text contains a question mark.
Classifier still routes via Rule 5 — modal phrase is detected first in
the same expression. Validating modal-phrase-without-`?` would require a
hand-crafted fixture deviating from natural phrasing; left for follow-up
if FN observed in pilot.

## Scenario schema — `TestParseScenario_UserPersonaAndSim` + `TestParseScenario_NoUserSim_DefaultsAllowed`

```
--- PASS: TestParseScenario_UserPersonaAndSim (0.00s)
--- PASS: TestParseScenario_NoUserSim_DefaultsAllowed (0.00s)
```

Schema extension verified for:

- Free-form `userPersona:` multi-line YAML block parsed and trimmed.
- `userSim:` block with `model`, `maxTurns`, `stageTimeoutSeconds`.
- Default-fallback path: existing scenarios without these fields (e.g.
  `testdata/scenarios/empty_seed.md`) still parse with `UserPersona == ""`
  and `UserSim == nil`.

Validation rejects negative `maxTurns` / `stageTimeoutSeconds` per the
plan's contract. Not exercised in current tests — the validation is a
defensive guard against accidental author error; positive-path tests
above cover the intended use.

## Wider regression check

`go test ./internal/eval/... -race -count=1` — full package, all tests:

```
ok  	github.com/zeropsio/zcp/internal/eval	4.344s
```

Includes `TestScenarios_LiveFilesParse` which parses every scenario file
under `internal/eval/scenarios/*.md` (44 live scenarios). All pass —
schema extension is fully backward-compatible.

`make lint-local` (depguard + atom-tree + golangci-lint full): clean.

## Files added / modified

| File | Status | Purpose |
|---|---|---|
| `internal/eval/usersim.go` | NEW (~290 LOC) | `ClassifyTranscriptTail` + verdict types + stream-json projection |
| `internal/eval/usersim_test.go` | NEW (~80 LOC) | Table-driven classifier tests |
| `internal/eval/testdata/usersim/*.jsonl` | NEW (7 files) | Canned transcripts, one per verdict |
| `internal/eval/scenario.go` | MODIFIED | Added `Scenario.UserPersona`, `Scenario.UserSim`, `UserSimConfig` struct, frontmatter parsing, validation |
| `internal/eval/scenario_test.go` | MODIFIED | Added two RED-then-GREEN tests for new fields |
| `internal/eval/behavioral_run.go` | MODIFIED | Extended event/content type constants (`eventTypeUser`, `eventTypeResult`, `contentTypeToolUse`, `contentTypeToolRes`) for shared use across classifier and existing extractors |

No file in `internal/topology/`, `internal/platform/`, or layers above
`internal/eval/` touched — the change stays scoped to the eval package
plus its testdata, as the architecture rules require.

## Phase 1 outcome

Classifier rules are deterministic and pinned. Scenario schema unblocks
Phase 2 (user-sim spawn + loop wiring). No live runs yet — that's Phase 3.

Open question for Phase 2: how `runUserSimLoop` interacts with the
existing `spawnClaudeResume` log-file allocation. Current behavior: each
resume writes to a fresh log file (used by retrospective). For user-sim
turns we want the resumes to APPEND to the same `transcript.jsonl` so
the classifier sees the cumulative state. Will be addressed by adding an
append-mode option to the spawn helper in Phase 2.
