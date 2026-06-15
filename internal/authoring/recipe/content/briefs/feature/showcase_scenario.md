# Showcase scenario specification

A `tier=showcase` recipe MUST produce an SPA (in the frontend codebase)
that visibly demonstrates EVERY managed-service category the recipe
provisions. Read the SPA as an extended health dashboard: one **card**
per managed-service category plus a leading Status strip, each card
showing the service's live state AND housing the interactive demo
that proves the category. The porter clicks the published recipe and
sees real numbers — row counts, hit/miss ratios, queue depth, object
count, indexed documents — before they touch anything.

This spec is engine-injected, framework-agnostic. A NestJS recipe, a
Laravel recipe, and a Rails recipe all implement the same dashboard
shape — only the framework idioms underneath the cards change.

## Mandate: one card per managed-service category

The frontend codebase MUST render these cards:

| Card | Proves | Live-state | Mandatory observable |
|------|--------|------------|----------------------|
| **Status strip** | per-service liveness | Row per managed service (api, db, cache, broker, search, storage); dot+label `ok`/`down` via `--zerops-success`/`-error`. Mandatory when any managed service is provisioned. | The strip is itself the demonstration. |
| **Items / DB** | crud through DB | Row count badge from `SELECT COUNT(*)` (or framework equivalent). | Create form + list; row count survives container restart; counter increments on create. |
| **Cache** | cache-demo (read-through) | `X-Cache: HIT/MISS` colored badge (success on HIT, warning on MISS) AND a hit-counter + miss-counter pair (both required, not either/or). | Trigger fires the demo endpoint; first call shows `MISS`, second `HIT`; the badge value is the load-bearing proof — counters are supplementary. |
| **Queue / Broker** | queue-demo via worker | Pending + processed counters + chip list of last 3 events. | Publish trigger; processed counter increments AND the indexed document appears in the Search card within seconds. The two-card integration is required. |
| **Storage** | storage-upload | Object count + chip list of recent uploads (filename + size). | Upload affordance; on success the file appears in the chip list AND object count increments. Browser-walk MUST observe the click handler firing — curl alone is insufficient. |
| **Search** | search-items (full-text) | Indexed-doc count badge. | Search box + ranked results; result count matches rendered list length. |

Scope the cards to the managed-service categories the recipe actually
provisions. A recipe without a queue/broker doesn't render a Queue
card; a recipe without object-storage doesn't render a Storage card.

The Status strip is the leading element of the dashboard — it answers
"is anything wired?" before the porter touches a CRUD form.

## Card anatomy

Every category card has the same top-to-bottom shape: header (category
name in `headline-md`, optional service-type subtitle in muted
`body-sm`, status dot mirroring the Status strip), live-state element
(counter in `headline-lg` + `body-sm` label, badge in status tokens,
or both — the Cache card requires both, the Queue card requires both
counters and a chip list), demo trigger (button or inline form, never
a modal or separate route), result display (chips, plain `<ul>`, or
ranked list). All cards share width, padding, and radius via
`var(--zerops-radius-card)` and `card-bg-light/dark` from the
feature brief's design-tokens table — never hardcode dimensions or
redefine tokens locally.

## Visualization vocabulary (closed list)

The dashboard framing is a magnet for chart libraries. It is wrong for
this brief. Allowed: plain text counters (`headline-lg` + `body-sm`
label), colored badges using `--zerops-success`/`-warning`/`-error`/
`-primary`, bullet/chip lists capped at 3-5 items (no virtualization,
no infinite scroll), 8px status dots next to labels.

