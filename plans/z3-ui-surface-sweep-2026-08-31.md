# Plan: z3-ui-surface-sweep

## Run State
- `phase:` awaiting-retest
- `base:` 538db7578a5fec50b7dc8770ab3fa6f3700663ff
- `integration:` 983927c92; S1 `8d67f114e`, S2 `e346810ce`, S3 `10265340e`, S4 `8aa389653`, S5 `10466d687`, S6 `b2792c06b`, S7 `ed40de5a6`, visual polish `abd1fbd27`, plus merge commits through `983927c92`
- `approved:` Rev-1, 2026-08-31 — owner explicitly instructed implementation of the previously proposed UI-first roadmap
- `codex:` plan CLEAN — `/tmp/codex-review-z3-ui-surface-sweep.md`; final assembly CLEAN — `/tmp/codex-assembly-z3-ui-surface-sweep.md`
- `next:` owner runs `plans/z3-ui-surface-sweep-2026-08-31.retest.md` against commit `983927c92`; do not LAND or deploy before Owner Gate 2

## Frame
**Outcome**: The existing Z3 web app presents a coherent, demo-ready Zerops interface in which the timeline is never obscured, project lifecycle and services are immediately legible, result cards communicate process and outcome, and the project picker feels like a finished product on desktop and narrow web.

| obs | evidence |
|---|---|
| The UI foundation programme is complete, but the production surface redesign never started. | [SELF-VERIFIED:`plans/z3-handoff-2026-08-31.md:25`] and [SELF-VERIFIED:`plans/z3-handoff-2026-08-31.md:37`] |
| The production Zerops surfaces rarely consume the completed primitive set. | [SELF-VERIFIED:`apps/web/src/components/zerops/primitives/index.ts:1`] and repository import search; production consumers are absent outside the showcase renderer. |
| The agent-auth card is mounted in an absolute, heightless overlay over the timeline. | [SELF-VERIFIED:`apps/web/src/components/ChatView.tsx:6820`] plus live capture at 1663×544 on 2026-08-31. |
| The intended UI keeps the existing three-column shell, adds a persistent lifecycle band, and treats the service map as a first-class surface. | [SELF-VERIFIED:`plans/z3-ui-redesign-concept-2026-08-29.md:17`] and [SELF-VERIFIED:`plans/z3-ui-redesign-concept-2026-08-29.md:716`] |
| The live picker and result cards work but retain sparse/ad-hoc anatomy instead of the intended Zerops hierarchy. | Live browser audit on 2026-08-31; source examples [SELF-VERIFIED:`apps/web/src/components/zerops/ZeropsProjectPicker.tsx:48`] and [SELF-VERIFIED:`apps/web/src/components/zerops/ZeropsToolCard.tsx:75`]. |

- AC1: No provider or agent-auth state obscures timeline content at 1440×900 or 390×844; agent authorization is presented once in the service-map tray — planned evidence: targeted chat-chrome/auth render tests and before/after browser captures.
- AC2: A full-width, compact lifecycle band below the thread header uses the existing lifecycle phrase and status primitive and opens the Zerops panel — planned evidence: lifecycle render/interaction tests and desktop/narrow captures.
- AC3: The service map uses the completed primitives to show liveness, compact Runtimes/Data/Infrastructure groups, service state, zcp emphasis, agent authorization and contextual actions without introducing mutations — planned evidence: service-map/panel/quick-action render tests and live-state capture.
- AC4: Recognized Zerops result cards use a process-shell hierarchy with a tonal header, steps and URL/info chips while unknown results still fall back safely — planned evidence: decoder/card render tests for success, failure, partial and unknown cases.
- AC5: The projects surface uses responsive grouped cards with clear status, project identity, readiness/reason and one primary action; provisioning/waiting states explain what is happening — planned evidence: picker/provisioning render tests and desktop/narrow captures.
- AC6: Existing theme, accessibility, reduced-motion and surface guardrails remain intact — planned evidence: focused unit tests, focused lint/type checks for changed files/packages, and light/dark keyboard-responsive browser review.

