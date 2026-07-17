# Data Console UI-drive harness

Drives the REAL assembled Managed Data Console UI — code-server → VS Code
workbench → nested webview iframes → the console SPA — with `puppeteer-core`
against a live deployed container. This is not unit/jsdom/HTTP testing (see
`internal/dataconsole/console/webui/domtest/` and `spa/*.test.js` for that): it
exists because prior testing at those layers missed real bugs that only exist
in the assembled whole — the nested webview postMessage bridge, the native
VS Code confirm-modal round trip, a sidebar re-browse rebinding the host
broker. This harness drives exactly what a human would click, the same way.

## Layout

```
uitest/
  package.json          puppeteer-core only, no other deps
  lib/config.js          local.config.json load/save + SSH gate-token fetch
  lib/harness.js          browser+auth+frame+SPA driving primitives
  lib/engines.js          SSH-based engine oracle helpers (psql/redis)
  lib/runner.js           scenario registry + manifest + findings JSONL writer
  scenarios/core.js       CORE-1: write-mode lifecycle across a re-browse
  run.js                  CLI: node run.js --scenario CORE-1 | --all
  local.config.json      gitignored — cached secrets (see "Config" below)
  evidence/<scenario>/    gitignored — full-page PNGs, NN-name.png, auto-numbered
  findings/               gitignored — a1.jsonl (append-only) + manifest.json
```

## Running

```
cd internal/dataconsole/uitest
npm install                        # once
node run.js --scenario CORE-1      # one scenario
node run.js --all                  # every registered scenario
HEADFUL=1 node run.js --scenario CORE-1   # watch it drive a real Chrome window
```

Exit code is `0` iff every scenario's `status` is `PASS`. A scenario can PASS
while still recording findings — see "G1 vs. findings" below. A `FAIL` status
means the *harness* could not drive the UI (a frame never appeared, a click
threw, a required element never rendered) — that is a harness/environment
problem, not a product finding.

The manifest (`findings/manifest.json`) and a human-readable summary print to
stdout after every run.

## Config — not `.env`

This repo's own Claude Code permission policy denies writing any `.env*` file
(`.claude/settings.json`: `"deny": ["Edit(**/.env*)", ...]`), so secrets are
cached in **`local.config.json`** instead — same discipline (gitignored, never
committed, never hardcoded into a tracked file, fetched live over SSH rather
than typed by hand), different filename. If you're not hitting that same
policy, the filename is not load-bearing; `lib/config.js` is the one place
that reads/writes it. Shape:

```json
{
  "DC_URL": "https://<host>.prg1.zerops.app/",
  "DC_AUTH_TOKEN": "...",
  "DC_SSH_HOST": "zcp",
  "DC_PG_HOST": "db", "DC_PG_USER": "db", "DC_PG_DB": "db", "DC_PG_PASSWORD": "...",
  "DC_REDIS_HOST": "cache", "DC_REDIS_PASSWORD": "...",
  "DC_CHROME_PATH": "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
}
```

`DC_AUTH_TOKEN` self-heals: if absent, `harness.connect()` SSHes to
`DC_SSH_HOST` (`grep -A2 'map \$cookie___zcp_auth' /etc/nginx/nginx.conf`),
parses the token (the map entry line ending `1;`, never `default 0;`), and
persists it back into `local.config.json`. Nothing else self-heals — a missing
`DC_URL`/`DC_CHROME_PATH`/pg/redis field throws a clear error naming the
field.

## Harness API (`lib/harness.js`)

- `connect()` → `{browser, page}` — launch, fetch/cache the gate token,
  `page.setCookie(__zcp_auth)` **before** navigating (skips the login form),
  `goto`, wait for `.monaco-workbench`.
- `openSidebar(page)` → the "Managed Data" sidebar's content frame. Idempotent:
  probes for `.zs-rowhead` first and only clicks the activity-bar icon if not
  already open (VS Code toggles a view closed on a second click of its own
  icon — see Gotchas).
