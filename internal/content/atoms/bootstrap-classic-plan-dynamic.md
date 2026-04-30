---
id: bootstrap-classic-plan-dynamic
priority: 2
phases: [bootstrap-active]
routes: [classic]
runtimes: [dynamic]
steps: [discover]
title: "Classic bootstrap — dynamic runtime plan"
---

The service plan includes at least one dynamic runtime (Node, Go, Python, Bun, Ruby, …). Classic bootstrap creates the runtime + managed services with `startWithoutCode: true` so dev containers reach RUNNING with an empty filesystem; `workflow=develop` then scaffolds `zerops.yaml`, writes the application, and runs the first deploy.

Confirm dev/stage pairing with the user before submitting the plan. Mode + close-mode + git-push capability decisions all happen later in develop, not here.
