# Mate registry entries outlive deleted projects and stop the Gitea broker for the whole org

**Surfaced**: 2026-09-23 — the Mate client state-model programme's live fixtures in the test org
KRLS: the org's Gitea broker had applied nothing since at least 01:13 UTC
(`the plan exceeds the destructive cap: 30 actions take something away, the cap is 10`), so a new
group got no Gitea org, repo, bot or runner.

**Why deferred**: the org was unblocked by hand (13 registry tags of 6 groups whose projects were all
gone removed from the Gitea tool project; nothing else changed). The fix spans three places — the
Mate client's project deletion, the broker's planner, and Zerops-side deletions the client never
sees — and needs its own design.

**Trigger to promote**: any org whose broker logs `rights loop stopped at the cap` or
`problems>0` with `applied=0`; any user report of "new group never gets its repository".

## What happens
- Deleting a project leaves its registry entry (`mate:gn:<group>` / `mate:gm:<group>:<projectId>:mate`
  tags on the Gitea tool project). The client's `deleteProject` is a bare `DELETE /project/{id}`;
  a project deleted in the Zerops UI cannot touch the registry at all.
- The broker keeps the Mate registered but cannot serve it ("its project has no zcp@ service yet"),
  so it stops minting for the bot — and every older token generation that bot accumulated falls due
  for revocation at once (`delete_bot_token`). A Mate whose container kept failing delivery had a new
  generation every pass. 7 dead Mates added up to 30 destructive actions.
- The destructive cap (10) then stops the WHOLE rights loop — every group of the org, including
  brand-new ones.
- Left behind after the manual unblock: the dead groups' Gitea orgs and repos, and 7 `mate-<id>` bots
  still ACTIVE with every token generation they hold. The broker reads bot tokens only for registered
  Mates, so it will never revoke them; deleting them needs Gitea site-admin write.

## Shipped (gitea-mate main `67c11ba`, 2026-09-24)
- The destructive cap holds one group, not the org: a group over it applies nothing and is reported
  by name; every other group's actions apply. One bot's superseded-generation cleanup is one unit.
- A registry entry whose project is missing from the org search and answers `projectNotFound` on two
  lookups at least one interval apart is excluded before planning, and its bot `mate-{id}` is retired
  (tokens deleted, login prohibited). Any other answer is not dead. Nothing is archived; no tag is
  written.
- A Mate's bot token is minted only after the plain variables were written, and deleted again on a
  definite refusal of the token write, so a refused delivery no longer adds a generation per pass.
  Older generations keep the full grace from when the held one replaced them.
- The person-token route answers 503 with CORS instead of a 502 (`3bc69ab`).

## Still open
- `ReadOrg` callers outside the rights loop (runner reconcile, `server/mate.go`, person-token rights)
  read the unfiltered registry, so a dead group's runner and environments are still served.
- Uniqueness is judged before liveness: a dead production plus a new one is a `registry.Parse`
  problem, and a dead group's slug stays taken.
- Bots whose registry tags were removed by hand (the 7 KRLS leftovers) are never swept.
- The broker's own token answering a deleted project is unmeasured; if it gets 403 instead of
  `projectNotFound`, nothing is detected (fail closed; the per-group cap still protects the org).
- Client: deleting a project from Mate does not touch its registry entries. Tags stay as tombstones by
  design; pruning `mate:gn:` frees the slug and orphans the group's bots, so it must never be pruned
  blindly.

## Refs
- Record of the manual unblock (local, not committed): the mate repo's
  `.plans/mate-state-model/e2e/broker-cleanup.md`.
- gitea-mate `internal/mirror/plan.go` (main `849eab1`): the cap counts `remove_team_member`,
  `demote_site_admin`, `deactivate_person`, `delete_person_tokens`, `delete_bot_token`.
