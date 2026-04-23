# API Validation — Fundamentals Plan

> **Scope**: Make ZCP a clean surface over Zerops's own validation APIs. Three
> structural changes, done together: (A) plumb the API's structured error
> `meta` through to the LLM, (B) call Zerops's `zerops-yaml-validation`
> endpoint before deploy, (C) delete all client-side validation that duplicates
> what the API already does. The result: LLM-facing errors carry field-level
> detail for every 4xx response across all MCP tools, pre-deploy YAML issues
> surface before a build cycle is wasted, and ZCP no longer maintains a
> shadow validator that can drift from the server.
>
> **Companion docs**:
> - `CLAUDE.md` — single-path engineering, fix-at-source, no fallbacks.
> - `CLAUDE.local.md` — Zerops platform knowledge rules.
> - `plans/friction-root-causes.md §2.3 (P2.3)` — superseded by this plan.

---

## 0. Principle

Trust the platform. Zerops API is the authoritative validator for every
platform concept: service types, modes, field names, YAML syntax, cross-field
constraints, versions, hostnames. ZCP's job at the API boundary is two things
only:

1. **Surface** what the API tells us about invalid input, with enough field-
   level detail that the LLM can fix the input without guessing.
2. **Catch** the narrow class of errors the API genuinely cannot know:
   filesystem state, ZCP-specific role semantics, cross-reference shape
   against live services, and known-gotcha values the API accepts but drops
   (e.g. `envVariables` at import service-level).

Any validation outside those two categories is technical debt: it can go
stale against the platform, it adds maintenance cost, it delays deploys, and
— when it diverges — it blocks valid configurations and confuses the LLM.
Delete it.

---

## 1. Empirical evidence (measured 2026-04-23)

Live API against project `eval-zcp` (`i6HLVWoiQeeLv8tV0ZZ0EQ`), SDK v1.0.17.
Probe: `cmd/probe-import-meta/`.

### 1.1 OpenAPI audit

The complete OpenAPI spec (`https://api.app-prg1.zerops.io/api/rest/public/swagger/openapi.yml`
— 710 KB, 17 371 lines, 203 operations) exposes exactly one YAML validation
endpoint relevant to ZCP:

- `POST /api/rest/public/service-stack/zerops-yaml-validation`
  - Operation: `ValidateZeropsYaml`
  - Request: `RequestZeropsYamlValidation` — `serviceStackName`,
    `serviceStackTypeId`, `serviceStackTypeVersionName`, `zeropsYaml`,
    `operation` (enum `DEPLOY | BUILD_AND_DEPLOY`, default `DEPLOY`),
    `zeropsYamlSetup` (nullable).
  - Response: `ResponseSuccess` (`{"success": true}`) on 200; `Error` with
    structured `meta[]` on 400.

**There is no import-YAML validation endpoint.** Import is validated by the
commit endpoint itself (`ImportServiceStack`, `ImportProject`); schema-level
rejections happen before any state change, and the response carries
field-level detail in `error.meta[].metadata`.

### 1.2 `projectImportInvalidParameter` returns field detail

Probe against `POST /project/{pid}/service-stack/import`:

| Input | Code | Meta.metadata |
|---|---|---|
| `object-storage + mode:NON_HA` | `projectImportInvalidParameter` | `{"{host}.mode":["mode not supported"]}` |
| `object-storage + mode:HA` | `projectImportInvalidParameter` | `{"{host}.mode":["mode not supported"]}` |
| `postgresql + mode:WEIRD` | `projectImportInvalidParameter` | `{"{host}.mode":["Allowed values: [HA, NON_HA], WEIRD given."]}` |
| `postgresql` bez mode | `projectImportMissingParameter` | `{"parameter":["{host}.mode"]}` |
| `nodejs@99` | `serviceStackTypeNotFound` | `{"serviceStackTypeVersion":["nodejs@99"]}` |
| Invalid hostname | `serviceStackNameInvalid` | `null` (kód sám je specifický) |

F#7 ("API returns no field context") is **wrong in its premise**. API returns
the field. ZCP drops it in `internal/platform/zerops_errors.go:54-94`
(`mapAPIError` ignores `apiErr.GetMeta()`) and again for per-service errors in
`internal/platform/zerops_search.go:64-69` (`APIError` struct has no `Meta`).

### 1.3 `ValidateZeropsYaml` returns field detail too

Probe against `POST /service-stack/zerops-yaml-validation`:

