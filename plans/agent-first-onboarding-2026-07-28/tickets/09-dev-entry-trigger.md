# 09 — Dev entry: invoke onboarding mode from a logged-in project

- `status:` closed
- `type:` grilling
- `assignee:` krls2020 (session 2026-07-28)
- `blocked-by:` —

## Question

Today the only entry into the ZCP onboarding mode is registration/login with `?zcp=true`
(cookie `claimZcpPool`, one-shot drain after full auth). For development and testing — and
possibly as a lasting affordance — we need to invoke the fullscreen onboarding state
artificially from an **already logged-in project**: switch the project view into the overlay
flow (agent pick + auth + embed) on demand.

Decide the trigger mechanism (query param on project detail, dev-only button, route), whether
it is dev-only or ships, and how it composes with the claim-cookie machinery it bypasses.

Graduated from the map's fog ("Re-onboarding entry") — the "dev-only or ships" call now owns
the user-facing re-onboard question: does ANY surface let a user re-run the prompted onboarding
launch after the first run, or is onboarding strictly once? Facts to carry: the abandoned-wizard
recovery is plain auth with deliberately NO "Onboard me" prompted-launch state (ticket 08 §2),
and the panel mock offers `Open terminal`/`Open extension` only — no per-row onboard action
(ticket 06). A re-onboard, if it exists, is expressible only as the FE re-sending `launch-agent`
with a fresh eventId — i.e. exactly this ticket's mechanism.

## Answer

Resolved 2026-07-28 (grilling with owner).

### 1. User-facing re-onboarding: strictly once

No surface ever re-invokes the fullscreen onboarding (wizard + auth) after the first run — no
button, no panel row, no user-facing route. The return path is the standard one: normal vscode
opens and the panel offers the agents (Authorize / Open terminal). A user who wants the
onboarding content again types the prompt into any agent CLI themselves (owner: "můžeš kdykoliv
napsat onboard me do jakéhokoliv agentic CLI" — the fallback of plain vscode + agent panel IS
the re-entry story).

### 2. Mechanism: one-shot query param on project detail, ships dark

`/project/:projectId?zcpOnboard=1` (placeholder name — exact param pinned by ticket 07). An FE
effect catches it, immediately strips it from the URL (`replaceUrl` — a reload must not
re-trigger), and raises the wizard. **No build-flag gate**: `isDevMode`-gating would split
behavior between the local dev serve and the deployed febridge rig (`build:zerops`); the param
works identically in local dev, the deployed rig, and production — dark: no UI, no docs, risk
nil (it only raises the wizard over the user's own project; auth is still required). Rejected:
dedicated route (a route existing only for dev), dev-only button (needs the build-flag gate).

### 3. Composition with the claim machinery: pure bypass, wizard enters at `picking`

- The dev-entry effect performs the drain's tail itself: resolve the ZCP stack **in the route's
  project** (`isControlPlaneService` + `projectId` filter — never first-match in the client-wide
  list; a user can own several projects), subscribe the `-zagent` userData, `prewarm(stack.id)`,
  raise the wizard at `picking`. The `claiming` state is skipped; no navigation — the user is
  already on the project detail.
- The `claimZcpPool` cookie is never read, written, or cleared by the dev entry. A stale pending
  cookie is harmless — the drain fires only at auth time (`storeUserDataSuccess`) and the cookie
  self-expires (10 min max-age). "Composition" = clean bypass.
- Already-authorized picked agent: the wizard's own semantics (ticket 08 §6 — auth-complete
  auto-fires `launch-agent`) skip the auth step straight to `launching` — exactly the repeated
  launch loop dev testing needs. A second terminal in a running container is an accepted dev
  consequence, not a bug.
- Project without a ZCP stack: **silent no-op** (param stripped, nothing raised; `console.warn`
  at most) — it is a dev affordance, error UI would be dead weight.
