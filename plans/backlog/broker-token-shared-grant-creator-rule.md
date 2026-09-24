# The broker's shared Zerops token is edited by every creator, and only its creator or an OWNER may edit it

**Surfaced**: 2026-09-24 — Mates created by an org ADMIN (not the OWNER) never got Gitea wiring: zcp
reported `waiting for Gitea (GITEA_URL, MATE_BROKER_URL, GITEA_TOKEN not on this service yet)`, the
agent deployed directly, and no pull request ever opened. Diagnosed live the same day.

**Why deferred**: the owner chose a hotfix: `mate-broker` gets org-wide `BASIC_USER`, so it reaches
every project and nobody edits it per project any more. The proper design — who holds which key
for which project, and what the broker may write where — needs a rethink of the broker as a whole,
not a patch.

**Trigger to promote**: the broker redesign starts; any org where the hotfix is not acceptable
(the broker's token reaching projects that have nothing to do with Mate); any new place that has to
edit a token somebody else created.

## What happens

- Platform rule (measured 2026-09-24 with no-op `PUT /client/{id}/integration-token/{tokenId}` as an
  org ADMIN): a token can be edited only by its **creator** or an **OWNER**. The token's own role is
  irrelevant. An ADMIN editing a token another ADMIN created is unmeasured. The refusal is
  `403 ownerRoleRequired` "Only Owners can manage Owner or Administrator roles."
- The Zerops GUI cannot give a token `BASIC_USER` on a single project at all: per-project access
  toggles only between FULL (ADMIN) and READ ONLY, and only an OWNER may grant FULL.
- The design (D20) gives the broker one shared token, `mate-broker` (org `READ_ONLY` + `BASIC_USER`
  on the Gitea project), and has every creator add `BASIC_USER` on each new Mate, stage and
  production to it. `mate-broker` is created by whoever stood up Gitea, normally the OWNER, so
  everything an ADMIN creates is registered but the broker cannot write into it.
- On mate main the birth worker retries the grant for ~2 min and then shows the refusal once, as an
  in-memory line on the projects page. Nothing retries afterwards, so the Mate never converges.
- The broker's `deliver_mate_access` then fails with `creating GITEA_URL: zerops api: 403` on every
  pass. Before the gitea-mate fix of 2026-09-24 it also minted a new bot token generation on every
  failed pass, and a bot that was minting skipped revocation, so the pile grew without bound and the
  eventual cleanup tripped the destructive cap (see `mate-registry-outlives-deleted-projects.md`).

## Other places that assume the editor created the token

- `addGroupEnvironment`: an ADMIN's stage or production stops at the broker grant, before its deploy
  token and its `environments.yaml` declaration; the half-made reconcile re-runs it on every page read
  and hits 403 each time an ADMIN views.
- *Register in {group}* (`registerMateInGroup`): the same PUT.
- Group reach (`planAccountGroupReach`): PUTs every Mate's `zcp-*` token in a group, including tokens
  other people created; the first 403 skips every later write in the same pass.
- Throwaway sweep (`planThrowawaySweep`): deletes every stale `mate-door:*` / `gitea-signin:*` token
  of the org regardless of `createdByUser`, although its doc says "the person's own".
- Gitea setup `regenerate-broker-token`: an ADMIN regenerating the OWNER's `mate-broker` is unmeasured.
- The broker itself writes stage and production (recipe `ImportServices`, `EnableSubdomainAccess`)
  with `mate-broker`, not with the environment's D27 deploy token.

## The hotfix (shipped instead of this)

- `mate-broker` is minted with org `BASIC_USER`; a broker token that already has it needs no
  per-project grant, so creators never edit it. Older `READ_ONLY` broker tokens keep the per-project
  grant until an OWNER switches them (one GUI edit per org).
- Cost: the broker's token can write every project of the org, including ones unrelated to Mate, and
  an org-wide `BASIC_USER` token passes every Mate's door. The broker is internet-facing (webhooks,
  OIDC, `/deploy/grant`).

## Sketch (recommended redesign, from the 2026-09-24 review)

One rule: the broker writes into a project only with a key that project's creator minted, kept as a
secret variable on the broker's own service; `mate-broker` keeps org `READ_ONLY` + the Gitea project
and is never edited after Gitea is stood up.

- Build it from the D27 deploy-token code, not beside it: generalise the client's `ensureDeployToken`
  into one "ensure broker key (prefix, project)" with the same mint → write → delete-on-failure steps,
  and the broker's deploy-token lookup into one "key for project X".
- A separate variable family for Mate keys (e.g. `MATE_ACCESS_TOKEN_{HEX(projectId)}`), not the
  deploy family: the deploy grant resolves its project from `environments.yaml` with no check that it
  is that group's registered stage or production, so a shared family would let a merged
  `environments.yaml` hand a Mate's key to a job.
- The broker uses the per-project key for every write into a project (Mate variables, recipe imports,
  subdomain enable), so `grantBrokerProject` and every per-project grant path disappear.
- States derived from reads only: a registered project has a key on the broker or it has none; a
  missing key is a named problem the projects page shows with who can repair it.

Rejected: org `BASIC_USER` as the permanent answer (the hotfix's cost above); the creator's client
writing `GITEA_URL`/`MATE_BROKER_URL`/`GITEA_TOKEN` itself (the bot token would pass through the
browser and D20 removed exactly that on purpose); reusing the Mate's own `zcp-*` token (its value is
unreadable after birth, so existing Mates could never converge); one broker token per person (the
whole-list PUT again, and a leaver takes their Mates' reach with them).

## Measure before building

- Whether an ADMIN may write a secret variable on the `broker` service in a Gitea project an OWNER
  created (also unmeasured for today's D27 deploy keys).
- Whether the creator rule is about the creator's identity or the creator's role (an ADMIN editing
  another ADMIN's token).
- Whether an ADMIN may delete a token an OWNER created (throwaway sweep, Mate deletion).

## Safe independent fixes

- Group reach: plan PUTs only for tokens the viewer may edit, and run them independently.
- Throwaway sweep: only tokens `createdByUser === self`.
- A Mate registered but not reachable by the broker as a state derived from a read, not an in-memory
  line that a reload loses.
- zcp: the recipe bootstrap route skips the `giteaPairPlanError` gate; the "waiting for Gitea" notice
  is one easily skipped line.

**Refs**: D20 and §6.6 in `docs/spec-mate.md`; the rights-loop paragraph (~line 2012) says the broker
writes with its own token.
