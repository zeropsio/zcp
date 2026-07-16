"use strict";

// Runtime card + deploy seam test.
//
// (1) The card renders one row per RUNTIME service (category==='runtime'); a
//     service with a live subdomain gets the openUrl affordance (whole-row +
//     "Open live ↗" button), never a managed service. Deploy is shown only for
//     locally-deployable runtimes (the GLOBAL mount heuristic), and never for a
//     `bootstrapping` service. Managed services do not appear in this card at all.
// (2) handlers/deploy.js (UNCHANGED) declares the { type:"deploy", handle } shape
//     the router discovers (its `type` is the webview->host allowlist key).
// (3) handle() routes the deploy through ctx.runVerb(["deploy","--service",host])
//     and, on success, calls ctx.refreshTopology() so the new subdomainUrl repaints.

const assert = require("assert");
const card = require("../templates/vscode-studio/cards/runtime");
const handler = require("../templates/vscode-studio/handlers/deploy");

// ---- (1) card render --------------------------------------------------------
const uiMap = {
  project: { id: "p", name: "demo", status: "ACTIVE" },
  services: [
    {
      id: "svc-app",
      hostname: "app",
      type: "nodejs@22",
      status: "ACTIVE",
      partKind: "adopted",
      category: "runtime",
      isInfrastructure: false,
      subdomainUrl: "https://app-xyz.zerops.app",
    },
    {
      id: "svc-db",
      hostname: "db",
      type: "postgresql@16",
      status: "ACTIVE",
      partKind: "managed-dep",
      category: "managed",
      isInfrastructure: true,
      subdomainUrl: "",
    },
  ],
  warnings: [],
};

const html = card.render(uiMap);

assert.strictEqual(card.id, "runtime", "card id is 'runtime'");
assert.strictEqual(card.order, 10, "runtime card orders first (order=10)");

// Deploy button wired for the runtime service via data-action/data-service.
assert.ok(
  html.indexOf('data-action="deploy" data-service="app"') >= 0,
  "runtime service gets a Deploy button keyed to its hostname"
);

// Live subdomain becomes the openUrl affordance (NOT a bare <a href>): the row
// carries it and an explicit "Open live ↗" button targets the same URL.
assert.ok(
  html.indexOf('data-action="openUrl" data-url="https://app-xyz.zerops.app"') >= 0,
  "non-empty subdomainUrl renders an openUrl affordance"
);
assert.ok(html.indexOf("Open live") >= 0, "a prominent Open-live affordance is present");

// The managed dep must NOT appear in the Runtime card at all.
assert.ok(html.indexOf('data-service="db"') < 0, "managed dep gets no Deploy button");
assert.ok(html.indexOf(">db<") < 0, "managed dep is not listed in the Runtime card");

// Empty-runtime case (only a managed service) renders the muted note, no button.
const emptyHtml = card.render({ services: [uiMap.services[1]] });
assert.ok(
  emptyHtml.indexOf("No runtime services in this project") >= 0,
  "zero runtime services -> muted note"
);
assert.ok(emptyHtml.indexOf('data-action="deploy"') < 0, "no Deploy button when no runtime services");

// In-container heuristic: when SOME runtime is mounted, only the mounted ones are
// locally deployable; the unmounted one is still LISTED but gets no Deploy button.
const mixed = card.render({
  services: [
    { hostname: "mounted", type: "go@1", status: "ACTIVE", partKind: "adoptable", category: "runtime", mountPath: "/var/www/mounted", subdomainUrl: "" },
    { hostname: "unmounted", type: "go@1", status: "ACTIVE", partKind: "adoptable", category: "runtime", mountPath: "", subdomainUrl: "" },
  ],
});
assert.ok(mixed.indexOf('data-service="mounted"') >= 0, "mounted runtime is deployable");
assert.ok(mixed.indexOf('data-service="unmounted"') < 0, "unmounted runtime (no local code in-container) gets no Deploy");
assert.ok(mixed.indexOf("unmounted") >= 0, "unmounted runtime is still listed (informational)");
assert.ok(mixed.indexOf('data-mount="/var/www/mounted"') >= 0, "Deploy button carries the mount working-dir");

// A `bootstrapping` runtime is action-light: listed, but never Deploy-able yet.
const boot = card.render({
  services: [
    { hostname: "fresh", type: "nodejs@22", status: "CREATING", partKind: "bootstrapping", category: "runtime", mountPath: "", subdomainUrl: "" },
  ],
});
assert.ok(boot.indexOf("fresh") >= 0, "bootstrapping runtime is listed");
assert.ok(boot.indexOf('data-action="deploy"') < 0, "bootstrapping runtime gets no Deploy button");
assert.ok(boot.indexOf("setting up") >= 0, "bootstrapping shows a non-steady state tag");

