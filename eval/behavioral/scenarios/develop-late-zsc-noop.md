---
# promote to required after two consecutive baseline greens (plan D9)
id: develop-late-zsc-noop
description: |
  S5 self-review friction (run 3): the dev-mode `start: zsc noop --silent`
  requirement (dev-mode keeps the runtime container alive via a no-op
  process; the real process is started separately via `zerops_dev_server`)
  only surfaces in develop-active guidance — AFTER the agent has already
  written a production `start:` command (e.g. `node dist/index.js`) into
  zerops.yaml during bootstrap/first-write, before any develop-active atom
  has fired. The agent has to discover the mismatch and rewrite
  zerops.yaml once develop guidance arrives. Frozen expectation: despite
  the rewrite, the run ends with the dev process live and reachable.
seed: empty
tags: [develop, dev-mode, zsc-noop, start-command, late-guidance, observe, s5-signal]
area: develop
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  You're a backend developer setting up a small Node.js API on Zerops,
  dev-only, to iterate on code live. You don't know the platform's
  internal start-command conventions — you expect the agent to figure
  those out. Accept the agent's framework choice and defaults. If the
  agent asks about hostname or project naming, keep it short.
verification:
  mode: observe
  spec: spec-workflows.md §4
  liveness: {service: api, marker: "zsc-noop-ready"}
  noFailedProcesses: true
  never: ["zerops_import{override=true}", "zerops_delete"]
notableFriction:
  - id: late-zsc-noop-guidance
    description: |
      S5 run 3 (verbatim): "My first zerops.yaml had `start: node
      dist/index.js`, which is the production start command. The develop
      workflow guidance (which only arrived after I started the develop
      session) told me dev-mode needs `start: zsc noop --silent` — a
      keepalive no-op — and you start the real process via
      `zerops_dev_server`. This is documented, but it only surfaces in the
      develop-active atoms, not during bootstrap when you're first writing
      the file. I had to rewrite zerops.yaml before the deploy worked
      correctly. If you're in dev mode, write `zsc noop --silent` from the
      start."
    suspectedCauses:
      - "internal/content/atoms/develop-dynamic-runtime-start-container.md (dev-mode start: convention not surfaced at bootstrap/first-write time)"
---

Set up a small Node.js API for me on Zerops, dev-only — I want to iterate on the code live, no stage, no production yet. Use project `{{projectId}}`. Once it's running, make `GET /` return `200` with a body containing the literal text `zsc-noop-ready`.
