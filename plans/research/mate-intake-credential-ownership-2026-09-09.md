# Research: intake surfaces, subscription ToS, and credential ownership

Date: 2026-09-09 · Repo state: z3 `ca6b665cd` (mate 0.8.0), clean tree.

Prompted by a competitive positioning draft comparing Mate to Devin / Jules / Codex cloud / Amp.
Three questions, in the order they were asked:

1. What would it cost to reach Devin's integration surface, and on which side does the work land?
2. Do provider *subscriptions* make a shared intake queue (Discord/Slack commands) a ToS problem?
3. If so, should only the person who authorized an agent be able to operate it?

Everything under **FACTS** is read out of the tree or the spec and carries a citation. Everything
under **DESIGN** is proposal from this session and is not decided, not built, and not evidence.
Nothing here belongs in the ledger until it is measured.

> **Corrections, 2026-09-15** (measured; see `zerops-auth-backbone-2026-09-15.md` and the fork's
> `verified.md` section of that date).
>
> - **There is a machine principal.** Pairing is gone, but an *integration token* with `BASIC_USER`
>   or higher on a Mate's project — including the container's own `ZCP_API_KEY`, and any org-wide
>   `BASIC_USER` token — passes `POST /api/auth/zerops-identity` and gets a session with
>   `orchestration:operate terminal:operate exec:operate`. §1's first heading and §2's "blocked on a
>   missing principal" are wrong; what is missing is a *policy* for machine principals.
> - **Attribution exists.** `apps/server/src/zerops/ZeropsAgentAuthorizers.ts` has recorded who
>   completed each agent login since 2026-09-05, and `authorizedBy` is on the agent-auth snapshot
>   (`packages/contracts/src/zerops.ts:325`). `agentOwnership.ts` holds the disclosure logic, but no
>   component in `apps/web` or `apps/mobile` consumes it yet, so nobody sees it. §4's "required input:
>   persist the login subject and add owner to the snapshot" is already done — the ownership gate
>   needs its enforcement and a UI.
> - **The relay is not deployed.** §1's "`infra/relay` … deployed as the `z3-relay` project" is wrong:
>   `verified.md:501` records that deploying it is a lasting resource awaiting the owner's go.

---

## 1. Architecture facts established

### FACTS — there is no machine principal, and its removal was deliberate

- The only door is `POST /api/auth/zerops-identity`: the client presents its own Zerops access
  token, the server proves membership with two reads against the platform API **using the caller's
  token, never the container's**, mints an ordinary pairing grant, and caps the session at
  `T3CODE_ZEROPS_MEMBERSHIP_TTL_SECONDS` (default 900 s). The client re-mints with the Zerops token
  it still holds, and *that* re-mint is the real membership check. — spec-mate.md §3.2, §3.3
- `account-lifecycle.md` (z3 `docs/internals/zerops/account-lifecycle.md`): "Manual browser-session/
  pairing HTTP endpoints and the `pair`/`auth` CLI groups are removed. Session cookies are not
  credentials for Mate HTTP or WebSocket access." Sessions live in the `zerops-user:<id>` subject
  namespace, "rejecting historical manually issued sessions". Only grants from the Zerops identity
  door are accepted in a Zerops environment.
- Consequence: a Slack bot, a GitHub App, a Discord command or a cron trigger holds no Zerops user
  token and therefore cannot hold a Mate session. **Every intake surface is blocked on the same
  missing thing, and it is a principal, not a connector.**

### FACTS — the transport is already there; only authorization is missing

- `/mate/` is an nginx `location` on the container's 8080 origin sitting **outside** the
  `__zcp_auth` gate. The full cookie-less chain (mint → `POST /z3/oauth/token` → websocket ticket →
  `GET /z3/ws` `101`) was measured from a laptop with no VPN. — `verified.md:227`
- `map.md` states the counterpart for the container generally: "SSH over VPN is the only way to run
  a command inside a container from outside. There is no API, webhook, or network-reachable MCP
  endpoint." That is true of *zcp*; the mate server's own `/mate/` origin is the exception, and it
  is publicly reachable.
- Any *new* route under `/mate/` is a **zcp template change**, not a mate change: `zcp init` is a
  `run.initCommands` boot step that re-renders nginx.conf from zcp's template on every container
  start, reverting anything patched live. — `map.md`

