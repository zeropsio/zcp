"use strict";

// refresh handler — S6 (type "refresh", L-CO-4).
//
// Receives the webview timer's {type:"refresh"} message (posted by the Sync
// card's clientScript every ~8s) and drives a LIVE topology refresh with no-op
// suppression:
//
//   1. ctx.runTransport() re-polls topology via the frozen E3 transport
//      (`zcp studio topology` -> discoverToUIMap). Reads go ONLY through the
//      transport — never raw .zcp/state (LG5). On a transport error we bail.
//   2. Hash the fresh uiMap and compare against the last one we acted on. An
//      identical hash means the project hasn't changed since the last poll, so
//      we SUPPRESS the re-render (no flicker). A burst of identical polls
//      coalesces — only a real topology change repaints.
//   3. On a real change, record the new hash and call ctx.refreshTopology() so
//      the whole cockpit repaints from fresh truth. No mutations.
//
// lastHash is module-level: the router require()s this handler once at
// activation, so the closure persists across every refresh tick of the session.

let lastHash = null;

async function handle(msg, ctx) {
  const t = await ctx.runTransport();
  if (!t || !t.ok) return; // transport failed — leave the last good paint in place

  const hash = JSON.stringify(t.uiMap);
  if (hash === lastHash) return; // no-op: identical topology, suppress re-render

  lastHash = hash;
  await ctx.refreshTopology();
}

module.exports = { type: "refresh", handle: handle };
