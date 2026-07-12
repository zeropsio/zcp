# Git contract fix — P1 set-if-absent identity, P2 no per-deploy commit, P4 human attribution

Analysis: `plans/git-sdlc-review-2026-07-12.md` (read it first — full mechanism
facts, SDLC map, Codex consultation, L1/CICD sweep). This plan is the binding
implementation scope. Orchestrated: each phase implemented by a Sonnet-5
subagent, reviewed by Codex, gates verified by the orchestrator.

## Target contract (from the review, Codex-confirmed)

1. Repo history is user-owned. ZCP never moves HEAD during direct deploy.
2. Direct deploy is an ARTIFACT operation — git is an implementation detail,
   never a user-visible commit.
3. Persistent repo-local identity is user-owned. ZCP fills MISSING values
   only; ZCP-internal commits use per-invocation `-c`, never persistent
   config.
4. git-push stays strict (committed HEAD only) — unchanged.
5. Launch/release/export stay read-only gates — unchanged.

**Out of scope (explicitly):** P3 delivery-ladder change (Karel's separate
product decision); the two latent CI/credential bugs (backlogged in F4); the
L0→L1 clean-history offering (backlogged in F4).

## Mechanism facts the implementer must not re-derive

- `zcli push` default `--workspace-state all` snapshots dirty trees WITHOUT a
  commit: temp `GIT_INDEX_FILE` + `git read-tree HEAD` + `git add -A` (incl.
  untracked) + `git stash create` + `git archive` (zcli
  `src/archiveClient/handler_gitArchiver.go`). Requires: repo, reachable HEAD,
  present identity (any). Minimum zcli: v1.0.61 (GLC-6 verified the archiver).
- B13's root: git identity auto-detect fails on non-FQDN container hostnames.
  The requirement is identity EXISTS — not that it is ZCP's.
- Nothing in ZCP reads 'deploy' commit history; only `rev-parse HEAD`
  existence/equality checks.
- The empty bootstrap commit satisfies `committedCodeCheckCmd`
  (`deploy_git_push.go:222` — any reachable HEAD) and
  `BuildGitWritePushProbeCommand`'s dry-run path.

---

## F1 — set-if-absent identity (P1)

**Sites (4):**
1. `internal/ops/deploy_ssh.go:258-261` `buildSSHCommand` gitConfig →
   per-key set-if-absent, stays OUTSIDE the init-OR (self-heal for migrated
   services + B13 class). VERIFIED fragment shape (locally probed
   2026-07-12; both properties are load-bearing):
   `(test -n "$(git config user.email)" || git config user.email 'agent@zerops.io') && (test -n "$(git config user.name)" || git config user.name 'Zerops Agent')`
   - MUST be parenthesized: an ungrouped `a && b || c` fires `c` when `a`
     fails (equal precedence, left-assoc) — same grouping style as the
     existing `(test -d .git || git init -q -b main)` guard.
   - MUST probe VALUE non-emptiness, not exit code: `git config user.email`
     on a key set to EMPTY string exits 0 (probe would skip) but a later
     commit dies on "empty ident not allowed". `test -n "$(...)"` covers
     unset AND empty.
   (identity values from `DeployGitIdentity` via shellQuote as today).
2. `internal/ops/service_git_init.go:42-45` `InitServiceGit` — same shape.
3. `internal/ops/git_credential.go:78-88` `BuildGitReconstructCommand` —
   same shape (inside its `test ! -d .git` guard a fresh init has no identity,
   so semantics are identical — change for single-shape consistency; F3 makes
   the fill derivable).
