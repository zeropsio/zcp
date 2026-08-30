_Current-checkout note:_ `z3/main` is at `6da740c68`, four commits beyond the brief’s `6b1b575d3`. The intervening changes touch only `ZeropsAgentAuthWatcher` among the reviewed areas, so the findings below still apply. I left the existing desktop-test modification untouched.

## 1. Order — verdict: adjust

Guard-first, token-before-primitives, and harness-before-surfaces are right. The dependency graph around them is too coarse.

- **F1 cannot honestly be “no product code changes.”** R2 needs subscriptions and commands separated; today they share [feeds.ts:27](/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/zerops/feeds.ts:27) and are exported together at [feeds.ts:126](/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/zerops/feeds.ts:126). R6 also starts with a map-row violation: [ZeropsServiceMap.tsx:11](/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/components/zerops/ZeropsServiceMap.tsx:11) imports `Spinner`. Either allow mechanical boundary/removal edits in F1 or admit that F1 only freezes exact baseline occurrences.

- **F2 should not globally block F5.** The plan makes the entire deletion programme a prerequisite at [foundations:142](/Users/macbook/Documents/Zerops-MCP/zcp/plans/z3-ui-foundations-2026-08-30.md:142). PR deletion is a full-stack stream; Connect deletion needs S5-5 coordination; Worktree needs a contract capability. None is required to build a `StatusDot` or token-only card frame. With the current edge, one unresolved deletion stalls all UI foundations.

- **F3 and F4 can run in parallel.** Runtime promotion is required before mobile screens and status consumers, but it does not precede palette projection logically. Keep semantic tone IDs in the status contract so F3 does not need concrete colors.

- **F4 must include a minimal generator.** Changing `MOBILE_DEFAULT_THEME_ID` does not recolor the mobile default: generated variants come only from `BUILT_IN_THEME_IDS` at [generate-uniwind-themes.mts:140](/Users/macbook/Documents/Zerops-MCP/z3/apps/mobile/scripts/generate-uniwind-themes.mts:140), while default values are read back from hand-maintained `global.css` at [generate-uniwind-themes.mts:201](/Users/macbook/Documents/Zerops-MCP/z3/apps/mobile/scripts/generate-uniwind-themes.mts:201). The default ID is deliberately outside that built-in set at [themePalettes.ts:1](/Users/macbook/Documents/Zerops-MCP/z3/packages/shared/src/themePalettes.ts:1). As written, F4 can say “zerops” while still rendering the old palette. Web also retains a manual boot copy at [index.html:15](/Users/macbook/Documents/Zerops-MCP/z3/apps/web/index.html:15), while runtime defaults still select T3 Chat at [themePalette.ts:1223](/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/themePalette.ts:1223). Deferring all generation makes R8 unavailable and breaks the claimed one-source token invariant.

- **Split F5 into harness and primitives.** Establish the scene schema, server fixture composition, and empty web-capture driver first; then primitives can populate it. Surface rounds should wait for both. Combining them into one L phase at [foundations:145](/Users/macbook/Documents/Zerops-MCP/zcp/plans/z3-ui-foundations-2026-08-30.md:145) creates unnecessary scheduling contention.

- **Remove `git clean` from F0.** A machine-local destructive cleanup is neither a reproducible repository gate nor safe agent guidance ([foundations:140](/Users/macbook/Documents/Zerops-MCP/zcp/plans/z3-ui-foundations-2026-08-30.md:140)). Replace it with an exact-path diagnostic and an owner-approved cleanup outside the programme.

**Concrete plan change:** replace the dependency paragraph with:

> F0 signs decisions and rule predicates. F1 lands syntactic guards plus exact, fingerprinted baselines and may perform mechanical module-boundary fixes. Preconditioned F2 deletion streams run independently and do not globally block primitives. F3 and F4 run in parallel; F4 includes the minimal web-boot/mobile-default projector and `--check`. F5a establishes the shared scene schema, fixture layers and showcase skeleton; F5b adds primitives. Surface rounds require F3 where they consume shared state, F4, F5a and the relevant F5b primitives. F6 remains coordinated with S4.

If the original order stands, the likely failures are a cosmetically green R2, stale mobile/default boot colors, status drift outside `client-runtime`, and UI work blocked behind unrelated full-stack deletions.

## 2. Fixture seam — verdict: adjust

