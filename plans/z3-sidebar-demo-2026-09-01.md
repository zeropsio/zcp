# Plan: z3-sidebar-demo-2026-09-01

## Run State
- `phase:` awaiting-retest
- `base:` 84269cf3338dce2b80c8fb46efb3aa4fbc620a5c
- `integration:` `5edff43f7b13f0af14873cc9f5592750eb57787f` on `z3/main`; deployed to `z3-eval/z3web` as `z3-v0.1.5-ui`; tag `v0.1.5`
- `approved:` Rev-1, 2026-09-01 — owner requested the plan and immediate implementation in this turn
- `codex:` manual Codex review required during ASSEMBLE
- `next:` owner retest of `https://z3.krls.cz`; stale test-project credentials can be refreshed by visiting `/zerops`, while durable credential recovery remains a separate P0 backlog item

## Frame
**Outcome**: the hosted z3 demo has a legible project/environment/thread sidebar, exposes the
Zerops project panel without discovery work on desktop, and makes every agent-blocking question or
approval visually unmistakable.

| obs | evidence |
|---|---|
| The live sidebar flattens every thread into repeated `www` rows and discards the project/member hierarchy already built in memory. | live capture 2026-09-01; `z3/apps/web/src/components/Sidebar.tsx:1786-1825,1955-2055,3590-3863` |
| The sidebar view model already contains logical groups, physical member projects, environment ids and labels. | `z3/apps/web/src/sidebarProjectGrouping.ts:6-20,40-89` |
| Topology can truthfully supply the Zerops project name, but neither Zerops tag group nor production-role metadata is in the client contract. | `z3/packages/contracts/src/zerops.ts:100-135` |
| The service map is first-class in the accepted concept but its persisted per-thread panel starts closed and currently opens only on click. | `plans/z3-ui-redesign-concept-2026-08-29.md:17-19,33-36`; `z3/apps/web/src/rightPanelStore.ts:41-89`; `z3/apps/web/src/components/zerops/ZeropsLifecycleStrip.tsx:102-130` |
| Questions preserve strong interaction behavior, while approval content/actions are compressed and all actions use the same muted button treatment. | `z3/apps/web/src/components/chat/ComposerPendingUserInputPanel.tsx:222-367`; `z3/apps/web/src/components/chat/ComposerPendingApprovalPanel.tsx:16-57`; `z3/apps/web/src/components/chat/ComposerPendingApprovalActions.tsx:19-52` |

- AC1: Outside search, visible threads render beneath their logical project and exact physical environment/workspace; generic repeated `www` labels disappear when a topology project name is available. Project and environment sections are collapsible, active ancestry remains visible, and existing thread actions/status/drag semantics survive — planned evidence: pure hierarchy tests + focused Sidebar tests + desktop/mobile browser capture.
- AC2: No UI invents tag-group or production identity. Missing topology/project name falls back to existing logical/environment labels; search stays a flat keyboard-navigable result mode — planned evidence: fallback/negative tests and source audit.
- AC3: On a viewport wider than 980px, available Zerops topology opens `zerops` once only for an untouched scoped thread; it never replaces another active surface, survives reload, respects explicit close/tab removal, and never auto-opens on narrow web or unavailable/unknown topology — planned evidence: pure policy/store migration tests + focused ChatView test + browser reload/viewport scenario.
- AC4: Pending question and approval surfaces visibly say `WAITING FOR YOU`, show a human request kind and complete detail/progress, wrap at 390px, and preserve every advertised option and existing keyboard/response behavior. `accept` is primary when present, `acceptForSession` secondary, decline/cancel quiet — planned evidence: focused component interaction/render tests + 390px/light/dark browser capture.
- AC5: Targeted web tests, web typecheck, focused lint/format, architecture/style guardrails, and `git diff --check` pass; the deployed `z3web` health check and live desktop/mobile smoke pass — planned evidence: ASSEMBLE verification log and owner/browser retest.

**Non-goals**: add Zerops tag-group API metadata; infer production from names; change server/contracts/client-runtime RPC; consolidate `/` and `/zerops`; native mobile; bundle splitting; redesign thread business actions. · **Constraints**: preserve the z3 hard-fork policy and current routes; use only existing tokens/primitives; no fabricated identity; wide-only panel default; targeted tests only; release/deploy only after the integrated build is green.

**Risk class**: medium — trigger: sidebar interaction and persisted panel-state changes cross primary demo navigation.

