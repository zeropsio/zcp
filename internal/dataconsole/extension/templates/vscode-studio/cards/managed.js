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
      // Same narrow-panel vertical card the Runtime rows use (shared .zs-row):
      // head = icon + title block (host over type) + status badge; then the
      // Browse action on its own line, right-aligned.
      return (
        '<li class="zs-row zs-row-link" data-action="openConsole" data-service="' + host + '">' +
        '<div class="zs-rowhead">' +
        icon +
        '<div class="zs-rowmain">' +
        '<span class="zs-host">' + host + "</span>" +
        '<span class="zs-svc-type">' + escapeHtml(s.type) + "</span>" +
        "</div>" +
        '<span class="zs-badge' + ok + '">' + escapeHtml(s.status) + "</span>" +
        "</div>" +
        '<div class="zs-rowact"><span class="zs-actbtns">' +
        '<button class="zs-btn zs-btn-sm" data-action="openConsole" data-service="' + host +
        '">Browse data →</button>' +
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