### FACTS — the relay is the only central component

- `infra/relay` is a Node service over direct Postgres, deployed as the `z3-relay` project. It
  keeps `mobile` (device + Live Activity registration), `link` (challenge + link), `token` (DPoP
  exchange), `server` (activity publish), health/metadata. — spec-mate.md §9.2
- Auth is the Zerops identity: a bearer verified at `/user/info`
  (`infra/relay/src/zerops/ZeropsAuth.ts`), principal is the Zerops user id, and an environment
  link proof must carry `zeropsProjectId` + `endpointOrigin`; the relay verifies the caller's
  membership of that project and that the origin belongs to one of its subdomain-enabled services.
  — spec-mate.md §9.2, invariant MK-2
- APNs delivery is already a durable job table: unique `job_id`, `SKIP LOCKED` lease, backoff,
  dead-letter at 5 attempts, lease recovery, 24 h expiry. — spec-mate.md §9.2, invariant MK-4
- Direction of flow today is **container → relay → APNs**. The relay has no path *into* a container.

### FACTS — the RPC surface has one chokepoint and a clean read/operate split

- Every agent-driving action goes through a single method: `orchestration.dispatchCommand`, mapped
  to `AuthOrchestrationOperateScope` — `apps/server/src/auth/RpcAuthorization.ts:25`. `thread.create`,
  `thread.start` etc. are `ClientOrchestrationCommand` variants dispatched through it
  (`packages/contracts/src/orchestration.ts:28`, `:832`), not separate RPCs.
- Every read path is its own method on `AuthOrchestrationReadScope`: `subscribeThread`
  (`RpcAuthorization.ts:32`), `getTurnDiff` (`:27`), `getFullThreadDiff`, `searchThreads`,
  `subscribeShell`, `getArchivedShellSnapshot`.
- Other doors that reach the same credential without passing `dispatchCommand`:
  `execRun` → `AuthExecOperateScope` (`:33`); `terminalOpen` (`:111`), `terminalWrite` (`:113`) and
  the rest of the terminal group → `AuthTerminalOperateScope`; `zeropsAgentLoginStart` (`:91`).
- `RPC_REQUIRED_SCOPES` is `satisfies Readonly<Record<WsRpcMethod, AuthEnvironmentScope>>`, so
  adding an RPC without choosing a scope is a type error, not a runtime failure.

### FACTS — the OAuth/token distinction already exists as a typed, tested bit

- `ZeropsAgentAuthState` (`packages/contracts/src/zerops.ts:235`) is a five-value matrix with
  `"authorized-token"` as a **first-class state separate from** `"authorized"`:

  | `flagOAuth` | `flagToken` | `credPresent` | state |
  |---|---|---|---|
  | false | false | false | `not-authorized` |
  | false | false | true | `local-only` |
  | true | false | true | `authorized` |
  | true | false | false | `reconnect` |
  | false | true | either | `authorized-token` |
  | true | true | false | `authorized-token` — token short-circuits before OAuth |

  Table-tested at `apps/server/src/zerops/ZeropsAgentAuth.test.ts:15-23`.
- `flagOAuth` = `ZCP_AGENT_OAUTH_<SUFFIX>` in the zembed env store, written by `zcp agent
  mark-oauth` **only after** mate verifies with the agent CLI's own status command
  (`claude auth status` / `codex login status`); presence of a credential file is explicitly not
  authentication. `flagToken` = `ZCP_AGENT_TOKEN_<SUFFIX>` present, any value. — spec-mate.md §8.1,
  `packages/contracts/src/zerops.ts:293,295`
- The snapshot streams to every member over `subscribeZeropsAgentAuth`, which is a **read** scope
  (`RpcAuthorization.ts:87`).
- Google Antigravity is **not** in the feed at all — it signs in through upstream's own flow. It
  therefore has no eligibility bit. — spec-mate.md §8.1

### FACTS — attribution does not exist

- `mark-oauth` records *that* an agent is authorized. Nothing records *who* authorized it.
- The login is server-driven through `zerops.agentLogin.start`
  (`packages/contracts/src/rpc.ts:288`) on an authenticated session, so the `zerops-user:<id>`
  subject is present in the handler and is currently not persisted.
