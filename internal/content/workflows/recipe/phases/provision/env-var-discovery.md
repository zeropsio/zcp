# Provision — 4. Discover env vars (mandatory before generate)

Run `zerops_discover includeEnvs=true` once every service has reached RUNNING. The response contains the real env var keys every managed service exposes. These are the authoritative names the zerops.yaml `run.envVariables` written in the generate step will reference.

Use the names from this response exactly as returned. Guessed names the platform does not recognise resolve at runtime to their literal source string — for example, `${search_apiKey}` when the real key is `${search_masterKey}` reaches the application as the literal `"${search_apiKey}"`, not as a resolved value, and the failure is silent at deploy time.

## Catalog the output on the attestation

Record the list of env var keys for every managed service in the provision-step attestation so the generate step has the authoritative list alongside the plan. Every managed service hostname gets its key list; every dev mount path is listed. Example shape (fill from the actual plan and discovery output):

```
Services: {runtime dev/stage pairs provisioned}, {managed service hostnames}.
Env var catalog:
  {managedServiceHostname}: {env var keys returned by zerops_discover for this service}
  ...
Dev mounts: {dev mount paths, one per codebase}
```

The placeholder names are intentionally abstract — replace them with the exact hostnames and keys the workspace reported. The shape of the list (number of dev/stage pairs, number of dev mounts) follows from the `sharesCodebaseWith` decisions recorded in `plan.Research.Targets`: single-codebase plans have one dev mount; multi-codebase plans have one per `sharesCodebaseWith` group. The authoritative shape table lives under "zerops.yaml — Write ALL setups at once" in the generate step.

## Contract cross-check

The keys discovered here are the ground truth for `plan.SymbolContract.EnvVarsByKind`. If a managed service returns a set that surprises the contract (a missing key the contract expected, or a key name the contract did not expect), stop and investigate — provision does not continue with a contract mismatch.

## When to skip discovery

Plans that provision no managed services (a pure static-frontend with no database, cache, queue, storage, or search service) have no env-var catalog to populate. Skip `zerops_discover` entirely — the generate step's `run.envVariables` for these plans lists only platform-provided vars (hostname, serviceId) and zero cross-service references.

## Signals that require investigation

- A managed service returns no `hostname` in its env catalog.
- A key name the plan did not expect appears in the catalog, with no corresponding kind in `plan.SymbolContract`.
- A key name the plan expected is absent from the catalog.

Each of these means provision stops and the plan or the contract is reconciled before generate starts.