A server layer swap is the right seam for **live feeds**, but it is not the sole source of all UI state. Use one scene artifact with two injection planes: Effect feed services and persisted thread projections.

### Layer composition

The current composition deliberately shares one auth instance with login:

- [zeropsFeedsLayer.ts:5](/Users/macbook/Documents/Zerops-MCP/z3/apps/server/src/zerops/zeropsFeedsLayer.ts:5) documents the exception.
- [zeropsFeedsLayer.ts:30](/Users/macbook/Documents/Zerops-MCP/z3/apps/server/src/zerops/zeropsFeedsLayer.ts:30) constructs auth once.
- [zeropsFeedsLayer.ts:32](/Users/macbook/Documents/Zerops-MCP/z3/apps/server/src/zerops/zeropsFeedsLayer.ts:32) uses `provideMerge`.
- [server.ts:455](/Users/macbook/Documents/Zerops-MCP/z3/apps/server/src/server.ts:455) explains why the feed layer must sit above runtime dependencies rather than beside them.

`Layer.mergeAll(FixtureAuth, AgentLogin.layer, …)` is wrong: sibling outputs do not satisfy sibling requirements. The shape must remain conceptually:

```ts
const auth = fixtureAgentAuthLayer(scene);
const login = fixtureAgentLoginLayer(scene).pipe(Layer.provideMerge(auth));

Layer.mergeAll(fixtureTopologyLayer(scene), fixtureLifecycleLayer(scene), login);
```

There is also a missing **fourth fixture service**. `subscribeZeropsAgentAuth` merges `ZeropsAgentAuth` with `ZeropsAgentLogin` at [ws.ts:2555](/Users/macbook/Documents/Zerops-MCP/z3/apps/server/src/ws.ts:2555). `mergeAgentAuthLogin` overwrites every row’s `login` property from the separate login service at [ZeropsAgentLogin.ts:94](/Users/macbook/Documents/Zerops-MCP/z3/apps/server/src/zerops/ZeropsAgentLogin.ts:94). A scripted auth snapshot containing `awaiting-browser` would therefore be replaced with `undefined` unless `ZeropsAgentLogin` is scripted too.

Publishers should be `Layer.scoped`, hold current state in `Ref`, publish changes through `PubSub`, and use the existing subscribe-before-snapshot pattern exposed by [ZeropsTopology.ts:66](/Users/macbook/Documents/Zerops-MCP/z3/apps/server/src/zerops/ZeropsTopology.ts:66), [ZeropsLifecycle.ts:52](/Users/macbook/Documents/Zerops-MCP/z3/apps/server/src/zerops/ZeropsLifecycle.ts:52), and [ZeropsAgentAuth.ts:186](/Users/macbook/Documents/Zerops-MCP/z3/apps/server/src/zerops/ZeropsAgentAuth.ts:186). Timers must use Effect `Clock` and scoped fibers so offline tests can use `TestClock` and shutdown cannot leak publishers.

### Result cards are a separate projection seam

The feed swap does not create `zerops_*` transcript activities:

- The server projects `payload.data.zerops` at [ActivityPayloadProjection.ts:355](/Users/macbook/Documents/Zerops-MCP/z3/apps/server/src/orchestration/ActivityPayloadProjection.ts:355) and again outside the MCP branch at [ActivityPayloadProjection.ts:407](/Users/macbook/Documents/Zerops-MCP/z3/apps/server/src/orchestration/ActivityPayloadProjection.ts:407).
- Web decodes exactly `data.zerops.{toolName,resultText,truncated}` at [activityResult.ts:36](/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/zerops/activityResult.ts:36), carries it into the timeline at [session-logic.ts:1015](/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/session-logic.ts:1015), and mounts the card at [MessagesTimeline.tsx:2595](/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/components/chat/MessagesTimeline.tsx:2595).
- The mobile harness starts a real server at [mobile-showcase.ts:607](/Users/macbook/Documents/Zerops-MCP/z3/scripts/mobile-showcase.ts:607), then seeds SQLite at [mobile-showcase.ts:1353](/Users/macbook/Documents/Zerops-MCP/z3/scripts/mobile-showcase.ts:1353).
- Its present activities are only generic command/file payloads at [mobile-showcase-environment.ts:557](/Users/macbook/Documents/Zerops-MCP/z3/scripts/mobile-showcase-environment.ts:557); none contains `data.zerops` or `resultText`.

