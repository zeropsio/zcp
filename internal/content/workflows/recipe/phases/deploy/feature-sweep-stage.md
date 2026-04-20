# Substep: feature-sweep-stage

This substep completes when every api-surface feature in `plan.Features` returns 2xx with `application/json` on its declared health-check path against the stage endpoints. It is the second and final content-type gate — the stage bundle is built from the dev source (via cross-deploy), and the SPA-fallback class manifests specifically at stage because the `build.envVariables: VITE_API_URL: ${STAGE_API_URL}` bake is stage-specific. A dev-green sweep with a broken source-code half flips to `text/html` on stage.

## Sweep shape against stage URLs

For every feature F in `plan.Features` where `F.surface` contains `"api"`, curl the stage subdomain that serves the API for this feature:

```
curl -sS -o /dev/null -w '%{http_code} %{content_type}\n' https://{F.host}stage-{subdomainHost}-{F.port}.prg1.zerops.app{F.healthCheck}
```

For static-base stage services (where the API is a different service), curl the API service's subdomain — `apistage`, not `appstage`. The sweep targets the URL the frontend's bundle actually calls, which is whichever service's origin the baked `VITE_API_URL` (or equivalent) points at. Static-base appstage services still get swept for their ui-surface features (the dashboard returns the SPA index) but the api-surface features always route to the API service's origin — that is the purpose of `VITE_API_URL`. The sweep's feature list is unchanged between dev and stage; only host and port change.

## Attestation shape

Same per-feature format as `feature-sweep-dev` — one line per api-surface feature ID with the 2xx status and `application/json` content-type:

```
items-crud: 200 application/json
cache-demo: 200 application/json
storage-upload: 200 application/json
search-items: 200 application/json
jobs-dispatch: 200 application/json
```

Same validator, same contract — every declared api-surface feature ID appears with a 2xx status and `application/json`.

## If the stage sweep reports HTML

Any `text/html` on a stage sweep is the SPA-fallback class. The frontend bundle is hitting the local SPA fallback instead of the API origin. The fix surface is the source-code half of the dual-runtime pattern (`phases/generate/zerops-yaml/dual-runtime-consumption.md`): every `fetch()` call flows through an API-URL helper that reads the framework's build-time env var. Fix the helper, redeploy the frontend through `cross-deploy-stage` (which re-runs the build with the correct bake), re-run the sweep.

## Completion

Submit:

```
zerops_workflow action="complete" step="deploy" substep="feature-sweep-stage" attestation="<one line per api-surface feature against stage URLs>"
```

Only after this substep passes does the flow proceed to the `readmes` substep. A stage sweep that still reports HTML is a deploy-blocking bug — the recipe cannot ship until the source-code half of the dual-runtime pattern works at stage.
