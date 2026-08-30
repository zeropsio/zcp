# z3 UI foundations — how the clients get built (plan)

Date: 2026-08-30. Owner: Karel. Status: **proposal v2 — Codex second opinion folded in (§1a);
nothing implemented**. Inputs: the redesign concept `plans/z3-ui-redesign-concept-2026-08-29.md`
(+ its `research/`), `docs/spec-z3.md` §5/§7–§9, `../z3/docs/internals/zerops/fork.md`, the day-2
handoff `plans/z3-handoff-2026-08-29.md`, the ledger rows of 2026-08-30, two fresh code maps of the
fork (web; mobile/desktop/shared — scratchpad, not committed) taken at `main` `6b1b575d3`, and the
Codex review taken at `07c3d0d8c` (six commits later — the CI fixes; nothing reviewed here moved).

Owner's framing (2026-08-30): the visual direction of the concept is accepted; **the UI/UX and the
flows will differ** from the concept's screens. So this plan lays the parts that survive any flow
change and defers every screen. It is transient; what survives goes to `docs/spec-z3.md` (a new
"client design system" section) and to tests.

---

## 0. Summary

**Thesis.** Foundations first, surfaces later — and the foundations are exactly the things no flow
change can invalidate: one token source with its projections generated, one UI-free logic layer
shared by three clients, one status resolver shared by four consumers, a vocabulary of primitives
per client, a machine-checked rulebook with fingerprinted exceptions, a deterministic harness that
renders any state from one scene bundle, behaviour-tested seams into T3's chrome, and a tree with
the dead T3 product removed under explicit preconditions. A screen is then a spec row + a scene +
a slice, not an architecture decision.

**What this changes against the concept's P0–P5 order** (§9 there):

1. **Guards before pixels.** The fork already has the machinery (`oxlint-plugin-t3code`, six rules
   incl. a mobile theme-escape-hatch rule; `scripts/z3-zone-architecture.test.ts` in CI). The
   rules land first, with exact fingerprinted baselines — not in P4.
2. **Shared logic before mobile, in parallel with tokens.** `packages/client-runtime/src/zerops/`
   exists (1,846 lines); 13 UI-free modules still sit in `apps/web/src/zerops/`. Moving them (=
   S5-2) gates every mobile screen; it does not gate the palette.
3. **A minimal token projector is part of the token phase**, not "generator last": the mobile
   default palette is a hand-maintained `global.css` block and the web boot palette a hand copy
   in `index.html` — changing the default theme id alone recolours nothing (§1a). The full
   six-target generator stays late; the two projections that make "one source" true do not.
4. **Delete under preconditions, not before everything.** ~25 k lines of T3 product are dead in a
   Zerops-only client, but two of the deletions reach live paths (the activity relay under
   `cloud/`, the `/connect` CLI handshake still printed by the server) and one is a capability, not
   a deletion (Worktree). Deletions run as their own streams and block nothing else.
5. **Nothing visual is decided here.** Landing, picker, band, map default-open, fold escape,
   question/credential cards, mobile screens — every one is a surface spec written when the
   owner fixes the flow (§5).

Phases F0–F6 (§4), then surface rounds (§5). Decisions: D0/D1/D9 confirmed; D10 adjusted; nine new
ones (§7).

---

## 1. What the fresh code maps changed (state at `main` `6b1b575d3` → `07c3d0d8c`)

