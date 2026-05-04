---
id: classic-rust-postgres-standard
description: |
  Greenfield Rust API + Postgres via classic route — no Rust recipe
  exists, so the agent stays on the classic path. Standard pair
  (dev/stage). Tests compiled-runtime provisioning (minRam=1.0
  default), classic-route plan with managed dep, and develop on a
  compiled dev runtime where `start` is `zsc noop --silent` and the
  dev_server command must be the build artefact (`./app`), not
  `cargo run`. Counterpart to the prior known dev-server compiled-lang
  trap.
seed: empty
tags: [bootstrap, classic-route, standard-pair, rust, postgres, compiled, dev-server-compiled]
area: bootstrap-and-develop
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  You are building a small Rust HTTP API with sqlx + tokio against
  Postgres. You want a dev environment for fast iteration and a stage
  slot for build verification. Managed catalog defaults are fine. You
  trust the agent's reasoning when it explains a choice; push back if
  it proposes simple mode (you explicitly want dev + stage) or skips
  managed dep wiring.
notableFriction:
  - id: compiled-runtime-ram
    description: |
      Compiled runtimes (rust/go/dotnet/java) need minRam=1.0 in plan
      submission; dynamic interpreted defaults are too small. Surfaces
      whether the provision atom flags this for compiled targets.
  - id: dev-server-compiled-artifact
    description: |
      Dev-mode dynamic runtimes have `start: zsc noop --silent`. The
      dev_server `command` must be the build artefact (`./app`), not
      the source-level runner (`cargo run`). Tests whether the
      compiled-lang dev-server guidance has reached the relevant atom.
---

Build me a small Rust HTTP API on Zerops, with Postgres. I want a dev environment plus a stage slot — and yes, I want to be able to iterate on dev fast.
