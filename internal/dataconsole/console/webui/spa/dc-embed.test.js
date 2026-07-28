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

// §4.4: the SPA's own service rail is the service selector and stays visible
// WHENEVER the console is embedded — a deep link only preselects the target,
// it never hides the rail. (The former rule hid the rail on a deep link
// because the sidebar card list acted as the selector; that sidebar no longer
// exists post-S4, so hiding the rail would strand the user with no way to
// switch services.) Standalone has never hidden the rail — unchanged.
assert.strictEqual(
  embed.shouldHideServiceRail({ embedded: true, deepLinkedService: "db", services, isBrowsable }),
  false,
  "embedded + deep-linked browsable service: rail stays visible (deep link only preselects)"
);
assert.strictEqual(
  embed.shouldHideServiceRail({ embedded: true, deepLinkedService: "search", services, isBrowsable }),
  false,
  "embedded + a view-only deep-link: rail stays visible"
);
assert.strictEqual(
  embed.shouldHideServiceRail({ embedded: false, deepLinkedService: "db", services, isBrowsable }),
  false,
  "standalone (own tab) keeps the rail even when deep-linked — unchanged"
);
assert.strictEqual(
  embed.shouldHideServiceRail({ embedded: true, deepLinkedService: null, services, isBrowsable }),
  false,
  "embedded without a target keeps the rail so the user can pick"
);
assert.strictEqual(
  embed.shouldHideServiceRail({ embedded: true, deepLinkedService: "ghost", services, isBrowsable }),
  false,
  "embedded + an unknown deep-link service: rail stays visible (never strand the user)"
);
assert.strictEqual(
  embed.shouldHideServiceRail({ embedded: true, deepLinkedService: "files", services, isBrowsable }),
  false,
  "embedded + a not-yet (unbrowsable) deep-link: rail stays visible"
);
assert.strictEqual(
  embed.shouldHideServiceRail({ embedded: true, deepLinkedService: "db", services: [], isBrowsable }),
  false,
  "embedded with no loaded services yet: rail stays visible"
);
// Added: embedded with NO opts at all still resolves to visible (defensive —
// a caller can never accidentally hide the rail by omission).
assert.strictEqual(
  embed.shouldHideServiceRail(),
  false,
  "no opts at all: rail stays visible"
);

console.log("dc-embed.test.js OK");
