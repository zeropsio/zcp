# Substep: browser-walk-dev (showcase)

This substep completes when every UI-surface feature in `plan.Features` is exercised in a real browser against the dev subdomain and the cross-deploy stage subdomain, with every per-feature pass criterion satisfied on both walks. It runs only for showcase-tier recipes; minimal recipes skip it.

curl proves the server responded. It does not prove the user sees what they should see. A showcase dashboard is a user-facing deliverable — a JS error, a broken fetch URL, a missing import, or a CORS failure makes curl return 200 while the dashboard renders blank. This substep is the only gate that catches those classes.

## Why `zerops_browser` is the only tool for this substep

The `zerops_browser` MCP tool wraps `[open url] + your commands + [errors] + [console] + [close]` as one batch and serializes all calls through a process-wide mutex. It auto-runs recovery on fork exhaustion and guarantees browser close even when a batch exits abnormally. Use it for every browser call. Raw `agent-browser` CLI invocations (or `echo ... | agent-browser batch` in Bash) are the wrong path — they leak Chrome processes when a batch does not close cleanly and race the persistent daemon when two fire in parallel.

## Non-negotiable sequencing

1. **Dev walk runs while dev processes are running.** The dev subdomain serves what the dev processes started in the `start-processes` substep serve — the walk verifies the live dev server. Run the dev walk first, then kill dev processes, then run the stage walk.
2. **One `zerops_browser` call per subdomain.** Pass the URL plus the inner commands array; the tool wraps open/errors/console/close. Passing multiple URLs or multiple open/close markers in one call is the wrong shape.
3. **One browser call at a time across agents.** When this substep is delegated to a verification sub-agent, you issue no browser calls while that sub-agent runs. The sub-agent tasked with close-step review (a separate substep) has no browser tool in its allow-list at all.
4. **On `forkRecoveryAttempted: true` in a stage walk** — a dev process was still running and consumed the fork budget. Before retrying, inspect `ssh {devHostname} "ps -ef | grep -E 'nest|vite|node dist|ts-node'"`, kill the leaked tree, then retry. On the dev walk, the usual cause is lingering subprocess trees from an earlier feature sub-agent — run manual recovery (`pkill -9 -f "agent-browser-"`, wait 1–2s for reaping) and retry once.

## Phase 1 — dev walk

The dev subdomain is live; every UI-surface feature is exercised here. Build the commands array by iterating `plan.Features` — every feature where `surface` contains `"ui"` gets exercised. The walk is feature-derived, not template-copied.

Per-feature sequence:

1. Locate the feature's section: `["get", "count", "[data-feature=\"{F.uiTestId}\"]"]` — must equal 1.
2. Observe initial state.
3. Perform the `Interaction` — `fill`, `click`, `find`+`click` by role or text, `type` — whatever the feature's interaction string declares.
4. Assert `MustObserve` — via `get text`, `get count`, or `is visible` against the selector the feature declares.
5. Capture the error banner: `["get", "text", "[data-feature=\"{F.uiTestId}\"] [data-error]"]` — must equal the empty string.

Example for a feature `{id: "items-crud", uiTestId: "items-crud", interaction: "Fill title, click Submit, row count +1", mustObserve: "[data-feature=\"items-crud\"] [data-row] count increases by 1"}`:

```
zerops_browser(
  url: "https://{appdev-subdomain}.prg1.zerops.app",
  commands: [
    ["snapshot", "-i", "-c"],
    ["get", "count", "[data-feature=\"items-crud\"]"],
    ["get", "count", "[data-feature=\"items-crud\"] [data-row]"],
    ["fill", "[data-feature=\"items-crud\"] input[name=\"title\"]", "browser walk test row"],
    ["find", "role", "button", "Submit", "click"],
    ["wait", "500"],
    ["get", "count", "[data-feature=\"items-crud\"] [data-row]"],
    ["get", "text", "[data-feature=\"items-crud\"] [data-error]"]
  ]
)
```

One `zerops_browser` call per URL. If the walk spans multiple URLs (rare — dual-runtime with separate frontend SPA routes), serialize calls; never batch multiple URLs.

If the dev walk returns a 502 or connection failure, the dev processes are not running. Diagnose via `ssh {devHostname} "ps -ef | grep -E 'nest|vite|node|ts-node'"` and restart through `start-processes` before continuing.

## Phase 2 — kill dev processes

