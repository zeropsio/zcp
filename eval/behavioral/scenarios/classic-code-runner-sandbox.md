---
id: classic-code-runner-sandbox
description: |
  A12 (docs/spec-scenarios.md §9.3). Brand-new project, classic route: the
  user wants a Python playground API that runs code people submit. The
  submitted code must never run inside the API's own container, so the app
  needs a sandbox next to its runtime. The bootstrap guidance says where
  such code runs — in a Zerops Box sandbox beside the app
  (docs/spec-workflows.md §8 PS-15) — and the agent provisions the Box as
  infrastructure the app uses, never as the app's runtime (PS-5: a
  bootstrap plan refuses a pool or Box as a runtime target).

  A Box exists only in a project in region prg1 (PS-16). The farm creates
  its run projects through the prg1 API with no location
  (docs/spec-eval-farm.md §2.1 FM-10; internal/eval/farm/project_yaml.go),
  so they are in prg1 — a farm run captured on 2026-09-08 served
  `*.prg1.zerops.app` — and this cell expects a Box. In a project outside
  prg1 the plan and the import are refused before anything is created.
  Until 2026-10-24 this cell expected a Swarm pool, the one sandbox whose
  API the knowledge described then.

  Surfaces the agent must navigate:

   1. Take "run strangers' code elsewhere" as the Box case
      (bootstrap-user-code-box at the classic discover step, the catalog's
      Box line) and provision `sandbox` as `ubuntu/zbox@1` beside the `api`
      runtime — a plan dependency or its own import, never a runtime
      target. zerops_import finishes the Box's setup (a READY `default`
      base) before it returns.
   2. Wire the Box's workspace key into `api` by reference
      (`${sandbox_ZBOX_WORKSPACE_API_KEY}` in zerops.yaml
      `run.envVariables`, the API at `http://sandbox:8080`), never by
      reading the value and pasting it anywhere.
   3. Implement POST /run against the workspace API (services theme,
      "Running code the app's users send"): the `default` base's digest
      from base-list, a sandbox per run (`AIRGAPPED`), an exec of
      `python3 -c <code>`, a terminate — never a subprocess or a namespace
      sandbox inside `api` (the failure seen live on 2026-10-24, before
      the bootstrap guidance named Box).
   4. Never run a dev server or a deploy on the Box: it has no zerops.yaml.

  The function oracle submits a snippet printing the hostname and kernel
  release it ran under. In a Box sandbox the hostname is the sandbox's id
  (lowercase letters and digits, no dots) and the kernel 6.18.53
  (zerops-docs zbox/images.mdx; both live-verified 2026-10-24). In the
  API's own container, or a pool container, the hostname is
  `node-id-<n>.runtime.<service>.zerops` on the host's kernel
  (`7.1.8-zabbly+`, live 2026-10-24), and namespaces in `api` change
  neither the kernel nor that. The match takes a dot-free hostname and a
  plain dotted kernel version, so a Box kernel update keeps it passing.
seed: empty
tags: [bootstrap, classic-route, simple-mode, node, zerops-box, sandbox, untrusted-code, credential-by-reference]
area: bootstrap
retrospective:
  promptStyle: briefing-future-agent
verification:
  mode: required
  spec: spec-workflows.md §8 PS-15
  expectedServices:
    - hostname: api
      status: [ACTIVE]
      type: nodejs@*
    - hostname: sandbox
      status: [ACTIVE]
      type: zbox*
  noFailedProcesses: true
  containerCheck:
    - service: api
      cmd: >-
        node -e 'fetch("http://localhost:3000/run", {method: "POST", headers: {"content-type": "application/json"},
        body: JSON.stringify({code: "import socket, platform\nprint(socket.gethostname(), platform.release())"}), signal: AbortSignal.timeout(150000)})
        .then(r => r.text()).then(t => console.log(t), e => { console.error(String(e)); process.exit(1) })'
      match: '"output"\s*:\s*"[a-z0-9]+ [0-9]+\.[0-9]+\.[0-9]+(\\n)?"'
    - service: api
      cmd: "grep -hoF '${sandbox_ZBOX_WORKSPACE_API_KEY}' /var/www/zerops.yaml /var/www/zerops.yml 2>/dev/null | head -n1"
      match: 'sandbox_ZBOX_WORKSPACE_API_KEY'
  noFabricatedSecret: true
  toolArg:
    - {never: 'zerops_env{action=set,variables~KEY=[^$]}'}
    - {never: 'zerops_import{content~KEY: *[A-Za-z0-9]}'}
  never:
    - "zerops_import{override=true}"
    - "zerops_delete"
    - "zerops_dev_server{hostname=sandbox}"
    - "zerops_deploy{targetService=sandbox}"
userPersona: |
  You are a developer building a small Python playground: people paste a
  snippet and get back what it printed. Nothing exists yet — no code, no
  repository. You know the snippets are arbitrary code from strangers, so
  they must never run inside the API's own container.

  Your preferences:
   - One Node.js service called `api` that just runs the API — no staging,
     no separate dev copy. Accept the default Node.js version.
   - Whatever runs the submitted code is called `sandbox`.
   - If the agent asks how the code should be isolated, let it pick what
     Zerops offers for running strangers' code; a service in beta is fine.
   - If the agent asks you for a token, key or password: you have none —
     Zerops generates what the services need; it should wire that, not
     ask you for it or show it to you.
   - If the agent wants a git remote: none yet.

  What you expect at the end:
   - `api` and `sandbox` running.
   - `POST /run` with `{"code": "print(6*7)"}` answers
     `{"output": "42\n"}` (or the same without the trailing newline).
   - One example request you can try yourself.
notableFriction:
  - id: sandbox-from-the-planning-rule
    description: |
      The classic discover step says code the app's users send runs in a
      Zerops Box sandbox beside the app. Surfaces whether the agent plans
      the Box from that, or still builds a namespace sandbox inside `api`
      (what an agent did live on 2026-10-24 before the rule existed) or
      reaches for a Swarm pool.
  - id: box-is-not-the-runtime
    description: |
      A bootstrap plan naming the Box as a runtime target is refused
      (spec-workflows.md §8 PS-5). The Box belongs in the plan's
      dependencies or its own import. Surfaces whether the refusal's
      guidance is enough when the agent first models it wrong.
  - id: workspace-api-shapes
    description: |
      A create needs the `default` base's digest (its name answers 404),
      `networkMode` and an `Idempotency-Key`; exec takes argv with no shell
      and no stdin. Surfaces whether the services theme's shapes are enough
      to code POST /run, or the agent has to probe the workspace API
      (its OpenAPI document is at /api/rest/swagger/openapi.yml).
  - id: key-by-reference
    description: |
      The app authenticates to the workspace API with
      `${sandbox_ZBOX_WORKSPACE_API_KEY}`, wired in zerops.yaml
      `run.envVariables` like every cross-service reference. Reading the
      value (an env dump, a discover with values) and pasting it into
      config, code or a zerops_env set is the failure this cell watches
      for.
---

In project `{{projectId}}`, build me a small Python playground API: people paste a Python snippet and get back what it printed. They will submit arbitrary code, so it must never run inside the API's own container — every submission runs in its own throwaway sandbox on Zerops, isolated from the API.

Write the API in Node.js as a single service called `api` (no staging, no extra dev copy), listening on port 3000. Call whatever runs the submitted code `sandbox`. The API has one endpoint: `POST /run` with the JSON body `{"code": "<python source>"}` answers JSON `{"output": "<what the program printed>"}`.

I don't want to see or paste any credential — wire whatever the API needs from Zerops itself. When it works, show me one request I can try.
