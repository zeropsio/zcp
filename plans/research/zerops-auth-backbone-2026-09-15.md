# Research: Zerops auth as the identity backbone — Mate, Gitea, CI and the four flows

Date: 2026-09-15 · Repo state: z3 `ca22ea50c` (mate 0.10.0), zcp `9314c8a2`, `zeropsio/recipe-gitea`
`abbde78`, Gitea 1.27.2.

The question was: test in depth what Zerops auth can be used for, and every part the self-hosted
Gitea / recipe / CI design touches or needs added. Answered with **live probes** against the
Onboarding org, a **local Gitea 1.27.2 lab** and a **working prototype** of the proposed identity
path, plus a code audit of the fork, zcp and `recipe-gitea`.

Measured facts live in the fork's ledger — `docs/internals/zerops/verified.md`, sections
*"Zerops auth surface — token kinds, roles, delegations and the door — 2026-09-15"* and *"Gitea
1.27.2 as a mirror of Zerops roles — local lab and a Zerops-backed OIDC prototype — 2026-09-15"* —
and open questions in `questions.md` (Q-16, Q-17, and the update to *"How does a Mate get its git
credential without a human?"*). This note is the reasoning. Everything marked **DESIGN** is a
proposal, not a decision.

Everything created for the probes (5 projects, 22 integration tokens, 2 local Gitea instances) was
deleted; the org was verified back at 9 tokens, 10 projects, 7 delegations, 11 members.

> **Superseded in part.** The measured facts and findings here stand. The design parts below are
> replaced where they differ; the current design is *Where the design stands* in
> `plans/zerops-auth-backbone-implementation-2026-09-15.md`. What changed:
>
> - **Gitea starts at sign-up**, in the background — not as a later owner action.
> - **Two kinds of repository per group:** service repos (code and `zerops.yaml`, one per codebase)
>   and one **group repo** holding the recipe — every tier's whole import in the published recipe
>   layout. The import never lives in a code repository, since an environment has many services from
>   many repositories.
> - **zcp's two jobs from the first minute:** connect git for each dev pair through the broker, and
>   propose and maintain the import file in the group repo.
> - **Every Mate keeps its own dev/stage pair**; the group's stages are separate, any number of them,
>   each fed by a branch or a mix of branches, following `main` by default.
> - **Release is orchestrated by workflows, not a lone button**, and no deploy key sits in Gitea, a
>   repository or a runner: the **broker** (this note's "reconciler") holds the only deploy key and
>   deploys only what protected branches and tags allow (D15).
> - **Gitea inside Mate:** the app acts in Gitea as the person over Gitea's OAuth2 (PKCE).
> - **Rejected:** a release repository per app, deploy secrets on runners, and a project of their own
>   for the runners — §4.6's "runners leave the Gitea project" is superseded (2026-09-16). A container
>   reads only its own service's variables unless a project sets `envIsolation: none`, so job code
>   beside Gitea cannot read its admin token or database password; what the pool's place does require
>   is that nothing there trusts the private network.
> - **Identity needs no relay** (D1, decided 2026-09-16): the app mints a throwaway, grant-less Zerops
>   token named for one Mate or one Gitea; the receiver reads who created it and checks that person's
>   role itself, and the app deletes it. Measured to work (the fork's ledger, *A member's throwaway
>   token as their identity*), which answers Q-17 and removes §0's premise that the relay is the only
>   component able to fill Zerops's identity gaps.
> - **Every decision is closed** (the guide's decision table, D1–D18): among them, adoption is the
>   new-app flow with existing source code, never in place, and the relay is not part of the design.

---

## 0. Answers on one screen

**What Zerops auth gives us.** Five tiered roles at org level with per-project overrides that can
*lower* as well as raise — even an org OWNER — so "read-only on production" is expressible for
anyone. Three token kinds. Integration tokens that are first-class identities (they pass the Mate
door). One-use, exact-shape **delegations** that let a person pre-authorize a machine to mint one
scoped token. Attribution of every process to the identity that ran it. `BASIC_USER` is enough to
deploy. A stage→production **promotion** that a browser can run on the approver's own session.

**What it does not give us.** No OIDC or OAuth-provider surface, no audience-bound or expiring
tokens, no membership-change events, no ACL on tags, no per-secret ACL, and no TTL on integration
tokens. Every one of those gaps has to be filled by a component we own — and the only component
every user already trusts is the **relay**.

**Findings in today's product, independent of Gitea** (§2):

1. **Every Mate sign-in hands the user's personal access token to a container** that every
   `BASIC_USER` member of that project can modify. The hosted client signs in by the platform's
   `authorize-app` hand-over, which mints a *personal* token: all of the user's organizations,
   permanently in sudo (reads secrets in clear), no expiry observed.
2. **Integration tokens pass the Mate door**, and an exchange that does not narrow its scopes gets
   `exec:operate` + `terminal:operate`; so the container's own `ZCP_API_KEY` is a Mate operator key —
   and zcp's `build-integration` writes it into a forge secret.
3. **A Mate can join another group** by writing its own project's tags; the client's group reach
   then widens its token onto that group (prod logs included).
4. **Every Mate can create one project** (a platform-minted delegation on its token), and zcp's
   launch token lands in a GitHub secret and is never revoked.
5. **Revocation lags**: ≤ 17 minutes at a Mate, unbounded in a Gitea web session.
6. **Anyone with a shell in a Mate holds project `ADMIN`.** `ZCP_API_KEY` is in the container's
   world-readable env store and in the Mate server's process env, so every agent turn, terminal and
   exec inherits it — a `BASIC_USER` colleague operating a Mate acts as `ADMIN`. Lowering the Mate's
   token to `BASIC_USER` on its own project closes this and the root of 3 at once (§2 F8).

Side findings from the same audit (§2.1): the Mate card's *Update* verb is refused for web users
(scope), agent provenance is recorded but shown nowhere, and the relay is designed but not deployed.

**The architecture that follows** (§4, prototype-proven end to end):

```
                person's Zerops session (browser/app)
                         │  (never leaves for a container)
                         ▼
   ┌──────────────────── relay = Zerops-backed OIDC provider ────────────────────┐
   │ verifies the token live, computes org role + per-group rights, signs claims │
   └───────┬───────────────────────────────┬─────────────────────────────────────┘
           │ short-lived assertion (aud=env)│ OIDC code flow, groups claim
           ▼                               ▼
     Mate door (per env)            Gitea (per account): groups → org teams at login
     role → scopes, ownership gate        ▲
                                          │ periodic: users/teams/tokens/bots
                     reconciler in the Gitea project (org READ_ONLY Zerops token + Gitea admin)
   Release to production = promotion run by the approver's own session (Zerops enforces the role)
```

**What to build, by component** — §6. **What the four flows become** — §5.

---

## 1. Zerops auth, measured

Condensed from the ledger section; every row there has its evidence.

### 1.1 Token kinds

| Kind | How it is made | Notable |
|---|---|---|
| **Session** | `POST /auth/login` (the dormant mobile app; the web's `login()` has no UI caller, `apps/web/src/zerops/ZeropsSessionProvider.tsx:280-312`) | `expiresIn: 432000` (5 days, `verified.md:150`) + refresh token; reads sensitive values as `REDACTED` unless in sudo |
| **Personal** | `POST /user-token` from a session, or minted by the platform's `authorize-app?app=zerops-code` hand-over — **what the hosted web and desktop clients hold** (`packages/client-runtime/src/zerops/handover.ts:6-14`; the "Zerops Code · macOS Chrome" tokens in the account's list) | "application token"; permanently in sudo; user's full rights across **all** their orgs; no expiry observed (tokens from 2024 still listed) |
| **Integration** | `POST /client/{id}/integration-token` | an **org member** `token-<id>@zerops.io` with its own org role + project grants; `/user/info` answers with `id` = token id; no TTL; cannot manage tokens without a delegation |

### 1.2 Roles

- Effective role on a project = project override ?? org role. Overrides lower org roles, **the
  OWNER's included** (the owner set to `READ_ONLY` on a project got `403` writing there).
- Measured per role: tags and rename need `OWNER`/`ADMIN`; env writes, service stop, **`zcli push`**
  need `BASIC_USER`; `READ_ONLY` reads everything non-sensitive, gets a log-stream URL, reads
  secrets as `REDACTED`, cannot deploy.
- Application tokens at `BASIC_USER`+ read sensitive service env **in clear** via
  `GET /service-stack/{id}/user-data`; `/reveal` needs a session in sudo.
- Creating a project: `OWNER`/`ADMIN`, or anyone with `canCreateProjects` — who then becomes the new
  project's `OWNER`. For a token below `ADMIN`, only `POST /client/{id}/project/import` works; the
  bare `POST /client/{id}/project` answers `400 userNotFound`.

### 1.3 Delegations

A person grants an integration token the right to mint **one** token of **exactly** a given shape
(org role, finance flags, `canCreateProjects`, project permissions). Mismatched or narrower shapes are
refused; the delegation is consumed on success; the minted token is owned by the granting person.
Project-scoped delegations work (a token with no access to B minted a token with `ADMIN` on B).
Integration tokens can neither create nor delete delegations or tokens themselves.

### 1.4 Identity and visibility

- `GET /user/info` works for every kind; for an integration token it names the token.
- A token reads its own `createdByUser`; the member list resolves it to a person — until the token
  is deleted, after which the platform can no longer name the person behind past deploys.
- **Every token, including a `NO_ACCESS` one with no grant, lists all org members (with emails and
  roles) and every project tag** (all group ids and names).
- `NO_ACCESS`-org tokens get `403` on `GET /client/{id}/project` but `POST /project/search` returns
  exactly their granted projects.
- One org `READ_ONLY` token can compute every member's effective role on every project
  (`user/list` + `GET /project/{id}` `userRoles`) — measured for all 25 members.

### 1.5 Revocation and attribution

- Deleting or regenerating a token kills it at the platform within a second.
- Processes and app versions record `createdByUser` = the identity that ran them
  (`token-…@zerops.io` for tokens).

### 1.6 What Zerops can be used for — the catalogue

| Use | Mechanism | Status |
|---|---|---|
| Who is signing in to a Mate | door: `GET /project/{id}` + `/user/info` with the caller's token | exists (F1 exposure) |
| Who is signing in to Gitea | relay OIDC, claims from the caller's token | **prototype works** |
| Mirroring roles into Gitea teams | org `READ_ONLY` token → reconciler | **measured input** |
| A Mate's own identity | its `zcp-<project>` integration token (`ADMIN` on its project) | exists |
| Proving a Mate to an issuer (Gitea credential) | `ZCP_API_KEY` → `/user/info` + own token detail + project | **measured** |
| Machine principal for intake | any `BASIC_USER`+ integration token passes the door | exists (F2) — needs a policy |
| Read-only sight of a Mate's group | token widened to `READ_ONLY` on siblings | exists (F3) |
| Who may release to production | `BASIC_USER`+ on the prod project; promotion API | **measured** |
| A person pre-authorizing a machine | delegation (one use, exact shape) | measured |
| Keeping non-owners out of the Gitea project | per-project override (lowers even `ADMIN`) | measured |
| Deploy audit trail | process `createdByUser` | measured (lost for deleted tokens) |
| Recovering code from a live service | `GET /app-version/{id}/app-code` (the uploaded source package) | measured for a CLI deploy |
| SSO / audience-bound / expiring tokens / membership events / tag ACL | — | **not available** |

---

## 2. Findings in today's product

### F1 — Every Mate sign-in exposes the user's personal access token

The hosted client's Zerops credential is a **personal access token** minted by the platform's
`authorize-app` hand-over (`packages/client-runtime/src/zerops/handover.ts:6-14`: "user-scoped, so it
still spans every organization"), kept in `localStorage` with no refresh. It sends that token to
each Mate (`apps/web/src/components/zerops/ZeropsProjectsPage.tsx` `autoConnectServedZeropsEnvironment`
passes `client.session?.accessToken`; body `{token}` at `POST /api/auth/zerops-identity`,
`packages/client-runtime/src/authorization/zerops.ts`), and again at every renewal inside the
15-minute window. The Mate server runs in a container where every `BASIC_USER` member has a terminal,
code-server and SSH, and the home is shared (`spec-mate.md` §8.3). A member who patches the server
can capture the token of every colleague who connects: a personal token is permanently in sudo
(measured: it reads every sensitive env in clear), spans **all** of that colleague's organizations,
and has shown no expiry. The door's "never stored, never logged" promise holds for our code, not for
a container other people control.

**Fix direction (DESIGN):** the container never sees a Zerops token. The relay verifies it and issues
a short-lived assertion with `aud` = the environment; the door verifies the relay's signature (§4.1).
Interim alternative: the client mints a per-Mate integration token (`NO_ACCESS` + its own effective
role on that project) and presents that — its blast radius is a project the attacker already
controls; the person is recoverable from `createdByUser`. Q-17 records the choice.

### F2 — Integration tokens are Mate operator keys

Measured on a live 0.10.0 Mate: project `BASIC_USER`/`ADMIN` tokens and an org-wide `BASIC_USER`
token all got sessions with `orchestration:operate terminal:operate exec:operate` — the exchange
issues whatever subset is requested (`apps/server/src/auth/EnvironmentAuth.ts:669-672`), and a
caller that requests nothing gets the whole grant; the product's own clients narrow to the five
standard scopes. Consequences:

- The container's own `ZCP_API_KEY` signs into its own Mate door.
- zcp's `build-integration` puts that key into a forge secret (`gh secret set ZEROPS_TOKEN -b
  "$ZCP_API_KEY"`, `internal/tools/workflow_build_integration.go:331-343`), and any writer of the
  repository can print it from a workflow (Gitea lab G6) — a forge-side Mate key.
- Any `BASIC_USER` org-wide integration token is a key to **every** Mate in the org.
- The previous research (`mate-intake-credential-ownership-2026-09-09.md` §1) said there is no
  machine principal. There is; it is just unintended.

**Fix direction:** decide the policy explicitly. With relay assertions (F1) the door stops taking raw
Zerops tokens, and machine principals become an explicit relay feature (distinct subject type,
restricted scopes — the intake case). Interim: detect integration tokens at the door (a token can read
`GET /client/{id}/integration-token/{own id}`; a person's token cannot be a token id) and refuse or
downscope them. In zcp, never put `ZCP_API_KEY` into a forge secret.

### F3 — A Mate can move itself into another group

Measured: a project-`ADMIN` token (every Mate's `ZCP_API_KEY`) rewrote its project's tags to
`mate:g:<another group>`, `mate:role:prod`, `mate:bot:Impostor`, and renamed the project; any token
lists every group id through `GET /client/{id}/project/tag/list`. The client's group reach derives
groups from those tags and rewrites every Mate token in a group to `READ_ONLY` on its siblings, in the
browser of any signed-in member whose organization admission allows writes — `canCreateProjects ===
true`, which OWNER and ADMIN always have (`apps/web/src/zerops/ZeropsInventoryProvider.tsx:519-529`,
`packages/client-runtime/src/zerops/data/commands.ts:37-42`) — who opens the projects screen,
failures swallowed (`apps/web/src/zerops/useZeropsGroupReach.ts:58-128`, called at
`apps/web/src/components/zerops/ZeropsProjectsPage.tsx:1250`; the rule is
`packages/client-runtime/src/zerops/groupReach.ts`). Payoff for the hopper: `READ_ONLY` on the
victim group's projects — measured to include a one-hour **log stream** — and non-sensitive env,
which carries the `connectionString` with its password (`verified.md`, 2026-09-06). A `BASIC_USER`
human with a terminal in a Mate can do the same through its `ZCP_API_KEY`. Precondition and code path
measured; not run end to end.

**Fix direction:** tags are hints, not authority. The reconcile may **narrow** automatically and
**widens only on a human action** in the client ("Nova joined Acme CRM — let it read 2
environments?"). A Mate cannot write integration tokens, so without an automatic widen a tag change
gains it nothing. And with F8's fix the Mate cannot write its tags in the first place.

### F4 — Every Mate can create a project; launch tokens outlive their use

`PUT …/development-container` with `createIntegrationToken: true` mints the `zcp-*` token **and a
one-time delegation `NO_ACCESS` + `canCreateProjects`** granted by the creating person; all seven older
`zcp-*` tokens carry one. Used, it yields a token that creates a project and becomes its `OWNER`
(measured through `project/import`). That is zcp's launch-production hand-over
(`internal/tools/launch_delegation.go`); the minted launch token is then copied into the GitHub
secret `ZEROPS_TOKEN_PROD` and never revoked (`workflow_launch_production.go:2122-2134`,
`launch_confirm.go:13-22`). Anyone with repo write can read it (G6) and create projects in the org.

**Fix direction:** production is created by the client in the group flow (§5.4), so the client
deletes the delegation right after environment creation, and sweeps `zcp-launch-*` tokens. zcp's
delegated launch stays for Mates outside the group model.

### F5 — Revocation windows

- Mate: a deleted token cannot open new sessions, but an open session lives out its 15 minutes and a
  grant minted before the deletion can be exchanged within its 2 minutes → up to ~17 minutes.
- Gitea: web sessions are never re-validated against the IdP (lab G11); a header-auth login persists
  as a cookie (G1). Removal needs the reconciler (§4.3) plus a bounded `SESSION_LIFE_TIME`.
- Nothing tells either side that a person left: there are no membership events (Q-16 asks what even
  happens to the tokens they created).

### F6 — Any token sees the whole org

Every integration token — every Mate — reads all org members' emails and roles and every group
name. Platform behaviour; nothing in the product can narrow it. Worth knowing for prompt-injection
threat models.

### F8 — A shell in a Mate is project `ADMIN`

`ZCP_API_KEY` sits in the container's live env store, "root-owned, world-readable", and the Mate
server's process environment is that store plus its contract file
(`zcp:internal/mate/mate.go:113-125`), so every agent turn, terminal and `exec` inherits it; the spec's
"mate never reads it, never forwards it" (`spec-mate.md:45-47`) holds only for reading. The token is
`ADMIN` on the project (plus `READ_ONLY` on the group, plus the one-time project-creation
delegation), so a `BASIC_USER` member who operates a Mate acts as `ADMIN`: rename and re-tag the
project (F3), create a project it then owns (F4). It also means any role-derived Mate access has a
ceiling of "whatever the container token can do". The Data Console already reads database
credentials with it (`zcp:internal/dataconsole/zcpadapter/adapter.go:79-90`) behind
`orchestration:read`.

**Fix direction (DESIGN):** the client lowers each `zcp-*` token to **`BASIC_USER` on its own
project** right after creation (the same `PUT` group reach uses; `findMateIntegrationToken` must then
match `ADMIN` or `BASIC_USER`). The permission matrix says zcp's work fits in `BASIC_USER` — service
import, deploy, env writes, stop/start, subdomain access are all `BASIC_USER` operations; only project
rename/tags/delete, mode upgrades and member management need `ADMIN` — but zcp has never run on it, so
it is an open question to test (§8). With it, operating a Mate is `BASIC_USER`-equivalent, matching
the humans it admits, and a Mate can no longer write its own group tags.

### 2.1 Side findings

- **The Mate card's *Update* verb is refused for web users.** The web asks the exchange for the five
  standard scopes (`apps/web/src/connection/platform.ts:149-152,318-321`), the server issues exactly
  those, and `zerops.mate.update` requires `exec:operate` (`RpcAuthorization.ts:100`); a five-scope
  session gets `EnvironmentAuthorizationError requiredScope:"exec:operate"` (`verified.md:367`).
  From code plus that measurement; the verb was not clicked in this pass.
- **Agent provenance is recorded and published, but shown nowhere.** `authorizedBy` is on the feed,
  yet no component in `apps/web` or `apps/mobile` consumes `agentOwnershipNotice` or
  `resolveAgentOwnership`; the ledger row claiming "the client shows a line" (`verified.md:760`) is
  corrected. A consumer must compare against the `zerops-user:<id>` subject, not the bare user id the
  module's doc names.
- **The relay is designed, not deployed** (`verified.md:501`: "Deployment to a `z3-relay` project is
  NOT done"). Its project binding also accepts any `200` on `GET /project/{id}` — a `READ_ONLY` user
  can link — and never re-verifies (`infra/relay/src/zerops/ZeropsProjectBinding.ts:161-163`).
- **A `BASIC_USER` "Set up Mate" can end half-done:** the container import is a `BASIC_USER`
  operation but the naming `PUT` that follows is `ADMIN`-only (measured), and the client allows the
  verb (`ZeropsProjectsPage.tsx:547-596`).
- **Two admission rules disagree:** organization writes need `canCreateProjects === true`
  (`ZeropsInventoryProvider.tsx:519-529`), the wizard's precheck accepts OWNER/ADMIN regardless
  (`accountScope.ts:46-52`).
- **Spec drift:** `spec-mate.md` §5.4 says the web must not manufacture tag groups
  (`:1194-1200`); the fork writes `mate:g`/`mate:role`/`mate:name`/`mate:bot`
  (`packages/client-runtime/src/zerops/groups.ts`). `groupReach.ts:24-25` and zcp's
  `agents_group.md` say `zerops_*` tools answer for the group; every tool is bound to one project
  (`zcp:internal/server/server.go:158-159`) — only `zcli` and the raw API see the siblings.

### F7 — Gitea today

- One site-admin `mate` user owns every repo and each Mate's token is `write:repository` on that
  user: **every Mate can push to every group's repository** (lab G3, confirmed). The recipe's
  runner token is instance-wide and readable by job code; secrets reach any writer (G6).
- The recipe binds `0.0.0.0:3000` with runners in the same project (`recipe-gitea` `app.ini:10-11`,
  `zerops.yaml:64`): turning on header auth there would let any workflow `curl -H 'X-WEBAUTH-USER:
  mate' http://web:3000` as site admin.
- `REQUIRE_SIGNIN_VIEW=false`, `DEFAULT_ALLOW_CREATE_ORGANIZATION=true`, unlimited repos, SSH on
  2222 outside any role sync (`app.ini:13-15,41-45`).

---

## 3. Gitea 1.27.2, measured (what matters for mirroring)

| Primitive | Verdict |
|---|---|
| Reverse-proxy header auth | **Unsafe here.** `TRUSTED_PROXIES` does not gate the header; the header authenticates git; a non-stripping proxy is full impersonation; the login persists as a cookie. |
| OIDC login source | **Works.** `--group-team-map` + `--group-team-map-removal`, `--admin-group`, `--restricted-group` re-sync on every login. Sessions are not re-validated; git/API need tokens. |
| Org teams | Confine `write:repository` tokens to the team's repos; site-admin tokens reach everything. |
| Restricted users | Hide public repos/orgs; combine with `max_repo_creation: 0`, `allow_create_organization: false`. |
| Bot users | `--user-type bot` (CLI only): password-less, token-only, pushes as a writer. |
| Protected tags | **No admin override**; enforced for push, tags API and releases. |
| Protected file patterns | Absolute on direct push for everyone; per branch (`**` for all); merge override blocked by `block_admin_merge_override`. |
| Actions secrets | Reach any writer via branch push or same-repo PR; no environment/approval gate; no job OIDC. |
| Job token | Bot `-2`, writes by default; `permissions: {contents: read}` clamps it. |
| Webhooks | `pusher` on `push`; HMAC-SHA256 signed. |
| Templates / migrate / mirrors | `${REPO_NAME}` etc. expand; GitHub migrate works; pull mirrors are read-only; push-mirror API exists. |
| Deprovisioning | Admin lists/deletes other users' tokens; purge-delete users. |

The prototype closed the loop with live data: six Zerops identities logged into a local Gitea via a
~200-line Zerops-backed OIDC provider and landed in exactly the teams their Zerops roles imply; a
role change moved one at its next login; a deleted identity could no longer log in (but kept its team
until something removes it).

---

## 4. The architecture (DESIGN)

### 4.1 Identity: the relay becomes a Zerops-backed OIDC provider

- **Endpoints:** discovery, `/authorize`, `/token`, `/jwks`, `/userinfo`, plus `/assert` for the Mate
  door. Keys rotate; containers cache the JWKS.
- **How it learns who you are:** the relay cannot read the Mate app's storage and must never collect
  Zerops passwords. `/authorize` bounces to a Mate-app route that already holds the Zerops session and
  posts that token to the relay (browser → relay, TLS); the relay verifies it with `/user/info`,
  computes claims **with the person's own token** (`/project/search` + `GET /project/{id}`), issues
  the code and returns to the client application. The Zerops token never reaches Gitea or a
  container.
- **Claims:** `sub` = Zerops user id, `email`, `name`, `preferred_username`, `groups` =
  `org:owner` · `g:<group>:read|write|release`. Short ID tokens (5 min).
- **Per-account clients:** creating Gitea registers an OIDC client for that instance (redirect URI =
  its callback), authorised by the owner's session; client id/secret go into the Gitea service env and
  `admin-init.sh` adds the source with `gitea admin auth add-oauth`.
- **The Mate door:** accepts a relay assertion (`aud` = environment id, `sub`, `role`, 2-minute
  `exp`) instead of a Zerops token; everything after the grant is unchanged. Closes F1 and makes F2 a
  policy the relay owns.
- **Cost:** the relay becomes a hard dependency for new sign-ins (open sessions survive ≤ 15
  minutes). It is the designated central service and its code already verifies Zerops bearers
  (`infra/relay/src/zerops/ZeropsAuth.ts`), but it is **not deployed** (`verified.md:501`); deploying
  it becomes a prerequisite, with the availability story of an IdP.

### 4.2 One role function, three enforcers

Input: org role and per-project overrides → effective role on each environment of a group.

| Output | Rule |
|---|---|
| **Mate server** (per environment with a Mate) | `BASIC_USER`+ → operate scopes · `READ_ONLY` → a **new "view" scope** (refused today at `ZeropsIdentity.ts:194`) — not today's `orchestration:read`, which also opens the Data Console on the container's database credentials, `/var/www` file reads including env files, and the browser stream (`RpcAuthorization.ts:71-76,96,106-107`) · `NO_ACCESS` → refused. Plus the ownership gate: an OAuth-authorized agent is driven only by its recorded authorizer (`authorizedBy` already exists, `packages/contracts/src/zerops.ts:325`); token agents by any operator. Ceiling: whatever the container token can do (F8). |
| **Gitea** group org | `readers` if ≥ `READ_ONLY` anywhere in the group · `writers` if ≥ `BASIC_USER` on a dev environment · `releasers` if ≥ `BASIC_USER` on production · site admin iff org `OWNER`. |
| **Release** | Zerops itself: `BASIC_USER`+ on production (measured). Gitea `releasers` + protected `v*` tags only keep the record honest. |
| **A Mate** | a Gitea **bot** user in its group's `writers`, `restricted`, no repo/org creation; never `releasers` unless the owner turns "Mates may release" on (which is a Zerops grant: `BASIC_USER` on production). |

The prototype implemented exactly the Gitea row against live roles.

### 4.3 The reconciler (in the Gitea project)

Holds a Gitea admin token (`write:user`, `write:org`, `read:admin` — not `all`) and an org
`READ_ONLY` Zerops token minted by the owner when Gitea is created. Every few minutes:

- recompute every member's rights (one token, measured for 25 members) and fix teams, admin and
  restricted flags — covering people who never log in again;
- disable and purge Gitea users whose Zerops membership is gone; delete their tokens (G10);
- keep one bot user per Mate (from the `zcp-*` tokens) in the right group org;
- issue a Mate its Gitea token on request: the Mate's zcp presents `ZCP_API_KEY`; the reconciler
  names it (`/user/info`, own token detail → project, `createdByUser`) and checks the group against
  the **human-confirmed** registry (F3), not the self-written tag. Answers the ledger question "How
  does a Mate get its git credential without a human?" with shape (2).

Why here and not in the relay: it needs Gitea admin and an org-wide Zerops reader; per account, in a
project only owners can reach, is the smaller target than a central store of every customer's reader.

### 4.4 Release: a promotion run by the approver

Measured end to end with a token holding `BASIC_USER` on stage and production only:
`GET /app-version/{stage}/app-code` → `POST /service-stack/{prod}/app-version` → `PUT
/app-version/{id}/upload` → `PUT /app-version/{id}/build-and-deploy` → production `ACTIVE`. CORS is
`*` on the API and the pre-signed download, so the Mate app can do it **on the person's own
session**. So:

- stage deploys continuously (CI or a deployer, §4.6), naming app versions by commit
  (`zcli push --version-name <sha>`);
- "Release `<sha>` to production" appears for people with `BASIC_USER`+ on production; it promotes
  the stage version and then creates the protected `v*` tag at that commit as the releaser;
- no production credential exists anywhere — not in Gitea, not on a runner, not in a broker;
- rollback = redeploy the previous production app version (zcp's R2 primitive, `635046e8`) or
  promote an older stage version;
- the audit trail is the person's own Zerops identity on the process, plus the tag.

It rebuilds from the same source rather than copying a binary; a deterministic build makes that
immaterial, and the platform has no cross-project artefact copy (an ask, §6.6).

### 4.5 Gitea configuration (recipe changes)

`REQUIRE_SIGNIN_VIEW=true`, `DEFAULT_PRIVATE=private`, `DEFAULT_ALLOW_CREATE_ORGANIZATION=false`,
`USER_MAX_CREATION_LIMIT=0`, `ALLOW_ONLY_EXTERNAL_REGISTRATION=true`,
`ENABLE_PASSWORD_SIGNIN_FORM=false` (the admin keeps a CLI break-glass), no reverse-proxy auth at all,
SSH off (or accept key auth outside the sync), bounded `SESSION_LIFE_TIME`, a scheduled `gitea dump`
to object storage (Gitea becomes the source of truth for code on a local-storage volume), the region
fix (PR #1) merged. Branch protection on `main` with no force-push and a catch-all `**` rule
protecting `.gitea/workflows/**`; `v*` protected to `releasers`; `block_admin_merge_override=true`.

### 4.6 Runners and stage deploys

- **Runners leave the Gitea project** (own project per group, or ephemeral): a job must not reach
  `web:3000` or anything that holds Gitea or Zerops credentials. Org-scoped registration tokens
  (`POST /orgs/{org}/actions/runners/registration-token`), passed by env, not argv.
- **Stage** is the one place a Zerops token may meet CI. Default: a `BASIC_USER`-on-stage token as the
  group org's secret, with the exposure said out loud — any writer, Mates included, can deploy to and
  read stage. Stronger option: a **deployer** beside the reconciler that takes the signed `push`
  webhook for `main` and runs `zcli push` itself, so Actions never hold a Zerops credential.

---

## 5. The four flows

### 5.1 New project

1. Needs `OWNER`/`ADMIN` or `canCreateProjects` (creator becomes each new project's `OWNER`).
2. The group is registered in the human-confirmed registry; the reconciler creates the Gitea org and
   its three teams.
3. **The repo is the recipe:** generate from a template repository (`${REPO_NAME}` expansion, G8)
   carrying `zerops.yaml` and the dev/stage/prod import tiers — replaces the mock store (H-26) and the
   lossy export clone (`verified.md` 2026-09-05).
4. The dev environment is created as today; the client then deletes the platform-minted delegation
   (F4). The Mate's bot user exists by the next reconcile; its zcp fetches its Gitea token (§4.3).
5. Never `buildFromGit` from the account's Gitea (`verified.md` 2026-09-07); dev imports the shell,
   code arrives by push.

### 5.2 Adopting an existing project

- Needs `OWNER`/`ADMIN` — tagging is a project write only they have (measured).
- **Default: a project that already serves traffic becomes the group's stage or production, never a
  Mate's home.** A new dev Mate is created beside it; its token is never widened to more than
  `READ_ONLY` there.
- **Compatible**: code in a reachable git repository, `zerops.yaml` with a setup per runtime, only
  importable service types → migrate the repository into Gitea once (optionally push-mirror back to
  GitHub; Gitea cannot sync both ways).
- **Incompatible** — the readiness check names what is missing: no repository (deployed by `zcli`
  from a laptop), no `zerops.yaml`, setups that do not match hostnames, a monorepo. **The platform
  keeps the uploaded source of CLI deploys**: `GET /app-version/{id}/app-code` returns the package, so
  "the code only exists in production" is recoverable by someone with `BASIC_USER` there, and becomes
  the repository's first commit. For `buildFromGit` services the source is the linked repository.

### 5.3 A new Mate in a project

- Anyone who may create projects can add one; the `zcp-*` token belongs to them (Q-16 is what
  happens when they leave).
- A subscription belongs to one person (previous research), so a new Mate is usually a new person:
  their Mate, their agent sign-in, their bot user in the group's `writers`.
- The dev environment comes from the repo's dev tier, not a sibling's export.
- Group reach widens it on the human's confirmation (F3 fix); Gitea membership follows at the next
  reconcile.
- Mates coordinate through branches `mate/<bot>/*` and PRs once a group has two; a solo group may push
  to `main`.

### 5.4 Stage and production

- Production is created **pipeline-first** (`startWithoutCode`) by the client in the group flow;
  subdomain access is enabled after the first deploy (`verified.md` 2026-09-06).
- `main` → stage continuously (§4.6); production by promotion (§4.4); rollback by app-version
  redeploy.
- Defaults: protected `main` (no force-push, workflow files protected), protected `v*`, releases only
  by people with production rights, Mates never release unless the owner grants it.

---

## 6. Work items by component

### 6.1 Fork server (`apps/server/src/zerops`)

- Door accepts relay assertions (`ZeropsIdentityGate.ts`); until then, detect integration tokens and
  refuse or downscope them (F2).
- Role → scope map instead of one `zeropsGrantScopes` (`ZeropsIdentityGate.ts:46`); admit
  `READ_ONLY` with a new view scope (`ZeropsIdentity.ts:194`), split out of `orchestration:read`
  (Data Console, file reads, browser stream), and audit every place that assumes a session can
  operate; move diff reads off `review:write` (`RpcAuthorization.ts:119-120`).
- Ownership gate in the `orchestration.dispatchCommand` handler using `authorizedBy`.
- Fix the *Update* verb's scope: either the web requests `exec:operate` or `zerops.mate.update`
  moves to a scope the web holds (§2.1).

### 6.2 Fork client (`packages/client-runtime/src/zerops`, `apps/web/src/zerops`)

- Relay sign-in; stop sending the Zerops token to containers.
- Lower each `zcp-*` token to `BASIC_USER` on its own project after creation (F8); teach
  `findMateIntegrationToken` the new grant.
- Group reach: auto-narrow, human-confirmed widen (`groupReach.ts`, `useZeropsGroupReach.ts`).
- After environment creation delete the platform delegation; sweep `zcp-launch-*` tokens.
- Show provenance (`agentOwnershipNotice`) and, once decided, enforce it; one admission rule for
  creation; gate row verbs by role so a `BASIC_USER` never starts an `ADMIN`-only flow.
- Gitea: owner-only creation; per-project overrides keeping non-owners out of the Gitea project;
  relay client registration; mint the reconciler's `READ_ONLY` token; "Open in Gitea".
- Release: promotion + tag; rollback.
- Adoption: readiness check; tag an existing project as stage/production; app-code recovery.

### 6.3 Relay (`infra/relay`)

OIDC provider + `/assert`; per-org client registration; JWKS rotation; the Mate-app consent route;
machine principals as an explicit, scoped subject type (the intake case).

### 6.4 `recipe-gitea`

§4.5 configuration; OIDC source from env at first boot; the reconciler process (bots via CLI, tokens,
teams, deprovisioning, Mate credential endpoint); optional deployer; runner addon moved to its own
project with org-scoped registration; backups; merge PR #1.

### 6.5 zcp

- Gitea as a forge kind matched on the `GITEA_URL` host (`internal/topology/git_pat.go:40-80`),
  forge-aware delivery recommendation (`topology/delivery.go:42-49`), `.gitea/workflows` emission.
- Never write `ZCP_API_KEY` into a forge secret (`workflow_build_integration.go:331-343`); mask
  `GITEA_TOKEN` (`ops/env.go:57-61`).
- Fetch the Mate's Gitea credential from the reconciler; feed it to `git-push-setup`.
- Release through promotion when the Mate is allowed to release; keep the delegated launch only
  outside the group model.
- Stale hints: `ZCP_PROJECT_ID` (`workflow_launch_production.go:139`, `workflow_export.go:74`).

### 6.6 Asks for the Zerops platform

1. Audience-bound, short-lived user tokens (or an OIDC surface) so no container ever needs a
   full-power bearer.
2. Membership and role change events.
3. A TTL on integration tokens; a `type` on `/user/info`.
4. Tag write restrictions (reserved prefix writable by `OWNER`/`ADMIN`) or a first-class project
   group.
5. An option to skip the delegation `createIntegrationToken: true` mints.
6. Cross-project app-version promotion without download and upload.
7. A defined lifecycle for tokens when their creator leaves (Q-16).

---

## 7. Corrections to earlier notes

- **`mate-intake-credential-ownership-2026-09-09.md`** — "there is no machine principal": wrong
  (F2). "Attribution does not exist": wrong — `ZeropsAgentAuthorizers.ts` records the authorizer
  since 2026-09-05 and `authorizedBy` is on the snapshot; the ownership gate needs only enforcement
  (and a UI — nothing shows it yet). "`infra/relay` … deployed as the `z3-relay` project": wrong —
  not deployed (`verified.md:501`).
- **The fork's ledger, `verified.md:760`** — "The client shows a line": no component consumes the
  notice; the row carries a dated correction.
- **`z3-new-mate-and-gitea-cicd-research-2026-09-06.md`** — §3.9 decision 1 ("one Gitea user, one
  token per Mate") is superseded: it is exactly why every Mate reaches every repo. §3.14's
  mint-read-revoke from the browser and the CORS dependency become unnecessary once the reconciler
  mints. The stage/prod secrets in Gitea (§3.3) are what G6 shows any writer can read.

---

## 8. Open questions

| Id | Question | Blocks |
|---|---|---|
| Q-16 | What happens to integration tokens (Mates' `zcp-*`, deploy tokens) when their creator leaves or is lowered? | Every lifecycle claim about Mates and CI |
| Q-17 | Relay assertion or per-Mate integration token at the door? | Closing F1 |
| — | Does zcp run end to end on a `BASIC_USER` container token (bootstrap, import, deploy, env, subdomain, the delegated launch)? | F8's fix, and with it F3 |
| — | Does a personal token from the `authorize-app` hand-over ever expire? | How long an F1 capture stays useful |
| — | Can the Mate app hold a Gitea token for the person (for the release tag) without a password — reconciler-minted PAT, or a Gitea OAuth app with Gitea as provider? | §4.4 tag step |
| — | Does `GET /app-version/{id}/app-code` exist for every deploy kind (git builds, old versions) and how long is it kept? | §5.2 recovery |
| — | How are the reconciler's two tokens rotated, and where does the break-glass Gitea admin live? | §4.3 |
| — | Relay availability target once it is an IdP | §4.1 |

---

## 9. Sources

Ledger (fork): `docs/internals/zerops/verified.md` — the two 2026-09-15 sections, plus 2026-09-05
(tags, Gitea as a tool, export), 2026-09-06 (Gitea credentials, READ_ONLY reach, Aurora),
2026-09-07 (`buildFromGit` from Gitea); `questions.md` Q-16, Q-17 and the 2026-09-07 git-credential
entry.

Code (fork): `apps/server/src/zerops/{ZeropsIdentity,ZeropsIdentityGate,ZeropsAgentAuthorizers}.ts`;
`packages/client-runtime/src/zerops/{api,newProject,groupReach,giteaCredential,agentOwnership}.ts`;
`packages/client-runtime/src/authorization/{zerops,remote}.ts`;
`apps/web/src/zerops/useZeropsGroupReach.ts`; `apps/web/src/components/zerops/ZeropsProjectsPage.tsx`;
`packages/contracts/src/{auth,zerops,environmentHttp}.ts`.

Code (zcp): `internal/auth/auth.go`; `internal/platform/{zerops_delegation,token_scoped,project_admin}.go`;
`internal/tools/{launch_delegation,workflow_launch_production,workflow_build_integration,workflow_git_push_setup}.go`;
`internal/topology/{git_pat,delivery}.go`; `internal/mate/manifest.go`.

`zeropsio/recipe-gitea@abbde78`: `app.ini`, `zerops.yaml`, `admin-init.sh`, `runner-init.sh`,
`zerops-project-import.yaml`. Gitea `v1.27.2` source pointers in the ledger rows.

Platform: `https://api.app-prg1.zerops.io/api/rest/public/swagger/openapi.yml` (194 paths, allowed
roles per operation).
