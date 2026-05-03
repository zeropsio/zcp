# Phase 2 live smoke run

## Run metadata

| Field | Value |
|---|---|
| Run timestamp | 2026-05-03 11:42:49 UTC |
| Suite ID | `20260503-114249` |
| Scenario | `greenfield-node-postgres-dev-stage` |
| Binary hash (linux-amd64) | `3b35493c7326` |
| Binary version | `v9.51.0-5-g7a514121-dirty` |
| Model | `claude-opus-4-6[1m]` (alias on container; `claude-opus-4-7` from frontmatter) |
| Total wall time | 7m43s |
| Scenario call | 7m0s |
| Retrospective call | 42s |
| Session ID | `bdbdd40b-85cb-4870-8f46-1682cf4f4c00` |
| `compactedDuringResume` | `false` |
| Final exit | 0 |

## Artifact shape verification

All six expected files present under `eval/behavioral/runs/20260503-114249/greenfield-node-postgres-dev-stage/`:

- `task-prompt.txt` — present, the verbatim user prompt (no eval awareness)
- `retrospective-prompt.txt` — present, the briefing-future-agent prompt
- `transcript.jsonl` — present, full stream-json from call 1 (scenario)
- `retrospective.jsonl` — present, stream-json from call 2 (resume)
- `self-review.md` — present, ~30 lines of plain-language friction notes
- `meta.json` — validates against expected schema; all fields populated

## Orchestrator-level verification (no verdict on retrospective content)

- ✅ Cleanup ran pre-run (init.Run wiped `.vscode`, `CLAUDE.md`)
- ✅ build-deploy.sh shipped binary with hash match
- ✅ scenario file scp'd to `zcp:/tmp/zcp-behavioral-scenarios/`
- ✅ ssh + claude headless call 1 ran to completion (7m0s, well under 80-turn cap)
- ✅ session_id captured from first stream-json system event
- ✅ ssh + claude `--resume <session_id>` second call succeeded in 42s, max-turns=3
- ✅ self-review.md non-empty + non-trivial (~3KB, 6 distinct paragraphs)
- ✅ meta.json validates with `compactedDuringResume: false`
- ✅ Post-scenario CleanupProject ran (deleted 3 services that the agent created)
- ✅ scp pulled `<suiteId>/` back to `eval/behavioral/runs/`

## Self-review content (verbatim, surfaced for downstream conversation)

The agent's retrospective surfaced six distinct friction points. One is a known
trap (Trap-2, dev-mode 502 messaging). One is a known trap class but a different
specific bug (Trap-1's siblings on classic route, not recipe route — agent chose
classic, so the original recipe-route plan-retry surface was not exercised).
Four are NEW frictions not previously catalogued:

1. **classic-route bootstrap surface mismatch** — `action="start" route="classic"` does
   NOT accept `plan`; agent bundled them and hit `INVALID_PARAMETER: plan is not
   accepted in action=start`. The error is clear; the natural assumption is wrong.
   (Different bug class than recipe-route Trap-1; that surface was not exercised
   because agent picked classic over recipe.)

2. **npm ci on first build with no lockfile** — fails because `package-lock.json`
   doesn't exist yet. Recovery is "ssh in and `npm install`", not switching to
   `npm install` in buildCommands. Develop guidance does not mention this.
   NEW finding.

3. **zerops.yaml schema confusion** — `deploy:` and `run:` are siblings under each
   setup entry, not `deploy:` nested under `run:`. Agent caught it pre-deploy;
   future agent who doesn't catch it would get YAML validation error pointing at
   wrong line. NEW finding.

4. **dev-mode 502 messaging** — *"subdomain not HTTP-ready: 502" warning right after
   a successful first deploy to a dev-mode service is expected and harmless — dev
   runtimes start with `zsc noop --silent` so nothing is listening yet. The deploy
   reports SUCCESS; the warning is just the platform's HTTP probe noticing nothing
   answers on the subdomain. The fix is to start the dev server via
   `zerops_dev_server`, not to debug the 502. Worth flagging because the warning
   text reads like a deploy problem.* — **Trap-2 surfaced** in agent's own words,
   with the recommendation that the warning text is misleading (different framing
   than original analysis but same root cause).

5. **cross-deploy `setup` parameter naming confusion** — `setup=prod` names the
   zerops.yaml block, not the source environment. Tool description says this but
   easy to misread. NEW finding.

6. **knowledge fetcher namespace gap** — `recipe=` doesn't accept guide URIs even
   though search returns them; no obvious way to pull a guide body except
   re-querying with more keywords. NEW finding.

## Decisions (deferred to live-session conversation)

- Whether to **adjust the scenario prompt** to push agent toward recipe route
  explicitly (would exercise original Trap-1 surface). Trade-off: more constrained
  prompt = less realistic.
- Whether to **separately backlog or fix** each NEW friction. Triage in next
  conversation.
- Whether the dev-mode 502 messaging fix is **atom-level** (rephrase the warning
  source atom) or **deploy-handler-level** (suppress / rephrase the warning when
  the runtime is dev-mode dynamic). Discussion needed.

## Conclusion

Two-shot resume orchestrator works end-to-end. Self-review extraction yields
substantive, lived-experience friction notes — clearly post-hoc retrospective,
not curated review-paper tone. Compaction did not trigger on this 70-event
scenario in Opus 4.6[1m]. Wall time and cost are within budget (7m43s, well
under the 14-17 min estimate).

POC closed. Subsequent conversation drives interpretation + fix prioritization.
