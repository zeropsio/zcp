"use strict";

// sync-env handler (L-EV-1).
//
// Shells `zcp studio sync-env` through ctx.runVerb — the ONLY sanctioned path to
// the binary (never spawn zcp/zcli here, never read .zcp/state; LG5). The verb
// returns { ok, data, error } where `data` is the parsed EnvDotenvResult JSON
// ({ path, setup, variables, diff, omittedPlatformKeys, warnings }). We post the
// outcome back to the webview; the env-vpn card's clientScript paints it inline.

async function handle(msg, ctx) {
  const r = await ctx.runVerb(["sync-env"]);
  if (r && r.ok) {
    ctx.postMessage({
      type: "sync-env-result",
      ok: true,
      variables: r.data && r.data.variables,
      path: r.data && r.data.path,
    });
  } else {
    ctx.postMessage({
      type: "sync-env-result",
      ok: false,
      message: (r && r.error) || "sync-env failed",
    });
  }
}

module.exports = { type: "sync-env", handle: handle };
