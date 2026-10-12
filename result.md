# spec-landing result

Docs-only Change in the prepared worktree `/Users/macbook/Documents/Zerops-MCP/zcp-wt/spec-landing`.
Root cause: §4.8 and its cross-references retained composer-prefill behaviour after Mate deleted its owners.
Invariant: landing composes no opening message for the person. Owning correction: `docs/spec-mate.md`.
No new mechanisms, abstractions or runtime changes.

## Evidence

Mate local main: `5b2941163a5addefee2eef22cf200fc330f96293`; fetched origin/main: `5b2941163a5addefee2eef22cf200fc330f96293`. The affected landing, hook, metadata and bootstrap files have no diff between these heads.
Removal: `b43248a3ac45e9a0a42dbff16e2fc92a76a742c3` (`fix(zerops): an empty conversation opens on an empty composer`). Its diff deletes `firstPrompt.ts`, `composeFirstPrompt.ts`, `firstPromptStorage.ts`, `creationHandoff.ts`, and `useZeropsCreationJob.ts`, and removes their callers from the landing route, Sidebar, ChatView and new-thread hook.
`git log main -S 'I just opened Zerops Mate on this project.'` and `-S 'zerops-mate.first-prompt.v1'` identify that removal. The pre-removal firstPrompt owner keys its composed marker on environmentId; both the prompt and marker are absent today.
Parked zcp commit `c6e90574b` was read for intent only; no cherry-pick.

Each §4.8 claim checked against current Mate code:

- Workspace project, conversations owned by engine: `apps/server/src/serverRuntimeStartup.ts:140–169`, `resolveAutoBootstrapProject`, dispatches only project.create; `serverRuntimeStartup.test.ts:150` pins no bootstrap thread.
- Main chat or draft: `apps/web/src/routes/_chat.index.tsx:56–71,238–267`; `apps/web/src/zerops/useOpenMate.ts:130–152` and `hooks/useHandleNewThread.ts:128–166` are the real consumers.
- Empty composer, no automatic opening message on connect/create/reconnect: the removal diff, current route/hook consumers, and `apps/web/src/hooks/useHandleNewThread.test.tsx:114–141`, which checks the real composer store prompt equals the empty string.
- Existing user words preserved: `useHandleNewThread.ts:149–157` carries user content only when explicitly requested and the destination is empty; normal landing only navigates.
- Hosted device label versus self-hosted web label: `apps/web/src/connection/clientMetadata.ts:89–99`; `clientMetadata.test.ts:100–123` checks exact labels. `git log -S 'Zerops Mate ·'` identifies branding commit `b85dc4ca97`; the label was not part of the prefill removal.

Other corrections: MC-8 states current landing; D17 retains its **No brief.** title and loses the stale generated hand-off; MB-25 loses only the hand-off claim and deleted test citation; MB-27 loses the hand-off from birth. All other composer references were checked and concern current features, not onboarding.

## Before §4.8

### 4.8 Landing in the thread

The mate server's own auto-bootstrap (§2.2) creates the first project and thread; the client composes
one fixed onboarding prompt into the composer after `connectZeropsIdentity` returns an environment
— filling it, never sending it, so the user reads it before it costs them a turn — guarded by a
marker keyed on `environmentId`, only for an identity-door environment, never a manually paired
one; a reconnect stays silent. The client label reads `Zerops Mate · <browser> on <os>` so two
devices on one container are told apart.
`firstPrompt.test.ts` — "composes once for a freshly connected Zerops environment", "stays quiet on
every reconnect to the same environment", "never writes into an environment somebody paired by
hand"; `clientMetadata.test.ts` — "names the device, so two clients on one container are told
apart".

One opening message is sent rather than composed. An environment this client created — the
new-project wizard, or a new environment in a group — carries a **creation hand-off**
(`creationHandoff.ts`): what the environment is, where its application came from, and the one job
left. Its birth keeps it until the connect names the environment, and the environment keeps it
until the job is said. It takes the fixed prompt's place in the composer, and `useZeropsCreationJob` sends it once a
coding agent is signed in and the thread can take it, retrying for up to 90 s. The hand-off is spent
only when the send lands, so a reconnect, a second tab or a later visit says nothing, and a send that
never lands leaves the job in the composer for the person to send.
`creationHandoff.test.ts` — "says the job once everything it needs is there", "says nothing while no
agent is signed in", "waits rather than gives up: the same input answers again once it is ready",
"refuses an empty composer rather than reporting an empty send as sent".

Closed 2026-09-16 (D17, mate 0.11.2): the form asks no question, so there are no words of the
person's to send; the generated hand-off is composed into the composer and left for them
(`resolveZeropsCreationJob` answers `compose` for a hand-off with no brief). Nothing is sent by
itself.


## After §4.8

### 4.8 Landing in the thread

The mate server's own auto-bootstrap (§2.2) creates the workspace project; conversations belong to
its engine. Landing opens the Mate's main chat when it has one, otherwise a draft in that project.
A conversation nobody has spoken into starts with an **empty composer**: the client composes no
opening message and sends nothing on the person's behalf when an environment is connected,
created or reconnected. Words the person has already entered stay theirs.
`useHandleNewThread.test.tsx` — "opens a Mate's conversation nobody has spoken into on an empty
composer".

The hosted client label reads `Zerops Mate · <browser> on <os>` so two devices on one container are
told apart; the self-hosted web client reads `Zerops Mate Web`.
`clientMetadata.test.ts` — "names the device, so two clients on one container are told apart",
"keeps the hosted/self-hosted split under the Zerops Mate brand".


## Checks and shipping

`git diff --check`: passed. Retired prompt/marker/hand-off reference scan: passed.
No docs-specific check exists in Makefile, hooks or CI. `make lint` is Go-only; no runtime tests required for this docs-only change. Mandatory pre-push Go lint passed: `0 issues.` (receipt: `/tmp/spec-landing-push.log`).
No runtime tests were removed or changed. No live container edits.
Shipping: commit `b6732bdf2` pushed successfully to zcp `origin/main`; receipt `c35eee79f..b6732bdf2 HEAD -> main`. Exact trailer: `Card: spec-landing`. The specification push completed; this report is included in the final docs-only delivery. No CI watch or release.
PATH workaround: pre-push requires golangci-lint; supplied `/Users/macbook/Documents/Zerops-MCP/zcp/bin` on PATH without changing or disabling hooks.
untested: runtime suites, live account, owner's screen and timing (docs-only; claims checked against current code and existing test assertions).
