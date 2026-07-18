"use strict";

const assert = require("assert");
const actions = require("../dist/dc-actions");

const readOnlyTabular = {
  hostname: "db",
  actions: [
    { id: "querySQL", enabled: true, readOnly: true, reason: "" },
    { id: "readTable", enabled: true, readOnly: true, reason: "" },
    { id: "editCell", enabled: false, readOnly: false, reason: "session is read-only" },
    { id: "insertRow", enabled: false, readOnly: false, reason: "session is read-only" },
    { id: "deleteRow", enabled: false, readOnly: false, reason: "session is read-only" },
    {
      id: "showVPNGate",
      enabled: true,
      readOnly: true,
      reason: "This managed service is on the project's private network; bring up the VPN if it is unreachable.",
    },
  ],
};

assert.deepStrictEqual(
  actions.actionOf(readOnlyTabular, "querySQL"),
  readOnlyTabular.actions[0],
  "actionOf reads a service.actions descriptor by id"
);
assert.deepStrictEqual(
  actions.actionOf(readOnlyTabular.actions, "readTable"),
  readOnlyTabular.actions[1],
  "actionOf also accepts a raw action array"
);
assert.strictEqual(actions.actionOf(readOnlyTabular, "deleteNode"), null, "absent actions are not available");
assert.strictEqual(actions.hasAction(readOnlyTabular, "querySQL"), true, "known action is available");
assert.strictEqual(actions.hasAction(readOnlyTabular, "deleteNode"), false, "unknown action is unavailable");
assert.strictEqual(actions.actionEnabled(readOnlyTabular, "querySQL"), true, "enabled action is true");
assert.strictEqual(actions.actionEnabled(readOnlyTabular, "editCell"), false, "disabled action is false");
assert.strictEqual(actions.actionReason(readOnlyTabular, "editCell"), "session is read-only", "reason comes from Go action descriptor");
assert.strictEqual(actions.actionReason(readOnlyTabular, "deleteNode"), "", "absent action has no reason");

assert.deepStrictEqual(
  actions.actionControl(readOnlyTabular, "editCell", "Edit cell"),
  {
    id: "editCell",
    label: "Edit cell",
    available: true,
    enabled: false,
    reason: "session is read-only",
  },
  "control state carries disabled reason for rendered-but-disabled controls"
);
assert.deepStrictEqual(
  actions.actionControl(readOnlyTabular, "deleteNode", "Delete"),
  {
    id: "deleteNode",
    label: "Delete",
    available: false,
    enabled: false,
    reason: "",
  },
  "control state marks absent actions as unavailable"
);

console.log("dc-actions.test.js OK");
