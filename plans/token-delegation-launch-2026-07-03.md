# Token delegations → self-minted launch token (Alpha 2026-07-03)

**Status:** Phase 0 (Alpha live verification) DONE 2026-07-03; **PROD verification
DONE 2026-07-10** — feature is live on prod, Phases 1-3 unblocked. One side
observation from alpha: plain CreateProject rejects minted tokens (likely
intended — import-path-only; error shape worth a mention to BE).

### Prod verification (2026-07-10, api.app-prg1.zerops.io)

- All delegation endpoints shipped, plus one NEW vs the alpha snapshot:
  `GET /client/{id}/integration-token/delegation` (`listAllIntegrationTokenDelegations`,
  client-wide — no tokenId needed; FE-oriented but also the cheapest availability
  probe for ZCP).
- **Existing ZCP tokens got BACKFILLED delegations**: the eval-zcp token
  (`zcp-eval-new`, tokenId `3U4vJrDsRvKrAIwBWAw32A`) carries one delegation with
  the standard spec, `created: 2026-07-10T08:54:17Z` (= deploy time). So the
  fallback path is NOT the majority path for existing users — confirm generality
  with Jan (observed n=1).
- Identity mechanics identical to alpha (tokenId == `/user/info` id).
- Deliberately did NOT mint on prod — it would consume the eval token's only
  delegation (wanted for Phase 1-2 live verification). Mint semantics were fully
  live-verified on alpha 2026-07-03; prod runs the same (newer) build.
- e2e repeatability note: a mint e2e test consumes the delegation permanently
  (re-grant needs a personal token). Plan: automated api-tier tests for READ
  paths + the negative mint (no delegation → typed 403); the positive mint is a
  one-shot manual live verification during Phase 2 bring-up.

Findings ledger in §Phase 0 results below.

## What the platform shipped (live on Alpha)

A **delegation** is a one-time power of attorney: a personal token grants ZCP's
integration token the right to mint ONE new integration token with a pre-defined
scope, owned by the delegating user. Every NEW ZCP token automatically carries one
delegation with spec:

```json
{"roleCode":"NO_ACCESS","canCreateProjects":true,"canEditFinances":false,"canViewFinances":false,"projectPermissions":[]}
```

This is **exactly** the launch-token shape our atom walkthrough tells the user to
hand-craft in the dashboard (`launch-mutation-key-required.md`: Custom access per
project + "Allow creating projects" ON + zero per-project entries — spike §A.8
called it "the minimal launch-window shape").

### API surface (from Alpha swagger, `plans/…` n/a — spec fetched 2026-07-03)

| Endpoint | Who can call | Notes |
|---|---|---|
| `GET /client/{clientId}/integration-token/{tokenId}/delegation` | any role incl. NO_ACCESS, **integration tokens allowed** | list; availability check |
| `GET …/delegation/{delegationId}` | integration tokens allowed | detail; docstring: "New token is created under the user who created the delegation… used for e.g. ZCP… hand-over phase" |
| `POST …/delegation` | **personal tokens only** (`NotAllowedForIntegrationToken`) | create — user-side, FE later |
| `DELETE …/delegation/{delegationId}` | **personal tokens only** | revoke — user-side |
| `POST /client/{clientId}/integration-token` | previously 403 for integration tokens (live-verified 2026-06-11); **the delegation carves out the one-time exception** | THE MINT. Response `ClientIntegrationTokenRaw` incl. raw `token` — shown once |

SDK coverage: mint **already in** zerops-go (`PostClientIntegrationToken`,
raw `Token` in output DTO). Delegation list/detail **not in SDK** (checked
v1.0.20 + v1.0.21) — hand-roll on an authorized `sdkBase.Environment` retained on
`ZeropsClient` (no parallel raw-HTTP path; inherits endpoint + bearer).

### Prior art — we designed this and shelved it on the 403