| zerops.yaml content | HTTP | Meta |
|---|---|---|
| Valid | 200 | `{"success":true}` |
| `build.base: nodejs@99` | 400 | `[{"zeropsYamlInvalidParameter", metadata: {"build.base":["unknown base nodejs@99"], "build.os":["unknown os "]}}, {"run.base":["nodejs@99"], ...}]` |
| `httpSupport: bogus` (type mismatch) | 400 | `{"content":[...], "reason":["yaml: line 5: cannot unmarshal !!str bogus into bool"]}` |
| Invalid YAML syntax | 400 | `{"reason":["yaml: line 2: did not find expected ',' or ']'"]}` |
| Unknown field (`bogusField: yes` under `run`) | **200** | — **silently accepted** |
| Missing `run` block | 200 | — accepted |

Server catches runtime-version existence, YAML syntax with line numbers, and
type-level mismatches. Server does **not** catch unknown-field-name ("bogus
field ignored") — that's the one semantic surface where ZCP's client-side
check remains legitimate (it differs from API behavior in a user-hostile way).

### 1.4 ZCP today — duplicative + missing validation surfaces

Inventory of client-side validation (non-test, non-semantic):

| File | Function | Status |
|---|---|---|
| `internal/knowledge/versions.go:149` | `ValidateServiceTypes` — type existence | **duplicate** of `serviceStackTypeNotFound` |
| `internal/knowledge/versions.go:220-236` | mode enum / mode required for managed | **duplicate** of `projectImportInvalidParameter` / `projectImportMissingParameter` |
| `internal/knowledge/versions.go:239-247` | objectStoragePolicy enum | **duplicate** |
| `internal/knowledge/versions.go:259-` | `ValidateProjectFields` — project-level enums | **duplicate** |
| `internal/schema/validate.go:57` | `ValidateZeropsYmlRaw` — field existence in zerops.yaml | **duplicate** of server validation (except silent-ignore gap above — see §5 K3) |
| `internal/ops/checks/yml_schema.go` | `CheckZeropsYmlFields` — wraps the above | **duplicate** |
| `internal/ops/import.go:108-117` | wiring of `ValidateServiceTypes` | delete once `ValidateServiceTypes` is gone |

Validation that stays (legitimate — see §5):

- `internal/ops/deploy_validate.go ValidateZeropsYml` — semantic (role, paths,
  sudo, empty-start/ports).
- `internal/ops/env_generate.go` — cross-service ref resolution against live
  services.
- `internal/ops/env.go rejectEncodingPrefixedSecrets` — typ-gotcha.
- `internal/ops/import.go` envVariables silent-drop warning — API accepts
  but silently drops.
- `internal/ops/import.go` hostname format — ZCP-side UX sugar (API catches
  too, but fast-fail at the right layer is fine).
- `internal/ops/import.go project: key rejection` — keep (specific message
  beats generic API error).

ZCP does **not** call `ValidateZeropsYaml` anywhere. Deploy flows push the
YAML and let the build fail when it's broken — a wasted build cycle every
time.

---

## 2. Non-negotiable constraints

1. Every API 4xx error that reaches the LLM via MCP **must** include the
   server's `meta` (field + reason) when the server sent it. No exceptions;
   no stripping for "tidy" error shapes.
2. Every commit on this plan keeps `go test ./... -count=1 -short` green.
   Single-path engineering (CLAUDE.md): no "v2" error paths alongside old
   ones, no feature flags.
3. Client-side validation that the server performs is **deleted**, not
   warning-downgraded, not commented out. History lives in git.
4. `zerops_import` and `zerops_workflow` tools stay structurally backward-
   compatible in their JSON response shape — `apiMeta` is an additive field.
5. `ValidateZeropsYaml` pre-deploy is **blocking** at the deploy tool layer
   (a structured error stops deploy; on 200 deploy proceeds). The endpoint
   is advisory, so a non-2xx network failure of the validator must **not**
   block deploy — log-and-proceed, same pattern as `ValidateServiceTypes`
   warnings today.
6. No new `importPreValidationRules` tables. This plan supersedes
   `friction-root-causes.md §2.3 P2.3` — pre-validation is deleted, not
   shipped.

---

## 3. Scope — what changes

```
A. PlatformError.APIMeta plumbing  ─────────────────────────┐
                                                            │
B. Per-service APIError.Meta surfacing  ────────────────────┤
                                                            │──→ single "API
C. convertError serializes apiMeta into LLM JSON  ──────────┤    error surface"
                                                            │    for every tool
D. Platform.Client.ValidateZeropsYaml method  ──────────────┤
                                                            │
E. deploy_{ssh,local,git_push}.go call validator pre-push  ─┘

F. Delete knowledge.ValidateServiceTypes + ValidateProjectFields (import duplicates)
G. Delete ops/import.go's ValidateServiceTypes call; retain envVariables warning
H. Leave recipe flow untouched — schema.ValidateZeropsYmlRaw, ExtractValidFields,
   checks/yml_schema.go, workflow_checks_recipe.go all stay as-is
I. Prune deploy_validate.go ValidateZeropsYml (role + fs checks stay)
J. Update atoms to reflect new error shape (apiMeta field)

K. Add TA-06 invariant: every new platform.Client method that calls a live
   API endpoint propagates meta into PlatformError.APIMeta.
L. Contract test: mapAPIError preserves Meta for every fixture in apitest/.
```

---

## 4. Workstreams

All numbered workstreams; dependencies in §7.

### W1 — `PlatformError.APIMeta` plumbing

**Files**: `internal/platform/errors.go`, `internal/platform/zerops_errors.go`,
`internal/platform/zerops_errors_test.go`, `internal/platform/apitest/fixtures.go`.

**Design**:

```go
// internal/platform/errors.go
type APIMetaItem struct {
    Code     string              `json:"code,omitempty"`
    Error    string              `json:"error,omitempty"`
    Metadata map[string][]string `json:"metadata,omitempty"`
}

type PlatformError struct {
    Code       string
    Message    string
    Suggestion string
    APICode    string
    Diagnostic string
    APIMeta    []APIMetaItem // NEW — server-provided field-level detail
}
```

**`mapAPIError`**: after computing `errCode` and `msg`, decode `apiErr.GetMeta()`
into `[]APIMetaItem` via a tiny typed decoder. Attach to every returned
`PlatformError` (every branch). When `Meta == nil` or shape doesn't match,
leave `APIMeta` empty — no panic, no fallback.

**Suggestion rewrite**: when `APIMeta` is non-empty, the generic suggestion
("Check the request parameters") becomes *"The platform flagged specific
fields — see apiMeta for each field's failure reason."* This prevents the
LLM from ignoring the structured block in favor of the stale suggestion.

**TDD (RED first)**:

1. `zerops_errors_test.go` — add table cases:
   - `projectImportInvalidParameter` with Meta: `{host.mode: [mode not supported]}` → PlatformError.APIMeta has one item with that metadata.
   - `projectImportMissingParameter` with Meta: `{parameter: [host.mode]}` → APIMeta preserved.
   - `serviceStackTypeNotFound` with Meta: `{serviceStackTypeVersion: [nodejs@99]}` → APIMeta preserved.
   - `errorList` code with multi-item meta (from zerops-yaml-validation) → multiple APIMeta items.
   - Meta=nil → APIMeta == nil (no spurious empty slice).
   - Malformed Meta shape → APIMeta empty, no error raised.

**File delta**: +40 LOC in errors.go, +30 in mapAPIError, +80 in tests.

### W2 — Per-service `APIError.Meta` surfacing

**Files**: `internal/platform/types.go` (APIError + ImportResult),
`internal/platform/zerops_search.go`, `internal/ops/import.go`,
`internal/ops/import_test.go`.

Import endpoint returns 200 with per-service `stack.Error` for partial
failures. Today ZCP copies only `{Code, Message}`:

```go
// zerops_search.go:64-69 (current)
imported.Error = &APIError{
    Code:    stack.Error.Code.String(),
    Message: stack.Error.Message.String(),
}
```

**Change**:

```go
type APIError struct {
    Code    string         `json:"code"`
    Message string         `json:"message"`
    Meta    []APIMetaItem  `json:"meta,omitempty"` // NEW
}
```

`ops.ServiceImportError` mirrors the Meta field; `ImportResult.ServiceErrors`
then propagates to the MCP response.

**TDD**: `ops/import_test.go` — fixture where API returns 200 with
per-service error carrying meta; assert `ImportResult.ServiceErrors[0].Meta`
matches.

**File delta**: +20 LOC types.go, +10 zerops_search.go, +30 import.go, +40 tests.

### W3 — `convertError` surfaces `apiMeta` + tool JSON contract

**Files**: `internal/tools/convert.go`, `internal/tools/convert_test.go`.

```go
result := map[string]any{"code": pe.Code, "error": pe.Message}
if pe.Suggestion != "" { result["suggestion"] = pe.Suggestion }
if pe.APICode != ""    { result["apiCode"] = pe.APICode }
if pe.Diagnostic != "" { result["diagnostic"] = pe.Diagnostic }
if len(pe.APIMeta) > 0 { result["apiMeta"] = pe.APIMeta }
```

`jsonResult` path (success) for `zerops_import` and any tool that bundles
per-service errors: include Meta in `serviceErrors[].meta`.

**TDD**: `convert_test.go` — table case with APIMeta populated asserts the
JSON output contains `apiMeta`; without APIMeta the field is absent.

**File delta**: +5 LOC convert.go, +60 LOC tests.

### W4 — `Platform.Client.ValidateZeropsYaml` method

**Files**: `internal/platform/client.go`, new `internal/platform/zerops_validate.go`,
`internal/platform/mock_methods.go`, `internal/platform/mock.go`, and a new
`internal/platform/zerops_validate_test.go`.

**Interface**:

```go
type ValidateZeropsYamlInput struct {
    ServiceStackID          string // resolved from hostname by ops layer
    ServiceStackTypeID      string // from platform.ServiceStack
    ServiceStackTypeVersion string // e.g. "nodejs@22"
    ServiceStackName        string // matches setup: name in zerops.yaml
    ZeropsYaml              string
    Operation               string // "DEPLOY" | "BUILD_AND_DEPLOY"
    ZeropsYamlSetup         string // optional
}

func (z *ZeropsClient) ValidateZeropsYaml(
    ctx context.Context,
    in ValidateZeropsYamlInput,
) error
```

- **200 success** → returns nil; caller proceeds.
- **Validation failure** (400 with `zeropsYamlInvalidParameter` /
  `yamlValidationInvalidYaml` / `errorList` — see §1.3 fixtures) → returns
  `*PlatformError` with `Code=ErrInvalidZeropsYml`, `APIMeta` carrying the
  server's field-level detail, `Suggestion` pointing the LLM at `apiMeta`.
- **Transport / auth / timeout** → returns the `mapSDKError`-wrapped
  platform error (network error, auth error, etc.).

**No `(Result, error)` split.** No fallbacks (CLAUDE.md). Validator failure
and transport failure both abort deploy. The LLM distinguishes them by the
error `code` field (`INVALID_ZEROPS_YML` vs `NETWORK_ERROR` vs
`AUTH_TOKEN_EXPIRED`). If Zerops API is unreachable, every subsequent deploy
step would fail anyway — failing fast at the validator is just less wasted
wall time.

**TDD**: unit test with mocked SDK — one case per error code shape observed
in §1.3; plus transport-failure case (returns `NETWORK_ERROR`-coded platform
error); plus success case.

**File delta**: +110 LOC new platform code, +80 tests.

### W5 — Deploy flows call `ValidateZeropsYaml` pre-push

**Files**: `internal/ops/deploy_ssh.go`, `internal/ops/deploy_local.go`,
`internal/ops/deploy_git_push.go`, `internal/ops/deploy_common.go` (shared
helper), test files for each.

**Where**: after `ValidateZeropsYml` client-side (semantic checks) and before
the actual push / build trigger. One shared helper `runPreDeployValidation`
takes the zerops.yaml content, the target service's live `ServiceStackID` +
`ServiceStackTypeID` + `ServiceStackTypeVersion`, and the intended operation.

**Failure path**: any non-nil error from `ValidateZeropsYaml` returns to
the caller as-is. For validation failures the error already carries
`APIMeta`; for transport failures the error code tells the LLM what
happened (e.g. `NETWORK_ERROR`). Deploy aborts before any file transfer.

**Operation selection**: **always `BUILD_AND_DEPLOY`**.

Post-unification (release B — commits `b76aa49` strategy centralization +
`e58badf` symmetric schema), `zerops_deploy` has two dispatch strategies:
(1) default / `push-dev` — SSH self-push, (2) `git-push` — git remote push.
`manual` is explicitly rejected at `deploy_strategy_gate.go:19-36` — it's a
ServiceMeta "don't deploy me" declaration, not a tool option. So validator
never runs for manual services.

Empirically (§1.3), `BUILD_AND_DEPLOY` validates `build.*` + `run.*` and
tolerates missing `build` blocks with HTTP 200. Using it universally:
(a) catches the superset of pre-deploy errors; (b) eliminates a per-branch
decision in the pre-flight helper; (c) works for push-dev services with
`zsc noop` build blocks because the block validates or is silently missing
either way.

**TDD**: per-deploy test pair —
1. validator 200 → deploy proceeds.
2. validator 400 (unknown build.base) → deploy aborts; error carries `APIMeta`.
3. validator transport error → deploy aborts; error code is `NETWORK_ERROR`.

**File delta**: +80 LOC shared helper, +30 in each of 3 deploy flows, +180 tests.

### W6 — Delete duplicative client-side import validation

**Scope narrowing (per user direction 2026-04-23)**: this workstream touches
the **import YAML** validation path only. Recipe authoring checks
(`schema.ValidateZeropsYmlRaw`, `checks/yml_schema.go`,
`workflow_checks_recipe.go` integration) are **out of scope** — leave
untouched. Recipe flow is a separately-owned subsystem; changes there
require its maintainer's sign-off.

**F6.1**: Delete `internal/knowledge/versions.go ValidateServiceTypes`
(entire function and its helpers `makeStringSet` if unused),
`ValidateProjectFields`, and all their tests. Only caller is
`ops/import.go:117` — verified by grep across internal/, no recipe-flow
dependency.

**F6.2**: In `internal/ops/import.go`:
- Remove the `knowledge.ValidateServiceTypes` call at line 117.
- Remove the `liveTypes []platform.ServiceStackType` and `schemas
  *schema.Schemas` parameters from `ops.Import` signature.
- Retain the `envVariables` service-level silent-drop warning loop
  (K1 — API accepts, silently drops).
- Retain the `waitForDeletingServices` call (K10 — orthogonal).
- Remove the `ops/import.go:132-137 platform.ValidateHostname` call
  (K11 — see also W7.1).

**F6.3**: In `internal/tools/import.go`:
- Drop the `liveTypes := cache.Get(...)` and `schemas := schemaCache.Get(...)`
  calls used only to feed `ops.Import`.
- Drop `cache *ops.StackTypeCache` and `schemaCache *schema.Cache`
  parameters from `RegisterImport` IF no other tool-layer consumer needs
  them. Verify via grep — keep if other tools still fetch via those caches.

**F6.4**: Schema-cache and stack-type-cache — **no deletion**. Both have
multiple out-of-scope consumers (bootstrap/workflow/recipe/knowledge for
`StackTypeCache`; recipe flow for `schema.Cache`). F6.3 only trims the
parameters off `RegisterImport`; cache constructors in `server.go`
stay. See §11 item 6.

**TDD**: `knowledge/versions_test.go` — all `TestValidateServiceTypes_*`
and `TestValidateProjectFields_*` cases are deleted alongside the code
they exercised. `ops/import_test.go` — update callers to drop
`liveTypes`/`schemas` arguments; add coverage for the retained paths
(envVariables warning, waitForDeletingServices).

**File delta**: −400 LOC knowledge/versions.go, −300 LOC tests, minor
edits in import.go + tools/import.go. Net loss: ~700 LOC of duplicative
validation.

### W7 — Atom + recipe guidance updates

**Files**: atoms under `internal/content/atoms/` and the recipe workflow
content under `internal/content/workflows/`.

**W7.1 — Hostname format rule surfaced to LLM** (enables K11 deletion):

Add the platform hostname constraint to `bootstrap-provision-rules.md`
(section "Managed service hostname conventions" already exists — extend it).
Wording matching `^[a-z][a-z0-9]{0,39}$`:

> **Format constraint**: 1-40 characters, lowercase letters and digits
> only (`a-z`, `0-9`), first char must be a letter. No dashes, no
> underscores, no uppercase. Examples: `appdev` ✓, `app42` ✓, `42db` ✗,
> `my-cache` ✗, `My_App` ✗.

Add the same rule to `internal/content/workflows/recipe.md` §"Hostname"
(line 34 currently says "lowercase alphanumeric only" — make it precise).

**W7.2 — Atom audit: remove stale validator references**:

```bash
grep -rln "Invalid parameter\|mode required\|objectStoragePolicy" \
    internal/content/atoms/ internal/content/workflows/
```

Any atom claiming "tool will warn you about invalid mode" (etc.) gets the
claim removed — the tool now defers to API, so the atom promise is stale.

**W7.3 — New atom `develop-api-error-meta.md`** — priority 2, phases
[bootstrap-active, develop-active], explaining the `apiMeta` JSON field
on MCP error responses:

> When any `zerops_*` tool returns an error with an `apiMeta` field, that
> array carries field-level detail from the Zerops API. Read
> `apiMeta[].metadata` keys — each key is a field path (e.g.
> `{service}.mode`), each value is a list of specific reasons (e.g.
> `["mode not supported"]`, `["Allowed values: [HA, NON_HA]"]`). Fix
> those fields in your YAML and retry; don't trial-and-error.

**File delta**: ~4 atom edits + 1 new atom (~40 LOC corpus).

### W8 — Invariants + contract tests

**TA-06 (new)**: every error returned by a `platform.Client` method for a
non-2xx API response has `APIMeta` populated when the server sent `meta` in
the response body. Enforced by a contract test against recorded fixtures
(`internal/platform/apitest/`).

**TA-07 (new)**: `convertError` emits `apiMeta` iff `PlatformError.APIMeta`
is non-empty. Enforced by an AST/reflection test or a table test covering
all PlatformError branches.

**Contract test**: `internal/platform/errors_contract_test.go` — iterates
over fixtures with known `meta` shapes, asserts `mapAPIError` preserves
each `metadata` key/value round-trip.

**File delta**: +120 LOC.

---

## 5. Non-goals / validations that stay

ZCP keeps these client-side checks because the API cannot know the answer:

| Check | Lives in | Why it stays |
|---|---|---|
| **K1** `envVariables` silent-drop warning | `ops/import.go` | API accepts then drops — no error, no meta. Only ZCP can warn. |
| **K2** Cross-service env ref resolution (`${host_VAR}`) against live services | `ops/env_generate.go` | API has no concept of "this ref won't resolve" at the import layer. |
| **K3** Unknown-field warning in zerops.yaml | **Recipe flow only — out of scope for this plan** | Kept as-is in `workflow_checks_recipe.go` path; not added to deploy pre-flight. Deploy trusts the API validator; if the server silently accepts unknown fields, that's the server's contract. When the server tightens, the recipe-flow owner decides whether to retire the recipe check. |
| **K4** Deploy role semantic (dev vs stage) | `ops/deploy_validate.go` | ZCP-specific concept; API doesn't know about roles. |
| **K5** `deployFiles` filesystem paths | `ops/deploy_validate.go` | Filesystem state, API cannot know. |
| **K6** `prepareCommands` sudo check | `ops/deploy_validate.go` | Semantic gotcha; API accepts, container fails at runtime. |
| **K7** `run.start` empty / `run.ports` empty | `ops/deploy_validate.go` | Semantic; API accepts. |
| **K8** Encoding-prefixed secret rejection | `ops/env.go` | Semantic gotcha — API stores the broken literal. |
| **K9** Preprocessor expansion errors | `ops/env.go` | Pre-API client-side transform. |
| **K10** DELETING-service wait before import | `ops/import.go` | Race prevention, orthogonal to validation. |
| **K11** Hostname format — import YAML only | **Delete from `ops/import.go:134`**; move the rule into an atom so LLM sees it before generating YAML | API catches with `serviceStackNameInvalid`. The rule (`^[a-z][a-z0-9]{0,39}$`) currently lives only in `platform.ValidateHostname` — LLM never sees it. Move to `bootstrap-provision-rules.md` + any atom that fires when LLM composes import YAML. Retain `platform.ValidateHostname` function for **mount** (`ops/mount.go`, `platform/mounter.go`) and **develop scope** (`workflow/validate.go`) — those are CLI input sanity checks, not API duplicates. |
| **K12** `project:` key rejection in import YAML | `ops/import.go` | Specific ZCP error `IMPORT_HAS_PROJECT` beats generic API error. |

Rule K11 is **deleted from the import path only** — the hostname format
rule moves into atom content so the LLM knows the constraint before
generating YAML. The `platform.ValidateHostname` function stays for
non-API surfaces (mount, develop scope).

---

## 6. TDD layers (per CLAUDE.md "tests FIRST at ALL affected layers")

| Change | Unit | Tool | Integration | E2E |
|---|---|---|---|---|
| W1 mapAPIError + APIMeta | ✓ `zerops_errors_test.go` | — | — | via W4/W5 e2e |
| W2 per-service APIError.Meta | ✓ `zerops_search_test.go` | — | ✓ `import_test.go` | ✓ `TestE2E_Import_MetaReachesLLM` |
| W3 convertError apiMeta | — | ✓ `convert_test.go` + each tool test | — | — |
| W4 ValidateZeropsYaml method | ✓ `zerops_validate_test.go` | — | — | ✓ `TestE2E_ValidateZeropsYaml` |
| W5 pre-deploy validator call | ✓ `deploy_*_test.go` | ✓ tool tests | ✓ integration | ✓ `TestE2E_Deploy_ValidatorAbortsOnBadBase` |
| W6 deletions | — existing tests deleted | — | updated integration | existing e2e baseline still green |
| W7 atom edits | ✓ `atoms_test.go` | — | — | — |
| W8 contracts | ✓ `errors_contract_test.go` | — | — | — |

E2E tests are the authoritative verification — they hit the real API and
prove the claims in §1. Add `e2e/api_error_meta_test.go` (new) that
reproduces every row of the §1.2 table and asserts MCP response contains
the expected `apiMeta.metadata` content.

---

## 7. Execution order

```
        ┌── W1 (PlatformError.APIMeta) ──┐
        │                                │
W2 (per-service) ──────────────────┐     │
        │                          │     │
        └──→ W3 (convertError) ────┴─────┴──→ W6 (deletions)
                                             │
        W4 (ValidateZeropsYaml method) ──────┤
                                             │
        W5 (deploy calls) ───────────────────┤
                                             │
        W7 (atoms) ──────────────────────────┤
                                             │
        W8 (contracts) ──────────────────────┘
                       │
                       ↓
                       release
```

Merge order:

1. **W1 + W2** together — both needed before W3 can produce coherent JSON.
2. **W3** — tool-layer surfacing.
3. **W4** — new client method (no callers yet, purely additive).
4. **W5** — deploy flow wires in the validator. Gate behind successful
   `e2e/deploy_validator_test.go` run.
5. **W6** — deletions. Must run after W1-W3 so the LLM-facing error surface
   already carries meta before we remove the preflight warnings.
6. **W7, W8** — finalize atoms + invariants. Atoms could ship slightly
   later without regressing behavior.
7. **Release** — single cut including all of W1-W8.

Each step's commits must leave `main` green (no RED across merges — same
discipline as friction-root-causes v2 A2/A3 lesson).

