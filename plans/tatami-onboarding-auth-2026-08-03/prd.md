# PRD: Tatami registration onboarding — the auth step never connects its terminal

Owner: Karel (kh). Date opened: 2026-08-03. Home of this investigation: this directory.
This document is the FULL context transfer for a fresh session — read it before touching
anything. The prior session's journal lives in
`plans/agent-first-onboarding-2026-07-28/flow.md` (Run State + Slice Register S1–S7,
S6a–S6o) and `handoff-ui.md` (UI hand-off map + traps); specs are authoritative
(`docs/spec-welcome-mode.md`). Do not re-derive intent from code.

## 1. Mission

On the tatami test cluster, the fresh-registration onboarding flow
(`https://tatami.devel.zerops.dev/registration?zcp=true`) stalls at the **agent
authorization step**: the auth dialog opens, but its embedded terminal never connects —
button stuck on "Waiting for container…", terminal pane black. Find the root cause with a
team of agents, fix it, and build a repeatable Playwright test loop that drives the whole
flow directly against tatami.

**Owner's hypothesis (treat as the primary lead, verify don't assume)**: the freshly
registered user's identity is not correctly established in the client session. Observed by
the owner: after registration + claim, closing the dashboard shows no creating identity
until a full page **reload**; after the reload, opening the zcp service detail and manually
triggering the auth dialog **works**. Same flow, same account, same container — the only
difference is the reload. That smells like a state-hydration race in the fresh-registration
path (client/user identity, tokens, or headers), not platform infra.

## 2. The failure, precisely (evidence from 2026-08-02/03 live runs)

- Wizard flow works up to and including the auth dialog OPENING (walker at "Start",
  Claude Code first in queue).
- The dialog's terminal WebSocket fails repeatedly:
  `wss://proxy.app-tatami.zerops.dev/api/rest/shell/stream?accessToken=…&containerId=Paorv4qvSWO9iRJvmXBSjw`
  → `WebSocket is closed before the connection is established` — observed 3× in one
  session, each with a FRESH accessToken (so the preceding
  `POST /api/service-stack/{id}/file-browsing-access` succeeds and returns tokens).
