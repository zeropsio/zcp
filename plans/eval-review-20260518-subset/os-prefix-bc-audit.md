# OS-prefix BC audit — workflow-wide

Auditing whether every type-string comparison in ZCP correctly handles
the post-Sunday-release composite form alongside legacy bare form.

## Methodology

Grepped `internal/` (excluding `_test.go`) for the following patterns and
inspected each candidate against the surrounding code to determine whether
the comparison is (1) load-bearing, (2) BC-vulnerable given the two
plausible input shapes (live API → composite, plan/recipe → bare), and
(3) whether each side originates from the same source (symmetric) or
crosses the plan↔catalog/plan↔live boundary (asymmetric):

```
grep -rn 'ServiceStackTypeVersionName'    # 28 sites, used as VALUE only at most → safe
grep -rn '\.Type\s*==\|\.Type\s*!='       # focuses non-event-type uses
grep -rn 'HasPrefix.*"<runtime|managed>"' # static lists scoped to known Zerops bases
grep -rn 'IsManagedService\|IsUtilityType\|IsRuntimeType\|IsObjectStorageType\|IsSharedStorageType'
grep -rn 'TypesAreEquivalent\|CanonicalBareForm'   # confirm already-fixed sites stay covered
grep -rn 'strings\.Cut.*"@"'              # base-vs-version splits — the cheap symptom
grep -rn 'switch base'                    # switch dispatch on base name
grep -rn 'serviceTypeSet\|BuildBaseSet\|RunBaseSet'  # strict-equal map lookups against live JSON schema
```

For each candidate I traced the data source of each side (live API,
agent-submitted plan, recipe yaml, JSON schema enum) and asked: when one
side is composite and the other bare, does this comparison produce the
right answer?

Cross-checked findings against the live schema mirror
(`internal/schema/testdata/import_yml_schema.json` and
`zerops_yml_schema.json`) — the upstream `services[].type` enum carries
**both** composite (`postgresql:single@18`) and legacy bare
(`postgresql@18`) values, but `zerops.yaml` `run.base` / `build.base` enums
carry **only** composite forms (`alpine/bun@1.2`, no bare `bun@1.2`). This
asymmetry matters for two sites in `internal/workflow/recipe_validate.go`
that are recipe scope (Aleš).

Classifier `topology.RuntimeClassFor` is out of scope (backlog
`plans/backlog/zerops-yaml-os-prefix-shape-drift.md`). Where downstream
sites depend on it, I note the silent mis-routing as context, not as a
separate BROKEN finding.

## Summary

- Total comparison sites inspected: **38**
- **BROKEN (need fix this round): 3**
  - `internal/workflow/validate.go:194-200` — `isManagedTypeWithLive` cut-before-lookup misses bare↔composite
  - `internal/ops/verify_checks.go:42-54` — `classifyRuntime` switch misses OS-prefixed implicit/static
  - `internal/ops/deploy_validate.go:438-447 & 473-492` — `IsImplicitWebServerType` + `hasImplicitWebServer` switch misses OS-prefixed (zerops.yaml `run.base` is composite-only)
- **SUSPECT (Karel verify): 6**
- **SAFE (intentional, already fixed, or shape-tolerant by accident): 29**

## Findings by severity

---

### BROKEN — needs fix this round

#### `internal/workflow/validate.go:192-200`

**What it does:** Plan-validator gate that decides whether a dependency
needs a `mode: HA / NON_HA` field; called downstream at `validate.go:351`
to default mode and reject unknown modes for managed deps.

**Current code:**

```go
func isManagedTypeWithLive(serviceType string, liveManaged map[string]bool) bool {
    base, _, _ := strings.Cut(serviceType, "@")
    if len(liveManaged) > 0 {
        return liveManaged[base]
    }
    return topology.IsManagedService(serviceType)
}
```

`liveManaged` is built by `knowledge.ManagedBaseNames(liveTypes)` which
also does `strings.Cut(v.Name, "@")`. Post-Sunday-release the live API
returns `postgresql:single@18`, so `ManagedBaseNames` stores key
`"postgresql:single"`. Plan-side dep type from a legacy/BC agent is
`postgresql@18` → cut gives `"postgresql"` → `liveManaged["postgresql"]`
== false → returns false → mode defaulting + HA/NON_HA validation are
SKIPPED for a managed dep. Downstream the plan still passes validation
but ships without mode normalization.

