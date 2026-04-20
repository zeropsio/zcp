# Substep: verify-stage

This substep completes when every `*stage` runtime target is verified healthy. The verification shape mirrors `verify-dev` but runs against the stage subdomains enabled now.

## Dispatch shape — parallel across targets

After every `zerops_deploy` in `cross-deploy-stage` returns `ACTIVE`, dispatch the subdomain enables and verifies as parallel tool calls. Each targets a different service and has no dependency on the others.

```
zerops_subdomain action="enable" serviceHostname="appstage"
zerops_subdomain action="enable" serviceHostname="apistage"     # API-first only
zerops_verify serviceHostname="appstage"
zerops_verify serviceHostname="apistage"                         # API-first only
zerops_logs serviceHostname="workerstage" limit=20               # showcase only, no HTTP
```

`zerops_verify` is mandatory for every runtime target after every deploy — dev and stage alike. It runs a standardized check suite that surfaces readiness-probe misconfiguration, env-var binding failures, and container state inconsistencies that `curl` alone misses. Call it for every `{name}dev` after self-deploy, and for every `{name}stage` after cross-deploy.

Worker targets without HTTP: skip `zerops_verify` (it checks HTTP endpoints) and confirm the process is running via `zerops_logs` instead.

## Targets to verify (by plan shape)

Every target below must attest in the submission. A target with no attestation line fails the substep gate.

- **Single-runtime minimal** — `appstage` (verify + subdomain).
- **Single-runtime showcase** — `appstage` (verify + subdomain) plus `workerstage` (logs).
- **Dual-runtime minimal** — `appstage` plus `apistage` (both verify + subdomain).
- **Dual-runtime showcase** — `appstage` plus `apistage` (both verify + subdomain) plus `workerstage` (logs).

## Worker log reading shape

For the showcase worker target at stage, read the container logs and confirm the worker has subscribed to its broker and is ready for messages. The signal is framework-specific — a NATS "subscribed" log line, a "listening" message, the consumer-loop entry log. A worker whose stage logs show a crash or a startup exception fails verify-stage and the fix loop returns to the dev source.

```
zerops_logs serviceHostname="workerstage" limit=20
```

## Attestation shape

One line per target: target name, status (RUNNING / subdomain ACTIVE / logs show started), and the observable signal you checked. All stage targets attest before this substep closes and the `feature-sweep-stage` substep begins.

## Present URLs

Record the live subdomain URLs for every stage service — the `completion` substep and the `readmes` substep both reference them.
