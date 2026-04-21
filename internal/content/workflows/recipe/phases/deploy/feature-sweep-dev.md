# Substep: feature-sweep-dev

This substep completes when every api-surface feature in `plan.Features` returns 2xx with `application/json` on its declared health-check path against the dev containers. It is a gate — the browser walk runs only after the sweep passes, and cross-deploy-stage runs only after the browser walk passes.

Minimal recipes run this substep too. The rule is tier-independent — every declared api-surface feature must sweep-green before cross-deploy. Minimal recipes usually have one or two features, which makes the sweep trivially short.

## Sweep shape (iterate plan.Features)

For every feature F in `plan.Features` where `F.surface` contains `"api"`, run one curl against the dev container and capture both status and content-type:

```
ssh {F.host}dev "curl -sS -o /dev/null -w '%{http_code} %{content_type}\n' http://localhost:{F.port}{F.healthCheck}"
```

- `{F.host}` — the apidev hostname for api-role features in dual-runtime recipes; `appdev` for single-runtime recipes.
- `{F.port}` — `plan.Research.HTTPPort`, the API's HTTP port as declared at research time.
- `{F.healthCheck}` — the path string the plan declared, e.g. `/api/items`, `/api/search`.

## Attestation shape

Submit the sweep result as one line per api-surface feature in the exact format `<featureId>: <status> <content-type>`:

```
items-crud: 200 application/json
cache-demo: 200 application/json
storage-upload: 200 application/json
search-items: 200 application/json
jobs-dispatch: 200 application/json
```

## Pass criteria

Every api-surface feature ID from `plan.Features` appears on its own line. Every line carries a 2xx status token. Every line contains `application/json` (case-insensitive). No line contains `text/html` and no line carries a 4xx or 5xx status.

## If the sweep fails

- **Feature returns `text/html` under 200** — the frontend is hitting the SPA fallback. The source-code half of the dual-runtime URL pattern (see `phases/generate/zerops-yaml/dual-runtime-consumption.md`) is the fix surface: every `fetch()` call goes through an API-URL helper that reads the framework's build-time env var (for Vite, `import.meta.env.VITE_API_URL`). Fix the helper, redeploy through `snapshot-dev`, re-run the sweep. Never attest success on an HTML response — the validator rejects it, and the browser walk that follows would render an empty dashboard.
- **Feature returns 4xx or 5xx** — the backend is broken. Fetch `zerops_logs serviceHostname={host}dev severity=ERROR since=5m`, fix the source on the mount, redeploy if the fix needs a fresh container, restart processes, re-run the sweep.

The substep gate is firm — every declared api-surface feature attests 2xx `application/json` before the gate opens. A partial pass ("4 of 5 features green") is a substep failure.

## Not part of this sweep

UI-only features (`surface` contains `ui` but not `api`) are exercised in the `browser-walk-dev` substep. Worker-only features (`surface` contains `worker` without `api`) surface either in the browser walk's result-element check or via `zerops_logs` against the worker container — the feature's `MustObserve` declares which.

## Completion

```
zerops_workflow action="complete" step="deploy" substep="feature-sweep-dev" attestation="<one line per api-surface feature, in the format shown above>"
```
