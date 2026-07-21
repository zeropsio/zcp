"use strict";
// Durable invariant (pins plans/dataconsole-audit/ui-walk.md "Posture
// presentation — correct [VERIFIED]"): the STANDALONE session (its own
// browser tab, bearer-only, never a write token — view-only by
// construction; see app.js `editing()`) always shows the read-only badge
// and always hides the write-mode toggle, so the UI never offers a false
// promise of write access. Must hold before and after the S15 renderer
// unification (excellence-program plan §6 DD-7).
//
// Complementary check: the EMBEDDED session shows the toggle instead of the
// badge, regardless of writeEnabled — per app.js renderWriteMode, embedded
// vs standalone (not writeEnabled) selects badge-vs-switch; writeEnabled
// only drives the switch's checked/"on" state once shown. Written out
// explicitly so a future implementer does not misread "hidden when not
// write-enabled" as an embedded-writeEnabled=false rule — it is not one.

const assert = require("assert");
const { buildConsole, waitFor, hostPostMessage, jsonRoute } = require("./harness");

const EMPTY_SERVICES = () => jsonRoute({ project: { id: "p1", name: "Proj" }, services: [], allowWrites: true });

async function main() {
  // ---- standalone: badge visible, switch hidden ----
  const c1 = buildConsole({
    url: "http://localhost/#t=FAKE",
    routes: (method, p) => (p === "/api/services" ? EMPTY_SERVICES() : null),
  });
  await waitFor(() => c1.document.getElementById("writemode").textContent !== "", { desc: "write-mode chrome render" });
  const badge1 = c1.document.getElementById("writemode");
  const sw1 = c1.document.getElementById("editswitch");
  assert.strictEqual(badge1.classList.contains("hidden"), false, "standalone: the read-only badge is visible");
  assert.strictEqual(badge1.textContent, "read-only", "standalone: the badge reads 'read-only'");
  assert.ok(badge1.className.includes("view-only"), "standalone: the badge carries the view-only style");
  assert.strictEqual(sw1.classList.contains("hidden"), true, "standalone: the write-mode toggle is hidden — no false promise of write access");

  // A page holding a window handle to this standalone tab (e.g. window.open /
  // opener) can post a forged 'dataconsole-init'. The standalone SPA never
  // acquires a vscodeApi (only a real embed host does), so it must ignore the
  // host-message channel entirely rather than consume the message and break
  // the authenticated session.
  const contentBefore = c1.document.getElementById("content").innerHTML;
  hostPostMessage(c1.window, { type: "dataconsole-init", writeEnabled: true, service: "db" });
  await new Promise((resolve) => setTimeout(resolve, 50));
  assert.strictEqual(c1.document.getElementById("content").innerHTML, contentBefore, "standalone: a forged host message must not touch the session (no error render, no embedded takeover)");
  assert.strictEqual(badge1.classList.contains("hidden"), false, "standalone: a forged host message must not hide the read-only badge");
  assert.strictEqual(sw1.classList.contains("hidden"), true, "standalone: a forged host message must not reveal the write-mode toggle");
  c1.close();

  // ---- embedded: switch visible, badge hidden — independent of writeEnabled ----
  for (const writeEnabled of [true, false]) {
    const c = buildConsole({
      url: "http://localhost/",
      embedded: true,
      routes: (method, p) => (p === "/api/services" ? EMPTY_SERVICES() : null),
    });
    await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready sent to host" });
    hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled });
    await waitFor(() => c.document.getElementById("editswitch").classList.contains("hidden") === false, { desc: "embedded chrome render" });
    const badge = c.document.getElementById("writemode");
    const sw = c.document.getElementById("editswitch");
    assert.strictEqual(sw.classList.contains("hidden"), false, "embedded (writeEnabled=" + writeEnabled + "): the write-mode toggle is visible");
    assert.strictEqual(badge.classList.contains("hidden"), true, "embedded (writeEnabled=" + writeEnabled + "): the standalone read-only badge stays hidden");
    assert.strictEqual(c.document.getElementById("editchk").checked, writeEnabled, "embedded: the toggle's checked state reflects writeEnabled");
    c.close();
  }

  console.log("readonly-posture.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