Therefore the seeded-SQLite route **does not cover Zerops cards today**. Extend the common scene format with `threadActivities`, seed the exact projected shape into `projection_thread_activities`, and include at least one `zerops_delete`-style non-MCP `itemType` case. Because direct SQLite seeding bypasses the projector, retain targeted `ActivityPayloadProjection` tests for live, reconnect, and detail paths as required by [spec-z3.md:638](/Users/macbook/Documents/Zerops-MCP/zcp/docs/spec-z3.md:638).

Fixture mode must also start the showcase server with `T3CODE_ZEROPS_PROJECT_ID`. That variable is the sole Zerops-mode switch at [ZeropsEnvironment.ts:6](/Users/macbook/Documents/Zerops-MCP/z3/apps/server/src/zerops/ZeropsEnvironment.ts:6), and current showcase startup passes neither it nor fixture configuration at [mobile-showcase.ts:630](/Users/macbook/Documents/Zerops-MCP/z3/scripts/mobile-showcase.ts:630). A feed can publish while the descriptor, policy, and door still say “ordinary server” otherwise.

### Required file scope

Touch or add:

- `apps/server/src/config.ts`
- `apps/server/src/cli/config.ts`
- `apps/server/src/zerops/zeropsFeedsLayer.ts`
- a new `apps/server/src/zerops/ZeropsFixtureFeeds.ts` plus focused tests
- `scripts/mobile-showcase.ts`
- `scripts/mobile-showcase-environment.ts`
- the new `scripts/web-showcase.ts`
- a versioned scene-schema/scene directory

`server.ts` and the three live services should not need conditional fixture logic if selection remains encapsulated in `zeropsFeedsLayer.ts`.

**Concrete plan change:** replace “one mechanism” with “one versioned scene bundle, consumed by two production-shaped adapters: four server feed services and the thread-activity projection seeder.” State explicitly that fixture tests validate wire/subscription behavior, not the live topology/lifecycle reducers they replace. Raw WebSocket tests must Ack streamed chunks, per [spec-z3.md:667](/Users/macbook/Documents/Zerops-MCP/zcp/docs/spec-z3.md:667).

## 3. R1–R8 — verdict: adjust

The current architecture test scans only TS/TSX and import strings ([z3-zone-architecture.test.ts:21](/Users/macbook/Documents/Zerops-MCP/z3/scripts/z3-zone-architecture.test.ts:21), [z3-zone-architecture.test.ts:77](/Users/macbook/Documents/Zerops-MCP/z3/scripts/z3-zone-architecture.test.ts:77)). Oxlint has scopes but no type information ([vite.config.ts:127](/Users/macbook/Documents/Zerops-MCP/z3/vite.config.ts:127)). Predicates must reflect that.

