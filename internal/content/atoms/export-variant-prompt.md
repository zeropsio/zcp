---
id: export-variant-prompt
priority: 2
phases: [export-active]
exportStatus: [variant-prompt]
modes: [standard, local-stage, stage]
environments: [container]
title: "Pick which half of the pair to package: dev or stage"
---
You are at `status="variant-prompt"`. The chosen `targetService` is part of a `mode=standard` (or `mode=local-stage`) pair, and the export workflow needs to know which half — `dev` or `stage` — to package into the bundle.

## What the variant decides

`variant="dev"` packages the dev hostname's working tree + the live `zerops.yaml` from `/var/www`. `variant="stage"` packages the stage hostname's tree. Each variant produces a different bundle because the two halves have different deploy histories, env values, and (sometimes) different `zerops.yaml` `setup:` blocks.

<!-- axis-k-keep: signal-#3 -->
Both bundles emit Zerops scaling `mode=NON_HA` for the runtime entry — single-runtime imports collapse to standalone scaling regardless of source variant. The destination project's topology mode (the `dev` / `stage` / `simple` / `local-stage` / `local-only` decision) is established by ZCP's bootstrap on the destination side at re-import, not embedded in the bundle.

## Re-call with `variant`

```
zerops_workflow workflow="export" targetService="{hostname}" variant="dev"
```

Substitute `variant="stage"` for the stage half. The `targetService` hostname must match the variant: the dev hostname goes with `variant="dev"`, the stage hostname with `variant="stage"` — passing the dev hostname with `variant="stage"` is a mismatch and the handler errors.

After the next call, the response advances to the next status (`scaffold-required` / `git-push-setup-required` / `classify-prompt` / `validation-failed` / `publish-ready`) depending on which preconditions hold for the chosen half.
