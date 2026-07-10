# Implementation spec: delegated launch-token minting

**For implementation agents.** This document is self-contained — do not rely on
conversation context. Companion analysis: `plans/token-delegation-launch-2026-07-03.md`
(background only; THIS spec wins on any conflict). Base branch: `feat/token-delegation-launch`
(cut from main @ d3715457).

## 0. What & why (one paragraph)

The Zerops platform shipped **integration token delegations** (prod since
2026-07-10): a one-time authorization attached to a ZCP integration token that
lets it mint ONE new integration token with a pre-defined scope
(`roleCode=NO_ACCESS, canCreateProjects=true`), owned by the delegating user.
Every new ZCP token gets one automatically; existing ZCP tokens were backfilled.
This replaces the manual step in launch-production where the agent walks the
user through creating a launch token in the dashboard and waits for a paste.
**The delegation replaces the ASK, not the lifecycle**: everything downstream of
token acquisition (staging as `ZCP_LAUNCH_TOKEN`, stage-first window reads,
secret-to-secret `ZEROPS_TOKEN_PROD` conveyance, physical window close) is
value-agnostic and MUST stay byte-for-byte unchanged.

## 1. Live-verified platform facts (alpha 2026-07-03, prod 2026-07-10)

Mocks and tests MUST encode exactly these behaviors — never assumed ones.

- **F1 — identity**: integration tokens are user-shaped. `GET /api/rest/public/user/info`
  with an integration token returns the token-as-user; its `id` **IS** the
  `{tokenId}` used in delegation paths. Verified on alpha and prod.
- **F2 — delegation list**: `GET /api/rest/public/client/{clientId}/integration-token/{tokenId}/delegation`
  works WITH the integration token itself (200). Live prod sample (eval-zcp token
  `zcp-eval-new`):
  ```json
  {"list":[{"id":"srdstsF6QM6J72yUMhDRJA","clientId":"BkC8AGjFQMyFrLbzjHoE9g",
    "clientUserId":"WeZQLcWnSJiQexQoZuwa5g","userId":"SbbWs0jmQyeElIA0T9qUxw",
    "tokenId":"3U4vJrDsRvKrAIwBWAw32A",
    "tokenPermissions":{"roleCode":"NO_ACCESS","canCreateProjects":true,
      "canEditFinances":false,"canViewFinances":false,"projectPermissions":[]},
    "created":"2026-07-10T08:54:17Z","lastUpdate":"2026-07-10T08:54:17Z"}]}
  ```
  A client-wide variant exists too (`GET .../integration-token/delegation`, no
  tokenId) — we use the per-token one.
