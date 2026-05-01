# Showcase scenario specification

A `tier=showcase` recipe MUST produce an SPA (in the frontend codebase)
that visibly demonstrates EVERY managed-service category the recipe
provisions. The reader is a porter clicking the published recipe to see
what each managed service can do — the SPA is the recipe's
demonstration surface.

This spec is engine-injected, framework-agnostic. A NestJS recipe, a
Laravel recipe, and a Rails recipe all implement the same demonstration
shape — only the framework idioms underneath the panels change.

## Mandate: one demonstration panel per managed-service category

The frontend codebase MUST render these panels:

| Panel | Proves | Mandatory observable |
|-------|--------|----------------------|
| **Items / DB** | crud through database | Form to create + list view; row count survives container restart. |
| **Cache** | cache-demo (read-through) | `X-Cache: HIT/MISS` header visible in the panel; trigger button + display. |
| **Queue / Broker** | queue-demo through worker | Trigger button publishes a job; live feed shows the worker processed it (log tail or status indicator); resulting indexed document appears in the search panel within seconds. |
| **Storage** | storage-upload (object storage) | File picker + upload; on success the uploaded file appears in the list. Browser-walk MUST observe a UI change from the click handler — curl-reaching the signed URL is insufficient; the handler must demonstrably fire. |
| **Search** | search-items (full-text) | Search box; query against the items list with ranked results. |
| **Status** | per-service liveness | One row per managed service (api, db, cache, broker, search, storage); per-service ping `ok` / `down`. Mandatory when the recipe provisions any managed services. |

Scope the panels to the managed-service categories the recipe actually
provisions. A recipe without a queue/broker doesn't render a Queue
panel; a recipe without object-storage doesn't render a Storage panel.

## Design priorities

- **Modern design.** Clean component shapes, sensible spacing, basic
  responsive layout. Tailwind utilities or framework defaults are fine;
  do NOT author a custom design system.
- **Demonstration components, not chrome.** Spend effort on what each
  panel demonstrates (the queue-demo's live feed, the cache-demo's
  HIT/MISS indicator, the search results ranking) — NOT on branding,
  hero sections, marketing copy, multi-column dashboards, or
  decorative iconography.
- **Reading order is panel-first.** Order: Status, Items, Cache,
  Queue, Storage, Search. Status leads — it answers "is anything
  wired?" before the porter touches a CRUD form.
- **Data is real.** Panels exercise the actual deployed managed
  services; no mock data, no client-only fixtures. The Items panel
  shows real Postgres rows; the Queue panel shows real worker output;
  the Search panel shows real Meilisearch hits.

### Stable selectors for browser-walk verification

Per-snapshot DOM element refs go stale across `zerops_browser` calls.
A click against a previous-snapshot ref produces a silent no-op (no
error; nothing happens), and the agent burns minutes diagnosing the
"button does nothing" symptom. Use stable attribute selectors:

- Add `data-feature="<name>"` to interactive elements you intend to
  exercise (publish triggers, search inputs, upload buttons).
- Add `data-test="<name>"` to result-display elements (search results,
  X-Cache badges, queue-feed entries, status indicators).
- In `zerops_browser` calls, target by attribute (`[data-feature=
  "publish"]`) rather than by per-snapshot ref.

If a click appears to do nothing, suspect a stale ref; re-query the
DOM via the data attribute and retry.

## Per-panel browser-verification

After implementing the panels, run `zerops_browser` against the SPA and
exercise EACH panel. For each one, record one fact:

```
zerops_recipe action=record-fact slug=<slug>
  topic=<frontend-cb>-<panel>-browser
  symptom="<what you saw + whether the demonstration signal was visible>"
  mechanism="zerops_browser"
  surfaceHint=browser-verification
  citation=none
  scope=<frontend-cb>/<panel>
  extra.console=<digest>
  extra.screenshot=<path or none-snapshot-only>
```

Mandatory facts (one per panel rendered):

- `<frontend-cb>-status-browser` — every provisioned managed service
  has a row reading `ok`; any `down` row is a wiring regression
- `<frontend-cb>-items-browser` — Items panel renders + create works
- `<frontend-cb>-cache-browser` — `X-Cache: MISS` first call, `HIT` second
- `<frontend-cb>-queue-browser` — publish fires; worker processes visibly;
  indexed document appears
- `<frontend-cb>-storage-browser` — upload click handler demonstrably
  fires (uploaded file appears in the list); curl-reaching the signed
  URL alone is insufficient
- `<frontend-cb>-search-browser` — search query returns ranked hits

Any browser walk producing console errors is a regression — fix before close.

## Layout: tabs or collapsed panels for > 3 demonstrations

`zerops_browser` runs headless Chrome at ~577px viewport. Click events
dispatch at element-center coordinates without auto-scrolling into
view; panels below the fold receive clicks at out-of-bounds coordinates
and the browser-walk burns time on missed interactions. Run-14 hit this
on a 6-panel single-column layout (R-14-5) — three panels were below
the fold, three of the per-panel browser-verification facts could not
be recorded without manual scroll.

Positive shape: when the page renders MORE THAN 3 demonstration panels,
use a layout that keeps the active panel above the fold by construction:

- **Tabs** — one tab per category (Status / Items / Cache / Queue /
  Storage / Search). Default selection picks Status; switching tabs
  swaps the active panel without scrolling.
- **Collapsed accordion** — collapsed by default, one expanded at a
  time. Clicking a panel header expands and collapses the prior one.
- **Two-column grid (≤3 wide)** — works only if the layout fits
  vertically; on the headless viewport this means the bottom row's
  CTAs land within the 577px window.

Avoid: a 6-panel single-column scroll. Even with stable selectors the
browser-walk dispatches clicks at out-of-bounds coordinates; the
verification step takes 2-3× longer and produces partial fact coverage.

The layout choice affects browser-verification reliability, not the
porter's UX choice — the porter can re-style after porting. The recipe
ships the layout that lets the engine's verification pass cleanly on
the first try.

## Panel scope, not feature-kind scope

The feature_kinds taxonomy (above) names the BACKEND endpoints each
demonstration requires. The PANELS are the frontend's responsibility. A
queue-demo backend that's never visualized fails this scenario spec
even if curl proves the round-trip works.
