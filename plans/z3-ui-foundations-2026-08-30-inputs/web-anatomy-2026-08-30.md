# z3 web client anatomy — `/Users/macbook/Documents/Zerops-MCP/z3` @ `main` (6b1b575d3)

## 1. Layout of `apps/web/src`

Totals (`.ts` + `.tsx`, tests included): **883 files / 211,148 lines**.

| Dir | files | lines |
|---|---|---|
| `components/` | 472 | 132,713 |
| `zerops/` | 39 | 5,648 |
| `terminal/` | 10 | 5,628 |
| `browser/` | 36 | 5,274 |
| `hooks/` | 24 | 4,219 |
| `routes/` | 26 | 3,404 |
| `state/` | 35 | 2,414 |
| `connection/` | 9 | 1,503 |
| `environments/` | 9 | 1,350 |
| `cloud/` | 11 | 1,336 |
| `lib/` | 62 | 7,234 |
| `rpc/` 290 · `observability/` 147 · `test/` 116 · `assets/` 74 | | |
| loose root files | ~145 | 39,798 |

`components/` sub-splits (direct files only): `settings/` 24,183 · `chat/` 22,564 · `pullRequest/` 14,066 · `preview/` 6,691 · `ui/` 6,626 · **`zerops/` 2,723** · `files/` 2,798 · `usage/` 1,486 · `sidebar/` 960 · `diffs/` 983 · `search/` 504 · `auth/` 367 · `clerk/` 332 · `cloud/` 322.

Top 15 files by lines:

1. 7417 `components/ChatView.tsx`
2. 3960 `components/Sidebar.tsx`
3. 3810 `composerDraftStore.ts`
4. 3600 `components/LegacySidebar.tsx`
5. 3599 `components/chat/ChatComposer.tsx`
6. 2732 `components/settings/SettingsPanels.tsx`
7. 2728 `components/chat/MessagesTimeline.tsx`
8. 2323 `session-logic.test.ts`
9. 2281 `components/CommandPalette.tsx`
10. 2262 `components/ChatMarkdown.tsx`
11. 2053 `components/GitActionsControl.tsx`
12. 2037 `components/pullRequest/PullRequestDetailPanel.tsx`
13. 1976 `themePalette.ts`
14. 1969 `routes/_chat.pull-requests.tsx`
15. 1920 `session-logic.ts`

## 2. The owned-product Zerops zone

Two directories, both listed as "Owned product" in `CLAUDE.md:29`: `apps/web/src/zerops/**` (logic, 39 files / 5,648 lines) and `apps/web/src/components/zerops/**` (views, 2,723 lines). Total **9,065 lines across 60 files**.

**`apps/web/src/zerops/` — logic (all `.ts` except two)**

| File | L | Purpose |
|---|---|---|
| `cards/payloads.ts` | 327 | one decoder per Zerops card + registry; each written against a Go response struct |
| `provisioning.ts` | 309 | pure state machine for the pool-claim → container-answers wait |
| `candidates.ts` | 228 | picker model: every zcp container the account can reach, grouped connected/ready/unavailable |
| `agentLogin.ts` | 207 | agent-CLI login presentation (server-driven, S7 F8) |
| `serviceMap.ts` | 181 | service-map presentation model derived from the two feeds |
| `useZeropsCandidates.ts` | 165 | fetching shell around `deriveZeropsCandidates`, fans out per org |
| `ZeropsSessionProvider.tsx` | 164 | signed-in Zerops account, outside the router |
| `turnstile.tsx` | 151 | Cloudflare Turnstile on the sign-up form |
| `feeds.ts` | 138 | the three server feeds as atoms (topology / lifecycle / agent-auth) |
| `containerHealth.ts` | 131 | two header-less probes: is Zerops Code on this container |
| `strip.ts` | 130 | lifecycle-strip wording projected from the envelope |
| `useZeropsProvisioning.ts` | 128 | clock driving `provisioning.ts` |
| `firstPrompt.ts` | 91 | the first thing said in a newly connected environment |
| `cards/decode.ts` | 84 | reads a `zerops_*` tool result into card-renderable shape |
| `quickActions.ts` | 78 | which quick actions the project state makes sensible (prompts, never calls) |
| `useZeropsCandidateHealth.ts` | 62 | per-row container probe for the picker |
| `useZeropsFeeds.ts` | 54 | component-facing reads of the feeds |
| `useAgentLogin.ts` | 54 | fires the agent-login RPC |
| `activityResult.ts` | 53 | reads the `zerops_*` result off an activity payload |
| `useAgentLoginCancel.ts` | 38 | cancel RPC |
| `composeFirstPrompt.ts` | 36 | puts the opening message in the new environment's draft |
| `storage.ts` | 33 | `localStorage` behind the runtime's async storage contract |
| `registrationHandoff.ts` | 31 | org + pool-claim state for `provisioning.start` |
| `autoEnterProvisioning.ts` | 31 | whether to enter the wait automatically after sign-in |
| `cards/liveFixtures.ts` | 24 | byte-exact tool results captured from `z3-eval` 2026-08-28 |

