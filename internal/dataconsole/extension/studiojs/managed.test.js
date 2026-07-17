"use strict";

// Managed card seam test.
//
// (1) One row per MANAGED service (category==='managed'). Each row carries one open
//     affordance: the whole row AND an explicit "Browse data →" button post
//     openConsole (embedded WebviewPanel). It carries no writes flag.
// (2) Write mode is NOT a card affordance — it is a host-confirmed toggle inside the
//     embedded panel. The card must carry no data-allow-writes at all.
// (3) Runtime services never appear here; managed rows never render a subdomain
//     link (managed services do not serve one — that is the Runtime card's job).
//
// The console handler (handlers/console.js) opens the embedded panel.

const assert = require("assert");
const card = require("../templates/vscode-studio/cards/managed");

assert.strictEqual(card.id, "managed", "card id is 'managed'");
assert.strictEqual(card.order, 20, "managed card orders second (order=20)");

const uiMap = {
  project: { id: "p", name: "demo", status: "ACTIVE" },
  services: [
    { hostname: "db", type: "postgresql:single@18", status: "RUNNING", partKind: "managed-dep", category: "managed", subdomainUrl: "" },
    { hostname: "cache", type: "valkey:single@7.2", status: "RUNNING", partKind: "managed-dep", category: "managed", subdomainUrl: "" },
    { hostname: "ch", type: "clickhouse:single@25.3", status: "RUNNING", partKind: "managed-dep", category: "managed", subdomainUrl: "" },
    { hostname: "files", type: "shared-storage", status: "RUNNING", partKind: "managed-dep", category: "managed", subdomainUrl: "" },
    { hostname: "api", type: "nodejs@22", status: "ACTIVE", partKind: "adopted", category: "runtime", subdomainUrl: "https://api.zerops.app" },
  ],
  warnings: [],
};

const html = card.render(uiMap);

// (U-08) Support-tier gating mirrors the console rail: the card can never offer
// Browse on a service the console renders as not-yet, nor imply full editing on a
// view-only engine. Tier badges label each row; not-yet disables Browse.
assert.ok(html.indexOf("zs-tier-ready") >= 0, "a full-support service (db/cache) shows a 'ready' tier badge");
assert.ok(html.indexOf("zs-tier-view") >= 0, "a view-only service (clickhouse) shows a 'view-only' tier badge");
assert.ok(html.indexOf("zs-tier-not-yet") >= 0, "a not-yet service (shared-storage) shows a 'not yet' tier badge");
// The not-yet service (files/shared-storage) must NOT be browsable — no openConsole
// deep-link for it, and its Browse button is disabled.
assert.ok(html.indexOf('data-service="files"') < 0, "a not-yet service posts NO openConsole deep-link (not browsable — U-08)");
assert.ok(html.indexOf("Not yet browsable") >= 0, "a not-yet service shows a disabled 'Not yet browsable' button, not an active Browse");
// A view-only service is still browsable (opens read-only).
assert.ok(html.indexOf('data-action="openConsole" data-service="ch"') >= 0, "a view-only service (clickhouse) is still browsable (opens read-only)");

// (1) read-only Browse deep-link per managed service (row + explicit button).
assert.ok(
  html.indexOf('data-action="openConsole" data-service="db"') >= 0,
  "db managed service gets a read-only Browse deep-link"
);
assert.ok(
  html.indexOf('data-action="openConsole" data-service="cache"') >= 0,
  "cache managed service gets a read-only Browse deep-link"
);
assert.ok(html.indexOf("Browse data") >= 0, "an explicit Browse affordance is present");

// The standalone "Open in browser" affordance is gone — Browse data (embedded) is
// the only way to open a managed service's console from this card.
assert.ok(html.indexOf("openConsoleBrowser") < 0, "no openConsoleBrowser action is posted from the managed card");
assert.ok(html.indexOf("Open in browser") < 0, "no Open-in-browser affordance renders");

// Write mode is NOT a card affordance anymore — it is a host-confirmed toggle
// INSIDE the Data Console panel. The card must carry no allow-writes at all — and
// neither open path (embedded or standalone) is a write grant.
assert.ok(html.indexOf("data-allow-writes") < 0, "the card has no write button (write mode lives in the panel toggle)");

// Each managed row shows its service-type brand icon (in a white chip) so it is
// recognizable by type — the same icon source as the Zerops dashboard.
assert.ok(html.indexOf('class="zs-svc-icon"') >= 0, "managed rows render a service-type icon chip");
assert.ok(html.indexOf("<svg") >= 0, "the icon chip carries an inline SVG");
// Resolution is by base type: postgresql:single@18 -> the postgresql icon.
const { iconFor } = require("../templates/vscode-studio/lib/svc-icons");
assert.ok(iconFor("postgresql:single@18").indexOf("<svg") === 0, "iconFor resolves a known managed type to an SVG");
assert.strictEqual(iconFor("nodejs@22"), "", "iconFor returns empty for a non-managed/unknown type");

// Narrow-panel layout parity with the Runtime card: managed rows are the SAME
// vertical card (icon + title block + badge in the head, Browse on its own
// right-aligned line) — keeping managed horizontal would reproduce the sidebar
// wrapping bug in the second card and fork the row system.
assert.ok(html.indexOf('class="zs-rowhead"') >= 0, "managed rows use the stacked head layout (zs-rowhead)");
assert.ok(html.indexOf('class="zs-rowmain"') >= 0, "managed host+type share a title block (zs-rowmain)");

// (3) runtime services are NOT in the Managed card, and no subdomain link leaks in.
assert.ok(html.indexOf('data-service="api"') < 0, "runtime service is not listed in the Managed card");
assert.ok(html.indexOf("zerops.app") < 0, "no subdomain/live link renders on a managed row");

// Empty case -> muted note, no rows.
const runtimeOnly = uiMap.services.find(function (s) { return s.category === "runtime"; });
const empty = card.render({ services: [runtimeOnly] }); // only the runtime svc
assert.ok(empty.indexOf("No managed services") >= 0, "zero managed services -> muted note");
assert.ok(empty.indexOf('data-action="openConsole" data-service=') < 0, "no Browse rows when no managed services");

// Stable order: rows sort by hostname regardless of discover/API order.
const shuffled = card.render({
  services: [
    { hostname: "zcache", type: "valkey:single@7.2", status: "RUNNING", partKind: "managed-dep", category: "managed", subdomainUrl: "" },
    { hostname: "adb", type: "postgresql:single@18", status: "RUNNING", partKind: "managed-dep", category: "managed", subdomainUrl: "" },
  ],
});
assert.ok(
  shuffled.indexOf(">adb<") < shuffled.indexOf(">zcache<"),
  "managed rows render sorted by hostname (stable across refreshes)"
);

console.log("managed.test.js OK");
