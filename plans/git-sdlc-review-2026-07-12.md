# Git across the SDLC — conceptual review (2026-07-12)

Trigger: Michal Saloň's field report (2026-07-11) — per-deploy auto-commits he
doesn't want, repo-local identity persistently overwritten to `Zerops Agent`,
and closeMode=manual not stopping either. Karel asked for a full review of how
ZCP works with git across the whole SDLC.

Verification basis: three parallel code/spec sweeps + zcli source
(github.com/zeropsio/zcli @ v1.1.0) + local git-semantics probe + Codex
(GPT-5.6 sol) consultation. All file:line refs verified against `main` @
032130ef.

---

## 1. The complaint, verified

All three complaints CONFIRMED. One function produces all of them:
`internal/ops/deploy_ssh.go:228-286` (`buildSSHCommand`) — fires on every
container-mode `zerops_deploy` that isn't `strategy="git-push"`:

```
zcli login -- <token>
cd /var/www
(test -d .git || git init -q -b main)
git config user.email 'agent@zerops.io' && git config user.name 'Zerops Agent'   # ALWAYS, persistent
git add -A && (git diff-index --quiet HEAD 2>/dev/null || git commit -q -m 'deploy')  # commit iff dirty
zcli push --service-id <target> [--setup X] [-g]
```

- **(a) auto-commit**: every dirty-tree deploy mints `'deploy'` (literal, no
  message input exists). Introduced deliberately in 9a5c453a (2026-03-24).
