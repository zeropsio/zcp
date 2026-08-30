# z3 fork — anatomy of the non-web clients and shared packages

Repo: `/Users/macbook/Documents/Zerops-MCP/z3` · branch `main` · HEAD `6b1b575d3`
("fix: the three clean-checkout CI failures — fmt, stale SPI version guard, deleted packages/ssh").
Mapped 2026-08-30. Every claim below carries a path; line numbers where they matter.

---

## 1. Monorepo shape

`pnpm-workspace.yaml:1-6` — workspace members:

```
apps/*
infra/*
oxlint-plugin-t3code
packages/*
scripts
```

Package manager `pnpm@11.10.0` (`package.json:59`), node `^24.13.1` (`package.json:62`).
The root manifest also carries a large `catalog:` block (effect `4.0.0-beta.103`, typescript
`~6.0.3`, `vite: npm:@voidzero-dev/vite-plus-core@0.2.2`, `vite-plus: 0.2.2`), 15
`patchedDependencies`, and `overrides` that strip all eight `@anthropic-ai/claude-agent-sdk-*`
platform binaries to `"-"` ("The SDK always receives the user's Claude executable, so its
bundled binaries are unused").

### Workspace members

| Path | `name` | Main dependencies |
|---|---|---|
| `apps/server` | `t3` | `@anthropic-ai/claude-agent-sdk`, `@effect/platform-bun`, `@effect/platform-node`, `@effect/platform-node-shared`, `@effect/sql-sqlite-bun`, `@ff-labs/fff-node`, `@opencode-ai/sdk`, `@pierre/diffs`, `effect`, `msgpackr-extract`, `node-pty`, `yaml`. **`@t3tools/contracts`, `@t3tools/shared`, `@t3tools/web` are devDependencies, not deps.** |
| `apps/web` | `@t3tools/web` | `@t3tools/client-runtime`, `@t3tools/contracts`, `@t3tools/shared`, `@base-ui/react`, `@clerk/react`, `@dnd-kit/*`, `@effect/atom-react`, `@legendapp/list`, `@lexical/react`, `@pierre/diffs`, `@pierre/trees`, `@tanstack/react-router`, `@tanstack/react-pacer`, `culori`, `jose`, `jszip`, `lucide-react`, `react`, `react-dom`, `react-markdown`, `tailwind-merge`, `zustand` |
| `apps/mobile` | `@t3tools/mobile` | `@t3tools/client-runtime`, `@t3tools/contracts`, `@t3tools/shared`, `@clerk/expo`, `@effect/atom-react`, `@expo/ui`, `@callstack/liquid-glass`, `@legendapp/list`, `@noble/curves`, `@noble/hashes`, `@pierre/diffs`, `@react-navigation/*`, `@shikijs/*`, `@tabler/icons-react-native`, ~35 `expo-*` packages, `react-native`, `react-native-reanimated`, `react-native-gesture-handler`, `react-native-nitro-modules`, `react-native-shiki-engine`, `uniwind`, `tailwind-merge`, plus three local native modules: `@t3tools/mobile-markdown-text`, `@t3tools/mobile-review-diff-native`, `@t3tools/mobile-terminal-native` |
| `apps/desktop` | `@t3tools/desktop` | `@t3tools/client-runtime`, `@t3tools/contracts`, `@t3tools/shared`, `@effect/platform-node`, `effect`, `electron`, `electron-store`, `electron-updater`, `playwright-core`, `react-grab`. devDeps: `electron-builder`, `tailwindcss`, `acorn`, `cross-env` |
| `packages/client-runtime` | `@t3tools/client-runtime` | `@t3tools/contracts`, `@t3tools/shared`, `effect` — **nothing else** |
| `packages/contracts` | `@t3tools/contracts` | `effect` only |
| `packages/shared` | `@t3tools/shared` | `@t3tools/contracts`, `@noble/curves`, `@noble/hashes`, `effect`, `jose`, `yaml` |
| `packages/effect-acp` | `effect-acp` | `effect` (import zone) |
| `packages/effect-codex-app-server` | `effect-codex-app-server` | `effect` (import zone) |
| `infra/relay` | `t3code-relay` | `@t3tools/client-runtime`, `@t3tools/contracts`, `@t3tools/shared`, `@effect/platform-node`, `@effect/sql-pg`, `@noble/curves`, `@noble/hashes`, `drizzle-orm`, `effect` |
| `oxlint-plugin-t3code` | `@t3tools/oxlint-plugin-t3code` | `@oxlint/plugins`, `@effect/platform-node`, `effect` |
| `scripts` | `@t3tools/scripts` | `@electron/asar`, `@electron/osx-sign`, `@effect/platform-node`, `@t3tools/contracts`, `@t3tools/shared`, `effect`, `pngjs`, `yaml` |

`packages/ssh/` and `packages/tailscale/` are **empty shells on disk** — each contains only a
stale `node_modules/` and **no `package.json`**, so neither is a workspace member any more. The
HEAD commit message names "deleted `packages/ssh`" as one of the three clean-checkout CI fixes.

### Who depends on which package

- `apps/web` → `@t3tools/client-runtime`, `@t3tools/contracts`, `@t3tools/shared`
- `apps/mobile` → `@t3tools/client-runtime`, `@t3tools/contracts`, `@t3tools/shared`
- `apps/desktop` → `@t3tools/client-runtime`, `@t3tools/contracts`, `@t3tools/shared`

Declared dependency is not the same as actual reach. Import-site counts for
`@t3tools/client-runtime`:

- `apps/web/src` — **238** import sites
- `apps/mobile/src` — **166** import sites
- `apps/desktop/src` — **3** import sites

### `packages/shared` exports map

`packages/shared/package.json` — **56 subpaths, no barrel** (each maps `types` + `import` to the
raw `./src/<name>.ts`):

```
./themePalettes        ./themePreview          ./projectFavicon       ./model
./advertisedEndpoint   ./agentAwareness        ./git                  ./sourceControl
./logging              ./observability         ./httpObservability    ./shell
./semver               ./Net                   ./DrainableWorker      ./KeyedCoalescingWorker
./schemaJson           ./schemaYaml            ./toolActivity         ./Struct
./serverSettings       ./backgroundActivitySettings                   ./String
./projectScripts       ./threadEnvMode         ./t3ProjectFile        ./orchestrationTiming
./remote               ./relaySigning          ./dpop                 ./dpopCommon
./relayAuth            ./relayUrl              ./relayJwt             ./oauthScope
./searchRanking        ./qrCode                ./cliArgs              ./connectAuth
./path                 ./keybindings           ./composerTrigger      ./composerInlineTokens
./terminalLabels       ./relayClient           ./relayTracing         ./preview
./previewViewport      ./filePreview           ./chatList             ./hostProcess
./httpReadiness        ./devHome               ./devProxy             ./basePath
./usageMerge           ./usageFormat           ./claudeCompaction
```

For contrast: `packages/contracts` exports only three subpaths — `.` (`./src/index.ts`),
`./settings`, `./relay`. `packages/client-runtime` exports 40 subpaths, listed in §4.

---

## 2. `apps/mobile/src`

Total ≈ 77,200 lines across the tree.

### Top-level directories, by line count

| Lines | Files | Directory |
|---:|---:|---|
| 56,020 | 281 | `apps/mobile/src/features/` |
| 7,739 | 59 | `apps/mobile/src/lib/` |
| 6,556 | 59 | `apps/mobile/src/state/` |
| 2,675 | 28 | `apps/mobile/src/components/` |
| 1,679 | 17 | `apps/mobile/src/native/` |
| 1,587 | 16 | `apps/mobile/src/connection/` |
| 1,267 | 7 | `apps/mobile/src/persistence/` |
| 664 | 2 | `apps/mobile/src/widgets/` |

Root files: `apps/mobile/src/Stack.tsx` 638, `apps/mobile/src/App.tsx` 106.

`features/` subdirectories: `agent-awareness`, `archive`, `cloud`, `connection`, `diffs`,
`files`, `home`, `keyboard`, `layout`, `observability`, `projects`, `review`, `settings`,
`sharing`, `shortcuts`, `showcase`, `terminal`, `threads`, `updates`, `usage`.

### Largest 10 files

| Lines | File |
|---:|---|
| 2241 | `apps/mobile/src/features/threads/ThreadFeed.tsx` |
| 1621 | `apps/mobile/src/lib/threadActivity.ts` |
| 1332 | `apps/mobile/src/features/threads/ThreadNavigationSidebar.tsx` |
| 1326 | `apps/mobile/src/features/terminal/ThreadTerminalRouteScreen.tsx` |
| 1234 | `apps/mobile/src/features/home/HomeScreen.tsx` |
| 1233 | `apps/mobile/src/features/threads/ThreadSettingsSheet.tsx` |
| 1148 | `apps/mobile/src/features/agent-awareness/remoteRegistration.ts` |
| 1125 | `apps/mobile/src/features/threads/new-task-flow-provider.tsx` |
| 1085 | `apps/mobile/src/features/threads/NewTaskDraftScreen.tsx` |
| 1085 | `apps/mobile/src/features/review/shikiReviewHighlighter.ts` |

(next two: `apps/mobile/src/features/threads/threadListV2.test.ts` 1054,
`apps/mobile/src/features/projects/AddProjectScreen.tsx` 995)

### Zerops presence in mobile: effectively zero

- **No `zerops` directory** anywhere under `apps/mobile/src`.
- `grep -rn -i zerops apps/mobile/src | wc -l` = **22**, spread over exactly **two files**, and
  every hit is a comment or a single wire field name — no logic:
  - `apps/mobile/src/features/agent-awareness/remoteRegistration.ts:398-400`, `:449-451`,
    `:551-553`, `:577-579` — four sites passing `zeropsToken: token`, each preceded by the
    comment *"Still the Clerk session token; S5-3 sources zeropsToken from the Zerops session
    instead."*
  - `apps/mobile/src/features/cloud/linkEnvironment.ts:214-216`, `:229-234`, `:252-254`, `:282`
    — same pattern (`zeropsToken: input.clerkToken`), plus a longer note at `:229-234`: the
    environment-link request "now carries required `zeropsProjectId` + `endpointOrigin` fields,
    verified relay-side against the caller's own Zerops token… mobile has nothing that
    identifies the target's Zerops project or public origin ahead of the S5-3 Zerops session",
    and `:282` "a Zerops-native environment is reached directly, never through one."
- `grep -rn "@t3tools/client-runtime" apps/mobile/src | grep -i zerops` = **0**. Mobile imports
  none of the Zerops client-runtime surface.

Directory names matching `*zerops*` under `apps/mobile` exist only inside the **untracked**
iOS prebuild: `apps/mobile/ios/ZeropsCode.xcodeproj`, `apps/mobile/ios/ZeropsCode.xcworkspace`,
`apps/mobile/ios/ZeropsCode`, `apps/mobile/ios/Pods/Target Support Files/Pods-ZeropsCode`.
`apps/mobile/ios` is gitignored (`apps/mobile/.gitignore:40` → `/ios`) and `git ls-files
apps/mobile/ios` returns **0 tracked files** — a local `expo prebuild` artifact, not repo state.
`docs/ios-build-brief.md:8-9` confirms: "Expo SDK-managed (no `ios/` dir checked in; `expo
prebuild` generates it)."

The committed app identity is still entirely T3 — `apps/mobile/app.config.ts:65-88`:

```
development: appName "T3 Code Dev",     scheme "t3code-dev",     ids com.t3tools.t3code.dev
preview:     appName "T3 Code Preview",  scheme "t3code-preview", ids com.t3tools.t3code.preview
production:  appName "T3 Code",          scheme "t3code",         ids com.t3tools.t3code
```

all three with `relyingParty: "clerk.t3.codes"`. Other brand strings: `app.config.ts:126-127`
widget "Agent Activity" / "Shows the current state of active T3 Code agents";
`:204` local-network permission "Allow T3 Code to connect to T3 Code servers on your local
network or tailnet"; `:298` camera permission "Allow T3 Code to access your camera so you can
scan pairing QR codes." `docs/ios-build-brief.md:118` warns: "**Never rename** the `t3code://`
URL schemes casually — the dev client launches through [them]."

### Theme

**`apps/mobile/global.css`** — 252 lines, **130 `--color-*` declarations**:

- `:1-3` — `@import "tailwindcss"; @import "uniwind"; @import "./generated-uniwind-themes.css";`
- `:7` `:root {`
- `:8` `@variant android { --font-mono: "monospace"; }`
- `:12` `@variant light { … }` — **65 `--color-*` tokens**
- `:111` `@variant dark { … }` — **65 `--color-*` tokens**

Token groups in the light block (`:13-40+`): page backgrounds (`--color-screen`,
`--color-sheet`, `--color-sheet-solid`), card/surface (`--color-card`, `--color-card-alt`,
`--color-card-translucent`), text (`--color-foreground`, `-secondary`, `-muted`, `-tertiary`),
borders & separators (`--color-border`, `-subtle`, `--color-separator`), subtle backgrounds
(`--color-subtle`, `--color-subtle-strong`, `--color-inline-skill-background`, `-border`,
`-foreground`), and onward.

**Generated files** (all committed, all under `apps/mobile/`):

- `generated-uniwind-themes.css` — 1374 lines
- `generated-uniwind-theme-names.json` — 10 names: `t3-chat-light`, `t3-chat-dark`,
  `grove-light`, `grove-dark`, `ocean-light`, `ocean-dark`, `ember-light`, `ember-dark`,
  `iris-light`, `iris-dark`
- `generated-uniwind-default-theme-variables.json` — 136 lines, two keys: `light` (65 vars),
  `dark` (65 vars)
- `apps/mobile/uniwind-types.d.ts`

**Generator: `apps/mobile/scripts/generate-uniwind-themes.mts`**

- Shebang `#!/usr/bin/env node`; invoked as `apps/mobile/package.json:42`
  `"generate": "node --disable-warning=MODULE_TYPELESS_PACKAGE_JSON scripts/generate-uniwind-themes.mts"`
  (i.e. `vp run --filter @t3tools/mobile generate`).
- `:6` imports `BUILT_IN_THEME_IDS`, `BuiltInThemeId` from `@t3tools/shared/themePalettes`.
- `:8-13` imports `getMobileThemeVariables`, `MOBILE_THEME_VARIABLE_NAMES`,
  `MobileThemeAppearance`, `MobileThemeVariables` from `../src/lib/mobileTheme.ts`.
- `:15` `const APPEARANCES = ["light", "dark"] as const;`
- `:16-25` the four paths: `GLOBAL_CSS_PATH` (input), `GENERATED_CSS_PATH`,
  `GENERATED_NAMES_PATH`, `GENERATED_DEFAULT_VARIABLES_PATH` (outputs).
- `:30-51` `color(family, shade, opacity)` helper resolving Tailwind colours, with `oklch` /
  `#fff` / `#000` / `color-mix` opacity handling.
- `:54-57+` `ADAPTIVE_COLORS` — the map that "replace[s] the remaining `dark:*` utility pairs.
  A registered palette theme is neither literally `light` nor `dark`, so appearance-sensitive
  values must also be represented as semantic variables for custom themes." Entries look like
  `"--color-adaptive-amber-500-a12-a16": [color("amber",500,0.12), color("amber",500,0.16)]`,
  `"--color-adaptive-amber-700-300"`, etc. — exactly the class names `threadPresentation.ts`
  consumes.
- `:232-244` `writeFileAtomically` — writes `<file>.<pid>.tmp` then renames, because "Metro
  watches the generated CSS. Replacing a complete temporary file keeps Tailwind from compiling
  a partially rewritten theme file."
- `:245-262` the entry block: `const checkOnly = process.argv.includes("--check")` (`:246`);
  in check mode a mismatch prints ``` `${relative} is stale. Run vp run --filter
  @t3tools/mobile generate.` ``` and sets `process.exitCode = 1` (`:248-256`).

**Test: `apps/mobile/scripts/generate-uniwind-themes.test.ts`** — 55 lines, one `describe`
(`:12` "generate mobile Uniwind themes") with three cases: `:13` "keeps the committed outputs
current", `:27` "registers every custom palette for both appearances", `:47` "generates the
default runtime bridge from the authored CSS".

**`createMobileThemeVariables` / `MOBILE_DEFAULT_THEME_ID`:**

- `packages/shared/src/themePalettes.ts:1` —
  `export const BUILT_IN_THEME_IDS = ["t3-chat", "grove", "ocean", "ember", "iris"] as const;`
- `packages/shared/src/themePalettes.ts:3-4` — `/** The mobile app's own hand-tuned palette,
  which is not part of the built-in library. */` `export const MOBILE_DEFAULT_THEME_ID =
  "t3-code";`
- `packages/shared/src/themePalettes.ts:6-11` — `MOBILE_THEME_IDS = [MOBILE_DEFAULT_THEME_ID,
  ...BUILT_IN_THEME_IDS]`, declared here so "host-side tooling (the app-store screenshot
  harness) can validate a requested theme without importing React Native application code."
