"use strict";

// Footer card — the panel's quiet bottom strip. It owns three things that are not
// services and not actions you take often:
//
//   1. The live-sync tick (L-CO-4). PRIMARY sync path is the host websocket watch
//      (`zcp studio watch`): it pushes topology-change events and the shell
//      debounce-refreshes — sub-second, no polling. This card's webview poll is the
//      FALLBACK, active only while the watch is NOT connected (the shell posts
//      {type:"watch-connected"}/{type:"watch-disconnected"}; the clientScript pauses
//      on connect, resumes on disconnect). The tick shows which mode is live.
//   2. The control-plane line. zcp-self (category==='system') is neither a runtime
//      you deploy nor a managed service you browse — it is ZCP itself running in the
//      project. It gets one muted, action-less line so the topology reads as honest
//      and complete without a fake affordance.
//   3. The opt-in "Zerops docs ↗" link (open-welcome handler → openExternal). The
//      brand header lives in the shell, so there is no separate welcome card.
//
// Honesty floor: render asserts nothing about project state; it reuses the shell's
// `vscodeApi` (acquireVsCodeApi is called once, by the shell) and never auto-opens.

function escapeHtml(s) {
  return String(s == null ? "" : s).replace(/[&<>"']/g, function (c) {
    return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
  });
}

function render(uiMap) {
  const services = (uiMap && uiMap.services) || [];
  const system = services
    .filter(function (s) {
      return s && s.category === "system";
    })
    .sort(function (a, b) {
      const ha = (a && a.hostname) || "";
      const hb = (b && b.hostname) || "";
      return ha < hb ? -1 : ha > hb ? 1 : 0;
    });
  const sysLine = system.length
    ? '<p class="zs-sys">' +
      system
        .map(function (s) {
          return (
            "<span>" +
            escapeHtml(s.hostname) +
            " · control plane · " +
            escapeHtml(String(s.status || "").toLowerCase()) +
            "</span>"
          );
        })
        .join(" ") +
      "</p>"
    : "";

  return (
    '<section class="zs-card zs-footer">' +
    '<p class="zs-tick"><span class="zs-dot" data-zs-sync-tick title="connecting…"></span> ' +
    '<span data-zs-sync-label>live</span></p>' +
    sysLine +
    '<div class="zs-actions"><button class="zs-btn zs-btn-sm" data-action="open-welcome">Zerops docs ↗</button></div>' +
    "</section>"
  );
}

// clientScript: the FALLBACK poll. Posts {type:"refresh"} every ~8s via the shell's
// existing `vscodeApi` (NOT acquireVsCodeApi — once per webview), but PAUSES while
// the host websocket watch is connected ({type:"watch-connected"}) and RESUMES on
// {type:"watch-disconnected"}. The tick label reflects the mode. IIFE. No auto-open.
const clientScript = [
  "(function(){",
  "  var INTERVAL_MS = 8000;",
  '  var dot = document.querySelector("[data-zs-sync-tick]");',
  '  var label = document.querySelector("[data-zs-sync-label]");',
  "  var timer = null;",
  "  function poll(){",
  '    if (typeof vscodeApi === "undefined" || !vscodeApi) return;',
  '    vscodeApi.postMessage({ type: "refresh" });',
  "  }",
  "  function startPoll(){ if (!timer) timer = setInterval(poll, INTERVAL_MS); }",
  "  function stopPoll(){ if (timer) { clearInterval(timer); timer = null; } }",
  '  window.addEventListener("message", function(ev){',
  "    var m = ev && ev.data; if (!m) return;",
  '    if (m.type === "watch-connected") {',
  "      stopPoll();",
  '      if (label) label.textContent = "live";',
  '      if (dot) dot.setAttribute("title", "live (push)");',
  '    } else if (m.type === "watch-disconnected") {',
  "      startPoll();",
  '      if (label) label.textContent = "syncing";',
  '      if (dot) dot.setAttribute("title", "polling (fallback)");',
  "    }",
  "  });",
  "  startPoll(); // until the watch confirms it is connected",
  "})();",
].join("\n");

module.exports = { id: "refresh", title: "Footer", order: 50, render: render, clientScript: clientScript };
