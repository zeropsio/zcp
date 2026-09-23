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

## Sketch
- Broker: a Mate whose project is confirmed gone (Zerops `projectNotFound`) is pruned from the
  registry by the broker itself, and its bot is deactivated with its tokens revoked; destructive
  actions above the cap are applied per group (or the cap is per group), so one dead group never
  blocks the others; the plan is logged (not just counts).
- Client: deleting a project from Mate removes its registry entries through the tag writer.
- A broker 502 is returned with CORS headers (today the browser reports a CORS error instead of the
  cause).

## Refs
- Record of the manual unblock (local, not committed): the mate repo's
  `.plans/mate-state-model/e2e/broker-cleanup.md`.
- gitea-mate `internal/mirror/plan.go` (main `849eab1`): the cap counts `remove_team_member`,
  `demote_site_admin`, `deactivate_person`, `delete_person_tokens`, `delete_bot_token`.