- `packages/shared/src/themePalettes.ts:13-15` — `BuiltInThemeId`, `MobileThemeId`,
  `ThemeAppearance = "light" | "dark"`.
- `packages/shared/src/themePalettes.ts:17-18+` — `/** Product roles shared by web CSS, React
  Native tokens, and native surfaces. */` `export const THEME_COLOR_ROLES = ["canvas",
  "chrome", …]`.
- `apps/mobile/src/lib/mobileTheme.ts:4` imports `MOBILE_DEFAULT_THEME_ID`;
  `:16` re-exports it as `DEFAULT_MOBILE_THEME_ID`; `:208` `export function
  createMobileThemeVariables(…)`; `:285` `export const MOBILE_THEME_VARIABLE_NAMES =
  Object.keys(…)`; `:286` and `:296` call sites.
- `apps/mobile/src/lib/mobileTheme.test.ts:8,52,155` exercise it.
- `apps/mobile/src/lib/useUniwindTheme.ts` is the runtime hook.
- `scripts/mobile-showcase.config.ts:2,16` — `import { MOBILE_DEFAULT_THEME_ID }` →
  `export const DEFAULT_SHOWCASE_THEME = MOBILE_DEFAULT_THEME_ID;`

### Thread-row status phrases

**`apps/mobile/src/features/threads/threadPresentation.ts`** — 129 lines. This is the whole
mobile vocabulary for thread state.

- `:1` imports `StatusTone` from `../../components/StatusPill`.
- `:5-8` `threadSortValue(thread)`.
- `:10-16` `export type ThreadStatusKind = "pending-approval" | "awaiting-input" | "working" |
  "connecting" | "error" | "plan-ready";`
