---
id: develop-mode-expansion
priority: 6
phases: [develop-active]
deployStates: [deployed]
modes: [dev, simple]
title: "Mode expansion — add a stage pair"
---

### Mode expansion — add a stage pair

Current service runs in a single-slot mode (dev or simple). If you need
a production sibling (stage), expand to **standard** mode without
touching the existing service's code or strategy. Expansion is an
**infrastructure change**, not a code change — it goes through bootstrap,
not through the develop loop.

```
zerops_workflow action="start" workflow="bootstrap"
  intent="expand {hostname} to standard — add stage"
```

Then submit a plan that flags the existing runtime and names the new
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

The bootstrap flow will:

1. Leave the existing service's code and infrastructure untouched.
2. Create the new stage service via `zerops_import`.
3. Preserve your deploy strategy, `BootstrappedAt`, and first-deploy
   timestamp on the upgraded `ServiceMeta`.
4. Ask for a stage deploy (cross-deploy from the dev half) to verify
   the new pair works end to end.

After expansion, the develop briefing for this service reports
`mode=standard` and includes stage-specific guidance.
