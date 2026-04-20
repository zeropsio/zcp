---
id: bootstrap-provision-runtime-properties
priority: 2
phases: [bootstrap-active]
routes: [classic, adopt]
steps: [provision]
title: "Runtime service properties by mode"
---

### Runtime service properties (import.yaml)

`startWithoutCode`, `maxContainers`, and `enableSubdomainAccess` vary by
mode. Set them correctly at import-yaml generation time.

| Property | Dev service | Stage service | Simple service |
|----------|-----------|---------------|----------------|
| `startWithoutCode` | `true` | omit | `true` |
| `maxContainers` | `1` | omit | omit |
| `enableSubdomainAccess` | `true` | `true` | `true` |
| `verticalAutoscaling.minRam` | `1.0` for compiled runtimes (Go, Rust, Java, .NET, Elixir, Gleam) | omit | omit |

**Why `startWithoutCode: true`** — dev and simple services need to reach
RUNNING before the first deploy; otherwise they sit at READY_TO_DEPLOY,
which blocks SSHFS mount and SSH access. Stage services deliberately
omit the flag — they wait at READY_TO_DEPLOY until the first cross-deploy
from dev, sparing resources.

**Expected post-import states**: Dev → RUNNING, Simple → RUNNING, Stage →
READY_TO_DEPLOY, Managed → RUNNING/ACTIVE.