- **F3 — the mint**: `POST /api/rest/public/client/{clientId}/integration-token`
  with body `{"name":"...","projects":[],"roleCode":"NO_ACCESS","canCreateProjects":true}`
  under the ZCP integration token → 200 with `ResponseClientIntegrationTokenRaw`
  incl. the raw `token` value (shown exactly once) and `id` (the new token's id).
  The zerops-go SDK already has this call: `handler.PostClientIntegrationToken`
  (output DTO `ClientIntegrationTokenRaw` incl. `Token`).
- **F4 — one-time**: a successful mint CONSUMES the delegation — it is **deleted**
  from the list (count drops to 0). A further mint returns
  `403 {"error":{"code":"notAllowedForIntegrationTokenWithoutDelegation",
  "message":"This action is not allowed for integration tokens without explicit
  delegation. Please create a one-time delegation, use a personal token or log
  in with your user account."}}`. (Pre-delegation platforms returned
  `notAllowedForIntegrationToken` for the same call — treat both as the same
  ZCP semantic.)
- **F5 — no over-reach**: requesting more than delegated (e.g. `roleCode=ADMIN`)
  → `403 roleLevelExceeded`, and the REJECTION DOES NOT consume the delegation.
  ZCP never requests anything but the delegated shape, so this code needs no
  dedicated mapping (generic 403 mapping is fine).
- **F6 — minted token capabilities**: authenticates via `/user/info` (own
  identity, `fullName` = the mint-request `name`); client-level role stays
  `NO_ACCESS` with `canCreateProjects=true`; `POST /client/{id}/project/import`
  → 200 (this is the endpoint SDK `PostClientProjectImport` uses — the exact
  call behind `ProjectAdminClient.CreateAndImportProject`); has creator access
  to projects it creates (GET/DELETE verified live); 403 on other projects;
  carries NO delegation itself (no chaining); CANNOT create delegations,
  tokens, or delete tokens.
- **F7 — plain create quirk**: `POST /client/{id}/project` (non-import
  CreateProject) fails for minted tokens with `400 userNotFound` — intended
  platform restriction (import-path-only). ZCP uses the import path everywhere;
  do not touch this.
- **F8 — GrantSelfRole**: fails for ALL integration tokens ("Insufficient
  permissions") and is already non-fatal by design in the launch flow. The
  minted token behaves the same. No change needed.

## 2. Design invariants (D-1 … D-9)

- **D-1 — platform is the sole source of delegation truth.** Availability is a
  fresh `ListOwnTokenDelegations` read at decision time. NEVER gate on a locally
  persisted "delegation consumed" flag. (An observation stamp in launch state for
  honest status display is allowed, but nothing may branch on it.)
- **D-2 — the minted token is a credential under the existing P-LP-1/P-LP-14
  discipline**: lives in handler memory for the current request only; staged as
  the `ZCP_LAUNCH_TOKEN` service secret; NEVER serialized into response, state
  file, or audit log. Sentinel-scan tests extend the existing pattern.
- **D-3 — mint LATE, exactly once, after every refusal gate.** Inside
  `executeLaunchMutation` the delegated-path block (list → mint → admin-client
  construction from the minted value) sits immediately BEFORE `stageLaunchToken`
  — i.e. AFTER `readAndValidateSourceState`, `runPublishSideSourceControlGate`,
  `composeLaunchBundleInputs`, `ops.BuildLaunchBundle`, and schema validation
  have all passed. Rationale: a successful mint burns the one-time delegation
  even if the flow fails later; mundane refusals (missing zerops.yaml, source
  drift, schema errors) must never cost the user their delegation. This creates
  a DELIBERATE asymmetry: the explicit-launchKey path keeps constructing the
  admin client at the function head (unchanged — D-5), the delegated path
  constructs it only after the mint. Safe because `admin` is provably unused
  between its head construction and `CreateAndImportProject` (only
  `defer admin.Close()` sits in between).
- **D-4 — explicit user consent replaces key-presence.** Today
  `publishing := input.LaunchKey != ""` encodes "the user consented by supplying
  the token". The delegated path needs an explicit `confirmLaunch=true` input
  the agent sets ONLY after the user's explicit go-ahead.
- **D-5 — explicit launchKey takes precedence.** If `launchKey` is provided, the
  delegated path is not consulted at all (zero delegation API calls). This keeps
  every existing flow, test, and eval scenario (which inject launchKey)
  behaviorally unchanged.
- **D-6 — fallback is today's flow.** No delegation / consumed / revoked /
  list-read error / typed-unavailable mint error → the response renders the
  existing manual dashboard WALKTHROUGH TEXT verbatim and asks for `launchKey`
  (the response ENVELOPE is augmented — blocker + `delegatedLaunch` block —
  per the §4.4 pinned contract; "verbatim" scopes to the walkthrough text
  only). Fail toward the manual path, never block the launch on
  delegation-machinery errors. Exception: an INDETERMINATE mint error is NOT
  the manual fallback — it gets the distinct `delegation-mint-indeterminate`
  recovery (§4.4 outcome table), because the delegation may already be burned.
- **D-7 — honest mid-flight failure.** If the mint succeeds but a later
  pre-create step fails (e.g. staging), the response must state: the one-time
  delegation was consumed; a token named `<name>` now exists in the dashboard;
  ZCP no longer holds its value; recovery = regenerate that token in the
  dashboard and re-call with `launchKey`.
- **D-8 — the agent still never fabricates a token.** Delegated minting is the
  PLATFORM's act authorized by the user's delegation. All "NEVER generate/guess
  a token" contracts stay; wording is adjusted only where it claims one-shot
  tokens don't exist (Phase 3).
- **D-9 — existing-project path untouched.** `ExistingProdToken` (project-scoped
  token for launching into an existing prod project) has nothing to do with
  delegations. Mutual-exclusion checks extend to `confirmLaunch` the same way
  they cover `launchKey`.

## 3. Phase 1 — L1 platform client

New file `internal/platform/zerops_delegation.go` (+ `zerops_delegation_test.go`).

### 3.1 DTOs (colocate in the new file)

```go
// TokenDelegation is a one-time authorization for THIS integration token to
// mint a new integration token with the given permissions. Live shape: F2.
type TokenDelegation struct {
    ID                string
    TokenID           string
    RoleCode          string
    CanCreateProjects bool
    Created           string // RFC3339, informational only
}

// MintedToken is the result of consuming a delegation. Token is a live
// credential — P-LP-1 applies: never serialize into responses, state, or logs.
type MintedToken struct {
    Token   string `json:"-"` // marshal-proof by construction; never format/log whole
    TokenID string
}
```
(Flatten `tokenPermissions` into the DTO — ZCP only branches on
`CanCreateProjects`; keep `RoleCode` for the honest status line. Do NOT carry
finance flags / projectPermissions — nothing consumes them. NO `Name` field on
MintedToken: recovery text uses the locally-retained REQUESTED name (§4.4),
never the returned DTO. Add a marshal test proving both a sentinel Token value
and its stable prefix are absent from `json.Marshal(MintedToken{...})`.)

### 3.2 Methods (on `*ZeropsClient`, added to the `Client` interface)

```go
// ListOwnTokenDelegations returns the delegations attached to the token this
// client authenticates with. Fresh read every call — the platform is the sole
// source of delegation truth (D-1).
ListOwnTokenDelegations(ctx context.Context) ([]TokenDelegation, error)

// MintDelegatedLaunchToken consumes the one-time delegation to mint a
// NO_ACCESS + canCreateProjects integration token named name. The returned
// Token value is shown by the platform exactly once (F3) — the caller owns
// the P-LP-14 staging discipline.
MintDelegatedLaunchToken(ctx context.Context, name string) (MintedToken, error)
```

Implementation notes:
- **clientId**: reuse the existing lazy cache `z.getClientID(ctx)`
  (`internal/platform/zerops.go`).
- **own tokenId**: F1 — it's `/user/info`'s `id`. Today `GetUserInfo`
  (`zerops.go`, ~lines 100-127) DISCARDS `out.Id`. Add `UserID string` to
  `platform.UserInfo` (`types.go` — note the existing naming trap: `UserInfo.ID`
  is the CLIENT id; document the distinction in the doc-comment), populate it,
  and add a lazy `getTokenID(ctx)` sibling of `getClientID` (same
  mutex/retry-on-error pattern; never hold the mutex across I/O — CLAUDE.md).
- **List is NOT in the zerops-go SDK** (verified absent in v1.0.20 and v1.0.21).
  Hand-roll the GET on the SDK's own transport so host normalization + bearer
  auth stay single-owner: keep an authorized `sdkBase.Environment` on
  `ZeropsClient` (build it in `NewZeropsClient` alongside the handler), then use
  the exported `sdkBase.Get(ctx, env, path)` helper and mirror a generated SDK
  method's decode (success DTO vs `struct{ Error apiError.Error }` on ≥300 —
  read e.g. `sdk/GetClientIntegrationTokenList.go` in the module cache at
  `$(go env GOMODCACHE)/github.com/zeropsio/zerops-go@v1.0.20/` and copy its
  shape). Feed errors through the existing `mapSDKError`/`mapAPIError` seam
  (`zerops_errors.go`). Do NOT open a parallel raw net/http path.
- **Mint IS in the SDK**: `z.handler.PostClientIntegrationToken(ctx,
  path.ClientId{...}, body.ClientIntegrationToken{...})` returning
  `output.ClientIntegrationTokenRaw` (the pinned v1.0.20 DTO name). Request
  body EXPLICITLY sets every permission: `roleCode: NO_ACCESS`,
  `canViewFinances: false`, `canEditFinances: false`,
  `canCreateProjects: true`, `projects: []` (empty, NOT nil if the SDK
  distinguishes), `name`. Finance denial is a delegated-token invariant, not
  an incidental Go zero value. Return the raw `Token` + `Id`.

### 3.3 Error mapping

- New ZCP code in `internal/platform/errors.go` (follow the existing doc-comment
  template): `ErrDelegationUnavailable` = `"DELEGATION_UNAVAILABLE"` — semantic:
  "this token has no unused delegation; the manual launchKey path is the
  recovery".
- apiCode consts colocated in `zerops_delegation.go`:
  `notAllowedForIntegrationTokenWithoutDelegation` AND the legacy
  `notAllowedForIntegrationToken` — BOTH translate to `ErrDelegationUnavailable`
  in the mint path (F4).
- **Do NOT add branches to `mapAPIError`'s switch** (it branches on HTTP status
  + entityType only; no apiCode branching exists there). Follow the repo's one
  apiCode-translation precedent (`apiCodeNoExternalRepositoryIntegration`,
  checked at its CALL SITE in `zerops_integration.go`/`project_admin.go:553`):
  in `MintDelegatedLaunchToken`, run the generic mapper, then
  `errors.As(mapped, &pe)` and if `pe.APICode` matches either const, rewrite
  ONLY the classification `Code` to `ErrDelegationUnavailable` + set the
  manual-launch `Suggestion`; preserve `Message`, `APICode`, `APIMeta`, and
  `Cause` (those are the actual `PlatformError` fields — there is no generic
  `Meta`).

