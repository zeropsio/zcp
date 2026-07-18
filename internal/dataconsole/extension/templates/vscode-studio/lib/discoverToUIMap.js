"use strict";

// discoverToUIMap — the §7.3 MUST-PIN seam (E3).
//
// Pure function: maps the discover JSON (the exact shape `zcp studio topology`
// and the zerops_discover MCP tool emit) into the cockpit's view map. The
// cockpit reads ONLY this map, so every key a card needs must be produced
// here. Drop a read and the view silently degrades — a test (studiojs/
// discover_to_uimap.test.js) feeds a fixture with distinct values per field
// and asserts each one survives the mapping, for all six AdoptionState
// buckets.
//
// SINGLE-OWNER classification: `partKind` is the service's `adoptionState`
// verbatim — the six-bucket enum ZCP's tools.EnrichWithMetaStatus owns. It is
// NEVER re-derived here (re-classifying would let the cockpit drift from the
// agent-facing tool). `category` is only a coarse display grouping derived
// from that one owner.

// Coarse display grouping over the six AdoptionState buckets. runtime services
// (adopted / resumable / adoptable / bootstrapping) deploy code; managed-dep is
// platform infrastructure (db/cache/storage); zcp-self is the control plane.
const CATEGORY_BY_KIND = {
  "adopted": "runtime",
  "resumable": "runtime",
  "adoptable": "runtime",
  "bootstrapping": "runtime",
  "managed-dep": "managed",
  "zcp-self": "system",
};

function categoryFor(partKind) {
  return Object.prototype.hasOwnProperty.call(CATEGORY_BY_KIND, partKind)
    ? CATEGORY_BY_KIND[partKind]
    : "runtime";
}

function discoverToUIMap(discover) {
  const d = discover || {};
  const project = d.project || {};
  const services = Array.isArray(d.services) ? d.services : [];
  return {
    project: {
      id: project.id || "",
      name: project.name || "",
      status: project.status || "",
    },
    services: services.map(function (s) {
      const svc = s || {};
      const partKind = svc.adoptionState || "";
      return {
        id: svc.serviceId || "",
        hostname: svc.hostname || "",
        type: svc.type || "",
        status: svc.status || "",
        partKind: partKind,
        category: categoryFor(partKind),
        isInfrastructure: !!svc.isInfrastructure,
        subdomainUrl: svc.subdomainUrl || "",
        // mountPath is the service's local code dir when present (an SSHFS mount
        // in-container; empty on a laptop where the workspace root IS the code).
        // The deploy card/handler uses it as the push working-dir so deploy
        // works in both modes — see handlers/deploy.js.
        mountPath: svc.mountPath || "",
      };
    }),
    warnings: Array.isArray(d.warnings) ? d.warnings : [],
  };
}

module.exports = { discoverToUIMap, categoryFor, CATEGORY_BY_KIND };