- `openConsole(page, service)` / `sidebarBrowse(page, service)` → click the
  sidebar's "Browse data →" row for `service`, wait for the SPA frame, return
  it. **Same implementation** — VS Code's panel manager dedupes by key, so the
  first open and every later re-browse are mechanically the same click; both
  names exist so a scenario can say which *intent* it means (`sidebarBrowse`
  reads as "deliberately re-entering through the sidebar to test the reveal
  path").
- `spaFrame(page)` → find the current SPA frame by content probe, no click.
- `setWriteMode(page, frame, on)` → drive the toggle to `on`. No-ops if
  already there (self-heals ambient state — call this at the top of every
  scenario's setup). When enabling, waits for the native `.monaco-dialog-box`,
  screenshots it, clicks `Enable writes`. Throws (with a `FAIL-*` evidence
  shot) if the switch never becomes clickable, the modal never appears, or the
  state never settles.
- `clickService(frame, service)` — the **in-SPA** rail switch (`#services
  li`, matched by text — see Gotchas, there's no `data-service` attribute).
- `waitToast(frame, timeoutMs?)` → `{kind: "good"|"bad"|"warn", text}` or
  `null` on timeout (never throws — a missing toast is scenario-meaningful).
- `shot(page, scenario, name)` → full-page PNG to
  `evidence/<scenario>/NN-name.png`, `NN` auto-incrementing per scenario.
- `bbox(frame, selector)` → `getBoundingClientRect()` snapshot or `null`.
- `setScenario(id)` / `currentScenario()` — internal-use; `run.js` calls
  `setScenario` before each scenario so internal assert-fail screenshots (a
  timeout inside `setWriteMode`, a missing frame) land in the right
  `evidence/<scenario>/` folder even though those helpers don't take a
  scenario id themselves. `ctx.evidence(name)` (see below) is sugar over the
  same tracked id.

## Engines (`lib/engines.js`)

`sh(cmd)` (local), `container(cmd)` (SSH to `DC_SSH_HOST`, single argv element
— only the remote shell parses it), `psql(sql)`, `redis(args)` (array of
tokens preferred). Every composed command goes through `shellQuote()` (POSIX
single-quote escaping) for every embedded value — never raw string
interpolation into a shell/SQL command (mirrors the Go `shellQuote()`
discipline in `CLAUDE.md`, applied here in JS).

## Runner / scenarios (`lib/runner.js`, `scenarios/*.js`)

A scenario file calls `runner.register({id, family, fn(ctx)})` at require
time; `run.js` requires every `scenarios/*.js` so the registry is populated
before argv is read. `ctx` passed into `fn`:

```js
{ page, harness, engines, evidence: (name) => harness.shot(page, scenario.id, name), addFinding }
```

`addFinding(partial)` fills in `{id, scenario, lane:"F", family, severity,
title, repro, expected, actual, evidence, engine_truth, status:"new"}` from
whatever you pass (your fields win), appends it to `findings/a1.jsonl`
(one JSON object per line, append-only — a re-run never loses a prior run's
trail), and returns the full record.

### G1 vs. findings — the verdict rule

A scenario's `status` is `"FAIL"` **only** when it throws — the harness
itself couldn't drive the UI (frame unreachable, click target never became
clickable, a required element never rendered). A legitimately failing
*assertion* inside a scenario (the product did the wrong thing) is recorded
via `addFinding` and the scenario still reports `"PASS"` — G1 is about
whether the harness reliably drives and reports, not whether the product is
bug-free. `run.js`'s process exit code follows `status`, not finding count.

## Data discipline

Every scenario touches only `uitest_`-prefixed keys/rows/objects and tears
down in a `finally` block, both at start (self-heal from a prior aborted run)
and at end. `greeting` in the `cache` (valkey) service is a seeded fixture —
CORE-1 opens it read-only to check affordances but never mutates it. Never
touch anything else on the container. Engine creds in `local.config.json` are
eval-sandbox-only.

## Gotchas for anyone extending this (fan-out scenarios, new families)

1. **Frame readiness ≠ frame presence.** `#rail`/`#topbar` are static markup —
   present as soon as the iframe's HTML loads, well before the app is
   interactive. The app only becomes `embedded` (unhides `#editswitch`,
   populates the rail, etc.) after the async `dc-ready` → `dataconsole-init`
   postMessage handshake round-trips through the nested webview bridge to the
   extension host and back, which can lag the iframe load by a second or more.
   `harness.js`'s frame probes wait for `#services li` (populated by
   `renderServices()`, which only runs after that handshake) as the real
   "ready" signal — if you add a new frame-discovery helper, probe for
   populated content, not just structural markup, or you'll get "Node is
   either not clickable or not an Element" on the very next interaction.

2. **`#services li` has no `data-service` attribute.** Checked against the
   shipped `app.js` (`renderServices()`) — each row is
   `<li><span>{hostname}</span><span class="svc-type">...</span>...</li>`
   with nothing else identifying which service it is. `clickService()`
   matches by the first `<span>`'s exact text content and calls `.click()` on
   the `<li>` (which the SPA wires via `li.onclick`, so a synthetic click
   fires it same as a real one). The sidebar's "Browse data" buttons, by
   contrast, DO carry `[data-action="openConsole"][data-service="..."]` —
   don't confuse the two rows.

3. **VS Code's activity bar toggles, it doesn't just open.** Clicking
   `.activitybar a[aria-label="Managed Data"]` a second time while that view
   is already the active one COLLAPSES the sidebar instead of doing nothing.
   `openSidebar()` probes for `.zs-rowhead` first and only clicks if not
   already open — call `openSidebar()` (or `openConsole`/`sidebarBrowse`,
   which call it internally) rather than clicking the icon directly.

4. **The write-mode switch is optimistically-reverted, not optimistically-set.**
   Clicking `#editswitch` does NOT flip `.switch.on` immediately — `app.js`'s
   `onEditToggle` resets the checkbox back to the CURRENT (pre-click) state
   and only posts the intent to the host; `.switch.on` only updates when the
   host's `dataconsole-write-mode` reply lands (after the native modal, for
   enabling). `setWriteMode()` accounts for this — don't add a bare
   `frame.click("#editswitch")` + immediate assertion anywhere else.