**Both sides bare?** ASYMMETRIC (plan side may be bare; live-API side is
now composite/mode-encoded). When live-API is absent the fallback
`topology.IsManagedService(serviceType)` HasPrefix path works for both
shapes — so the bug is specifically the `liveManaged` map lookup.

**BC concern:** **BROKEN** — plan-side (possibly bare) vs live-side
(composite) — strict-equal map lookup after string cut, fails when keys
differ in mode/OS decoration.

**Fix:** Two coordinated changes:
1. `knowledge.ManagedBaseNames` MUST canonicalize each `v.Name` via
   `topology.CanonicalBareForm` before the cut (so the stored key is
   `"postgresql"`, not `"postgresql:single"`).
2. `isManagedTypeWithLive` MUST canonicalize `serviceType` the same way
   before looking up the key.
   Pinning: extend `TestManagedBaseNames_*` with composite catalog
   fixtures; add a workflow-level test that submits a bare-form managed
   dep against a composite live catalog and asserts mode defaulting fires.

---

#### `internal/ops/verify_checks.go:41-54`

**What it does:** Runs after `zerops_verify` resolves a live service and
decides which verify checks to dispatch (HTTP probe vs worker treatment
vs static treatment vs implicit-web treatment). Misclassification means
the wrong check set runs — silent verify drift.

**Current code:**

```go
func classifyRuntime(serviceType string, hasPorts bool) RuntimeClass {
    base, _, _ := strings.Cut(serviceType, "@")
    switch base {
    case runtimePHPApach, runtimePHPNginx:
        return RuntimeImplicit
    case runtimeStatic, runtimeNginx:
        return RuntimeStatic
    }
    if !hasPorts {
        return RuntimeWorker
    }
    return RuntimeDynamic
}
```

`serviceType` is the LIVE API `ServiceStackTypeVersionName`. Post-Sunday-
release that is `alpine/php-nginx@8.4` / `alpine/nginx@1.22` / `ubuntu/
static@1.0` etc. `strings.Cut("alpine/php-nginx@8.4", "@")` yields base
`"alpine/php-nginx"`. The switch matches neither `"php-nginx"` nor
`"php-apache"`. Falls through to RuntimeDynamic. Effect: a PHP-Nginx
service no longer runs the implicit-web check set; a static service no
longer runs the static set. Verify output silently changes shape.

**Both sides bare?** ASYMMETRIC (right-hand-side literals are bare;
left-hand-side from live API is now composite).

**BC concern:** **BROKEN** — HasPrefix-style switch dispatch on a string
that now carries OS prefix. Distinct from `topology.RuntimeClassFor`
(backlog) — this is a SECOND, parallel classifier inside `ops` that the
backlog item doesn't cover. Two parallel classifiers should not exist;
this is the deeper "parallel paths" CLAUDE.md violation that the backlog
sketch hinted at but didn't enumerate.

**Fix:** Either (a) strip the OS prefix via `topology.CanonicalBareForm`
before the switch, or (b) replace `classifyRuntime` entirely with a
delegation to `topology.RuntimeClassFor` (once the backlog item fixes
that classifier) plus a worker check for `!hasPorts`. (b) is the
structural fix per CLAUDE.local.md (one canonical classifier, peer of
ops + workflow + tools). Pinning: extend any existing
`TestClassifyRuntime_*` table to cover the four OS-prefixed shapes
`alpine/{php-nginx,php-apache,nginx,static}@X`.

---

#### `internal/ops/deploy_validate.go:438-447, 473-492`

**What it does:** Two helpers that classify a service as "implicit web
server" (PHP-Nginx, PHP-Apache, Nginx, Static):

```go
// :440
func IsImplicitWebServerType(serviceType string) bool {
    b, _, _ := strings.Cut(serviceType, "@")
    switch b {
    case runtimePHPApach, runtimePHPNginx, runtimeNginx, runtimeStatic:
        return true
    }
    return false
}

// :476
func hasImplicitWebServer(runBase string, buildBases []string) bool {
    bases := append([]string{runBase}, buildBases...)
    for _, base := range bases {
        if base == "" { continue }
        if base == runtimeStatic { return true }
        b, _, _ := strings.Cut(base, "@")
        switch b {
        case runtimePHPApach, runtimePHPNginx, runtimeNginx, runtimeStatic:
            return true
        }
    }
    return false
}
```