- `:18-26` `ThreadStatusPresentation extends StatusTone` — adds `kind`, `iconColor`
  ("Foreground color for the leading status icon"), `iconBackground` ("Background color for the
  leading status icon circle"), `pulse` ("Whether the indicator represents in-flight activity").
- `:29-32` `export const THREAD_STATUS_NEUTRAL_ICON = { iconColor: "#8e8e93", iconBackground:
  "rgba(142,142,147,0.22)" }` — "Neutral icon colors for threads with no actionable status."
- `:34-42` `isLatestTurnSettled(latestTurn, session)`.
- `:44-48` doc-comment: "Resolves the user-facing status of a thread, in priority order. Returns
  `null` for quiescent threads so rows stay free of 'Idle'-style noise. **Mirrors
  `resolveThreadStatusPill` in `apps/web/src/components/Sidebar.logic.ts`.**"
- `:49-128` `resolveThreadStatus(thread)` — six branches in strict priority order, each
  returning a label, two Tailwind/Uniwind class names, **and two hard-coded colour literals**:

| Lines | kind | label | pillClassName / textClassName | iconColor | iconBackground | pulse |
|---|---|---|---|---|---|---|
| `:52-62` | `pending-approval` | "Needs Approval" | `bg-adaptive-amber-500-a12-a16` / `text-adaptive-amber-700-300` | `#ff9f0a` | `rgba(255,159,10,0.22)` | false |
| `:64-74` | `awaiting-input` | "Awaiting Input" | `bg-adaptive-indigo-500-a12-a16` / `text-adaptive-indigo-700-300` | `#5e5ce6` | `rgba(94,92,230,0.22)` | false |
| `:76-86` | `working` | "Working" | `bg-adaptive-sky-500-a12-a16` / `text-adaptive-sky-700-300` | `#0a84ff` | `rgba(10,132,255,0.22)` | **true** |
| `:88-98` | `connecting` | "Connecting" | `bg-adaptive-sky-500-a12-a16` / `text-adaptive-sky-700-300` | `#0a84ff` | `rgba(10,132,255,0.22)` | **true** |
| `:100-110` | `error` | "Error" | `bg-adaptive-rose-500-a12-a16` / `text-adaptive-rose-700-300` | `#ff453a` | `rgba(255,69,58,0.22)` | false |
| `:112-126` | `plan-ready` | "Plan Ready" | `bg-adaptive-violet-500-a12-a16` / `text-adaptive-violet-700-300` | `#bf5af2` | `rgba(191,90,242,0.22)` | false |

- `:128` returns `null` otherwise.

Trigger conditions: `thread.hasPendingApprovals` (`:52`), `thread.hasPendingUserInput` (`:64`),
`thread.session?.status === "running"` (`:76`), `=== "starting"` (`:88`), `=== "error" ||
thread.latestTurn?.state === "error"` (`:100`), and `interactionMode === "plan" &&
isLatestTurnSettled(...) && hasActionableProposedPlan` (`:112-115`).

**Consumers:** `apps/mobile/src/features/threads/thread-list-items.tsx:28` imports it and calls
it at `:456`. Rendered by **`apps/mobile/src/components/StatusPill.tsx`** (37 lines) — a
`View` with `rounded-full`, size-dependent padding (`px-2.5 py-1` compact / `px-3 py-1.5`
default) and `props.pillClassName`, wrapping an `AppText` with `font-t3-bold`, `text-2xs`/
`text-xs` and `props.textClassName`. `StatusTone` (`:5-9`) is `{ label, pillClassName,
textClassName }`.

Other `StatusPill`/tone consumers: `apps/mobile/src/features/connection/connectionTone.ts:1`,
`apps/mobile/src/features/threads/ThreadDetailScreen.tsx:57`, and a separate composer-local
pill (`apps/mobile/src/features/threads/ThreadComposer.tsx:169-222,719`
`ComposerStatusPillState` / `ComposerConnectionStatusPill`).

There is also a v2 list behind a flag: `apps/mobile/src/features/threads/threadListV2.ts` (+
`threadListV2.test.ts` 1054 lines), `thread-list-v2-items.tsx` 967, gated by
`apps/mobile/src/features/threads/use-thread-list-v2-enabled.ts:5`
(`resolveThreadListV2Enabled`).

### `AgentActivity.tsx` — not a thread row

**`apps/mobile/src/widgets/AgentActivity.tsx`** (381 lines, + `AgentActivity.test.ts` 283) is
the **iOS Live Activity / Dynamic Island widget**, not a list row:

- `:1-17` imports SwiftUI primitives from `@expo/ui/swift-ui` (`HStack`, `Image`, `Spacer`,
  `Text`, `VStack`, `ZStack`) and modifiers (`font`, `foregroundStyle`, `frame`,
  `layoutPriority`, `lineLimit`, `padding`, `resizable`, `widgetURL`), plus `createLiveActivity`,
  `LiveActivityComponent`, `LiveActivityLayout` from `expo-widgets`.
- `:22-29` its own, **different** phase vocabulary:
  `AgentActivityPhase = "starting" | "running" | "waiting_for_approval" | "waiting_for_input" |
  "completed" | "failed" | "stale"`.
- `:30-41` `AgentActivityRowProps` — `environmentId`, `threadId`, `projectTitle`, `threadTitle`,
  `modelTitle`, `phase`, `status`, `updatedAt`, `deepLink`.
- `:42-48` `AgentActivityProps` — `title`, `subtitle`, `activeCount`, `updatedAt`, `activities`.
- `:49` note that the function "is serialized into the widget extension's JS bundle".
- `:59` "Use SwiftUI's semantic label colors rather than fixed hex keyed off the …" — i.e. the
  widget deliberately does *not* hard-code colours the way `threadPresentation.ts` does.
- `:69` cross-references the same web source of truth:
  "(apps/web/src/components/Sidebar.logic.ts resolveThreadStatusPill): amber".
- `:135` `const outcomeLabel = failedRow ? "Agent work failed" : "Agent work completed";`
- `:191` "…status label. Single-line keeps rows inside the expanded island's hard …"
- `:259` "…red) the way the Done/Failed status labels do."
- `:363` "…curvature (right edge clipped status labels; titles hugged the left)."

So the same conceptual state is expressed three times with three vocabularies — web
(`Sidebar.logic.ts`), mobile list (`ThreadStatusKind`), and widget (`AgentActivityPhase`) — held
together only by comments.

### `AdaptiveWorkspaceLayout`

**`apps/mobile/src/features/layout/AdaptiveWorkspaceLayout.tsx`** — 575 lines. The responsive
workspace shell that turns window size into a pane arrangement.

- `:1-4` contracts/client-runtime types (`EnvironmentProject`, `EnvironmentThreadShell` from
  `@t3tools/client-runtime/state/shell`; `EnvironmentId`, `ThreadId`,
  `SidebarProjectGroupingMode` from `@t3tools/contracts`).
- `:5` `useAtomValue` from `@effect/atom-react`; `:6-13` React Navigation
  (`useFocusEffect`, `NavigationContext`, `NavigationRouteContext`, `StackActions`,
  `useNavigation`).
- `:25` `useWindowDimensions`, `View` from react-native.
- `:26` `Animated, { useAnimatedStyle, useSharedValue, withTiming }` from
  `react-native-reanimated`.
- `:29-36` the derivation functions from `../../lib/layout`: `deriveFileInspectorPaneLayout`,
  `deriveLayout`, `deriveWorkspacePaneLayout`, plus types `FileInspectorPaneLayout`, `Layout`,
  `WorkspaceAuxiliaryPaneRole`, `WorkspacePaneLayout`.
- `:37` `resolveThreadSelectionNavigationAction` from `../../lib/adaptive-navigation`.
- `:39-43` preferences + project-grouping atoms.
- `:45-48` `parseActiveThreadPath`, `useHardwareKeyboardCommand` (hardware-keyboard commands).
- `:49-51` `AndroidHomeFabLayout`, `HomeListOptionsProvider`, `ThreadNavigationSidebar`.
- `:52-53` `WORKSPACE_PANE_TIMING` from `./workspace-pane-animation`, `WorkspaceInspectorPane`
  from `./workspace-inspector-pane`.
- `:55-64+` `AdaptiveWorkspaceContextValue` — `layout`, `panes`, `fileInspector`,
  `primarySidebarSearchQuery`, `activateAuxiliaryPaneRole(role) => () => void`, …

**Consumed by 10 other files:** `apps/mobile/src/Stack.tsx`,
`features/home/HomeRouteScreen.tsx`, `features/threads/ThreadRouteScreen.tsx`,
`features/threads/NewTaskRouteScreen.tsx`, `features/threads/sidebar-navigation-shell.tsx`,
`features/terminal/ThreadTerminalRouteScreen.tsx`, `features/files/ThreadFilesRouteScreen.tsx`,
`features/review/ReviewSheet.tsx`, `features/layout/workspace-sidebar-toolbar.tsx`,
`features/layout/workspace-inspector-pane.tsx`.

### Screenshot harness — yes, a full one

- Root `package.json:14` — `"screenshots:mobile": "node scripts/mobile-showcase.ts"`.
- `apps/mobile/package.json:15` — `"screenshots": "node ../../scripts/mobile-showcase.ts"`;
  a sibling `"showcase"` script boots Expo in showcase mode.
- **`scripts/mobile-showcase.ts`** — 49,763 bytes. Boots real iOS Simulator / Android emulator
  instances, launches the app against locally spun `apps/server` fixture environments, walks the
  configured scenes (threads / environments / thread / terminal / review) per device × theme ×
  appearance, and captures + validates store-compliant PNGs (`finalizeCapture` /
  `validateStoreAsset`, `:242-274`; path builder `:228-240`).
- **`scripts/mobile-showcase.config.ts`** — 7,493 bytes; `:2,16` pull `MOBILE_DEFAULT_THEME_ID`
  as `DEFAULT_SHOWCASE_THEME`; `:98` `outputDirectory: "artifacts/app-store/screenshots"`.
- **`scripts/mobile-showcase-environment.ts`** — 26,141 bytes (fixture environments).
- **`scripts/mobile-showcase.test.ts`** — 12,094 bytes.
- Output path shape: `artifacts/app-store/screenshots/<device-store-dir>/<appearance>/<theme>/<scene>.png`.
- App-side driver: **`apps/mobile/src/features/showcase/`** — 844 lines, 9 files:
  `ShowcaseCaptureCoordinator.tsx` 312, `nativeShowcaseScene.ts` 119,
  `showcasePendingTasks.ts` 66 (+72 test), `showcaseEnvironmentRows.test.ts` 66,
  `showcaseRenderSignal.ts` 39 (+52 test), `showcaseRetry.ts` 46 (+44 test).
- Runbook: `docs/operations/mobile-app-store-screenshots.md`.

### How mobile connects to a server today — pairing only

There is no Zerops identity path on mobile. The whole flow is host + pairing code, optionally
scanned from a QR.

**`apps/mobile/src/features/connection/`** — 1233 lines, 10 files:

| Lines | File |
|---:|---|
| 311 | `ConnectionsNewRouteScreen.tsx` |
| 212 | `ConnectionEnvironmentRow.tsx` |
| 119 | `ConnectionStatusDot.tsx` |
| 110 | `EnvironmentConnectionNotice.tsx` |
| 101 | `ConnectionsRouteScreen.tsx` |
| 99 | `ConnectionSheetButton.tsx` |
| 96 | `pairing.ts` |
| 83 | `pairing.test.ts` |
| 59 | `useConnectionController.ts` |
| 43 | `connectionTone.ts` |

**`ConnectionsNewRouteScreen.tsx`** — the add-a-server screen:

- `:189`, `:194` — header title toggles `showScanner ? "Scan QR Code" : "Add Environment"`.
- `:251-255` — camera-permission block with an "Allow camera" action.
- `:273` — host field, `placeholder="192.168.1.100:8080"`.
- `:287` — code field, `placeholder="abc-123-xyz"`.
- `:298` — submit, `label={isSubmitting ? "Pairing..." : "Add environment"}`.

**`pairing.ts`**:

- `:1` `import { readHostedPairingRequest } from "@t3tools/shared/remote";`
- `:3` `import { normalizeBasePath } from "@t3tools/shared/basePath";`
- `:5` `const MOBILE_PAIRING_URL_PARAM = "pairingUrl";`
- `:7-20` `isIpLiteral(host)` — IPv6 bracket-strip + IPv4 four-octet check.
- `:22-29` `PairingQrPayloadEmptyError` — "Scanned QR code did not contain a pairing URL."
- `:31-44` `buildPairingUrl(host, code)` — scheme defaults to `http` for IP literals and
  `https` otherwise (`:38`); the code goes in the **URL fragment** as `#token=<code>`
  (`:39-40`), falling back to a raw `` `${h}#token=${c}` `` on parse failure.
- `:46+` `parsePairingUrl(url)` — delegates to `readHostedPairingRequest(parsed)` and strips a
  trailing slash off the host.

Lower-level connection state lives in **`apps/mobile/src/connection/`** (16 files, 1587 lines):
`platform.ts` 254, `environment-cache-store.ts` 247 (+133 test), `storage.ts` 138 (+97 test),
`catalog-store.ts` 121, `background-activity.ts` 116 (+77 test), `migration.ts` 110 (+78 test),
`background-activity-scopes.ts` 92, `runtime.ts` 45, `onboarding.ts` 35,
`app-state-wakeups.ts` 18 (+21 test), `catalog.ts` 5.

---

## 3. `apps/desktop` after the S5-1 deletion

`apps/desktop/src` — **29,802 lines, 105 files**.

### Directories

| Lines | Files | Directory |
|---:|---:|---|
| 12,257 | 21 | `apps/desktop/src/preview/` |
| 3,883 | 25 | `apps/desktop/src/app/` |
| 3,321 | 18 | `apps/desktop/src/electron/` |
| 2,604 | 6 | `apps/desktop/src/window/` |
| 2,367 | 7 | `apps/desktop/src/settings/` |
| 2,344 | 7 | `apps/desktop/src/updates/` |
| 1,309 | 11 | `apps/desktop/src/ipc/` |
| 999 | 2 | `apps/desktop/src/shell/` |

Root files: `preload.ts` 203, `linuxSecretStorage.ts` 178 (+`linuxSecretStorage.test.ts` 203),
`main.ts` 116, `preview-pip-preload.ts` 17, `preview-pick-preload.ts` 1.

Largest: `preview/Manager.ts` 4215 (+`Manager.test.ts` 3386), `preview/PickPreload.ts` 1305,
`window/DesktopWindow.test.ts` 1029, `preview/FaviconCapture.test.ts` 999,
`updates/DesktopUpdates.ts` 856 (+836 test), `window/DesktopWindow.ts` 776,
`preview/FaviconCapture.ts` 679, `settings/DesktopSavedEnvironments.ts` 609,
`shell/DesktopShellEnvironment.ts` 551, `app/DesktopConnectionCatalogStore.ts` 516.

Top-level `apps/desktop/`: `package.json`, `resources/`, `scripts/`, `src/`, `tsconfig.json`,
`vite.config.ts`, plus build output `dist-electron/`.

### What the S5-1 deletion actually removed

- **WSL — 0 hits.** `grep -rni wsl apps/desktop/src` returns nothing.
- **Clerk — 13 hits, all in test files, none functional.**
  `app/DesktopPreReadyPlatform.test.ts:82-125` uses a *fixture* class literally named
  `ClerkShaped` to assert layer ordering ("acquires a synchronous pre-ready layer before an
  asynchronous Clerk-shaped layer", `:82`; asserts `events === ["pre-ready", "clerk"]`, `:125`).
  `updates/DesktopUpdates.test.ts:311,326` merely quote an upstream release-note string
  ("[codex] Upgrade Clerk stack by @juliusmarminge in #3821").
- **SSH — 55 hits, but no SSH implementation.** They are all one persisted field, `desktopSsh`,
  on saved-environment records, plus tests:
  `settings/DesktopSavedEnvironments.ts:17-18` (`PersistedSavedEnvironmentDesktopSsh`),
  `:23,25` (`desktopSsh?`), `:39` (`DesktopSshTargetSchema`), `:53`
  (`desktopSsh: Schema.optionalKey(DesktopSshTargetSchema)`); also
  `app/DesktopConnectionCatalogStore.ts` (8 hits, +12 in its test),
  `shell/DesktopShellEnvironment.ts` (3, +10 in test), `electron/ElectronShell.ts` (3, +5 test),
  `preview/PickPreload.ts` (4), `settings/DesktopSavedEnvironments.test.ts` (2).
- **Local backend — gone, stated in code.** `electron/ElectronProtocol.ts:55-59`: "Where the
  renderer origin's requests are served from. Development proxies to the Vite dev server (HMR);
  every other run (packaged or an unpackaged production-mode launch) serves the staged
  hosted-static web bundle straight off disk — **the desktop no longer runs a local backend to
  proxy to.**"

### Where the renderer source is now

**`apps/desktop/src/electron/ElectronProtocol.ts`** (339 lines, +338 test):

- `:14` `export const DESKTOP_HOST = "app";`
- `:15` `export const DESKTOP_PRODUCTION_SCHEME = "t3code";`
- `:16` `export const DESKTOP_DEVELOPMENT_SCHEME = "t3code-dev";`
- `:18-20` `getDesktopScheme(isDevelopment)`
- `:22-24` `getDesktopOrigin(isDevelopment)` = `` `${scheme}://${DESKTOP_HOST}` `` → **`t3code://app`**
- `:26-28` `getDesktopUrl(isDevelopment)` = origin + `/`
- `:31-52` `ElectronProtocolRegistrationError` / `ElectronProtocolUnregistrationError`
- `:63-65` `DesktopProtocolTarget` — a two-case union:
  `{ _tag: "development"; devServerUrl: URL }` | `{ _tag: "static"; bundleDir: string }`
- `:70-77` the `ElectronProtocol` Effect service, id `"@t3tools/desktop/electron/ElectronProtocol"`
- `:79-97` `makeDesktopContentSecurityPolicy({ scheme })`, with the comment at `:80-85`:
  "The renderer connects directly to user-selected Zerops environments (**and the Zerops API**)
  in addition to whatever served this bundle. Those origins are not known when this response
  policy is created, so restrict connections by the network schemes the client supports instead
  of by host." The policy: `default-src 'self'`; `script-src 'self' 'unsafe-inline'
  'wasm-unsafe-eval' https://challenges.cloudflare.com`; `connect-src 'self' http: https: ws:
  wss:`; `img-src 'self' <scheme>: blob: data: http: https:`; `style-src 'self' 'unsafe-inline'`;
  `font-src 'self' <scheme>: data:`; `worker-src 'self' blob:`;
  `frame-src 'self' https://challenges.cloudflare.com`; `form-action 'self'`.
- `:110-133` `registerDesktopSchemePrivilegesSync()` — "Must run synchronously during process
  bootstrap, before Electron emits `ready`"; both schemes registered with
  `{ standard: true, secure: true, supportFetchAPI: true, corsEnabled: true }`.
- `:141` `TRANSIENT_FETCH_RETRY_DELAYS_MS = [0, 50, 150]`; `:143-158`
  `fetchWithTransientRetry` over `Electron.net.fetch`.
- `:159+` `proxyToDevServer(request, devServerUrl, contentSecurityPolicy)` — 404s anything whose
  host is not `DESKTOP_HOST` (`:165-167`), rewrites the URL onto the dev server, strips
  `host`/`origin`/`referer`/`connection` headers.
- `:313-330` registration/unregistration via `Electron.protocol.handle` /
  `Electron.protocol.unhandle`.

**How the bundle gets there** — `scripts/build-desktop-artifact.ts`:

- `:328` `"web-dist": "hosted web bundle (apps/web/dist)"`
- `:370` allowed build commands literal: `["vp run build:desktop", "vp run --filter @t3tools/web build"]`
- `:598-601` note that the hosted-static `apps/web` build (with `VITE_HOSTED_APP_CHANNEL` set)
  contributes every file under its parent directory — "see `stageHostedWebBundle`"
- `:1408-1476` **`stageHostedWebBundle`** — "Builds (unless skipBuild) the hosted-static
  apps/web bundle — the same build the Vercel-hosted web app uses (`VITE_HOSTED_APP_CHANNEL`
  set, `VITE_HTTP_URL`/`VITE_WS_URL` unset so `isHostedStaticApp()` is true…)". `:1434` filters
  to `@t3tools/web`; `:1443` sets `VITE_HOSTED_APP_CHANNEL: resolveDesktopUpdateChannel(...)`;
  `:1444-1450` explicitly scrubs both URL env vars so "a developer's .env/.env.local" cannot
  bake in an origin; `:1451,1460` label the command; `:1476` logs "Staged hosted web bundle at
  …".
- `:1668` `stageHostedWebBundle({...})` call site in the main artifact pipeline.

**Navigation fencing** — `apps/desktop/src/window/DesktopWindow.ts:153-158`
`isSameOriginRendererNavigation({ applicationUrl, navigationUrl })` compares `new URL(...).origin`;
checked at `:173`, `:474`, `:592`; the window is loaded at `:561`
`void window.loadURL(applicationUrl).catch(() => undefined);`.

### Brand constants — all still T3

**`apps/desktop/src/app/DesktopEnvironment.ts`** (230 lines):

- `:76` `const APP_BASE_NAME = "T3 Code";`
- `:93` `resolveDesktopAppStageLabel(input)`
- `:100-129` arch normalization (`arm64`/`x64`, `runningUnderArm64Translation`)
- `:137-138` `devServerUrl` from `DesktopConfig`; `isDevelopment = Option.isSome(devServerUrl)`
- `:139-146` per-platform appData dir (win32 / darwin / else)
- `:154-158` `resolveDesktopAppBranding(...)` → `displayName`
- `:165` `userDataDirName = isDevelopment ? "t3code-dev" : "t3code"`
- `:166` `legacyUserDataDirName = isDevelopment ? "T3 Code (Dev)" : "T3 Code (Alpha)"`
- `:206` bundle id `isDevelopment ? "com.t3tools.t3code.dev" : "com.t3tools.t3code"`
- `:208` `linuxDesktopEntryName: isDevelopment ? "t3code-dev.desktop" : "t3code.desktop"`
- `:209` `linuxWmClass: isDevelopment ? "t3code-dev" : "t3code"`
- `:229` `export const layer = (input) => …`

Other brand sites:

- `apps/desktop/src/app/DesktopLinuxUrlHandler.ts:23`
  `URL_HANDLER_DESKTOP_ENTRY_NAME = "t3code-url-handler.desktop"`
- `apps/desktop/src/app/DesktopEarlyElectronStartup.ts:84`
  `linuxWmClass: isDevelopmentEnvironment(input.env) ? "t3code-dev" : "t3code"`
- `apps/desktop/src/app/DesktopApp.ts:73` `"T3 Code failed to start"`; `:96` comment
  "non-http `t3code://` origin"
- `apps/desktop/src/app/DesktopAppIdentity.ts:17,84` `t3codeCommitHash`
- `apps/desktop/src/window/DesktopApplicationMenu.ts:69`
  `` `T3 Code ${updateState.currentVersion} is currently the newest version available.` ``
- `apps/desktop/src/linuxSecretStorage.ts:115` "T3 Code could not access GNOME Keyring to save
  this environment credential…"; `:119` the KWallet equivalent
- `apps/desktop/src/preview/BrowserSession.ts:12` `PREVIEW_PARTITION_PREFIX =
  "persist:t3code-preview-"`; `:138` UA scrub `.replace(/\s*t3code\/[\d.]+/, "")`
- `apps/desktop/src/preview/PickPreload.ts:28` `OVERLAY_ATTRIBUTE = "data-t3code-annotation-ui"`;
  `:408`, `:525`, `:1197` `data-t3code-annotation-tool`
- `apps/desktop/src/preload.ts:11` an `oxlint-disable-next-line t3code/no-global-process-runtime`
  (the only in-repo disable of that rule I saw)
- `scripts/build-desktop-artifact.ts:35` `DESKTOP_APP_ID = "com.t3tools.t3code"`;
  `:567` `t3codeCommitHash`; `:583` comment "T3 Code always passes the user's installed Claude
  executable to the SDK"; `:1006` tmp prefix `"t3code-icon-build-"`;
  `:1260-1261` product name `"T3 Code (Nightly)"` / `desktopPackageJson.productName ?? "T3 Code"`;
  `:1310-1311` and `:1352-1353` protocol registration `name: "T3 Code", schemes: ["t3code",
  "t3code-dev"]`; `:1344` `executableName: "t3code"`; `:1349` comment "t3code:// OAuth callbacks
  to the app"; `:1358` `StartupWMClass: "t3code"`; `:1527` stage tmp prefix
  `` `t3code-desktop-${options.platform}-stage-` ``; `:1686` `name: "t3code"`; `:1689`
  `t3codeCommitHash: commitHash`

### Titlebar colours

**`apps/desktop/src/window/DesktopWindow.ts`**:

- `:31` `const TITLEBAR_HEIGHT = 40;`
- `:32` `const TITLEBAR_COLOR = "#01000000"; // #00000000 does not work correctly on Linux`
- `:33` `const TITLEBAR_LIGHT_SYMBOL_COLOR = "#1f2937";`
- `:34` `const TITLEBAR_DARK_SYMBOL_COLOR = "#f8fafc";`
- `:53-56` `WindowTitleBarOptions = Pick<…, "titleBarOverlay" | "titleBarStyle" |
  "trafficLightPosition">`
- `:110-112` `getInitialWindowBackgroundColor(shouldUseDarkColors)` → `"#0a0a0a"` / `"#ffffff"`
- `:180-199` `getWindowTitleBarOptions(shouldUseDarkColors, platform)` — mac gets
  `titleBarStyle: "hiddenInset"` (`:186`); everything else `titleBarStyle: "hidden"` +
  `titleBarOverlay: { color: TITLEBAR_COLOR, height: TITLEBAR_HEIGHT, symbolColor:
  shouldUseDarkColors ? TITLEBAR_DARK_SYMBOL_COLOR : TITLEBAR_LIGHT_SYMBOL_COLOR }` (`:192-197`)
- `:203-214` `syncWindowAppearance(...)` — `setBackgroundColor` + `setTitleBarOverlay`
- `:263`, `:294`, `:297` initial window construction; `:768-770` re-sync on theme change

### Release / signing workflow

**None in CI.** `.github/workflows/` contains exactly one file, `ci.yml`, triggered on
`pull_request` and `push` to `main` — **no tag trigger**, no codesign/notarize/electron-builder
job.

The signing and packaging machinery exists as repo scripts wired only to npm scripts:

- `scripts/sign-macos.ts` (317 bytes, + `sign-macos.test.ts` 801 bytes) — codesign batching
- `scripts/build-desktop-artifact.ts` (68,422 bytes, + 26,624-byte test) — dmg / AppImage / nsis
  staging, driven by root `package.json:36-43`: `dist:desktop:artifact`, `dist:desktop:dmg`
  (+`:arm64`, `:x64`), `dist:desktop:linux`, `dist:desktop:win` (+`:arm64`, `:x64`)
- `scripts/notify-discord-release.ts`, `scripts/resolve-nightly-release.ts`,
  `scripts/resolve-previous-release-tag.ts`, `scripts/update-release-package-versions.ts`,
  `scripts/merge-update-manifests.ts`, `scripts/mock-update-server.ts`

The CI job `release_smoke` runs `node scripts/release-smoke.ts` — it exercises release-path
*logic*; it does not build or sign a release.

---

## 4. `packages/client-runtime/src`

### Module list with line counts

| Lines | Files | Module |
|---:|---:|---|
| 19,436 | 79 | `state/` |
| 5,937 | 21 | `connection/` |
| 1,846 | 9 | `zerops/` |
| 1,762 | 8 | `authorization/` |
| 1,561 | 5 | `relay/` |
| 1,439 | 7 | `rpc/` |
| 1,096 | 5 | `operations/` |
| 505 | 6 | `platform/` |
| 404 | 9 | `errors/` |
| 307 | 7 | `environment/` |

Root files: `providerSkills.ts` 71 (+`providerSkills.test.ts` 120),
`markdownImages.ts` 98 (+`markdownImages.test.ts` 62).

**`zerops/`** (1846 lines, 9 files):

| Lines | File |
|---:|---|
| 599 | `packages/client-runtime/src/zerops/api.ts` |
| 343 | `packages/client-runtime/src/zerops/api.test.ts` |
| 238 | `packages/client-runtime/src/zerops/newProject.test.ts` |
| 154 | `packages/client-runtime/src/zerops/newProject.ts` |
| 148 | `packages/client-runtime/src/zerops/registration.test.ts` |
| 117 | `packages/client-runtime/src/zerops/session.test.ts` |
| 116 | `packages/client-runtime/src/zerops/session.ts` |
| 74 | `packages/client-runtime/src/zerops/registration.ts` |
| 57 | `packages/client-runtime/src/zerops/index.ts` |

**`connection/`** (21 files): `supervisor.test.ts` 1160, `registry.test.ts` 947,
`supervisor.ts` 819, `registry.ts` 678, `onboarding.ts` 361, `resolver.test.ts` 335,
`onboarding.test.ts` 259, `resolver.ts` 239, **`onboarding.zerops.test.ts` 193**,
`presentation.test.ts` 187, `model.ts` 173, `catalog.ts` 130, `presentation.ts` 129,
`errors.ts` 88, `driver.ts` 64, `layer.ts` 39, `wakeups.ts` 33, `index.ts` 33,
`credentialStore.ts` 27, `profileStore.ts` 24, `connectivity.ts` 19.

**Other modules** (selected, by size): `relay/managedRelay.ts` 767 (+449 test),
`authorization/remote.test.ts` 557, `rpc/session.test.ts` 413, `authorization/layer.test.ts` 409,
`rpc/client.test.ts` 390, `operations/commands.ts` 333, `operations/projects.ts` 316,
`authorization/service.ts` 316, `rpc/client.ts` 301, `authorization/remote.ts` 267,
`relay/managedRelayState.ts` 190, `rpc/http.ts` 173, `rpc/session.ts` 150,
`platform/storageDocument.ts` 141, `platform/persistence.ts` 131,
**`authorization/zerops.test.ts` 126**, `errors/safeLog.ts` 107, `environment/scoped.ts` 69,
`platform/capabilities.ts` 68, `errors/errorTrace.ts` 50, **`authorization/zerops.ts` 43**,
`environment/knownEnvironment.ts` 41, `errors/transport.ts` 39,
`authorization/tokenStore.ts` 37, `environment/descriptor.ts` 17, `platform/source.ts` 15,
`environment/endpoint.ts` 14, `errors/orchestration.ts` 10, `rpc/protocol.ts` 8.

### Which modules are Zerops-related

- **`zerops/api.ts`** — the Zerops REST client.
- **`zerops/session.ts`** — session + selection persistence.
- **`zerops/registration.ts`** — sign-up body + captcha rejection.
- **`zerops/newProject.ts`** — project/container creation bodies, `zcp` service import YAML.
- **`zerops/index.ts`** — the barrel.
- **`authorization/zerops.ts`** (43) + **`authorization/zerops.test.ts`** (126).
- **`connection/onboarding.zerops.test.ts`** (193) — a Zerops-specific onboarding case layered
  over the generic `connection/onboarding.ts` (361).

`packages/client-runtime/src/zerops/index.ts` in full (57 lines):

```
:1-23  from "./api.ts":
       DEFAULT_ZEROPS_API_BASE, ZeropsApiClient, ZeropsApiError, buildZeropsContainerUrl,
       isUsableZeropsSession, isZeropsSession, requiresZeropsTwoFactor, zeropsClientsFromUser,
       zeropsRegionFromPublicZone,
       type ListProjectsOptions, ZeropsApiClientOptions, ZeropsApiErrorKind,
       ZeropsClientMembership, ZeropsLoginResponse, ZeropsOrganization, ZeropsProject,
       ZeropsRegistrationResponse, ZeropsService, ZeropsServicePort, ZeropsSession, ZeropsUser
:25-37 from "./session.ts":
       ZEROPS_SELECTION_STORAGE_KEY, ZEROPS_SESSION_STORAGE_KEY, clearZeropsSelection,
       clearZeropsSession, loadZeropsSelection, loadZeropsSession, parseZeropsSession,
       saveZeropsSelection, saveZeropsSession,
       type ZeropsSelection, ZeropsStorageAdapter
:39-45 from "./registration.ts":
       ZEROPS_CAPTCHA_ERROR_CODE, buildZeropsRegistrationBody, isZeropsCaptchaRejection,
       type ZeropsRegistrationBody, ZeropsRegistrationInput
:47-57 from "./newProject.ts":
       VSCODE_PASSWORD_LENGTH, buildCreateProjectBody, buildDevelopmentContainerImportBody,
       buildZcpServiceImportYaml, generateVscodePassword, nextZcpServiceName,
       type CreateProjectBody, DevelopmentContainerImportBody, RandomBytes
```

**There is no `candidates`, `identity`, or `provisioning` module inside client-runtime.** Those
concepts live app-side in `apps/web/src/zerops/`: `candidates.ts`, `useZeropsCandidates.ts`,
`provisioning.ts` (+`provisioning.test.ts`, `autoEnterProvisioning.test.ts`),
`registrationHandoff.ts` (+test), `agentLogin.ts`, `feeds.ts` (+`feeds.test.ts`), `storage.ts`,
`ZeropsSessionProvider.tsx`.

### Zerops import counts, web vs mobile

`grep -rn "@t3tools/client-runtime" <app> | grep -i zerops`:

- **`apps/web/src` — 19 hits**, all inside `apps/web/src/zerops/**` and
  `apps/web/src/components/zerops/**`:
  - `apps/web/src/zerops/ZeropsSessionProvider.tsx` → `@t3tools/client-runtime/zerops`
  - `apps/web/src/zerops/provisioning.ts` → `/zerops`
  - `apps/web/src/zerops/provisioning.test.ts` → `/zerops`
  - `apps/web/src/zerops/candidates.ts` → `/zerops`
  - `apps/web/src/zerops/candidates.test.ts` → `/zerops`
  - `apps/web/src/zerops/useZeropsCandidates.ts` → `/zerops`
  - `apps/web/src/zerops/storage.ts` → `/zerops`
  - `apps/web/src/zerops/registrationHandoff.ts` → `/zerops`
  - `apps/web/src/zerops/registrationHandoff.test.ts` → `/zerops`
  - `apps/web/src/zerops/autoEnterProvisioning.test.ts` → `/zerops`
  - `apps/web/src/zerops/agentLogin.ts` → `/state/runtime`
  - `apps/web/src/zerops/feeds.ts` → `/connection`, `/state/runtime`
  - `apps/web/src/zerops/feeds.test.ts` → `/connection`, `/rpc`
  - `apps/web/src/components/zerops/ZeropsLifecycleStrip.tsx` → `/environment`
  - `apps/web/src/components/zerops/ZeropsPanel.tsx` → `/environment`
  - `apps/web/src/components/zerops/ZeropsProjectPicker.test.tsx` → `/zerops`
  - `apps/web/src/components/zerops/landing/ZeropsHostedLanding.tsx` → `/zerops`
- **`apps/mobile/src` — 0**
- **`apps/desktop/src` — 0**
- **`infra/relay/src` — 0**

### React inside client-runtime

**None.** `grep -rln 'from "react"' packages/client-runtime/src` = **0 files**; the same grep
extended to `react-dom` and `react-native` also returns 0. The package is pure TS/Effect, as its
manifest implies (deps: `@t3tools/contracts`, `@t3tools/shared`, `effect` only).

### client-runtime exports map (40 subpaths)

`./connection`, `./authorization`, `./environment`, `./markdown-images`, `./errors`, `./rpc`,
`./operations`, `./operations/projects`, `./platform`, `./providerSkills`, `./relay`,
`./state/auth`, `./state/assets`, `./state/connections`, `./state/entities`,
`./state/filesystem`, `./state/git`, `./state/models`, `./state/orchestration`,
`./state/presentation`, `./state/preview`, `./state/projects`, `./state/pull-requests`,
`./state/project-grouping`, `./state/review`, `./state/runtime`, `./state/server`,
`./state/session`, `./state/shell`, `./state/source-control`, `./state/terminal`,
`./state/threads`, `./state/subagentRuntime`, `./state/thread-sort`, `./state/thread-settled`,
`./state/thread-search`, `./state/vcs`, **`./zerops`**.

---

## 5. `packages/contracts/src`

**58 files, 20,543 lines.** Largest five: `orchestration.ts` 1823, `rpc.ts` 1219,
`providerRuntime.ts` 1219, `ipc.ts` 1216, `pullRequest.ts` 1119.

### Zerops-related files

**`packages/contracts/src/zerops.ts`** — 475 lines, entirely Zerops, re-exported from
`packages/contracts/src/index.ts:34`. Exported top-level names include:

- `ZeropsServiceGroup`, `ZeropsService`, `ZeropsProject`
- **`ZeropsTopologySnapshot`** — `zerops.ts:113`
- `ZeropsStateEnvelope`
- **`ZeropsLifecycle`** — `zerops.ts:294-301`, fields `threadId`, `envelope`, `recentTools`,
  `updatedAt` (this is the lifecycle *snapshot* type)
- `ZeropsLifecycleGetInput` — `zerops.ts:308`
- `ZeropsAgentId` — `zerops.ts:325`, `["claude-code", "codex"]`
- `ZeropsAgentAuthState`, `ZeropsAgentLoginState`, `ZeropsAgentAuth`
- **`ZeropsAgentAuthSnapshot`** — `zerops.ts:418`
- `ZEROPS_AGENT_LOGIN_COMMANDS`, `ZeropsAgentLoginError`

**`packages/contracts/src/rpc.ts:1050-1109`** — the Zerops RPC block:

- `WsZeropsTopologyGetRpc`
- `WsZeropsTopologyRefreshRpc`
- `WsSubscribeZeropsTopologyRpc`
- `WsZeropsLifecycleGetRpc` — success `ZeropsLifecycle` (`rpc.ts:1069`)
- `WsSubscribeZeropsLifecycleRpc` — success `ZeropsLifecycle` (`rpc.ts:1075`)
- `WsSubscribeZeropsAgentAuthRpc` — success `ZeropsAgentAuthSnapshot`
- `WsZeropsAgentLoginStartRpc`
- `WsZeropsAgentLoginCancelRpc`

**`packages/contracts/src/auth.ts`** — 382 lines:

- **`bootstrapMethods` — `auth.ts:162`**:
  `ServerAuthDescriptor.bootstrapMethods: Schema.Array(ServerAuthBootstrapMethod)`, documented
  at `:149-150`.
- **the `"zerops-identity"` bootstrap literal — `auth.ts:61`**, a member of the
  `ServerAuthBootstrapMethod` union, documented at `:54-56`.

**`packages/contracts/src/environmentHttp.ts`** — 646 lines: the `EnvironmentZeropsHttpApi`
HTTP group, with the identity endpoint **`POST /api/auth/zerops-identity` at `:632`**.

**`packages/contracts/src/relay.ts`** — 847 lines: `zeropsProjectId` field at `:197`;
`RelayEnvironmentLinkUnavailableReason = ["zerops_api_unavailable"]` at `:345`; DPoP /
token-exchange comments referencing the "Zerops access token".

**`packages/contracts/src/providerRuntimeSpi.ts`** — 96 lines: doc-only references to "the
Zerops feeds" and `zerops_*` tool calls; not itself Zerops-typed.

### Strip / stripped lifecycle types

**None exist.** "strip" appears in `zerops.ts:21,246,270,275` only as UI prose ("status strip").
"stripped" appears in `ipc.ts:107,109,663` about menu/CSS stripping — unrelated to lifecycle.

---

## 6. Architecture / zone tests + CI + lint

### The zone architecture test

**`scripts/z3-zone-architecture.test.ts`** (10,843 bytes). Its header comment (`:1-8`) states it
works by regex over import specifiers, with no AST. Four rules:

1. **`:99-128` — Ported zone stays Zerops-free.** Files under `apps/server/src/provider/**`,
   `packages/effect-codex-app-server`, `packages/effect-acp`, and the direct children matching
   `packages/contracts/src/provider*.ts` must not import any specifier matching `/zerops/i`.
2. **`:130-177` — Owned product must not reach into provider internals.** Files under
   `apps/server/src/zerops/**` must not import a specifier containing `/provider/` nor name
   `ProviderService` in the import clause. The escape hatch
   `KNOWN_OWNED_PRODUCT_PROVIDER_VIOLATIONS` is declared at `:157` and is **empty**.
3. **`:179-231` — textGeneration/usage reach providers through one door.**
   `apps/server/src/textGeneration/**` and `apps/server/src/usage/**` may import `/provider/`
   paths only via the single allowlisted file
   `apps/server/src/provider/Services/ProviderInstanceRegistry.ts` (`:198`).
4. **`:233-266` — SPI-4, no raw payload reads.** No file under `apps/server/src/zerops/**` may
   contain the literal text `payload.data`; only `apps/server/src/spi/toolCall.ts` may read it.

**Zones and how a file is assigned to one — purely by path glob; there is no tag or marker
mechanism:**

- **import zone** — `packages/effect-acp`, `packages/effect-codex-app-server` (byte-pinned by
  `imported.lock`)
- **port zone** — `apps/server/src/provider/**`, `packages/contracts/src/provider*.ts` (must
  stay Zerops-free so future upstream ports stay cheap)
- **owned zone** — `apps/server/src/zerops/**` (plus `textGeneration/**`, `usage/**`, which must
  reach providers only through `apps/server/src/spi/**`)

### `imported.lock`

- File: `/Users/macbook/Documents/Zerops-MCP/z3/imported.lock` — **7 lines, 2 path entries**.
- Format: JSON `{ upstream: <sha>, paths: { <path>: <git-tree-oid> } }`. The two entries are
  `packages/effect-codex-app-server` and `packages/effect-acp`.
- Tooling: **`scripts/imported-lock.ts`** (11,839 bytes) — CLI at `:283-341` accepting
  `--check` and `--write --upstream <ref>`; compares `git rev-parse <ref>:<path>` tree OIDs
  against the lock (`checkImportedLock` / `writeImportedLock`, `:247-281`).
- Test: **`scripts/imported-lock.test.ts`** (14,104 bytes) runs real `git` against a fixture
  repo (comment `:31-33`).

### CI

**One workflow only: `.github/workflows/ci.yml`**, triggered on `pull_request` and `push` to
`main`. **No tag trigger, no release/signing workflow anywhere in `.github/`.**

| Job | What it runs |
|---|---|
| `check` | `imported.lock` check → the zone-architecture test → ensure Electron → `vp check` → `vpr typecheck` → `vp run build:desktop` → verify-preload-bundle |
| `test` | `vp run --parallel --concurrency-limit 4 --filter '!t3' --filter '!@t3tools/monorepo' test` |
| `test_server` | matrix shard 1-3: `vp run --filter t3 test --shard N/3` (239 files, single-threaded per shard) |
| `rust` | `cargo fmt --check` + `cargo test` on `native/resource-monitor` |
| `mobile_native_changes` | path-detection gate (decides whether the macOS lint job runs) |
| `mobile_native_static_analysis` | needs the gate; `vp run lint:mobile` on macOS |
| `release_smoke` | `node scripts/release-smoke.ts` |

No branch-protection "required" markers are visible in the workflow file itself.

### Lint

**No `.oxlintrc.json`, no biome, no eslint config exists in the fork** (the only such configs on
disk live under the vendored, read-only `./.repos/*`). The entry point is root
`package.json:23`:

```
"lint": "vp lint --report-unused-disable-directives"
```

`vp` bundles `oxlint` and `oxfmt` inside the `vite-plus` npm package (its `package.json` `bin`
block declares `oxfmt`, `oxlint`, `vp`, `vpr`). A separate mobile-native lint sits at root
`package.json:24`: `"lint:mobile": "node scripts/mobile-native-static-check.ts"`
(`scripts/mobile-native-static-check.ts` 8,568 bytes + 5,083-byte test).

**`oxlint-plugin-t3code/index.ts:10-21`** registers the plugin `"t3code"` via
`@oxlint/plugins definePlugin`, with **6 rules**:

| # | Rule | Forbids |
|---|---|---|
| 1 | `namespace-node-imports.ts:47-51` | Non-canonical Node built-in imports; requires `NodeFS`-style namespace imports |
| 2 | `no-global-process-runtime.ts:46-50` | Direct `process.platform` / `process.arch` reads outside `packages/shared/src/hostProcess.ts` |
| 3 | `no-inline-schema-compile.ts:105-109` | Effect `Schema` decode/encode compiler calls inside function bodies (must hoist to module scope) |
| 4 | `no-manual-effect-runtime-in-tests.ts:83-87` | Manual Effect runtime execution (`runSync`/`runPromise`/…) in test files; use `@effect/vitest` |
| 5 | `no-mobile-uniwind-theme-escape-hatches.ts:57-61` | Raw Tailwind/Uniwind `dark:` / `light:` variant utilities in `apps/mobile/src/**` outside a reviewed allowlist; requires semantic theme classes |
| 6 | `no-native-title-tooltip.ts:10-15` | The native `title` attribute as a tooltip on intrinsic JSX elements; use the styled `Tooltip` component |

**Is there a lint for hard-coded colours?** **No.** None of the six rule files inspects hex or
colour literals. This is why `apps/mobile/src/features/threads/threadPresentation.ts` can carry
eight raw `#rrggbb` / `rgba()` literals (`:30-31`, `:58-59`, `:70-71`, `:82-83`, `:94-95`,
`:106-107`, `:122-123`) and pass lint.

**Is there a design-token lint?** **Partial, and mobile-only.** Rule 5 forces semantic Uniwind
*class names* over `dark:`/`light:` variants inside `apps/mobile/src/**`. Nothing equivalent
guards `apps/web`, and the rule does not look at literal values.

**Is there a vocabulary / wording lint?** **No.** No rule or script references banned words,
copy, or product vocabulary.

---

## 7. Test tooling

- `vp` is the **`vite-plus`** npm package ("The Unified Toolchain for the Web"), which supplies
  the `vp` and `vpr` binaries plus bundled `oxlint` / `oxfmt`.
- Root `package.json:30` `"test": "vp run -r test"` — recursively runs each workspace package's
  own `test` script. Companions: `:26` `"typecheck": "vp run -r --concurrency-limit 2
  typecheck"`, `:29` `"lint"`, `:31` `"test:resource-monitor"` (cargo),
  `:33` `"test:desktop-smoke": "vp run --filter @t3tools/desktop smoke-test"`,
  `:34-35` `"fmt"` / `"fmt:check"` (`vp fmt`).
- **`packages/client-runtime/vite.config.ts:1-9`** — the whole config:
  `import "vite-plus/test/config"; defineConfig({ test: { environment: "node", include:
  ["src/**/*.test.ts"] } })`.
- `apps/mobile/package.json:43` — `"test": "vp test run"`.
- `apps/web/package.json:12` — `"test": "vp test run --passWithNoTests --project unit"`;
  config at `apps/web/vite.config.ts`.
- `apps/desktop/package.json` — `test` plus a separate `smoke-test` script.

**Storybook / Ladle / component showcase for web: none.** `grep -rn 'storybook\|ladle'` over
every `package.json` and `scripts/*.ts` returns **zero** hits. Every `showcase` hit is the
mobile screenshot harness (`scripts/mobile-showcase*.ts`, `apps/mobile/src/features/showcase/`,
`apps/mobile/package.json` `"showcase"`). `apps/web` has no component gallery of any kind.

**`scripts/mobile-showcase.ts`** — invoked via root `"screenshots:mobile"` or
`apps/mobile` `"screenshots"`; boots real iOS Simulator / Android emulator instances against
locally spun `apps/server` fixture environments, walks scenes per device × theme × appearance
from `scripts/mobile-showcase.config.ts`, captures and validates store-compliant PNGs
(`finalizeCapture` / `validateStoreAsset`, `:242-274`), landing them at
`artifacts/app-store/screenshots/<device-store-dir>/<appearance>/<theme>/<scene>.png`
(`scripts/mobile-showcase.config.ts:98` `outputDirectory: "artifacts/app-store/screenshots"`;
path builder `scripts/mobile-showcase.ts:228-240`).

---

## 8. `scripts/` at the repo root

35 `.ts` files + `clean-tsgo-backups.mjs` + `lib/` (14 files) + `package.json` + `tsconfig.json`.

| Size | File |
|---:|---|
| 68,422 | `scripts/build-desktop-artifact.ts` |
| 49,763 | `scripts/mobile-showcase.ts` |
| 39,376 | `scripts/dev-runner.test.ts` |
| 29,960 | `scripts/dev-runner.ts` |
| 26,624 | `scripts/build-desktop-artifact.test.ts` |
| 26,141 | `scripts/mobile-showcase-environment.ts` |
| 25,729 | `scripts/export-brand-icons.ts` |
| 14,104 | `scripts/imported-lock.test.ts` |
| 12,444 | `scripts/release-smoke.ts` |
| 12,094 | `scripts/mobile-showcase.test.ts` |
| 11,839 | `scripts/imported-lock.ts` |
| 11,646 | `scripts/resolve-previous-release-tag.ts` |
| 11,303 | `scripts/update-release-package-versions.test.ts` |
| 10,867 | `scripts/sync-reference-repos.test.ts` |
| 10,843 | `scripts/z3-zone-architecture.test.ts` |
| 10,320 | `scripts/sync-reference-repos.ts` |
| 9,010 | `scripts/notify-discord-release.ts` |
| 8,707 | `scripts/merge-update-manifests.test.ts` |
| 8,568 | `scripts/mobile-native-static-check.ts` |
| 7,493 | `scripts/mobile-showcase.config.ts` |
| 6,999 | `scripts/resolve-nightly-release.ts` |
| 6,827 | `scripts/resolve-previous-release-tag.test.ts` |
| 6,683 | `scripts/announce-connect-ga.ts` |
| 6,114 | `scripts/notify-discord-release.test.ts` |
| 5,692 | `scripts/update-release-package-versions.ts` |
| 5,118 | `scripts/mock-update-server.ts` |
| 5,083 | `scripts/mobile-native-static-check.test.ts` |
| 4,976 | `scripts/resolve-nightly-release.test.ts` |
| 3,900 | `scripts/mock-update-server.test.ts` |
| 3,659 | `scripts/merge-update-manifests.ts` |
| 2,450 | `scripts/apply-web-brand-assets.ts` |
| 1,030 | `scripts/clean-tsgo-backups.mjs` |
| 801 | `scripts/sign-macos.test.ts` |
| 317 | `scripts/sign-macos.ts` |

`scripts/lib/`: `brand-assets.ts` (+test), `build-target-arch.ts` (+test),
`cli-external-packages.ts` (+test), `icon-export.ts` (+test), `public-config.ts` (+test),
`reference-repos.ts`, `update-manifest.ts`.

### Existing generators with `--check`

**Two generators plus one verifier:**

1. **`apps/mobile/scripts/generate-uniwind-themes.mts`** — note it lives in the *mobile package*,
   **not** in root `scripts/`. `--check` at `:246`; generates `apps/mobile/generated-uniwind-themes.css`
   (1374 lines), `apps/mobile/generated-uniwind-theme-names.json` (10 names),
   `apps/mobile/generated-uniwind-default-theme-variables.json` (light 65 / dark 65). Failure
   message: "`<file>` is stale. Run vp run --filter @t3tools/mobile generate." Test:
   `apps/mobile/scripts/generate-uniwind-themes.test.ts` (3 cases).
2. **`scripts/export-brand-icons.ts`** — exposed as root `package.json:15-16`
   `"icons:export": "node scripts/export-brand-icons.ts"` and
   `"icons:check": "node scripts/export-brand-icons.ts --check"`. Backed by
   `scripts/lib/icon-export.ts` (+test) and `scripts/lib/brand-assets.ts` (+test); the web-side
   applier is `scripts/apply-web-brand-assets.ts`.
3. **`scripts/imported-lock.ts`** — takes `--check` (and `--write --upstream <ref>`), but
   verifies rather than generates.

---

## 9. Docs

### `docs/` top level

```
docs/README.md
docs/ios-build-brief.md
docs/upstream-README.md
docs/architecture/      terminal-renderers.md
docs/operations/        mobile-app-store-screenshots.md, observability.md,
                        relay-observability.md, release.md
docs/user/              background-service.md, composer.md, install.md, keybindings.md,
                        mobile-appearance.md, permission-modes.md, project-settings.md,
                        providers-claude.md, providers-codex.md, remote-access.md,
                        source-control.md, thread-sidebar.md, updating.md, usage.md
docs/internals/         ci.md, connection-runtime.md, environment-auth.md, glossary.md,
                        overview.md, product-analytics.md, providers.md, remote.md,
                        resource-telemetry.md, scripts.md, server-updates.md,
                        t3-code-connect-auth-flow.html, t3-connect.md, work-artifacts.md,
                        workspace-layout.md, zerops/
```

### `docs/internals/zerops/`

| Size | File |
|---:|---|
| 327,977 | `verified.md` |
| 30,443 | `hacks.md` |
| 17,219 | `fork.md` |
| 14,986 | `poc-findings.md` |
| 12,668 | `spi.md` |
| 7,900 | `map.md` |
| 6,466 | `README.md` |
| 3,094 | `compat.md` |
| 3,053 | `questions.md` |
| 1,079 | `intake.md` |

### `docs/internals/zerops/design-system.md`

**Does not exist.** There is no design-system document anywhere under `docs/`.

### `AGENTS.md` — the fork's UI rules (161 lines)

**Fork override header, `:1-6`** (quoted in full):

> **This is `z3`, a Zerops hard fork of T3 Code.** Read [`CLAUDE.md`](CLAUDE.md) and
> [`docs/internals/zerops/fork.md`](docs/internals/zerops/fork.md) first. Two rules override
> upstream's guide below: never `git merge`/`rebase upstream/main` — the fork is frozen at tag
> `upstream-base-2026-08-28`; in the ported zone (`apps/server/src/provider/**`, provider
> contracts) keep edits minimal so future ports stay cheap. Where the two disagree about product
> name or scope, the rules above win.

**Animations — two places:**

- `:155` (under "Taste"): "Our users drive agents all day and notice a dropped frame, a lying
  spinner, and a stale label. **No continuously repainting animations; they peg the GPU on
  high-refresh displays.**"
- `:24` (under "Performance without compromise"): "We regularly audit for performance
  regressions, often caused by sending too much data over websockets, **css animations causing
  gpu spikes**, lists being hard to render, and more. Make sure all changes are considerate of
  performance impact."

**One-way doors — `:80`:**

> **Reverse states.** If you added a way in, add the way out and the way to see it. Snooze needs
> unsnooze. Close needs reopen. **A one-way door is a bug.**

**"Hit every surface" — `:72-82`** (the section header is literally "Hit every surface"):

> The most common defect in this repo is a change that works on the path you tested and is
> missing everywhere else. Before calling frontend work done, walk this list and say which
> entries applied:

- `:76` **Entry points.** "A behavior reachable from the chat view is usually also reachable from
  Settings, the command palette, and a keybinding. Fixing one is not fixing the feature."
- `:77` **Clients.** "Web, desktop (wraps web, adds Electron shell/IPC), and mobile (React
  Native, separate navigation). Shared logic lives in `packages/client-runtime`"
- `:78` **Providers.** "Codex, Claude, Cursor, Grok, and OpenCode each have an adapter.
  Provider-shaped features need a decision per adapter, even if the decision is 'not supported
  here'."
- `:79` **Contracts.** "Anything crossing the wire is typed in `packages/contracts`. Change the
  schema and the server, web, mobile, and desktop all follow."
- `:80` **Reverse states.** (quoted above)
- `:81` **Connection modes.** "Local, remote/relay, and tunnel behave differently. Multi-device
  and multi-environment cases are real."
- `:82` **Docs.** "`docs/` splits by audience. Behavior changes that a user would notice belong
  in `docs/user/` (shipped-product voice, no repo tooling or source paths); architecture and
  contributor changes in `docs/internals/`; runbooks in `docs/operations/`; new vocabulary in
  `docs/internals/glossary.md`."

**Tokens: AGENTS.md says nothing about design tokens, colours, palettes, or theming.** The only
token-adjacent enforcement in the repo is the oxlint rule
`no-mobile-uniwind-theme-escape-hatches` (§6), which is mobile-only.

**Other lines that bear on UI work:**

- `:32-38` the three surfaces. `:34` Web "is kind of two surfaces, as we have the public facing
  'app.t3.codes' as well as locally hosting the web app through the `npx t3` command."
  `:36` "**Desktop** is the main surface most users install first. It's a full Electron app that
  bundles the server runner as well. The desktop app can also be used as the host server,
  allowing remote connections from app.t3.codes or the mobile app." — **this is now false after
  S5-1** (see §3: the desktop no longer runs a local backend). `:38` "**Mobile** is a React
  Native app for both iOS and Android, available on the App Store and Google Play."
- `:152` "Complexity belongs at the adapter boundary. Orchestration stays pure, **UI stays
  dumb**."
- `:153` "Inferred types over annotations. `any` is the enemy."
- `:156` "If a rule here fights the task in front of you, say so loudly and get a human sign-off
  before breaking it."
- `:113` "**Do not run repo-wide checks.** No `vp check`, no `vp run -r test`, no `vp run -r
  typecheck` unless I ask. CI owns the full suite."
- `:116` "Upon request, user-visible frontend changes should get one integrated pass in a real
  client: `test-t3-app` for web, `test-t3-mobile` for mobile."
- `:123` "UI changes need before/after images. Motion or timing needs a short video."
- `:130` "Do not commit implementation plans, research notes, or agent scratch files."
- `:70` "**Baking in origins.** Never set `VITE_HTTP_URL` or `VITE_WS_URL` for dev."
- `:143-148` where code lives, incl. `:147` "`packages/client-runtime` - client code shared by
  web and mobile" and `:148` "`.repos/` - vendored read-only references… Never edit or import
  from them."

---

## Three facts worth carrying forward

1. **Mobile and desktop have zero Zerops-aware code.** Both have 0 imports of
   `@t3tools/client-runtime/zerops`; all 22 mobile "zerops" hits are comments on a Clerk token
   occupying a field named `zeropsToken`, explicitly deferred to "S5-3"
   (`apps/mobile/src/features/agent-awareness/remoteRegistration.ts:398-400` et al.). Mobile's
   only way onto a server today is host + pairing code / QR
   (`apps/mobile/src/features/connection/pairing.ts`,
   `ConnectionsNewRouteScreen.tsx:189-298`).
2. **There is no design-system doc and no colour or token lint outside the mobile-only Uniwind
   variant rule.** `apps/mobile/src/features/threads/threadPresentation.ts` hard-codes eight
   hex/rgba values and deliberately duplicates
   `apps/web/src/components/Sidebar.logic.ts::resolveThreadStatusPill`
   (`threadPresentation.ts:47`), with the widget
   (`apps/mobile/src/widgets/AgentActivity.tsx:22-29,69`) carrying a *third*, differently-named
   phase vocabulary. Nothing mechanical keeps the three in sync.
3. **The desktop is now a thin shell over the same hosted-static `apps/web` bundle Vercel
   serves**, mounted at `t3code://app` (`ElectronProtocol.ts:14-24`, `:55-59`,
   `build-desktop-artifact.ts:1408-1476`), with a CSP that already anticipates direct
   connections to Zerops environments and the Zerops API (`ElectronProtocol.ts:80-85`). Yet
   every brand constant across desktop (`DesktopEnvironment.ts:76,165-166,206-209`), mobile
   (`app.config.ts:65-88`), and packaging (`build-desktop-artifact.ts:35,1260-1261,1311,1344`)
   is still `T3 Code` / `t3code` / `com.t3tools.t3code`.