- spec-mate.md §8 states as an accepted property: "The agent process and every project member share
  the container home: a credential file is [visible to all]".
- Role gating today is coarse and role-based only: OWNER, ADMIN and BASIC_USER may operate Mate;
  READ_ONLY and NO_ACCESS may not. Per-project `userRoles` override the organization role in either
  direction. — `account-lifecycle.md`

### FACTS — no integration code exists

- `grep -rn -i "webhook\|slack\|jira\|linear"` across `apps/server/src`, `infra/relay/src`,
  `packages/shared/src` returns only: a `mcp__linear__create_issue` string in
  `ClaudeAdapter.test.ts` permission-rule fixtures, an `"linear"` MCP server name in
  `ActivityPayloadProjection.test.ts`, CSS `linear-gradient`, and an unrelated `MTIME_SLACK_MS`
  constant in `UsageService.ts`. There is no connector, no webhook receiver, no outbound poster.
- The Claude adapter *does* already handle `mcp__<server>__<tool>` names and permission rules, and
  spec-mate.md §5.2 records the normalization (`mcp__<server>__<tool>` → `<tool>`).

---

## 2. Q1 — Devin integration parity

### DESIGN — the four buckets

| Devin surface | Side it lands on | Cost |
|---|---|---|
| MCP marketplace (Notion, Figma, Sentry, Postgres…) | mate server + web | Small to *consume* — adapter already handles `mcp__*`. Non-trivial for the *one-click* half: OAuth install flows and per-server secret storage |
| Reaching the running system (logs, metrics, DB) | **zcp**, not mate | Small, and free credit — the only question is whether `zerops_*` exposes logs/metrics/query as tools |
| Intake (Slack, Discord, Linear, Jira, issues, API, schedules) | **infra/relay + mate server** | The whole job |
| Enterprise (SCIM, service accounts, org defaults) | **Zerops platform**, not mate | Not built here; also not controlled here |

Two corrections to the positioning draft that prompted this:

- MCP is **not** "largely free". Consuming servers is free; Devin's marketplace value is the
  one-click OAuth install and secret management, which is real UI plus a secrets surface Mate does
  not have.
- Inheriting SCIM/service accounts from Zerops accounts is a genuine structural advantage, but in
  procurement it reads as roadmap risk, because it sits on another team's queue.

### DESIGN — where intake goes, and why

- **Not zcp.** zcp has two entry points, stdio MCP and CLI, and does not distinguish its caller
  (spec-mate.md §1). Wrong layer.
- **Not per-container.** Slack, Discord and GitHub Apps each post to one URL. Per-mate installs
  mean N installs, a fan-out you would build anyway, and webhook retries racing container restarts.
- **The relay.** It is the only component with all three properties intake needs: one stable public
  URL, Zerops identity verification with project binding, and a durable lease/backoff/dead-letter
  job table already in production shape.

### DESIGN — invert the direction

Have the **container dial the relay and pull its queue**, rather than building a way for the relay
to call into a container.

- The mate server already stamps a link proof carrying `zeropsProjectId` + `endpointOrigin` and
  already holds environment credentials and publish signatures (`EnvironmentCredentials`,
  `EnvironmentPublishSignatures`, spec-mate.md §9.2 / MK-3). It proves itself *to* the relay.