`IsImplicitWebServerType` receives the LIVE API type; `hasImplicitWebServer`
receives `entry.Run.Base` / `entry.Build.Base*` from the parsed
zerops.yaml the user authored. The live `zerops.yaml` schema enum is now
**composite-only** for non-`static` bases (verified against
`internal/schema/testdata/zerops_yml_schema.json` — only the bare
`"static"` token survives). So:

- A user copying current-recipe-output yaml gets composite forms in
  `run.base` / `build.base`. `strings.Cut("alpine/php-nginx@8.4", "@")`
  → base `"alpine/php-nginx"`. Switch misses. Returns false.
- Downstream effect: `ValidateZeropsYml` then warns "run.start is empty
  — app will not start after deploy" even though the implicit-web flow
  is correct. False positive in the deploy preflight; agent may go
  fix-non-bug.
- Mirror effect via `IsImplicitWebServerType` taking the API type —
  same misclassification.

**Both sides bare?** ASYMMETRIC (LHS now composite from the YAML schema /
live API; RHS const bare).

**BC concern:** **BROKEN** — switch dispatch on `strings.Cut(@)` result
fails for OS-prefixed `run.base` / `build.base` values, which are the
only forms the live schema now lists. This is the deploy-side mirror of
the `verify_checks.go` finding.

**Fix:** Strip OS prefix via `topology.CanonicalBareForm` (or inline-
copy its `stripKnownOSPrefix` logic) before the switch. Same pattern in
both helpers — either consolidate into one or apply identically.
Pinning: extend any existing `TestValidateZeropsYml_*` or
`TestIsImplicitWebServerType_*` table to feed composite bases through.

---

### SUSPECT — Karel verify

#### `internal/ops/discover.go:130-132` & `:139`

**What it does:** `buildSummaryServiceInfo` surfaces `mode` field on
managed services and sets `IsInfrastructure` based on
`topology.ServiceSupportsMode` / `topology.IsManagedService`.

**Current code:**

```go
mode := ""
if topology.ServiceSupportsMode(typeVersion) {
    mode = svc.Mode
}
return ServiceInfo{
    ...
    IsInfrastructure: topology.IsManagedService(typeVersion),
```

Both helpers HasPrefix-walk the managed prefix list — they fire correctly
for `postgresql:single@18` because it does HasPrefix `"postgresql"`. So
the predicates are SHAPE-TOLERANT.

**BC concern:** SUSPECT — but a SECOND-ORDER question lurks: when the
API now encodes the deployment mode inside the type string itself
(`postgresql:single@18` vs `postgresql:ha@18`), is `svc.Mode` still
populated by the API at all? If so the surfacer is correct; if not, this
emits `mode: ""` for HA-encoded services and agents reading discover
output get a missing field. Not a comparison bug, but a presentation-
layer drift question that crosses the audit boundary.

**Fix:** Karel verify — what does the live API return for `Mode` field
on a `postgresql:ha@18` service? If empty, derive mode from the type
suffix via `topology.CanonicalBareForm` delta detection and surface it.
If populated, no change.

---

#### `internal/workflow/intent_dependencies.go:102-132 (RecipeServiceTypes)`

**What it does:** Parses recipe `import.yaml` and returns a normalized
dependency-family token list for `CompareStacks` consumption.

**Current code:**

```go
for _, svc := range doc.Services {
    if svc.Type == "" { continue }
    base, _, _ := strings.Cut(svc.Type, "@")
    base = strings.ToLower(base)
    if fam, ok := dependencyAliases[base]; ok { base = fam }
    if seen[base] { continue }
    ...
```

Recipe yamls today are author-written bare (`postgresql@18`,
`nodejs@22`) so this works. But if the recipe-yaml pipeline starts
emitting composite forms (per the backlog plan §5), e.g.
`postgresql:single@18`, the cut gives `"postgresql:single"`, no alias
hit, no group match, and the recipe is silently judged as having NO
postgresql even though it does. Comparison cascade is then wrong.

