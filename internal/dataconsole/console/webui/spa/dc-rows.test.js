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

assert.strictEqual(
  rows.keyColScore([{ name: "member" }, { name: "score" }], ["alice", "12.5"]),
  12.5,
  "zset score is read from the score column"
);
assert.strictEqual(
  rows.keyColScore([{ name: "member" }], ["alice"]),
  null,
  "rows without a score column have no score"
);
assert.ok(
  Number.isNaN(rows.keyColScore([{ name: "score" }], ["not-a-number"])),
  "non-numeric score preserves Number conversion semantics"
);

console.log("dc-rows.test.js OK");