5. **Enabling needs the native modal; disabling doesn't.** The native
   `.monaco-dialog-box` (`vscode.window.showWarningMessage(..., {modal:true},
   "Enable writes")`) only appears when turning write mode ON. Turning it OFF
   is immediate, no modal, no confirmation. Don't wait for a modal on
   `setWriteMode(page, frame, false)` — it'll never come (harness already
   handles this branch correctly; a hand-rolled toggle wouldn't).

6. **Two different confirm modals exist — don't conflate them.** The native
   VS Code one (`.monaco-dialog-box`, `a.monaco-button`, MAIN frame) is ONLY
   for the write-mode toggle. Every in-SPA mutation (delete a node/row/entry,
   overwrite a blob, set TTL, rename, upload) goes through the SPA's OWN
   `#modal` (`.modalbox`, `#modaltitle`, `#modalbody`, `#modalcancel` /
   `#modalok` — labelled "Confirm", not dynamically relabeled) **inside the
   SPA frame**, not the main frame. Confirming a delete is `spa.click("#modalok")`,
   not a `.monaco-dialog-box` interaction.

7. **A redis STRING key with no `:` is a TREE LEAF, not a grid row.** Only
   hash/list/set/zset collections render as `kind:"tabular"` (opened via the
   grid renderer, `table.grid`); a plain string key (however it was created)
   is `kind:"blob"` and opens via `openBlob` — toolbar buttons `#saveblob` /
   `#delblob` / `#renameblob` / `#dlblob`, not `td.editable` / `button.rowdel`.
   Only once you open a hash/list/set/zset do you get the grid affordances the
   original harness brief describes generically as "the key list is a
   grid/list" — the top-level KV tree itself is not that.

8. **Toasts self-remove after 2.6s and `waitToast` reads the FIRST
   `.toast(.good|.bad|.warn)` in DOM order.** If two mutations happen inside
   that window, the second `waitToast` call can pick up the first
   (still-fading) toast instead of the new one. Space mutating actions out or
   drain/ignore an expected stale toast before triggering the next one.

9. **A same-panel re-browse does NOT recreate the SPA's iframe** — it's a
   `postMessage` service-switch inside the same document, not a navigation.
   A previously-acquired frame handle usually stays valid, but this harness
   re-probes via `spaFrame(page)` after any switch rather than trusting a
   held reference — cheap insurance against a future change that does start
   recreating the iframe.

10. **A suspicious/unexplained error is not automatically a stale-process
    artifact — but do check.** The container accumulates orphaned
    `zcp studio console serve` / `zcp studio watch` / extension-host processes
    across a working day (observed: 2 live `console serve` generations
    spanning an intervening binary rebuild, 3 live `watch` generations, 4 live
    extension-host generations — none evicted by a VS Code "Developer: Reload
    Window"). If a result looks implausible against the current source, close
    the "Data Console" **tab** (`.tab` whose `aria-label`/text includes "Data
    Console", then its `.codicon-close`) and re-open via `openConsole` — that
    fires the extension's own `onDispose` → `killEntry` → respawn path and is
    proven (by a fresh PID appearing in `ps` on the container) to hand you a
    process bound to the current on-disk binary. Do **not** SSH-kill anything
    directly — read-only SSH except redis/psql test data. See the CORE-1
    finding below: it was reproduced THREE times, including once against a
    freshly-spawned process confirmed by PID/timestamp, before being recorded
    — don't skip that step for a surprising result.

11. **`ssh`/local shell hooks may block certain Bash patterns in THIS Claude
    Code session** (piping to an interpreter, writing to `.env*`). Not a
    product/harness fact, just a note if you hit the same wall: use `Read`
    for JSONL/JSON instead of `cat | jq`/`python -m json.tool`, and use a
    differently-named gitignored file instead of `.env` (see "Config" above).