- No new inbound credential on the mate server, no `zerops-bot:` subject namespace, no second
  session model — which `account-lifecycle.md` explicitly guards against ("dormant native
  connection source cannot become a pairing escape hatch").
- It also avoids the nginx template change, since no new inbound route is needed.
- Second auth problem also dissolves: `thread.create`/`thread.start` need a session, but if the
  mate server drives its own dispatch in-process for a pulled item, there is no session to mint.
  Provider credentials live in the container, so the agent runs with no human present.
- `thread.settled` already exists as the completion event — the natural trigger for posting a reply
  back. Outbound HTTP from the container is unrestricted.

### DESIGN — rough sizing

- **Relay intake spine** — connector registry, correlation table (external thread ↔ mate thread ↔
  environment), routing rules, queue, container pull endpoint, mate-side puller, reply path. Bulk
  of the work; most of a quarter.
- **First connector** — Slack is the worst to start with (per-workspace OAuth install, Events API,
  thread semantics, `!agent`/`!channel`). GitHub App is the most mechanical. Linear/Jira are
  webhook-in / comment-out and get cheap once the spine exists.
- **API + schedules** — nearly free afterwards; schedules are cron rows in a table shaped like the
  one already running.
- **MCP install UX** — independent and parallelizable; mostly a secrets-storage question.

### DESIGN — the open design problem, not plumbing

Devin's intake lands in a fresh VM, so a new ticket never contends. Mate's lands in a long-lived
thread in an environment that may be mid-work, and the positioning explicitly says a mate works its
queue serially. So intake is a real queue with real backpressure: what happens when a ticket
arrives while the mate is deploying, whether an item may interrupt, whether two external threads
become two mate threads or two turns in one. This touches the thread model, not the edges, and is
cheap to get wrong and expensive to change after connectors exist. **Settle it before any connector
is written.**

---

## 3. Q2 — subscriptions and shared intake

### FACTS (external, not measured here)

Anthropic and OpenAI consumer terms scope a subscription to the individual and prohibit providing
access to third parties through the account. The API/commercial terms are what products are built
on. Treat the specifics as needing legal confirmation before anything ships; the shape of the
constraint is not in doubt.

### DESIGN — two risks, different weights

- **Whose prompts** is the hard line. Routing other people's work through one person's subscription
  is what those clauses exist to stop.
- **Unattended volume** is a gradient. The owner's own queued or scheduled work is still the
  owner's use — Claude Code is automation by design — but sustained bulk patterns are what trips
  detection.

Enforceable rule: **the credential owner must be the requester, or the credential must not be
personal.**

### DESIGN — the eligibility bit already exists

Queue eligibility is `state === "authorized-token"`. That value is already computed, already typed,
already streamed per-agent, already table-tested (§1). It covers API key, Bedrock and Vertex — every
commercial-terms credential — and excludes exactly the personal OAuth logins. This is reading a
bit, not building one.

Note an OAuth login remains "personal" regardless of plan tier: a Team or Enterprise seat is still
one person's seat, so the bit stays correct.

### DESIGN — two intake modes

- **Shared queue.** Requires `authorized-token`. Anyone in the channel or on the board enqueues.
  Billed to a key the org owns. This is the mode Discord, Slack, Linear and schedules run in.
- **Personal trigger (`@task`).** Allowed on `authorized`(OAuth). "User-triggered" must mean more
  than "a human typed it" — a human typing in Discord is still *a* human, not necessarily *the*
  human. Requires resolving the external identity to a Zerops user and checking equality with the
  attributed credential owner. That mapping table is needed regardless.

Refusal copy when a subscription mate is handed someone else's work: *this mate runs on your
personal Claude subscription, so only you can trigger it — add an API key to let the team use it.*
An honest constraint rather than a paywall, and the correct commercial nudge.

### DESIGN — consequence for pricing positioning

Qualifies the "no seats, bring your own subscription" pitch. Bring-your-own works for personal work;
shared intake requires a commercial credential from someone. Honest line: *bring your subscription
for your own work, bring a key for the team's.* Still far better than per-seat, and the draft's own
heavy-usage scenario already assumes API-key economics.

---

## 4. Q3 — credential ownership gate

Owner's decision this session: **only the person who authorized an agent should be able to operate
it; other members see it.**

### DESIGN — the rule

> An agent in OAuth mode is operated only by its attributed owner. An agent in token mode is
> operated by any member who passes the role gate.

One rule covers both Q2 and Q3: intake is simply a non-owner requester, so shared-queue eligibility
falls out of this rather than being a second mechanism.

### DESIGN — where the check goes

Inside the `dispatchCommand` handler: resolve the thread's agent; if that agent's state is
`authorized`(OAuth), require `session.subject === attributedOwner(agentId)`.

Not a new scope — scopes are per-method (`RPC_REQUIRED_SCOPES`), and this is per-command and
per-agent. The existing read/operate split already delivers "the rest just see it" with no new read
paths.

Eligibility is per **(mate, agent)**, not per mate: a project can run Claude on the owner's OAuth
and Codex on an org key, and those have opposite answers. A queued item must pin an agent.

### DESIGN — which commands

The line is *does this spend the credential*, not *is this a mutation*.

- **Owner-only** — sending a message, starting or resuming a turn, `/compact` (a real turn),
  realtime audio, and **`thread.approval.respond`**. The last is easy to miss: approving a pending
  tool call both continues the turn on the owner's subscription and authorizes an action in the
  owner's name.
- **Any member with operate** — `thread.archive`, `thread.pin`, `thread.snooze`, `thread.delete`,
  `thread.meta.update`, `thread.checkpoint.revert`. Housekeeping does not touch the credential.
- **`thread.interrupt` is the exception.** Stopping reduces spend, and someone must be able to halt
  a runaway while the owner is asleep. Let project OWNER/ADMIN interrupt anyone's agent — the one
  place role should beat ownership.

### DESIGN — required input

Persist the `zerops-user:<id>` subject from the `zerops.agentLogin.start` session (§1: it is in the
handler and currently dropped), and add an `owner` field to the agent-auth snapshot that already
streams on a read scope.

Two states must be designed, not one:

- **Unattributed** — the credential appeared from a terminal or SSH login with no mate session
  behind it. Fails closed: nobody operates it until someone claims it. This will be common early,
  so the claim flow cannot be an afterthought.
- **Orphaned** — the owner left the project or lost membership. The agent is still signed in and
  nobody can drive it. Correct, but must render as a visible state with "re-authorize" as the exit,
  not as a mate that mysteriously stopped working.

### DESIGN — what this is NOT

**This is a product behaviour and an accountability signal, not a security boundary.** Four other
doors reach the same credential:

1. `terminalOpen` + `terminalWrite` — open a terminal, type `claude`
2. `execRun` — argv `["claude", "-p", …]`; no shell needed, argv is enough
3. code-server at `location /` behind `__zcp_auth`, with its own terminal — not mate's RPC at all
4. SSH into the container over the VPN

The credential is a file in a home directory every project member shares, and spec-mate.md §8
already records that as accepted. Real enforcement needs per-member OS accounts or credential
isolation in the container — a zcp-level change, and a large one.

The defensible claim is the weak one: the product does not *offer* anyone else's credential, and
the high-volume path (intake) is genuinely closed. The ToS argument only ever needed that. Claiming
"nobody else can use Alice's subscription" is load-bearing in exactly the conversation where it
breaks.

### DESIGN — the product half

A dead composer reads as a bug. Per the client design rule that the face carries the state, this
should present as a fact about *the mate* — it is Alice's — not a permission error on an input.

The affordance that turns the dead end into something useful: a non-owner cannot run it, but can
**leave a request**. That is the same queue from §2 in personal-trigger mode — an item the mate
will not pick up until the owner says so. Same machinery, and it is what the non-owner actually
wanted.

Side benefit: `serverGetUsageSummary` currently reports the container's usage. With an owner it
reports *whose*, which is the number that matters when the bill is somebody's personal
subscription.

### DESIGN — cost

A slice, not a quarter: persist the login subject; add `owner` to the agent-auth contract that
already streams; add the predicate over a short command allowlist in the dispatch handler; build the
claim flow. Engineering is modest; the time goes into the read-only-mate UI shape and the
unattributed/orphaned edge cases.

---

## 5. Open questions

| # | Question | Blocks |
|---|---|---|
| 1 | Does `zerops_*` already expose logs / metrics / DB query as MCP tools, or is that zcp work? | the "reaching the running system" credit in §2 |
| 2 | Queue semantics when a mate is mid-turn: refuse, defer, or interrupt? | any connector work (§2) |
| 3 | Legal confirmation of the provider-terms reading in §3 | the shared/personal mode split |
| 4 | Does the relay's link table generalize to routing, or does intake need its own? | intake spine sizing |
| 5 | Claim flow for an unattributed credential — who may claim, and can it be contested? | §4 |
| 6 | Antigravity has no auth-feed bit; ineligible by default is safe, but is it acceptable? | §3, §4 |

## 6. Nothing here is ledger

No `verified.md` / `questions.md` / `hacks.md` / `map.md` / `poc-findings.md` row was written or
changed by this session. The FACTS above are citations to existing sources, not new measurements.
If any DESIGN item is built, its measured behaviour earns a ledger row then, and a decision earns a
spec-mate.md section.
