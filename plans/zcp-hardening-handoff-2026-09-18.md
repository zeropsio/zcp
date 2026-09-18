# zcp hardening — the hand-off

2026-09-18 · For whoever hardens zcp for Zerops Mate next. The backbone runs end to end, and this
lists what zcp still does wrong or leaves to chance there. Every item was seen on a live run of the
test org `Mate`; the facts, with times and commits, are in the fork's ledger
(`z3/docs/internals/zerops/verified.md`, the sections _D27 and D28 proven live, twice_ and _The last
tests before the hand-off to zcp hardening_). The fork's `docs/internals/zerops/primer.md` says where
the whole stands. The decisions are in `docs/spec-mate.md` §10 (MB-26 is the delivery).

## Where it stands

zcp v9.180.2 is the latest release, and all three Mates of the test org run it: Todo's Vera and
Fen, and Notes' Iris. With it, a wired pair's stage deploy commits, fetches, merges the base in,
pushes to `mate/{bot}` and opens or updates the pull request. The request is titled after the task
and merged by squash from the Mate's conversation. A push to `main` deploys the stage, and a tag
deploys production. On the last run each of these steps took 58–73 s from click to serving, rollback
included. Nothing below blocks that path; each item makes it cheaper, safer or less confusing.

## The items, most consequential first

1. **Running Mates keep their zcp until a restart.** zcp looks for a newer build once, when its
   server starts (`update.Once`, `cmd/zcp/main.go`), from an answer cached for 24 hours
   (`internal/update/check.go`). It replaces the binary on disk for the _next_ start, and the
   running process never switches. So a release reaches a running Mate only through a restart,
   and the check that would notice it can be a day stale. The test Mates got v9.180.1 and v9.180.2
   because they were restarted by hand (`PUT /service-stack/{zcp}/restart`). v9.179.3's boot
   refresh covers the Mate server's manifest, not zcp itself. Every fix in this list waits for a
   restart nobody asked for before it reaches a Mate in the field. Done when a running Mate takes a newer zcp within the hour at a point where no session is
   open, or says it has one waiting.

2. **The Mate's branch takes `main` only when it delivers.** The owner asked that merging "put its
   git on main where it was merged". The app cannot do it. It has no hold on the Mate's checkout,
   and a pull there follows the branch's own upstream, `mate/{bot}`, not `main`. The app's attempt
   never ran and is removed. Today the branch catches up inside the delivery
   (`BuildGiteaDeliveryCommand` merges `origin/<base>` before it pushes), so the next pull request
   carries only the new work. Iris's branch shows it: `afed1e5` is the task, then
   `f4b4f3b "Merge remote-tracking branch 'origin/main' …"` as the bot. But between a merge and the
   next delivery the agent edits a tree without the work other Mates have landed, and a collision
   first shows at delivery, as `ZCP_MERGE_CONFLICT:`. The agents make up for it by hand. Fen now
   merges `main` before it edits ("main had moved twice since our last landing … so I merged
   origin/main in again"). Vera rebuilt its branch on `main` and rewrote the remote branch (`b911db6`
   directly on #8's squash, its earlier `d32492f` gone), committing as "Todo App" instead of the bot.
   Done when a
   develop session starts by taking the base in: the same fetch and merge, the same conflict
   marker, so the agent builds on the current `main` and the hand merge has no reason to exist.

3. **A wired pair's stage deploy is recorded dirty by construction.** In `deploy_ssh.go`,
   `attempt.Dirty` comes from the tree the deploy takes, and `deliverGiteaPair` commits that tree
   afterwards. So the first stage deploy of any new work is "dirty". Iris told the person: "the
   deploy to stage was marked dirty … worth a clean re-deploy later if you want the stage build to
   be exactly reproducible". Done when the delivery commits before the build, so a stage deploy is
   always of a commit, or when a tree the delivery commits in the same call is not reported dirty.

4. **One change, three deploys.** Asked for one rename, Fen ran a git-push deploy of `appdev`, which
   pushed `mate/{bot}` and opened #8. It then ran a direct deploy of `appdev` and of `appstage`, and
   the stage's delivery pushed again. A wired pair now has two delivery paths that land the same
   thing, and the agent takes both. Done when one path is the delivery and the other either
   delegates to it or is not offered on a wired pair.

5. **The recipe is not proposed again when the setups change.** Spec 2.2 says the group recipe is
   "kept current". After an expansion the pair's Gitea record is kept (v9.178.0), but a change to
   `zerops.yaml`'s setups does not update the recipe's pull request, and a recipe composed before
   v9.179.1 still names the dev half's setup for stage and production. This is the primer's open 22.

6. **A wired Mate is still asked the service mode** (dev/stage pair, dev only, simple), though the
   pair is the only answer a wired Mate accepts. A Mate that adopted the recipe's services also
   suggests `launch-production`, zcp's path for projects without Gitea. This is the primer's open 25.

7. **The workflow file moves only at a delivery.** Since v9.180.1 a delivery replaces a pre-D27
   `.gitea/workflows/zerops.yml` and keeps its Test step. A repository whose Mate never delivers
   again keeps a workflow that deploys nothing. This is the primer's open 31.

8. **What a person reads about zcp's tools is the agent's guess.** In the conversation: Vera, on
   v9.180.1, "git-push delivery keeps failing because the tool tries to push straight to the
   protected main branch — I work around it by pushing to the Mate's own branch directly and
   deploying via the direct path" (fixed in v9.180.2, but the thread remembers it); an earlier
   "created:false, already exists"; Iris's "dirty" above; Fen's hand merges. Each is a tool result
   the agent paraphrased to the person. Done when every answer that reaches a person states what
   happened in the product's words (branch, request, stage, release), with no internal
   flag for the agent to guess about.

9. **The pull request's title carries the task's instructions.** Todo #8 is "Rename the app to
   "Team Todo" in the page title and main heading, then deploy.", because the session's first line
   includes the person's "then deploy". Notes #2, from a task where that sentence stood on its own,
   came out clean. Done when the title is the task without the deploy instruction.

## Not zcp's

These came up on the same runs and are handled elsewhere.

- The app: the Mate's merge banner stayed after its merge, and the pull after a merge never ran.
  Both are fixed on the fork's `main`.
- The app: _Roll back to this_ moves under the pointer when a new release row appears. This is
  open in the primer.
- The app: "Zerops request result is uncertain" now keeps the platform's words for a `400`.
- The broker: the deploy token is long-lived, because only a person can mint one. This is the
  primer's open 31.