**Assumptions**:
- [VERIFIED] Existing logical grouping is the only truthful hierarchy available without a contract change — `z3/apps/web/src/sidebarProjectGrouping.ts:40-89`.
- [VERIFIED] `ZeropsTopologySnapshot.project.name` is optional and feed availability is explicit — `z3/packages/contracts/src/zerops.ts:100-135`.
- [VERIFIED] The panel store is already scoped and persisted per thread — `z3/apps/web/src/rightPanelStore.ts:10-16,143-157,550-558`.
- [ASSUMED] A 980px breakpoint remains the correct wide/narrow boundary because the current right-panel layout owns that breakpoint.

## Evidence Ledger
| claim | gates | surface | command | observed | verdict | promote |
|---|---|---|---|---|---|---|
| Current contracts cannot identify a true tag group or prod lane. | AC2 | repo | `rg -n "ZeropsProject|tag|production" packages/contracts/src/zerops.ts` | project has id/name/status only | CONFIRMED | `docs/spec-z3.md` §5.4 |
| Existing fold logic already preserves Zerops milestone cards. | scope | repo | inspect `MessagesTimeline.logic.ts:610,749,936-957,989` and tests | old backlog item is stale | CONFIRMED | remove during backlog reconciliation |
| Wide panel default can be isolated from mobile sheet behavior. | AC3 | repo | inspect `rightPanelLayout.ts` and `ChatView.tsx:7133-7185` | breakpoint and sheet/inline branches exist | CONFIRMED | pure policy test + spec §5.4 |

## Slice Register
| ID | Title | Depends | Files | Layers | Gate | State |
|---|---|---|---|---|---|---|
| S1 | Project → environment → thread sidebar tree | — | `apps/web/src/sidebarProjectGrouping.ts`, `.test.ts`, `components/Sidebar.tsx`, focused sidebar component/logic files and tests | unit/component/browser | review | landed `54e964ad3`, `ff97bd2c8`, `b8186de84` |
| S2 | Default-open remembered Zerops panel | — | `apps/web/src/rightPanelStore.ts`, `.test.ts`, `zerops/defaultPanel.ts`, `.test.ts`, `components/ChatView.tsx`, focused test | unit/store/component/browser | review | landed `d39847e68`, `1b93f178f` |
| S3 | Coherent Waiting-for-you question/approval surface | — | `apps/web/src/components/chat/ComposerPendingUserInputPanel.tsx`, `.test.tsx`, `ComposerPendingApprovalPanel.tsx`, `.test.tsx`, `ComposerPendingApprovalActions.tsx`, `.test.tsx`; `ChatComposer.tsx` only if shared wrapper is required | component/interaction/browser | review | landed `694ebcf7f` |
| S4 | Coalesce generic Zerops roots | S1 | `components/sidebar/SidebarProjectTree.tsx`, `.test.tsx` | component/browser | review | landed `ccd6d3e5c` |
| S5 | Mobile overlay and provider-dialog reachability | — | `components/Sidebar.tsx`, `Sidebar.logic.ts`, `RightPanelSheet.tsx`, `settings/AddProviderInstanceDialog.tsx`, focused tests | unit/component | review | landed `2db39c33c`, `327ab15a0` |
| S6 | Recoverable file-browser loading | — | `components/files/FileBrowserPanel.tsx`, `FileBrowserPanelState.tsx`, focused tests | unit/component | review | landed `7a4a02cf7` |

## Verify Trace
| ACx | check | result | evidence |
|---|---|---|---|
| AC1/AC2 | hierarchy pure/component tests + live desktop | pass | coalescing test 5/5; final live smoke shows exactly one `Projects` root, `2 workspaces`, `zerops-xyz`/`zerops-code`, service metadata and nested threads |
| AC3 | policy/store/component tests + reload live scenario | pass | live untouched thread opened Zerops; explicit close survived reload and an existing file tab was not replaced |
| AC4 | question/approval tests | pass with browser fixture gap | integrated focused web suite passed; no safe live pending request existed to capture |
| AC5 | target tests, typecheck, guardrails, health + smoke | pass | 14 files / 464 web tests; 49 client-runtime auth tests; 62 architecture/token tests; web typecheck, lint, format, theme check, production build, `z3web` deploy, GitHub CI and `v0.1.5` release workflow passed |
| — | negative/regression: no fake tag/prod labels and no auto-open mobile/override | pass | explicit fallback and panel-policy tests; live fallback keeps unavailable topology honest |

## Promotion
- Contracts → `docs/spec-z3.md` §5.4
- Invariants → focused hierarchy, default-panel, question and approval tests + spec §5.4
- CLAUDE.md trap line (≤1): none
- This plan → `plans/archive/` on LAND close