| Rule | Verdict and evidence | Concrete predicate/change |
|---|---|---|
| **R1** | **Adjust.** Import bans are enforceable; “storage/fetch/clock are injected” is not. Raw searches for `window` also hit strings, comments and shadowed locals. | Zone test rejects exact module prefixes `react`, `react-dom`, `react-native`, `expo`, `expo-*`, `@effect/atom-react`. An Oxlint rule rejects references whose resolved scope is the global `window`, `document`, `localStorage` or `fetch`; property keys and locally bound parameters do not count. Constructor tests prove storage/clock/fetch dependencies are accepted explicitly. |
| **R2** | **Disagree as written.** Import specifiers contain no mutability metadata, and subscriptions plus commands are co-located in [feeds.ts:63](/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/zerops/feeds.ts:63). A component can import a harmless-looking local hook that mutates transitively. Conversely, the whole Zerops directory legitimately creates projects and restarts services at [ZeropsProjectsPage.tsx:257](/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/components/zerops/ZeropsProjectsPage.tsx:257), and session UI legitimately signs in/registers at [ZeropsSessionProvider.tsx:116](/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/zerops/ZeropsSessionProvider.tsx:116). | Split subscriptions from command modules. Define named protected roots—map, band, result cards and quick actions—not the whole door/picker directory. Recursively resolve local imports from each root and reject imports of command constructors, platform API clients and an exact reviewed set of write modules. For `WS_METHODS`, compare against an explicit read/allowed-command set; do not infer mutability from `subscribe` naming. |
| **R3** | **Adjust.** A raw string scan will misclassify SVG/XML, test data and technical palettes. Existing infrastructure already grants whole-file exceptions at [no-mobile-uniwind-theme-escape-hatches.ts:10](/Users/macbook/Documents/Zerops-MCP/z3/oxlint-plugin-t3code/rules/no-mobile-uniwind-theme-escape-hatches.ts:10). Legitimate technical literals include the ANSI-16/Pierre terminal table at [terminalTheme.ts:21](/Users/macbook/Documents/Zerops-MCP/z3/apps/mobile/src/features/terminal/terminalTheme.ts:21), Shiki theme imports at [shikiReviewHighlighter.ts:10](/Users/macbook/Documents/Zerops-MCP/z3/apps/mobile/src/features/review/shikiReviewHighlighter.ts:10), and embedded SVG asset colors at [pierre-icons.ts:14](/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/pierre-icons.ts:14). | In JS/TS, inspect color literals only in semantic sinks: style keys, `StyleSheet.create`, CSS-variable objects, and SVG `fill`/`stroke`/`stopColor`/`color`; never `d`, `points`, `viewBox`, IDs or URL fragments. Inspect Tailwind palette utilities only as complete class tokens with a known color-property prefix. Inspect `dark:`/`light:` only in class-like literals. Parse CSS declarations separately; Oxlint does not cover `.css`. Technical exceptions are exact AST/declaration fingerprints, not files. Include the named status/widget consumers—the current `features/zerops/**` glob would miss [AgentActivity.tsx:81](/Users/macbook/Documents/Zerops-MCP/z3/apps/mobile/src/widgets/AgentActivity.tsx:81) and [threadPresentation.ts:28](/Users/macbook/Documents/Zerops-MCP/z3/apps/mobile/src/features/threads/threadPresentation.ts:28). |
| **R4** | **Adjust.** “JSX text + string attributes” is viable only with a closed list of UI sinks. A per-file fallback exception can hide unrelated future vocabulary. The manual path genuinely contains the forbidden terms throughout [PairingRouteSurface.tsx:112](/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/components/auth/PairingRouteSurface.tsx:112). | Check `JSXText` plus values of `aria-label`, `title`, `placeholder`, `alt`, `label`, `description` and explicitly registered copy tables. Use exact word-boundary patterns. Move fallback copy to a named component and allow exact normalized literals/sinks with owner and reason; identifiers, imports, comments and an entire fallback file are never exempt. |
| **R5** | **Disagree.** “No other exported table” is trivially evaded by an inline switch. The relay already has a private `statusForPhase` switch at [AgentActivityPublisher.ts:184](/Users/macbook/Documents/Zerops-MCP/z3/infra/relay/src/agentActivity/AgentActivityPublisher.ts:184), while shared logic emits another headline vocabulary at [agentAwareness.ts:119](/Users/macbook/Documents/Zerops-MCP/z3/packages/shared/src/agentAwareness.ts:119). The widget cannot import a module-scope table because its function is serialized and must remain self-contained ([AgentActivity.tsx:50](/Users/macbook/Documents/Zerops-MCP/z3/apps/mobile/src/widgets/AgentActivity.tsx:50)). | Put the canonical phase/status resolver in `packages/shared`, with a client-runtime presentation adapter. Run one vector table through web row, rollup pill, mobile row and relay aggregation. Pass resolved status/tint data into widget props; do not require the serialized widget to import the table. Architecture checks may ban known local status-table shapes in named consumers, but parity is a behavioral test. Keep lifecycle-strip phrase resolution separate unless it has the same input domain. |
| **R6** | **Adjust.** Search uses, not `@keyframes` definitions. `steps()` alone is gameable (`steps(120)`), and “duty cycle” has no numeric meaning. Current continuous uses include [index.css:491](/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/index.css:491), [index.css:2479](/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/index.css:2479), [LoadingStrip.tsx:43](/Users/macbook/Documents/Zerops-MCP/z3/apps/mobile/src/components/LoadingStrip.tsx:43), and [ConnectionStatusDot.tsx:46](/Users/macbook/Documents/Zerops-MCP/z3/apps/mobile/src/features/connection/ConnectionStatusDot.tsx:46). | CSS parser visits `animation`/`animation-iteration-count` declarations containing the exact `infinite` keyword and resolves the referenced keyframe; definitions alone do not fail. Permit only a reviewed low-frequency stepped helper with a cap on steps and a minimum hold, or require finite repeats. Oxlint resolves `withRepeat` to its Reanimated import and rejects count `-1` except an approved, tested duty-cycle helper. `Spinner` checks resolve the imported binding—including aliases—and flag JSX use only in named map/band roots. |
| **R7** | **Agree, with precision.** This is a pure shared test over the role list at [themePalettes.ts:17](/Users/macbook/Documents/Zerops-MCP/z3/packages/shared/src/themePalettes.ts:17). | Assert exact key equality, not merely coverage; both appearances; parsed alpha exactly 1. Name actual pairs such as `accentForeground/accent`, `accentSurfaceForeground/accentSurface`, `messageActionForeground/messageAction`, and each semantic foreground/surface. Do not contrast indicator colors as if they were text. Also assert generated boot/default projections equal their source roles. |
| **R8** | **Agree only after generation exists.** The mobile generator already has a byte-check mechanism at [generate-uniwind-themes.mts:245](/Users/macbook/Documents/Zerops-MCP/z3/apps/mobile/scripts/generate-uniwind-themes.mts:245), but it does not generate the default palette. | Move a minimal complete projector into F4 and make R8 active there. If generation remains late, label R8 “deferred,” not a guard landed by F1. |

