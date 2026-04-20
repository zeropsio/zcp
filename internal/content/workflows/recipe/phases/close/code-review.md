# Close — code-review substep entry

This substep completes when a framework code-review sub-agent has inspected the shipped codebases, returned its findings, and every CRITICAL / WRONG finding has been fixed and redeployed on dev + stage.

## What main does at this substep

1. **Compose the code-review dispatch brief** by stitching the atoms under `briefs/code-review/` with the recipe-specific context (framework name, appDir list, `plan.Features` feature list, `ZCP_CONTENT_MANIFEST.json` path from the writer's output, `SymbolContract`). The composition surface lives outside the transmitted prompt — the dispatch brief is a leaf artifact addressed only to the reviewer.
2. **Dispatch the code-review sub-agent** with the composed brief. One sub-agent per review; the reviewer is a framework expert (not a Zerops platform expert) and reviews framework-level code that the main agent's checks and platform validators do not catch.
3. **Await the return payload**. The reviewer returns findings tagged `[CRITICAL]`, `[WRONG]`, `[STYLE]`, or `[SYMPTOM]`. Apply every CRITICAL and WRONG fix the reviewer proposed (directly or via a small Edit on the mount).
4. **Redeploy the affected targets** so the fixes are live before close-browser-walk begins. If zerops.yaml or app source changed, `zerops_deploy` the affected targets on dev, then cross-deploy to stage. If only a finalize-layer artifact changed, re-run the finalize flow. A browser walk against code that hasn't shipped the fixes is meaningless.
5. **Attest the substep** once all CRITICAL + WRONG findings are fixed and the redeploy is green.

## Attestation

```
zerops_workflow action="complete" step="close" substep="code-review" attestation="{framework} expert sub-agent reviewed N files, found X CRIT / Y WRONG / Z STYLE. All CRIT and WRONG fixed and redeployed. Silent-swallow scan: clean. Feature coverage scan: clean (all {N} declared features present)."
```

The attestation names the finding counts and the fixes applied. Bare "review done" or "no issues found" attestations are rejected at the substep validator. If the reviewer found nothing, say so with "0 CRIT / 0 WRONG / 0 STYLE" and explicitly name the scans that ran clean.

## Scope separation — what this atom owns vs what the brief owns

This atom frames what main does at this substep. The reviewer's task instructions — what files to read, which antipatterns to scan for, how to report, which tools are permitted — live inside the transmitted dispatch brief, addressed to the reviewer alone. The separation is the point: each atom keeps one audience.
