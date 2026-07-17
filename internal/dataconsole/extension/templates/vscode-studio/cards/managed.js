"use strict";

// Managed card — the project's MANAGED services (category==='managed': the
// managed-dep bucket — db / cache / storage / search / …). One row per service;
// this card is the SINGLE owner of managed presentation (the old console.js
// decorative list + the managed slice of parts.js collapsed here).
//
// Each row carries one "open this service's data" affordance: the whole row (and
// an explicit "Browse data →" button) posts openConsole, which opens the Data
// Console EMBEDDED as a native WebviewPanel deep-linked to that service. Write mode
// is NOT a separate card button — it is a host-confirmed toggle INSIDE the embedded
// panel (the server-side per-request write token is the mutation boundary).
//
// Managed services never serve a subdomain, so there is no live-URL affordance here
// (that is the Runtime card's job) — this keeps the two categories cleanly split.

const { iconFor } = require("../lib/svc-icons");

function escapeHtml(s) {
  return String(s == null ? "" : s).replace(/[&<>"']/g, function (c) {
    return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
  });
}

function statusOk(s) {
  return /^(ACTIVE|RUNNING)$/i.test(String(s || ""));
}

// baseType strips the mode/version decoration ("postgresql:single@18" ->
// "postgresql"), matching the console's own provider.BaseType.
function baseType(type) {
  const s = String(type == null ? "" : type).toLowerCase().trim();
  const i = s.search(/[:@]/);
  return i >= 0 ? s.slice(0, i) : s;
}

// tierFor mirrors the console's SUPPORT classification (provider.SupportFor) so a
// card can never offer Browse on a service the console renders as not-yet, nor
// imply full editing on a view-only engine (U-08 — the cards used to Browse
// EVERYTHING). "ready" = full (browse + edit), "view" = view-only (browse only),
// "not-yet" = no console support (Browse disabled). Kept a thin mirror of the Go
// owner; the console's /api/services is still the authority the panel enforces.
const TIER_FULL = { "object-storage": 1, "objectstorage": 1, "postgresql": 1, "mariadb": 1, "mysql": 1, "valkey": 1, "elasticsearch": 1, "meilisearch": 1, "typesense": 1 };
const TIER_VIEW = { "clickhouse": 1, "qdrant": 1, "kafka": 1, "nats": 1 };
function tierFor(type) {
  const b = baseType(type);
  if (TIER_FULL[b]) return "ready";
  if (TIER_VIEW[b]) return "view";
  return "not-yet";
}
function tierBadge(tier) {
  const label = tier === "ready" ? "ready" : tier === "view" ? "view-only" : "not yet";
  return '<span class="zs-tier zs-tier-' + tier + '">' + label + "</span>";
}

// Stable, deterministic row order: sort by hostname so the list NEVER reshuffles
// on a topology refresh (discover/API order is not guaranteed stable). Order
// changes only when a service is added / removed / renamed.
function byHostname(a, b) {
  const ha = (a && a.hostname) || "";
  const hb = (b && b.hostname) || "";
  return ha < hb ? -1 : ha > hb ? 1 : 0;
}

function render(uiMap) {
  const services = (uiMap && uiMap.services) || [];
  const managed = services
    .filter(function (s) {
      return s && s.category === "managed";
    })
    .sort(byHostname);

  if (!managed.length) {
    return (
      '<section class="zs-card">' +
      "<h2>Managed · data</h2>" +
      '<p class="zs-muted">No managed services in this project.</p>' +
      "</section>"
    );
  }

  const rows = managed
    .map(function (s) {
      const host = escapeHtml(s.hostname);
      const ok = statusOk(s.status) ? " ok" : "";
      // The service's own brand icon (same one the Zerops dashboard shows), in a
      // white chip so dark-coloured logos stay legible on the dark panel. The SVG
      // is a trusted bundled asset (not user input) — inlined verbatim, allowed
      // under the webview CSP (inline markup, not a resource load).
      const svg = iconFor(s.type);
      const icon = svg ? '<span class="zs-svc-icon">' + svg + "</span>" : '<span class="zs-svc-icon zs-svc-icon-none"></span>';
      // Support tier gates the open affordance, matching the console rail (U-08): a
      // not-yet service is NOT browsable, so its row is not a link and its Browse
      // button is disabled with the reason; a view-only service opens but is labelled.
      const tier = tierFor(s.type);
      const browsable = tier !== "not-yet";
      const rowOpen = browsable ? ' zs-row-link" data-action="openConsole" data-service="' + host + '"' : '"';
      const browseBtn = browsable
        ? '<button class="zs-btn zs-btn-sm" data-action="openConsole" data-service="' + host + '">Browse data →</button>'
        : '<button class="zs-btn zs-btn-sm" disabled title="This service type is not yet browsable in the Data Console.">Not yet browsable</button>';
      // Same narrow-panel vertical card the Runtime rows use (shared .zs-row):
      // head = icon + title block (host over type) + tier + status badge; then the
      // Browse action on its own line, right-aligned.
      return (
        '<li class="zs-row' + rowOpen + ">" +
        '<div class="zs-rowhead">' +
        icon +
        '<div class="zs-rowmain">' +
        '<span class="zs-host">' + host + "</span>" +
        '<span class="zs-svc-type">' + escapeHtml(s.type) + "</span>" +
        "</div>" +
        tierBadge(tier) +
        '<span class="zs-badge' + ok + '">' + escapeHtml(s.status) + "</span>" +
        "</div>" +
        '<div class="zs-rowact"><span class="zs-actbtns">' +
        browseBtn +
        "</span></div>" +
        "</li>"
      );
    })
    .join("");

  return (
    '<section class="zs-card">' +
    '<div class="zs-cardhead">' +
    "<h2>Managed · data</h2>" +
    "</div>" +
    '<p class="zs-muted">Browse managed-service data.</p>' +
    '<ul class="zs-list">' + rows + "</ul>" +
    '<p class="zs-status" data-console-status></p>' +
    "</section>"
  );
}

// clientScript: paints the transient console-launch status ("starting console…" /
// "open (read-only)") posted by handlers/console.js. textContent only; IIFE.
const clientScript = [
  "(function(){",
  '  window.addEventListener("message", function (ev) {',
  "    var m = ev && ev.data;",
  '    if (!m || m.type !== "console-status") return;',
  "    var el = document.querySelector('[data-console-status]');",
  "    if (el) el.textContent = m.message || '';",
  "  });",
  "})();",
].join("\n");

module.exports = { id: "managed", title: "Managed", order: 20, render: render, clientScript: clientScript };