Replace count-monotonic allowlists with entries containing path, AST/declaration kind, normalized fingerprint, owner, reason, expiry/removal phase. CI should fail on new, changed **and dead** entries; equal counts can otherwise exchange one violation for another indefinitely.

## 4. Deletions — verdict: adjust

| Deletion | Verdict | Evidence and concrete plan change |
|---|---|---|
| **PR workflow** | **Needs a precondition; under-scoped.** | `pullRequest.ts` is imported by the environment HTTP contract at [environmentHttp.ts:37](/Users/macbook/Documents/Zerops-MCP/z3/packages/contracts/src/environmentHttp.ts:37) and owns an HTTP group at [environmentHttp.ts:554](/Users/macbook/Documents/Zerops-MCP/z3/packages/contracts/src/environmentHttp.ts:554). Server composition includes its HTTP layer at [server.ts:475](/Users/macbook/Documents/Zerops-MCP/z3/apps/server/src/server.ts:475), and WS/auth have the full method set ([RpcAuthorization.ts:57](/Users/macbook/Documents/Zerops-MCP/z3/apps/server/src/auth/RpcAuthorization.ts:57)). Client-runtime exports it to web and mobile; mobile uses linked detail at [use-thread-pr.ts:1](/Users/macbook/Documents/Zerops-MCP/z3/apps/mobile/src/state/use-thread-pr.ts:1). More importantly, thread PR metadata is a separate orchestration contract at [orchestration.ts:429](/Users/macbook/Documents/Zerops-MCP/z3/packages/contracts/src/orchestration.ts:429) and [orchestration.ts:843](/Users/macbook/Documents/Zerops-MCP/z3/packages/contracts/src/orchestration.ts:843), with separate capabilities at [environment.ts:59](/Users/macbook/Documents/Zerops-MCP/z3/packages/contracts/src/environment.ts:59). Split “embedded review workspace” from “external change-request reference/status/deep link” and decide the latter explicitly before deletion. `providerRuntime.ts` has no PR-specific fields to remove: its request options are generic provider approvals at [providerRuntime.ts:433](/Users/macbook/Documents/Zerops-MCP/z3/packages/contracts/src/providerRuntime.ts:433) and must remain. Do not use typecheck alone; add projection/thread-reducer compatibility tests. |
| **LegacySidebar** | **Web safe after D1; mobile needs its own precondition.** | Web defaults to the new sidebar at [AppSidebarLayout.tsx:229](/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/components/AppSidebarLayout.tsx:229), but the flag also changes `chat.new` behavior at [_chat.tsx:92](/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/routes/_chat.tsx:92), and the setting spans schema and patch contracts at [settings.ts:229](/Users/macbook/Documents/Zerops-MCP/z3/packages/contracts/src/settings.ts:229) and [settings.ts:941](/Users/macbook/Documents/Zerops-MCP/z3/packages/contracts/src/settings.ts:941). Mobile has a separate device preference and two implementations at [mobile-preferences.ts:35](/Users/macbook/Documents/Zerops-MCP/z3/apps/mobile/src/persistence/mobile-preferences.ts:35) and [threadListV2.ts:140](/Users/macbook/Documents/Zerops-MCP/z3/apps/mobile/src/features/threads/threadListV2.ts:140). Make web and mobile separate rows; explicitly retain the new sidebar’s multi-project command-palette behavior and delete the mobile legacy branches/preferences only after naming `thread-list-v2-items.tsx` as the survivor. |
| **Worktree option** | **Wrong in F2; DN3 is correct.** | The selector has no capability input and always constructs/offers Worktree at [BranchToolbarEnvModeSelector.tsx:22](/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/components/BranchToolbarEnvModeSelector.tsx:22) and [BranchToolbarEnvModeSelector.tsx:124](/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/components/BranchToolbarEnvModeSelector.tsx:124). The server deliberately supports worktrees off Zerops and forbids them on Zerops at [ZeropsPolicy.ts:55](/Users/macbook/Documents/Zerops-MCP/z3/apps/server/src/zerops/ZeropsPolicy.ts:55), but the environment descriptor has no worktree capability ([environment.ts:48](/Users/macbook/Documents/Zerops-MCP/z3/packages/contracts/src/environment.ts:48)). Replace the F2 deletion with DN3’s `worktreesAllowed`/`threadEnvironmentModes` capability and filter client options/defaults. Unconditional removal is valid only after manual/non-Zerops connections are impossible. |
| **`cloud/` and Connect routes** | **Blanket deletion is wrong; routes need S5-5 coordination.** | The fork explicitly keeps the activity relay at [fork.md:74](/Users/macbook/Documents/Zerops-MCP/z3/docs/internals/zerops/fork.md:74), and S5-4b remains open at [spec-z3.md:1040](/Users/macbook/Documents/Zerops-MCP/zcp/docs/spec-z3.md:1040). Web runtime still depends on `dpop`, `managedRelayLayer` and relay config at [lib/runtime.ts:11](/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/lib/runtime.ts:11); `linkEnvironment.ts` says it is the activity-only Zerops path at [linkEnvironment.ts:191](/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/cloud/linkEnvironment.ts:191). Keep those. Conversely, `/connect` is explicitly the old hosted CLI OAuth handshake at [connectCliAuth.ts:25](/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/cloud/connectCliAuth.ts:25), and the still-live server CLI prints that route via [connectAuth.ts:41](/Users/macbook/Documents/Zerops-MCP/z3/packages/shared/src/connectAuth.ts:41). Delete the routes/auth surface only in the same slice that removes or disables the S5-5 CLI entry points; otherwise the server advertises a dead flow. |
| **Web Clerk while mobile Clerk stays** | **Needs the Connect/S5-5 precondition, not the mobile one.** | Web Clerk currently wraps the app at [main.tsx:29](/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/main.tsx:29) and supplies the old relay/Connect session at [managedAuth.tsx:39](/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/cloud/managedAuth.tsx:39). The retained web activity-link function already accepts a Zerops token directly at [linkEnvironment.ts:197](/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/cloud/linkEnvironment.ts:197), so web does not need to wait for mobile S5-3. Mobile still explicitly labels its Clerk token as temporary until S5-3 at [remoteRegistration.ts:397](/Users/macbook/Documents/Zerops-MCP/z3/apps/mobile/src/features/agent-awareness/remoteRegistration.ts:397). Plan separate package deletions: remove web Clerk with Connect CLI reach while preserving the direct-Zerops activity-link path; leave `@clerk/expo` until S5-3. |