---

## 8. Rollback strategy

Per-workstream non-squash merges. Each rollback removes a behavioral
surface:

| Workstream | Rollback impact |
|---|---|
| W1 | `PlatformError.APIMeta` unused; LLM sees old stripped errors |
| W2 | per-service errors drop to `{code, message}` again |
| W3 | JSON response drops `apiMeta` key; field-level detail invisible |
| W4 | `ValidateZeropsYaml` method unused; no regression |
| W5 | deploy calls skip validator; build-time failures return (what we have today) |
| W6 | deleted validators… cannot be trivially restored — these are the *point* of the plan. Not recommended to revert this workstream alone. If needed, revert the whole plan series from the tag. |
| W7 | atoms restored to pre-plan wording |
| W8 | invariants unenforced; drift becomes possible but not actual |

Revert of W6 alone is a design decision: do we want the warning back? Only
if API silently-accept behavior changes or if field-meta stripping returns.

---

## 9. Verification — "done"

1. `go test ./... -count=1` green.
2. `go test ./e2e/ -tags e2e -count=1` green — including new `api_error_meta_test.go`
   and `deploy_validator_test.go`.
3. `go vet ./...` + `make lint-local` clean.
4. Eval `internal/eval/scenarios/greenfield-nodejs-todo.md` run shows LLM
   receives `apiMeta` on any injected-typo test. Captured transcript stored
   in `internal/eval/local-scenarios/`.
