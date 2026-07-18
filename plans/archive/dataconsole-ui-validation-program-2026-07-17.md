# Data Console UI/UX Validation Program — 2026-07-17

Status: ACTIVE. G0 proven (this doc §2), A1 harness in flight.
Owner: Fable (orchestrator) + Sonnet agents (execution).

## 0 Why this program exists

The excellence program verified the console at four layers — Go unit, JS unit,
jsdom DOM, HTTP conformance (11/11 live) — and Karel's ten-minute manual pass
still found three real bugs. All three lived in the one layer never driven
live: the **assembled embedded UI** (real webview → host broker → server →
engine). Emulated layers cannot catch divergence *between* layers (SPA toggle
green while broker read-only), styling of native controls, or layout reflow.

This program adds the missing lane: **scripted driving of the real UI in the
real container**, with independent engine-truth oracles, plus a **UX audit
lane** judging what a human actually sees. It is the verification standard for
the console from now on: a change is done when this suite is green, not when
unit tests pass.

## 1 Method

Every functional scenario asserts **three oracles**:

| Oracle | Question | Channel |
|---|---|---|
| O-UI | Does the UI show the right thing? | DOM assertions + screenshot in the real webview |
| O-ENGINE | Did the engine actually change (or stay unchanged)? | Independent CLI on the container over SSH (`psql`, `redis-cli`, `curl`, `mc`) — never the console's own API |
| O-HONESTY | Does the UI's claim equal engine truth? | success toast ⇒ applied; error ⇒ unchanged; write-off ⇒ refused AND looks off |

Two lanes share one harness:

- **Lane F (functional)** — scripted scenarios per family (§4), pass/fail.
- **Lane X (UX)** — full state-gallery screenshot sweep, then independent
  critic agents scoring against the 10-point rubric (§8). Finds the
  "katastrofa" class: things that work but look or feel broken.

## 2 Access facts (G0 — proven live 2026-07-17)

Every item below was executed, not assumed:

- **Entry**: `https://zcp-8a-8080.prg1.zerops.app/` → nginx token gate (nginx
  managed by zcp) → code-server (`--auth none`) on `:8081`.
