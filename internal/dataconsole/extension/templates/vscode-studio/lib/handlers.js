"use strict";

// E2 — handler router.
//
// Discovers webview->host message handlers by scanning the handlers/ directory
// at ACTIVATION (R-FLEET: no import-time side effects, no hand-maintained
// central list). Each handlers/<name>.js exports:
//
//   module.exports = { type: string, handle(msg, ctx) -> void|Promise }
//
// The discovered set IS the webview->host allowlist: a message whose `type`
// isn't a discovered handler is dropped (never dispatched).
//
// Directory discovery makes FILENAMES collision-free but NOT the `type`
// namespace — two files declaring the same `type` would silently shadow at the
// router. buildRouter throws on a duplicate `type`, turning the one residual
// parallel-safety hazard into a hard failure instead of a silent shadow. Every
// slice therefore pins its handler `type` strings explicitly.

const fs = require("fs");
const path = require("path");

function enumerateHandlers(handlersDir) {
  let files;
  try {
    files = fs.readdirSync(handlersDir);
  } catch (_) {
    return [];
  }
  const handlers = [];
  for (const f of files.slice().sort()) {
    if (!f.endsWith(".js") || f.endsWith(".test.js")) continue;
    const mod = require(path.join(handlersDir, f));
    const h = mod && mod.default ? mod.default : mod;
    if (!h || typeof h.handle !== "function" || !h.type) {
      throw new Error("invalid handler module " + f + ": must export { type, handle }");
    }
    handlers.push(h);
  }
  return handlers;
}

// buildRouter turns a discovered handler list into { allow:Set, dispatch }.
// Throws on a duplicate `type` across the set (the E2 parallel-safety contract).
function buildRouter(handlers) {
  const table = new Map();
  for (const h of handlers) {
    if (table.has(h.type)) {
      throw new Error(
        "duplicate handler type " + JSON.stringify(h.type) +
          " — handler types must be unique across the discovered set"
      );
    }
    table.set(h.type, h);
  }
  return {
    allow: new Set(table.keys()),
    async dispatch(msg, ctx) {
      if (!msg || !table.has(msg.type)) return false; // not in allowlist — drop
      await table.get(msg.type).handle(msg, ctx);
      return true;
    },
    // dispose tears down any handler that owns long-lived resources (e.g. the
    // console handler's child processes) — wired to ctx.subscriptions so it runs
    // on extension deactivate.
    dispose() {
      for (const h of table.values()) {
        if (typeof h.dispose === "function") {
          try {
            h.dispose();
          } catch (_) {
            /* best effort */
          }
        }
      }
    },
  };
}

module.exports = { enumerateHandlers, buildRouter };
