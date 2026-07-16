"use strict";

// Branding + opt-in docs contract (L-BR-1 / L-BR-3).
//
// The standalone welcome card is gone: the brand header lives in the shell
// (logo + project name), and the opt-in "Zerops docs ↗" link moved into the
// FOOTER card (cards/refresh.js). This test pins:
//   * the footer card offers the opt-in "Zerops docs" action (data-action=
//     "open-welcome") with branded copy;
//   * open-welcome exports its `type` + a handle fn;
//   * the dropped "apply-branding" affordance/handler stays gone;
//   * no slice file wires onStartupFinished (L-BR-3 no auto-open).
//
// Plain node: open-welcome requires("vscode") LAZILY inside handle(), so requiring
// the module is clean; its law is pinned by reading the source.

const assert = require("assert");
const fs = require("fs");
const path = require("path");

const footer = require("../templates/vscode-studio/cards/refresh");
const openWelcome = require("../templates/vscode-studio/handlers/open-welcome");

function fail(msg) {
  console.error("branding.test.js FAIL: " + msg);
  process.exit(1);
}

try {
  // (1) Footer render — the opt-in branded docs action.
  const html = footer.render({ project: { id: "p-1", name: "demo", status: "ACTIVE" }, services: [] });
  assert.ok(
    html.indexOf('data-action="open-welcome"') >= 0,
    'footer must include the opt-in data-action="open-welcome" docs link'
  );
  assert.ok(html.toLowerCase().indexOf("zerops") >= 0, "footer must carry branded Zerops copy");

  // The dropped "Apply Zerops accent" affordance must NOT reappear.
  assert.ok(
    html.indexOf('data-action="apply-branding"') < 0,
    'the confusing "apply-branding" affordance must be gone'
  );

  // A hostile zcp-self hostname can't break out of the footer's system line.
  const evil = footer.render({
    services: [{ hostname: "<img src=x onerror=1>", status: "ACTIVE", category: "system" }],
  });
  assert.ok(evil.indexOf("<img src=x") < 0, "system hostname must be HTML-escaped in the footer");

  // (2) open-welcome handler shape.
  assert.strictEqual(openWelcome.type, "open-welcome", "open-welcome handler type");
  assert.strictEqual(typeof openWelcome.handle, "function", "open-welcome handle is a function");

  // (3) The apply-branding handler file must be gone (slice slimmed).
  assert.ok(
    !fs.existsSync(path.join(__dirname, "../templates/vscode-studio/handlers/apply-branding.js")),
    "handlers/apply-branding.js must be removed"
  );

  // (4) L-BR-3: no slice file wires onStartupFinished (no auto-open).
  const sliceFiles = [
    "../templates/vscode-studio/cards/refresh.js",
    "../templates/vscode-studio/handlers/open-welcome.js",
  ];
  for (const rel of sliceFiles) {
    const src = fs.readFileSync(path.join(__dirname, rel), "utf8");
    assert.ok(
      src.indexOf("onStartupFinished") < 0,
      rel + " must NOT reference onStartupFinished (L-BR-3: no auto-open)"
    );
  }

  console.log("branding.test.js OK");
} catch (err) {
  fail((err && err.message) || String(err));
}