4. `internal/ops/git_auth_probe.go:79` `BuildGitOriginSyncCommand` — ADD
   set-if-absent identity after its GAP4-1 init guard (today it can init a
   repo with NO identity → agent's first commit in the git-push flow fails).

Single owner: one Go helper composing the set-if-absent fragment from
`DeployGitIdentity` (e.g. `gitIdentityEnsureFragment()` in ops), consumed by
all 4 sites — the tell and the check can't drift.

**RED first — tests to rewrite (from blast-radius inventory):**
- `internal/ops/deploy_ssh_test.go`: `TestBuildSSHCommand_Shape` (new
  fragment shape; keep the config-not-nested-in-OR forbidden assertion),
  `TestBuildSSHCommand_FreshInitPath` (behavioral, real git: fresh repo gets
  robot identity) + NEW behavioral case: pre-set identity SURVIVES the
  command (the P1 point).
- `internal/ops/deploy_git_test.go`: `TestBuildSSHCommand_GitGuard` table
  (fragment strings), keep "identity ignores auth.Info" case (constant stays
  the source).
- `internal/ops/service_git_init_test.go`: `TestInitServiceGit_HappyPath`,
  `_Idempotent` (byte-identical two calls must still hold).
- `internal/tools/workflow_bootstrap_automount_test.go:87-141`.
- `internal/tools/workflow_git_push_setup_reconstruct_test.go:38-99`.
- NEW test for origin-sync identity fragment (site 4).

**Prose in the same phase:** `internal/ops/deploy_failure_signals.go:599-601`
(`git-identity-missing` signal): "writes the deploy identity into git config
on every SSH deploy" → set-if-absent wording; workaround line stays valid.

**Gate:** `go test ./internal/... -short`, `make lint-fast`.

## F2 — remove per-deploy auto-commit + bootstrap HEAD (P2)

**Code:**
1. `internal/ops/deploy_ssh.go:263-266` — DELETE the `gitCommit` fragment
   (`git add -A && (git diff-index ... || git commit -q -m 'deploy')`).
   Follow the chain: `gitCommit` var, comment block 236-257 rewrite (it
   currently explains the commit cases), `pushCmd` assembly.
2. HEAD guarantee, atomic with (1) — BOTH sites (D2a ordering risk).
   Fragment (Codex plan-review finding 2 folded — `commit --allow-empty`
   would commit STAGED INDEX content as 'zcp init' on an unborn repo with
   staged files; the guard must be index- and worktree-independent):
   `(git rev-parse -q --verify HEAD >/dev/null || git update-ref HEAD "$(git -c user.email='agent@zerops.io' -c user.name='Zerops Agent' commit-tree "$(git mktree </dev/null)" -m 'zcp init')")`
   — empty tree via `mktree </dev/null`, parentless `commit-tree`, HEAD set
   via `update-ref`; never reads the index or worktree. (`rev-parse -q
   --verify HEAD` on unborn HEAD: exit 1, silent — locally probed; grouping
   mandatory as in F1.)
   - `InitServiceGit`: after init + identity (canonical site).
   - `buildSSHCommand` safety-net: same fragment (migrated / cold services
     that never ran bootstrap).
   - `confirmGitPushSetupContainer`: run the ensure fragment BEFORE the
     write probe (Codex finding 3) — today `BuildGitWritePushProbeCommand`
     silently degrades to read-only `ls-remote` proof on unborn HEAD
     (InitServiceGit failures are swallowed at bootstrap), so setup could
     stamp `configured` from a read-only proof. With HEAD ensured, the
     probe is always the real `push --dry-run`. Test: bootstrap meta
     exists + `.git` missing/unborn → setup still uses dry-run write proof.
   - Per-invocation `-c` robot identity — the marker commit is ZCP's, not
     the user's; values from `DeployGitIdentity` via the single-owner
     fragment builder. Idempotent by construction (HEAD exists ⇒ no-op),
     which keeps `TestInitServiceGit_Idempotent`'s byte-identical +
     no-second-commit properties.
   - NEW real-git test: unborn repo WITH staged files → ensure fragment
     creates 'zcp init' that contains NO staged content; staging area
     untouched.
3. zcli floor: document ≥ v1.0.61 in the spec GLC block (no runtime probe —
   containers ship platform-maintained zcli; live verification covers it).

**RED first:**
- DELETE/REWRITE: `TestBuildSSHCommand_AlwaysCommits`,
  `TestBuildSSHCommand_NoChanges_SkipsCommit` (deploy_git_test.go:142-158).
- EDIT EXISTING TABLES (Sonnet plan-review finding 2 — these also assert the
  commit fragment and would go red silently):
  `TestBuildSSHCommand_Shape` wantContains (deploy_ssh_test.go:553-554) and
  `TestBuildSSHCommand_GitGuard` "basic command" wantParts
  (deploy_git_test.go:48-49) — STRIP `"git add -A"` + `"git commit -q -m
  'deploy'"` lines and MOVE them to the forbidden/wantAbsent sets.
- NEW: shape test asserting NO `git add -A` and NO `-m 'deploy'` anywhere in
  the emitted command; HEAD-guard fragment present; `zcp init` commit carries
  inline `-c` identity.
- REWRITE behavioral `TestBuildSSHCommand_FreshInitPath`: fresh repo →
  after command, HEAD exists with exactly one `zcp init` commit; a dirty tree
  run leaves the tree DIRTY (no new commit) — run real git as today.
- `TestInitServiceGit_*`: add HEAD-guarantee assertions; idempotency (second
  run adds NO second commit).

**Content/spec in the same phase (blast-radius §3/§4 checklist):**
- `docs/spec-workflows.md`: GLC intro :1110 ("substrate ... runs `git add -A
  && git commit` against") + :1112 happy-path prose (pre-B13 stale ANYWAY) +
  GLC-2 row (set-if-absent + no-commit + HEAD guard) + GLC-3 row (write
  policy note) + :877 and :906 pre-existing false claims ("git commit + push"
  — git-push never commits) + GLC-5 dead citation (`bootstrap-git-init`
  scenario doesn't exist) + GLC-6 zcli floor note.
- Atoms: `develop-platform-rules-local.md` VERIFIED CORRECT as-is (its
  "ships the tree / needs no git state" lines describe LOCAL mode, which is
  `--no-git` and unchanged — the earlier inventory flag was a false
  positive; `develop-platform-rules-container.md` makes no commit claims).
  Do extend `internal/content/git_push_atom_sentinel_test.go` with forbidden
  phrases claiming the default deploy auto-commits (tell==check).
- Authoring briefs (prose-only, maintainer domain):
  `internal/authoring/recipe/content/phase_entry/scaffold.md:14-16`
  ("deploy commits — created by zerops_deploy" → gone) + the stale "no git
  identity by default" blocks (scaffold.md:40-56,
  briefs/scaffold/decision_recording.md:122-133,
  decision_recording_slim.md:168-180 — pre-existing drift, uses a different
  identity; align with the new contract). Check
  `internal/authoring/recipe/validators_test.go:665,683` anchors still pass.
- `deploy_failure_signals.go` B13 signal: still valid as recovery (identity
  can be absent on foreign repos), reword "normally writes ... on every
  deploy".

**Explicit behavior change (documented, intended):** dev-container trees stay
dirty between direct deploys. The launch `dev-tree-dirty` gate + close-cadence
guidance become the explicit sign-off-commit enforcement (recovery text
already correct). Note in spec §4.3/GLC. Flow-eval watch item post-ship.

**Wider prose/testdata sweep (Codex finding 7):**
- Golden scenarios: atom edits require refreshing
  `internal/workflow/scenarios_golden_test.go` goldens (comparison is
  default-on).
- `docs/spec-work-session.md:628` — old commit-ownership wording.
- Code comments encoding the old contract: `DeployGitIdentity` doc comment
  (deploy_ssh.go:23-25), `BuildGitPushCommand` comment, the
  `deploy_failure_signals` matcher comment.

**Landing rule (Codex finding 1):** F1+F2 are implemented as separate slices
but land on main as ONE commit — F1 alone would attribute robot 'deploy'
commits to a foreign clone identity (the analysis's own P1-incomplete
warning).

**Gate:** full `go test ./... -short`, `make lint-fast`, then `make
lint-local`.

## F3 — human attribution at git-push-setup (P4)

**Code:**
1. New `internal/ops/github_user.go` (or similar): derive identity from the
   PAT — `GET https://api.github.com/user` with `Authorization: Bearer
   <token>`; name := login; email := public email if non-empty else
   `<id>+<login>@users.noreply.github.com` (documented noreply format;
   Codex-verified: works for fine-grained PATs without extra permissions).
   HTTP via the EXISTING `ops.HTTPDoer` seam already threaded through
   `RegisterWorkflow` (Codex finding 5) — no new package-global client, no
   parallel test hook. ONLY for remotes whose host is `github.com`
   (`parseGitHost` is the owner); other hosts skip derivation (P1 robot
   fallback stands).
2. `confirmGitPushSetupContainer` (workflow_git_push_setup.go): after probe
   passes, best-effort seed via SSH: write user.name/user.email IFF current
   value is absent OR exactly equals `DeployGitIdentity` (the stomped-repo
   migration — exact-match means ZCP wrote it). Derivation/SSH failure is
   non-blocking (warning, setup proceeds). If a CUSTOM identity differs from
   the derived one: preserve it AND surface a visible "identity preserved"
   note (never silent).
3. `BuildGitReconstructCommand` call site (same file :722): pass the derived
   identity when available so a reconstructed repo lands human-attributed;
   robot fallback otherwise. Local mode: untouched (GLC-6 — never touch local
   identity).
4. Already-configured recall path (Codex finding 4): the tokenless
   same-remote recall (`gitPushConfiguredRecall`, :483/:738) returns before
   probe/seed, so an existing configured pair with robot identity would
   never migrate. Fix: on recall, read the current identity; if it exactly
   equals the robot identity, include a response note instructing a one-time
   re-run with `gitToken` to migrate attribution (no token ⇒ cannot derive;
   never fabricate). When the re-run arrives with a token, the normal probe
   path seeds (item 2).

**RED first:** unit tests for derivation (public email / private email /
non-github host / API failure), seed-command shape tests (absent → seeds;
exact-robot → replaces; custom value → untouched), reconstruct-with-identity
test. New behavior — new tests, no rewrites expected beyond reconstruct
shape.

**Consequence for free (assert in tests where cheap):** release tags + export
commits + flatten commits inherit the seeded identity (they read ambient
config; verified in the review §7a).

**Documented semantics (Sonnet plan-review item 7 — notes, not code):**
- Migration robot→human fires ONCE; a later PAT rotation to a different
  GitHub account does NOT re-seed (identity is user-owned once set). State
  in the spec GLC identity row.
- A buildFromGit clone carrying a recipe-baked non-robot identity is neither
  absent nor exactly-robot ⇒ never auto-migrated; the "identity preserved"
  note (item 2) covers visibility.

**Gate:** full `go test ./... -short`, `make lint-fast`.

## F4 — backlog + doc closure

- `git rm plans/backlog/m1-glc-safety-net-identity-reset.md` (superseded:
  P1 ships set-if-absent; the entry's open question is answered).
- NEW `plans/backlog/ci-selfdeploy-git-config-wipe.md`: CI self-target `-g`
  ships the runner checkout `.git` → wipes credential helper (+identity);
  `gitPushConfiguredRecall` existence-only check reports already-configured;
  launch push-proof `|| true` misclassifies as `head-not-pushed`. One root
  cause, three symptoms; sketch: post-CI reconcile or helper re-assert +
  probe-based recall.
- NEW `plans/backlog/l0-to-l1-history-offering.md`: robot-'deploy' histories
  of EXISTING pairs ship to GitHub verbatim on first push; optional
  clean-slate (flatten) offering at git-push-setup. Product decision.
- Review doc: mark §5 directions with final state; archive when merged.
- CLAUDE.md: no new trap line (tests pin the contract; GLC spec rows are the
  home). Verify budget rule not violated.

## Live verification (orchestrator-owned, before merge)

On eval-zcp (services provisioned ad-hoc, deleted after):
0. `zcli --version` on the container ≥ 1.0.61 (record the actual value in
   the ship report); submodule + LFS probe through the dirty-tree deploy
   path (expect pointer semantics, prove not a regression vs. the old
   commit path) — Codex finding 6.
1. Fresh nodejs service, bootstrap → mount → verify `zcp init` commit exists,
   identity robot (fresh repo).
2. Dirty-tree direct deploy → build succeeds, content matches dirty tree,
   NO new commit, tree still dirty (P2 core).
3. Set custom identity on container → deploy → identity SURVIVES (P1 core);
   commit as user → committer is the user.
4. B13 class: service provisioned via buildFromGit (clone without identity),
   direct deploy → set-if-absent fires, zcli stash-create succeeds.
5. Self-deploy `-g` round-trip → .git + identity + history survive artifact
   replacement.
6. git-push-setup on fresh pair (zcp-init-only HEAD) → probe + first
   strategy=git-push push succeed; GitHub shows human-attributed commits
   (P4); repo-local config seeded (exact-robot replaced).
7. Existing e2e suite: `TestE2E_InitServiceGit` (rewritten for new
   semantics), `git_delivery_fullchain_test.go`, `deploy_test.go` with
   `-tags e2e`.

## Execution protocol

Per phase: Sonnet-5 implementer agent (RED → GREEN → REFACTOR, phase
checklist from this plan) → orchestrator runs gates → Codex diff review →
fold findings → next phase. Plan-fidelity walk per phase before "shipped"
claim. Landing: F1+F2 as ONE commit on main (see F2 landing rule); F3 and F4
as their own commits (no release, no `make install`).
