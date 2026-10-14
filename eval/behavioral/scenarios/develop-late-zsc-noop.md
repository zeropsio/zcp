---
# promote to required after two consecutive baseline greens (plan D9)
id: develop-late-zsc-noop
description: |
  S5 self-review friction (run 3): the agent wrote a production `start:`
  (e.g. `node dist/index.js`) or `zsc noop --silent` into the dev setup
  before any develop-active atom fired, and had to rewrite zerops.yaml once
  develop guidance arrived. Dev servers now run under the dev service's
  supervisor (`zcp-devsup`, the dev setup's `run.start`): the dev service's
  first `zerops_deploy` writes the supervisor's two lines into its dev
  setup when a published zcp release ships the supervisor binary (or
  ZCP_DEVSUP_URL names a dev build), so the first `zerops_dev_server
  action=start` only declares the server in `.zcp/dev-servers.json`;
  without either, that first start moves the dev setup onto it by itself
  with one redeploy. Neither asks anything. Frozen expectation: the run
  ends with the dev process running under the supervisor and reachable,
  its server declared, and no rewrite of the dev setup's start by hand.
seed: empty
tags: [develop, dev-mode, zsc-noop, supervisor, start-command, late-guidance, observe, s5-signal]
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
  toolArg:
    - {always: "zerops_dev_server{action=start}"}
  containerCheck:
    - {service: api, cmd: "grep -qE 'start: *zcp-devsup run' /var/www/zerops.y*ml && echo supervised", match: "^supervised"}
    - {service: api, cmd: "test -s /var/www/.zcp/dev-servers.json && echo declared", match: "^declared"}
    - {service: api, cmd: "zcp-devsup ctl status --json | grep -qE '\"status\": *\"running\"' && echo running", match: "^running"}
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
      - "internal/tools/dev_server_migrate.go superviseNewDevSetup (the first deploy writes the supervisor lines only when a release ships zcp-devsup or ZCP_DEVSUP_URL is set; otherwise the first dev-server start migrates with one redeploy)"
---

Set up a small Node.js API for me on Zerops, dev-only — I want to iterate on the code live, no stage, no production yet. Use project `{{projectId}}`. Once it's running, make `GET /` return `200` with a body containing the literal text `zsc-noop-ready`.