| Finding | Evidence | Consequence |
|---|---|---|
| The web Zerops zone has **zero** colour literals; the 225 Tailwind palette utilities are in T3 chrome | `ResourceTelemetryDiagnostics` 38 · `Sidebar.tsx` 34 · `Sidebar.logic.ts` 32 · `pullRequestPresentation` 21 · `ChatMarkdown` 15 · `ThreadStatusIndicators` 14 · `RightPanelTabs` 8; zone dirs 0 | zero tolerance in the zone is free today; the rest is a fingerprinted baseline |
| The zone test covers the **server only** (4 rules, regex over import specifiers, TS/TSX only) | `scripts/z3-zone-architecture.test.ts:21,77,99-266` | client rules are new rows in the same file; anything needing scope or CSS goes to oxlint / a CSS parser |
| Custom oxlint plugin, no type-aware lint | `oxlint-plugin-t3code/`, `vite.config.ts` `lint` (`typeAware:false`, `typeCheck:false`); the mobile theme rule uses a **whole-file** allowlist (`no-mobile-uniwind-theme-escape-hatches.ts:10`) — the rot pattern to avoid | predicates must be syntactic/scoped, exceptions must be fingerprints, never files |
| **Four thread-status vocabularies** held together by comments | web `Sidebar.tsx:848-897` (*Approval*), web `Sidebar.logic.ts:128-141` (*Pending Approval*), mobile `threadPresentation.ts:49-128` (*Needs Approval*, **8 hex/rgba literals**), widget `AgentActivity.tsx:22-29` (`AgentActivityPhase`, serialized — cannot import a table, `:50`); plus the relay's private `statusForPhase` (`infra/relay/.../AgentActivityPublisher.ts:184`) and `packages/shared/src/agentAwareness.ts:119` | the canonical resolver belongs in `packages/shared` (the relay is a consumer too); the widget receives resolved props |
| `client-runtime/src/zerops/` exists; web imports it from 19 sites; **mobile 0, desktop 0** | `packages/client-runtime/src/zerops/index.ts` | the promotion is completion, not creation |
| Subscriptions and commands are co-located in the web feed module | `apps/web/src/zerops/feeds.ts:27,63,126` | a "no mutating RPC" rule needs the split first — a mechanical boundary fix, part of F1 |
| The feeds compose with `provideMerge`, sharing one `ZeropsAgentAuth` with `ZeropsAgentLogin`; `ws.ts:2555` merges auth + login and overwrites every row's `login` | `apps/server/src/zerops/zeropsFeedsLayer.ts:5-32`, `ZeropsAgentLogin.ts:94`, `server.ts:455` | a fixture layer must script **four** services and keep the `provideMerge` shape |
| `zerops_*` results reach cards via the projection (`payload.data.zerops`), not via the feeds; the mobile showcase seeds only generic activities | `ActivityPayloadProjection.ts:355,407`; `activityResult.ts:36`; `session-logic.ts:1015`; `MessagesTimeline.tsx:2595`; `mobile-showcase-environment.ts:557` | cards need a projection seeder in the scene bundle; the feed swap alone shows no card |
| Zerops mode on the server is one env var | `ZeropsEnvironment.ts:6` (`T3CODE_ZEROPS_PROJECT_ID`); the showcase startup passes neither it nor fixtures (`mobile-showcase.ts:630`) | fixture mode must set it, or the door/policy/descriptor say "ordinary server" |
| No web gallery/browser project; web tests are `renderToStaticMarkup`; mobile has a full seeded showcase | `apps/web/vite.config.ts:83-94`; `scripts/mobile-showcase{,-environment,.config}.ts`, `apps/mobile/src/features/showcase/` | the web harness copies the mobile pattern; static markup is not the only primitive test |
| Mobile's default palette is **not** driven by the theme engine | `generate-uniwind-themes.mts:140` (variants only for `BUILT_IN_THEME_IDS`), `:201` (defaults read back from hand-written `global.css`); `themePalettes.ts:1-4` keeps `t3-code` outside the built-in set; web boot copy `index.html:15-50`, runtime default `themePalette.ts:1223` | "ZEROPS_THEME as default" needs a projector for the mobile default block and the web boot snippet — F4 |
| Dead T3 product still on `main`, with different preconditions each | PR: web `components/pullRequest/` 14,066 + route 1,969 + `state/pullRequests.ts`; server `apps/server/src/pullRequest/**` + `ws.ts` + `auth/RpcAuthorization.ts:57` + `server.ts:475`; contracts `pullRequest.ts` 1,119 imported by `environmentHttp.ts:37,554`, **thread PR metadata is a separate orchestration contract** (`orchestration.ts:429,843`, `environment.ts:59`); mobile `state/use-thread-pr.ts` · `LegacySidebar.tsx` 3,600 behind `settings.legacySidebarEnabled` (also changes `chat.new` at `_chat.tsx:92`; contracts `settings.ts:229,941`); mobile thread-list v2 behind a device preference (`mobile-preferences.ts:35`, `threadListV2.ts:140`) · `cloud/`: `linkEnvironment.ts:191,197` is the **live activity-relay path** (Zerops token accepted), `lib/runtime.ts:11` needs `dpop`/`managedRelayLayer`; `/connect` = the old hosted CLI OAuth handshake (`connectCliAuth.ts:25`) still printed by the server CLI (`packages/shared/src/connectAuth.ts:41`) · web Clerk wraps the app (`main.tsx:29`) and feeds the old relay session (`cloud/managedAuth.tsx:39`); mobile Clerk explicitly temporary until S5-3 (`remoteRegistration.ts:397`) · Worktree option offered unconditionally (`BranchToolbarEnvModeSelector.tsx:22,124`) while the server forbids it on Zerops (`ZeropsPolicy.ts:55-65`, `decider.ts:294,384,856`, `ws.ts:913-917,1068`) and the environment descriptor carries no such capability (`environment.ts:48`) · `vercel.ts`, `announce-connect-ga.ts`, `SidebarStageBackdrop.tsx` | one decision row per item with its precondition (§7) |
| Brand constants are T3 on every client | desktop `DesktopEnvironment.ts:76,165-166,206-209`, `build-desktop-artifact.ts:35,1260-1261,1311,1344`; mobile `app.config.ts:65-88`; `docs/ios-build-brief.md:118` — never rename `t3code://` casually | naming is a slice set with a fixed keep-list |
| S7 web has landed (`ZeropsAgentAuthCard` in the panel and as a `ChatView.tsx:6981` overlay); desktop is the hosted-static bundle on `t3code://app` with a Zerops-ready CSP | web map §8; `ElectronProtocol.ts:14-24,55-59,80-85` | no structural desktop work; the card is the mint tray's occupant |
| CI: the four red findings of 2026-08-30 are fixed on `main` (`bb866a93a`…`07c3d0d8c`) | `git log 6b1b575d3..07c3d0d8c` | F0 verifies a green run rather than fixing one |
| `packages/ssh/`, `packages/tailscale/` survive on the laptop as untracked shells | `git ls-files` empty; `ls` succeeds; the release-smoke failure came from this | a CI-side exact-path diagnostic, not a cleanup step in the programme |
| No `design-system.md`; `AGENTS.md` says nothing about tokens and still claims the desktop hosts a server | `AGENTS.md:32-38` | F0 writes the ledger file and corrects the banner |

