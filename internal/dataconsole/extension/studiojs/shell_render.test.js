"use strict";

// Shell render contract: the branded shell embeds the project name, locks the
// webview script to a CSP nonce, composes each discovered card's fragment, and
// surfaces the run-`zcp init` call-to-action when the transport reports the
// project isn't initialized. Loads the real extension.js through the vscode
// stub (require("vscode") -> ./vscode-stub).

const assert = require("assert");
require("./vscode-stub"); // installs the require("vscode") hook — must precede the extension require
const ext = require("../templates/vscode-studio/extension");

const uiMap = {
  project: { id: "p", name: "my-proj", status: "ACTIVE" },
  services: [],
  warnings: [],
};
const card = {
  id: "parts",
  title: "Parts",
  render: (m) => "<section>X-" + m.services.length + "</section>",
};

const html = ext.renderShell(uiMap, [card], "NONCE123");
assert.ok(html.indexOf("my-proj") >= 0, "project name in branded header");
assert.ok(html.indexOf("nonce-NONCE123") >= 0, "CSP nonce present");
assert.ok(
  html.indexOf("script-src 'nonce-NONCE123'") >= 0,
  "script-src locked to the nonce (no inline/unsafe script)"
);
assert.ok(html.indexOf("<section>X-0</section>") >= 0, "card fragment composed into shell");
assert.ok(html.indexOf("acquireVsCodeApi()") >= 0, "webview->host bridge present");
assert.ok(html.indexOf("data-action") >= 0, "generic event-delegation hook present");

// Orphan-proofing (S2): "· Managed" is glued with &nbsp; so a narrow-panel
// line break can only land before the middle dot, never strand it alone at a
// line's end. A plain "Zerops · Managed Data" (breakable space) is the
// regression this guards against.
assert.ok(html.indexOf("Zerops ·&nbsp;Managed Data") >= 0, "brand heading glues the middle dot to \"Managed\" with a non-breaking space");

// A card that throws on render must not blow up the shell (R-FLEET blast-radius).
const bad = { id: "bad", title: "Bad", render: () => { throw new Error("boom"); } };
const safe = ext.renderShell(uiMap, [bad, card], "N");
assert.ok(safe.indexOf("<section>X-0</section>") >= 0, "a throwing card is isolated; others still render");

// CTA names `zcp init` when the transport needs initialization (L-ON-1).
const cta = ext.renderCTA({ needsInit: true }, "N2");
assert.ok(cta.indexOf("zcp init") >= 0, "CTA must point the user at `zcp init`");
assert.ok(cta.indexOf("Zerops ·&nbsp;Managed Data") >= 0, "CTA brand heading also glues the middle dot to \"Managed\"");

console.log("shell_render.test.js OK");