**Both sides bare?** SAFE today, ASYMMETRIC if recipe-yaml shape moves.

**BC concern:** SUSPECT — depends on whether recipe-yaml regeneration
ships composite forms. Recipe scope (Aleš) for the actual yaml emit;
this consumer in `workflow/` would need to read either shape.

**Fix:** Canonicalize via `topology.CanonicalBareForm` before the cut.
Two-line change; pinning by adding a table case with mode-encoded yaml.

---

#### `internal/eval/prompt.go:54-105 (runtimeTypes + extractFromImportYml)`

**What it does:** Eval prompt generation parses recipe content and
extracts the runtime/managed split via a hardcoded base-name set:

```go
var runtimeTypes = map[string]bool{
    "php-nginx": true, "php-apache": true, "php": true, "nodejs": true,
    "bun": true, "python": true, "go": true, "java": true, "dotnet": true,
    "ruby": true, "rust": true, "elixir": true, "gleam": true, "deno": true,
    "static": true,
}
// :94
baseType, _, _ := strings.Cut(svc.Type, "@")
if runtimeTypes[baseType] { ... }
```

If recipe markdown shows `type: alpine/nodejs@22` in the example yaml
block, cut gives `"alpine/nodejs"`, not in the set, falls through to
"managed" branch — eval scenario then has an inverted runtime/dep
classification.

**Both sides bare?** SUSPECT — depends on whether recipe markdown YAML
samples are bare or composite.

**BC concern:** SUSPECT — Aleš's recipe-emit scope determines actual
shape; eval/prompt is downstream.

**Fix:** Canonicalize via `topology.CanonicalBareForm` before the
map lookup.

---

#### `internal/eval/prompt.go:154-176 (inferRole)`

**What it does:** Eval helper that infers a service role from its type
+ hostname for downstream prompt rendering. Switch on
`strings.Cut(svcType, "@")` result against bare-name cases.

**Current code (selection):**

```go
baseType, _, _ := strings.Cut(svcType, "@")
switch baseType {
case "postgresql", "mariadb": return "db"
case "valkey", "keydb":       return "cache"
case "object-storage":        return "storage"
...
}
```

`postgresql:single@18` → base `"postgresql:single"` — no case hits → falls
to hostname. Inverted "db" → some-other-role.

**Both sides bare?** ASYMMETRIC if recipe markdown carries composite.

**BC concern:** SUSPECT — same scope/source question as the prior
finding.

**Fix:** Canonicalize before switch.

---

#### `internal/eval/verification.go:275-280 (matchTypeGlob)`

**What it does:** Eval-side type matcher with a single-trailing-`*`
wildcard:

```go
func matchTypeGlob(actual, pattern string) bool {
    if prefix, hadStar := strings.CutSuffix(pattern, "*"); hadStar {
        return strings.HasPrefix(actual, prefix)
    }
    return actual == pattern
}
```

If a scenario fixture expects `pattern = "postgresql@*"` and the live API
returns `actual = "postgresql:single@18"`, `HasPrefix("postgresql:single@18", "postgresql@")`
is FALSE. Exact `pattern = "postgresql@18"` matches against `"postgresql:single@18"`
is FALSE. Eval scenarios that pin a type via the glob shape now silently
fail/false-negative.

**Both sides bare?** ASYMMETRIC — fixtures often bare, live composite.

**BC concern:** SUSPECT — depends on fixture authoring. If eval scenarios
deliberately use the wildcard to be shape-tolerant, the fix is to make
the matcher equivalence-aware (use `TypesAreEquivalent` for the
non-wildcard case; for the wildcard, strip composite decoration on
`actual` before HasPrefix).

**Fix:** Replace exact `==` with `TypesAreEquivalent(actual, pattern)`;
for the `*` branch, run `topology.CanonicalBareForm(actual)` before
HasPrefix.

---

#### `internal/eval/probe.go:99`

**What it does:** Filters the ZCP control-plane container out of probe
candidates:

```go
if strings.HasPrefix(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName, "zcp@") {
    continue
}
```

`"zcp@1"` HasPrefix `"zcp@"` — TRUE. Survives the BC drift because `zcp`
is a single ZCP-controlled type that has not adopted OS-prefix or
mode-suffix encoding.