**Non-goals**: backend credential repair; new platform mutations; Home/Apps/Environments IA; Data Console implementation; native mobile redesign; prod line; integrations; settings redesign; full sidebar data-model repair; deployment to the hosted demo. · **Constraints**: evolve `apps/web`; reuse existing tokens/primitives and canonical presentation rules; preserve total decoders and read-only map semantics; keep unrelated working-tree state untouched; run targeted checks only.

**Risk class**: medium — trigger: owner asked for a broad, parallel multi-surface UI implementation.

**Assumptions**:
- [VERIFIED] The shared tokens, primitives, status resolver and lifecycle phrase producer are complete and are the source of truth — `docs/internals/zerops/design-system.md:19`, `docs/internals/zerops/design-system.md:70`.
- [VERIFIED] The service-map view model already encodes availability, degradation and Runtimes/Data/Infrastructure ordering — `docs/spec-z3.md:808`.
- [VERIFIED] The tool-result decoder is total and unknown results may safely fall back to the ordinary tool block — `docs/spec-z3.md:859`.
- [VERIFIED] The project and auth surfaces already have targeted render/unit tests suitable as regression oracles — `apps/web/src/components/zerops/ZeropsAgentAuthCard.test.tsx:29`, `apps/web/src/components/zerops/ZeropsProjectPicker.test.tsx:1`.
- [ASSUMED] The hosted demo will be released separately after local assembly and owner retest; this run does not mutate the live deployment.

## Evidence Ledger
| claim | gates | surface | command | observed | verdict | promote |
|---|---|---|---|---|---|---|
| PROVE skipped — no load-bearing uncertainty | AC1–AC6 | repo | Frame assumption audit | zero `[PROBE]` claims | CONFIRMED | focused component/render tests |

## Slice Register
| ID | Title | Depends | Files | Layers | Gate | State |
|---|---|---|---|---|---|---|
| S1 | Timeline-safe auth handoff and lifecycle band (tracer) | — | `apps/web/src/components/ChatView.tsx`; `apps/web/src/components/zerops/ZeropsLifecycleStrip.tsx`; `apps/web/src/components/zerops/ZeropsLifecycleStrip.test.tsx`; `apps/web/src/zerops/chatChrome.ts`; `apps/web/src/zerops/chatChrome.test.ts` | unit, render, browser | autonomous | landed |
| S2 | Service map, liveness, zcp emphasis and single agent tray | S1 | `apps/web/src/components/zerops/ZeropsPanel.tsx`; `ZeropsPanel.test.tsx`; `ZeropsServiceMap.tsx`; `ZeropsServiceMap.test.tsx`; `ZeropsQuickActions.tsx`; `ZeropsQuickActions.test.tsx`; `ZeropsAgentAuthCard.tsx`; `ZeropsAgentAuthCard.test.tsx` | unit, render, browser | autonomous | landed |
| S3 | Zerops process-shell result cards | — | `apps/web/src/components/zerops/ZeropsToolCard.tsx`; `apps/web/src/components/zerops/ZeropsToolCard.test.tsx`; `apps/web/src/components/chat/MessagesTimeline.test.tsx` | unit, integration-render, browser | autonomous | landed |
| S4 | Responsive project cards and provisioning hierarchy | — | `apps/web/src/components/zerops/ZeropsProjectPicker.tsx`; `ZeropsProjectPicker.test.tsx`; `ZeropsProjectsPage.tsx`; `ZeropsProjectsPage.test.ts`; `ZeropsProvisioningPanel.tsx`; `ZeropsProvisioningPanel.test.tsx` | unit, interaction, render, browser | autonomous | landed |
| S5 | Timeline-safe provider status banner | S1 | `apps/web/src/components/ChatView.tsx`; focused contract/render test | unit, render, browser | autonomous | landed |
| S6 | Honest asynchronous deploy result state | S3 | `apps/web/src/components/zerops/ZeropsToolCard.tsx`; `ZeropsToolCard.test.tsx` | unit, render | autonomous | landed |
| S7 | Effective identity-exchange retry | S4 | `apps/web/src/components/zerops/ZeropsProjectsPage.tsx`; focused project/provisioning tests | unit, interaction, render | autonomous | landed |