## 5. Missing foundations — verdict: adjust

### Make “hit every surface” checkable

The UI brief omits connection modes at [foundations:187](/Users/macbook/Documents/Zerops-MCP/zcp/plans/z3-ui-foundations-2026-08-30.md:187), even though the repository rule includes them at [AGENTS.md:72](/Users/macbook/Documents/Zerops-MCP/z3/AGENTS.md:72). Manual fallback remains a real mode until it is deleted.

Add a checked-in machine-readable surface manifest keyed by feature ID with:

- entry points
- web/desktop/mobile
- provider applicability
- connection modes
- contracts/projections
- reverse states
- docs
- required test and capture IDs
- explicit `n/a` reason

The test can prove completeness of declared coverage, not that every UI is visually correct. That is a realistic guard for agent-maintained work.

### Replace source-presence “seam tests” with behavior tests

“Each seam gets a named test” is realistic only after extracting a small adapter; mounting or snapshotting [ChatView.tsx](/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/components/ChatView.tsx:1) at 7,417 lines is not.

Concrete assertions:

- **ChatView:** extract a `resolveZeropsChatChrome`/small host component. Assert topology availability exposes the `zerops` right-panel kind, unavailable hides it, lifecycle receives the active scoped thread, and auth renders only for attention. Both inline and sheet paths must consume the same availability value; they are currently duplicated at [ChatView.tsx:7320](/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/components/ChatView.tsx:7320) and [ChatView.tsx:7358](/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/components/ChatView.tsx:7358).