// Stable order: rows sort by hostname regardless of the discover/API order, so a
// topology refresh never reshuffles the list.
const shuffled = card.render({
  services: [
    { hostname: "zebra", type: "go@1", status: "ACTIVE", partKind: "adopted", category: "runtime", mountPath: "", subdomainUrl: "" },
    { hostname: "alpha", type: "go@1", status: "ACTIVE", partKind: "adopted", category: "runtime", mountPath: "", subdomainUrl: "" },
    { hostname: "mid", type: "go@1", status: "ACTIVE", partKind: "adopted", category: "runtime", mountPath: "", subdomainUrl: "" },
  ],
});
assert.ok(
  shuffled.indexOf(">alpha<") < shuffled.indexOf(">mid<") &&
    shuffled.indexOf(">mid<") < shuffled.indexOf(">zebra<"),
  "runtime rows render sorted by hostname (stable across refreshes)"
);

// ---- (1b) narrow-panel layout ----------------------------------------------
// Each row is a VERTICAL card (head + actions + status line), not one crammed
// horizontal flex row: at sidebar width the old single line wrapped host/type,
// floated the badge, and let the deploy error crowd the buttons. The title
// block (host over type) lives in zs-rowmain inside zs-rowhead; the deploy
// progress/error gets its OWN full-width line; the "deploy to preview" nudge is
// a distinct zs-hint so an error paint can't restyle it.
assert.ok(html.indexOf('class="zs-rowhead"') >= 0, "row uses the stacked head layout (zs-rowhead)");
assert.ok(html.indexOf('class="zs-rowmain"') >= 0, "host+type share a title block (zs-rowmain)");

const devRow = card.render({
  services: [{ hostname: "web", type: "go@1", status: "ACTIVE", partKind: "adopted", category: "runtime", mountPath: "", subdomainUrl: "" }],
});
assert.ok(
  devRow.indexOf('class="zs-hint">deploy to preview') >= 0,
  "the deploy-to-preview nudge is a distinct zs-hint, not the status line"
);
assert.ok(
  devRow.indexOf('data-deploy-status="web"></span>') >= 0,
  "the deploy status/error renders on its own line, empty until the clientScript paints it"
);
assert.ok(
  card.clientScript.indexOf("zs-status-err") >= 0,
  "a failed deploy paints the status line with the error modifier (zs-status-err)"
);

// ---- (2) handler shape (handlers/deploy.js is unchanged) --------------------
assert.strictEqual(handler.type, "deploy", "handler type is exactly 'deploy'");
assert.strictEqual(typeof handler.handle, "function", "handler exports a handle function");

// ---- (3) handle() drives the transport + refresh ----------------------------
(async function () {
  const calls = { verb: [], posts: [], refreshed: 0 };
  const ctx = {
    runVerb: function (args) {
      calls.verb.push(args);
      return { ok: true, data: { status: "READY", subdomainUrl: "https://app-xyz.zerops.app" } };
    },
    refreshTopology: function () {
      calls.refreshed += 1;
    },
    postMessage: function (m) {
      calls.posts.push(m);
    },
  };

  // With a mountPath (in-container SSHFS mount), the handler pushes from it.
  await handler.handle({ type: "deploy", service: "app", mount: "/var/www/app" }, ctx);

  assert.strictEqual(calls.verb.length, 1, "runVerb called exactly once");
  assert.deepStrictEqual(
    calls.verb[0],
    ["deploy", "--service", "app", "--working-dir", "/var/www/app"],
    "runVerb invoked with the deploy verb args incl. the mount working-dir"
  );
  assert.strictEqual(calls.refreshed, 1, "refreshTopology called after a successful deploy");

  // Without a mount (laptop: workspace root is the code), no --working-dir.
  const noMount = { verb: [], refreshed: 0, runVerb: function (a) { noMount.verb.push(a); return { ok: true }; }, refreshTopology: function () { noMount.refreshed += 1; }, postMessage: function () {} };
  await handler.handle({ service: "app" }, noMount);
  assert.deepStrictEqual(noMount.verb[0], ["deploy", "--service", "app"], "no mount -> verb omits --working-dir");

  // No service -> no-op (no transport call, no refresh).
  const noop = { runVerb: function () { throw new Error("should not run"); }, refreshTopology: function () { throw new Error("should not refresh"); }, postMessage: function () {} };
  await handler.handle({ type: "deploy" }, noop);

  // Failure path posts a deploy-error and does NOT refresh.
  const failCalls = { posts: [], refreshed: 0 };
  const failCtx = {
    runVerb: function () { return { ok: false, error: "build failed" }; },
    refreshTopology: function () { failCalls.refreshed += 1; },
    postMessage: function (m) { failCalls.posts.push(m); },
  };
  await handler.handle({ service: "app" }, failCtx);
  assert.strictEqual(failCalls.refreshed, 0, "failed deploy does not refresh");
  assert.ok(
    failCalls.posts.some(function (m) { return m.type === "deploy-error" && m.message === "build failed"; }),
    "failed deploy posts a deploy-error with the transport error"
  );

  console.log("runtime.test.js OK");
})().catch(function (err) {
  console.error(err && err.stack ? err.stack : err);
  process.exit(1);
});