5. F#7 friction observed in baseline evals no longer appears — the agent
   self-corrects within one turn after seeing the `apiMeta` block.
6. `wc -l` of `internal/knowledge/versions.go` decreases by ~200; of
   `internal/schema/validate.go` by ~100. Net repo LOC drops.
7. `grep -r "ValidateServiceTypes\|ValidateZeropsYmlRaw\|CheckZeropsYmlFields"
   internal/` returns zero matches (references + definitions gone together).

---

## 10. Evidence index

### Code anchors (verified 2026-04-23)

- `internal/platform/errors.go:62-68` — `PlatformError` struct (current shape).
- `internal/platform/zerops_errors.go:54-94` — `mapAPIError` ignores `GetMeta()`.
- `internal/platform/zerops_search.go:64-69` — per-service `APIError` lacks Meta.
- `internal/platform/types.go:146-149` — `APIError` struct definition.
- `internal/tools/convert.go:38-67` — `convertError` JSON shape.
- `internal/knowledge/versions.go:149-255` — `ValidateServiceTypes` (target for deletion).
- `internal/knowledge/versions.go:259-` — `ValidateProjectFields`.
- `internal/schema/validate.go:36-52, 57-` — `ExtractValidFields`, `ValidateZeropsYmlRaw`.
- `internal/ops/checks/yml_schema.go` — whole file targeted for deletion.
- `internal/ops/import.go:107-129` — current preflight wiring.
- `internal/ops/deploy_validate.go:22-121` — `ValidateZeropsYml` (semantic; stays).
- `internal/ops/deploy_ssh.go:126` — current deploy caller.
- `internal/ops/deploy_local.go:110` — current deploy caller.
- `internal/platform/hostname.go:8` — `^[a-z][a-z0-9]{0,39}$` regex, 40-char limit (E2E-verified 2026-04-02).
- `internal/platform/mounter.go:75,146,169,209,220` + `internal/ops/mount.go:68,153,273` + `internal/workflow/validate.go:131` — retained `ValidateHostname` callers (non-API, CLI-input sanity).
- `internal/content/atoms/bootstrap-provision-rules.md` — target for W7.1 hostname format rule.
- `internal/content/workflows/recipe.md:34` — existing "lowercase alphanumeric only" line; make precise.
- SDK `PostServiceStackZeropsYamlValidation.go` — endpoint wrapper (v1.0.17/18).
- SDK DTO `input/body/zeropsYamlValidation.go` — request shape.
- SDK `apiError/base.go:8-13` — `Error.Meta interface{}` already on SDK.
- SDK upgrade v1.0.17 → v1.0.18 applied 2026-04-23 (see §11 item 1).

