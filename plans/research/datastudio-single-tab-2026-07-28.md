# Research: Data Studio single-tab readiness

Ticket: `plans/agent-first-onboarding-2026-07-28/tickets/05-research-datastudio-single-tab.md`
Scope: `internal/dataconsole/**` (console engine, extension host, embedded SPA) + `docs/spec-dataconsole.md`,
verified against `feat/agent-first-onboarding` (identical to `main` for this subtree — `git diff main -- internal/dataconsole/ docs/spec-dataconsole.md` is empty).

---

## FACTS

### 1. Multi-service switching inside one instance — already built, at every layer

The console is **not** per-service at any layer. It is per-**workspace** (one child
process, one webview panel key), and every service-scoped operation is a request
parameter, not an identity.

- **Session manager is workspace-keyed, not service-keyed.** `sessionKey(workspaceRoot)`
  is the only key used for both the spawned child process (`servers[key]`,
  `internal/dataconsole/extension/templates/vscode-studio/lib/consoleSession.js:22,52,149-171`)
  and the webview panel (`panels[key]`,
  `internal/dataconsole/extension/templates/vscode-studio/lib/consolePanel.js:136,189`).
  Opening a second managed service **reuses** the same child and the same panel —
  `ensureReady()`'s doc comment states this explicitly: "so opening a second managed
  service ... never starts a competing process for the same workspace"
  (`lib/consoleSession.js:145-148`).

- **The panel manager already does reveal-and-switch, not recreate.** `consolePanel.js`'s
  `show(key, opts)` (`lib/consolePanel.js:184-270`): if a panel for the key already
  exists, it **reveals** it (`entry.panel.reveal(column())`) and posts a
  `dataconsole-switch-service` message with the new service — "A same-key reveal with a
  new service deep-links via a host->webview switch message (no reload — the SPA
  listens for it)" (`lib/consolePanel.js:181-183,204-205`).

- **The SPA has its own full multi-service rail already wired**, independent of any
  host-side card list: `state.services` (all project services from `/api/services`),
  `renderServices()` (`internal/dataconsole/console/webui/dist/app.js:407-419`), and
  `selectService(s)` — clicking a rail row switches the active service in place, no
  navigation. The rail is populated once at `start()` (`app.js:330-341`) and is a
  project-wide list, not scoped to whatever service the panel was opened with.

- **Host→webview switching is a real, tested message contract**, not a stub:
  `dataconsole-init` (initial deep link) and `dataconsole-switch-service` (in-place
  switch) are both handled in `onHostMessage` (`app.js:274-317`), and
  `openPendingService()`/`selectService()` apply the switch without a page reload. Both
  message types are pinned by extension-side tests
  (`internal/dataconsole/extension/studiojs/console.test.js:153` — "panel is shown again
  (reveal + switch-service)"; `consolepanel.test.js:100-103,198`).

- **No per-service assumption in the broker.** The embed broker's guard is
  method+path **shape** only (`ALLOW`/`MUTATING` sets built from `consoleRoutes.js`,
  generated from `server.go`'s `apiRoutes()` —
  `internal/dataconsole/extension/templates/vscode-studio/lib/consoleClient.js:20-48`).
  Service identity travels as a **query parameter** (`?service=<hostname>`), read by
  `withRouteContext` (`internal/dataconsole/console/server/server.go:183-190`), never as
  part of the route pattern or the panel's `viewType`. The panel's own VS Code identity
  (`vscode.window.createWebviewPanel("zcpDataConsole", "Data Console", ...)`,
  `consolePanel.js:212-217`) is a single constant string — not parameterized by service.
  CSP (`default-src 'none'`, no `connect-src` — `consolePanel.js:71-76`) is likewise
  service-agnostic; it gates the webview↔host channel, not which service is reachable.

- **The one place service identity DOES gate UI is a documented, single-purpose
  chrome decision**, not a structural constraint: `DC.embed.shouldHideServiceRail`
  (`internal/dataconsole/console/webui/dist/dc-embed.js`) hides the SPA's own rail only
  when embedded **and** deep-linked to one browsable service — because today "the
  host's own managed-service list IS the service selector" (the sidebar card list;
  `dc-embed.js:1-11`). This is the exact seam the single-tab conversion needs to flip
  (see ASSESSMENT).

**Verdict on the mechanism question: yes** — the loopback server, the broker, the panel
manager, the session manager, and the SPA all already support one running instance
serving/switching between every managed service in the project. This is pre-existing,
tested behavior (`TestWriteToken_DualClient_CallerBound`-adjacent tests aside — see
`console.test.js`/`consolepanel.test.js`/`dc-embed.test.js` above), not something that
needs to be built.

