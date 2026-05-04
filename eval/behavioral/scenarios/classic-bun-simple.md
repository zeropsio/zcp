---
id: classic-bun-simple
description: |
  Greenfield Bun service via classic route, simple mode (single
  immutable runtime, no dev/stage pair, no managed dependency). Tests
  the alternative-Node runtime atoms — Bun has different start
  semantics from nodejs (`bun run` vs `node`), different lockfile
  (`bun.lockb`), and different default ports. Bun has a recipe
  (`bun-hello-world`), so route-pick must reconcile recipe vs
  classic-fallback for a "just one container" prompt.
seed: empty
tags: [bootstrap, classic-route, simple-mode, bun, alternative-runtime, recipe-vs-classic-pick]
area: bootstrap
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  You are prototyping a tiny Bun script (single endpoint, no DB,
  nothing fancy) and want it on Zerops as one container. You don't
  want dev/stage pairs — just deploy and run. If the agent suggests
  the bun-hello-world recipe template, accept it as a starting point
  but do not let it grow into a multi-service plan.
notableFriction:
  - id: simple-mode-pick
    description: |
      User asks for one container, no extras. Agent must land on
      simple mode. Surfaces whether the simple/dev/standard split is
      telegraphed clearly when an alternative runtime is involved.
  - id: bun-runtime-specifics
    description: |
      Bun start command is `bun run <entry>` (or just `bun <entry>`),
      lockfile is `bun.lockb` not `package-lock.json`, and there is
      no separate buildCommand for many simple cases. Surfaces
      whether bun atoms diverge from nodejs atoms cleanly.
---

Set up a small Bun HTTP service for me on Zerops. Just one container, nothing fancy — no database, no staging.