After the dev walk passes, free the fork budget before the stage walk. API-first recipes: both apidev and appdev. Single-runtime: just appdev.

```
ssh apidev "pkill -f 'nest start' || true; pkill -f 'ts-node' || true; pkill -f 'node dist/worker' || true"
ssh appdev "pkill -f 'vite' || true; pkill -f 'npm run dev' || true"
```

## Phase 3 — stage walk

Stage containers run their own processes and are unaffected by the Phase 2 kill. Re-generate the commands array from the same `plan.Features` iteration — identical feature coverage, different URL (`https://{appstage-subdomain}.prg1.zerops.app`).

## Per-feature pass criteria

Every one of the criteria below must hold for every feature on both walks. A walk passes only when every feature passes.

1. **Section located** — `[data-feature="{uiTestId}"]` count equals 1. Zero indicates the scaffold did not emit the test hook; multiple indicates an ambiguous selector.
2. **MustObserve satisfied** — the state change the feature declared is visible. If `MustObserve` names a count increase, after-count is strictly greater than before-count. If it names a text pattern, the element's text matches. "Zero hits" or "empty state" is a failure unless the feature's `MustObserve` string explicitly permits it.
3. **`[data-error]` text is empty** — the error banner the scaffold's observable-failure rule requires must be empty after the interaction. Non-empty means the feature's fetch or logic raised a user-visible error.
4. **No JS runtime error in `consoleOutput`** — the auto-appended `["console"]` output contains no `Uncaught`, `TypeError`, `SyntaxError`, or `Unexpected token '<'` (the last signals a `res.json()` parsed HTML — the SPA fallback class from the dual-runtime URL pattern).
5. **No network failure in `errorsOutput`** — no `net::ERR_*`, no failed-request lines targeting the feature's API path.
6. **`forkRecoveryAttempted` is false** — recovery firing means orphaned processes are leaking. See rule 4 in the non-negotiable sequencing list.

If any criterion fails for any feature, the walk fails. Fix on the mount, redeploy via `snapshot-dev`, restart processes via `start-processes`, re-verify via `verify-dev` and `feature-sweep-dev`, then re-run both walks. This counts toward the 3-iteration budget.

## Command vocabulary (inside the `commands` array)

Use these dedicated commands rather than `eval`. Each one produces structured output designed for agents.

| Need | Command | Notes |
|---|---|---|
| Interactive element tree with clickable refs | `["snapshot", "-i", "-c"]` | `-i` = interactive only, `-c` = compact. Yields `@e1`, `@e2` refs usable in `click`, `fill`, `get`. |
| Text content of an element | `["get", "text", "<sel>"]` | Or `["get","text","@e3"]` with a ref. |
| Element count | `["get", "count", "<sel>"]` | e.g. row count in a table. |
| Is something visible / enabled / checked | `["is", "visible", "<sel>"]` | Plus `is enabled`, `is checked`. |
| Find by semantic locator | `["find", "role", "button", "Submit", "click"]` | Locators: `role`, `text`, `label`, `placeholder`, `testid`. |
| Click / fill / type | `["click", "@e1"]`, `["fill", "@e2", "text"]`, `["type", "<sel>", "text"]` | Refs from snapshot. |
| Wait for element or milliseconds | `["wait", "<sel>"]` or `["wait", "500"]` | Integer = ms. |
| Capture network traffic | `["network", "har", "start"]` … interact … `["network", "har", "stop", "./net.har"]` | Full HAR. |

`["open", ...]` and `["close"]` inside `commands` are stripped by the tool — it adds its own wrappers. `["errors"]` and `["console"]` are auto-appended; you may add extra `["errors","--clear"]` calls mid-walk to checkpoint.

## Report shape per subdomain

- Per feature: ID, before-state, interaction performed, after-state, MustObserve PASS/FAIL, error banner text (expected empty).
- `errorsOutput` (expected: empty).
- `consoleOutput` (expected: empty or benign info only).
- `forkRecoveryAttempted` (expected: false).

Advance past this substep only when both the appdev walk and the appstage walk show every feature PASS, empty errors, and no console noise.

Worker-only features (`surface` contains `worker` without `ui`) are verified by a post-interaction check on the result-element selector their `MustObserve` declares — typically a DOM node a polling frontend consumer populates. Api-only features were verified in `feature-sweep-dev` / `feature-sweep-stage`. Every feature is exercised exactly once at the surface its declaration names.