**`apps/web/src/components/zerops/` — views (all `.tsx`)**

| File | L | Purpose |
|---|---|---|
| `landing/ZeropsLandingShell.tsx` | 355 | presentational entry frame: sign-in / sign-up / 2FA forms |
| `ZeropsProjectsPage.tsx` | 315 | `/zerops` picker + "New project" path |
| `ZeropsAgentAuthCard.tsx` | 280 | one row per agent CLI with its authorization state |
| `ZeropsProjectPicker.tsx` | 241 | renders the grouped candidates |
| `ZeropsToolCard.tsx` | 233 | one card per `zerops_*` tool result |
| `landing/ZeropsHostedLanding.tsx` | 191 | hosted-client landing; keeps upstream's manual connect as fallback |
| `ZeropsServiceMap.tsx` | 149 | presentational service map |
| `ZeropsProvisioningPanel.tsx` | 133 | "Preparing your project" |
| `ZeropsLifecycleStrip.tsx` | 90 | exports `ZeropsStripLine` + the connected strip |
| `ZeropsPanel.tsx` | 79 | the right-panel Zerops surface (map + quick actions + auth card) |
| `ZeropsQuickActions.tsx` | 55 | one click prefills the composer |

Plus outside both dirs: `components/settings/ZeropsSettings.tsx` (78), `state/zerops.ts` (4, the feed-atoms handle), `routes/zerops.tsx` (7), `routes/settings.zerops.tsx` (7).

**React vs pure TS.** The distinction is `.ts` vs `.tsx`, not the import (JSX automatic runtime — several `.tsx` never name React). Pure `.ts` logic files: all of `zerops/` except `ZeropsSessionProvider.tsx` and `turnstile.tsx`. React-hook `.ts` files that import `react`: `useAgentLogin.ts:14`, `useAgentLoginCancel.ts:14`, `useZeropsCandidateHealth.ts:10`, `useZeropsCandidates.ts:12`, `useZeropsProvisioning.ts:9`. Every file in `components/zerops/` is a component.

**Import direction.** Files reaching `@t3tools/client-runtime`: `ZeropsSessionProvider.tsx:26` (`/zerops`), `candidates.ts:17`, `provisioning.ts:19`, `registrationHandoff.ts:15`, `storage.ts:1`, `feeds.ts:27,31` (`/connection`, `/state/runtime`), `agentLogin.ts:14` (`/state/runtime`), `ZeropsPanel.tsx:8` + `ZeropsLifecycleStrip.tsx:13` (`/environment`), `ZeropsHostedLanding.tsx:7`.
Files reaching `apps/web` internals outside `zerops/`: `ZeropsProjectsPage.tsx:10-21` (`connection/onboarding`, `env`, `state/use-atom-command`, five `ui/*`, three `Workspace*`), `useAgentLogin.ts:16-18` + `useAgentLoginCancel.ts:16-17` (`state/zerops`, `state/use-atom-command`, `terminalUiStateStore`), `useZeropsFeeds.ts:20` (`state/zerops`), `useZeropsCandidates.ts:16` (`state/environments`), `composeFirstPrompt.ts:9` (`composerDraftStore`), `ZeropsQuickActions.tsx:10` (`composerHandleContext`), `ZeropsLifecycleStrip.tsx:19` (`rightPanelStore`), plus `ui/*` and `lib/utils` in the views.

