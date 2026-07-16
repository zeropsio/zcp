"use strict";

// The §3 auth state matrix (docs/spec-welcome-mode.md §3, W-STATE / W4): the
// platform flag and the local credential artifact are two INDEPENDENT inputs
// that compose a matrix, never a boolean union. computeAgentState is the
// per-agent decision; buildState assembles the full webview payload —
// including the anyAuthorized gate that unlocks steps 2+ (local-only must
// NOT unlock: it is platform-unverified).

const test = require("node:test");
const assert = require("node:assert/strict");
const { loadWelcome, TEST_REGISTRY, TEST_AGENT_IDS } = require("./harness.js");

function loadPureFns() {
  const { welcome } = loadWelcome();
  return welcome;
}

// The five spec §3 rows, plus the precedence/edge cases called out in the
// P2 brief. `authType` is included in every case even though the matrix
// itself never keys on it (see the comment on computeAgentState) — this
// documents that carrying it is inert, not just untested.
const MATRIX_CASES = [
  {
    name: "row 1: flag absent, cred absent -> not-authorized",
    input: { flagOAuth: false, flagToken: false, authType: undefined, credPresent: false, credVerifiable: true },
    want: "not-authorized",
  },
  {
    name: "row 2: flag absent, cred present -> local-only",
    input: { flagOAuth: false, flagToken: false, authType: undefined, credPresent: true, credVerifiable: true },
    want: "local-only",
  },
  {
    name: "row 3: flag present, cred present -> authorized",
    input: { flagOAuth: true, flagToken: false, authType: "oauth", credPresent: true, credVerifiable: true },
    want: "authorized",
  },
  {
    name: "row 4: flag present, cred absent, verified probe -> reconnect (rebuild-orphaned flag)",
    input: { flagOAuth: true, flagToken: false, authType: "oauth", credPresent: false, credVerifiable: true },
    want: "reconnect",
  },
  {
    name: "row 5: token env present -> authorized-token, cred is n/a",
    input: { flagOAuth: false, flagToken: true, authType: "token", credPresent: false, credVerifiable: true },
    want: "authorized-token",
  },
  {
    name: "token-over-cred precedence: token present AND cred present -> still authorized-token",
    input: { flagOAuth: false, flagToken: true, authType: "token", credPresent: true, credVerifiable: true },
    want: "authorized-token",
  },
  {
    name: "token-over-cred precedence: token present AND oauth flag present -> token wins",
    input: { flagOAuth: true, flagToken: true, authType: "token", credPresent: false, credVerifiable: true },
    want: "authorized-token",
  },
  {
    name: 'unverifiable-probe flag-only row: flag present, no verified probe -> authorized ("flag is the only truth")',
    input: { flagOAuth: true, flagToken: false, authType: "oauth", credPresent: false, credVerifiable: false },
    want: "authorized",
  },
  {
    name: "unverifiable probe, no flag either -> not-authorized (antigravity/grok/cursor baseline)",
    input: { flagOAuth: false, flagToken: false, authType: undefined, credPresent: false, credVerifiable: false },
    want: "not-authorized",
  },
];

test("computeAgentState — full §3 matrix", () => {
  const { computeAgentState } = loadPureFns();
  for (const c of MATRIX_CASES) {
    assert.equal(computeAgentState(c.input), c.want, c.name);
  }
});

test("buildState — no-zembed environment: flags all absent, cred-only state, environment.zembed false", () => {
  const { buildState } = loadPureFns();

  const result = buildState({
    registry: TEST_REGISTRY,
    agentIds: TEST_AGENT_IDS,
    zembedEnv: null, // deps.readZembedEnv() returned null (no store / unreadable)
    creds: { "claude-code": { present: true, verifiable: true } },
    guided: { state: "unknown" },
  });

  assert.equal(result.environment.zembed, false);
  const claude = result.agents.find((a) => a.id === "claude-code");
  assert.equal(claude.state, "local-only", "no zembed -> no flags, but the local cred still surfaces local-only");
  for (const a of result.agents) {
    if (a.id === "claude-code") continue;
    assert.equal(a.state, "not-authorized", `agent ${a.id} has no flag and no cred with no zembed store`);
  }
});

test("buildState — per-agent probeVerified threads through the creds input's verifiable flag", () => {
  // buildState is pure: it has no registry-of-verified-probes knowledge of
  // its own (that's collectCred's CRED_PROBE constant, exercised end-to-end
  // in handshake.test.js) — it only reports what the creds input says. An
  // agent absent from creds (never collected) degrades to unverifiable.
  const { buildState } = loadPureFns();

  const result = buildState({
    registry: TEST_REGISTRY,
    agentIds: TEST_AGENT_IDS,
    zembedEnv: null,
    creds: {
      "claude-code": { present: false, verifiable: true },
      "codex": { present: false, verifiable: true },
      "antigravity": { present: false, verifiable: false },
      // grok, cursor: absent from creds entirely -> the same safe default
    },
    guided: { state: "unknown" },
  });

  const byId = Object.fromEntries(result.agents.map((a) => [a.id, a]));
  assert.equal(byId["claude-code"].probeVerified, true);
  assert.equal(byId["codex"].probeVerified, true);
  for (const id of ["antigravity", "grok", "cursor"]) {
    assert.equal(byId[id].probeVerified, false, `${id}: unverifiable (explicit or missing from creds) -> probeVerified false`);
  }
});

test("buildState — anyAuthorized gating: local-only alone must NOT unlock", () => {
  const { buildState } = loadPureFns();

  const result = buildState({
    registry: TEST_REGISTRY,
    agentIds: TEST_AGENT_IDS,
    zembedEnv: null,
    creds: {
      "claude-code": { present: true, verifiable: true }, // -> local-only
      "codex": { present: false, verifiable: true }, // -> not-authorized
    },
    guided: { state: "unknown" },
  });

  assert.equal(result.agents.find((a) => a.id === "claude-code").state, "local-only");
  assert.equal(result.anyAuthorized, false, "local-only is platform-unverified and must not unlock steps 2+");
});

test("buildState — anyAuthorized gating: authorized unlocks", () => {
  const { buildState } = loadPureFns();

  const result = buildState({
    registry: TEST_REGISTRY,
    agentIds: TEST_AGENT_IDS,
    zembedEnv: { ZCP_AGENT_OAUTH_CLAUDE_CODE: "true" },
    creds: { "claude-code": { present: true, verifiable: true } },
    guided: { state: "unknown" },
  });

  assert.equal(result.agents.find((a) => a.id === "claude-code").state, "authorized");
  assert.equal(result.anyAuthorized, true);
});

test("buildState — anyAuthorized gating: authorized-token alone unlocks", () => {
  const { buildState } = loadPureFns();

  const result = buildState({
    registry: TEST_REGISTRY,
    agentIds: TEST_AGENT_IDS,
    zembedEnv: { ZCP_AGENT_TOKEN_CODEX: "some-token-value" },
    creds: {},
    guided: { state: "unknown" },
  });

  assert.equal(result.agents.find((a) => a.id === "codex").state, "authorized-token");
  assert.equal(result.anyAuthorized, true);
});

test("buildState — passes guided/skills/bridge through unchanged", () => {
  const { buildState } = loadPureFns();

  const result = buildState({
    registry: TEST_REGISTRY,
    agentIds: TEST_AGENT_IDS,
    zembedEnv: null,
    creds: {},
    guided: { state: "enabled" },
  });

  assert.deepStrictEqual(result.guided, { state: "enabled" });
  assert.deepStrictEqual(result.skills, []);
  assert.deepStrictEqual(result.bridge, { status: "unknown" });
});