Forbidden: chart dependencies (recharts, visx, chart.js, d3, victory,
apexcharts, plotly, nivo, lightweight-charts, observable-plot);
generated chart components (`<LineChart>`/`<BarChart>`/`<Sparkline>`/
`<Gauge>`/`<Donut>`); hand-rolled SVG/canvas/CSS sparklines (gradient
bars, rotated divs, `<polyline>` paths, `stroke-dasharray` ring/arc
progress); HTML viz primitives standing in for charts (`<progress>`,
`<meter>`, `aria-valuenow` divs styled as bars) — the verifier reads
`data-test` text content, none of these are text; any `npm install`/
`yarn add`/`composer require` for a visualization package; animated
counters that interpolate on transition; CSS animations on counter or
badge elements (`animate-pulse`, `animate-bounce`, `transition` on
the value); emoji used as a status indicator (use the colored dot +
text label, not 🟢/🔴). Render numbers directly; the verifier reads
the DOM at click+wait, not mid-tween.

## Live-state pattern (no websockets, no fake state)

"Live state" means counters and badges reflect the actual managed
service state AT THE TIME OF FETCH — not a real-time stream. On
mount, each card fetches once via whatever the backend feature pass
already exposes — the existing collection endpoint (`.length` for
the count) or a dedicated `*-state` route if shipped. Do not invent
endpoints. After demo trigger: re-fetch the live-state value
immediately on trigger success — without the re-fetch the counter
goes stale and the verifier sees no change. Async convergence (queue → search): the
publishing card polls the search card's state endpoint at 500ms
intervals up to a 5s ceiling, then surfaces `processed` once the
indexed document appears; no background polling outside that bounded
window. Forbidden: websockets, server-sent events, unbounded
setInterval timers, client-only optimistic state reconciled silently,
client-only fake counters that increment without a backend
round-trip. The recipe does not ship websocket infra; do not invent
it.

## List ordering — newest-first across every card

Every list rendered on a card (chip lists, ranked results, event
streams, items rows) MUST display newest-first. The browser-walk's
primary observable for any "trigger fires + something appears in the
list" verification is *the just-added item lands at the top of the
visible list*. If the list is ordered ASC (oldest-first) or by a
non-time key (alphabetical, key-name), the just-added item lands
offscreen at position 6+ after a few iterations and the verifier
reads "no change" — false-negative click failure with no recovery
signal.

Backend contract per surface:

- **DB-backed cards (Items, etc.)** — `ORDER BY created_at DESC` (or
  framework equivalent: TypeORM `order: { createdAt: 'DESC' }`,
  Eloquent `latest()`, Prisma `orderBy: { createdAt: 'desc' }`).
- **Cache / Redis-style cards (Queue events, etc.)** — `LPUSH` puts
  newest at the head; `LTRIM 0 N-1` keeps the most-recent N; `LRANGE
  0 N-1` reads them newest-first by construction. Do not author
  `RPUSH` for an event-stream list.
- **Object-storage cards** — `ListObjectsV2Command` returns
  alphabetical-by-key. With timestamp-suffixed upload keys
  (`uploads/dashboard-upload-${Date.now()}.txt`), alphabetical IS
  monotonic-numeric — but the natural order is OLDEST-first. Sort
  the result DESC by `LastModified` (or by key for monotonic
  timestamp keys) on the API side before returning. The frontend
  `slice(0, N)` then takes the N newest.
- **Search results** — Meilisearch / equivalent return by relevance
  score, not by recency; that's correct for a search box (relevance
  > recency). Do NOT reverse-sort search results by time.

Frontend contract: `slice(0, N)` on a backend list assumes the API
already ordered DESC by recency. Do not author client-side
re-sorting; the backend owns the contract.

The verifier reads `[data-test=...]` text and the first chip in
`[aria-label="..."] li`. If position 1 is yesterday's item, the
verification fails silently no matter how many times the click fires
correctly.

## Design priorities

- **Demonstration-first content.** Effort goes on what each card
  demonstrates. No hero sections, marketing copy, decorative
  iconography, or chrome that exceeds the viewport.
- **Design tokens, not custom systems.** Use the Zerops design tokens
  via the Tailwind utility shapes from the feature brief; do not
  author a custom design system or add a second CSS framework.