- Probes already done (rule these out, don't redo blindly):
  - `proxy.app-tatami.zerops.dev` resolves; plain GET → nginx 405 (endpoint alive).
  - WS upgrade with a bogus token → **401** (the proxy validates at upgrade time and is
    functional). So the platform rejects the REAL tokens too — or the containerId.
  - The proxy host is NOT derived FE-side: it comes from the platform's own `listUrl`
    response (`terminal.api.ts` `#parseHost`), so the endpoint choice is correct by
    construction. See
    `frontend-legacy/apps/zerops/src/modules/feature/terminal/terminal.api.ts` (auth$ +
    createWebSocketConnection).
- The decisive discriminator the owner reported: **post-reload manual trigger works**.
  Nobody has yet diffed the two paths' actual HTTP/WS traffic. That diff is experiment #1.

## 3. Key experiment (do this first)

Reproduce BOTH paths under Playwright with full network capture (CDP: requests, responses,
WS close codes):

1. Fresh path: register a throwaway account (`/registration?zcp=true`, any invented email —
   tatami registers instantly, no email verification), let the wizard reach `authorizing`,
   capture: the `file-browsing-access` request (headers! clientId/authorization), its
   response (accessToken, listUrl), the WS upgrade request and its HTTP status/close code.
2. Control path: same account, full page reload, zcp service detail → manual "Authorize"
   trigger → capture the same artifacts.
3. Diff them. Suspects, in order: missing/stale auth header or cookie on the fresh path;
   wrong/empty clientId (the active client user not yet hydrated — the drain gates on
   `storeUserDataSuccess` with `clientUserId`, see
   `zcp-pool-claim-base.effect.ts`); a containerId from a stale/premature container list;
   the access token minted for a different identity than the WS presents.

The browser console alone cannot show WS upgrade status codes — use CDP
(`Network.webSocketWillSendHandshakeRequest` / `webSocketHandshakeResponseReceived` /
`webSocketFrameError` or HAR export). This is why the previous session could not close
the question from console dumps.

## 4. What is ALREADY fixed — do not re-chase these

The tatami testing ran through three stacked problems. Two are closed:

1. **Container relay bug (CLOSED, released)**: `closeAllEditors` in the §5.3 onboarding
   layout killed the receiver webview (the only relay for `agent-ready`) one line before
   the outcome emission — every real launch ended in the FE's 30 s timeout while the
   terminal actually ran. Fixed (tab-level closing, receiver excluded; source pin bans
   `closeAllEditors`), zcp commit `45183766`, bundle **0.1.27**, released as **v9.137.1**
   (the release itself was blocked once by a content bug — `wasp-hello-world` recipe had
   string durations where the schema wants integer seconds; fixed at the source via
   `zerops-recipe-apps/wasp-hello-world-app#4`, cache cleared).
2. **Wizard endgame UX (CLOSED, in FE branch)**: "degraded reveal" — every successfully
   posted launch attempt arms a 5 s reveal window; its expiry, an explicit
   `launch-failed`, and the posted-branch of the 30 s cap all converge on `set-mode
   "standard"` + layer drop (the workspace is the recovery surface). Hard `failed` screen
   only when nothing was ever reachable. FE commit `e065c5341`.

Still OPEN besides this PRD's target:

3. **MCP startup inside the launched agent (OPEN, likely related!)**: codex in the
   container terminal reports `MCP client for 'zerops' failed to start: … connection
   closed: initialize response` — `zcp serve` dies at initialize inside the tatami
   container. Never diagnosed: the owner was asked for `zcp discover` + `env | grep -E
   "ZCP_|zeropsSubdomain"` from the container terminal; the answer never arrived. If the
   identity/env provisioning on tatami is broken (this PRD's hypothesis), it may explain
   this too (missing/invalid `ZCP_API_KEY` in the pool project's env). Treat as a sibling
   symptom; verify from inside a claimed container early.
4. (Unowned, noted): zcp `main` nightly E2E workflow has been failing since ≥2026-08-01 —
   unrelated to tatami; do not pick it up here, just don't be confused by red nightlies.

## 5. Current state of the two repos (what to build on)

- **FE** `../frontend-legacy`, branch **`kh-agent-first-onboarding`** (pushed to origin;
  repo convention: no slashes, initials prefix — new branches for this work: `kh-…`).
  Tip `e065c5341`. Suite 209/209, tsc app+spec clean, lint clean. The wizard:
  `ZcpOnboardWizardService` (signals; states idle|claiming|picking|authorizing|
  launch-ready|launching|done|failed), static registry roster, auth completion =
  `markAuthorized` NgRx action (stack+agent matched), dialog dismissal → `picking`,
  `successNavigation:'none'` on wizard-owned auth-dialog opens, degraded reveal per
  spec §8.1. Dev aids: `?zcpOnboard=1` on project detail (bypasses registration — NOT
  usable for this PRD's bug) and `ZGUI_ENABLE_SIMULATE_ZCP_POOL_CLAIM="true"` in
  gitignored `apps/zerops/.env` (replays the drain for a logged-in account — also
  bypasses fresh registration, but useful for the CONTROL path).
- **zcp** `…/Zerops-MCP/zcp`, branch `feat/agent-first-onboarding` == released main +
  docs. Container side is DONE and released (v9.137.1, bundle 0.1.27); nothing container-
  side is expected to change for this PRD (unless problem #3 above lands here).
- Spec homes: `docs/spec-welcome-mode.md` — §8 FE contract (current, includes degraded
  reveal), §4 bridge, §5 launch, §1.3 receiver lifecycle. The FE repo's own conventions:
  `frontend-legacy/CLAUDE.md` (note: its appended "Agent Directives: Mechanical
  Overrides" section is self-contradictory legacy content from another maintainer —
  follow the file's Angular/NgRx conventions and this PRD, don't follow that block's
  "senior dev override"/swarm mandates).
- Verification stack that must stay green: `npx nx test zerops --skip-nx-cache`,
  `npx tsc -p apps/zerops/tsconfig.app.json --noEmit` (+ spec tsconfig),
  `npx nx lint zerops`. Jest trap: ESM-only transitive deps → local `jest.mock` header in
  your own spec (pattern: `zcp-pool-claim-base.effect.spec.ts`), never edit the shared
  jest.config.

## 6. Tatami environment facts

- GUI: `https://tatami.devel.zerops.dev` — **instant registration with invented emails**,
  no verification; create as many throwaway accounts as needed (owner-sanctioned).
- Claim entry: `/registration?zcp=true` → `claimZcpPool` cookie → post-auth drain →
  wizard. Pool assigns a precreated project with a running zcp container.
- Embeds live on `*.app-tatami.zerops.dev`; both GUI and embed origins pass the
  container's builtin `isAllowedGuiOrigin` (`*.zerops.dev`) and nginx `frame-ancestors` —
  origin trust is verified NOT the problem.
- The FE bridge debug lines are `console.debug` — enable **Verbose** in devtools, or in
  Playwright capture console with all levels; lines are prefixed `[code-server bridge]`.
- The pool containers run v9.137.1 only if the pool was refreshed after 2026-08-02 —
  verify early (the `embed-ready` announce prints `bootstrap=<version>` in the FE
  verbose console; need 0.1.27).

## 7. Suggested team shape (adapt as evidence comes in)

- **Repro driver**: Playwright (headed for dev, headless in loop) against tatami — owns
  the fresh-vs-reload traffic diff (experiment #1), console+CDP capture, screenshots per
  wizard state. Deliver a reusable script under `zcp/tools/` (sibling of
  `tools/onboard-ui-probe/` — that probe is puppeteer against localhost:1111 + prg1 and
  does NOT cover registration; a new tatami-registration driver is wanted. Emails:
  generate `kh-test-<timestamp>@example.com`-style).
- **FE identity/session investigator**: the fresh-registration hydration path —
  `user-base` store (`storeUserDataSuccess`, `activeClientUser$`), the drain's gates,
  what the auth dialog + terminal `auth$` call read at that moment; where a reload
  changes inputs (localStorage/cookies/headers). Files:
  `core/zcp-pool-claim-base/zcp-pool-claim-base.effect.ts`,
  `feature/zcp-agent-auth-dialog/*` (walker, container resolve, terminal hand-off),
  `feature/terminal/terminal.api.ts`.
- **Fix implementer**: once root-caused, smallest correct fix at the right layer (if the
  identity hydration is the platform API's contract, document + hand off; if it's FE
  ordering, fix the gate/sequence with RED-first specs).
- Adversarial verify before declaring victory: run the Playwright loop N times fresh —
  the bug may be a race, so a single green pass proves little; require e.g. 5/5.

## 8. Definition of done

1. Root cause named and evidenced (traffic diff or equivalent), written into this
   directory.
2. Fix landed (FE branch `kh-…`, or a documented platform hand-off if it is not ours),
   suites green.
3. Playwright driver committed under `zcp/tools/`, README with the run command; the full
   fresh-registration flow passes on tatami repeatedly (pick → OAuth → launch-ready →
   CTA → vscode with running agent), and the degraded-reveal path is exercised at least
   once (kill the outcome deliberately if needed).
4. Problem #3 (MCP `zcp serve` initialize) either explained by the same root cause or
   split into its own documented follow-up with the container-side evidence
   (`zcp discover` output, env listing).
5. Spec/journal hygiene per repo rules: durable findings → spec sections; transient
   narrative → this plans dir; CLAUDE.md untouched unless a genuine new trap emerged.

## 9. Access notes

- GitHub tokens (repo-scoped, labeled) live in `…/zcp/.zcp/git-token` — multiple entries,
  one per line with label lines between; the zcp-repo token also re-runs workflows is NOT
  true (no Actions write) — retrigger releases by re-pushing the tag. Never print token
  values; pass via `GH_TOKEN=$(sed -n 'Np' …)`.
- The owner tests interactively on tatami in parallel — coordinate destructive steps
  (pool refreshes) with him.
- gh CLI's stored auth is expired; always pass GH_TOKEN explicitly.