## 3. Theme engine

`packages/shared/src/themePalettes.ts` (748 lines) is the single definition consumed by web, mobile, and host tooling.

- `THEME_COLOR_ROLES` (`:112-185`) — **57 roles**, from `canvas`/`chrome` through toolbar, surface, message, code, sidebar and terminal families.
- `ThemeDefinition` (`:81-93`): `id`, `label`, `appearance` (`"light" | "dark"`), `colors: ThemeColors` (all 57), optional `variants` (per-appearance override), `collection`, `sidebarArtwork`, `managed`.
- Built-ins (`BUILT_IN_THEME_IDS:95`, `BUILT_IN_THEMES:730`): **`t3-chat`, `grove`, `ocean`, `ember`, `iris`** — five, each light-primary with a `dark` variant, all `sidebarArtwork: true`. `MOBILE_DEFAULT_THEME_ID = "t3-code"` (`:98`) is mobile-only and not in the library.
- `getThemeColorsForAppearance` (`:742`) resolves `colors` vs `variants[appearance]`.

Default selection: `apps/web/src/themePalette.ts:1224` — `getDefaultThemeColors(appearance)` returns `T3_CHAT_THEME.variants!.dark!` for dark, `T3_CHAT_THEME.colors` for light, i.e. the flagship palette is the default. `apps/web/index.html:37-50` carries a hand-copied boot-time `DEFAULT_THEME_PALETTES` (4 roles: background/foreground/accent/chrome, light+dark), read at `index.html:332` to complete a partial stored custom theme before React mounts; a second copy `BUILT_IN_THEME_PALETTES` (`index.html:52+`) does the same for the five built-ins. Both are documented as needing manual sync with `themePalettes.ts`.

`apps/web/src/index.css` (2,523 lines):
- `@import "tailwindcss"` at `:1`; `@theme` font tokens `:139`; `@theme inline` `:145`.
- First `:root` at `:79` — geometry/scrollbar/glass/workspace vars, with its dark overrides in a nested `@variant dark { }` at `:115`.
- Second `:root` at `:1388` — `color-scheme: light` plus the semantic palette (`--background`, `--foreground`, `--toolbar-*`, `--terminal-*`), dark overrides at `@variant dark` `:1457` and `:1521`.
- Per-theme blocks `html[data-theme-id="t3-chat"]` `:578`, `grove` `:598`, `ocean` `:632`, `ember` `:652`, `iris` `:686`, each with its own nested `@variant dark`; a combined selector at `:706`/`:729`.
- The remap: `html[data-theme-id]:not([data-theme-id=""])` at `:1552` rebinds every semantic var to the `--app-theme-*` custom properties. Comment there explains the non-empty marker exists purely to outrank the generated root dark variant without reintroducing raw `.dark` selectors.
- Runtime application: `themePalette.ts:1767+` maps each role to its `--app-theme-*` name; `applyThemePalette` (`:1854`) sets `root.dataset.themeId` (`:1864`) or deletes it (`:1873`); `applyThemeColorPreview` (`:1838`) does the same under a preview id. `hooks/useTheme.ts:314` toggles the `dark` class.

Mobile consumes the same definition: `apps/mobile/src/lib/mobileTheme.ts:2-14` imports `BUILT_IN_THEMES` / `ThemeColors` from `@t3tools/shared/themePalettes` (+ `themePreview`); `createMobileThemeVariables(colors, appearance)` at `:208` translates the 57 roles into React-Native `--color-*` uniwind variables (e.g. `--color-screen: c.canvas`, `--color-sheet: withAlpha(c.chrome, 0.98)`). `apps/mobile/scripts/generate-uniwind-themes.mts:6` imports `BUILT_IN_THEME_IDS` and is wired as the `generate` script (`apps/mobile/package.json:42`).

## 4. `apps/web/src/components/ui/*`

