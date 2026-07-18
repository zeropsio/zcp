"use strict";
// FIX 4 (S1): Escape in the inline cell editor must CANCEL, never commit. Root
// cause: editCell()'s Escape handler does `td.textContent = fmt(oldVal)`, which
// removes the still-focused <input> from the DOM. A real browser fires `blur`
// when a focused element is removed, and `input.onblur = commit` was never
// unbound -- so commit() runs with the typed value and the toast honestly (but
// wrongly) says "Saved." This test dispatches Escape AND the resulting blur
// explicitly, rather than relying on jsdom to replicate DOM-removal-triggers-
// blur, so the repro is deterministic regardless of jsdom's fidelity there.

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, hostPostMessage } = require("./harness");

async function main() {
  const service = {
    hostname: "db", type: "postgresql:single@18", support: "supported",
    actions: [{ id: "editCell", enabled: true, readOnly: false, reason: "" }],
  };
  const cellEdits = [];
  const routes = (method, p, body) => {
    if (p === "/api/services") return jsonRoute({ project: { id: "p1", name: "Proj" }, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "t", kind: "tabular", path: { service: "db", segments: ["public", "t"] } }] });
    if (p.startsWith("/api/table")) return jsonRoute({
      columns: [{ name: "id", dataType: "int", pk: true, editable: false, reason: "primary key" }, { name: "val", dataType: "text", pk: false, editable: true, reason: "" }],
      rows: [["1", "hello"]], rowKeyCols: ["id"],
    });
    if (method === "POST" && p === "/api/cell") { cellEdits.push(body); return jsonRoute({ ok: true }); }
    return null;
  };

  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "db" });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node render" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.querySelector("#content td.editable"), { desc: "editable cell render" });

  const cell = c.document.querySelector("#content td.editable");
  click(cell);
  await waitFor(() => cell.querySelector("input.celledit"), { desc: "inline editor opens" });
  const input = cell.querySelector("input.celledit");
  input.value = "typed-but-should-not-save";

  const w = c.window;
  input.dispatchEvent(new w.KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
  input.dispatchEvent(new w.FocusEvent("blur"));

  // Give any wrongly-fired async commit() a chance to reach the network before
  // asserting it did not.
  await new Promise((resolve) => setTimeout(resolve, 50));

  assert.strictEqual(cellEdits.length, 0, "Escape must not commit -- no /api/cell request may be issued");
  assert.strictEqual(cell.textContent, "hello", "Escape must restore the original cell value, not the typed one");

  // Companion: a NORMAL blur (no Escape) must still commit -- proves the fix
  // does not also break the legitimate commit-on-blur path.
  click(cell);
  await waitFor(() => cell.querySelector("input.celledit"), { desc: "inline editor re-opens" });
  const input2 = cell.querySelector("input.celledit");
  input2.value = "changed-for-real";
  input2.dispatchEvent(new w.FocusEvent("blur"));
  await waitFor(() => cellEdits.length === 1, { desc: "normal blur still commits" });
  assert.strictEqual(JSON.parse(cellEdits[0]).newValue, "changed-for-real", "the legitimate blur-commit path still sends the typed value");

  c.close();
  console.log("cell-edit-escape.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
