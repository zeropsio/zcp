# Token-delegation audit fixes (2026-07-12)

Fix round for the confirmed findings of the 2026-07-12 audit of the merged
`feat/token-delegation-launch` feature (Codex GPT-5.6-sol adversarial review +
5-dimension Sonnet review with adversarial verification + live platform
verification). Binding upstream spec: `plans/token-delegation-implementation-spec-2026-07-10.md`
(D-1..D-9, §4.4 mint-outcome table, §4.5 delegated retry + reset).

**Scope rule:** every fix aligns code/guidance to the ALREADY-SPECIFIED contract
(spec-fidelity repair), except P4 which hardens reset sequencing the spec itself
under-specified. No new features, no wire-shape breaks; blocker IDs stay; message
texts may change (no test pins the exact texts being edited — verified).

Out of scope (open notes, not fixed here):
- Backfill generality (do ALL pre-existing ZCP tokens carry a delegation?) — needs
  Jan's answer before any messaging distinguishes "structurally never available"
  from "not granted"; the P3 could-not-check split is independent of it.
- Concurrency TOCTOU on launch state files (two sessions racing) — pre-existing
  property of the launch state machine, not delegation-specific.
- Stale eval run `eval/behavioral/runs/20260710-111019/` — local artifact, not tracked.

---

## P1 — Forensic state retention on post-mint aborts (audit #2, major)

**Today:** `abortDelegatedMint` (`internal/tools/launch_delegation.go:162`) writes a
bare `launchState{LaunchID, SourceProjectID, TargetProjectName, Status:Failed,
LastError}` — dropping `TokenAcquisition`/`MintedTokenName` persisted by the FATAL
pre-mint write, and never carrying `TargetServiceHostname`. On exactly the D-7
outcomes where a standing token EXISTS (empty-token, admin-factory failure,
staging failure) the state can no longer name it, and reset skips the staged-secret
delete (`launch_reset.go:212` keys on `state.TargetServiceHostname != ""`) after an
ambiguous staging failure whose env write may have committed (`launch_stage.go:112`).

**Fix:** extend `abortDelegatedMint` to accept the forensic truth of each call site:

```go
abortDelegatedMint(stateDir, launchID, sourceProjectID, targetProjectName, reason,
    mintedName, stagedHostname string)
```

- `TokenAcquisition: "delegated"` always (this helper is delegated-path-only).
- `MintedTokenName: mintedName` — per call site truth:
  - race-unavailable (`launch_delegation.go:273`): `""` — the 403 proves nothing was created.
  - indeterminate mint (`:281`): requested name — the token MAY exist (outcome-table row 2).
  - empty-token (`:286`): requested name — the token DOES exist.
  - admin-factory failure (`workflow_launch_production.go:823`): requested name.
  - staging failure (`workflow_launch_production.go:858`): requested name.
- `TargetServiceHostname: stagedHostname` — `""` everywhere except the staging-failure
  site, which passes `primaryRuntime.PushHostname` (the write may have committed →
  reset must attempt the delete; `EnvDeleteServiceKeyIfPresent` already tolerates
  absent keys, so a not-committed write is a no-op delete).

**Tests (RED first), `internal/tools/launch_delegation_test.go`:**
- Per-outcome state-file assertions: after each of the 5 abort outcomes, read the
  state file and assert `TokenAcquisition`/`MintedTokenName`/`TargetServiceHostname`
  match the table above (race asserts EMPTY name).
- Reset-after-staging-failure: drive the staging-failure outcome, then
  `handleLaunchReset` with valid ack → asserts `EnvDeleteServiceKeyIfPresent` is
  reached for the push hostname (staged secret no longer orphaned).

## P2 — Failed-state recovery guidance: delegated retry, not reset (audit #1, major)

**Today:** for EVERY `failed` state the status path says reset:
`renderLaunchTerminalRecovery` (`launch_status_recovery.go:151+`) emits
`NextCall: action="reset"` unconditionally and its fallback guidance says
"reset ... before retrying with a fresh launchKey"; the atom
`launch-status-recovery.md` launch-failed section says "reset required" +
"fresh launchKey". But the resume gate (`workflow_launch_production.go:286`)
allows direct retry for `failed + TargetProjectID==""`, and §4.5 delegated retry
reuses the staged token with ZERO delegation calls. Following today's guidance
after a delegated failure DELETES the staged token (reset = abandonment) while the
delegation is already consumed — the agent burns the user's only remaining copy in
a recoverable situation. Spec P-LP-15/§4.5 explicitly requires: "The failed-state
guidance directs the caller to retry with confirmLaunch=true."

