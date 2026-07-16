"use strict";

// Webview -> host message allowlist (docs/spec-welcome-mode.md §8, W-SEC):
// exactly "ready" and "open-url" (with an allowlisted url) do anything;
// everything else — a non-allowlisted url, an unknown type — is silently
// dropped, never thrown, never surfaced to the user.

const test = require("node:test");
const assert = require("node:assert/strict");
const { loadExtension } = require("./harness.js");

async function openWelcome() {
  const { stub, extension, extensionDir } = loadExtension();
  await extension.activate({ subscriptions: [], extensionPath: extensionDir });
  const handler = stub.registeredCommands.get("zerops.welcome");
  handler();
  const panel = stub.panels.find((p) => p.viewType === "zeropsWelcome");
  return { stub, panel };
}

test("open-url with a non-allowlisted url is dropped", async () => {
  const { stub, panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "open-url", url: "https://evil.example.com" });

  assert.equal(stub.openedExternalUrls.length, 0);
});

test("open-url with an allowlisted url opens externally", async () => {
  const { stub, panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "open-url", url: "https://docs.zerops.io" });

  assert.deepStrictEqual(stub.openedExternalUrls, ["https://docs.zerops.io"]);
});

test("an unknown message type does nothing", async () => {
  const { stub, panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "not-a-real-type", url: "https://docs.zerops.io" });

  assert.equal(stub.openedExternalUrls.length, 0);
  assert.equal(panel.postedMessages.filter((m) => m.type === "state").length, 0);
});
