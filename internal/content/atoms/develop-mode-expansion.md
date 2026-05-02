---
id: develop-mode-expansion
priority: 6
phases: [develop-active]
deployStates: [deployed]
modes: [dev, simple]
title: "Mode expansion — add a stage pair"
references-fields: [workflow.ServiceSnapshot.Mode, workflow.ServiceSnapshot.CloseDeployMode, workflow.ServiceSnapshot.StageHostname, workflow.ServiceSnapshot.Bootstrapped, workflow.ServiceSnapshot.Deployed]
---

### Mode expansion — add a stage pair

This atom fires once per in-scope `mode: dev` or `mode: simple` (single-slot) service — for each, expanding to **standard** adds a stage sibling without touching the existing service. Expansion is an infrastructure change — it runs through the bootstrap workflow, not develop. Repeat the procedure below per service when multiple in-scope services need stage pairs.

```
zerops_workflow action="start" workflow="bootstrap"
  intent="expand {hostname} to standard — add stage"
```

Submit a plan that flags the existing runtime and names the new
stage hostname:

```json
{
  "runtime": {
    "devHostname": "{hostname}",
    "type": "<same type as current service>",
    "isExisting": true,
    "bootstrapMode": "standard",
    "stageHostname": "<new-stage-hostname>"
  },
  "dependencies": [
    { "hostname": "<existing dep>", "type": "<dep type>", "resolution": "EXISTS" }
  ]
}
```

Bootstrap leaves the existing service's code and runtime container untouched,
creates the new stage service via `zerops_import`, and at close the
envelope shows both snapshots:

- the original (now `mode: standard` with `stageHostname` set,
  `bootstrapped: true`, `deployed: true`, strategy intact);
- the new stage (`mode: stage`, `bootstrapped: true`,
  `deployed: false`).

After close, run a dev→stage cross-deploy to verify the pair
end-to-end.
