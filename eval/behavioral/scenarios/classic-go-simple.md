---
id: classic-go-simple
description: |
  Greenfield Go service via classic bootstrap route, simple mode (single
  immutable runtime, no dev/stage pair, no managed dependency). Smallest
  possible bootstrap; baseline for compiled-runtime + simple-mode path.
seed: empty
tags: [bootstrap, classic-route, simple-mode, dynamic-compiled, go, no-managed-deps]
area: bootstrap
retrospective:
  promptStyle: briefing-future-agent
notableFriction:
  - id: simple-mode-pick
    description: |
      Agent must pick simple mode (not dev, not standard) for a single
      immutable runtime — surfaces whether the mode-prompt atom telegraphs
      the simple/dev/standard split clearly.
  - id: compiled-runtime-ram
    description: |
      Compiled runtimes (go/rust/java/dotnet) want minRam=1.0 in import
      yaml; dynamic interpreted runtimes (node/python/bun) don't. Surfaces
      whether the provision atom flags this distinction.
---

Set up a small Go HTTP service for me on Zerops. Just one container, nothing fancy.
