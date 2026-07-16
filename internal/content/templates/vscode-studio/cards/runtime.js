"use strict";

// Runtime card — the project's RUNTIME services (category==='runtime': the four
// AdoptionState buckets adopted / resumable / adoptable / bootstrapping). One row
// per service; this card is the SINGLE owner of runtime presentation (the old
// deploy.js + the runtime slice of parts.js collapsed here — there is no longer a
// second card that re-lists the same services).
//
// Each row carries its two category-appropriate affordances:
//   • Open live  — when the service serves a subdomain, the row itself opens the
//     live URL (data-action="openUrl") AND a prominent "Open live ↗" button does
//     the same. Opening goes through the openUrl handler (vscode.env.openExternal
//     via asExternalUri), NOT a bare <a href>, so it is reliable in code-server too.
//   • Deploy     — the existing data-action="deploy" path (handler unchanged),
//     shown only when the service is locally deployable.
//
// Deploy gating keeps the GLOBAL mount heuristic centralized here (it must see all
// runtimes at once): in-container each service is a separate SSHFS mount, so
// deployable == has a mountPath; on a laptop nothing is mounted and the workspace
// root IS the (single) service's code, so all are deployable. `bootstrapping`
// services are action-light (still provisioning) — listed, never Deploy-able until
// the next refreshTopology() moves them out of that state.
//
// Honesty floor: the static render NEVER asserts a post-deploy state. Transient
// progress/error arrives via webview messages ({type:"deploying"}/{type:"deploy-
// error"}) and is painted by the clientScript; the authoritative new state (incl.
// a freshly-enabled subdomain link) is whatever the next refreshTopology() paints.

function escapeHtml(s) {
  return String(s == null ? "" : s).replace(/[&<>"']/g, function (c) {
    return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
  });
}

function statusOk(s) {
  return /^(ACTIVE|RUNNING)$/i.test(String(s || ""));
}

// Stable, deterministic row order: sort by hostname so the list NEVER reshuffles
// on a topology refresh (the platform/discover order is not guaranteed stable, and
// the websocket watch re-renders on every change). Order changes only when a
// service is added / removed / renamed — never just because its status moved.
function byHostname(a, b) {
  const ha = (a && a.hostname) || "";
  const hb = (b && b.hostname) || "";
  return ha < hb ? -1 : ha > hb ? 1 : 0;
}

// Show a state tag ONLY when the service is NOT in its steady state — the group
// header already says "Runtime", so tagging the common `adopted` case is noise.
const STATE_LABEL = {
  adoptable: "not adopted",
  resumable: "resuming",
  bootstrapping: "setting up",
};

function render(uiMap) {
  const services = (uiMap && uiMap.services) || [];
  const runtimes = services
    .filter(function (s) {
      return s && s.category === "runtime";
    })
    .sort(byHostname);

  if (!runtimes.length) {
    return (
      '<section class="zs-card">' +
      "<h2>Runtime</h2>" +
      '<p class="zs-muted">No runtime services in this project yet.</p>' +
      "</section>"
    );
  }

  // GLOBAL mount heuristic (see header): if ANY runtime is mounted we're
  // in-container, so only the mounted ones have local code here; otherwise (laptop)
  // all are deployable. Bootstrapping services are never deployable yet.
  const anyMounted = runtimes.some(function (s) {
    return s.mountPath;
  });

  const rows = runtimes
    .map(function (s) {
      const host = escapeHtml(s.hostname);
      const url = s.subdomainUrl ? escapeHtml(s.subdomainUrl) : "";
      const mount = escapeHtml(s.mountPath || "");
      const deployable =
        (!anyMounted || !!s.mountPath) && s.partKind !== "bootstrapping";

      const ok = statusOk(s.status) ? " ok" : "";
      const stateTag = STATE_LABEL[s.partKind]
        ? '<span class="zs-tag">' + escapeHtml(STATE_LABEL[s.partKind]) + "</span>"
        : "";

      const openLive = url
        ? '<button class="zs-btn zs-btn-sm zs-open" data-action="openUrl" data-url="' +
          url +
          '">Open live ↗</button>'
        : "";
      const deployBtn = deployable
        ? '<button class="zs-btn zs-btn-primary zs-btn-sm" data-action="deploy" data-service="' +
          host +
          '" data-mount="' +
          mount +
          '">Deploy</button>'
        : "";
      // A subtle nudge: a deployable runtime with no live URL yet gets one on its
      // first deploy (the stage subdomain auto-enables). Never a fake link.
      const hint =
        !url && deployable
          ? '<span class="zs-hint">deploy to preview</span>'
          : "";

      // Narrow-panel layout: stack the row into labelled lines instead of one
      // horizontal flex line that wrapped and overlapped at sidebar width.
      //   head    — title block (host over type, ellipsis) + state-tag + badge
      //   actions — "deploy to preview" hint (left) + action buttons (right),
      //             rendered only when there's something to show
      //   status  — transient deploy progress/error, full-width; stays empty
      //             (and collapses) until the clientScript paints into it
      const actionLine =
        hint || openLive || deployBtn
          ? '<div class="zs-rowact">' +
            hint +
            '<span class="zs-actbtns">' + openLive + deployBtn + "</span>" +
            "</div>"
          : "";

      // The whole row opens the live URL when there is one (data-action on the
      // <li>); the inner Deploy/Open-live buttons win via closest([data-action]).
      const rowAttrs = url
        ? ' class="zs-row zs-row-link" data-action="openUrl" data-url="' + url + '"'
        : ' class="zs-row"';

      return (
        "<li" + rowAttrs + ' data-deploy-row="' + host + '">' +
        '<div class="zs-rowhead">' +
        '<div class="zs-rowmain">' +
        '<span class="zs-host">' + host + "</span>" +
        '<span class="zs-svc-type">' + escapeHtml(s.type) + "</span>" +
        "</div>" +
        stateTag +
        '<span class="zs-badge' + ok + '">' + escapeHtml(s.status) + "</span>" +
        "</div>" +
        actionLine +
        '<span class="zs-status" data-deploy-status="' + host + '"></span>' +
        "</li>"
      );
    })
    .join("");

  return (
    '<section class="zs-card">' +
    "<h2>Runtime</h2>" +
    '<ul class="zs-list">' + rows + "</ul>" +
    "</section>"
  );
}

// clientScript: advisory deploy progress only (honesty floor). Listens for
// {type:"deploying"} / {type:"deploy-error"} and writes a transient line into the
// matching row's status span via textContent (no markup injection). IIFE.
const clientScript = [
  "(function(){",
  '  window.addEventListener("message", function (ev) {',
  "    var m = ev && ev.data;",
  "    if (!m || !m.service) return;",
  '    if (m.type !== "deploying" && m.type !== "deploy-error") return;',
  '    var el = document.querySelector(\'[data-deploy-status="\' + (window.CSS && CSS.escape ? CSS.escape(m.service) : m.service) + \'"]\');',
  "    if (!el) return;",
  '    if (m.type === "deploying") {',
  '      el.textContent = "deploying\\u2026";',
  '      el.className = "zs-status";',
  "    } else {",
  '      el.textContent = "deploy failed" + (m.message ? ": " + m.message : "");',
  '      el.className = "zs-status zs-status-err";',
  "    }",
  "  });",
  "})();",
].join("\n");

module.exports = { id: "runtime", title: "Runtime", order: 10, render: render, clientScript: clientScript };