### 2. What the single-tab conversion touches in the extension

Two independent subsystems live in `templates/vscode-studio/`, cleanly separated by
`require()` graph (verified: only `extension.js` requires `lib/cards`, `lib/handlers`,
`lib/webviewSession`, `lib/transport`, `lib/discoverToUIMap`; only
`handlers/console.js` requires `lib/consoleSession`):

**A. The sidebar-view subsystem (becomes obsolete under single-tab):**
- `package.json` — `contributes.viewsContainers.activitybar[0]` (keep the container/icon
  entry) and `contributes.views.zcpStudio` (the `zcpStudioView` webview view — deleted or
  replaced per §3 below); `activationEvents: ["onView:zcpStudioView"]` (would change to
  command-based activation, or stay if a stub view is kept — see §3).
  `internal/dataconsole/extension/templates/vscode-studio/package.json:9-21`.
- `extension.js`'s `activate()` — the `resolveWebviewView` provider and
  `registerWebviewViewProvider(VIEW_ID, provider)` call
  (`extension.js:46-86`).
- `cards/managed.js` (per-service "Browse data →" rows — the exact functionality the
  in-SPA rail already duplicates) and `cards/refresh.js` (the sidebar's own live-sync
  tick/footer).
- `handlers/refresh.js` (drives sidebar topology re-paint) — `handlers/console.js`
  (`type: "openConsole"`) is the one handler that stays, though its dispatch site moves
  from a webview `data-action` click to a VS Code command.
- `lib/webviewSession.js` (webview HTML shell render, message router, `zcp studio watch`
  process management for live topology sync), `lib/cards.js`, `lib/handlers.js`,
  `lib/transport.js` (`runStudioVerb` → `zcp studio topology`), `lib/discoverToUIMap.js`.
- `lib/svc-icons.js` — used only by `cards/managed.js` today; dead unless something else
  adopts it.
- The matching test files:
  `internal/dataconsole/extension/studiojs/{managed,refresh,webview_session,shell_render,transport,discover_to_uimap,discovery_contract}.test.js`.

**B. The panel/session subsystem (survives unchanged, already singleton + multi-service):**
`lib/consoleSession.js`, `lib/consolePanel.js`, `lib/consoleClient.js`,
`lib/browserDownload.js`, `handlers/console.js`'s `mgr.open(...)` call, and their tests
(`console.test.js`, `consolepanel.test.js`, `consoleclient.test.js`,
`browserdownload.test.js`). The new entry points (command, agent-panel link, icon) all
just need to call the equivalent of `mgr.open({ workspaceRoot, extensionPath, service,
postMessage })` — the exact call `handlers/console.js:16-21` already makes.

**New surface needed:** a `contributes.commands` entry (e.g. `zcpStudio.open`, optionally
`zcpStudio.openService` taking a hostname arg for the agent-panel link) and a
`vscode.commands.registerCommand` handler in `extension.js` that calls the session
manager directly — this is a thin wrapper around existing `handlers/console.js` logic,
not new plumbing.

### 3. Icon-as-direct-entry feasibility — VS Code does not support this; confirmed by the VS Code team

