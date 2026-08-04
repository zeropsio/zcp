# Data Console — blank slates (2026-08-04)

Goal: replace the bare one-line empty states in the Data Console SPA with proper
blank slates — title + detail + (gated) next-step action — family-aware, through
ONE canon renderer. SPA-only change: no server/provider/extension edits.

## Ground rules (from spec + repo)

- SPA source is `internal/dataconsole/console/webui/dist/` (committed hand-written
  vanilla JS, no build step). Tests: `webui/domtest/*.dom.test.js` load the REAL
  dist files via `domtest/harness.js`; Go gate `spa_domtest_test.go` runs them.
- State canon P.4 (`docs/spec-dataconsole.md` §7.4): ONE state rendering per
  condition, shared across tree / grid / blob / query. Blank slates EXTEND the
  canon — one renderer, never per-surface ad hoc HTML.
- A mutating affordance renders ONLY when `editing() && actionEnabled(...)` —
  never disabled-with-a-reason. Read affordances (query console, search) gate on
  `actionEnabled` alone.
- XSS invariant: every dynamic string through `esc()`; never a raw value into a
  title/attribute.
- `/api/services` already returns `family` per service
  (tabular/kv/document/object/stream — see `console/testdata/services_contract.json`);
  the SPA does not read it yet. Use `svcOf(service).family` — no client-side
  type-string parsing (the family registry is server truth, spec §6).
- TDD mandatory: new domtests RED first, then implement.

## Work items

### W1 — canon blank-slate renderer (app.js state-canon section ~L491 + style.css)

Extend `stateEmpty` to accept either a plain string (legacy call sites keep
working) or an options object `{icon, title, detail, actionsHTML}` where
`actionsHTML` is pre-gated button markup supplied by the caller. Output stays a
single `.state.empty` root element — `appendTreePage`'s DOM-clear step removes
`:scope > .state` and must keep working. Add `.state.empty` blank-slate layout
in `style.css` (title/detail/action rows; muted palette via existing CSS vars;
no new colors).

### W2 — tree root empty (appendTreePage root branch, app.js ~L534)

Family-aware blank slate via `svcOf(service).family`:

| family | title | detail | CTA (gating) |
|---|---|---|---|
| tabular | No tables yet | tables are created with SQL | "Open query console" → `openQuery(service)` (gate: `actionEnabled(querySQL)` — read action) |
| kv | No keys yet | — | "Add key" → `createKeyForm(service)` (gate: `editing() && actionEnabled(createKey)`) |
| document | No indexes yet | indexes appear once the app creates them | none (`createDoc` targets an existing index) |
| object | Bucket is empty | mention the upload bar when it is present | keep existing `addUploadBar` behavior unchanged (the bar IS the CTA) |
| stream | No streams yet | read-only family | none |
| unknown/absent | Nothing here yet | — | none |

### W3 — grid zero rows (renderGrid empty branch, app.js ~L1167)

Keep the `td.state.empty` + `colSpan = cols + delete col` invariant (pinned by
`grid-empty-state-colspan.dom.test.js`). Copy by source:
- query result (`source:"query"`): "Query returned no rows" — distinct from a
  table's own emptiness.
- table/collection: "No rows yet"; when the toolbar's Insert row button rendered
  (`canWrite && insertRow && !noKey`), add detail "Use Insert row above to add
  the first one". Text only inside the td — the mutating control stays in the
  toolbar, never duplicated into the slate.

### W4 — search pane (runSearch, app.js ~L1708)

- Prompt state ("Enter search text") → blank-slate copy through the canon
  renderer.
- Zero-hit → `No matches for "<q>"` with the query `esc()`d.

### W5 — no managed services (renderServices + initial placeholder)

When `/api/services` returns zero services: rail gets a muted "No managed
services" note; the `#content` placeholder becomes a blank slate — title
"No managed services in this project", detail: the console surfaces managed
databases/caches/queues/storage; use ↻ to re-discover. No CTA. Embedded
deep-link path unaffected.

### W6 — not-yet-browsable service (selectService "not yet" branch, app.js ~L464)

Route through the canon renderer: title "<host> isn't browsable yet", detail =
support-tier explanation. Tree still cleared.

### W7 — spec touch (docs/spec-dataconsole.md §7.4)

One short paragraph extending the state canon: the empty state is a blank slate
(one renderer; family-aware copy; a CTA only when its action is enabled), and a
query-result empty is worded distinctly from a table empty.

## Tests (T2 offline domtests — RED first; follow existing harness patterns)

1. `blank-slate-tree-family.dom.test.js` — tabular empty tree shows the query
   CTA (querySQL enabled) and clicking it opens the query console; kv shows
   "Add key" ONLY with write mode on + createKey enabled; a view-only kv shows
   NO CTA at all.
2. `blank-slate-grid-copy.dom.test.js` — table-empty vs query-result-empty copy
   are distinct; the empty td still spans every header cell.
3. `blank-slate-services.dom.test.js` — `services: []` → rail note + content
   blank slate.
4. Search zero-hit echoes the escaped query — hostile payload
   (`<img src=x onerror=…>`) asserted inert (no element injected).
5. Entire existing domtest suite stays green.

## Verification

```
go test ./internal/dataconsole/... -short
make lint-fast
```

## Out of scope

Server/provider/extension code, uitest (Puppeteer) scenarios, paginator range
label ("No rows" there is a range readout, not a state), blob "Binary content —
use Download" placeholder (not an empty state).