### 1a. Codex corrections (2026-08-30), applied

| Codex finding | Change in this plan |
|---|---|
| F1 cannot be "no product code" — R2 needs subscriptions split from commands (`feeds.ts:27,126`), R6 starts red (`ZeropsServiceMap.tsx:11` imports `Spinner`) | F1 lands syntactic guards + fingerprinted baselines **and may perform mechanical boundary fixes** (the split, the Spinner) |
| F2 must not globally block F5; each deletion has its own precondition | F2 is a set of independent, preconditioned streams; F5 depends on F1/F3/F4 only |
| F3 and F4 are independent if the status contract carries semantic tone ids | F3 ∥ F4; `threadStatus` emits tone ids, `brand.ts` maps tone → colour |
| "Generator last" contradicts "one source": the mobile default is hand-written CSS, the web boot palette a hand copy; R8 would never land | F4 includes a **minimal projector** (mobile default `@variant light/dark` block + web `index.html` boot snippet) with `--check`; the six-target generator stays late |
| F5 mixes harness and primitives; `git clean` in F0 is a machine-local destructive step | F5 split into F5a harness / F5b primitives / F5c seam adapters; `git clean` replaced by a CI diagnostic |
| A feed-layer swap is right for live feeds but needs **four** scripted services (`ZeropsAgentLogin` overwrites `login`), `Layer.provideMerge` not `mergeAll`, `Layer.scoped` + `Ref` + `PubSub` + `Clock`, and it does **not** produce `zerops_*` cards — those come from the thread-activity projection | "one versioned scene bundle, two production-shaped adapters": four fixture feed services + a `projection_thread_activities` seeder in the exact projected shape (incl. a non-MCP `itemType` case); `T3CODE_ZEROPS_PROJECT_ID` set in fixture mode; fixture tests validate wire/subscription behaviour, not the reducers they replace |
| R2 "by import specifier" is gameable and over-broad (the picker legitimately creates projects, `ZeropsProjectsPage.tsx:257`; the session provider signs in, `ZeropsSessionProvider.tsx:116`) | R2 = named protected roots (map, band, cards, quick actions) + recursive local-import resolution + an explicit read/allowed-command set for `WS_METHODS` |
| R3 raw scans misclassify SVG path data, ANSI tables, shiki; `features/zerops/**` misses the named status consumers | R3 inspects semantic sinks only, CSS via a parser, exceptions are AST fingerprints; named consumers (`threadPresentation.ts`, `AgentActivity.tsx`) are in scope |
| R4 needs a closed sink list; a whole-file fallback exemption hides future drift (`PairingRouteSurface.tsx:112`) | closed sink list, word-boundary patterns, fallback copy moved into a named component with exact-literal exemptions |
| R5 "no second exported table" is evaded by an inline switch; the widget cannot import a table (serialized, `AgentActivity.tsx:50`); the relay has its own switch | canonical resolver in `packages/shared`, client-runtime presentation adapter, resolved props into the widget, one vector table run through web row / pill / mobile row / relay — parity is behavioural |
| R6 must inspect `animation` **uses**, not keyframe definitions; `steps()` alone is gameable; `withRepeat(-1)` must be resolved to its import | CSS-parser predicate over `animation`/`animation-iteration-count` with `infinite`; capped stepped helper; oxlint rule resolving `withRepeat`; `Spinner` by resolved binding in the protected roots |
| R7: assert exact key equality, alpha = 1, the actual role pairs; do not contrast indicator colours as text; boot projections equal their source | as stated in R7 |
| Count-monotonic allowlists swap one violation for another forever | entries carry path · AST kind · normalized fingerprint · owner · reason · expiry phase; CI fails on new, changed **and dead** entries |
| PR deletion is under-scoped and needs a decision: "embedded review workspace" vs "external change-request reference/status/deep link" (separate orchestration contract) | DN1 split into DN1a (review workspace — delete) and DN1b (external PR reference on threads — owner decides) |
| `cloud/` blanket deletion is wrong (activity relay kept per `fork.md`, S5-4b open); `/connect` routes go only with the S5-5 CLI entry points; web Clerk goes with Connect reach, not with mobile Clerk | F2 rows rewritten with those preconditions |
| Worktree: capability, not deletion; the descriptor has no such field | DN3 = a contract field on the environment descriptor; the client filters options and defaults |
| LegacySidebar: web safe after D1 but the flag also drives `chat.new`; mobile is a separate preference with its own survivor | DN2a web / DN2b mobile |
| "Each seam gets a named test" is unrealistic against `ChatView.tsx` (7,417 lines) | F5c extracts small adapters (`resolveZeropsChatChrome`, right-panel kind adapter, timeline row-vector, status state-vector, door resolver) and tests **behaviour** |
| Missing: a machine-readable surface manifest (incl. connection modes — the manual fallback is a real mode), consumer-driven contract fixtures, interaction/a11y tests beyond static markup | F1 (manifest), F5a (contract fixtures), F5b (test kinds), §5/§6 updated |

