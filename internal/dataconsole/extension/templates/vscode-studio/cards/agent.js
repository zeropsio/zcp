"use strict";

// Agent card — S4's vertical feature (L-AG-1/L-AG-2/L-AG-3/L-AG-4).
//
// Single-agent only: Claude Code. No multi-agent set, no auth-mode matrix
// (L-AG-3). The probe (agent-status handler) is the ONLY direct file read in the
// product: ~/.claude/.credentials.json existence (NN6). Authorization happens in
// Claude Code's own /login, never in Studio (L-AG-2) — when unauthorized we link
// out with plain text. The launch button opens Claude Code with the DEFAULT
// safety posture — never --dangerously-skip-permissions (L-AG-4 / R-SEC-LOCAL).
//
// The static render asserts nothing about authorization; the badge reads
// "checking…" until the host's {type:"agent-status-result"} message arrives.

function escapeHtml(s) {
  return String(s == null ? "" : s).replace(/[&<>"']/g, function (c) {
    return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
  });
}

function render(uiMap) {
  // uiMap unused: agent status is machine-local, not a topology property.
  void uiMap;
  return (
    '<section class="zs-card">' +
    "<h2>Claude Code</h2>" +
    '<p class="zs-status">Status: <span id="zs-agent-status" class="zs-badge">checking…</span></p>' +
    '<p id="zs-agent-hint" class="zs-muted"></p>' +
    '<div class="zs-actions">' +
    '<button class="zs-btn zs-btn-primary zs-btn-sm" data-action="agent-launch">Launch Claude Code</button>' +
    '<button class="zs-btn zs-btn-sm" data-action="agent-status">Re-check</button>' +
    "</div>" +
    "</section>"
  );
}

// clientScript: on load, requests status; on {type:"agent-status-result"} writes
// the verdict (teal when authorized) + a text-only /login hint otherwise. No
// login flow runs in Studio (L-AG-2). IIFE to avoid global collisions.
const clientScript = [
  "(function(){",
  '  function api(){ return (typeof vscodeApi !== "undefined") ? vscodeApi : null; }',
  '  function requestStatus(){ var a = api(); if (a) a.postMessage({ type: "agent-status" }); }',
  '  window.addEventListener("message", function (ev) {',
  "    var m = ev && ev.data;",
  '    if (!m || m.type !== "agent-status-result") return;',
  '    var badge = document.getElementById("zs-agent-status");',
  '    var hint = document.getElementById("zs-agent-hint");',
  "    if (badge) {",
  '      badge.className = m.authorized ? "zs-badge ok" : "zs-badge";',
  '      badge.textContent = m.authorized ? "Authorized" : "Not authorized";',
  "    }",
  "    if (hint) {",
  "      hint.textContent = m.authorized",
  '        ? ""',
  '        : "Launch Claude Code and run /login to authorize — Studio never collects your credentials.";',
  "    }",
  "  });",
  "  requestStatus();",
  "})();",
].join("\n");

module.exports = { id: "agent", title: "Claude Code", order: 40, render: render, clientScript: clientScript };
