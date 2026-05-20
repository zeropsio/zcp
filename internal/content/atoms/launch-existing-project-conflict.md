---
id: launch-existing-project-conflict
priority: 4
phases: [launch-production-active]
title: "Existing-project conflicts — ask per-service skip / replace"
references-fields: []
---

### Existing-project conflicts — ask per-service skip / replace

Existing-project launch (you passed `existingProjectId` + `existingProdToken`) detected services in the target project whose hostnames collide with what the launch bundle would create. **Production must not silently overwrite the user's existing services.** Ask the user per conflict; re-call with `mergeStrategy` populated.

**Per-blocker shape.** Each blocker's `id` is `existing-project-conflict-<hostname>`. The `message` carries the existing service's type + status so the user has context to choose. AskUserQuestion the user per conflict with these options:

| Choice | What it does | Re-call shape |
|---|---|---|
| **Skip** (additive launch — recommended default) | Drop this entry from the bundle. The existing target service is left untouched. Other promotables still get created. | `mergeStrategy={"<hostname>": "skip"}` |
| **Replace** | Overwrite the existing target service with what's in the source. Destructive — requires explicit `confirmDestructive` ack. | `mergeStrategy={"<hostname>": "replace"}` + `confirmDestructive={operation: "launch-production-replace", acknowledgedTargets: ["<hostname>", ...]}` |
| **Cancel** | Abort the launch. No mutation. | Stop calling; pass the cancellation back to the user. |

**Replace requires destructive ack.** The diagnose-before-destruct invariant extends here: you cannot merge `replace` into the launch without a populated `confirmDestructive` whose `acknowledgedTargets` lists EVERY hostname flagged `replace`. The launch handler refuses with `existing-project-replace-needs-ack` until both align.

**Worked example.** Source has `app`, `worker`, `db`, `redis`; target already has `app`, `db`. User wants to additively promote `worker` + `redis`, keep existing `app` + `db`:

```
zerops_workflow action="start" workflow="launch-production"
  productionProjectName=<from inputs>
  existingProjectId=<from inputs>
  existingProdToken=<from inputs>
  promotables=[{hostname: appstage}, {hostname: workerstage}]
  envClassifications=<from inputs>
  mergeStrategy={"app": "skip", "db": "skip"}
```

After this re-call, the composer drops the `app` runtime + `db` managed dep from the bundle and the launch advances to classify-prompt (or directly to ready-to-launch, depending on prior progress).

**For the same scenario but the user wants to replace `app`:**

```
zerops_workflow action="start" workflow="launch-production"
  ... (same as above) ...
  mergeStrategy={"app": "replace", "db": "skip"}
  confirmDestructive=<operation: launch-production-replace, acknowledgedTargets: [app]>
```

**Do not** invent a strategy without asking the user. The conflict prompt is the explicit "what do you want to do" handoff; ZCP refuses to advance without per-conflict resolution.

After every conflict has a `mergeStrategy` entry (and every `replace` is ack'd via `confirmDestructive`), re-call launch — the handler proceeds through the canonical mutation pipeline.
