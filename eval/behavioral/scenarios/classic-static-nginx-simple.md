---
id: classic-static-nginx-simple
description: |
  Greenfield static site (nginx) via classic bootstrap, simple mode. Tests
  RuntimeStatic path — no zsc noop lifecycle, no zerops_dev_server primitive,
  L7 serves from deployFiles directly. Contrast to dynamic-runtime first-deploy
  502 trap (Trap-2) which does NOT apply here.
seed: empty
tags: [bootstrap, classic-route, simple-mode, static-runtime, nginx, no-managed-deps]
area: bootstrap
retrospective:
  promptStyle: briefing-future-agent
notableFriction:
  - id: static-vs-dynamic
    description: |
      Agent must classify nginx/static as RuntimeStatic — different lifecycle
      from dynamic (no dev_server, no zsc noop). Surfaces whether the
      runtime-class atom telegraphs the distinction at bootstrap time.
  - id: no-dev-server-call
    description: |
      Static runtimes never call zerops_dev_server (no dev-server lifecycle).
      Agent should not reach for that primitive. Surfaces whether the dev_server
      atom gates correctly on RuntimeClass.
  - id: deployfiles-shape
    description: |
      Static sites deploy build artifacts (dist/, public/) via deployFiles
      pattern — surfaces whether the develop atoms cover deployFiles for
      static runtimes distinctly from dynamic .
---

Put up a small static landing page for me using nginx. Just HTML, no backend.