- **Real data.** Cards exercise the actual deployed managed services
  — real rows, real worker output, real index hits. No mocks, no
  client-only fixtures.
- **Card uniformity over decoration.** Same card shape with
  category-specific content reads as polish; per-card custom styling
  reads as chaos.

### Stable selectors for browser-walk verification

Per-snapshot DOM refs go stale across `zerops_browser` calls (silent
no-op clicks). Use stable attribute selectors. Add `data-feature` to
interactive elements (`publish`, `upload`, `search`, `create-item`,
`cache-fetch`). Add `data-test` to every counter and badge using the
patterns `[data-test="<category>-<metric>"]` for counters,
`[data-test="<category>-state-badge"]` for state badges, and
`[data-test="status-<service>"]` for service health; a variant
adding a new card follows the same naming rules. The cards in this
scenario emit: `[data-test="items-count"]`,
`[data-test="cache-hits"]`, `[data-test="cache-misses"]`,
`[data-test="cache-state-badge"]` (carrying the literal `HIT` or
`MISS` text), `[data-test="queue-processed"]`,
`[data-test="storage-objects"]`, `[data-test="search-indexed"]`,
`[data-test="status-<service>"]`. Browser-walk targets by
attribute, not per-snapshot ref.

## Layout: card grid sized to the headless viewport

`zerops_browser` runs headless Chrome at a small default viewport;
treat sizing as conservative-minimum and do not assume a specific
pixel value. Click events dispatch at element-center coordinates
without auto-scrolling; elements below the fold receive clicks at
out-of-bounds coordinates. Single-column multi-panel scrolls have
failed verification before; the layout below avoids that by
construction.

Canonical layout: a **responsive grid (3-col → 2-col → 1-col by
viewport) under a full-width Status strip**. Status strip on top;
cards flow into the grid in the canonical order Items / Cache /
Queue / Storage / Search. The Status strip + the first row MUST fit
within the viewport without scrolling — verify on first render that
the Items card's CTA is reachable. Card bodies stay compact when
collapsed (live-state + trigger visible; result display may expand
on demand). When a card grows on interaction, re-target by
`data-feature` selector after expansion, not by coordinates.

This layout is **normative** for the showcase recipe. Do not switch to tabs, accordions, or single-panel layouts. The dashboard
deliverable is "every card visible at a glance"; tabs/accordions
hide cards behind interaction and produce a non-dashboard output.
If a card is below the fold, scroll-into-view handles that — see
the browser-verification subsection below.

### Browser verification — scroll below-fold cards into view, do not abandon the layout

`zerops_browser` dispatches clicks at element-center coordinates. If
the target element is below the fold, your test must explicitly
scroll it into view BEFORE clicking. The supported pattern is
`data-feature` selectors paired with
`element.scrollIntoView({block: 'center'})` invoked before the
click step:

```js
// Before each click on a card outside the initial viewport.
const el = document.querySelector('[data-feature="upload"]');
el.scrollIntoView({block: 'center'});
// then dispatch the click via zerops_browser
```

Do NOT abandon the spec layout to make every element above-fold by
default; that produces a non-dashboard deliverable. Below-fold
clicks are a verification-script bug, not a layout-spec issue.

## Dev loop — appdev HMR first, cross-deploy last

For each feature card (Items / Cache / Queue / Storage / Search):

1. **Author the card on appdev.** Self-deploy via `git push` is the
   only deploy; the dev container's HMR (`npm run dev` already running
   under SSH) picks up the change automatically.
2. **Browser-walk on appdev** with `zerops_browser`. Click the card's
   primary action; verify the response state lands. Use
   `data-feature` selectors and `scrollIntoView({block: 'center'})`
   per the layout-pinning section above.