**The activity bar cannot open an editor-area `WebviewPanel` with no view.** This is not
an implementation gap in ZCP — it is VS Code's own architecture, confirmed directly by
the VS Code team in
[microsoft/vscode#149556](https://github.com/microsoft/vscode/issues/149556)
("Clicking Activity Bar Icon Launches WebView Panel...", closed `*out-of-scope`):

> "the activity bar switches between views and is not meant to be used for performing
> generic operations. You can use the activity bar if you have a `webview view`.
> Otherwise try finding a different way for users to open your webview panel"
> — Matt Bierner (`mjbvz`, VS Code core team), 2022-05-16.

A second core-team reply in the same thread (David Dossett) explains *why*: activity bar
items are **stateful** (one active item, tied to what's shown in the sidebar); an icon
that opens something unrelated to the sidebar "acted like a link — which is not what
users would expect... It's almost as if a browser tab stole your click and instead
opened up a dialog." This is a deliberate product-semantics decision, not a missing API.

**The honest pattern, confirmed by the same thread's workaround discussion and a related
closed issue:** keep a minimal `webview` view registered under the container (satisfies
VS Code's requirement — `contributes.views` requires `"type": "webview"` for
`resolveWebviewView` to ever fire), whose `resolveWebviewView` immediately triggers the
open command (`vscode.commands.executeCommand("zcpStudio.open")`) and then collapses the
sidebar (`workbench.action.closeSidebar` or equivalent). Two hard constraints on this
pattern, both confirmed by VS Code issues:

1. `resolveWebviewView` **only fires once the view is actually shown/focused** — there is
   no API to pre-resolve a hidden view (`microsoft/vscode#152382`, "Programmatically
   resolve Webview View," closed as a duplicate, unresolved as a stable feature). This
   means the sidebar view genuinely becomes visible for at least one frame before code
   can collapse it — the community's own name for this is "the user can briefly see the
   tree ui open and close" (cited from the same #149556 workaround discussion, on
   TreeView-based versions of the trick; a webview-view version has the same mechanic).
2. VS Code's activity bar **toggles** its own view — clicking the icon a second time
   while its view is the active one collapses the sidebar instead of re-opening it. ZCP's
   own uitest harness already documents this exact VS Code behavior as trap #3 (see §5).

Real extensions that need "one click → editor content" either accept the visible view
(a real, useful tree/list, e.g. Docker, GitLens) or route through something other than
the activity bar (status bar item, command palette, welcome-view button) per
`gjsjohnmurray`'s and `mjbvz`'s suggestion in the same thread. There is no third option
where the icon is purely a command trigger with zero view.

### 4. Singleton-tab semantics — reveal-existing is already implemented; nothing survives a reload

- **Reveal vs. recreate: already reveal-existing.** Confirmed in §1 —
  `consolePanel.js:189-210` reveals the existing panel and rebinds a fresh broker rather
  than creating a second panel, for any subsequent `open()` call in the same workspace.
- **What survives a window/editor reload: nothing, today.** `grep -rn
  "registerWebviewPanelSerializer"` across `internal/dataconsole/` returns no matches —
  there is no `WebviewPanelSerializer` registered. Per VS Code's API
  (`window.registerWebviewPanelSerializer(viewType, serializer)`), a `WebviewPanel`
  **without** a registered serializer for its `viewType` does not survive a window
  reload/restore at all — it is simply gone; the extension would need to be re-triggered
  from the icon/command again. This is a pre-existing gap, not something the single-tab
  conversion introduces or fixes; if reload-survival matters for the single-tab UX, it
  needs its own serializer for `viewType: "zcpDataConsole"` (would need to re-spawn/
  re-attach to the still-running per-workspace child via `ensureReady()`, which already
  supports "reusing the live child if one is running" — `consoleSession.js:145-148`).
- **How current per-service panels handle a reveal:** covered in §1 — the write-mode
  flag is explicitly carried over from the outgoing broker to the new one on reveal
  (`consolePanel.js:190-208`, with an explicit comment on why: "the webview keeps its
  green write toggle... while the new broker silently reverts to read-only" if this
  weren't done) — this logic is state that already needs to persist across
  service-switches within the singleton tab, and does.

### 5. Spec/test surfaces the change would reconcile

- **`docs/spec-dataconsole.md` — no direct rework needed.** A full-text grep for
  `sidebar|activity.bar|Activity Bar|WebviewView|webview view` in the spec returns no
  hits — the spec never commits to the sidebar-view integration as the embed mechanism.
  §4.1 ("Embedded — WebviewPanel + host broker," lines 90-113) already describes the
  panel/broker/CSP contract at the right level of abstraction (a `WebviewPanel`, broker
  guards, fixed loopback destination) and stays accurate verbatim under single-tab. §8
  (Install, lines 486-512) describes extension packaging/versioning mechanics that don't
  reference manifest view structure either. **Nothing in the spec text is factually wrong
  after the conversion** — it simply currently under-documents the reveal/switch
  mechanism in §1 as a first-class capability, which would be worth adding once built.
- **Go install tests do not pin manifest content.** `internal/init/adapters/studio.go`
  and `studio_test.go` (the `TestInstallStudioExtension_*` family, plus
  `TestStudioExtVersion_ParityWithPackageJSON`) test directory materialization, extension
  index registration/idempotency, and version-string parity — a `grep` for
  `viewsContainers|contributes|activationEvents` in both files returns nothing. These
  tests are unaffected by any `views`/`viewsContainers`/`commands` content change, as
  long as `package.json`'s `version` field and the Go `studioExtVersion` const stay in
  sync (mechanical bump, not a design constraint).
- **`internal/dataconsole/extension/studiojs/*.test.js` is the real reconciliation
  surface**, per §2's subsystem split: `managed.test.js`, `refresh.test.js`,
  `webview_session.test.js`, `shell_render.test.js`, `transport.test.js`,
  `discover_to_uimap.test.js`, `discovery_contract.test.js` all test sidebar-only
  machinery that a single-tab conversion deletes; `console.test.js`,
  `consolepanel.test.js`, `consoleclient.test.js`, `browserdownload.test.js` test the
  panel/session layer that survives and stays green.
- **The uitest harness (`internal/dataconsole/uitest/`) hard-codes the sidebar path** and
  would need a new entry helper:
  - `openSidebar(page)` (`lib/harness.js:165-185`) clicks
    `.activitybar a[aria-label="Managed Data"]` and waits for a **sidebar iframe**
    probed by `.zs-rowhead` (a `cards/managed.js` row) — this frame/probe disappears
    entirely once the sidebar view is gone.
  - The harness's own documented trap #3 (`README.md:222-227`): "VS Code's activity bar
    toggles, it doesn't just open" — clicking the icon a second time while its view is
    active **collapses** the sidebar. This is the exact VS Code semantic §3 cites from
    `mjbvz`/`daviddossett`; if the icon's view becomes a pass-through stub (§3's "honest
    pattern"), this toggle behavior likely still applies to that stub, so the harness's
    workaround (probe-before-click, idempotent open) would need re-deriving for
    whatever the new icon behavior turns out to be (reveal-panel vs. toggle-stub-view).
  - Trap #2 (`README.md:212-220`) already distinguishes the sidebar's
    `[data-action="openConsole"][data-service="..."]` buttons from the SPA's own
    `#services li` rail (`clickService()`) — under single-tab, every scenario file
    currently entering via `openConsole`/`sidebarBrowse` (the sidebar path) would need to
    switch to entering via the command/icon and then `clickService()` (the in-SPA rail),
    which the harness already has a helper for.

---

## ASSESSMENT

**The owner's claim is correct, and unusually well-evidenced in the code itself — not
just plausible, but explicitly documented as the intended target state already.** The
comment in `dc-embed.js:1-11` is the clearest single piece of evidence: it names the
*current* sidebar card list as "the service selector" that the SPA's own rail defers to,
framing the SPA's rail as the primary/default UI that is *suppressed* today, not absent.
Combined with the session manager and panel manager already being workspace-keyed (not
service-keyed) with a working reveal-and-switch message contract, the multi-service,
one-app behavior is not a redesign — it is dormant functionality that ships today,
gated off by one boolean-returning function (`shouldHideServiceRail`) whose *only*
reason to hide the rail is the sidebar's existence.

This significantly changes the shape of the work from "build multi-service switching in
the console" to "delete the sidebar-view subsystem and flip one chrome decision." The
actual engineering effort is concentrated in three places, none of which is the console
engine or the SPA's data logic:

1. **Manifest + entry-point wiring** (§2, §3) — genuinely bounded by VS Code's own
   platform semantics, not a ZCP design choice. The activity bar cannot be a bare command
   trigger; a stub `webview` view has to remain behind the icon, and it will produce a
   real (if brief) view-open/close each time it is used, because `resolveWebviewView`
   cannot be pre-fired. The owner's phrasing ("the icon stays, but should open the tab
   directly, no sidebar") is achievable as "no *visible, populated* sidebar" but not as
   "the icon has literally no view" — that distinction is worth surfacing to the owner
   explicitly, since #149556 shows other extension authors hit this exact wall and were
   told no by the VS Code team.
2. **`shouldHideServiceRail`'s policy needs to flip**, not just its call site. Today it
   hides the rail specifically because *the sidebar duplicates it*; once the sidebar is
   gone, the icon's default no-deep-link entry (`deepLinkedService: null`) already keeps
   the rail per the existing test (`dc-embed.test.js:29-33`, "embedded without a target...
   keeps the rail so the user can pick") — that branch is exactly right for the new
   default entry path and needs no change. The branch that DOES need a decision is a
   deep-linked entry (the agent-panel "browse this service" link): today it *hides* the
   rail on the assumption a sidebar card list is still visible as the selector; under
   single-tab there is no such fallback, so hiding the rail on a deep link would strand
   the user with no way to switch services without closing/reopening the tab. This is a
   genuine design decision the ticket should flag for SHAPE, not an obvious flip.
3. **A large, cleanly-separated deletion** of the sidebar subsystem (§2A) and its
   dedicated test suite (§5), which is good news for risk: the `require()` graph shows
   zero entanglement with the panel/session/broker code that must survive, so this is a
   subtraction, not a refactor with blast radius into the parts that already work.

One caveat not to lose: the sidebar subsystem currently owns the **live topology sync**
UX (`zcp studio watch` process, `handlers/refresh.js`, `cards/refresh.js`'s
connected/polling tick) for keeping the *service list itself* fresh (a service appearing/
disappearing/changing status). The SPA's own `/api/services` + `/api/refresh` covers
per-load freshness but does not obviously replicate the push-based live-watch UX today —
whether the single-tab SPA needs an equivalent "live" indicator for topology changes (new
service added while the tab is open) is a product question the conversion surfaces, not
one this research resolves.