---

## 2. Fixed vs open

**Fixed — foundation-worthy** (the concept's principles the owner accepted):

- The Zerops grammar: depth by tint, `MicroLabel`, `StatusDot` + word (never a bare dot), pills
  and chips, **blue acts / teal identifies**, native containers on mobile (concept §3 P1–P8).
- Theme first: `ZEROPS_THEME` on the 57 roles + a small set of fixed brand tokens (`brand.ts`,
  no new package — D9); the theme library stays; "Reset to Zerops".
- T3's shell and `Sidebar.tsx` stay (D0 evolve, D1) — the seams into them become adapters with
  behaviour tests.
- One phrase function (`strip.ts`) and one status resolver for every consumer.
- The client renders, the agent mutates (P6): no protected Zerops surface reaches a mutating RPC.
- No continuously repainting animation; reverse states; hit every surface (`AGENTS.md`).

**Open — deferred to surface specs** (the owner is changing these): first screen and door copy,
picker anatomy, band vs strip, map default-open and the width ladder, which cards escape the fold
(D2), question-card tiles, the credential card (D4), quick actions' home, palette intents,
settings IA, every mobile screen, Data Console embed (D6). **No foundation phase builds a
surface** — primitives are exercised only by showcase probes.

---

## 3. Target architecture of the client layer

```
L0  packages/contracts/src/{zerops,rpc,auth,environment}.ts      wire types + feeds + capabilities
L1  packages/shared/src/themePalettes.ts   ZEROPS_THEME             57 roles × light/dark
    packages/shared/src/brand.ts                                    fixed tokens: status tones (tone id → colour),
                                                                    mint panel, chip tints, mark path data, icon map,
                                                                    radius/type constants, copy glossary
    packages/shared/src/threadStatus.ts                             the ONE status resolver: thread facts → {kind, toneId}
                                                                    (consumers: web row + pill, mobile row, widget props, relay)
    packages/shared/src/generated/*                                 projector outputs (F4: web boot snippet, mobile default block)
L2  packages/client-runtime/src/zerops/**                           UI-free logic, three consumers:
      api · session · registration · newProject (exist)
      + candidates · provisioning · containerHealth · serviceMap · strip · quickActions
        · firstPrompt · cards/{payloads,decode,liveFixtures} · agentLogin · activityResult
        · registrationHandoff · autoEnterProvisioning · statusPresentation (labels for threadStatus kinds)
L3  apps/web/src/zerops/**            web adapters only: feed subscriptions (feeds.ts) SPLIT from commands (commands.ts),
                                      use* hooks, SessionProvider, turnstile, storage
    apps/mobile/src/features/zerops/runtime/**   the RN counterparts (S5-3)
L4  apps/web/src/components/zerops/primitives/**                   StatusDot · MicroLabel · Chip · Pill · FlatCard
    apps/mobile/src/features/zerops/primitives/**                  · MintPanel · ProcessSteps · KeyChip · LivenessLine
L5  apps/web/src/components/zerops/<surface>/**                    map · strip/band · cards · picker · landing · panel · agent-auth
    apps/mobile/src/features/zerops/screens/**
    apps/desktop                                                   = the web bundle; brand + window colours only
```

**Protected roots** (R2): `components/zerops/{map,band,cards,quickActions}/**` and their mobile
counterparts — the surfaces that only render. The door, picker, session provider and agent-auth
card legitimately issue commands (sign-in, register, create project, restart, agent login) and are
not protected; their commands are an explicit reviewed set.

**Seams** — the only files outside the zone a surface slice may touch, each behind a small
adapter with a behaviour test (F5c): `ChatView.tsx` → `resolveZeropsChatChrome` (topology
availability ⇒ `zerops` panel kind offered; lifecycle receives the scoped thread; auth overlay only
on attention; inline `:7320` and sheet `:7358` consume the same value), `MessagesTimeline.tsx` →
milestone predicate at the three hiding mechanisms (turn `hiddenEntryIds`, group summary, overflow)
+ the card mount `:2595`, `Sidebar.tsx` header via `sidebar/SidebarChrome.tsx` + row status via the
shared resolver, `rightPanelStore.ts` → an exhaustive `RightPanelKind → availability/content/
migration` adapter, the door → one resolver over the descriptor/bootstrap-method matrix,
`CommandPalette.tsx` (intents), `SettingsPanels.tsx` (sections), `ui/*` (variants added).
Existing `components/zerops/*.tsx` files move into the L4/L5 layout **when a surface round
touches them**, never as a big-bang move.

**Rules — machine-checked, each with its predicate and its test** (the DS rows of the future spec
section). Exceptions everywhere are **fingerprint entries** (path · AST/declaration kind ·
normalized fingerprint · owner · reason · expiry phase); CI fails on a new, a changed or a dead
entry.

| # | Rule | Predicate | Enforced by |
|---|---|---|---|
| R1 | `client-runtime/src/zerops/**` is UI-free and platform-free | zone test: no import whose specifier starts with `react`, `react-dom`, `react-native`, `expo`, `expo-`, `@effect/atom-react`. oxlint: no reference whose resolved scope is the global `window`, `document`, `localStorage`, `fetch` (property keys and local bindings do not count). Constructor tests prove storage / fetch / clock are accepted explicitly | zone rule 5 + `t3code/no-platform-globals` + module tests |
| R2 | Protected roots render only | for each protected root, resolve local imports recursively; reject any import of a command constructor (`commands.ts`, `use-atom-command` write helpers), a platform API client (`client-runtime/zerops/api`), or a module in the reviewed write set; `WS_METHODS` used from a root must be in the explicit read set (subscriptions, gets) or the allowed-command set (`zerops.agentLogin.start/cancel`, the credential verb later). Mutability is never inferred from a `subscribe` prefix | zone rule 6 (module-graph walk) |
| R3 | Tokens only | JS/TS: colour literals (`#hex`, `rgb(`, `rgba(`, `oklch(`, `hsl(`) are rejected only in semantic sinks — style objects / `StyleSheet.create`, CSS-variable objects, SVG `fill` / `stroke` / `stopColor` / `color` — never in `d`, `points`, `viewBox`, ids, URLs; Tailwind palette utilities only as complete class tokens with a colour-property prefix; `dark:` / `light:` only in class-like literals. CSS: a parser over declarations. Scope: the Zerops dirs of web and mobile (zero tolerance), the named status consumers (`threadPresentation.ts`, `AgentActivity.tsx`), then repo-wide with a fingerprinted baseline (today's 225 web + 155 mobile) | `t3code/no-theme-escape-hatches` (the mobile rule generalised) + a CSS check script |
| R4 | No legacy vocabulary in user-facing copy | sinks: `JSXText`, values of `aria-label`, `title`, `placeholder`, `alt`, `label`, `description`, and registered copy tables; word-boundary patterns for `T3 Code`, `T3 Connect`, `Tailscale`, `pairing`, `worktree`, `Local checkout`, `control plane`; the manual one-time-link fallback moves into one named component whose exact literals are exempt with owner + reason; identifiers, imports, comments and whole files are never exempt | `t3code/no-legacy-vocabulary` |
| R5 | One status resolver, one phrase producer | `packages/shared/threadStatus.ts` is the only resolver; a **vector test** runs one table of thread facts through the web row, the palette pill, the mobile row, the widget-prop builder and the relay aggregation and asserts identical `{kind, toneId}`; a structural check bans the known local status-table shapes in the named consumers. The lifecycle phrase stays `client-runtime/zerops/strip.ts` (different input domain) with the same vector treatment across web band, mobile subtitle and Live Activity | `packages/shared` vector test + zone rule 7 |
| R6 | No continuous repaint | CSS parser: any `animation` / `animation-iteration-count` declaration containing `infinite` must reference a keyframe using a reviewed stepped helper (steps ≤ N, hold ≥ M ms) or is a fingerprinted exception; oxlint: `withRepeat` resolved to its Reanimated import with count `-1` is rejected outside the approved duty-cycle helper; `Spinner` resolved by binding (aliases included) is rejected in JSX inside the protected roots. Known continuous uses today: `index.css:491,2479`, `LoadingStrip.tsx:43`, `ConnectionStatusDot.tsx:46` | CSS check script + `t3code/no-infinite-motion` |
| R7 | The theme is complete and legible | `Object.keys(ZEROPS_THEME.colors)` **equals** `THEME_COLOR_ROLES` for both appearances; every parsed value has alpha exactly 1; contrast ≥ 4.5 on the named text pairs (`text/canvas`, `text/surface`, `messageActionForeground/messageAction`, `accentForeground/accent`, `accentSurfaceForeground/accentSurface`, each semantic fg/surface) and ≥ 3.0 for `focus/canvas` and the 60 %-mixed sidebar icon; indicator colours are not contrast-tested as text; a teal foreground over a light surface fails; the generated boot/default projections equal their source roles | `packages/shared` test |
| R8 | Generated copies are current | `generate:theme --check` byte-compares the F4 projections; extends to every target the later generator adds | root script + CI `check` |

---

## 4. The foundation programme

Effort: S ≤ 1 slice · M 2–4 slices · L a stream. Routing per the house rule: Sonnet slices with
self-contained briefs in their own worktree (RED → GREEN), Opus review, Fable orchestrates.
Every phase ends with the guards green on `main` and a ledger/spec touch.

| Phase | What | Needs | Effort |
|---|---|---|---|
| **F0 Ground** | (a) verify CI green on current `main` (the 2026-08-30 fixes are in; add the clean-checkout diagnostic for untracked package shells); (b) the decision table (§7) signed and the rule predicates (§3) frozen; (c) `../z3/docs/internals/zerops/design-system.md` created as a dated ledger file: the vocabulary table (component → anatomy → states → phrase source), the glossary, the icon map, rules R1–R8 with their test names, the exception ledger; (d) `fork.md §3` owned-product row extended with `apps/web/src/components/zerops/**`, `packages/client-runtime/src/zerops/**`, `apps/mobile/src/features/zerops/**`, `packages/shared/src/{brand,threadStatus}.ts`; (e) `AGENTS.md` "three surfaces" corrected | — | S |
| **F1 Guards + manifest** | R3, R4, R6 with fingerprinted baselines that pass on day one; R2 after its **mechanical boundary fixes** (split `feeds.ts` into subscriptions + `commands.ts`; drop `Spinner` from the map row); wired into the CI `check` job beside the zone test; the **surface manifest** — a checked-in, machine-readable table keyed by feature id: entry points · clients (web/desktop/mobile or `n/a` + reason) · provider applicability · **connection modes** (Zerops door, manual one-time link) · contracts/projections · reverse states · docs · required test and capture ids; a test proves every declared row is complete | F0 | M |
| **F2 Clean tree** — independent, preconditioned streams; none blocks F3–F6 | **F2.1** PR review workspace (DN1a): web `components/pullRequest/` + route + state, server `apps/server/src/pullRequest/**` + `ws.ts` handlers + `RpcAuthorization` scopes + `server.ts:475` HTTP layer, contracts `pullRequest.ts` + `environmentHttp.ts:37,554` group, client-runtime `state/pull-requests`, mobile `use-thread-pr.ts` + list/home affordances — server half first, projection/reducer compatibility tests, not typecheck alone; the thread's external PR reference (`orchestration.ts:429,843`, `environment.ts:59`) is DN1b and stays until decided · **F2.2** `LegacySidebar.tsx` + `legacySidebarEnabled` (+ its `chat.new` branch `_chat.tsx:92`, settings contract) (DN2a) · **F2.3** mobile thread-list: name `thread-list-v2-items.tsx` the survivor, delete the legacy branches + preference (DN2b) · **F2.4** Worktree as a capability (DN3): `worktreesAllowed` / `threadEnvironmentModes` on the environment descriptor, client filters options and defaults · **F2.5** `/connect*` routes + `connectCliAuth.ts` + web Clerk (`@clerk/react`, `components/clerk/`, `main.tsx:29`, `cloud/managedAuth.tsx`) **in the same slice as S5-5's CLI entry points** (`connectAuth.ts:41`) — the direct-Zerops activity-link path (`cloud/linkEnvironment.ts`, `dpop`, `managedRelayLayer`) is **kept**; mobile `@clerk/expo` stays until S5-3 · **F2.6** `vercel.ts`, `announce-connect-ga.ts`, `SidebarStageBackdrop` + stage art, `docs/user/*` pages describing deleted features · **F2.7 naming** (concept P0): `branding.ts`, `SplashScreen`, `index.html`, `clientMetadata`, `settingsSearch`, desktop `DesktopEnvironment.ts` names + menu, mobile `app.config.ts` display names — keep-list: bundle ids, `t3code://` schemes, the npm name on the container prefix | F1 (guards catch regressions); F2.5 needs S5-5 | M each; F2.1 L |
| **F3 Shared logic** (= S5-2) ∥ F4 | Move the 13 modules + tests to `client-runtime/src/zerops/`; inject storage / fetch / clock; rewrite web imports; R1 on. **The status resolver**: `packages/shared/threadStatus.ts` (facts → `{kind, toneId}`) + `client-runtime/zerops/statusPresentation.ts` (labels) consumed by `Sidebar.tsx` rows, `Sidebar.logic.ts` pill, mobile `threadPresentation.ts` (its 8 literals go), the widget via resolved props, the relay's `statusForPhase`; R5 vector test on. Tone ids only — no colours in F3 | F1 | M |
| **F4 Tokens** ∥ F3 | `ZEROPS_THEME` (light + `variants.dark`, values from the R5/R7 research, flattened per declared surface with the surface recorded) + `brand.ts` (exports entry; subpath without the substring `zerops`); default on web via `getDefaultThemeColors` / `useTheme.ts` and on mobile; **the minimal projector** `scripts/generate-theme-tokens.ts` with two targets — the web `index.html` boot snippet (`DEFAULT_THEME_PALETTES`, `SPLASH_COLORS`, `theme-color`) and the mobile default `@variant light/dark` block in `global.css` (so `generate-uniwind-themes.mts:201` reads a generated default) — with `--check` (R8 on) and byte-equality tests; R7 tests; Roboto self-hosted woff2 (web) / `@expo-google-fonts/roboto` (mobile); provider accent fallback off `#2563eb`; the colour paths outside the roles (F4 research) each get a decision row — ANSI-16 / Pierre / shiki accepted as fingerprinted exceptions, `--success`/`--info` and status tones sourced from `brand.ts`, the `AuthSurfaceShell` gradient deleted, the Appearance artwork row deleted, the rest of the theme library kept. No component changes shape | F1 | M |
| **F5a Harness** | (a) **scene bundle v1**: a versioned directory, one scene = topology + lifecycle + agent-auth + agent-login snapshots + `threadActivities` in the exact projected shape (`payload.data.zerops` cases incl. a `zerops_delete`-style non-MCP `itemType`, a malformed result, an over-cap drop) + seeded projects/threads; scenes recorded from `z3-eval` (the `cards/liveFixtures.ts` precedent), dated; (b) **server fixture feeds** `apps/server/src/zerops/ZeropsFixtureFeeds.ts`: four scripted services (`ZeropsTopology`, `ZeropsLifecycle`, `ZeropsAgentAuth`, `ZeropsAgentLogin`) as `Layer.scoped` publishers over `Ref` + `PubSub` on the Effect `Clock`, composed with the same `provideMerge` shape as `zeropsFeedsLayer.ts`, selected there by `T3CODE_ZEROPS_FIXTURES=<dir>` (`config.ts`, `cli/config.ts`); fixture mode also sets `T3CODE_ZEROPS_PROJECT_ID`; (c) **the projection seeder** shared by `mobile-showcase-environment.ts` and the new `scripts/web-showcase.ts`, writing `projection_thread_activities` (the projector's own tests stay the guard for live / reconnect / detail paths, spec §5.3); (d) **consumer-driven contract fixtures**: every scene decodes through the public Effect schemas, runs through the web, mobile and relay presentation adapters, asserts each discriminant has a visible state or a total fallback, includes missing optionals and one newer/unknown shape, and fails when a contract changes without a scene migration; (e) `scripts/web-showcase.ts`: seeded environment + `playwright-core` (already a desktop dependency), every scene at 1440×900 and 390×844, light and dark, into `artifacts/web-showcase/`; captures are PR artifacts, not a pixel gate; (f) desktop `smoke-test` gains one `capturePage()`. Fixture tests validate wire/subscription behaviour (acking every chunk, spec §5.5), not the reducers they replace | F1, F3 (contract adapters) | L |
| **F5b Primitives** | Web primitives in `components/zerops/primitives/`, mobile primitives; tests of four kinds — pure state/presentation tables, interaction + accessibility (focus order, keys, reduced motion) where applicable, static markup for passive web variants, scene captures for review; `ui/*` gets **added** variants (`pill`, `chip`, `flat`) — defaults unchanged, switching a default is a surface decision | F4, F5a | M |
| **F5c Seam adapters** | `resolveZeropsChatChrome` extracted from `ChatView.tsx` with the availability/lifecycle/attention assertions; the `RightPanelKind` adapter with an exhaustive test; the `MessagesTimeline` **row-vector test** (ordinary tool, each milestone kind, malformed Zerops data, agent-spawn row) asserting escape/fold at all three hiding mechanisms; the door **resolver** over the descriptor × bootstrap-method matrix (replacing the four per-route checks — the F6 half); the status **state-vector test** (F3's) | F3 | M |
| **F6 The door** (coordinated with S4) | One Zerops-aware bundle keyed on the server descriptor: `PairingRouteSurface` branches on `bootstrapMethods`; `resolveChatIndexView` widened past hosted-static; the four route files consume the F5c resolver; identity base URL from `window.location` under `/z3/`; `cli.ts pack` stops baking T3 favicons. Copy of the first screen stays a surface decision | F5c | S–M |

**Order and why.** F0 is a gate. F1 first because every later slice is written by an agent and
the guard catches what the brief forgot; its baselines are exact, so it costs no product change
beyond the two mechanical fixes. F2 streams run whenever their precondition holds and block
nothing — a restyle of code about to be deleted is waste, but an unresolved deletion must not
stall a `StatusDot`. F3 and F4 are independent (tone ids vs colours) and both precede F5b. F5a
precedes every surface round because a Sonnet slice without a scene cannot show its work. F5c
turns "seam tests" into adapters with behaviour; F6 is its door half. The six-target generator
(concept P4), icons per channel and the desktop `.icon` work stay after the first surface rounds.

---

## 5. Surface rounds — how a screen gets built once the flow is decided

1. **Manifest row + spec row**: the feature id in the surface manifest (entry points · clients ·
   providers · connection modes · contracts · reverse states · docs · test + capture ids) and its
   anatomy in `design-system.md` (states with phrase source · data source · tones/tokens · seams).
2. **RED**: the scene(s) in the bundle (decoded through the contract fixtures); tests per state
   (tables; interaction/a11y where applicable; static markup for passive variants); the adapter
   test for each seam touched.
3. **GREEN** in a worktree by a Sonnet slice whose brief is the two rows + the rules; captures
   attached to the report.
4. **Review**: guards, captures (both widths, both appearances, both connection modes where the
   surface differs), the manifest completeness test; owner retest on `z3-eval` through the push
   loop whenever a live feed is involved.
5. **LAND**: a DS row in `docs/spec-z3.md`'s client design-system section; the plan row deleted.

Candidate first round (the concept's order, **not decided**): sidebar header + row status (the
shared resolver makes it a two-file edit) → lifecycle band → map rows + mint panel → picker →
cards + fold predicates → the landing on the container origin.

---

## 6. The UI-slice brief — template

```
Surface: <name>   Manifest id: <feature-id>   Spec row: design-system.md §<n>   Clients: web | mobile | both
Connection modes: zerops-door | manual-link | both        Files allowed: <zone dirs> + <named seam adapters>
Rules: R1–R8 stay green; tokens only; protected roots render only; every state has a phrase; exceptions are fingerprints with owner+reason+expiry
RED first: scene <name>.json (decodes through the contract fixtures); <test file>: one table row per state; interaction/a11y where applicable; seam adapter test
Then GREEN; then `vp test run <files>` + package typecheck + `vp lint` + zone test + `generate:theme --check` + showcase capture
Hit every surface: entry points · clients · providers (n/a unless provider-shaped) · contracts · reverse states · connection modes · docs/user
Report: commits (atomic, English, no trailers), capture paths, which manifest rows applied, facts as text (one ledger writer)
```

---

## 7. Decisions for the owner

| # | Decision | Recommendation |
|---|---|---|
| D0 | Evolve `apps/web` vs the "new UI app" `fork.md` reserves | **Evolve** (concept); the L2–L5 layout makes a later rewrite cheaper, not necessary |
| D1 | `Sidebar.tsx` stays; header + row status are the only edits | **Confirm** |
| D9 | Fixed tokens in `packages/shared/src/brand.ts`, no `packages/brand` | **Confirm** |
| D10 | Generator last | **Adjust**: the two projections that make "one source" true (web boot snippet, mobile default block) land in F4 with `--check`; the six-target generator stays late |
| DN1a | Delete the embedded PR **review workspace** (full-stack: web + server `pullRequest/**` + contracts + client-runtime + mobile affordances) | **Delete, as a stream, server half first**, with projection/reducer compatibility tests |
| DN1b | The thread's **external change-request reference** (status + deep link; `orchestration.ts:429,843`, `environment.ts:59`) | **Owner decides** — keep as a read-only link (cheap, matches "review lives in GitHub") or delete with DN1a |
| DN2a | `LegacySidebar.tsx` + `legacySidebarEnabled` (+ its `chat.new` branch, settings contract) | **Delete** — one sidebar on web before its status row is redesigned |
| DN2b | Mobile thread list: `thread-list-items.tsx` vs `thread-list-v2-items.tsx` (device preference `mobile-preferences.ts:35`) | **v2 survives**; delete the legacy branches + preference |
| DN3 | The Worktree env-mode option | **A capability on the environment descriptor** (`worktreesAllowed` / `threadEnvironmentModes`), the client filters; never a build flag, never an unconditional deletion while a non-Zerops server can connect |
| DN4 | Deterministic UI state source | **One versioned scene bundle, two production-shaped adapters** (four server fixture feeds + the projection seeder) — serves web, mobile, desktop and offline tests; fixtures are recordings of the real wire |
| DN5 | Web showcase driver | **`playwright-core`** (already a desktop dep) over adding puppeteer |
| DN6 | Mobile scope now (concept D13) | **Resolver + runtime promotion + fixture layer + primitives now (F3–F5); screens after S5-3** |
| DN7 | Naming slice timing and keep-list | **In F2 (F2.7)**; keep bundle ids and `t3code://` (dev client, OAuth callbacks) |
| DN8 | Home of the status resolver | **`packages/shared`** (the relay is a consumer too; `client-runtime` holds only the presentation adapter) |
| DN9 | `cloud/` on web | **Keep the activity-relay path** (`linkEnvironment.ts`, `dpop`, `managedRelayLayer`); delete only `/connect*` + `connectCliAuth` + web Clerk, with S5-5 |
| DN10 | Exception policy for every guard | **Fingerprint entries with owner, reason, expiry phase; CI fails on new, changed and dead entries** — no whole-file allowlists, no count-only budgets |
| D5 | Roboto beside SF on iOS | **Defer to captures** (F5a shows it) |
| D12 | Turnstile on a container origin | platform question; the hand-off to app.zerops.io stays the only sign-up path until answered |

---

## 8. Risks

1. **Concurrent streams edit the same files.** S5-2 *is* F3 — one owner, one worktree. S5-3
   (mobile session) and S7-4 (mobile card) consume F3's output — they wait for it. F6 *is* S4's
   files — one slice, coordinated. F2.5 *is* S5-5's client half — one slice with the server half.
   S5-4b UI is independent.
2. **Clean merges duplicate contract blocks** (ledger rule, day 2). Post-merge grep for duplicate
   `export const` after every parallel-contract merge — in the orchestrator's checklist.
3. **Exceptions rot.** Fingerprinted entries with an expiry phase; a dead entry fails CI just like
   a new one, so the ledger only shrinks.
4. **Fixture drift from the real wire.** Scenes are dated recordings from `z3-eval`, re-recorded in
   the intake ritual when a contract changes; the contract fixtures fail the build before a
   stale scene can pass a wrong UI.
5. **A fixture layer validates the wire, not the reducers.** The live `ZeropsTopology` /
   `ZeropsLifecycle` tests stay the truth for reducer behaviour; a green showcase says nothing
   about the doorbell.
6. **R2 can still be gamed** by a helper outside a protected root that mutates. The module-graph
   walk closes the transitive path; the reviewed write set is small and named; a new write
   module is a decision, not a lint bypass.
7. **DN1a reaches shared contracts.** Server half first; projection/reducer compatibility tests;
   `vpr typecheck` is necessary, not sufficient.
8. **The pink flash** (concept risk 5): `getDefaultThemeColors` and the generated boot snippet
   point at `ZEROPS_THEME` in the same F4 slice that registers it; R7 asserts the projection
   equals the source.
9. **Mobile stays on paper longer** than the concept hoped: that is the correct order; the
   resolver, the runtime promotion, the fixture feeds and the primitives make the first mobile
   screen a slice, not a stream.

---

## 9. What this plan does not decide

The first screen and its copy, the picker, the band, the map's default state and width ladder,
which cards escape the fold, the question tiles, the credential card and its zcp verb, quick
actions' home, palette intents, the settings IA, every mobile screen, the Data Console embed, the
six-target generator's shape, icons per channel. Each is a §5 spec row when its flow is decided.
