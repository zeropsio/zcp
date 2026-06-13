---
id: bootstrap-classic-plan-dynamic
priority: 2
phases: [bootstrap-active]
routes: [classic]
steps: [discover]
title: "Classic bootstrap — dynamic runtime plan"
---

### Dynamic runtime plan

If the plan you're about to submit includes a dynamic runtime (Node, Go, Python, Bun, Ruby, …), apply this section. Classic bootstrap creates the runtime + managed services with `startWithoutCode: true` so dev containers reach RUNNING with an empty filesystem; `workflow=develop` then scaffolds `zerops.yaml`, writes the application, and runs the first deploy.

`bootstrapMode` and `stageHostname` MUST be inside `runtime` — flat placement is hard-rejected by the schema. Nest them under `runtime` (see the shape below) the first time.

```json
[
  {
    "runtime": {
      "devHostname": "appdev",
      "stageHostname": "appstage",
      "type": "nodejs@22",
      "bootstrapMode": "standard",
      "isExisting": false
    },
    "dependencies": [
      {"hostname": "db", "type": "postgresql:single@18", "resolution": "CREATE"}
    ]
  }
]
```

Confirm dev/stage pairing with the user before submitting the plan. Mode + close-mode + git-push capability decisions all happen later in develop, not here.
