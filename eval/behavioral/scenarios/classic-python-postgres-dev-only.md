---
id: classic-python-postgres-dev-only
description: |
  Greenfield Python + Postgres via classic bootstrap, dev-only mode (single
  dev container, NO stage pair). Tests bootstrapMode=dev path explicitly —
  contrast to standard pair scenario; agent should NOT request stageHostname.
seed: empty
tags: [bootstrap, classic-route, dev-mode, dynamic, python, postgres, managed-dep]
area: bootstrap
retrospective:
  promptStyle: briefing-future-agent
notableFriction:
  - id: dev-only-vs-standard
    description: |
      Agent must distinguish dev (single container) from standard (dev/stage
      pair). User says "for development" — explicit dev mode signal.
      Surfaces whether agent reflexively picks standard or correctly picks dev.
  - id: stagehostname-nesting
    description: |
      If agent attempts standard mode anyway and adds stageHostname, schema
      requires it nested under runtime — top-level placement rejects with
      INVALID_PARAMETER. Surfaces whether the post-v9.54.1 schema description
      update telegraphs the nesting clearly.
  - id: managed-dep-resolution
    description: |
      Postgres is a CREATE resolution (new managed service); plan must list
      it under runtime.dependencies, not as a separate top-level target.
---

Set up a Python web service with a Postgres database for me. Just a development environment, no production stage needed.
