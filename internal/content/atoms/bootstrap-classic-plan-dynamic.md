---
id: bootstrap-classic-plan-dynamic
priority: 2
phases: [bootstrap-active]
routes: [classic]
runtimes: [dynamic]
steps: [discover]
title: "Classic bootstrap — dynamic runtime plan"
---

The service plan includes at least one dynamic runtime (Node, Go, Python,
Bun, Ruby, …). Classic bootstrap deploys a minimal verification server per
runtime with a `/status` endpoint proving managed services are reachable;
`workflow=develop` replaces it with real code.

Confirm dev/stage pairing and deploy strategy with the user before deploying.
