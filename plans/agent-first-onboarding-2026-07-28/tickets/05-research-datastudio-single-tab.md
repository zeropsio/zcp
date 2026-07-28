# 05 — Research: Data Studio single-tab readiness

- `status:` closed
- `type:` research
- `assignee:` research-subagent (fired 2026-07-28)
- `blocked-by:` —

## Question

The owner's claim: the console is by-design one app and the sidebar-view integration bends it.
Verify against `internal/dataconsole/**` on main + `docs/spec-dataconsole.md`:

1. Does the SPA/loopback server already support **multi-service switching inside one
   instance** (session manager, per-service routes, any existing service selector), or is the
   per-service assumption baked in anywhere (broker, CSP, panel identity)?
2. What the single-tab conversion touches in the `zcp-studio` extension: manifest
   (`viewsContainers`/`views`), the sidebar webview view, commands, panel lifecycle.
3. **Icon-as-direct-entry feasibility**: can an activity-bar icon open a WebviewPanel tab
   directly (no sidebar view), or does VS Code force a view container — and what's the honest
   pattern if it does (auto-forward + collapse, command re-route)?
4. Singleton-tab semantics options (reveal-existing vs. re-create; what survives a reload).
5. Which `spec-dataconsole.md` sections the change reconciles (§4.1 embed, install §, uitest
   harness assumptions like `.activitybar a[aria-label="Managed Data"]`).

Findings: `plans/research/datastudio-single-tab-2026-07-28.md`.

## Answer

Findings: [`plans/research/datastudio-single-tab-2026-07-28.md`](../../research/datastudio-single-tab-2026-07-28.md).

The owner's "prepared by design" claim **holds unusually well**: the session + panel managers are
keyed by workspaceRoot (not service), `consolePanel.js` already reveals-and-switches an existing
panel via a `dataconsole-switch-service` postMessage, and the SPA ships a complete multi-service
rail (`renderServices`/`selectService`) that is merely HIDDEN when embedded+deep-linked by one
boolean (`DC.embed.shouldHideServiceRail`, whose doc comment names today's sidebar card list as
the reason). Multi-service-in-one-tab is dormant shipped functionality, not a build. The
extension's `require()` graph cleanly separates the sidebar subsystem (cards/, refresh handlers,
~7 test files) from the untouched panel/session subsystem — the conversion is a bounded deletion.

**VS Code constraint** (vscode#149556, core-team quote): an activity-bar item cannot open an
editor panel with zero views — the honest "icon as third entry" is a stub webview view that
auto-forwards to the open-tab command and collapses, with a brief unavoidable view flash
(#152382: `resolveWebviewView` can't pre-fire hidden). "No populated sidebar", not "no view".

**Surfaced shape decision** (for the spec ticket): `shouldHideServiceRail`'s deep-link branch
hides the rail assuming the sidebar fallback exists; under single-tab, a deep-linked open (the
agent-panel link) must keep the rail so switching stays possible — likely "rail always visible in
single-tab mode". The icon path already keeps the rail correctly.