52 entries, 6,626 lines. By size: `sidebar.tsx` 1043, `toast.tsx` 811, `combobox.tsx` 403, `menu.tsx` 310, `autocomplete.tsx` 271, `command.tsx` 261, `select.tsx` 242, `sheet.tsx` 200, `card.tsx` 196, `dialog.tsx` 183, `number-field.tsx` 156, `alert-dialog.tsx` 146, `alert.tsx` 140, `empty.tsx` 114, `popover.tsx` 113, `input-group.tsx` 113, `toggle-group.tsx` 98, `group.tsx` 93, `scroll-area.tsx` 91, `table.tsx` 87, `button.tsx` 84, `qr-code.tsx` 81, `input.tsx` 77, `tooltip.tsx` 64, `checkbox.tsx` 60, `field.tsx` 59, `badge.tsx` 56, `toggle.tsx` 54, `textarea.tsx` 45, `panel-tab-close-button.tsx` 41, `collapsible.tsx` 39, `radio-group.tsx` 36, `kbd.tsx` 28, `switch.tsx` 27, `fieldset.tsx` 26, `label.tsx` 24, `draft-input.tsx` 21, `separator.tsx` 19, `form.tsx` 17, `skeleton.tsx` 16, `spinner.tsx` 15, plus logic/style files (`toast.logic.ts` 131, `toastHelpers.ts` 66, `dialog-styles.ts` 10, `sidebarState.ts` 9).

**Yes, a `cva` variant system** — `class-variance-authority` (`button.tsx:5`, `const buttonVariants = cva(` at `:10`). Nine files use it: `button`, `badge`, `alert`, `select`, `sidebar`, `toggle`, `group`, `input-group`, `empty`.

Used by the Zerops zone (8 primitives, 25 import sites): `spinner` ×7, `button` ×6, `badge` ×3, `sidebar` (`SidebarInset`) ×2, `scroll-area` ×2, `label` ×2, `input` ×2, `tooltip` ×1. Nothing in `zerops/**` uses card, dialog, sheet, menu, select, table, toast, or the cva variant props beyond `Button`/`Badge` defaults.

## 5. Right panel

Kind union declared in `apps/web/src/rightPanelStore.ts:17-26` as `RIGHT_PANEL_KINDS` (const tuple) → `RightPanelKind` at `:27`. **Eight kinds**: `diff`, `files`, `file`, `preview`, `terminal`, `pull-request`, `agents`, `zerops`. The discriminated `RightPanelSurface` union follows at `:29-67` (`{ id: "zerops"; kind: "zerops" }` at `:67`). Store is zustand + persist, key `t3code:right-panel-state:v2` (`:69`); a v9 migration removed the old `plan` kind (`:70`).

Breakpoint: `apps/web/src/rightPanelLayout.ts` is a 3-line file — `RIGHT_PANEL_INLINE_LAYOUT_MEDIA_QUERY = "(max-width: 980px)"` (`:1`) and `RIGHT_PANEL_SHEET_CLASS_NAME` (`:2`). There is no `shouldUseRightPanelSheet` export; it is a local in `ChatView.tsx:1473` (`useMediaQuery(RIGHT_PANEL_INLINE_LAYOUT_MEDIA_QUERY)`).

**Six consuming sites in `ChatView.tsx`**: `:1758` (`canMaximizeRightPanel`), `:1761` (`inlineRightPanelOwnsTitleBar`), `:6742`, `:6877` (panel layout controls), `:7319` (inline panel), `:7356` (sheet). Tab plumbing lives in `components/RightPanelTabs.tsx` (986 lines) — `zeropsAvailable` prop at `:85`/`:267`/`:977`, hint strings at `:107` ("The Zerops project map is only available from a thread.") and `:130` ("Available in a Zerops project."), switch arms at `:528`/`:615`. `components/RightPanelSheet.tsx` is 30 lines.

## 6. Sidebar

| File | L |
|---|---|
| `components/Sidebar.tsx` | 3960 |
| `components/LegacySidebar.tsx` | 3600 |
| `components/Sidebar.logic.ts` | 969 |
| `components/ThreadStatusIndicators.tsx` | 651 |
| `components/sidebar/SidebarChrome.tsx` | 250 |
| `components/sidebar/SidebarUpdatePill.tsx` | 374 · `SidebarProviderUpdatePill.tsx` 210 · `DesktopUpdateStatusIcon.tsx` 126 |
| `components/ui/sidebar.tsx` (primitive) | 1043 |
| tests: `Sidebar.logic.test.ts` 1712 · `ThreadStatusIndicators.test.ts` 603 · `.test.tsx` 39 | |