- **MessagesTimeline:** build a row vector containing an ordinary tool, each milestone kind, malformed Zerops data and an agent-spawn row. Assert selected milestones remain outside all three hiding mechanisms—turn `hiddenEntryIds`, group summary, overflow—while mount/subdomain remain folded and named. The three mechanisms are identified at [redesign concept:528](/Users/macbook/Documents/Zerops-MCP/zcp/plans/z3-ui-redesign-concept-2026-08-29.md:528); the current settled-only card mount is [MessagesTimeline.tsx:2595](/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/components/chat/MessagesTimeline.tsx:2595).

- **Sidebar/status:** one canonical state-vector test should exercise full web row, rollup pill, mobile row and relay aggregate. Current web labels differ even internally—`Pending Approval` in [Sidebar.logic.ts:128](/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/components/Sidebar.logic.ts:128) versus `Approval` in [Sidebar.tsx:868](/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/components/Sidebar.tsx:868)—and mobile says `Needs Approval` at [threadPresentation.ts:52](/Users/macbook/Documents/Zerops-MCP/z3/apps/mobile/src/features/threads/threadPresentation.ts:52).

- **Right panel:** test an exhaustive `RightPanelKind → availability/content/migration` adapter. The union is centralized at [rightPanelStore.ts:29](/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/rightPanelStore.ts:29), but rendering remains a long conditional in `ChatView`.

- **Door:** test the descriptor/bootstrap-method matrix through the one proposed resolver; do not keep one assertion per route file.

### Add consumer-driven contract fixtures

Typechecking detects incompatible types, not optional/default/discriminant changes that compile and alter presentation. Every showcase scene should:

1. decode with the public Effect schemas;
2. run through web, mobile and relay presentation adapters;
3. assert each known discriminant has a visible state or explicit total fallback;
4. include missing optional fields and one unknown/newer shape;
5. fail when a contract changes without scene migration.

This is especially important for card fallback semantics documented at [activityResult.ts:10](/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/zerops/activityResult.ts:10) and for auth/login composition.

### Tighten allowlist lifecycle

The current whole-file mobile allowlist demonstrates the rot risk ([no-mobile-uniwind-theme-escape-hatches.ts:10](/Users/macbook/Documents/Zerops-MCP/z3/oxlint-plugin-t3code/rules/no-mobile-uniwind-theme-escape-hatches.ts:10)). Add exact fingerprints, owners, reasons, expiry phases and dead-entry detection. A non-growing count is insufficient.

### Do not make static markup the sole primitive test

Static markup is useful for passive web anatomy but cannot establish focus order, keyboard behavior, event transitions, reduced motion, or React Native/native-container behavior. Amend [foundations:165](/Users/macbook/Documents/Zerops-MCP/zcp/plans/z3-ui-foundations-2026-08-30.md:165) to require:

- pure state/presentation tables,
- interaction and accessibility tests where applicable,
- static markup for passive web variants,
- shared-scene captures for visual review.

## Top 5 changes

1. Split the fixture design into four correctly composed feed services plus a thread-projection seeder, all driven by one versioned scene bundle.
2. Rewrite F2 around explicit deletion preconditions: preserve activity-relay `cloud/`, coordinate Connect/Clerk with S5-5, and implement Worktree as a server capability.
3. Replace R2/R3/R5/R6 policy prose with exact AST/module-graph/behavioral predicates and fingerprinted exceptions.
4. Move the minimal web-boot/mobile-default theme projector and `--check` into F4; “generator last” is not compatible with one-source tokens.
5. Put status semantics in a genuinely shared resolver, pass resolved widget presentation across its serialization boundary, and add the machine-readable surface/seam contract matrix.
