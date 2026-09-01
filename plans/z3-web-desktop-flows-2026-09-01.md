# Plan: z3-web-desktop-flows-2026-09-01

## Run State
- `phase:` assemble
- `base:` z3/main at `1dae9edba`; preserve the owner's existing dirty desktop/mobile worktree
- `integration:` shared dirty z3 worktree; S13-S15 plus the owner-requested S13 modal follow-up
  implemented and verified, intentionally uncommitted
- `approved:` Rev-1, 2026-09-01 — owner requested autonomous implementation of the three named
  outcomes and explicitly confirmed authorization is per-ZCP, never Settings → Providers
- `codex:` CLEAN — plan `/root/sidebar_architecture`; auth-boundary implementation
  `/root/default_zerops_panel`, 2026-09-01; both review findings fixed and re-reviewed
- `next:` owner review, then explicit release/deploy decision

## Frame
**Outcome**: Web and desktop share one coherent, per-ZCP agent authorization flow, expose no
product shell while the Zerops account is signed out, and make captured multi-service checkpoint
changes inspectable in the UI.

| obs | evidence |
|---|---|
| The agent card currently compresses identity, status and every browser/code action into one row | [SELF-VERIFIED:../z3/apps/web/src/components/zerops/ZeropsAgentAuthCard.tsx:41] |
| Hosted-static is classified as app-gated without consulting the Zerops account session | [SELF-VERIFIED:../z3/apps/web/src/routes/-door.ts:65] |
| The root mounts the app shell, bootstrap and repair before any Zerops account gate | [SELF-VERIFIED:../z3/apps/web/src/routes/__root.tsx:93] |
| Preview/browser hosts currently mount as siblings outside `ZeropsSessionProvider` | [SELF-VERIFIED:../z3/apps/web/src/AppRoot.tsx:19] |
| Turn-diff querying is disabled by the single-repo `/var/www` Git status | [SELF-VERIFIED:../z3/apps/web/src/components/DiffPanel.tsx:236] |
| Checkpoint diff queries currently call one cwd while Zerops target resolution already models every mounted repository | [SELF-VERIFIED:../z3/apps/server/src/checkpointing/CheckpointDiffQuery.ts:132], [SELF-VERIFIED:../z3/apps/server/src/zerops/ZeropsCheckpointTargets.ts:74] |

- AC1: The per-ZCP authorization surface renders exactly the agents in that ZCP's server snapshot;
  each row has recognizable identity, semantic status and a narrow-safe action area. It never adds
  or edits global provider instances under Settings. — planned evidence: focused static-render tests.
- AC2: Active login phases open a focused per-ZCP modal with a clear start → browser → finish
  sequence, fixed controls and the real dedicated server terminal. Codex's device code is prominent
  and copyable, Claude never gets a false device-code affordance, and all existing
  sign-in/cancel/retry callbacks remain unchanged. — planned evidence: phase-table render tests and
  browser interaction/capture.
- AC3: For Zerops session `loading`, `signed-out` and `totp-required`, every ordinary/deep route
  renders only the Zerops sign-in surface. Sidebar, command palette, child outlet and product
  background hosts do not mount; handover stays bare and `signed-in` retains the app. — planned
  evidence: account-gate table, render seams and AppRoot host-boundary tests.
- AC4: The exclusive sign-in surface does not offer manual backend onboarding. — planned evidence:
  landing render tests for exclusive and non-exclusive modes.
- AC5: Saved turn and full-thread checkpoint diffs fan out across Zerops repositories, merge safe
  `<host>/`-prefixed patches, and leave non-Zerops single-repository behavior unchanged. — planned
  evidence: two-repository and upstream query tests plus DiffPanel enablement logic test.
- AC6: Restore confirmation says that newer messages/diffs and tracked/staged working changes will
  be overwritten. — planned evidence: pure copy test and caller assertion.
- AC7: Focused regressions, web typecheck/build and local light/dark agent showcase pass for the
  shared web/desktop implementation. — planned evidence: named commands and inspected captures.

**Non-goals**: `Settings → Providers`; provider-instance creation; static five-agent catalog;
credential revoke/token management; client terminal parsing/manual terminal walker; Git
commit/push/PR UI; async restore receipt; mobile changes; release/deploy.

**Constraints**: preserve the server snapshot as the only roster; preserve every existing login
phase and callback; no contracts/RPC changes in S13; fail closed before mounting routed/background
product surfaces; preserve the authorization handover fragment route; never run Git against the
sshfs mount; keep untracked Zerops files during restore; use existing Zerops tokens/atoms and no
continuous decorative motion; targeted tests/package checks only; do not touch unrelated dirty files.

**Risk class**: medium — trigger: S14 moves a credential/data-exposure mount boundary and S15 spans
the web/server Git read path.

**Assumptions**:
- [VERIFIED] Desktop renders the same hosted-static web bundle, so S13/S14 require no parallel
  desktop component. — [SELF-VERIFIED:../z3/apps/desktop/src/app/DesktopApp.ts:100]
- [VERIFIED] The existing snapshot contains only the real `claude-code` and `codex` states for the
  current ZCP and the card already receives sign-in/cancel callbacks. — [SELF-VERIFIED:../z3/apps/web/src/components/zerops/ZeropsAgentAuthCard.tsx:41]
