# The deploy non-running gate is a category error — investigation + verdict

**Date:** 2026-06-04
**Trigger:** Karel asked to question whether the whole deploy/destruct *gate paradigm* is right — not to design the previously-proposed `confirmDeploy` ack-escape (rejected as a symptom-fix).
**Method:** my own code+commit read + a Codex deep pass + a 5-angle Workflow (archaeology / family-coherence / information-contract / agent-process-transcript / greenfield). **All seven converged, no dissent.** Every load-bearing claim verified against the code.
**Status:** DECISION-READY. No code touched (Wave 6 flow-eval was building the tree). Implementation awaits Karel's fork choice + wave completion.

---

## Verdict (one paragraph)

`GateNonRunningOnDeploy` — the pre-flight refusal that blocks `zerops_deploy` / `dev_server` when the target is FAILED or READY_TO_DEPLOY-with-failed-history — **should be deleted**, keeping only the `zerops_import override=true` gate. Gating a corrective redeploy of a failed service is a **category error**: a failed deploy is *non-destructive* (ZCP's own classifier writes "the new appVersion is DEPLOY_FAILED and was not activated; the previous version keeps serving … stays ACTIVE on the old version" — `deploy_failure.go:174-176`). The only genuinely destructive op in the set is `zerops_import override=true` (`destructive_ack.go:21-24` enumerates the loss: containers, code, env, mounts), and it **already has the correct gate**: a `wouldDestroy` payload + a `confirmDestructive` ack that actually *clears* it (`import.go:206`). The deploy gate, by contrast, has **no satisfied-state parameter at all** (signature is `ctx/client/fetcher/projectID/target` — a pure live read), so it re-fires byte-identically every retry and clears *only* when the service reaches RUNNING — which requires the successful deploy the gate blocks. That is the deadlock, and it is the gate's designed control flow. **Net effect is inverted safety: the SAFE corrective action has a no-exit hard block while the DESTRUCTIVE escape has a working exit — so the blocked agent rationally escalates into the wipe the system meant to prevent.**

---

## The deadlock (byte-reproduced, recipe-nextjs-ssr-frontend-standard, run 20260603-232540)

```
cross-deploy appdev→appstage BUILD_FAILED ("public/ missing" — verbatim in buildLogs)
  → agent reads the cause FROM the failed-deploy response, fixes it (creates public/)
  → redeploy → GateNonRunningOnDeploy refuses: DIAGNOSIS_REQUIRED, recovery=zerops_events
  → agent runs zerops_events (the prescribed recovery)
  → redeploy → BYTE-IDENTICAL refusal (no "diagnosis done" exit exists)
  → agent escalates to zerops_import override=true → WIPES appstage  ← the exact harm the gate exists to prevent
```

---

## Evidence (each verified against code / commit / doc)

1. **It's a category error, grounded in platform reality.** A failed deploy doesn't activate the bad version; the prior keeps serving — `deploy_failure.go:170-181`. The only loss-enumerating path is import-override — `destructive_ack.go:21-24`. No `DeployGateError` path carries a `wouldDestroy` payload because there's nothing to destroy.
2. **The deadlock is structural, not an edge case.** `GateNonRunningOnDeploy` (`deploy_preflight_gate.go:76-102`) switches purely on `target.Status`. READY_TO_DEPLOY + classified failed history (the post-BUILD_FAILED state) has exactly one outcome: fire. Grep-confirmed **zero** `confirmDestructive`/ack/diagnosed references across the gate + both deploy paths + both tool layers. Contrast `import.go:206 ValidateDestructiveAck`, which lets the second call through.
3. **The gate's stated job is already done by the response.** Every failed deploy carries `failureClassification{Category,LikelyCause,SuggestedAction,Signals}` + buildLogs/runtimeLogs — `deploy_common.go:44-52` + `deploy_poll.go:127-142`; "agents read this FIRST" (CLAUDE.md). The classifier commit `821f6113` (2026-04-27) **predates the gate** `9f3a16e8` (2026-05-05) by 8 days. The prescribed `zerops_events` recovery reuses the *same* `ClassifyDeployFailure` path (`failed_context.go:123`) → surfaces nothing new. Textbook "masking fallback" the Information Contract forbids.
4. **The v4 plan itself disowned the generalized gate.** `plans/archive/zcp-diagnose-before-destruct-2026-05-05.md:16` — "Read-before-act as a generalized invariant is the wrong abstraction"; ":18" — "the empirical problem is a single missing gate on `zerops_import`." The deploy gate was Phase 2.2 *"defence-in-depth,"* never the architectural piece. Its destructive-surface catalog never lists a redeploy.
5. **The gate is a VESTIGE.** At introduction, the READY_TO_DEPLOY recovery it wrapped WAS an auto-destructive `import override=true` (the Wave-1 data-loss bug). When that recovery was changed to read-only `zerops_events` (Wave-1 fix), the gate that wrapped it survived the change that removed its justification.
6. **The gate's own doc-comment is the disproof.** `deploy_preflight_gate.go:61-70` reasons "a deploy is CORRECTIVE + non-destructive … broadening would block the very recovery deploy the agent issues after diagnosing" — yet the *current narrow* predicate already gates exactly that recovery deploy. The 2026-06-03 Codex-review rejection addressed predicate-*width* but never asked whether the gate should exist.
7. **The family is incoherent.** Two gates fire on the SAME failed service, route to DIFFERENT tools (deploy→`zerops_events`; import→`zerops_logs`+`confirmDestructive`), share no satisfied-state. The coherent owner is **operation safety class**: destructive→hard ack gate; non-destructive corrective→advisory+proceed; impossible-precondition→precondition error; read→Recovery hint.
8. **The code contradicts ZCP's PUBLISHED contract.** `../zerops-docs/.../tokens-and-project-access.mdx:120`: *"Two operations carry an explicit confirmation gate … service deletion and wholesale service replacement on a service with prior failed deploy history. Everything else — deploys, env changes … runs without pausing for approval … a bad deploy is fixed by another deploy … not via a pre-call gate."* The doc gates exactly delete + import-override; deploys explicitly run ungated. **Deleting the deploy gate restores the code to its own published spec.** (Doc reads as designed intent → code is the drift.)
9. **Transcript evidence: zero positive instances.** ANGLE-4 surveyed June runs: in every case the gate pushed the agent toward the destructive escape; it never once prevented a bad action. In the deadlock run the agent had already diagnosed + fixed from the deploy response before being refused.

---

## Blast radius (verified)

**Delete refusals (3 non-test call sites):**
- `deploy_local.go:95`, `deploy_ssh.go:134` — DELETE the `GateNonRunningOnDeploy` call + the `errors.As(&DeployGateError)` branches in `tools/deploy_local.go` + `tools/deploy_ssh.go`.
- `dev_server.go:276` — SPLIT, don't reuse (see decision D2).
- Delete `GateNonRunningOnDeploy` + `NewDeployGateError` + `DeployGateError`.

**Keep unchanged:**
- The import-override gate (`gateOverrideOnFailedHistory` + `confirmDestructive`) — the only gate guarding a destructive op, with the correct working ack-exit.
- `NonRunningRecovery` as a **non-blocking HINT** on the `workflow_checks.go:230` status check — a status check reporting non-running is informational, not a block.
- The DM-2 self-deploy source-destruction gate (`deploy_validate.go:87`) — genuinely destructive, correctly hard.

**Tests to rewrite (refuses→proceeds):** `deploy_preflight_gate_test.go`, `deploy_gate_integration_test.go`, `dev_server_gate_test.go`. **Invariant to rewrite:** the CLAUDE.md "Diagnose-before-destruct gates always-dangerous operations" bullet — distinguish HINT (keep) from REFUSAL (delete); narrow "always-dangerous" to delete + import-override only.

---

## Options

| Option | Prevents the real wipe? | Avoids deadlock? | Verdict |
|---|---|---|---|
| **(e) Delete deploy+dev_server refusal; keep import-override gate (RECOMMENDED)** | Yes — import-override stays gated | Yes — no hard block remains on the safe op | Best fit; restores published contract |
| (a) Delete refusal + attach prior `failureClassification` as an advisory field on the redeploy response | Yes | Yes | ≈ (e), small response noise; agent already has it |
| (c) Keep gate + add `confirmDeploy` ack exit (the rejected symptom-fix) | No net gain — gates an op that destroys nothing | Mechanically yes | Entrenches the wrong abstraction + a published-spec contradiction |
| (loop-guard) Replace status-gate with "N consecutive byte-identical-input redeploys" detector | No (not a destruction guard) | Yes | Machinery for an unproven need; transcripts show agents self-correct turn-1 |

**Recommendation: (e).** It removes the deadlock at the root, restores the code to its own published contract, and leaves the one genuinely-destructive op correctly gated. The only thing lost is a "don't burn a build cycle" *efficiency nudge* — which should be advisory (the failed-deploy response already nudges).

---

## Genuine Karel-decisions

- **D1 — (e) vs (c).** Deleting a shipped, pinning-tested behavior (flip three test files refuses→proceeds) vs the cheap ack-patch. Recommendation (e); (c) entrenches a spec contradiction.
- **D2 — dev_server pre-spawn.** Spawning a dev server on FAILED/READY_TO_DEPLOY genuinely can't proceed (SSH would time out opaquely) — so keep a CLEAR check there, but reshape it: a *precondition error* ("deploy the service to RUNNING first") with `ErrInvalidParameter`-style semantics, NOT the `DeployGateError`/`DIAGNOSIS_REQUIRED` refuse-redeploy shape. It is not a deadlock once the deploy gate is gone (the resolution — deploy — now succeeds). Karel confirms: reshape vs leave.
- **D3 — loop-guard.** Add a byte-identical-redeploy detector at all? Transcripts show agents self-correct on turn 1; the gate was the only blocker. Likely "no" per "don't backlog plumbing without a consumer." Karel confirms.
- **D4 — unify now or later.** Formalize a single `DestructiveOpGuard` (import-override + future env-delete) now, or leave import-override as the sole gate? `destructive_ack.go:14-17` says scope is intentionally minimal — a "family" may be premature.

## Open questions (flagged, not blocking)

- Is there ANY platform state where a redeploy onto a non-running service IS destructive (e.g. a new push tearing down an in-flight build)? If yes, THAT — not "failed history" — is the real exit-able predicate. Not resolvable from code/docs; a live probe on eval-zcp would settle it.
- launch-production parity: `launchFirstDeployFailedResponse` (`workflow_launch_production.go:724`) is a corpus-independent *response builder*, not the gate — shares NonRunningRecovery lineage (`33fb9358`). Confirm it doesn't carry its own no-exit structure.

## Safe-now (no decision needed; bundle with the implementation)

- Fix stale comment `workflow_checks.go:209` ("READY_TO_DEPLOY with failed history → import override") — code returns `zerops_events`, not override.
- Fix stale comment `dev_server.go:260` ("events / import override") — same drift.
- (Footgun already gone: `NonRunningRecovery` emits only `zerops_events`/`zerops_logs`; the `override=true…WIPED` text is a comment describing the old bug.)