Two sidebar implementations coexist (`Sidebar.tsx` is v2, `LegacySidebar.tsx` v1; both import `resolveThreadStatusPill` from `Sidebar.logic.ts`).

**Thread-row status phrase.** Two separate renderers:
- **v2 sidebar rows**: `SidebarThreadRow` (`Sidebar.tsx:696`) calls `resolveSidebarThreadStatus(thread)` at `:817`, then builds a local `topStatus` object at `:848-897` with the literal labels `Working` / `Monitoring` / `Approval` / `Input` / `Failed` / `Woke` / `Done` and hard-coded hue classes; the phrase is rendered as `<span role="status">{topStatus.label}</span>` at `:1468` and `:1489`.
- **Shared pill** (command palette, legacy sidebar): `ThreadStatusPill` type at `Sidebar.logic.ts:128-141` with labels `Working | Monitoring | Connecting | Completed | Pending Approval | Awaiting Input | Plan Ready`, priority table at `:145`, resolver `resolveThreadStatusPill` at `:653`; rendered by `ThreadStatusLabel` (`ThreadStatusIndicators.tsx:466`), whose visible text is `<span className="hidden md:inline">{status.label}</span>` (~`:508`), mounted from `ThreadRowTrailingStatus` at `:587`.

The two label vocabularies differ — v2's inline labels are not the shared pill's.

## 7. Routes

`apps/web/src/routes/` — 26 files, 3,404 lines: `__root.tsx` 466, `_chat.pull-requests.tsx` 1969, `_chat.index.tsx` 200, `_chat.tsx` 196, `settings.tsx` 111, `_chat.draft.$draftId.tsx` 97, `_chat.$environmentId.$threadId.tsx` 97, `pair.tsx` 54, `projects.$projectKey.tsx` 15, `connect.tsx` 13, `connect_.callback.tsx` 13, eight 7–11-line `settings.*.tsx` leaves, `usage.tsx` 7, **`zerops.tsx` 7**, **`settings.zerops.tsx` 7**, plus `-chatIndexView.ts` 18 and three `-*.test.ts`, and the generated `routeTree.gen.ts` (518, at `src/` root).

**The auth-gate decision** is in `__root.tsx:59-79` (`createRootRoute.beforeLoad`), in order: `/pair` + `hasHostedPairingRequest` → `status: "hosted-pairing"`; `isHostedStaticApp(...)` → `status: "hosted-static"` (`:68-72`); else `await resolveInitialServerAuthGateState()` (which can return `"requires-auth"`, declared at `environments/primary/auth.ts:130`, returned at `:277`). The shell renders only for `authenticated` or `hosted-static` (`__root.tsx:111`). Same pair-check repeats in `_chat.tsx:190`, `settings.tsx:101`, `projects.$projectKey.tsx:9`, `pair.tsx:18`.

`isHostedStaticApp` lives in `apps/web/src/hostedPairing.ts:34-45`: false if `configuredBackendUrl()`; true if `configuredHostedAppChannel()`; else origin-match against `configuredHostedAppUrl()`. Also read by `connection/platform.ts:104,246` and `cloud/connectCliAuth.ts:31`.

`resolveChatIndexView` is `routes/-chatIndexView.ts:9-17` — returns `"zerops-onboarding"` iff `authGateStatus === "hosted-static" && environmentCount === 0`, else `"draft-landing"`. Consumed at `_chat.index.tsx:30`, which renders `<ZeropsHostedLanding manualFallback={<HostedStaticOnboardingState />} />` at `:37`.

`PairingRouteSurface` → `apps/web/src/components/auth/PairingRouteSurface.tsx` (323 lines), exporting `PairingRouteSurface`, `HostedPairingRouteSurface`, `PairingPendingSurface`; mounted by `routes/pair.tsx:6,40,46`.
`ZeropsHostedLanding` → `apps/web/src/components/zerops/landing/ZeropsHostedLanding.tsx:32` (191 lines).