**Both sides bare?** SAFE today, SUSPECT in the future.

**BC concern:** SUSPECT — only breaks if ZCP itself ever ships an
OS-prefixed or mode-encoded variant (e.g. `alpine/zcp@1`). Not a
present-tense bug.

**Fix:** No change needed; flag for revisit if zcp@1 ever moves to
composite shape. Parallel site `tools/launch_source_context.go:162`
uses strict `== "zcp@1"` — same property, same flag.

---

### SAFE — already correct (or shape-tolerant by accident)

Brief notes (file:line + reason):

- `internal/topology/predicates.go:16-24, 50-65 (IsManagedService, IsObjectStorageType,
  IsSharedStorageType, IsUtilityType)` — HasPrefix against bare base name;
  composite `postgresql:single@18` and `alpine/nodejs@22` BOTH classify
  correctly because the prefix list keys are bare names that survive
  unchanged in mode-suffix encoding and absent in OS-prefix encoding.
- `internal/topology/predicates.go:71-73 (IsRuntimeType)` — defined by
  exclusion from IsManagedService + IsUtilityType, inherits their tolerance.
- `internal/topology/predicates.go:38-46 (ServiceSupportsMode, ServiceSupportsAutoscaling)`
  — composed of the prefix predicates; tolerant.
- `internal/workflow/validate.go:412-421 (typeAcceptedByCatalog)` — already
  uses `topology.TypesAreEquivalent`.
- `internal/workflow/recipe_override.go:172, 209` — already use
  `topology.TypesAreEquivalent`.
- `internal/tools/workflow_checks.go:247 (checkServiceType)` — already uses
  `topology.TypesAreEquivalent`.
- `internal/tools/workflow_checks.go:260-266 (isManagedNonStorage)` — delegates
  to topology predicates; tolerant.
- `internal/workflow/route.go:298 (adoptableServices)` — `IsManagedService`
  shape-tolerant.
- `internal/workflow/adopt_local.go:79, 257 (local adoption split)` —
  `IsManagedService` shape-tolerant.
- `internal/workflow/adopt.go:16-18 (isControlPlaneType)` — HasPrefix on
  `"zcp"` matches `zcp@1` and any hypothetical decorated form;
  trade-off is no Zerops type currently starts with `zcp` except the
  control plane.
- `internal/workflow/adopt.go:43, 46 (InferServicePairing)` — both sides
  same-source (live API), tolerant by symmetry.
- `internal/tools/workflow_route.go:41 (managed filter on live svc list)` —
  `IsManagedService` shape-tolerant.
- `internal/tools/workflow_adopt_local.go:95 (managed reject)` — same as above.
- `internal/tools/env.go:297 (isAutoRestartEligible)` — same as above.
- `internal/tools/launch_source_context.go:122 (ServiceStackTypeCategoryName != "USER")` —
  category-based, not type-based; both shapes carry the same category.
- `internal/tools/launch_source_context.go:162 (zcp@1 strict equality)` —
  load-bearing strict, same property as `isControlPlaneType`.
- `internal/ops/discover.go:139 (IsManagedService)` — shape-tolerant.
- `internal/ops/verify.go:107 (isManagedCategory)` — category-based,
  not type-based.
- `internal/ops/helpers.go:220-227 (dbServiceTypeWithBareConnectionString)` —
  cuts on `@` then switches on `"postgresql"` / `"mariadb"`. Composite mode-
  encoded form `postgresql:single@18` would `Cut` to `"postgresql:single"` →
  no match → returns false → no connectionString warning emitted on a
  HA-encoded service. This is a NEAR-MISS: the same pattern as the
  BROKEN findings, but the consequence is only the LOSS of a helpful
  warning (PostgreSQL connectionString shape note). Karel may want to
  promote to SUSPECT — flagging here so the audit covers it. Workable
  with same `CanonicalBareForm` strip pattern.
- `internal/ops/deploy_validate_api.go:71` — passes type through to the
  Zerops API which natively accepts both shapes per BC contract.
- `internal/ops/events.go:244` and `internal/tools/workflow_record_deploy.go:211` —
  these compare `ev.Type == "deploy" | "build"`. That is the
  TimelineEvent type (event kind), NOT a service-stack type. Off-topic.
