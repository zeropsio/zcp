"use strict";

// Deploy handler — S2 (type "deploy"). Receives the webview's
// { type:"deploy", service:"<hostname>" } message (emitted by the Deploy
// button's data-action/data-service via the shell's generic delegation) and
// drives the deploy through the frozen E3 transport.
//
// Flow:
//   1. Resolve the target hostname from msg.service; no service => no-op.
//   2. Post {type:"deploying"} so the card shows an advisory progress line.
//   3. ctx.runVerb(["deploy","--service",host, ...]) — the ONE sanctioned path
//      to ZCP (`zcp studio deploy ...`). When the card supplies msg.mount (the
//      service's local code dir — an SSHFS mount in-container), pass it as
//      --working-dir so the push runs from the right place; on a laptop mount is
//      empty and the verb defaults to cwd. Never spawn zcp/zcli here; never read
//      .zcp/state (LG5).
//   4. On success -> ctx.refreshTopology() so the new subdomainUrl + status
//      badge repaint from fresh truth (honesty floor: we don't fabricate the
//      post-deploy state, we re-read it).
//      On failure -> post {type:"deploy-error"} with the transport's error.
//
// L-DP-4 (strategy=git-push) is out of scope here (C-tier, non-default).

async function handle(msg, ctx) {
  const service = msg && msg.service;
  if (!service) return;

  ctx.postMessage({ type: "deploying", service: service });

  const args = ["deploy", "--service", service];
  if (msg.mount) {
    args.push("--working-dir", msg.mount);
  }
  const r = await ctx.runVerb(args);

  if (r && r.ok) {
    await ctx.refreshTopology();
    return;
  }

  ctx.postMessage({
    type: "deploy-error",
    service: service,
    message: (r && r.error) || "deploy failed",
  });
}

module.exports = { type: "deploy", handle: handle };