Replay evidence: S1 RED=1 (20 expected assertion failures on base), GREEN=0 (30/30), integrated GREEN=0 (30/30). S2 RED=1 (10 expected assertion failures on the S1 integration base), GREEN=0 (50/50), integrated GREEN=0 (50/50). S3 RED=1 (8 expected assertion failures), GREEN=0 (49/49), integrated GREEN=0 (49/49). S4 RED=1 (9 expected assertion failures), GREEN=0 (39/39), integrated GREEN=0 (39/39). S5 RED=1 (missing flow-layout banner region), GREEN=0 (42/42), integrated GREEN=0 (95/95 fix battery). S6 RED=1 (`BUILD_TRIGGERED` rendered green `Deployed`), GREEN=0 (13/13), integrated GREEN=0 (95/95 fix battery). S7 RED=1 (`retryZeropsProjectConnection is not a function`), GREEN=0 (26/26), integrated GREEN=0 (95/95 fix battery). Visual-polish RED=1 (default 24px service external-link icon), GREEN=0 (14/14).

## Verify Trace
| ACx | check | result | evidence |
|---|---|---|---|
| AC1 | timeline-safe auth/provider states; closed/open panel × auth-state reachability; browser at desktop and 390×844 | passed | final battery: `Test Files  13 passed (13)` / `Tests  186 passed (186)`; provider region contract is `flow` + `shrink-0` and excludes `absolute`, `top-0`, `z-20`; local thread browser captures at 2560×1184 and 390×844 showed no page overflow (`scrollWidth: 390`) |
| AC2 | lifecycle render/interaction test; lifecycle click opens map | passed | final battery: `Test Files  13 passed (13)` / `Tests  186 passed (186)`; lifecycle interaction matrix is included in the focused suite |
| AC3 | panel/service-map/quick-action/auth render and interaction matrix | passed | final battery: `Test Files  13 passed (13)` / `Tests  186 passed (186)`; includes single-tray ownership, liveness/group order, degraded/empty states, zcp emphasis and prompt-only quick actions |
| AC4 | all recognized card kinds plus success/failure/running/partial/undecodable/absent/oversize fallback | passed | final battery: `Test Files  13 passed (13)` / `Tests  186 passed (186)`; `BUILD_TRIGGERED` fixture now asserts busy `Build triggered` + running step, while terminal deploy success remains green |
| AC5 | picker/provisioning matrices; callback-once, busy-disable, reverse/error and identity-retry behavior; browser desktop/narrow | passed | final battery: `Test Files  13 passed (13)` / `Tests  186 passed (186)`; ready-phase identity retry calls exchange once and not provisioning retry; local `/zerops` captures at desktop and 390×844 showed responsive scope header and no horizontal overflow (`scrollWidth: 390`) |
| AC6 | focused typecheck/lint; architecture and CSS/theme guards; build; light/dark/reduced-motion browser review | passed | `$ tsgo --noEmit`; focused lint and `git diff --check 538db757…HEAD` passed with no output; `Test Files  1 passed (1)` / `Tests  33 passed (33)`; guard pass lines: `no-infinite-motion: ledger reconciled (5 entries)`, `no-infinite-motion: ledger reconciled (19 entries)`, `no-legacy-vocabulary: ledger reconciled (93 entries)`, `no-theme-escape-hatches: ledger reconciled (35 entries)`, `no-theme-escape-hatches: ledger reconciled (410 entries)`, `Theme token projections are current.`; production build: `✓ built in 11.11s`; browser confirmed real light/dark themes and reduced-motion at desktop/390px |
| Regression | existing non-Zerops thread shell, generic tool fallback and theme/layout guardrails | passed | showcase + generic timeline tests are included in `186 passed (186)`; browser reviewed a populated ordinary thread in light/dark at desktop and 390×844; hosted demo was restored to its original URL and viewport after QA |

## Promotion
- Contracts → extend `docs/spec-z3.md` §5.4 with the approved lifecycle-band, single panel-owned agent-auth tray, service-row and process-card presentation contract; no public wire/schema change
- Invariants → focused component/render tests; no new platform invariant
- CLAUDE.md trap line (≤1): none
- This plan → `plans/archive/` on LAND close