**Fix (three surfaces, one truth):**
- `renderLaunchTerminalRecovery`: branch on the state. `Failed + TargetProjectID==""`
  → `NextCall: action="start" ... confirmLaunch=true` when
  `TokenAcquisition=="delegated"` (guidance line: retry reuses the staged token,
  zero delegation consumed; reset only to ABANDON — it deletes the staged token);
  plain retry guidance otherwise. `Failed + TargetProjectID!=""` → keep reset
  (resume gate refuses retry there — correct today).
- Envelope honesty (D-7 loop closure, uses P1 fields): add non-secret
  `TokenAcquisition` + `MintedTokenName` to `launchActiveEnvelope` (omitempty),
  so a post-compaction `action="status"` can still say which dashboard token to
  regenerate. Names/modes only — never values.
- Atom `internal/content/atoms/launch-status-recovery.md`: rewrite the
  launch-failed section to the two-branch truth (retryable delegated no-target
  case vs reset-for-abandonment / failed-with-target case); fix the line-20
  quick-reference row ("ready-to-launch needs launchKey" → delegation-aware,
  matching its own paragraph at line 32). Respect atom lint (no handler verbs,
  no spec IDs). Regenerate goldens: `ZCP_UPDATE_ATOM_GOLDENS=1 go test ./internal/workflow/...`.

**Tests (RED first):**
- `renderLaunchTerminalRecovery` table test: (failed, no target, delegated) →
  NextCall carries `confirmLaunch=true` + guidance names the staged-token reuse
  and warns reset deletes it; (failed, no target, manual/empty acquisition) →
  retry guidance without the delegated line; (failed, with target) → reset NextCall
  (current behavior pinned).
- Envelope test: MintedTokenName/TokenAcquisition surface for failed states,
  absent (omitempty) when empty.
- Existing pins on the old unconditional reset NextCall: update.

## P3 — List-error honesty at mutation time (audit #4, minor)

**Today:** `resolveDelegatedLaunchToken` (`launch_delegation.go:231-238`) routes
`listErr != nil` and confirmed-empty through the identical
`delegationUnavailableResponse`, whose blocker text asserts definitively
"never granted one, already consumed, or revoked" — a lie when the truth is
"the availability check itself failed" (spec §4.4 pinned contract: a LIST failure
= "could not check" → manual fallback WITHOUT exposing the underlying error).

