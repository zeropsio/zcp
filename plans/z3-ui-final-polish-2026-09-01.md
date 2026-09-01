# Plan: z3-ui-final-polish-2026-09-01

## Run State
- `phase:` ship
- `base:` e27216b0af9b07c64e6857dfba33cf37dba06a66
- `integration:` z3/main at `1dae9edba`; S10-S12 integrated; `z3-v0.1.7-ui` deployed to `z3-eval/z3web`; GitHub `v0.1.7` published
- `approved:` Rev-1, 2026-09-01 — owner approved the six-item register with “ok, udelej to”
- `codex:` clean — primary Codex review; three bounded web-only slices, no contract or mutation expansion
- `next:` owner retest; archive only after confirmation

## Frame
**Outcome**: The final low-risk web polish pass makes workspace creation, Zerops naming, agent
sign-in, composer copy, provider identity and wide-panel density read as one coherent UI.

| obs | evidence |
|---|---|
| A workspace shortcut is rendered only when an untouched thread already exists | [SELF-VERIFIED:../z3/apps/web/src/components/sidebar/SidebarProjectTree.tsx:209] and live browser snapshot, 2026-09-01 |
| The live header uses `activeProject.title` while the sidebar can use the topology project name | [SELF-VERIFIED:../z3/apps/web/src/components/ChatView.tsx:6747], [SELF-VERIFIED:../z3/apps/web/src/components/sidebar/SidebarProjectTree.tsx:198] |
| The active topology is already available in ChatView | [SELF-VERIFIED:../z3/apps/web/src/components/ChatView.tsx:3377] |
| Connected composer copy advertises advanced syntax before the plain task | [SELF-VERIFIED:../z3/apps/web/src/components/chat/ChatComposer.tsx:3422] |
| Awaiting-browser auth actions are one non-wrapping horizontal row | [SELF-VERIFIED:../z3/apps/web/src/components/zerops/ZeropsAgentAuthCard.tsx:105] |
| Provider branding keeps its accent colour in every resting thread card | [SELF-VERIFIED:../z3/apps/web/src/components/Sidebar.tsx:1544] |
| The right-panel content expands to the full surface width | [SELF-VERIFIED:../z3/apps/web/src/components/zerops/ZeropsPanel.tsx:52] |

- AC1: Every workspace row has one named new-thread action. It opens an existing untouched shell
  when present and otherwise creates a draft for that exact workspace; it never creates a second
  shell while one exists. — planned evidence: Sidebar tree and action-resolution unit tests plus
  live two-workspace browser drive.
- AC2: A non-empty active Zerops topology project name is used across the thread header, draft hero,
  file panel and active composer environment label; the existing local labels remain exact
  fallbacks when the feed is absent/unavailable. — planned evidence: chat-chrome projection tests
  and live breadcrumb/context-strip inspection.
- AC3: A connected Zerops thread uses one plain-language composer placeholder while every higher
  priority approval/question/disconnected state keeps its current copy. — planned evidence:
  placeholder resolver tests and live composer inspection.
- AC4: Agent sign-in rows wrap safely, expose the device code as code, keep Open as the primary
  action and Cancel secondary, and retain every existing handler/state. — planned evidence:
  static render/action tests at narrow-safe class seams.
- AC5: Provider marks are visually muted at rest and regain their brand on row interaction;
  semantic thread status remains the only urgent colour. — planned evidence: sidebar presentation
  helper test and live card capture.
- AC6: Zerops panel content has a readable centered maximum width while remaining full-width on
  narrow panels. — planned evidence: panel render test and wide live capture.
- AC7: Search, collapse, DnD, rename/context-menu, agent login, non-Zerops chrome, right-panel
  scrolling and advanced composer syntax remain functional. — planned evidence: focused regression
  battery, typecheck and production browser smoke.

**Non-goals**: credential recovery; tag/prod data; wire contracts; onboarding routes; bundle work;
desktop/mobile surfaces. · **Constraints**: web-only; no inferred Zerops identity; tokens only;
no continuous motion; untouched means both activity signals absent.

**Risk class**: low — trigger: owner asked for the six-item multi-seam polish register.

**Assumptions**:
- [VERIFIED] The workspace member carries the exact environment/project ids required by the existing
  new-thread handler. — `sidebarProjectGrouping.ts:40-47`, `lib/chatThreadActions.ts:10-19`
- [VERIFIED] Topology project name is presentation data already consumed by the sidebar and may
  truthfully replace a generic label. — `SidebarProjectTree.tsx:192-200`, spec-z3 §5.4
- [VERIFIED] All six changes are reversible client presentation/wiring changes; no live platform
  mutation or public schema change is required. — write-set inspection above

## Evidence Ledger
PROVE skipped — no load-bearing uncertainty.

## Slice Register
| ID | Title | Depends | Files | Layers | Gate | State |
|---|---|---|---|---|---|---|
| S10 | Always-available workspace thread action | — | `Sidebar.logic.ts`, `Sidebar.tsx`, `sidebar/SidebarProjectTree.tsx`, focused tests | unit | autonomous | done |
| S11 | Consistent Zerops chrome naming and composer copy | S10 | `zerops/chatChrome.ts`, `ChatView.tsx`, `chat/ChatComposer.tsx`, `composerPlaceholder.ts`, focused tests | unit | autonomous | done |
| S12 | Responsive auth, quiet provider identity and readable panel width | S11 | `zerops/ZeropsAgentAuthCard.tsx`, `zerops/ZeropsPanel.tsx`, `Sidebar.logic.ts`, `Sidebar.tsx`, focused tests | unit | autonomous | done |

## Verify Trace
| ACx | check | result | evidence |
|---|---|---|---|
| AC1 | Sidebar action tests + live reuse/create paths | pass | 209-test focused web battery; live browser shows one named action in both hosted workspaces |
| AC2 | chatChrome tests + live header/context labels | pass with live connected-state limitation | projection tests cover available/unavailable naming; live hosted credential was invalid and truthfully exercised the raw-label fallback |
| AC3 | composer placeholder tests + live text | pass with live connected-state limitation | resolver tests cover connected Zerops copy; live browser confirmed disconnected copy retains priority |
| AC4 | agent-auth render/action tests | pass | static render/action suite preserves every login state and handler, with wrapping controls and persistent device code |
| AC5 | sidebar presentation test + live capture | pass | muted/restored provider presentation test plus deployed desktop capture |
| AC6 | ZeropsPanel render test + wide capture | pass | deployed panel measured `max-width: 768px`, `padding: 16px` |
| AC7 | focused regressions, typecheck, guards, build, deploy smoke | pass | 209 web tests, 62 architecture/CSS guards, typecheck, focused lint, theme check, production build, release workflow and CI |
| — | negative/regression: non-Zerops fallbacks and extra untouched shells remain visible | pass | live disconnected fallback plus tree unit coverage |

## Promotion
- Contracts → `docs/spec-z3.md` §5.4
- Invariants → focused web tests cited in §5.4
- CLAUDE.md trap line: none
- This plan → awaiting owner retest; archive only after confirmation
- Hosted credential recovery remains the existing P0 in `plans/z3-ui-backlog-2026-08-31.md`; this
  presentation pass did not mutate credentials or hide the failure.