3. **Iterate WITHIN appdev.** If the click silently fails, the card
   re-renders incorrectly, or a fetch returns wrong data — debug
   on appdev. The bundle is the same one appstage will run; build
   pipeline is shared. There is no class of bug visible only on
   appstage that is invisible on appdev.
4. **Cross-deploy to appstage ONCE per feature-pass close.** After
   all five cards browser-walk green on appdev, ONE cross-deploy
   verifies the production bundle path (build-time `VITE_API_URL`
   bake, CORS allow-list, TLS termination, `dist/~` strip). One pass
   per feature-pass; not one per iteration.

### Why this matters

`zerops_deploy sourceService=appdev targetService=appstage` runs
`npm ci` + `vite build` + ships `dist/~`. That's 30-60 s per
iteration. Eight iterations cost 4-8 min — equal to the entire
features-frontend pass for two cards in run-26. Run-28 features-
frontend agent dispatched 8 cross-deploys debugging one card; the
right loop was appdev HMR + browser-verification + a single
cross-deploy at close.

### When cross-deploy IS the right tool

- The card's behavior depends on a build-time env-var bake
  (`VITE_API_URL`) and you suspect the bake is wrong. Cross-deploy
  once; inspect the compiled JS.
- A CORS / cross-origin / TLS issue surfaces only against the
  HTTPS subdomain (appstage), not the dev http://localhost path
  (appdev with port-forwarded HTTPS via L7 covers most of these).
- The feature-pass is closing and you want stage-side smoke. One
  cross-deploy.

### When cross-deploy is the WRONG tool

- The click handler doesn't fire. Fix on appdev — same JS.
- A fetch returns wrong data. Fix on appdev — same backend.
- A card renders incorrectly. Fix on appdev — same component.
- ANY in-bundle behavior. The bundle is shared.

If you've cross-deployed the same source twice in a row debugging
the same card, stop and reach for appdev HMR.

## Per-card browser-verification

After implementing the cards, run `zerops_browser` against the SPA and
exercise EACH card. For each one, record one fact:

```
zerops_recipe action=record-fact slug=<slug>
  topic=<frontend-cb>-<category>-browser
  symptom="<what you saw + whether the demonstration signal was visible AND the counter delta held>"
  mechanism="zerops_browser"
  surfaceHint=browser-verification
  citation=none
  scope=<frontend-cb>/<category>
  extra.console=<digest>
  extra.screenshot=<path or none-snapshot-only>
```

Mandatory facts, one per card — `<frontend-cb>-<category>-browser`:
status (every provisioned service `ok`, any `down` is a wiring
regression); items (create works + `[data-test="items-count"]`
increments by 1, AND row count survives a `zerops dev restart` —
read counter, restart container, browser-walk again, counter holds);
cache (`[data-test="cache-state-badge"]` reads `MISS` first call,
`HIT` second call; `[data-test="cache-hits"]` and
`[data-test="cache-misses"]` both increment to reflect the two
calls); queue (publish fires + `[data-test="queue-processed"]`
increments + indexed doc appears in `[data-test="search-indexed"]`
within 5s); storage (upload click fires + `[data-test=
"storage-objects"]` increments + file appears in the chip list —
curl alone is insufficient); search (query returns ranked hits,
result count matches rendered list length).

Verification protocol for any card with a counter: wait for the
counter selector to be present AND its `textContent` to parse as an
integer → read the counter selector → click `[data-feature="<name>"]`
→ wait for response (up to 5s on async) → re-read the same counter
selector → assert delta (typically +1). The counter delta is the canonical
click-caused-state-change signal but does NOT replace the
category-specific mandatory observable (X-Cache `HIT`/`MISS` badge
text, ranked results, queue→search integration, upload-handler-fires,
row-count-survives-restart). Both must hold; the load-bearing proof
on the Cache card is the badge text, not the counters.

Any browser walk producing console errors is a regression — fix
before close. The feature_kinds taxonomy names the backend endpoints;
the cards are the frontend's responsibility — a queue-demo backend
that's never visualized fails this scenario spec even if curl proves
round-trip. The dashboard is the deliverable.

