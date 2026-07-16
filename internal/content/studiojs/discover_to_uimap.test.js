"use strict";

// MUST-PIN seam test (§7.3): the mapping zerops_discover JSON -> the cockpit UI
// map. Drop a read here and the Parts view silently degrades. The fixture gives
// every field a distinct value; we assert each SURVIVES the mapping, for all
// six AdoptionState buckets — so mapping a field to a constant "" would fail.

const assert = require("assert");
const fs = require("fs");
const path = require("path");
const {
  discoverToUIMap,
  CATEGORY_BY_KIND,
} = require("../templates/vscode-studio/lib/discoverToUIMap");

const discover = JSON.parse(
  fs.readFileSync(path.join(__dirname, "fixtures", "discover.json"), "utf8")
);
const ui = discoverToUIMap(discover);

// Project fields mapped from the real source values.
assert.strictEqual(ui.project.id, "proj-123");
assert.strictEqual(ui.project.name, "demo-project");
assert.strictEqual(ui.project.status, "ACTIVE");

assert.strictEqual(ui.services.length, 6, "all six services mapped");

const byId = {};
ui.services.forEach((s) => {
  byId[s.id] = s;
});

// A runtime service carries every field through, including the subdomain URL.
const adopted = byId["svc-adopted"];
assert.ok(adopted, "adopted service mapped (serviceId -> id)");
assert.strictEqual(adopted.hostname, "api");
assert.strictEqual(adopted.type, "nodejs@22");
assert.strictEqual(adopted.status, "ACTIVE");
assert.strictEqual(adopted.partKind, "adopted");
assert.strictEqual(adopted.category, "runtime");
assert.strictEqual(adopted.isInfrastructure, false);
assert.strictEqual(adopted.subdomainUrl, "https://api-abc.zerops.app");

// A managed dep maps to the managed category and is flagged infrastructure.
const db = byId["svc-managed"];
assert.ok(db, "managed dep mapped");
assert.strictEqual(db.partKind, "managed-dep");
assert.strictEqual(db.category, "managed");
assert.strictEqual(db.isInfrastructure, true);
assert.strictEqual(db.subdomainUrl, "");

// The control-plane service maps to the system category.
assert.strictEqual(byId["svc-self"].category, "system");

// partKind is the adoptionState VERBATIM for every bucket, and category is the
// single-owner-derived coarse grouping. Pin all six so a future enum addition
// can't silently fall through to the default.
const expectCategory = {
  adopted: "runtime",
  resumable: "runtime",
  adoptable: "runtime",
  bootstrapping: "runtime",
  "managed-dep": "managed",
  "zcp-self": "system",
};
ui.services.forEach((s) => {
  assert.ok(s.partKind in expectCategory, "unexpected partKind: " + s.partKind);
  assert.strictEqual(
    s.category,
    expectCategory[s.partKind],
    "category mismatch for partKind=" + s.partKind
  );
});

// The view-map's required key set is frozen: adding/removing one is a
// deliberate seam change that must update this list AND every card reading it.
const REQUIRED_SERVICE_KEYS = [
  "id",
  "hostname",
  "type",
  "status",
  "partKind",
  "category",
  "isInfrastructure",
  "subdomainUrl",
  "mountPath",
];

// mountPath carries through from discover (the per-service local code dir the
// deploy card pushes from); empty when the service isn't locally mounted.
assert.strictEqual(byId["svc-adoptable"].mountPath, "/var/www/web");
assert.strictEqual(byId["svc-adopted"].mountPath, "");

ui.services.forEach((s) => {
  REQUIRED_SERVICE_KEYS.forEach((k) => {
    assert.ok(
      Object.prototype.hasOwnProperty.call(s, k),
      "mapped service missing required key: " + k
    );
  });
});

// CATEGORY_BY_KIND covers exactly ZCP's six AdoptionState buckets — no drift.
assert.deepStrictEqual(
  Object.keys(CATEGORY_BY_KIND).sort(),
  ["adopted", "adoptable", "bootstrapping", "managed-dep", "resumable", "zcp-self"].sort()
);

console.log("discover_to_uimap.test.js OK");