**Fix:** `delegationUnavailableResponse(..., couldNotCheck bool)`; the blocker
message gets a could-not-check variant ("Could not verify delegation availability
(the check itself failed — transient platform/network error, details in server
logs). Fall back to the manual path ... or retry with confirmLaunch=true.").
Blocker ID stays `delegation-unavailable`; underlying error stays stderr-only.
Ready-to-launch decoration (`workflow_launch_production.go:515+`) is already
spec-compliant (`available:false`, fail-open) — unchanged.

**Tests (RED first):** mutation path with mock list error → response carries the
could-not-check wording, NOT the definitive no-delegation wording, zero mint calls
(this branch has NO test today); confirmed-empty keeps the definitive wording.

## P4 — Reset idempotence after partial success (audit #3, minor)

**Today:** reset deletes the orphan project, then the staged secret, then the state
file (`launch_reset.go:174-240`). If the project delete SUCCEEDS and the secret
delete FAILS, state still carries `TargetProjectID`; the retry re-runs
`DeleteProject` on the now-gone project, errors, and aborts before ever reaching
the secret again — permanently wedged (only manual escape).

**Fix:** checkpoint between the two steps: after a successful `DeleteProject`,
persist the state with `TargetProjectID=""` (+ `LastError` note "orphan project
<id> deleted by reset; staged-secret cleanup pending") BEFORE attempting the
secret delete. A retry then derives `deleteProject=false` and goes straight to
the secret. The retry needs a fresh destructive ack for the narrowed target set —
acceptable honest round-trip. If the checkpoint write itself fails, refuse
completion (state preserved, same shape as the existing delete-failure refusals).

**Tests (RED first):** project delete OK + secret delete fails → response refuses,
persisted state has empty `TargetProjectID`; re-call with fresh ack → zero
`DeleteProject` calls, secret delete retried, state removed on success.

**Acknowledged residual (plan review):** after the checkpoint, `action="status"`
reads the state as failed-no-target and offers the retry direction even though
the operator intended abandonment — safe (no wedge, no delegation burned, the
staged secret still exists so a retry genuinely works); the `LastError`
"staged-secret cleanup pending" note carries the reset context.

**Plan-review adjustment applied:** the D-7 envelope forensics are surfaced by
BOTH renderers — `renderLaunchTerminalRecovery` AND `renderLaunchActiveRecovery`
(the crash-after-mint scenario leaves a non-terminal `launching` state that
routes through the active renderer). Pinned by
`TestRenderLaunchActiveRecovery_SurfacesDelegatedForensics`.

## P5 — Missing test pins (audit #5, minor)

1. **L1 mint POST body** (`internal/platform/zerops_delegation_test.go`): the mint
   test server never reads `r.Body`. Add body capture + assertions: `roleCode=="NO_ACCESS"`,
   `canCreateProjects==true`, `canViewFinances==false` AND `canEditFinances==false`
   PRESENT-and-false (not absent), `projects` present as `[]` (not null), `name`
   round-trips. Spec §3.2: finance denial is an invariant, "not an incidental Go
   zero value" — currently unpinned.
2. **§4.5 stage-read-error branch** (`launch_delegation.go:218-226`): dedicated test —
   stage READ error → `delegation-stage-read-failed` blocker, zero
   `ListOwnTokenDelegations`/`MintDelegatedLaunchToken` calls.
3. **Staging-failure sentinel scan parity** (`launch_delegation_test.go:755+`):
   extend `TestExecuteLaunchMutation_MintOutcome_StagingFailure` with the
   response-text sentinel check and stderr capture its sibling outcome tests
   already do (state-dir scan alone today).

## P6 — Truth-sweep leftovers in engineer-facing prose (audit #6, minor)

Agent-facing content verified clean; these are the surviving stale claims:
- `docs/spec-workflows.md` §10.1 (~line 1276): header "Mutation pipeline
  (`launchKey` required from this point on)" → "(token acquisition required from
  this point on — delegated `confirmLaunch=true` mint, or manual `launchKey`
  fallback)"; the `failed` bullet (~1280) "retries with a fresh launchKey" →
  "+ retries with `confirmLaunch=true` (delegated; staged token reused) or a fresh
  `launchKey`". Also re-check the §10.2 reset prose against P4's checkpoint.
- `internal/topology/types.go` (~244, 292, 295, 311): package + status-constant
  doc-comments still present launchKey as the sole mechanism → mention the
  delegated confirmLaunch path (comments only, no code).
- Backwards "the one-shot launchKey is minted" comments:
  `workflow_launch_production.go:208`, `:1136`, `launch_source_control_gate.go`
  (grep `launchKey is minted\|one-shot launchKey is minted`) — the manual key is
  user-generated, only the delegated path MINTS.

## Review rounds (post-implementation record)

**Sonnet plan review** (pre-implementation): GO-WITH-ADJUSTMENTS. Applied: the
D-7 envelope forensics surface in BOTH status renderers (the crash-after-mint
scenario leaves a non-terminal `launching` state → active renderer). Acknowledged:
P4 post-checkpoint status offers retry though the operator intended abandonment
(safe residual).

**Codex GPT-5.6-sol diff review**: NO-GO round with 5 findings, all fixed:
1. (High) P4 checkpointed an ASYNC deletion as complete → now
   `deleteOrphanProjectConfirmed` polls the delete process to terminal via
   `ops.PollProcess`; FAILED/unconfirmed ⇒ no checkpoint, state kept
   (`TestHandleLaunchReset_DeleteProcessFails_NoCheckpoint`).
2. (High) P2 equated "delegated" with "staged" → staged-token-reuse guidance now
   requires `TargetServiceHostname` (staging reached); pre-stage aborts get the
   honest probe-then-fallback wording
   (`TestRenderLaunchTerminalRecovery_FailedNoTargetDelegatedPreStage_NoStagedClaim`).
3. (Medium) atom + blocker texts said "generate a launchKey" → user-mediated
   wording ("ask the user to generate ... never create or guess a token value").
4. (Low) missing staging-failure → reset chain pin → added
   (`TestExecuteLaunchMutation_StagingFailureThenReset_ReachesSecretDelete`, drives
   the real abort state, seeds the committed-write case, asserts actual deletion).
5. (Low) doc leftovers: P-LP-14 "no project, no state" scoped to the manual path;
   resume-gate + B6 + renderer doc-comments reworded.

## Sequencing & gates

P1 → P2 (P2's guidance branches on P1's retained fields) → P3 → P4 → P5 → P6.
Each phase: RED evidence → GREEN → `go test ./internal/tools/... ./internal/platform/... -short`.
Atom edits (P2): regen goldens + `internal/content` lint tests + eval-scenario
drift test. Final gate: `go test ./... -short`, `go test ./internal/tools/
./internal/platform/ -race -count=1`, `make lint-local`, per-item plan-fidelity walk.

Estimated ~350-450 LOC incl. tests, single sitting.
