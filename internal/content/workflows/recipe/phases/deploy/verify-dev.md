# Substep: verify-dev

This substep completes when every dev-phase runtime target in the plan is verified healthy. Every runtime target the plan declared must be verified — HTTP targets use `zerops_verify` plus `zerops_subdomain`, non-HTTP targets (workers) use `zerops_logs` to confirm the process started.

## Targets to verify (by plan shape)

Enumerate from `plan.Research` and verify every target below. A skipped verification means a broken target may ship to stage undetected.

- **Single-runtime minimal** — `appdev` (HTTP: verify + subdomain).
- **Single-runtime showcase, shared worker** — `appdev` (HTTP: verify + subdomain). The worker logs live in `appdev` because the worker runs as a background process on the host target's container.
- **Single-runtime showcase, separate worker** — `appdev` (HTTP: verify + subdomain) plus `workerdev` (logs only; no HTTP endpoint).
- **Dual-runtime minimal** — `appdev` (HTTP) plus `apidev` (HTTP).
- **Dual-runtime showcase** — `appdev` (HTTP) plus `apidev` (HTTP) plus `workerdev` (logs only).

## HTTP target verification shape

For every HTTP target in the list above, dispatch the subdomain enable and the verify call. In dual-runtime shapes, the API-side pair runs first (see the `deploy-dev` substep's API-first ordering); this substep closes by running both pairs.

```
zerops_subdomain action="enable" serviceHostname="appdev"
zerops_verify serviceHostname="appdev"
```

`zerops_verify` runs a standardized check suite that surfaces readiness-probe misconfiguration, env-var binding failures, and container state inconsistencies that a plain `curl` misses. The subdomain enable is idempotent — calling it on a dev target that already has a subdomain returns the existing one.

## Worker (non-HTTP) verification shape

For every worker target without an HTTP endpoint, verify the process is running by reading its container logs. The log hostname depends on the recipe's `sharesCodebaseWith` shape:

- **Shared-codebase worker** (`sharesCodebaseWith` names `app` or `api`) — the worker runs in the host target's container, so its logs live there. Use `zerops_logs serviceHostname="appdev"` for an app-shared worker, `serviceHostname="apidev"` for an api-shared worker.
- **Separate-codebase worker** (`sharesCodebaseWith` is empty) — the worker owns its own container. Use `zerops_logs serviceHostname="workerdev"`.

```
zerops_logs serviceHostname="{worker_hostname}" limit=20
```

Look for the worker's start-ready signal — the library's subscription-ready log line, a "listening" message, or the consumer-loop entry log. A worker whose logs show a crash or startup exception fails verify-dev and the fix + redeploy loop reruns.

## Attestation shape

Submit one attestation line per verified target — service name, status (RUNNING / subdomain ACTIVE / logs show started), and the observable signal you checked. A target without a verify attestation line fails the substep gate.