## 8. Zerops UI surfaces on main, and where they mount

| Surface | Path | L | Mounted by |
|---|---|---|---|
| `ZeropsSessionProvider` | `zerops/ZeropsSessionProvider.tsx` | 164 | `AppRoot.tsx:8,22` (wraps the whole app, outside the router) |
| `ZeropsPanel` | `components/zerops/ZeropsPanel.tsx` | 79 | `ChatView.tsx:171` → rendered at `:6839` for `activeRightPanelSurface.kind === "zerops"` |
| `ZeropsServiceMap` | `components/zerops/ZeropsServiceMap.tsx` | 149 | `ZeropsPanel.tsx:24` |
| `ZeropsQuickActions` | `components/zerops/ZeropsQuickActions.tsx` | 55 | `ZeropsPanel.tsx:23,57` |
| `ZeropsAgentAuthCard` | `components/zerops/ZeropsAgentAuthCard.tsx` | 280 | two sites — `ZeropsPanel.tsx:22,51` and `ChatView.tsx:173,6981` (overlay above the timeline, gated on `zeropsAgentAuthNeedsAttention`) |
| `ZeropsStripLine` / `ZeropsLifecycleStrip` | `components/zerops/ZeropsLifecycleStrip.tsx:30`/`:83` | 90 | `ChatView.tsx:172` → `:6932`, inside the `WorkspacePageHeader` |
| `ZeropsToolCard` | `components/zerops/ZeropsToolCard.tsx` | 233 | `chat/MessagesTimeline.tsx:120` → `:2605` |
| `ZeropsHostedLanding` | `components/zerops/landing/ZeropsHostedLanding.tsx` | 191 | `routes/_chat.index.tsx:7,37` |
| `ZeropsLandingShell` | `components/zerops/landing/ZeropsLandingShell.tsx` | 355 | `ZeropsHostedLanding.tsx:18,80,94,117` |
| `ZeropsProjectsPage` | `components/zerops/ZeropsProjectsPage.tsx` | 315 | `routes/zerops.tsx:3,6` and `ZeropsHostedLanding.tsx:14,49` |
| `ZeropsProjectPicker` | `components/zerops/ZeropsProjectPicker.tsx` | 241 | `ZeropsProjectsPage.tsx:34,241` |
| `ZeropsProvisioningPanel` | `components/zerops/ZeropsProvisioningPanel.tsx` | 133 | `ZeropsProjectsPage.tsx:35,211` |
| `ZeropsSettings` | `components/settings/ZeropsSettings.tsx` | 78 | `routes/settings.zerops.tsx:3,6` |

**Credential card**: no such component in `apps/web/src`. The "credential" hits are all connection/token plumbing (`connection/storage.ts`, `connection/platform.ts`, `environments/primary/{auth,httpLayer,index}.ts`). The closest UI is `ZeropsAgentAuthCard`.
**Data console**: absent from the web client. The only repo-wide hit is `apps/server/src/zerops/ZeropsCli.ts` (a zcp CLI mention). Nothing under `apps/web` or `packages/client-runtime`.

## 9. Tests

Runner is **vite-plus** (`vp test`), not bare vitest. Root `package.json:test` = `vp run -r test`; `apps/web/package.json:12` = `vp test run --passWithNoTests --project unit`. Config: root `vite.config.ts` (node env, 60s timeouts) plus `apps/web/vite.config.ts:83-94` defining a **single project `unit`**, `include: ["src/**/*.test.{ts,tsx}"]`, 15s timeouts — wired at `apps/web/vite.config.ts:278`. There is no browser project; component tests render via `renderToStaticMarkup` from `react-dom/server` (e.g. `ZeropsToolCard.test.tsx:1`).

Counts: **23** test files under the Zerops zone (`apps/web/src/zerops/` + `components/zerops/`) out of **288** in `apps/web/src` — ~8%. Largest: `ZeropsAgentAuthCard.test.tsx` 444, `cards/payloads.test.ts` 399, `provisioning.test.ts` 364, `agentLogin.test.ts` 327, `feeds.test.ts` 316, `serviceMap.test.ts` 307, `strip.test.ts` 248.

