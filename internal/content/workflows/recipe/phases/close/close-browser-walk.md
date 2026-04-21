# Close — browser walk (showcase)

This substep completes when the main agent has walked every declared feature in a browser on BOTH the dev subdomain AND the stage subdomain, every `MustObserve` satisfied, every `[data-error]` banner empty, and no JS console errors or failed network requests surface.

The walk reuses the same 3-phase feature-iterating shape as the deploy-step browser walk. The iteration template, per-feature pass criteria, and command vocabulary live in `phases/deploy/browser-walk-dev.md` — those rules apply here unchanged.

## Close-specific rules (on top of the deploy-step rules)

- **Rebuild the commands array from `plan.Features` every walk** — the command array is read live from the feature list, not reused from the deploy walk. The code-review sub-agent may have added `data-feature` hooks during its fix pass; a stale array would silently skip the newly added features. Read `plan.Features` at the start of this substep and regenerate the walk commands against it.
- **Re-run the feature-sweep against stage URLs first** — before starting the browser walk, run the stage feature-sweep (curl-level). The code-review substep's redeploy may have shifted behavior; the sweep has to come back green on every API-surface feature before the UI walk iterates. Sweep is the curl-level gate; the walk is the user-level gate; both pass at close.
- **Main agent is single-threaded here** — the browser walk runs from main, not from any sub-agent. If a walk surfaces a problem: the tool closes the browser itself, so fix on mount, redeploy the affected target, re-run the affected sweep, then re-call the browser tool for the affected subdomain. Each re-verification counts toward the close-step's 3-iteration budget.
- **Walk both dev AND stage every time** — a walk that only covers one subdomain is rejected at the substep validator.

## Close-step pass criteria (belt-and-suspenders)

1. Code-review substep: every CRITICAL / WRONG finding fixed, silent-swallow scan clean, feature-coverage scan clean.
2. Stage feature-sweep: every API-surface feature returns 2xx with `application/json` (no `text/html`).
3. Browser walk (dev + stage): every UI-surface feature satisfies its `MustObserve`, every `[data-error]` banner empty, no JS console errors, no failed network requests.

Close proceeds only when every layer is green.

## Attestation

```
zerops_workflow action="complete" step="close" substep="close-browser-walk" attestation="Browser walk iterated {N} features on dev AND stage. Every MustObserve satisfied. [data-error] empty across all sections. No JS console errors, no failed network requests. Rebuilt commands from plan.Features live — no cached array."
```

The attestation names the feature count and explicitly confirms BOTH dev and stage walks passed.

Only after BOTH `substep="code-review"` AND `substep="close-browser-walk"` are attested can the step-level `zerops_workflow action="complete" step="close"` call land. Attempting step-complete without both substeps returns an error naming the pending ones.
