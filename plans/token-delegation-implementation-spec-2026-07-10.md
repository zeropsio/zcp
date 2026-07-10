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
- **D-6 — fallback is today's flow, verbatim.** No delegation / consumed /
  revoked / list-read error / mint error → the response renders the existing
  manual dashboard walkthrough and asks for `launchKey`. Fail toward the manual
  path, never block the launch on delegation-machinery errors.
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
    Token   string
    TokenID string
    Name    string
}
```
(Flatten `tokenPermissions` into the DTO — ZCP only branches on
`CanCreateProjects`; keep `RoleCode` for the honest status line. Do NOT carry
finance flags / projectPermissions — nothing consumes them.)

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
  path.ClientId{...}, body.ClientIntegrationToken{...})` with
  `roleCode=NO_ACCESS`, `canCreateProjects=true`, `projects: []` (empty, NOT
  nil if the SDK distinguishes), `name`. Return the raw `Token` + `Id`.

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
  `errors.As(mapped, &pe)` and if `pe.APICode` matches either const, return the
  `ErrDelegationUnavailable`-coded error (preserve the original message + Meta).

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
- api tier (`//go:build api`, `apitest.New(t)` harness — skips without
  `ZCP_API_KEY`): `TestAPI_ListOwnTokenDelegations_WellFormed` — asserts the
  call succeeds and rows (if any) parse; MUST NOT assert a specific count (the
  eval token's delegation will be legitimately consumed by later live
  verification).
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

Add to `WorkflowInput` (next to `LaunchKey`, ~line 154):

```go
ConfirmLaunch bool `json:"confirmLaunch,omitempty" jsonschema:"Launch-production publish only: set true ONLY after the user explicitly confirmed launching production via the delegated-token path (ZCP mints the launch token from the user-granted one-time platform delegation; no token value crosses the conversation). If launchKey is also provided, launchKey wins and no delegation is consumed."`
```

### 4.2 Publish gate (`workflow_launch_production.go`, ~line 436)

`publishing := input.LaunchKey != "" || input.ConfirmLaunch || hasExistingPath`

Extend the existing launchKey↔existing-path mutual-exclusion check (~442-448)
to also refuse `confirmLaunch=true` combined with the existing-project inputs
(same error shape as the launchKey conflict). `confirmLaunch` echoes in the
input echo (it is not a secret): add a field to `launchInputsEcho` (~1195,
currently ProductionProjectName/Region/KeepNonHA) and populate it in
`echoInputs()` (~2008); `launchKey` stays excluded.

### 4.3 Ready-to-launch response — advertise the available path