- `internal/recipe/gate_object_storage_priority.go:21 (svc.Type != "object-storage")` —
  strict-equal but `object-storage` is the bare token kept in the live
  schema (no version suffix, no OS prefix possible — verified in
  testdata `"enum": ["object-storage", "objectstorage"]`). Tolerant.
- `internal/platform/zerops_validate.go:83 + zerops_mappers.go:109,139` —
  carries the type as a value through the SDK; no comparison.
- `internal/platform/types.go:65-67 (IsSystem)` — category-based.
- `internal/eval/probe.go:99 (zcp@ prefix)` — see SUSPECT entry above;
  property-equivalent to other zcp@1 sites.
- `internal/eval/verification.go:167 (exp.Type != "")` — empty-string
  check, not a type comparison.
- `internal/envclass/classify.go:76 (env.Type == platform.ProjectEnvSystem)` —
  ENV-VAR type (USER/SYSTEM), not service-stack type.

---

### Out of scope — recipe domain (Aleš)

These are listed for completeness; per CLAUDE.local.md they belong to
Aleš's recipe-generation scope. The same OS-prefix / mode-suffix shape
sensitivity applies — flag, never act:

- `internal/recipe/validators_tier_prose.go:180, 294-302 (HasPrefix on
  bare runtime/storage names)` — strict bare-prefix; composite forms
  fall through to the "managed" default.
- `internal/recipe/briefs.go:507 (HasPrefix(cb.BaseRuntime, "nodejs"))` —
  bare-only.
- `internal/recipe/consumes_services.go:121, 148` — `strings.IndexByte(family, '@')`
  + equality check against `serviceFamilyNATS`. Composite mode-encoded
  forms break the equality.
- `internal/recipe/tier_service_deltas.go:38 + yaml_emitter.go:405
  (managedServiceSupportsHA)` — same family-match shape.
- `internal/workflow/recipe_validate.go:100-138, 169-210, 380-387,
  recipe_decisions.go (workflow side of recipe pipeline)` — same
  family. Recipe scope.
- `internal/workflow/recipe_service_types.go:108-131 (serviceTypeKind,
  utilityBuildFromGitURL)` — switch on cut result.
- `internal/workflow/recipe_templates.go:574-595 (serviceIntroLabel)` —
  switch on cut result.
- `internal/workflow/symbol_contract.go:260-277 (contractKindForType)` —
  switch on cut result.
- `internal/workflow/recipe_plan_predicates.go:104-108 (isServeOnlyType),
  :130-144 (hasServeOnlyProd), :220-231 (hasManagedServiceCatalog)` —
  switch / map on cut result.
- `internal/workflow/recipe_knowledge_chain.go:294-301 (normalizeRuntimeBase)` —
  switch on bare base name.

These cluster around the recipe-yaml shape decision (Aleš's call: keep
recipe yamls bare-form vs migrate to composite). If recipe yamls move
to composite, ALL of these need the same `CanonicalBareForm` strip
pattern. If they stay bare, they continue working because both the
authored input and the hardcoded constants are bare.

---

## Recommended fix ordering

Lowest blast radius first:

1. **`internal/ops/verify_checks.go::classifyRuntime`** (one function,
   single caller via `verifyService`, well-bounded tests). Strip OS
   prefix or delegate to `topology.RuntimeClassFor`.
2. **`internal/ops/deploy_validate.go::IsImplicitWebServerType` +
   `hasImplicitWebServer`** (two adjacent helpers, same fix, used by
   `ValidateZeropsYml` + `ZeropsYmlEntry` methods). Strip OS prefix.
3. **`internal/workflow/validate.go::isManagedTypeWithLive` +
   `internal/knowledge/versions.go::ManagedBaseNames`** (paired
   change — must canonicalize at BOTH sides of the map; isolated
   change but two files).

After (1) and (2), one parallel-paths question remains: ZCP carries
**two** runtime classifiers, `topology.RuntimeClassFor` (general,
backlogged) and `ops.classifyRuntime` (verify-scoped, partially
overlapping enum). Long-term the verify classifier should fold into
the topology one; that's a separate plan, not this fix round.