- `plans/archive/launch-pipeline-first-2026-06-11.md` P5 "v2 automation": designed
  `platform.MintIntegrationToken` via `PostClientIntegrationToken`, DEFERRED solely
  because live probe returned `403 notAllowedForIntegrationToken`. The delegation
  removes exactly that 403.
- `plans/archive/launch-single-token-lifecycle-2026-06-11.md`: "one-shot only by
  convention; platform expiry is roadmap" — the roadmap arrived (as one-time
  delegation, not token expiry).

## Alpha verification (Phase 0 — BLOCKS everything)

Probe script ready: `eval/scripts/alpha-delegation-probe.sh` (curl-level, no zcp
binary needed; tokens masked; raw responses kept for error-mapping design).

**Needed from Karel:**
1. A **fresh ZCP integration token created on Alpha** (after the feature deploy —
   carries the auto-delegation). `ALPHA_ZCP_TOKEN=…`
2. Optionally an **Alpha personal token** (`ALPHA_PERSONAL_TOKEN=…`) — unlocks the
   create/revoke-delegation tests + cleanup of minted probe tokens. Without it,
   the one delegation is consumed by a single probe run and re-testing needs a new
   ZCP token each time.

Test matrix (what the probe answers):

| # | Question | Why it matters |
|---|---|---|
| 1 | Does `/user/info` work for the ZCP token; is its `id` == `{tokenId}` in delegation paths? | self-identity: ZCP doesn't know its own tokenId today (`GetUserInfo` discards `out.Id`) |
| 2 | Fresh ZCP token shows exactly 1 delegation with the advertised spec? | availability check drives the workflow branch |
| 3 | Mint with MORE than delegated (roleCode=ADMIN) rejected? | security — over-reach must fail |
| 4 | Mint with matching body returns raw token? Exact-match or subset semantics? | request-shape contract for `platform.MintDelegatedToken` |
| 5 | Second mint fails — with WHAT error code/body? Delegation row deleted or kept+flagged? | error mapping (`ErrDelegationConsumed`?) + honest status |
| 6 | Minted token: authenticates, `canCreateProjects`, creates project, has Owner/creator access on it, NO access elsewhere, cannot chain-delegate? | the minted token must carry the whole launch window + GH Actions `zcli push` |
| 7 | ZCP token cannot create a delegation for itself? | privilege-escalation guard |
| 8 | Personal token: create → revoke → mint fails? | revocation semantics for the FE story |

Report findings to Jan (he gates the prod deploy on this). Anything red in #3/#7
is a BE security bug, not a ZCP problem.

### Phase 0 results (live Alpha run, 2026-07-03; raw responses archived in the run scratchpad)