- **(b) identity stomp**: persistent repo-local `git config` (wins over the
  user's global), re-asserted on EVERY deploy — a user fixing it gets
  re-clobbered on the next deploy. History: config was once inside the
  init-OR-guard (f7a22c01), deliberately reverted in 6720c923 ("B13"): a
  buildFromGit-cloned `.git` carries missing/foreign identity and the deploy
  commit failed with "unable to auto-detect email address". The fix chosen was
  unconditional overwrite; **set-if-absent was never considered** — and the
  open backlog entry `plans/backlog/m1-glc-safety-net-identity-reset.md`
  explicitly predicted this exact failure ("if the user customised
  user.name/user.email, ZCP would silently overwrite") with trigger "a live
  run shows wrong commit attribution that a user notices" — **the trigger has
  now fired**.
- **(c) closeMode orthogonal**: `CloseDeployMode` is read only by work-session
  auto-close derivation (`internal/workflow/work_session.go:508`) and delivery
  resolution (`internal/workflow/deploy_intent.go:264`) — never by the deploy
  transport. Michal's field conclusion "no clean mode for that today" is
  correct.

### Truth table (who commits / who stomps, today)

| Mode | Strategy | GitPushState | Tree | Auto-commit? | Identity overwritten? |
|---|---|---|---|---|---|
| Container | default | unconfigured (or pre-first-deploy) | dirty | YES, msg='deploy' | YES, every call |
| Container | default | unconfigured | clean | no (diff-index quiet) | YES (config still runs) |
| Container | default | configured + first deploy done | any | redirected to git-push (`deploy_repo_delivery.go:33`); breakGlass ⇒ row 1 | per breakGlass |
| Container | git-push | configured | dirty | refused (requires committed HEAD) | no |
| Local (Mac) | default | any | any | **no** (`zcli push --no-git`, zero git ops) | no |
| Local (Mac) | git-push | any | dirty | refused; user's own identity/creds | no |

closeMode never changes any cell.

---

## 2. Load-bearing discovery: the commit is not needed for delivery

zcli source (`src/archiveClient/handler_gitArchiver.go`): default
`--workspace-state all` snapshots a dirty tree WITHOUT committing — temp
`GIT_INDEX_FILE` + `git read-tree HEAD` + `git add -A` (temp index; includes
untracked) + `git stash create` (ephemeral commit objects, no ref moved, no
history) + `git archive <stash>`. Verified by local probe: HEAD unchanged,
untracked files included, tree stays dirty.

zcli's real prerequisites: a repo, an existing HEAD (≥1 commit ever), and a
*present* identity (`stash create` writes commit objects; auto-detect fails on
non-FQDN container hostnames — the true root of B13). Identity must EXIST, not
be ZCP's.

Nothing in ZCP reads the 'deploy' commit history — the only git-history reads
in the codebase are `rev-parse HEAD` existence/equality checks
(`launch_source_control_gate.go:426`, `launch_source_read.go:143`,
`deploy_git_push.go:225`). The commits' one systemic side effect: with `-g`
(self-deploy) the artifact ships a `.git` whose HEAD matches the deployed
tree.

---

## 3. The conceptual defect: one `.git`, two masters

The container `/var/www/.git` serves simultaneously as:

- **(A) ZCP's transport substrate + persistence** — spec'd as ZCP-owned
  infrastructure ("the container-side substrate zerops_deploy runs `git add -A
  && git commit` against", spec-workflows GLC intro; identity "persists for
  the service's lifetime", GLC-1). Robot commits + robot identity are fine
  here.
- **(B) the user's real project history** — the moment git-push-setup repoints
  origin to the user's GitHub, THE SAME repo becomes the ledger that launch
  gates verify, release tags decorate, and the user's team reads on GitHub.
  Human-curated commits + human identity are mandatory here.

(A) was designed first (GLC-1..6, 'deploy' snapshots, fixed identity); (B) was
layered on top (git-push delivery, L1 ladder, launch gates) without ever
evicting (A)'s writes from (B)'s ledger. Every path that ships history to the
user's GitHub ships the robot noise:

- L0→L1 transition: history full of 'deploy' commits gets pushed on first
  git-push.
- Agent-made real commits inherit the stomped config ⇒ committer `Zerops
  Agent` on GitHub (Michal's amend-loop).
- Sign-off surgery (`git reset --soft` + one real commit) becomes the
  user-side workaround — institutionalized in their recipe.

The parts of the system built AFTER the philosophy shift already embody the
right contract — and prove the codebase knows it:

- git-push strategy (container+local): "transmits the committed HEAD only; it
  never stages or commits for you" (`deploy_git_push.go:246-255`); an earlier
  auto-commit fallback was REMOVED because it "masqueraded 'agent forgot to
  commit' as successful pushes of empty state".
- Local mode: `--no-git`, GLC-6 "identity, branch, .gitignore conventions are
  personal".
- Delivery ladder rationale (`deploy_intent.go:256`): the never-pushed
  'deploy' commit class was recognized as a "bug factory" on 2026-06-10 — but
  the remedy chosen (redirect direct→git-push) treated delivery, not the
  minting.

## 4. Full SDLC map (where git enters, and each site's verdict)

| Phase | Git activity | Verdict |
|---|---|---|
| Bootstrap/adopt | `InitServiceGit`: git init + identity write, once per service (`service_git_init.go:24`) | OK shape; identity write should be set-if-absent |
| Dev iteration (container, direct) | per-deploy config-stomp + add -A + commit 'deploy' + zcli push (`deploy_ssh.go:228`) | **the defect** — commit unnecessary, stomp harmful |
| Dev iteration (local, direct) | `zcli push --no-git`, zero git ops (`deploy_local.go:180`) | clean, the reference behavior |
| git-push delivery (both envs) | push committed HEAD only; refuses uncommitted; dirty warn (`deploy_git_push.go`, `deploy_local_git.go`) | clean |
| git-push-setup | dry-run probe first; origin add/set-url; persistent URL-scoped credential helper; reconstruct (one mixed `git reset`, tree-safe) (`workflow_git_push_setup.go`, `git_credential.go:78`) | clean; reconstruct writes identity → set-if-absent |
| Close / auto-close | no git codepath; guidance-only; manual = done-ness ownership | OK; misleads users into expecting it gates commits |
| launch-production | read-only gates: clean tree + HEAD==remote (hard block); agent commits per instruction text | clean mechanically; guidance says `git add -A && git commit` with stomped identity ⇒ robot committer on prod history |
| Release | annotated tag at verified HEAD, pushed via session-env helper (`git_credential.go:143`) | clean |
| Export | instructions-only ("export bundle" text) | clean |
| Authoring (maintainer, gated) | `PushAppSource` auto-commits maintainer's dir "recipe: <slug>" (`publish_recipe.go:254`); sync = pure `gh api` | acceptable (maintainer-only), noted |
| Credentials | GIT_TOKEN session-env inline helper, per-invocation `-c`, no argv/disk residue | clean, keep |

Doc drift found in passing: spec-workflows GLC section — the prose bullet
"Deploy time — happy path (GLC-2)" still describes the pre-B13 shape (init+
config skipped when `.git` exists) while the GLC-2 invariant row correctly
describes always-run config. Internal spec inconsistency; fix with whichever
direction ships.

---

## 5. Design directions (assessed)

**Final state (2026-07-12):** P1+P2 shipped as `814a37e1`, P4 as `4ed32bbe`
(both on main; implementation plan `plans/git-contract-2026-07-12.md`,
each phase Codex-reviewed with findings folded). P3 remains an OPEN product
decision — deliberately unshipped. Latent bugs surfaced by the review live
in `plans/backlog/ci-selfdeploy-git-config-wipe.md` and
`plans/backlog/l0-to-l1-history-offering.md`.

### P1 — identity: set-if-absent, never stomp  (fixes complaint b)
`git config user.email >/dev/null || git config user.email 'agent@zerops.io'`
(same for name) at `InitServiceGit`, `buildSSHCommand`, and
`BuildGitReconstructCommand`. Solves B13's actual requirement (identity must
EXIST for zcli's `stash create` / any commit) while preserving user-set
identity forever. Backward compat: existing stomped configs stay until the
user changes them — but ZCP stops re-clobbering, so a user fix finally
sticks. Closes backlog entry `m1-glc-safety-net-identity-reset`.

Codex nuance (accepted): **P1 alone is incomplete** — while ZCP still mints
real commits, set-if-absent would attribute robot 'deploy' commits to
whatever foreign identity a buildFromGit clone happened to carry. P1 is
stop-the-bleeding only; it pairs with P2 in the same ship.

Migration for already-stomped repos (Codex): set-if-absent means ZCP stops
reasserting, so a one-time user fix sticks — minimum viable. Proactive
variant: wherever ZCP touches identity, treat config that EXACTLY equals the
robot identity (`Zerops Agent <agent@zerops.io>`) as ZCP-written and eligible
for replacement — e.g. git-push-setup swaps it for the derived GitHub
identity (P4). Never touch any other value.

### P2 — drop the per-deploy auto-commit  (fixes complaint a)
Remove `git add -A && git commit 'deploy'` from `buildSSHCommand`; let zcli's
native `workspace-state=all` snapshot the dirty tree ephemerally. ZCP
guarantees only the three real prerequisites: repo exists (init guard stays),
HEAD exists (one bootstrap commit at `InitServiceGit` — new: empty
`zcp: initial commit` via `-c` ephemeral identity — plus the same guard in
the deploy safety-net for migrated services), identity exists (P1).
- Self-deploy `-g` still ships `.git`; the redeployed container honestly shows
  the same dirty state (nothing committed ⇒ nothing pretends to be).
- No ZCP consumer of the 'deploy' history exists (verified §2).
- The L1 "repo trails container" hazard is unchanged — L1 delivery is git-push
  (committed HEAD) and the launch gate hard-blocks dirty/unpushed regardless.
- Persistence caveat: on L0, uncommitted work now lives ONLY in the container
  working tree + latest deploy artifact (tree content survives `deployFiles
  [.]` round-trips; it just isn't in git history). Same durability as today's
  local mode.
- Hidden checkpoint refs (`refs/zcp/deploys/<ts>` via `commit-tree`) —
  REJECTED for first ship (Codex concurs): preserves a forensic feature
  nobody asked for, adds retention/GC questions, and keeps ZCP writing git
  objects in a workflow whose core complaint is git side effects.
- Bootstrap-HEAD tradeoff (explicit, per Codex): zcli's archiver needs an
  existing HEAD (`git read-tree HEAD`); today the repeated 'deploy' commits
  satisfy it accidentally. P2 replaces them with ONE empty `zcp init` commit
  (per-invocation `-c` robot identity) at `InitServiceGit` + the deploy
  safety-net for migrated services. That single commit can still end up in
  user history when the repo began life inside the container — one marker
  commit vs. N 'deploy' commits; acceptable, documented.
- zcli assumption guard (Codex): P2 leans on zcli's temp-index/stash archive
  path (`workspace-state=all`, present since ≥v1.0.61 per GLC-6 verification,
  current v1.1.0). Define a minimum supported zcli version or probe once per
  container — never silently rely on it for old containers.

### P3 — iterate-vs-deliver cadence under L1  (Michal's "5 deploys, 1 commit")
Under L1 the intended inner loop is edit + dev-server verify (no deploys at
all); push delivers to the build target when the user is ready. Michal's
friction pattern (deploy per fix) is an L0/simple-mode pattern. With P2, L0
iteration mints zero commits, and the sign-off commit is the user's single
curated one — his desired flow becomes the DEFAULT L0 behavior. Whether L1
should additionally allow direct dev-half iteration without per-iteration
push (redirect relaxation mid-session, reconciled at close/launch by the
existing gates) is a real product decision — deferred; the redirect already
carries breakGlass + divergence warning for the escape hatch.

### P4 — authorship of real (GitHub-bound) commits
At git-push-setup time, derive the human identity from the PAT (`GET /user` —
no fine-grained permissions needed) and seed repo-local config. Codex
constraints (accepted): if email is private/null use GitHub's documented
noreply pattern `ID+USERNAME@users.noreply.github.com`; seed ONLY when
identity is absent or exactly equals the old robot identity; never overwrite
a custom value. Agent guidance ("git add -A && git commit") then attributes
to the account that owns the remote. Ships after P1/P2; carries the
stomped-repo migration from P1.

---

## 6. Codex consultation (GPT-5.6 sol, 2026-07-12)

Verdict: framing confirmed — "the bug is not 'Git is used', it is that
container direct deploy uses the user's project `.git` as a packaging
substrate and then mutates the same ledger that later becomes GitHub
history." B13 fix was "too broad — 'identity missing' and 'identity present
but not ours' are different states; ZCP only needed to fill absence."

Greenfield contract (adopted as the target semantics):
1. Repo history is user-owned; ZCP never moves HEAD during direct deploy.
2. Direct deploy is an ARTIFACT operation — may use git as an implementation
   detail, must not create user-visible commits.
3. Persistent repo-local identity is user-owned; ZCP sets missing values
   only; ZCP-internal commits use per-invocation `git -c`, never persistent
   config.
4. git-push stays strict (committed HEAD only) — unchanged.
5. Launch/release/export stay read-only source-control gates — unchanged.
6. `GitPushState=configured` should mean "repo delivery available/preferred",
   not "every dev-loop preview becomes a GitHub-bound commit" — but ship the
   ladder change SEPARATELY from the bug fix.

Ranked ship order: P1 (+ stomped-repo handling) → P2 (+ zcli guard) → P4 →
P3 last, unbundled. Risks flagged: submodule/LFS archive semantics need
explicit verification (git archive ships pointers, likely not a regression
but untested); bootstrap-HEAD commit tradeoff must be explicit; additional
spec drift — some launch/workflow wording still implies git-push may
"commit + push" while the implementation correctly refuses.

---

## 7a. L1 / CI-CD perspective (completed 2026-07-12, second pass)

Karel's follow-up: "how does it work for git-push delivery and full CI/CD?"
Two agent sweeps (L1 flow trace + blast-radius inventory) answered:

**Clean under P1/P2/P4 with zero code changes:**
- Emitted GitHub Actions workflows (dev-loop + prodCD): `actions/checkout@v4` +
  `zcli push` from a clean checkout at HEAD — no identity needed on the
  runner, no dependency on 'deploy' commits (`workflow_build_integration.go:593`,
  `workflow_launch_production.go:2047`).
- Webhook integration: zero ZCP-side git ops (server-side buildFromGit clone).
- Release tag + export commit + flatten commit carry NO inline identity — they
  inherit ambient repo config. Today that means **every release tag on GitHub
  is tagger-attributed to Zerops Agent**; after P1+P4 they attribute to the
  human automatically (no change to `workflow_release.go`/`workflow_export.go`).

**Findings that extend the fix:**
- `BuildGitOriginSyncCommand` (`git_auth_probe.go:79`) can `git init` a repo
  (GAP4-1 guard) but writes NO identity → fresh git-push flow without a prior
  deploy can hit commit-without-identity. P1 gains a 4th site.
- D2a ordering: the first-ever deploy is ALWAYS direct even when git-push-setup
  ran first — the bootstrap-HEAD guarantee must ship atomically with the
  auto-commit removal or `committedCodeCheckCmd` strands a fresh pair.
- The empty `zcp init` bootstrap commit satisfies `committedCodeCheckCmd`
  (`internal/tools/deploy_git_push.go:222` — tools, not ops; same basename
  exists in both packages) and the write-probe's empty-repo path (HEAD-unborn
  fallback becomes legacy-only).
- Launch gate `dev-tree-dirty` fires MORE often under P2 (trees stay dirty
  between iterations) — this is CORRECT: the gate becomes the explicit
  sign-off-commit enforcement that the invisible auto-commit used to fake.
  Recovery text is already accurate. Verify agent behavior via flow-eval
  after ship.
- No GitHub API call site exists in the repo — P4's `GET /user` is net-new.

**Latent pre-existing bugs surfaced (→ backlog, not this pass):**
- CI self-target `-g` ships the RUNNER's fresh checkout `.git` → silently
  wipes the container's credential helper (+identity; identity self-heals,
  helper does not). `gitPushConfiguredRecall` checks only `.git` existence →
  reports "already-configured" post-wipe.
- Launch push-proof `BuildGitAuthedLsRemoteCommand` has `|| true` → a broken
  credential helper reads as empty SHA → misclassified as `head-not-pushed`
  instead of broken credential wiring.
- L0→L1 of an existing pair pushes the FULL robot-'deploy' history to the
  user's GitHub; no clean-slate offering exists (product gap; P2 stops the
  minting going forward, old histories stay).

## 7. Verification obligations before any ship

- Live-verify on eval-zcp: container deploy with dirty tree and NO ZCP commit
  (P2 shape) — zcli `workspace-state=all` path end-to-end incl. `-g`
  self-deploy artifact round-trip and a buildFromGit-provisioned service
  (B13 regression class: non-FQDN hostname, no identity → set-if-absent must
  fire before zcli's `stash create`).
- Submodules + LFS through zcli's stash-archive path (Codex risk): expect
  pointer semantics, prove it's not a regression vs. today's commit path.
- zcli minimum-version decision (pin vs. probe) before removing the commit.
- `TestBuildSSHCommand_Shape` + `deploy_git_test.go` ("split init/config and
  always-commit") pin the CURRENT shape — they change WITH the fix (RED
  first).
- GLC-1/2/3 spec rows + the stale happy-path prose + the launch/workflow
  "commit + push" wording drift, all in the same commit as the code change.
- Atoms/guidance sweep: sites teaching `git add -A && git commit -m '<msg>'`
  stay valid (git-push contract unchanged), but any text claiming ZCP commits
  on deploy must flip.
