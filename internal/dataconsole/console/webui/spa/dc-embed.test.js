"use strict";

const assert = require("assert");
const embed = require("../dist/dc-embed");

// isBrowsable mirrors the SPA's `supported()` predicate (supported | view-only).
const isBrowsable = (s) => s.support === "supported" || s.support === "view-only";
const services = [
  { hostname: "db", support: "supported" },
  { hostname: "search", support: "view-only" },
  { hostname: "files", support: "not yet" },
];

assert.strictEqual(
  embed.shouldHideServiceRail({ embedded: true, deepLinkedService: "db", services, isBrowsable }),
  true,
  "embedded + deep-linked browsable service hides the duplicate rail"
);
assert.strictEqual(
  embed.shouldHideServiceRail({ embedded: true, deepLinkedService: "search", services, isBrowsable }),
  true,
  "a view-only deep-link still hides the rail (it is browsable)"
);
assert.strictEqual(
  embed.shouldHideServiceRail({ embedded: false, deepLinkedService: "db", services, isBrowsable }),
  false,
  "standalone (own tab) keeps the rail even when deep-linked"
);
assert.strictEqual(
  embed.shouldHideServiceRail({ embedded: true, deepLinkedService: null, services, isBrowsable }),
  false,
  "embedded without a target (the edit-data entry) keeps the rail so the user can pick"
);
assert.strictEqual(
  embed.shouldHideServiceRail({ embedded: true, deepLinkedService: "ghost", services, isBrowsable }),
  false,
  "an unknown deep-link service keeps the rail (never strand the user)"
);
assert.strictEqual(
  embed.shouldHideServiceRail({ embedded: true, deepLinkedService: "files", services, isBrowsable }),
  false,
  "a not-yet (unbrowsable) deep-link keeps the rail as an escape hatch"
);
assert.strictEqual(
  embed.shouldHideServiceRail({ embedded: true, deepLinkedService: "db", services: [], isBrowsable }),
  false,
  "no loaded services means no resolvable target — keep the rail"
);

console.log("dc-embed.test.js OK");