### Live API

- `POST /api/rest/public/project/{pid}/service-stack/import` — `ImportServiceStack`.
- `POST /api/rest/public/client/{cid}/project/import` — `ImportProject`.
- `POST /api/rest/public/service-stack/zerops-yaml-validation` — `ValidateZeropsYaml`.

### OpenAPI spec

- `https://api.app-prg1.zerops.io/api/rest/public/swagger/openapi.yml` — 710 KB,
  17 371 lines, 203 operations. Cached at `/tmp/zerops-openapi.yml` for this plan.

### Measured baselines

- §1.2 response shapes for import errors — reproducible via
  `cmd/probe-import-meta/`.
- §1.3 response shapes for `ValidateZeropsYaml` — reproducible via
  `/tmp/zyv-probe.sh` (sample script; promote to e2e in W4).

---

## 11. Open questions

1. **SDK upgrade** — v1.0.18 **applied** (2026-04-23). Safe upgrade: no ZCP
   consumer touches the only breaking surfaces (`Location` type change,
   `invoiceStatusEnum` removal, 3 deleted download endpoints). Build +
   full short-test suite green.

2. **`platform.Client.ValidateZeropsYaml` signature** — confirmed classic
   `error`. Any non-nil result aborts deploy; no fallbacks (CLAUDE.md).
   Rationale: if the API is unreachable, every subsequent deploy step
   would fail anyway; there's no useful "proceed on transport failure"
   behavior for a validator whose purpose is early-exit UX. See §4 W4.