- **Auth**: gate checks cookie `__zcp_auth` against a token mapped in
  `/etc/nginx/nginx.conf` on the container (canonical source: the zcp
  service's `VSCODE_PASSWORD` env). Harness reads the token at runtime via SSH
  and sets the cookie via CDP (`page.setCookie`, works for HttpOnly). The
  token value is never committed — cached in gitignored `uitest/.env`.
- **Webviews are SAME-ORIGIN**: served under
  `/stable-<hash>/static/out/vs/workbench/...` on the same host. Structure is
  outer webview iframe → inner iframe → SPA document. Plain
  `frame.contentDocument` traversal works; Node puppeteer `page.frames()` too.
- **Sidebar**: activity bar item `aria-label="Managed Data"`; rows carry
  `[data-action="openConsole"][data-service="<hostname>"]` + "Browse data →".
- **Native write-confirm modal** renders in the MAIN workbench DOM as
  `.monaco-dialog-box` with buttons `Cancel` / `Enable writes` — enumerable and
  clickable. Proven: toggled the switch, saw the modal, clicked Cancel.
- **Console panel** opened via real sidebar click showed the SPA: `#topbar`
  with `.switch` (Write mode), `#rail` with all 11 services, `#tree` with the
  postgres schema.
- **Engine CLIs on container**: `psql`, `redis-cli`, `curl`, `mc` present;
  `clickhouse-client`, `nats`, `kafka-topics` absent (ch oracle via HTTP
  `curl`; stream families are view-only — UI oracle + existing conformance
  suite suffice). mariadb oracle: check `mariadb`/`mysql` client, else install
  or fall back to HTTP-less skip with a logged gap.
- **Local rig**: Node v24.11.1, npm 11, Chrome at
  `/Applications/Google Chrome.app` (use puppeteer-core, executablePath, no
  Chromium download). SSH alias `zcp` reaches the container non-interactively.

## 3 Test matrix (what is deployed on zcp-eval-clean)

| Service | Engine | Family | Tier | O-ENGINE channel |
|---|---|---|---|---|
| db | postgresql:18 | tabular | full | `psql` (user `db`) |
| mariadb | mariadb | tabular | full | `mariadb`/`mysql` client (verify presence) |
| ch | clickhouse | tabular | view-only | `curl http://ch:8123` |
| cache | valkey:7.2 | kv | full | `redis-cli -h cache` |
| storage | object-storage | object | full | `mc` |
| es | elasticsearch:9 | document | full | `curl http://es:9200` |
| docs | typesense:30.2 | document | full | `curl` + api key |
| search | meilisearch | document | full | `curl` + master key |
| vectors | qdrant | document | view-only | `curl http://vectors:6333` |
| queue | nats | stream | view-only | UI + conformance only |
| events | kafka | stream | view-only | UI + conformance only |

Engine creds come from `zerops_env` (orchestrator fetches, hands to agents;
stored only in gitignored `uitest/.env`). mysql (the third tabular dialect) is
NOT deployed; mariadb covers the dialect family — add a mysql import only if
tabular findings suggest dialect-specific risk (decision DD-B).

**Data discipline**: all test data is `uitest_`-prefixed (tables, keys,
indices, objects); teardown removes it; seeded conformance fixtures (e.g. the
`greeting` cache key) are never touched. Family agents own disjoint services,
so parallel runs cannot collide on data.

## 4 Scenario catalog — Lane F

Notation: each scenario = Steps → O-UI / O-ENGINE / O-HONESTY. Karel's three
reported bugs are encoded as permanent regressions: CORE-1 (write-mode
divergence), TAB-3 (layout shift), KV-3 (add-key modal).

### CORE — cross-family invariants (run on db + cache unless noted)

- **CORE-1 write-mode lifecycle across service switches** *(regression: the
  broker-rebind bug)*. Open db → toggle Write → native modal appears
  (screenshot) → Enable → toggle shows ON. From the SIDEBAR click Browse data
  on cache (same panel reveals) → toggle must STILL show ON → delete a
  `uitest_core_tmp` key → succeeds (engine confirms gone). Toggle OFF →
  mutation affordances disappear.
- **CORE-2 write-off honesty**: with Write OFF, no edit affordances render
  (no ✎, no rowdel, no Insert); nothing in the UI looks enabled-but-refused.
- **CORE-3 standalone read-only by construction**: open the standalone URL in
  a plain tab → no Write toggle exists, no edit affordances, browsing works.
- **CORE-4 reveal/reopen**: hide the panel (open another editor tab), reveal
  → state consistent, write mode per design; close panel, reopen → clean load.
- **CORE-5 state canon**: loading spinner (throttled), empty table/bucket/
  index, error (bad query) — rendered per canon on every family surface.
- **CORE-6 error honesty / "Invalid request" hunt**: induce 409 (create
  existing key), 400 (invalid JSON doc, bad cell value) → toast/error shows
  the honest structured message, never a bare generic; engine unchanged.
  Sweep KV add-key with edge inputs (empty value, unicode key, `:` namespaced,
  huge value) to reproduce Karel's screenshot.
- **CORE-7 rapid service hopping**: click all 11 services in quick succession
  → each renders its family view, no crash, no cross-family bleed, no stuck
  spinner.

### TAB — tabular (db=postgres full, mariadb full, ch view-only)

- **TAB-1 tree**: schemas → tables; row counts in `.nmeta`.
- **TAB-2 paging**: `uitest_wide` table > 1 page → load-more works; count honest.
- **TAB-3 cell edit, layout stability** *(regression)*: click editable cell →
  input appears **in place** — assert table/column bounding boxes UNCHANGED
  (before/after getBoundingClientRect diff < 2px) → edit → Enter → good toast
  → grid updated → `psql` confirms.
- **TAB-4 cell types**: number, boolean, timestamp, json, NULL set/unset;
  invalid input → `.invalid` border + honest error + engine unchanged; Escape
  cancels cleanly.
- **TAB-5 locked cells**: PK column visibly locked (muted, no ✎), title
  explains why; query-result cells locked.
- **TAB-6 insert row**: form per column type → submit → visible + `psql`
  confirms; required-field validation honest.
- **TAB-7 delete row**: rowdel → confirm modal (danger styling) → row gone +
  `psql` confirms; cancel leaves row.
- **TAB-8 query**: SELECT renders grid (read-only cells); errors render
  honestly (syntax error → message, engine untouched).
- **TAB-9 clickhouse view-only honesty**: grid renders data; ZERO write
  affordances (no ✎/rowdel/Insert — assert absent, not disabled-dead); badge
  view-only; query works.
- **TAB-10 weird data**: long text ellipsis + full view path, unicode, wide
  table scrolls inside `.gridwrap` only (no page h-scroll).

### KV — cache (valkey)

- **KV-1 key list + scan/filter** honest counts.
- **KV-2 type rendering**: string/hash/list/set/zset fixtures each render
  correct structure.
- **KV-3 add-key modal** *(regression: design + function)*: open modal →
  screenshot vs rubric (styled select, no white native controls, box-sized
  inputs) → create one key of EACH type → visible + `redis-cli TYPE/GET`
  confirms; duplicate key → honest 409.
- **KV-4 edit + TTL**: edit string value, hash field; set TTL → `redis-cli
  TTL` in range; clear TTL → -1.
- **KV-5 delete key** *(regression)*: from list and from detail → gone in UI +
  `redis-cli EXISTS`=0.
- **KV-6 edge inputs**: empty value, 100KB value, unicode/space/colon keys.

### OBJ — storage (S3)

- **OBJ-1 tree**: buckets → folders → objects.
- **OBJ-2 previews**: text renders, image preview + thumb, binary → honest
  download-only state.
- **OBJ-3 upload** via host dialog (code-server DOM file picker — drivable) →
  object in tree + `mc ls` confirms.
- **OBJ-4 download** via save dialog → file lands on container FS (SSH stat).
- **OBJ-5 delete object** → gone + `mc ls` confirms; folder delete per contract.

### DOC — es + docs (typesense) + search (meilisearch) full; vectors (qdrant) view-only

- **DOC-1 index/collection list + doc grid** per engine.
- **DOC-2 search pane**: query → results; zero-hit → empty state; styled select.
- **DOC-3 create doc**: valid JSON → visible + `curl` engine confirms; invalid
  JSON → honest 400, `.invalid` styling, engine unchanged.
- **DOC-4 edit + delete doc** → engine confirms both.
- **DOC-5 XSS fixture**: doc containing `<script>`/`<img onerror>` renders
  ESCAPED everywhere (grid, detail, search results).
- **DOC-6 qdrant**: vector collapse (summary + toggle raw floats), view-only
  honesty (zero write affordances).

### STR — queue (nats), events (kafka), view-only

- **STR-1 metadata card**: labelled card (never an editable-looking blob);
  fields plausible vs conformance data.
- **STR-2 consumer-unavailable state** renders honestly.
- **STR-3 zero write affordances**.

## 5 Evidence & findings discipline

- Evidence: `uitest/evidence/<scenario>/step-NN-<slug>.png` (+ final page
  screenshot on any failure). Gitignored.
- Findings: `uitest/findings/<agent>.jsonl`, one record per finding:
  `{id, scenario, lane, family, severity, title, repro[], expected, actual,
  evidence[], engine_truth, status}`.
- Severity: **S1** mutation lies / data loss / crash / write without token ·
  **S2** dead affordance, layout break, wrong data, dishonest error ·
  **S3** inconsistency, unclear copy, missing state · **S4** polish.
- Runner prints a manifest (scenario → PASS/FAIL/SKIP + evidence dir); re-runs
  diff against the previous manifest.
- **Adjudication**: every S1/S2 is re-run by the orchestrator before entering
  the fix queue (adversarial: reproduce or downgrade).

## 6 Harness — `internal/dataconsole/uitest/`

Node-run (same precedent as `studiojs/` and `webui/domtest/`), live-only
(needs the container), never part of `go test`. Committed as a permanent tier;
`spec-testing-architecture.md` gets its line at LAND (DD-A).

```
uitest/
  package.json          puppeteer-core only
  .env                  gitignored: URL, gate token, engine creds, SSH alias
  lib/harness.js        connect/login (CDP cookie), frames(), openConsole(svc),
                        spaFrame(), clickService(), setWriteMode(on) incl.
                        native-modal click, waitToast(), shot(name), bbox()
  lib/engines.js        engine(cmd) → ssh zcp '<cmd>' wrapper + per-engine
                        helpers (psqlRow, redisCmd, curlJson, mcLs)
  lib/runner.js         scenario registry, manifest, findings writer
  scenarios/core.js …   one file per family (disjoint → parallel-agent-safe)
  run.js                node run.js --scenario TAB-3 | --family kv | --all
  README.md             how to run, env contract, discipline
```

Harness rules: headless default (`HEADFUL=1` to watch), viewport 1440×900,
generous waits (webview loads are seconds), every failure screenshots before
throwing, no hardcoded secrets, `uitest_` data prefix + teardown.

## 7 Agent organization & gates

| Gate | What must be true | Who |
|---|---|---|
| G0 | Real UI drivable end-to-end incl. native modal | DONE (Fable, inline) |
| G1 | Harness runs CORE-1 green on the container with full evidence chain | A1 (Sonnet) |
| G2 | Lane F fan-out complete: A2 tabular, A3 kv+object+stream+standalone, A4 document — findings in | A2–A4 parallel (Sonnet) |
| G3 | Lane X complete: A5 gallery sweep, then A6+A7 independent critics | A5 → A6,A7 (Sonnet) |
| G4 | All S1/S2 adjudicated, fixed, redeployed, failed scenarios re-run green + CORE smoke | Fable + fix agents (worktree) |
| G5 | Full-matrix green run on deployed build + Czech report + gallery artifact | Fable |

Parallel-safety: agents share the container but own disjoint services and
disjoint scenario files; only A1 runs `npm install`; concurrent browser
sessions are independent code-server connections (separate workbench windows,
separate console processes).

## 8 UX rubric — Lane X (critics score each point per surface, with evidence)

1. **Layout stability** — no reflow on hover/edit/toggle (bounding-box diffs).
2. **Dark-theme uniformity** — no unstyled native controls, no white flashes.
3. **Modal anatomy** — title → body → right-aligned actions (ghost Cancel,
   primary/danger confirm); Esc/Enter/focus-trap work; danger styled danger.
4. **Affordance clarity** — editable/locked/plain visibly distinct; disabled ≠
   broken (title says why); every control has hover/active/disabled states.
5. **Feedback** — every mutation ends in a toast ≤1s after response; wording
   says what happened; spinner for >300ms operations; no silent outcomes.
6. **State canon** — loading/empty/error consistent across tree/grid/blob/
   query/search.
7. **Visual consistency** — one spacing scale, border-radius family, badge
   colors = semantics, button hierarchy uniform across families.
8. **Copy** — sentence case, specific, English; no raw error dumps as the
   primary line.
9. **Density & truncation** — long values ellipsize with a full-view path;
   tables scroll inside their wrap; no page h-scroll at 900px or 1600px.
10. **Keyboard** — Tab order sane; Enter confirms, Esc cancels (modals, cell
    edit, search).

## 9 Decisions

- **DD-A harness permanence**: DECIDED (Karel 2026-07-17) — committed
  `uitest/` tier. The fix loop and every future console change re-run it;
  spec-testing-architecture.md gets the tier line at LAND.
- **DD-B mysql coverage**: import mysql into zcp-eval-clean for the third
  tabular dialect vs rely on mariadb. → RECOMMENDED: rely on mariadb; revisit
  only if tabular findings are dialect-shaped.
- **DD-C standalone depth**: full duplicate matrix vs smoke (CORE-3 + one
  render per family). → RECOMMENDED: smoke — the broker/write path is the
  risk; the server and SPA are shared between reach paths.
- **DD-D fix batching**: fix-as-found vs batch after the full sweep.
  → RECOMMENDED: batch — dedupe across agents, one redeploy per round,
  regression re-run has a stable target.
