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
runtime with a `/status` endpoint that proves all managed services are
reachable. The verification server is discarded at develop time — real
application code goes in via `workflow=develop`.

Before deploying, confirm the plan with the user. Explicit approval is
required for dev/stage pairing and deploy strategy.