3. **ServiceStackID resolution in W5** — confirmed: W5 is pre-*deploy*,
   stack exists. No dry-run-before-import use case in deploy paths.

4. **Operation enum** — **resolved: always `BUILD_AND_DEPLOY`**. Release B
   (commits `b76aa49` + `e58badf`) consolidated the deploy tool surface:
   `zerops_deploy` dispatches on two strategies (default push-dev, git-push);
   `manual` is rejected at the gate, never reaches validator. Empirically
   (§1.3) `BUILD_AND_DEPLOY` validates build + run supersets and tolerates
   missing build blocks (200). Universal operation = simpler pre-flight
   helper, no per-strategy branching. Updated §4 W5.

5. **K11 hostname check** — **resolved: delete from import path**. Move
   rule to atom corpus (see W7.1). Keep `platform.ValidateHostname`
   function for mount + develop-scope CLI input sanity checks.

6. **`ops.StackTypeCache` and `schema.Cache` cleanup** — **resolved: no
   cleanup needed**. Both caches have multiple consumers outside the
   import path and outside this plan's scope:
   - `StackTypeCache` is used by `tools/workflow.go`,
     `tools/workflow_bootstrap.go` (5 call sites), `tools/workflow_recipe.go`,
     and `tools/knowledge.go` — all survive W6.
   - `schema.Cache` is used by `tools/workflow_checks_recipe.go` (recipe
     flow, out of scope) and `tools/workflow_recipe.go`.

   W6's only cache-related change: `RegisterImport` (`tools/import.go:46`)
   drops the `cache` and `schemaCache` parameters it no longer uses; the
   `server.go` call site is trimmed to match. Cache constructors in
   `server.go:103-104` stay. No tracking ticket needed (the earlier TR-02
   suggestion was based on an incorrect assumption that recipe flow was
   also being stripped).

7. **Recipe workflow integration** — **resolved: recipe flow is
   out-of-scope for this plan**. `schema.ValidateZeropsYmlRaw`,
   `schema.ExtractValidFields`, `internal/ops/checks/yml_schema.go`, and
   all integration through `workflow_checks_recipe.go` stay untouched.
   This plan only removes import-YAML client-side duplicates
   (`knowledge.ValidateServiceTypes`) — a narrower scope than v1.

---

## 12. What this plan supersedes

- `plans/friction-root-causes.md §2.3 P2.3` ("Import pre-validation with
  empirical-evidence discipline") — **deleted from the friction plan**.
  The friction point F#7 is resolved by W1-W3 of this plan, not by
  pre-validation.
- `plans/friction-root-causes.md §4 A4` agent mission — **deleted**.
- `plans/friction-root-causes.md §0 Non-Negotiable #7` ("Every
  `importPreValidationRule` carries empirical evidence") — **deleted**.
- `plans/friction-root-causes.md §8 Rollback P2.3 row` — **deleted**.

`plans/friction-root-causes.md` continues as the home for the other six
friction points (F#2, F#3, F#6, F#8, F#9, F#10). A follow-up commit to
that file removes the P2.3 section and adds a pointer to this plan.
