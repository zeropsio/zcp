# Research — completion predicate

Research is complete when both artifacts below hold on the server plan:

## Predicate

1. `plan.Research` is populated with every required field:
   - `serviceType`, `packageManager`, `httpPort` are set.
   - `buildCommands`, `deployFiles`, `startCommand`, `cacheStrategy` are set.
   - `dbDriver` and `migrationCmd` are set; `seedCmd` is set when the framework ships a seeder.
   - `needsAppSecret` is set; when true, `appSecretKey` names the framework-specific secret key.
   - `loggingDriver` is set.
   - Showcase recipes additionally have `cacheLib`, `sessionDriver`, `queueDriver`, `storageDriver`, `searchLib`, and `mailLib` populated for the managed-service kinds the recipe ships.

2. `plan.Research.Targets` carries one row per runtime target plus one row per managed service, each with `type` set to a concrete `<runtime>@<version>` taken from `availableStacks`. Any managed-service target that pins a non-latest version carries a one-sentence `typePinReason`.

3. `plan.SymbolContract` is populated in full: `EnvVarsByKind` covers every managed-service kind the targets declare; `Hostnames` has one row per runtime role; `FixRecurrenceRules` carries the 12 seeded rules filtered by `appliesTo` against the targets present.

## Attestation

```
zerops_workflow action="complete" step="research" attestation="Research populated: targets={list role+hostname+type per target}, managed services={list hostname+type}. SymbolContract computed: envVarsByKind covers {list kinds}, hostnames={list role→dev/stage}, fixRecurrenceRules={count} applied."
```

The attestation names the concrete target list and the contract's scope. Provision consumes `plan.Research.Targets` as input; scaffold, feature, and writer dispatches consume `plan.SymbolContract` as input. The contract is frozen from this point forward.
