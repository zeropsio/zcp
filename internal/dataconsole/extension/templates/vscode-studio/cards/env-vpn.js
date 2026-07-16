"use strict";

// Env & VPN card — S3's slice (L-EV-1/L-EV-2/L-EV-3).
//
//   1. "Sync .env" (L-EV-1) — posts `sync-env`; handlers/sync-env.js shells
//      `zcp studio sync-env` and posts the result, painted inline.
//   2. The VPN boundary in TWO TIERS (L-EV-2 / R-VPN-COPY) — never a blanket
//      "no VPN". Tier 1 (deploy/preview/sync) needs none; Tier 2 (your LOCAL app
//      → a managed DB/cache) needs the project VPN. The exact
//      `zcli vpn up <projectId>` is rendered from live data (uiMap.project.id),
//      with a "Check VPN" reachability probe (L-EV-3) posting `vpn-status`.
//
// render is pure + host-side; everything interpolated is escaped. The
// clientScript IIFE only reads inbound host messages and writes via textContent.

function escapeHtml(s) {
  return String(s == null ? "" : s).replace(/[&<>"']/g, function (c) {
    return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
  });
}

// Both tiers ALWAYS render together — the Tier-1 "no VPN" is true only because
// the Tier-2 line stands beside it. Dropping either would be a blanket claim.
const TIER1_COPY = "Tier 1 — deploy, preview, and sync env need no VPN.";
const TIER2_COPY =
  "Tier 2 — to run your local app against a managed service (DB/cache), connect the project VPN:";

function render(uiMap) {
  const projectId = (uiMap && uiMap.project && uiMap.project.id) || "";
  const vpnCommand = "zcli vpn up " + projectId;
  return (
    '<section class="zs-card">' +
    "<h2>Env &amp; VPN</h2>" +
    '<div class="zs-actions"><button class="zs-btn" data-action="sync-env">Sync .env</button></div>' +
    '<p class="zs-status" data-zs-env-result></p>' +
    '<p class="zs-muted">' + escapeHtml(TIER1_COPY) + "</p>" +
    '<p class="zs-muted">' + escapeHtml(TIER2_COPY) + "</p>" +
    '<code class="zs-code">' + escapeHtml(vpnCommand) + "</code>" +
    '<div class="zs-actions" style="margin-top:9px"><button class="zs-btn zs-btn-sm" data-action="vpn-status">Check VPN</button></div>' +
    '<p class="zs-status" data-zs-vpn-result></p>' +
    "</section>"
  );
}

const clientScript =
  "(function(){" +
  'window.addEventListener("message",function(ev){' +
  "var m=ev.data||{};" +
  'if(m.type==="sync-env-result"){' +
  'var el=document.querySelector("[data-zs-env-result]");if(!el)return;' +
  "if(m.ok){" +
  "var v=m.variables;" +
  'var count=Array.isArray(v)?v.length:(v&&typeof v==="object"?Object.keys(v).length:v);' +
  'el.className="zs-status ok";' +
  'el.textContent="Synced "+(count==null?"":count+" ")+"var(s) to "+(m.path||".env");' +
  "}else{" +
  'el.className="zs-status";' +
  'el.textContent="Sync failed: "+(m.message||"unknown error");' +
  "}" +
  '}else if(m.type==="vpn-status-result"){' +
  'var r=document.querySelector("[data-zs-vpn-result]");if(!r)return;' +
  "if(m.connected===null||m.connected===undefined){" +
  'r.className="zs-status";r.textContent=m.note||"No managed services to reach.";' +
  "}else if(m.connected){" +
  'r.className="zs-status ok";r.textContent="Reached "+(m.managedHost||"the managed service")+" — VPN looks connected.";' +
  "}else{" +
  'r.className="zs-status";r.textContent="Could not reach "+(m.managedHost||"the managed service")+" — if your local app needs it, run the command above.";' +
  "}" +
  "}" +
  "});" +
  "})();";

module.exports = { id: "env-vpn", title: "Env & VPN", order: 30, render: render, clientScript: clientScript };