**Architecture/zone test**: `scripts/z3-zone-architecture.test.ts` (267 lines). Four assertions, all machine-checked by regex over import statements: (a) the ported zone — `apps/server/src/provider`, `packages/effect-codex-app-server`, `packages/effect-acp`, `packages/contracts/src/provider*.ts` — imports **nothing matching `/zerops/i`** (`:99-127`); (b) `apps/server/src/zerops` reaches provider internals only at a now-empty known-violations list (`:129-177`); (c) `textGeneration/` + `usage/` reach providers only through `spi/` (`:179-231`); (d) `apps/server/src/zerops` never reads `payload.data` (`:233-266`). **It does not cover `apps/web/src/zerops/**`** — no import-direction rule is enforced on the web zone today.

## 10. Hard-coded palette colour

`grep -rEo '(text|bg|border|ring)-(red|blue|…)-[0-9]{2,3}' apps/web/src | wc -l` → **225**.

Top 8 files: `components/settings/ResourceTelemetryDiagnostics.tsx` 38 · `components/Sidebar.tsx` 34 · `components/Sidebar.logic.ts` 32 · `components/pullRequest/pullRequestPresentation.tsx` 21 · `components/ChatMarkdown.tsx` 15 · `components/ThreadStatusIndicators.tsx` 14 · `components/Sidebar.logic.test.ts` 12 · `components/RightPanelTabs.tsx` 8. (Then `settings/DiagnosticsSettings.tsx` 7, `PullRequestThreadDialog.tsx` 6.)

Notable: **the Zerops zone contributes zero** — the same grep over `apps/web/src/zerops` + `apps/web/src/components/zerops` returns nothing. The concentration is in the sidebar/status vocabulary (`Sidebar.tsx:857` `text-sky-600 dark:text-sky-400`, `:870` amber, `:876` indigo, `:882` red, `:894` emerald) and the PR presentation.

## 11. Build / packaging

Container delivery is a two-step in `apps/server/scripts/cli.ts` (374 lines):
- `cli.ts build` (`:143-176`) — runs `tsdown` via `apps/server`'s `build:bundle`, then copies `apps/web/dist` into `apps/server/dist/client` (`:165-172`). It does **not** build the web app; the web build must already exist.
- `cli.ts pack` (`:333-357`) — the publish rewrite (catalog: → concrete versions) ending in a local tarball instead of a registry upload; explicitly for hand-delivered dev builds.

The web build itself is driven by the zcp-side push loop `/Users/macbook/Documents/Zerops-MCP/zcp/eval/scripts/z3-dev-push.sh:88`:
`VITE_BASE_PATH="${Z3_BASE_PATH:-}" VITE_HOSTED_APP_CHANNEL="${Z3_HOSTED_APP_CHANNEL:-latest}" vp run --filter @t3tools/web build`, then `cli.ts build` (`:91`) and `cli.ts pack --out builds/z3 --app-version "$version"` (`:94`); `Z3_SKIP_WEB=1` reuses `apps/web/dist`.

The two env vars that matter:
- **`VITE_BASE_PATH`** → `apps/web/vite.config.ts:36`, `base = normalizeBasePath(...) + "/"`. Comment at `:29-35`: the bundle is served under `<origin>/z3/` beside code-server on the same 8080 origin; every asset reference is root-absolute, so without it assets 404 or are answered by whoever owns the origin root. Read back at runtime by `src/basePath.ts` off `import.meta.env.BASE_URL`.
- **`VITE_HOSTED_APP_CHANNEL`** → `vite.config.ts:50`, baked at `:212`. Read by `hostedPairing.ts:22` (`configuredHostedAppChannel`) — a non-empty value makes `isHostedStaticApp()` true unconditionally, which is what puts the client in hosted mode (Zerops identity + `/zerops` landing) rather than local-server mode. Also drives `branding.ts:13`.
- Adjacent: `VITE_HTTP_URL`/`VITE_WS_URL` are forbidden for dev (`AGENTS.md:70` — baking localhost into the bundle silently breaks every remote browser); `VITE_ZEROPS_TURNSTILE_SITE_KEY` (`vite.config.ts:53`) gates the sign-up captcha widget.

