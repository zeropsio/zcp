"use strict";

const assert = require("assert");
const rows = require("../dist/dc-rows");

const cols = [
  { name: "id", dataType: "integer", pk: true },
  { name: "tenant", dataType: "text", pk: false },
  { name: "value", dataType: "text", pk: false },
];

assert.deepStrictEqual(
  rows.rowKeyOf(cols, ["id"], [42, "acme", "one"]),
  { id: 42 },
  "rowKeyOf returns the requested primary key column"
);
assert.deepStrictEqual(
  rows.rowKeyOf(cols, ["id", "tenant"], [42, "acme", "one"]),
  { id: 42, tenant: "acme" },
  "rowKeyOf supports composite keys"
);
assert.deepStrictEqual(
  rows.rowKeyOf(cols, ["missing"], [42, "acme", "one"]),
  {},
  "unknown key columns do not invent row-key fields"
);
assert.deepStrictEqual(
  rows.rowKeyOf(cols, [], [42, "acme", "one"]),
  {},
  "tables without a safe key produce an empty row key"
);

// ---- entryEditPlan: PUT /api/entry cell interactivity + payload building ----
// Server-sent TablePage shapes per redis collection type (kv.go ReadTable):
// hash=[field,value] RowKeyCols=[field]; set=[member] RowKeyCols=[member];
// zset=[member,score] RowKeyCols=[member]; list=[index,value] RowKeyCols=nil.
const hashCols = [{ name: "field" }, { name: "value" }];
const hashKeyCols = ["field"];
const setCols = [{ name: "member" }];
const setKeyCols = ["member"];
const zsetCols = [{ name: "member" }, { name: "score" }];
const zsetKeyCols = ["member"];
const listCols = [{ name: "index" }, { name: "value" }];
const listKeyCols = []; // RowKeyCols nil from the server

// hash: the field cell is a row-key column with a sibling (value) — locked.
// Editing it must never reach a payload, since HSET would otherwise overwrite
// the field's VALUE with whatever was typed into the field cell (D-02).
assert.deepStrictEqual(
  rows.entryEditPlan(hashCols, hashKeyCols, ["username", "alice@example.com"], 0, "renamed-field"),
  { kind: "locked" },
  "hash field cell (row-key with a sibling column) is locked"
);
// hash: the value cell is not a row-key column — edits it, field stays anchored
// to the row's existing key so HSET targets the same field.
assert.deepStrictEqual(
  rows.entryEditPlan(hashCols, hashKeyCols, ["username", "alice@example.com"], 1, "bob@example.com"),
  { kind: "edit", payload: { field: "username", value: "bob@example.com" } },
  "hash value cell edits the field's value; field stays anchored to the row key"
);

// set: member is the row's ONLY column (no sibling) — SREM+SADD rename is the
// correct, working server semantic, so it stays editable.
assert.deepStrictEqual(
  rows.entryEditPlan(setCols, setKeyCols, ["alice"], 0, "alicia"),
  { kind: "edit", payload: { field: "alice", value: "alicia" } },
  "set member cell (sole column) stays editable — server does SREM old + SADD new"
);

// zset: the member cell is a row-key column with a sibling (score) — locked.
// Editing it in place would re-add the OLD member at its OLD score (no-op).
assert.deepStrictEqual(
  rows.entryEditPlan(zsetCols, zsetKeyCols, ["alice", "12.5"], 0, "bob"),
  { kind: "locked" },
  "zset member cell (row-key with a sibling column) is locked"
);
// zset: the score cell is not a row-key column — numeric-only, maps to
// KVEntryEdit.Score (never .Value, which the server ignores for zset).
assert.deepStrictEqual(
  rows.entryEditPlan(zsetCols, zsetKeyCols, ["alice", "12.5"], 1, "42.5"),
  { kind: "edit", payload: { field: "alice", score: 42.5 } },
  "zset score cell sends the typed score as a Number; member stays anchored; value is omitted"
);
assert.deepStrictEqual(
  rows.entryEditPlan(zsetCols, zsetKeyCols, ["alice", "12.5"], 1, "-3"),
  { kind: "edit", payload: { field: "alice", score: -3 } },
  "zset score accepts negative numbers"
);
assert.deepStrictEqual(
  rows.entryEditPlan(zsetCols, zsetKeyCols, ["alice", "12.5"], 1, "1e2"),
  { kind: "edit", payload: { field: "alice", score: 100 } },
  "zset score accepts scientific notation (a valid JS number)"
);
assert.deepStrictEqual(
  rows.entryEditPlan(zsetCols, zsetKeyCols, ["alice", "12.5"], 1, "not-a-number"),
  { kind: "invalid", reason: "Score must be a number." },
  "non-numeric zset score is rejected client-side — no request is ever built"
);
assert.deepStrictEqual(
  rows.entryEditPlan(zsetCols, zsetKeyCols, ["alice", "12.5"], 1, ""),
  { kind: "invalid", reason: "Score must be a number." },
  "empty zset score is rejected"
);
assert.deepStrictEqual(
  rows.entryEditPlan(zsetCols, zsetKeyCols, ["alice", "12.5"], 1, "   "),
  { kind: "invalid", reason: "Score must be a number." },
  "whitespace-only zset score is rejected, not silently coerced to 0"
);
assert.deepStrictEqual(
  rows.entryEditPlan(zsetCols, zsetKeyCols, ["alice", "12.5"], 1, "Infinity"),
  { kind: "invalid", reason: "Score must be a number." },
  "non-finite zset score is rejected (also unmarshalable as JSON)"
);

// list: RowKeyCols is empty ⇒ no safe entry identity ⇒ every cell is locked,
// regardless of column. List stays fully non-editable (D-03 territory, deferred).
assert.deepStrictEqual(
  rows.entryEditPlan(listCols, listKeyCols, [0, "a"], 0, "9"),
  { kind: "locked" },
  "list index cell is locked (no RowKeyCols ⇒ no safe entry identity)"
);
assert.deepStrictEqual(
  rows.entryEditPlan(listCols, listKeyCols, [0, "a"], 1, "b"),
  { kind: "locked" },
  "list value cell is locked (no RowKeyCols ⇒ no safe entry identity)"
);

console.log("dc-rows.test.js OK");
