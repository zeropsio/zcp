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
| **Storage** | storage-upload (object storage) | File picker + upload button; on success, signed URL displayed; clicking URL retrieves the same bytes. |
| **Search** | search-items (full-text) | Search box; query against the items list with ranked results. |
| **Status** (optional but recommended) | per-service liveness | One row per managed service (api, db, cache, broker, search, storage); per-service ping result `ok` / `down`. |

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
- **Reading order is panel-first.** A porter scanning the page sees
  panels in the order: Items, Cache, Queue, Storage, Search, Status.
  Headings explain what each panel proves.
- **Data is real.** Panels exercise the actual deployed managed
  services; no mock data, no client-only fixtures. The Items panel
  shows real Postgres rows; the Queue panel shows real worker output;
  the Search panel shows real Meilisearch hits.

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

Mandatory facts:

- `<frontend-cb>-items-browser` — Items panel renders + create works
- `<frontend-cb>-cache-browser` — Cache panel shows `X-Cache: MISS` on
  first call, `HIT` on second
- `<frontend-cb>-queue-browser` — Queue panel: publish trigger fires;
  worker processes (visible somehow); indexed document appears
- `<frontend-cb>-storage-browser` — Upload + signed-URL retrieve
- `<frontend-cb>-search-browser` — Search query returns ranked hits

Status panel verification optional. Any browser walk that produces
console errors is a regression — fix before close.

## Panel scope, not feature-kind scope

The feature_kinds taxonomy (above) names the BACKEND endpoints each
demonstration requires. The PANELS are the frontend's responsibility. A
queue-demo backend that's never visualized fails this scenario spec
even if curl proves the round-trip works.