`launchReadyToLaunchResponse` (~1458) takes no ctx/client and has 4 existing
test call sites (`launch_ready_consent_test.go`) — do NOT change its signature.
Instead:
- in `handleLaunchProduction` (which owns ctx+client), where the ready-to-launch
  response is selected (~484): call `client.ListOwnTokenDelegations(ctx)`;
  availability := `err == nil && any(d.CanCreateProjects)`; then DECORATE the
  built response object post-construction (new helper, e.g.
  `attachDelegatedLaunch(resp, available)`).
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
mintedName := ""
if launchToken == "" {            // delegated path (publishing came via ConfirmLaunch)
    list delegations (D-1)        // none usable -> return delegationUnavailableResponse (D-6)
    name := delegatedTokenName(input.ProductionProjectName)
    minted, err := client.MintDelegatedLaunchToken(ctx, name)
    if err (incl. ErrDelegationUnavailable race) -> delegationUnavailableResponse (D-6)
    launchToken, mintedName = minted.Token, minted.Name
    admin = projectAdminClientFactory(launchToken, apiHost)      // + defer Close; auth-fail -> launchFailedAuthResponse shape
}
stageLaunchToken(..., launchToken)   // stage-BEFORE-create preserved
admin.CreateAndImportProject(...)    // unchanged
```

- `delegatedTokenName(prodName)`: `"zcp-launch-" + sanitize(prodName)` —
  lowercase, keep `[a-z0-9-]`, collapse repeats, trim to ≤48 chars, fallback
  `"zcp-launch"` for an empty result. Deterministic + user-recognizable in the
  dashboard token list (it also improves the existing best-effort
  `matchLaunchToken` in confirm-production).
- `delegationUnavailableResponse`: NOT an error envelope — a launch response in
  the ready-to-launch shape with a blocker (id `delegation-unavailable`,
  category matching existing blocker taxonomy) whose message says the delegation
  is absent/consumed and hands over to the manual walkthrough (today's atom) +
  `launchKey` ask. Include `"delegatedLaunch": {"available": false}`.
- **D-7 honesty**: the stage-failure abort message builder
  `launchTokenStageFailedMessage(stageErr error, pushHostname string) string`
  (`launch_stage.go:106`, one call site ~721) gains a third param
  `mintedName string` — when non-empty, the message appends: the one-time
  delegation was consumed; token `<mintedName>` exists in the dashboard; ZCP no
  longer holds its value; regenerate it there and re-call with `launchKey`.
  (`mintedName` is a local threaded value — never stored anywhere persistent.)
- The minted value must flow through EXACTLY the same two seams the launchKey
  does today (factory + stage) — no new storage, no new parameter surfaces
  beyond the local variable.

### 4.5 Window ops / recovery paths — UNCHANGED

`launchKeyFromStage`, `resolveLaunchWindowToken`, prod-ops re-ask messages,
reset, confirm-production close: no re-mint logic anywhere (after a launch mint
the token has no delegation left by definition — F4). Do not touch these files
except where §4.4 says.

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
- **Sentinel leak (P-LP-1/D-2)**: mock mints a sentinel value; extend the
  existing banned-strings scans (`workflow_launch_production_mutation_test.go`
  pattern) — sentinel absent from serialized response, launch state file, audit
  log. Also `TestLaunchState_NoLaunchKeyFieldExists`-style: no new state field
  holds it.
- D-7: stage-failure with minted token → message names the token + regenerate
  recovery.
- Ripple per CLAUDE.md change-impact: tool tests + `annotations_test.go` (input
  schema changed) + `integration/` + e2e compile (`make vet-tags` or the repo's
  equivalent guard). NOTE: `integration/` has ZERO launch-production tests today
  — the delegated-path conductor test is first-of-its-kind there; mirror the
  harness pattern of `integration/bootstrap_conductor_test.go` (in-process MCP
  server over `platform.Mock`).

### 4.7 Acceptance gate

1. RED evidence; `go test ./... -short` green; `go test ./internal/tools/... -race` green.
2. `make lint-local` clean.
3. Grep-proof: no new read of `input.LaunchKey` outside the two existing seams;
   `MintDelegatedLaunchToken` called from exactly ONE site.
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
7. Eval scenarios (`eval/behavioral/scenarios*/`): (a) verify token-injected
   launch scenarios (`launch-with-existing-cicd`, `launch-failure-build-stuck`,
   `launch-to-existing-prod-project`) still read correctly under D-5 precedence
   (they pass launchKey → delegated path never consulted) — adjust expectation
   prose only where it asserts the agent WALKS THE USER through dashboard
   minting; (b) add ONE new scenario `launch-production-delegated.md` (prompt
   style per repo rules: 1-2 sentences, user-style, no ZCP internals) asserting:
   ready-to-launch advertises the delegated path, agent asks for explicit
   confirmation, no token value ever appears in conversation, fallback message
   when delegation is consumed. Tag it clearly as **live-one-shot** (a real run
   consumes the project token's delegation) so it is excluded from `all` sweeps
   — follow the existing tag conventions in neighboring scenario files.
8. Run the drift guard (`internal/content/eval_scenario_drift_test.go`) and the
   full `-short` suite.

Acceptance: per-item walk of 1-8; `go test ./... -short` green; `make lint-local`
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
