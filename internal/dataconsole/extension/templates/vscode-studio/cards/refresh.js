"use strict";

// Footer card — the panel's quiet bottom strip. It owns the live-sync tick
// (L-CO-4): the PRIMARY sync path is the host websocket watch (`zcp studio
// watch`), which pushes topology-change events and the shell debounce-
// refreshes — sub-second, no polling. This card's webview poll is the
// FALLBACK, active only while the watch is NOT connected (the shell posts
// {type:"watch-connected"}/{type:"watch-disconnected"}; the clientScript
// pauses on connect, resumes on disconnect). The tick shows which mode is live.
//
// Honesty floor: render asserts nothing about project state; it reuses the
// shell's `vscodeApi` (acquireVsCodeApi is called once, by the shell).

function render(uiMap) {
  // uiMap unused: the footer carries no per-service content.
  void uiMap;
  return (
    '<section class="zs-card zs-footer">' +
    '<p class="zs-tick"><span class="zs-dot" data-zs-sync-tick title="connecting…"></span> ' +
    '<span data-zs-sync-label>live</span></p>' +
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