## Storage-upload card — resilient shape

The `zerops_browser` tool surface (commands: `click`, `fill`, `find`,
`get`, `is`, `wait`, `snapshot`) does NOT include a file-input
selector primitive — there is no `setInputFiles` or equivalent. A
real `<input type="file">` element cannot be programmatically
populated from a browser-walk. The Storage card MUST therefore expose
both:

- **A real file selector** (`<input type="file" data-feature="upload-file"
  accept="*">` plus a labeled "Select file" affordance) for the human
  porter who deploys the recipe and uses it normally.
- **A blob-fallback button** (`<button data-feature="upload">`) whose
  click handler builds an in-memory `Blob` programmatically (no DOM
  file dialog), wraps it in `FormData`, and POSTs to
  `/api/storage/upload`. The blob shape is tiny (a one-line text
  payload with a timestamp); the button is the browser-walk path.

Both affordances target the same `POST /api/storage/upload` endpoint.
The selector lets a human upload a real file; the blob button lets
`zerops_browser` exercise the upload pipeline without a file dialog.

### Styling — both affordances adopt the design tokens

The file selector and "Upload selected" button must share the visual
vocabulary of the rest of the card — same button radius, same primary
colour for the active CTA, same border treatment on the file-input
wrapper. Bare browser default styling (`<input type="file">`'s native
"Choose File" widget; an unstyled `<button>`) is a regression — it
ships visibly different fonts, shapes, and colours than the card it
sits inside. Two concrete moves:

1. **Hide the native file-input chrome and project a styled label.**
   Wrap `<input type="file" data-feature="upload-file" class="sr-only">`
   in a `<label>` whose visual shape uses the same token-driven
   classes as the rest of the card (rounded panel, design-token border,
   `font-[var(--zerops-font-body)]`). Show the chosen filename
   adjacent to the label via JS (`input.files[0]?.name ?? 'No file
   chosen'`). Native `Choose File` button visible inside the card is
   the regression signal.
2. **The blob/"Upload selected" button uses the same token vocabulary
   as the primary "Upload sample blob" button** — same radius, same
   `--zerops-primary` background when enabled, same disabled-state
   colour. A grey-on-grey unstyled button next to a teal primary
   button is the regression signal.

Object list items (the recently-uploaded files below the upload form)
get the same token-driven card treatment — design-token background,
no default-browser blue/purple `text-decoration: underline` link
styling. If a list item renders as a raw `<a>` with browser-default
underline, it didn't pass the styling pass.

### Browser-walk fallback escape hatch

If `[data-feature="upload"]` click delivery fails (silent, counter
doesn't increment) after **2 attempts** with `scrollIntoView` applied
per the Layout section above, record the field_rationale and move on
— do NOT loop. The backend curl chain in the features-backend pass
is the canonical proof of the upload pipeline; the frontend
browser-walk is a supplementary check that some headless-Chromium
versions can't deliver:

```
zerops_recipe action=record-fact slug=<slug>
  topic=<frontend-cb>-storage-upload-click-headless-fragility
  kind=field_rationale
  symptom="zerops_browser click on [data-feature=upload] silent
    after 2 attempts with scrollIntoView; counter
    [data-test=storage-upload-attempts] did not increment;
    no console errors visible"
  fixApplied="recorded as platform-side click-delivery limitation;
    backend curl chain in features-backend pass remains canonical
    upload-pipeline proof"
  surfaceHint=field-rationale
  scope=<frontend-cb>/storage
```

Then proceed to the next card. Two click attempts is the cap; do not
spend a feature-pass debugging click-delivery. The on-mount card
surface (object count from `getStorageState()` + chip list of recent
uploads, newest-first per the List ordering section) is the
demonstration; click-fired-on-button is the bonus.
