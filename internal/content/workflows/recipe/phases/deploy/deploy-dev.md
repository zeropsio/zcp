# Substep: deploy-dev

This substep completes when every dev target in the plan returns `ACTIVE` from `zerops_deploy setup=dev`. The deploy builds from the files already on the SSHFS mount; no git push is required.

## Single dispatch shape

For a single dev target:

```
zerops_deploy targetService="appdev" setup="dev"
```

The `setup="dev"` parameter maps the hostname `appdev` to the `setup: dev` block in zerops.yaml. The call blocks until the build completes and returns a structured status.

## Batched dispatch for clusters (≥2 targets)

When deploying more than one service in the cluster — initial dev for two or three codebases, snapshot-dev after the feature sub-agent, stage cross-deploy, close-time redeploys — use `zerops_deploy_batch` in a single MCP call. The platform runs the N builds in parallel server-side and aggregates results in one response.

```
zerops_deploy_batch targets=[
  {"targetService": "apidev", "setup": "dev"},
  {"targetService": "appdev", "setup": "dev"},
  {"targetService": "workerdev", "setup": "dev"}
]
```

Per-target failures run independently; a failing sibling does not cancel the others. The response aggregates per-target results. Apply targeted fixes by calling `zerops_deploy targetService=X setup=Y` on the failing target alone, not by rolling back the whole cluster.

Single-service redeploys (e.g. one failing target above, or a worker rebuild after a fix) still call `zerops_deploy` directly — the batch tool is only worth its overhead at two or more targets.

## Dual-runtime (API-first) ordering

When the plan declares an API-first shape (`apidev` + `appdev`, optionally with `workerdev`), deploy the API first. The frontend depends on the API being reachable before its own build — in build-time-baked configurations the frontend bakes the API URL into its bundle at build time; in runtime-config configurations the frontend still verifies reachability during its walk.

Order:

1. `zerops_deploy targetService="apidev" setup="dev"` — blocks until API build completes.
2. Start the API process (see the `start-processes` substep for the exact shape).
3. `zerops_subdomain action="enable" serviceHostname="apidev"` then `zerops_verify serviceHostname="apidev"`. Confirm the API's health endpoint returns 200 via curl before moving to the frontend deploy.
4. `zerops_deploy targetService="appdev" setup="dev"` — blocks until appdev build completes.
5. Start appdev processes, enable its subdomain, verify the dashboard loads and fetches from the API.

API-first log reading is dual-container. The API typically owns migration and seed commands and the frontend is often a static build with no initCommands at all. After both deploys are ACTIVE, fetch logs from both hostnames side-by-side:

```
zerops_logs serviceHostname="apidev" limit=200 severity=INFO since=10m
zerops_logs serviceHostname="appdev" limit=200 severity=INFO since=10m
```

The API must emit CORS headers allowing the frontend subdomain. Use the framework's standard CORS middleware and allow the frontend's subdomain origin.

## Deploy output is a fresh container

Every `zerops_deploy` call creates a fresh container. All background processes from the previous container (asset dev server, queue worker) are gone after a redeploy. The `start-processes` substep restarts them; iteration inside this substep (fix code, redeploy, re-verify) counts as one full pass through start-processes and verify-dev — never skip restart on a redeploy.

Iteration budget within this substep: up to 3 rounds of fix → redeploy → re-verify before escalating. Each round goes through start-processes and verify-dev again.