| # | Verdict | Finding |
|---|---|---|
| 1 | ✅ | `tokenId == /user/info id` — integration tokens are user-shaped; `Kco8ya6aTpmXQaEHD6iKZg` is both. `UserInfo.UserID` capture (Phase 1) is the right self-identity mechanism. Integration token can read `integration-token/list` (200) and its own delegation list. |
| 2 | ✅ | Fresh ZCP token carries exactly 1 auto-delegation, spec byte-identical to the advertised `{NO_ACCESS, canCreateProjects:true, no finances, projectPermissions:[]}`. |
| 3 | ✅ | Over-reach mint (roleCode=ADMIN) → `403 roleLevelExceeded` ("Cannot manage role 'ADMIN' - your role 'NO_ACCESS' has insufficient privileges"). Rejection does NOT consume the delegation (mint succeeded afterwards). |
| 4 | ✅ | Mint → 200 with raw `token` in body; minted token = NO_ACCESS + canCreateProjects. Request body mirroring the delegation spec works; over-asking is rejected (see #3), so semantics are "≤ delegation". |
| 5 | ✅ | Second mint → **`403 notAllowedForIntegrationTokenWithoutDelegation`** ("…Please create a one-time delegation, use a personal token or log in…") — the typed apiCode ZCP maps for the fallback branch. **Consumed delegation is DELETED from the list** (count 0) — availability check = list non-empty; no used-flag state to interpret. |
| 6 | ✅ | Minted token authenticates (own identity `slEWPtixTpy5NUzwp6Kspw`, name from mint request); **no delegation chaining** (mint does not auto-delegate the minted token); `POST /client/{id}/project/import` → 200 (created + ACTIVE), GET own project 200, GET other project **403** (isolation), DELETE own project 200 — creator access carries the full launch-window lifecycle. Side observation: plain `POST /client/{id}/project` (CreateProject) → persistent `400 userNotFound` on the token's OWN clientUserId (`DsfCoWQlQLOUgYme8AEa5g`) — per Karel likely INTENDED (minted tokens are import-path-only), not a bug. If intended, the error shape is off: deliberate blocks elsewhere return typed 403s (`notAllowedForIntegrationToken`, `roleLevelExceeded`), not 400 `userNotFound` on the caller's own identity — worth a low-priority mention to BE. Irrelevant to ZCP either way (SDK `PostClientProjectImport` hits the working `/client/{id}/project/import`). |
| 7 | ✅ | Integration token cannot create a delegation for itself (403). |
| 8 | ⏭ | Skipped — no Alpha personal token (create/revoke-delegation semantics untested; revoked-delegation mint untested). |

Residue on the Alpha account (integration tokens cannot delete tokens): minted
probe token `zcp-launch-probe` (id `slEWPtixTpy5NUzwp6Kspw`) — delete in the GUI.
The ZCP token's single delegation is consumed; a re-run needs a new ZCP token or
a personally-created delegation.

## Integration design (Phases 1–3, after prod deploy)

**Pattern: the delegation replaces the ASK, not the lifecycle.** Everything
downstream of token acquisition is value-agnostic and stays byte-for-byte:
stage as `ZCP_LAUNCH_TOKEN` before create (P-LP-14), stage-first window reads,
secret-to-secret `ZEROPS_TOKEN_PROD` conveyance, physical close by env delete.
Only the acquisition head changes: mint-if-delegated, ask-if-not.

### Phase 1 — platform layer (L1)

- `internal/platform/zerops_delegation.go`: `ListTokenDelegations` /
  `GetTokenDelegation` (hand-rolled via retained `sdkBase.Environment`),
  `MintDelegatedToken` (SDK `PostClientIntegrationToken` on the **standing**
  client — clientId already cached via `getClientID`).
- Capture own token id: add `UserInfo.UserID` from `out.Id` (zerops.go:121-126)
  — pending probe #1 confirmation that this equals `{tokenId}`.
- New error codes in `errors.go` (shape from probe #5): consumed/absent delegation
  must map to a typed code the workflow can branch on (fallback to ask-user).
- Mock additions encode PROBE-VERIFIED behavior only (CLAUDE.md: mocks never
  encode assumed behavior). api-tier test behind `//go:build api`.

### Phase 2 — workflow integration (L3/L4)

- **Availability read** at launch-production entry (scope-prompt/ready-to-launch):
  list delegations for self; response says which path is live. Platform stays the
  single source of truth — no local consumed-flag gating (the `WindowClosedAt`
  precedent: local stamps are honest-status only).
- **Mutation head seam** (`executeLaunchMutation`, workflow_launch_production.go:611+710):
  when a delegation is available, mint LATE — after all pre-create gates pass,
  immediately before staging — then feed both existing use-sites and stage
  instantly. `input.LaunchKey` becomes fallback-only.
- **Publish gate**: today `publishing := input.LaunchKey != ""` — key-presence is
  the user-consent signal. With delegation there is no key; replace with an
  explicit confirm input on the mutation call (design decision: reuse the
  ready-to-launch → re-call pattern with a `confirmLaunch`-style flag).
- **Fallback = today's flow, verbatim**: no delegation (ALL existing user tokens),
  consumed delegation, revoked delegation, mint API error → surface the current
  dashboard-mint walkthrough + ask. Second launch-production on the same ZCP token
  lands here by design (one delegation per token until FE ships delegation
  management).
- **Mid-flight loss**: mint burns the delegation even if the flow dies before
  staging. Recovery is honest: report the burned delegation + fall back to the
  manual ask. No retry magic, no local persistence of the raw value (P-LP-1).
- Minted token `name`: deterministic + user-recognizable (e.g.
  `zcp-launch-<prodProjectName>`) — shows up in the dashboard token list and in
  the confirm-production `tokenLifecycle` match.

### Phase 3 — knowledge + spec + eval truth sweep

Statements the feature falsifies (prior-art reader's ledger):
- `docs/spec-workflows.md` §10 intro "ONE user-minted integration token" + P-LP-14
  "no one-shot token type exists" (line 1310).
- `internal/tools/launch_confirm.go:228` tokenLifecycle "Zerops has no one-shot
  token type"; `workflow_launch_production.go:1468` (user-mints guidance).
- Atoms: `launch-intro.md:29`, `launch-mutation-key-required.md` (whole dashboard
  walkthrough becomes the FALLBACK path), `launch-scope-prompt`, `idle-launch-entry`
  ("user-minted… passed once").
- Credential contract wording (`errwire.go:158` + CLAUDE.md trap): keep "the AGENT
  never fabricates a token"; permit "ZCP mints via platform delegation". The
  boundary moves from "no token is ever created by software" to "token creation is
  the platform's act, authorized by a user-granted delegation".
- `internal/knowledge/themes/operations.md:109` "Integration tokens cannot exceed
  creator's permissions" — needs the delegation carve-out sentence.
- Eval scenarios: all launch-production scenarios asserting the "user generates/
  pastes token" step get a delegated-path variant; token-injected live scenarios
  (`launch-with-existing-cicd`, …) keep working (fallback path).
- Atom goldens regen (`ZCP_UPDATE_ATOM_GOLDENS=1`), eval-scenario drift test.

### Explicitly NOT in scope

- ZCP creating delegations (platform forbids for integration tokens; user-side FE).
- Local persistence of delegation state (platform is the source of truth).
- Any change to `ExistingProdToken` path (project-scoped token, different shape —
  delegation spec has `projectPermissions:[]`, doesn't cover it).
- zcli/alpha region seams (`zcli login` without `--regionUrl` in deploy paths) —
  pre-existing gap, only matters for running full ZCP against Alpha; the probe is
  curl-level and doesn't hit it. Backlog-worthy separately if we ever want
  full-stack Alpha e2e.

## Sequencing / gates

0. **Probe on Alpha** (needs tokens from Karel) → findings to Jan → he deploys prod.
1. Phase 1 after prod deploy (api-tier tests need the feature on prod; eval-zcp
   gets a fresh ZCP token to carry a real delegation).
2. Phase 2 + 3 together (guidance must flip in the same release as behavior —
   tell/check single-owner discipline), flow-eval round on the delegated path +
   the fallback path.

## Open questions

Probe answered #1 (`tokenId == /user/info id`), #4 (mint ≤ delegation, over-ask
403 `roleLevelExceeded`), #5 (consumed = deleted; absent-delegation mint =
`notAllowedForIntegrationTokenWithoutDelegation`) — see Phase 0 results. Still open:

- Does the auto-delegation also fire for integration tokens created via the
  public API by a personal token, or only for the FE "ZCP token" creation flow?
  (Matters for e2e repeatability — how eval-zcp gets a delegated token.) Ask Jan.
- Whether existing ZCP tokens get backfilled delegations on prod deploy
  (assumption: NO — desc says "all NEW ZCP tokens"). Ask Jan.
- Confirm with Jan: plain CreateProject rejecting delegation-minted tokens
  (Phase 0 #6) is intended (import-path-only by design, per Karel)? If so,
  suggest a typed 403 instead of 400 `userNotFound` — cosmetic, non-blocking.
- Delegation create/revoke semantics (probe #8) — untested without a personal
  token; FE story territory, not needed for ZCP Phase 1-3.
