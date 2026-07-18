"use strict";

(function () {
  const root = typeof window !== "undefined" ? window : null;
  if (root) root.DC = root.DC || {};

  function rowKeyOf(cols, keyCols, row) {
    const key = {};
    (cols || []).forEach((c, i) => {
      if ((keyCols || []).includes(c.name)) key[c.name] = row[i];
    });
    return key;
  }

  // entryEditPlan decides what one KV-collection cell edit (the PUT /api/entry
  // path: hash/set/zset) means, derived purely from the server-sent TablePage
  // shape (columns + rowKeyCols) — no server contract change.
  //
  // A RowKeyCols column is the entry's identity (hash field, set/zset member).
  // Editing it in place is locked whenever the row has a sibling non-key column:
  // the /api/entry wire shape carries one Field + one Value/Score, so a hash
  // field edit would otherwise send {field: oldField, value: typedFieldName}
  // and HSET would overwrite the field's VALUE with that text, and a zset
  // member edit would re-add the OLD member at its OLD score (no-op). A
  // single-column collection (set: [member]) has no sibling, so its key cell
  // stays editable — SREM(old)+SADD(new) is the correct, already-working
  // server semantic for a member rename. A row with no key column at all
  // (redis list: RowKeyCols empty) has no safe entry identity, so every cell
  // is locked.
  //
  // A "score" column (zset) is not a key column but is numeric-only: it maps
  // to KVEntryEdit.Score, never .Value (kv.go SetEntry's zset case ignores
  // Value entirely), so a non-numeric typed value is rejected before any
  // request is built — sending the old score back would silently no-op.
  function entryEditPlan(columns, rowKeyCols, rowValues, colIndex, typedText) {
    const cols = columns || [];
    const keyCols = rowKeyCols || [];
    const col = cols[colIndex];
    if (!col || keyCols.length === 0) return { kind: "locked" };

    const isKeyCol = keyCols.includes(col.name);
    const hasNonKeyCol = cols.some((c) => !keyCols.includes(c.name));
    if (isKeyCol && hasNonKeyCol) return { kind: "locked" };

    const keyIdx = cols.findIndex((c) => c.name === keyCols[0]);
    const field = String((rowValues || [])[keyIdx]);

    if (col.name === "score") {
      const text = typedText == null ? "" : String(typedText).trim();
      const n = Number(text);
      if (text === "" || !Number.isFinite(n)) {
        return { kind: "invalid", reason: "Score must be a number." };
      }
      return { kind: "edit", payload: { field: field, score: n } };
    }
    return { kind: "edit", payload: { field: field, value: typedText } };
  }

  const api = {
    rowKeyOf,
    entryEditPlan,
  };

  if (root) root.DC.rows = api;
  if (typeof module !== "undefined") module.exports = api;
})();
