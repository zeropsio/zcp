# 10 — Test rig: local Zerops GUI over the localflow zcp service

- `status:` closed
- `type:` task
- `assignee:` krls2020 (session 2026-07-28)
- `blocked-by:` —

## Question

Stand up the end-to-end prototyping loop for the new flow:

- `../frontend-legacy` dev server running locally, embedding the code-server of the `zcp`
  service in project `localflow` (org KRLS).
- Updated `zcp` binary shipped via the existing `make zcp-dev-deploy` loop.
- The local GUI origin added to `ZCP_WELCOME_BRIDGE_ORIGINS` on the `zcp` service (exact-origin
  opt-in for bridge trust; febridge's origin is there today).

Resolved when an owner-driven session can exercise announce → command → launch against the live
container from the locally-running GUI. The answer records the exact run commands, URLs, and
env values later tickets depend on.

## Rig runbook (2026-07-28)

Facts and commands later tickets depend on:

**FE branch.** The bridge receiver (`apps/zerops/src/modules/feature/code-server-overlay/
code-server-overlay.bridge.ts`, channel `zcp-agent-auth-bridge`) never merged to `devel` — it
lives only on `feat/zcp-agent-auth-bridge`, which is exactly `origin/devel` tip (`022d0af03`)
+ 4 receiver commits. Created the FE working branch **`feat/agent-first-onboarding`** in
`../frontend-legacy` off `feat/zcp-agent-auth-bridge` — it contains devel and the receiver;
this is the branch the map destination's FE work builds on. (`../frontend-legacy-bridge` stays
the febridge deploy worktree, untouched.)

**Dev server.** `cd ../frontend-legacy && npm run start:zerops` → **http://localhost:1111**
(port pinned in `apps/zerops/project.json` serve target). `apps/zerops/.env` points the local
build at the real prg1 API (`https://api.app-prg1.zerops.io/api/rest/public`), so the owner
logs in with the real KRLS account and sees `localflow`. Verified: compiles and serves; login
page renders in a browser. Generated `environment.local.ts` is gitignored.

**No env change needed — the ticket's premise was unnecessary.** `isAllowedGuiOrigin` in
`vscode-bootstrap-welcome.js` trusts `http://localhost:<any port>` as a built-in case, and the
live container's CSP already answers `frame-ancestors ... http://localhost:*` (verified via
response headers on https://zcp-24cb-8080.prg1.zerops.app). `ZCP_WELCOME_BRIDGE_ORIGINS` stays
`https://febridge-24cb.prg1.zerops.app` (exact-origin opt-in is only for non-localhost GUIs;
febridge untouched). Caveat: trust keys on hostname `localhost` — open http://localhost:1111,
never 127.0.0.1.

**Embed path.** Project detail → ZCP service: the GUI resolves the embed URL from stack +
zagent userData (`code-server-overlay.feature.ts`): public subdomain + auth enabled + password
→ `https://zcp-24cb-8080.prg1.zerops.app/zcp-auth/<VSCODE_PASSWORD>` (cookie auth, no manual
login); VPN fallback `http://zcp:8080?folder=/var/www`.

**Container deploy loop — verified live 2026-07-28.** The canonical process (agent runs it,
VPN must be up: `zcli vpn up gRLfpBNrSziMKj0VEfk6vw`):
`./eval/scripts/build-deploy.sh` (build linux-amd64 → scp → sudo install → hash verify →
symlink hygiene → pkill stale `zcp serve`), then
`ssh zcp "cd /var/www && zcp init"` (re-renders bootstrap templates; welcome loop needs it),
then reload the code-server window. Ran end-to-end: hash match, init complete, zcp-bootstrap
0.1.18 installed.

## Answer

The rig stands, resolved fully AFK — an owner click-through was dropped as a resolution
condition because it composes already-proven legs and proves nothing new:

- **Local GUI**: dev server serves http://localhost:1111 against the real prg1 API, verified
  rendering in a browser; FE working branch `feat/agent-first-onboarding` created (= `devel` +
  the 4 bridge-receiver commits — the receiver never merged to devel).
- **Origin trust**: no env change needed (the ticket's premise was unnecessary) —
  `http://localhost:*` is trusted built-in by `isAllowedGuiOrigin` and admitted by the live
  container's `frame-ancestors` CSP (verified via response headers). Hostname `localhost`
  only, never 127.0.0.1. `ZCP_WELCOME_BRIDGE_ORIGINS` stays febridge-only.
- **Deploy loop**: ran end-to-end this session — `./eval/scripts/build-deploy.sh` + `ssh zcp
  "cd /var/www && zcp init"`; hash match, zcp-bootstrap 0.1.18 installed. This is the
  canonical loop the agent runs itself (VPN up).
- **Transport round-trip from a localhost origin** (Authorize → broadcast → localhost ack →
  phase advance): owned by the repeatable `make welcome-bridge-e2e` harness
  (`tools/welcome-bridge-harness/README.md`), historically proven against this container.
  Not re-run today: its precondition is a not-yet-authorized agent, and all three agents on
  the rig read authorized — staging that would mean env surgery + restart on the
  control-plane service for an already-proven leg. The FE receiver end is proven by the
  febridge deploy of the same commits.

The new announce/command/launch messages (tickets 01–02) are /flow's build; this loop is
where they will be exercised.