- [VERIFIED] Existing checkpoint capture and restore already resolve the Zerops repository set;
  the missing behavior is read-time diff fan-out plus UI availability. — [SELF-VERIFIED:../z3/apps/server/src/zerops/ZeropsCheckpointTargets.ts:198]
- [VERIFIED] The per-ZCP authorization card consumes a Zerops agent snapshot and sign-in/cancel
  callbacks, while global provider creation is owned by a separate Settings component; neither
  imports the other. — [SELF-VERIFIED:../z3/apps/web/src/components/zerops/ZeropsAgentAuthCard.tsx:41],
  [SELF-VERIFIED:../z3/apps/web/src/components/settings/ProviderSettingsPanel.tsx:811]

## Evidence Ledger
PROVE skipped — Frame contains zero `[PROBE]` assumptions; all load-bearing claims are source-traced.

## Slice Register
| ID | Title | Depends | Files | Layers | Gate | State |
|---|---|---|---|---|---|---|
| S13 | Per-ZCP branded agent authorization card and focused modal | — | `apps/web/src/components/zerops/ZeropsAgentAuthCard.tsx`; `apps/web/src/components/zerops/ZeropsAgentAuthCard.test.tsx`; `apps/web/src/components/zerops/ZeropsAgentAuthorizationDialog.logic.ts`; `apps/web/src/components/zerops/ZeropsAgentAuthorizationDialog.logic.test.ts`; `apps/web/src/components/zerops/ZeropsAgentAuthorizationDialog.tsx`; `apps/web/src/components/zerops/ZeropsAgentAuthorizationDialog.test.tsx`; `apps/web/src/components/zerops/ZeropsPanel.tsx`; `apps/web/src/components/zerops/ZeropsPanel.test.tsx`; `apps/web/src/zerops/useAgentLogin.ts` | unit, browser | autonomous | verified |
| S14 | Zerops account auth-only mount boundary | S13 | `apps/web/src/routes/-accountGate.ts`; `apps/web/src/routes/-accountGate.test.ts`; `apps/web/src/routes/__root.tsx`; `apps/web/src/AppRoot.tsx`; `apps/web/src/AppRoot.test.tsx`; `apps/web/src/components/zerops/landing/ZeropsHostedLanding.tsx`; `apps/web/src/components/zerops/landing/ZeropsHostedLanding.test.tsx`; `apps/web/src/components/zerops/landing/ZeropsLandingShell.tsx`; `apps/web/src/components/zerops/landing/ZeropsLandingShell.test.tsx` | unit, integration | review | verified |
| S15 | Zerops multi-repository checkpoint diff and truthful restore copy | S14 | `apps/server/src/zerops/ZeropsCheckpointTargets.ts`; `apps/server/src/zerops/ZeropsCheckpointTargets.test.ts`; `apps/server/src/checkpointing/CheckpointDiffQuery.ts`; `apps/server/src/checkpointing/CheckpointDiffQuery.test.ts`; `apps/web/src/components/DiffPanel.tsx`; `apps/web/src/components/DiffPanel.logic.ts`; `apps/web/src/components/DiffPanel.logic.test.ts`; `apps/web/src/components/ChatView.tsx`; `apps/web/src/components/ChatView.logic.ts`; `apps/web/src/components/ChatView.logic.test.ts` | unit, integration | review | verified |

## Verify Trace
| ACx | check | result | evidence |
|---|---|---|---|
| AC1 | `vp test run apps/web/src/components/zerops/ZeropsAgentAuthCard.test.tsx` exact roster/identity/status assertions | pass | 26 tests; snapshot roster only, branded identity, semantic status and responsive actions |
| AC2 | modal phase-table/surface suites and interactive `web:agent-auth-attention` browser smoke | pass | opened Codex awaiting-browser modal from the real card; inspected wide two-column layout, scoped dark terminal canvas, project/ZCP context, device-code actions and fixed footer |
| AC3 | account/door/AppRoot focused suites plus signed-out deep-route browser smoke | pass | real `/zerops/authorized` incl. slash/case; no pre-auth `DocumentTitleSync`; hosted-static remains login-only while authenticated local server pairing is not hidden by the Zerops account gate |
| AC4 | hosted landing and shell focused suites | pass | exclusive hides manual backend; ordinary landing preserves it; 13 tests |
| AC5 | server target/query and web DiffPanel logic suites | pass | server 34 tests; web logic 4 tests; two-repository and upstream fallback covered |
| AC6 | ChatView logic restore-copy assertion | pass | confirmation names tracked/staged working changes; affected logic suite pass |
| AC7 | final affected suites, package typechecks, write-set lint/format, production web build, local hosted smoke | pass | 10 files / 381 tests; web and server typechecks; lint/format/diff-check; hosted production build; browser login interaction |
| — | negative/regression: Settings provider wizard and non-Zerops single-repo flow remain unchanged | pass | no Settings provider file changed; optional repository source preserves single-cwd query tests |

## Promotion
- Contracts → Z3C-10 promoted to `docs/spec-z3.md` §4.1/Invariants at Owner Gate 1. Existing §5.4,
  §6.4 and §8.2 already own S13/S15 contracts.
- Invariants → focused tests named in the three briefs; checkpoint read-time fan-out extends Z3G-7.
- CLAUDE.md trap line: none.
- This plan → delete on LAND per local policy.