### 3.4 Mock (`internal/platform/mock.go` + `mock_methods.go`)

Adding to the `Client` interface breaks `var _ Client = (*Mock)(nil)` — that's
the compile-forcing seam. Add:
- state: `tokenDelegations []TokenDelegation`, `mintedToken *MintedToken`,
  plus error hooks via the existing `getError` mechanism.
- builders: `WithTokenDelegations(...TokenDelegation)`,
  `WithMintedToken(MintedToken)`.
- **the mock encodes F4's one-shot semantics**: a successful
  `MintDelegatedLaunchToken` call clears `tokenDelegations` and any further
  mint returns `ErrDelegationUnavailable`; `ListOwnTokenDelegations` after a
  mint returns empty. `trackCall` both methods (tests assert call counts —
  D-5's "zero delegation calls" needs this).
- default (no builder): empty delegation list + mint returns
  `ErrDelegationUnavailable` — i.e. a pre-delegation / consumed platform.

### 3.5 Tests (RED first — run them failing before implementing)

- Unit: DTO mapping table-driven; error mapping (both apiCodes →
  `ErrDelegationUnavailable`); `getTokenID` caching (mirrors the `getClientID`
  test if one exists, else a focused test).
- Mock behavior: one-shot semantics (list → mint → list empty → second mint
  errors); call tracking.
- api tier: unit tests live in `zerops_delegation_test.go`; live API coverage
  in a SEPARATE `zerops_delegation_api_test.go` with file-level
  `//go:build api` (build tags are file-wide; `apitest.New(t)` harness — skips
  without `ZCP_API_KEY`): `TestAPI_ListOwnTokenDelegations_WellFormed` —
  asserts the call succeeds and rows (if any) parse; MUST NOT assert a specific
  count (the eval token's delegation will be legitimately consumed by later
  live verification). Also extend the existing `TestAPI_GetUserInfo`
  (`zerops_api_test.go`) with `UserID != ""` — the delegated-mint contract
  depends on that platform field (F1).
- **HARD RULE: no automated test ever calls the real mint.** One-shot semantics
  + real token creation as a side effect make it non-idempotent and
  non-repeatable; the positive mint is verified once, manually, in the live
  verification phase (§6). The CI-safe negative (mint on a token whose
  delegation is consumed → typed 403) may be added ONLY as a skipped-by-default
  test with an explicit opt-in env var, or not at all.

### 3.6 Acceptance gate (walk each item, report per-item)

1. `go test ./internal/platform/... -short` green, RED evidence reported.
2. `go test ./... -short` green (interface ripple compiled everywhere).
3. `make lint-fast` clean.
4. No response/state surface touched (L1-only change).
5. Spec-fidelity: every §3.1-3.5 item done / deviated-with-reason.

## 4. Phase 2 — workflow integration (L3/L4)

### 4.1 Input surface (`internal/tools/workflow.go`)

Add to `WorkflowInput` (next to `LaunchKey`, ~line 154). **`FlexBool`, NOT raw
`bool`** — `TestInputStructsUseFlexBoolForBooleans`
(`input_flexbool_guard_test.go`) rejects raw bool on every *Input struct:

```go
ConfirmLaunch FlexBool `json:"confirmLaunch,omitempty" jsonschema:"Launch-production publish only: set true ONLY after the user explicitly confirmed launching production via the delegated-token path (ZCP mints the launch token from the user-granted one-time platform delegation; no token value crosses the conversation). If launchKey is also provided, launchKey wins and no delegation is consumed."`
```

All control-flow and echo reads use `input.ConfirmLaunch.Bool()`. Schema
ripple: add `patchFlexBoolProperty(s, "confirmLaunch")` in
`workflowInputSchema` (mirror the existing FlexBool patches ~line 284),
include `confirmLaunch` in `TestWorkflowInputSchema_FlexBoolPublished`
(`workflow_input_schema_test.go`), extend `workflow_schema_test.go` for the
published property + direct-bool + string-"true" unmarshalling, and if the
schema byte-budget test (`schema_byte_budget_test.go`) trips, raise the
ceiling with a one-line size rationale. `annotations_test.go` needs NO change
(tool annotations don't change).

### 4.2 Publish gate + input-conflict tightening (`workflow_launch_production.go`, ~line 436)

`publishing := input.LaunchKey != "" || input.ConfirmLaunch.Bool() || hasExistingPath`

The existing-project path is currently recognized only when BOTH
`existingProjectID` and `existingProdToken` are present — a request with
`confirmLaunch=true` plus only ONE of them would silently fall through to
new-project delegated creation. Define:

```go
hasAnyExistingInput := input.ExistingProjectID != "" || input.ExistingProdToken != ""
hasExistingPair     := input.ExistingProjectID != "" && input.ExistingProdToken != ""
```

Before any delegation call: reject an incomplete existing pair (one field
without the other); reject `(input.LaunchKey != "" || input.ConfirmLaunch.Bool())
&& hasAnyExistingInput` (same error shape as the current launchKey conflict).
Only `hasExistingPair` may enter existing-project mutation. Already-launched /
in-progress resume handling keeps its current earlier precedence.

`confirmLaunch` echoes in the input echo (it is not a secret): add a field to
`launchInputsEcho` (~1195) populated from `input.ConfirmLaunch.Bool()` in
`echoInputs()` (~2008); `launchKey` stays excluded. Tests: each partial-field
case + both complete conflict forms.

### 4.3 Ready-to-launch response — advertise the available path

`launchReadyToLaunchResponse` (~1458) marshals straight into an
`*mcp.CallToolResult` — there is no typed object left to decorate after it
returns, and its signature has 4 existing test call sites
(`launch_ready_consent_test.go`). Restructure without breaking either:
- extract `buildLaunchReadyToLaunchPayload(...) launchProductionResponse`
  (the typed payload construction), and keep
  `launchReadyToLaunchResponse(...)` as a thin compatibility wrapper doing
  `jsonResult(buildLaunchReadyToLaunchPayload(...))` — existing call sites
  untouched.
- add to `launchProductionResponse` (~1114):
  `DelegatedLaunch *delegatedLaunchAvailability \`json:"delegatedLaunch,omitempty"\``
  with `Available bool`.
- in `handleLaunchProduction` (owns ctx+client), where the ready-to-launch
  response is selected (~484): call `client.ListOwnTokenDelegations(ctx)`;
  availability := `err == nil && any(d.CanCreateProjects)`; build the typed
  payload, set the `DelegatedLaunch` block + the guidance line, marshal
  exactly once. Never decode-and-rewrite TextContent.
- **available** → decoration adds: structured block
  `"delegatedLaunch": {"available": true}` + a guidance line making the
  delegated path primary ("a one-time delegation from the token owner is
  available; on your explicit confirmation ZCP mints the launch token itself —
  no token value will cross the conversation; re-call with confirmLaunch=true"),
  manual launchKey path mentioned as the alternative.
- **unavailable OR list error** → `"delegatedLaunch": {"available": false}` and
  otherwise exactly today's response (manual walkthrough atom; D-6 fail-open).
  A list error must NOT surface as a launch blocker — stderr log only.
- The availability read runs ONLY when the response being built is
  ready-to-launch on the new-project path (not for scope/classify statuses, not
  for the existing-project path).

### 4.4 Mutation seam (`executeLaunchMutation`, `workflow_launch_production.go` ~597)

New-project path only. Control-flow reality of the function today: the
admin-client construction (`projectAdminClientFactory(input.LaunchKey, apiHost)`)
is the FIRST statement (~611), followed by the refusal gates
(`readAndValidateSourceState` ~625, `runPublishSideSourceControlGate` ~639,
`composeLaunchBundleInputs` ~647, `ops.BuildLaunchBundle` ~674, schema
validation ~690), then `stageLaunchToken` (~710), then
`admin.CreateAndImportProject` (~756). `admin` is unused between construction
and the create (only `defer admin.Close()`).

Restructure per D-3/D-5:

```
// head (~611): construct admin ONLY on the explicit-launchKey path — unchanged
var admin platform.ProjectAdminClient        // nil on the delegated path until minted
if input.LaunchKey != "" {
    admin = projectAdminClientFactory(input.LaunchKey, apiHost)  // + existing defer Close / auth-fail handling
}

... ALL existing refusal gates run unchanged ...

// immediately before stageLaunchToken (~710):
launchToken := input.LaunchKey
mintedName := ""                  // the locally-retained REQUESTED name — recovery text
                                  // uses THIS, never the returned DTO
if launchToken == "" {            // delegated path (publishing came via ConfirmLaunch)
    staged := launchKeyFromStage(...)          // delegated-retry: a prior attempt may have
    if staged != "" {                          // staged the token before failing (see §4.5)
        launchToken = staged                   // zero delegation list/mint calls
    } else if stage-read errored {
        return retry-read blocker              // do NOT proceed to mint on an unconfirmed read
    } else {
        list delegations (D-1)                 // none usable -> delegationUnavailableResponse (D-6)
        mintedName = delegatedTokenName(input.ProductionProjectName)
        write launch state (acquisition=delegated, tokenName) — FATAL on failure:
            abort BEFORE the mint; nothing burned yet         // delegated-path-only gate
        minted, err := client.MintDelegatedLaunchToken(ctx, mintedName)
        if err -> see the mint-outcome table below
        launchToken = minted.Token
    }
    admin = projectAdminClientFactory(launchToken, apiHost)   // + defer Close;
                                  // auth-fail here -> consumed-delegation narrative (below),
                                  // NOT the generic launchFailedAuthResponse
}
stageLaunchToken(..., launchToken)   // stage-BEFORE-create preserved
admin.CreateAndImportProject(...)    // unchanged
```

**Mint-outcome table (delegated path; each row has a dedicated test):**

| Outcome | Response |
|---|---|
| Delegation list empty / typed `ErrDelegationUnavailable` from the mint (race) | `delegationUnavailableResponse` (D-6) |
| Any OTHER mint error (timeout, 5xx, transport) | blocker `delegation-mint-indeterminate`: the POST may have committed server-side — token `<mintedName>` MAY exist and the delegation MAY be consumed; direct the user to check the dashboard for that token, regenerate it if present, and re-call with `launchKey`. NEVER auto-retry the POST; never serialize the raw SDK error body. |
| Mint 200 but empty `Token`, admin-factory failure on the minted value, or staging failure | ONE shared consumed-delegation narrative (D-7): the delegation was consumed; token `<mintedName>` exists in the dashboard; ZCP no longer holds its value; regenerate it there and re-call with `launchKey`. Do NOT use the generic auth recovery (it assumes a user-held token) and do NOT use `delegationUnavailableResponse`. Staging-failure wording must say staging was "not confirmed", not "failed" — the write may have committed before the error. |
| Launch-state write failure BEFORE the mint | plain abort (existing state-write error shape) — nothing was burned; safe to re-call. This write is FATAL only on the delegated path; the explicit-launchKey path keeps today's non-fatal behavior (D-5). |

- `delegatedTokenName(prodName)`: final name = `"zcp-launch-" + suffix`,
  TOTAL length ≤ 48 chars (truncate the sanitized suffix to
  `48-len("zcp-launch-")`); sanitize = lowercase, keep `[a-z0-9-]`, collapse
  HYPHEN runs only (never repeated letters); empty suffix → exactly
  `"zcp-launch"`. Purpose: operator recognition in the dashboard + the D-7
  recovery text — it does NOT feed `matchLaunchToken` (which matches on
  access properties, not name). Table tests: long, empty, punctuation-only,
  repeated-letter, repeated-hyphen inputs.

- `delegationUnavailableResponse` — pinned contract: `status:
  "ready-to-launch"` + the current active phase; preserves the manual launch
  walkthrough text VERBATIM plus sanitized inputs, bundle preview, readiness
  checks, and source context when available; sets
  `"delegatedLaunch": {"available": false}`; adds blocker
  `{id: "delegation-unavailable", severity: "block", category: "auth",
  recovery: workflow start launch-production}` (taxonomy:
  `internal/topology/types.go` blocker shapes). Semantics: empty list / typed
  unavailable = absent-or-consumed delegation; a LIST failure = "could not
  check" → manual fallback WITHOUT exposing the underlying error; an
  indeterminate MINT error uses the distinct `delegation-mint-indeterminate`
  blocker from the outcome table, never this response.
- **D-7 honesty — message builder**: `launchTokenStageFailedMessage(stageErr
  error, pushHostname string) string` (`launch_stage.go:106`) has **TWO call
  sites**: new-project (`workflow_launch_production.go:721`) and
  existing-project (`launch_existing.go:315`). Add a third param
  `mintedName string`; delegated new-project passes the retained requested
  name, explicit-key new-project AND existing-project pass `""`; empty name
  preserves the existing message byte-for-byte. When non-empty, append the
  consumed-delegation narrative (outcome-table row 3). Test: existing-project
  staging failure shows NO delegated/consumed-token narrative.
- **Effective-token discipline**: initialize `launchToken := input.LaunchKey`
  once after input validation; delegated acquisition may reassign it. From
  that point the effective raw token appears ONLY as the admin-factory
  argument and the staging argument — never in formatting, logging, response,
  blocker, state, audit, or error constructors. (Reads of `input.LaunchKey`
  for validation/gating are legitimate; the grep-proof rule in §4.7 applies to
  the effective-token local, not to every mention of the input field.)

### 4.5 Window ops / recovery paths — delegated retry + reset close the loop

`resolveLaunchWindowToken` internals, prod-ops re-ask messages, launched-state
resume, and confirm-production close stay UNCHANGED — and there is NO re-mint
logic anywhere (after a launch mint the token has no delegation left by
definition — F4). Two deliberate changes close the state-machine holes a
consumed delegation would otherwise open:

- **Delegated retry resolves the staged token first.** A `failed` state (or
  stale `launching`) with empty `TargetProjectID` is retryable today. On such a
  retry with `confirmLaunch=true` and NO explicit `launchKey`, the mutation
  resolves the already-staged `ZCP_LAUNCH_TOKEN` BEFORE any delegation call
  (§4.4 pseudocode): non-empty staged value → it becomes the effective token,
  zero list/mint calls (the prior attempt already consumed the delegation and
  staged the result); stage READ error → retry-read blocker (do not mint on an
  unconfirmed read); confirmed-absent → delegated acquisition or manual
  fallback. The failed-state guidance directs the caller to retry with
  `confirmLaunch=true`.
- **Reset means abandonment — it deletes the staged secret.** Today
  `launch_reset.go` resolves/deletes staged credentials only when a target
  project exists; a no-target reset deletes state and ORPHANS the staged
  secret. Change: after deleting any target project, delete the staged
  launch-token env BEFORE deleting launch state; if that deletion fails,
  preserve the state and refuse completion (mirror of the confirm-production
  delete-first rule). Report `stagedSecretDeleted` in the reset result.

### 4.6 Tests (RED first; all against `platform.Mock`)

- Publish gate: `confirmLaunch=true` publishes; `confirmLaunch` + existing-path
  inputs refused; neither key nor confirm → read-only statuses as today.
- Precedence (D-5): `launchKey` provided + delegations configured in mock →
  mint/list call counts BOTH zero.
- Delegated happy path: mock with delegation → mutation mints once, stages the
  MINTED value (assert via the staged-env capture the existing stage tests use),
  creates the project, response carries `delegatedLaunch.available=true` at
  ready-to-launch and no token anywhere.
- Fallback: mock without delegation → `confirmLaunch=true` mutation returns the
  `delegation-unavailable` blocker response (no error envelope, no mint).
- List-error fail-open: mock list error → ready-to-launch renders the manual
  path, no blocker, no crash.
- Ordering (D-3): mint is not called when any pre-mint gate refuses (pick one
  existing refusing gate, assert mint call count 0).
- Mint-outcome table rows (§4.4), each its own test: indeterminate mint error →
  `delegation-mint-indeterminate` blocker + zero project creation; empty-token
  mint / admin-factory failure / staging failure → consumed-delegation
  narrative + zero project creation; pre-mint state-write failure → abort with
  zero mint calls.
- Delegated retry + reset (§4.5): delegated create-failure leaves staged
  sentinel + failed state → retry with `confirmLaunch` uses the staged token
  with ZERO list/mint calls; stale-`launching` equivalent; no-target reset
  deletes the staged secret; stage-delete failure preserves state and refuses
  completion.
- **Sentinel leak (P-LP-1/D-2)**: mock mints a unique sentinel; scans check
  BOTH the full sentinel AND a stable token-prefix sentinel across the
  serialized MCP response, every file under the launch-production state dir
  (incl. audit records), and captured stderr — extend the existing
  banned-strings pattern (`workflow_launch_production_mutation_test.go`).
  Run the scan for success + admin-factory failure + staging failure +
  create failure. Also `TestLaunchState_NoLaunchKeyFieldExists`-style: no new
  state field holds a token value (the acquisition/tokenName state fields from
  §4.4 are non-secret: name + mode only).
- D-7: stage-failure with minted token → message names the token + regenerate
  recovery; existing-project stage-failure carries none of it.
- Ripple per CLAUDE.md change-impact: tool tests + FlexBool schema tests
  (§4.1 — NOT `annotations_test.go`; annotations don't change) + `integration/`
  + e2e compile (`make vet-tags` or the repo's equivalent guard). NOTE:
  `integration/` tests live in an EXTERNAL package (`integration_test`) and the
  admin-factory seam (`projectAdminClientFactory`) is private to
  `internal/tools` — the in-process conductor test therefore covers ONLY: the
  published `confirmLaunch` schema, ready-to-launch `delegatedLaunch`
  availability reporting, and the no-delegation manual fallback. The full
  mint → admin → stage → create path is covered in `internal/tools` tests where
  the factory can be injected. Do NOT export a test-only setter. Mirror the
  harness pattern of `integration/bootstrap_conductor_test.go`.

### 4.7 Acceptance gate

1. RED evidence; `go test ./... -short` green; `go test ./internal/tools/... -race` green.
2. `make lint-local` clean.
3. Grep-proof (per the §4.4 effective-token discipline): the effective-token
   local reaches ONLY the admin-factory + staging arguments;
   `MintDelegatedLaunchToken` called from exactly ONE site; no
   formatting/logging/response/state/audit constructor receives it.
4. Spec-fidelity walk of §4.1-4.6.

## 5. Phase 3 — truth sweep (docs, atoms, guidance, evals)

The feature falsifies statements in these exact places. Each item: reword to
the new truth (delegated mint primary, manual mint = fallback), keeping D-8.

1. `docs/spec-workflows.md` §10 intro ("ONE user-minted integration token") and
   P-LP-14 ("no one-shot token type exists") — rewrite; ADD invariant **P-LP-15**:
   delegated mint (fresh availability read D-1; mint late D-3; consent D-4;
   precedence D-5; fallback D-6; consumed-honesty D-7), naming the Phase 2 tests.
2. `internal/tools/launch_confirm.go` — `tokenLifecycle.truth` string + the
   file-head comment: replace the "Zerops has no one-shot token type" claim with
   the delegation-aware truth (token stays valid; the DELEGATION was one-time).
3. Atoms (`internal/content/atoms/`): `launch-intro.md` (one-shot claim, single-
   token lifecycle paragraph), `launch-mutation-key-required.md` (reframe: this
   walkthrough is the FALLBACK when no delegation is available; delegated path
   first), `launch-scope-prompt.md` + `idle-launch-entry.md` ("user-minted …
   passed once" phrasing), `launch-status-recovery.md` (re-supply hint mentions
   fallback only). Respect the atom authoring lint (`TestAtomAuthoringLint`) —
   no handler verbs, no spec IDs. Regenerate goldens:
   `ZCP_UPDATE_ATOM_GOLDENS=1 go test ./internal/workflow/...`.
4. `internal/knowledge/themes/operations.md` "Integration tokens cannot exceed
   creator's permissions" — append the delegation carve-out (one-time,
   user-granted, subset-bounded).
5. Project `CLAUDE.md` trap line "Credentials are user-owned" — extend with:
   launch token may be platform-minted via a user-granted one-time delegation
   (agent-side fabrication stays forbidden). Keep it one line.
6. `internal/tools/errwire.go` `credentialUserOwnedContract`: UNCHANGED (it
   scopes to GIT_TOKEN, which has no delegation mechanism). Same for the prodCD
   `secret.source` "NEVER fabricate" string — still true.
7. Eval scenarios (`eval/behavioral/scenarios/` AND `scenarios-local/`):
   (a) audit EVERY launch-production scenario in BOTH directories under the new
   truth — adjust expectation prose wherever it asserts the agent WALKS THE
   USER through dashboard minting as the primary path (token-injected scenarios
   stay behaviorally valid via D-5 precedence: launchKey provided → delegated
   path never consulted); (b) **`all`-run guard first**: scenario tags are
   DESCRIPTIVE ONLY — a `live-one-shot` tag does NOT exclude a scenario from
   `behavioral all` (`cmd/zcp/eval_behavioral.go` runs every loaded scenario).
   Add frontmatter `excludeFromAll: true` to `scenarioFrontmatter` + `Scenario`
   (`internal/eval/scenario.go`), filter it in the all-run selection, and add
   parser + selection tests. Direct execution by scenario id stays allowed.
   (c) only THEN add the new scenario `launch-production-delegated.md` (prompt
   style per repo rules: 1-2 sentences, user-style, no ZCP internals) carrying
   BOTH the `live-one-shot` tag (descriptive) and `excludeFromAll: true`
   (enforced), asserting: ready-to-launch advertises the delegated path, agent
   asks for explicit confirmation, no token value ever appears in conversation,
   fallback message when delegation is consumed. A routine `behavioral all`
   must never consume the live delegation.
8. Additional sweep targets the feature also falsifies (same reword rule):
   `internal/tools/workflow.go` — the registered tool description (~:329,
   "one-shot launchKey trust model") and the `LaunchKey` field jsonschema/
   doc-comments (~:154); `internal/content/templates/agents_shared.md` (~:31,
   "user supplies a one-shot launch key");
   `internal/content/atoms/launch-source-control-required.md` (~:24 trust-
   boundary note); file-head/lifecycle comments in `launch_stage.go`,
   `launch_reset.go`, `workflow_launch_production.go`; eval drift-guard
   diagnostics text (`internal/content/eval_scenario_drift_test.go`) and
   workflow golden scenario fixtures
   (`internal/workflow/scenarios_fixtures_test.go` + goldens). Guiding
   invariant for all rewording: delegated launch crosses the credential
   through the conversation ZERO times; manual fallback crosses it ONCE.
   "Byte-for-byte unchanged" (D-5/D-6) applies to BEHAVIOR, not to guidance or
   comments that would become false.
9. Run the drift guard (`internal/content/eval_scenario_drift_test.go`) and the
   full `-short` suite.

Acceptance: per-item walk of 1-9; `go test ./... -short` green; `make lint-local`
clean; goldens regenerated and committed.

## 6. Live verification (NOT for implementation agents — the director runs it)

On the eval-zcp container with the branch binary: availability read via the MCP
surface → delegated launch against a scratch repo (real mint — consumes the
eval token's backfilled delegation, planned one-shot) → assert staged secret +
sentinel absence → prod project created → fallback branch check (second
attempt → `delegation-unavailable` blocker) → cleanup (delete created prod
project; the minted token is left for manual dashboard deletion — integration
tokens cannot delete tokens).

## 7. Out of scope (do NOT implement)

- Self-downgrade of the minted token to project-scope (BE feature request, not
  shipped). No `updateIntegrationToken` calls anywhere.
- ZCP creating/deleting delegations (platform forbids for integration tokens).
- Any change to the `ExistingProdToken` path beyond the §4.2 mutual exclusion.
- Local persistence of delegation state (D-1).
- Re-mint logic in recovery/window ops (§4.5).
- The client-wide delegation list endpoint (per-token one suffices).

## 8. Working rules for implementation agents

- TDD is mandatory: write the phase's tests first, run them, capture the RED
  output, then implement to GREEN. Report the RED evidence.
- Respect layering + depguard: `platform/` imports no internal packages;
  `tools/` may use `platform.Client` directly (launch code precedent).
- `fmt.Errorf("op: %w", err)` wrapping; no `panic`; no `interface{}` where the
  concrete type is known; English everywhere.
- Do not commit — the director reviews, runs the full gates, and commits.
- Report per-item spec-fidelity (done / deviated+why / skipped+ask) — silent
  scope cuts are forbidden; if an item looks wrong or infeasible, STOP and
  report instead of improvising.