**Fork guidance files**: `/Users/macbook/Documents/Zerops-MCP/z3/CLAUDE.md` (fork map: zone table at `:26-32` — "Owned product" explicitly names `apps/web/src/zerops/**`; commands; disciplines) and `/Users/macbook/Documents/Zerops-MCP/z3/AGENTS.md` (upstream's, 13.7 KB, still authoritative below the banner). Deeper rules in `docs/internals/zerops/{fork,spi,compat,map,verified,questions,hacks,intake,poc-findings}.md`.

UI rules stated in `AGENTS.md`:
- **"Hit every surface"** — `:72-86`. "The most common defect in this repo is a change that works on the path you tested and is missing everywhere else." Walk: entry points (chat view / Settings / command palette / keybinding), clients (web / desktop / mobile), providers (per-adapter decision), contracts, **reverse states** — "If you added a way in, add the way out and the way to see it. Snooze needs unsnooze. Close needs reopen. **A one-way door is a bug**" (`:80`), connection modes, docs.
- **Animation** — `:24`: performance audits target "css animations causing gpu spikes"; `:155` (Taste): "Our users drive agents all day and notice a dropped frame, a lying spinner, and a stale label. **No continuously repainting animations; they peg the GPU on high-refresh displays.**" This is visibly honoured in `Sidebar.tsx:852-857` ("No shimmer: a label that animates forever is noise… repaints every vsync") and in `index.css:145+` where the status-pulse animations are duty-cycled ("long holds with stepped ramps, so the compositor updates discrete frames instead of every vsync").
- `:152`: "Complexity belongs at the adapter boundary. Orchestration stays pure, **UI stays dumb**."
- `:123`: "UI changes need before/after images. Motion or timing needs a short video."

## 12. `packages/client-runtime/src`

Top-level modules (34,644 lines total):

| Module | L | Zerops-related |
|---|---|---|
| `state/` | 19,436 | partly — exports `state/runtime` (`AtomCommandResult`) consumed by `zerops/feeds.ts`, `agentLogin.ts` |
| `connection/` | 5,937 | yes in part — `connection/onboarding.zerops.test.ts`; `EnvironmentRegistry`/`EnvironmentSupervisor` feed `zerops/feeds.ts` |
| `authorization/` | 1,762 | `authorization/zerops.ts` (43) + `.test.ts` (126) |
| **`zerops/`** | **1,846** | `api.ts` 599, `newProject.ts` 154, `registration.ts` 74, `session.ts` 116, `index.ts` 57, + 3 test files |
| `relay/` | 1,561 | Zerops-adjacent (relay auth carries a `zeropsToken`) |
| `rpc/` | 1,439 | no |
| `operations/` | 1,096 | no |
| `platform/` | 505 | no |
| `errors/` | 404 | no |
| `environment/` | 307 | no (but `scopeThreadRef` is used by the Zerops views) |
| loose: `markdownImages.ts`, `providerSkills.ts` | | no |

`packages/client-runtime/src/zerops/index.ts` is the barrel the web client imports as `@t3tools/client-runtime/zerops` (`ZeropsApiClient`, `ZeropsProject`, `ZeropsService`, `ZeropsUser`, `ZeropsRegistrationResponse`, `ZeropsStorageAdapter`, `isZeropsCaptchaRejection`).

**Mobile imports no Zerops module from client-runtime today.** `grep -rn "zerops" apps/mobile/src` yields 13 hits in exactly two files — `features/cloud/linkEnvironment.ts` (`:216,254` pass `zeropsToken:`, `:229` mentions `zeropsProjectId`) and `features/agent-awareness/remoteRegistration.ts` (`:400,451,553,579`) — and every one is a *relay request field* named `zeropsToken`, with comments saying it still carries the Clerk session token and that "S5-3 sources zeropsToken from the Zerops [session]". No `import … from "@t3tools/client-runtime/zerops"` anywhere in `apps/mobile`. Mobile's only shared-Zerops coupling is the theme package (§3).
