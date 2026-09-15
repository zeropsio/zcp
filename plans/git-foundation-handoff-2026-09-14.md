# Git foundation — handoff (read this first)

Status 2026-09-14 (evening): branch `feat/git-foundation` carries slices 1, 2a, A (tag evidence), B (adopt baseline), C (rollback) — read §8 first, it supersedes §4–§7 where they differ.
The original plan `git-foundation-2026-09-14.md` was written before its author read the
spec's git lifecycle section; §3 below states what it got wrong. Treat THIS file as the
entry point; the older plan is kept only for its scenario tables (§6) and platform ledger.

Audience: an agent taking over. Everything here is either quoted from the owner, verified
live on the platform (with the memory pointer), or read from spec/code with the reference.
Nothing is inferred.

## 1. Intent — in the owner's words (Karel, 2026-09-14, Czech, verbatim)

1. *"zamysli se nad tim jak vyresit koncepci prace s gitem u zcp mnohem lepe, projdi ten
   lifecycle... git je foundational layer veskereho kodu... podporovat vsechny ruzne typy
   provideru... nejdulezitejsi je ta koncepce... neco jako ma orb, t3... pocitat s novymi,
   importovanymi nebo adoptovanymi projekty"*
2. *"promylis to ve vsech dusledcich ... podle me to pak muze ovlivnit deploye ... jestli
   cely ten mechanismus nejak proste uplne neposunout"*
3. *"podivej se jak s tim pracuje treba t3 https://github.com/pingdotgg/t3code nebo amp
   code ci podobne"*
4. From the discussion with Aleš (pasted by Karel): Karel — basic git on Zerops permanent
   storage with bare repos; Aleš — shared git needed for mate1/mate2 collaboration and for
   CI/CD-on-merge to prod without GitHub; self-hosted Gitea as a first-class recipe;
   bootstrap should be able to push dev/stage pairs + gitea + prod with CI/CD; Karel —
   git+CI inside Zerops in the default form so basic use is not painful.
5. *"gitea na zeropsu v centralnim projektu bude optional default, ale nebudeme ji mit
   vzdy. ale v podstate bychom to meli tak ze jakekoliv nasazeni produkce ale uz znamena
   mit nejaky origin git s ci. co myslis?"*
6. Rejected the assistant's rule "prod = pull from origin": *"promysli cely ten retezec,
   od a do zet ... aby to fungovalo pro vsechny mozne situace a konfigurace a pritom to
   bylo jednoduche a 'spravne'"*
7. *"rozpracuj tu tezi do komplexniho planu co by to realne znamenalo pro zcp ... otestuj
   si co budes potrebovat bud pres farmu nebo eval zcp"*
8. *"vymysli jake k tomu udelame scenare zaroven budes muset vymyslet jak se to dotkne
   tech stavajicich do farmy abychom to presto delali a pust se do implementace pomoci
   agentu. udelej to cele na nejake branchi"*
9. Correction: *"zcli ma prece with git nejakej prepinac, na tom je ted postavenej ten
   self deploy, ze to dela s nim a diky tomu se ten var/www zachova"* — true, see §2 P10.
10. Verdict on the process: the assistant *"neprostudoval spravne co a jak funguje a
    nepochopil ani zakladni koncepty"* — see §3.

What the intent is NOT: it is not "replace the delivery model". It is: git is the base
of every code path, provider-agnostic (GitHub, GitLab, self-hosted Gitea, any git),
covering new / imported / adopted projects, with prod requiring an origin + CI, Gitea
inside Zerops as the optional default origin, and mate collaboration over a shared repo.

## 2. Verified platform facts (live, project `eval` Rd2bffa7…; full evidence in
`.claude/agent-memory/platform-verifier/verified-facts.md`, sections dated 2026-09-14)

| # | Fact | Consequence |
|---|---|---|
| P1 | `zcli push --version-name <s>` is stored only in ES `SearchAppVersions.name`; direct GET DTOs have no `name`; no commit sha/branch anywhere on an appVersion, even `source: GIT` | The platform cannot tell you which commit runs. A ledger must live in git. |
| P2 | Self-deploy replaces the whole container (new ZFS root); ignored files (`.env`, `node_modules`) and `/home/zerops` are gone | Nothing off-tree survives a deploy. |
| P10 | **With `zcli push -g` (what zcp's container self-deploy already uses, GLC-2), `/var/www/.git` lands byte-complete in the new container**: commits, tags, custom refs `refs/zcp/*`, reflog, `.git/config`, `.git/info/exclude`. Default `--workspace-state all` also ships uncommitted files; `clean` ships HEAD only. A `gitdir:` pointer file is NOT rescued. A committed build output (`dist/`) reads ` M` after every self-deploy because the build regenerates it — exclude patterns must cover build dirs. | The repo survives self-deploy because it travels inside the artifact. Dev self-deploy stays legitimate. The ledger (slice 1) survives too. |
| P3 | Git-dir push archive = tracked + untracked-unignored, excludes ignored and `.git` (unless `-g`); non-git dir needs `--no-git` | Push-from-commit needs an extracted tree. |
| P8 | zcli has NO tarball input; `--archive-file-path` is an output tee joined onto the workdir | Push-from-commit = `git archive sha \| tar -x -C tmp` + `zcli push --working-dir tmp --no-git --version-name sha`. |
| P9 | zcli default `--workspace-state all` exits 128 on a `gitdir:` pointer-file `.git` (temp index inside `.git`) | Never emit that layout. |
| P4 | `buildFromGit` accepts only github.com / gitlab.com (Gitea, Codeberg fail 36 ms after a successful ls-remote, `internalServerError`) | Platform pull from any other forge is impossible today. |
| P5 | Webhook receivers exist only as `github-webhook` / `gitlab-webhook`, bound to the account's OAuth integration; no generic HMAC trigger. Authenticated trigger: `PUT /service-stack/{id}/trigger-pipeline` (host-restricted) or `zcli push` | CI from another forge needs either a platform change (ask Aleš) or a zcp-owned relay. |
| P6 | `POST /client/{clientId}/integration-token` mints a token scoped to `projects:[{projectId, roleCode}]`; regenerate/delete; no TTL/one-time field | A scoped token can be minted, used, deleted by zcp. (The platform ALSO has a separate launch delegation, see §3.) |
| P7 | Gitea 1.24 runs on Zerops as `ubuntu@24.04` + binary: needs `HOME`, app.ini before `gitea migrate`, ≥1 GB RAM; no managed type/recipe | "Managed Gitea" = a recipe + zcp provisioning. |
| — | git + zcli present on nodejs@22, ubuntu@24.04, static | In-container git is always available. |

## 3. Reality of the code and spec (read before designing anything)

The spec already encodes most of "git as the foundation" — for GitHub/GitLab. The
original plan claimed to introduce things that exist. Table of what exists, with the
authoritative home:

| Concern | What exists today | Home |
|---|---|---|
| Repo on every dev service | `ops.InitServiceGit` after mount at bootstrap AND adopt: SSH-exec `git init`, identity set-if-absent, HEAD guarantee (empty marker commit). Deploy path self-heals a missing `.git` | `spec-workflows.md` §Git Lifecycle GLC-1..6; `internal/ops/service_git_init.go`, `git_identity.go::GitEnsureRepoHeadCommand`, `deploy_ssh.go::buildSSHCommand` |
| Self-deploy keeps the repo | container self-deploy runs `zcli push -g` (`includeGit`), tree ships via zcli's stash-archive, zcp mints no commit | GLC-2; `deploy_ssh.go:335`; P10 |
| Deploy classes | DM-1..DM-5 (self vs cross, `deployFiles` contracts, `ClassifyDeploy`) | §8 DM |
| Origin (shared repo) | `zerops_workflow action=git-push-setup`: probes (remoteUrl, token) before any state write, stores `GIT_TOKEN` as sensitive service var, url-scoped credential helper in the container, stamps `GitPushState=configured` + `RemoteURL`; rotation and reconstruction paths | §4.3/§4.4, line ~60; `tools/workflow_git_push_setup*.go`, `ops/git_credential.go` |
| Delivery after origin | derived, not chosen: `configured` ⇒ commit+push is the terminal act; direct deploy on a configured service redirects to push only when `BuildIntegration=webhook\|actions` consumes pushes | §4.3 "Delivery is derived"; `topology/delivery*.go`, `tools/deploy_repo_delivery.go`, `TestResolve_ConfiguredDrivesGitPushDelivery` |
| CI shapes | `BuildIntegration` none / webhook (GitLab) / actions (GitHub); `action=build-integration` writes the workflow file / webhook integration | §4.3; `tools/workflow_build_integration.go` |
| Close-mode | auto / manual = done-ness ownership only; legacy `git-push` folds to auto | §4.3 |
| Prod requires origin | launch-production gate: `GitPushState=configured`, RemoteURL matches live `git remote`, tree clean, HEAD pushed | P-LP-10, P-LP-11 |
| Prod without a standing token | single-token staged secret (`ZCP_LAUNCH_TOKEN`, once) OR **platform delegation**: `ListOwnTokenDelegations` → zcp mints the launch token itself, token never in the conversation, delegation consumed on mint | P-LP-14, P-LP-15; scenario `launch-production-delegated.md` |
| Prod pipeline | pipeline-first, prod import has no `buildFromGit`, first release is the first build; GitHub Actions with `ZEROPS_TOKEN_PROD` repo secret or GitLab webhook | §10; `tools/workflow_launch_production.go` |
| Deploy history | `DeployAttempt` in the work session (no sha), `FirstDeployedAt`, `record-deploy` for async builds | `spec-work-session.md`; `workflow/work_session.go:76` |
| Mate | each mounted dev service is its own repo over SSH; policy disables worktrees, stacked commit→push→PR, background fetch — "commit and push are zcp's" | `spec-mate.md §6`; `../z3 apps/server/src/zerops/ZeropsPolicy.ts` |
| Eval | 30-cell gate set derived from `spec-scenarios.md §9.3`; scenarios pushed as their own content-addressed tree, independent of the candidate binary | `eval/farm/gate-set.txt`, `TestEvalMatrix_*`, `spec-eval-farm.md` |

**What the original plan got wrong** (so the next agent does not repeat it):
- Claimed no git init exists → GLC-1 does it. Claimed dev must never self-deploy → `-g`
  keeps the repo (P10). Claimed delegation must be emulated with integration tokens → the
  platform has a launch delegation (P-LP-15) already used by zcp. Claimed "prod = origin"
  is a new rule → P-LP-10/11 already enforce it.
- Root cause: the author read the CLAUDE.md index and delegated code mapping to scouts,
  trusted a scout's negative finding ("no git init anywhere"), and had the verifier test
  the author's own phrasing (a laptop push without `-g`) instead of zcp's real command.
  Rule for the successor: read `spec-workflows.md` §4.3, §8 DM, §Git Lifecycle, §10 and
  `spec-mate.md §6` yourself before touching the design.

## 4. What is on the branch (`feat/git-foundation`, off main 9314c8a2)

Landed and green (`go test ./... -short`, `make lint-fast`, `make lint-local`):

- **Slice 1 — ledger + deploy from a commit** (commits a2101f5b..59b5e0df). New:
  `internal/topology/git_ledger.go` (`LedgerEntry`, `EnvRefName`, `DeployRefPrefix`);
  package `internal/ops/git/` (`Runner` = `LocalRunner` | `SSHRunner`, `ResolveSHA`,
  `ExtractCommitToTemp`, `MkTempDir`/`RemoveTemp`, `WriteLedger`, `ReadEnvRef`);
  `zerops_deploy` gains `sha` (explicit only; empty = today's path byte-for-byte on the
  push args). Flow: resolve → extract to tmp OUTSIDE workdir → `zcli push --no-git
  --version-name <sha>` → after the build resolves, `refs/zcp/env/<target>` → sha and
  `refs/zcp/deploy/<unix-nanos>` → commit object (parent sha, JSON message
  `{sha, appVersionId, target, project, at}`); result text says `replaces <prev7>`;
  `DeployResult.SHA/PreviousSHA/AppVersionID`; `DeployAttempt.SHA/AppVersionID` rendered
  in status. Spec `spec-workflows.md §4.9`. Reviewed by a judge; six findings fixed
  (`cd ''` in the SSH script, commit identity for `commit-tree`, orphans, cancel-safe
  cleanup). Genuinely new: today nothing records which commit runs where (P1).
- **Slice 2a — repo extras** (e34098fb..d689ec4c). `ServiceMeta.Repo{BaselineAppVersion,
  Provenance}`, adopt tags `zcp/baseline/<appVersionId>`, `.git/info/exclude` seeding by
  runtime class, `repo:` block in the status envelope, provision StepChecker. Written
  without knowing GLC-1 exists and first duplicated the init; **consolidated** (commits
  a6d47f94..70c5f92b): `git.InitRepo`/scaffold commit and `ops.EnsureScaffoldRepo`
  deleted, exclude seeding is a fragment inside `GitEnsureRepoHeadCommand` (threaded by
  runtime class through every caller), §4.10 folded into the GLC table (GLC-1 + new
  GLC-7). G4 `dev-self-deploy-keeps-repo` scenario written (not run).
- **Eval**: scenarios written, NOT run: `deploy-from-commit-stage` (G3),
  `rollback-stage-from-ledger` (G5, preseed `deploy-from-commit-twice.sh` unverified),
  `repo-always-bootstrap` (G1), `repo-always-adopt-baseline` (G2); table G in
  `spec-scenarios.md §9.3` with `promote:` status (not in the gate set). Farm impact
  tables: `git-foundation-2026-09-14.md §6`.
- **Known gaps on the branch**: no atom for deploy-from-commit (gating would auto-inject
  into goldens; needs an envelope signal); provenance `artifact-only` never computed
  (adopted appVersion's sourceService/deployFiles not on the DTO — verify live);
  spec §4.9 says "§4.5" in one code comment (`deploy_ssh.go`).
- Plan briefs in `git-foundation-2026-09-14-briefs/`: `slice-2a` (superseded by the
  consolidation), `slice-3-origin` (**withdrawn** — it deletes working machinery; see §5).

## 5. The corrected direction — extend, do not replace

Given §3, the real delta against the intent in §1 is:

| # | Gap | Shape of the change (additive) |
|---|---|---|
| D1 | No record of which commit runs where | DONE on branch (slice 1). Next: make `sha` the default when the tree is clean; keep dirty-tree push as the dev loop. |
| D2 | Origin is GitHub/GitLab-shaped | `topology.GitHostKind` gains `gitea` + `generic`; `git-push-setup` already speaks HTTPS+PAT generically — verify against Gitea live, drop GitHub-only assumptions (`IsGitHubRemote` identity, `RecommendDelivery` gitlab-string sniff). |
| D3 | CI only via GitHub Actions / GitLab webhook (P4, P5) | Either platform asks for Aleš (host allowlist / generic HMAC trigger / sha on appVersion) or a zcp **relay**: `BuildIntegration=relay`, a small zcp process in the target project holding a project-scoped token (P6), receiving the forge webhook, `git archive sha` → `zcli push --version-name sha`. Relay is the universal fallback. |
| D4 | Managed Gitea | recipe (P7) + `ops/gitea/` provisioning (admin, org, repo, token, webhook) + placement decision (org hub project vs per-project). `git-push-setup` against it = origin `managed`. |
| D5 | Prod launch is GitHub-centric | launch-production learns the relay / Gitea pipeline for non-GH origins; delegation stays P-LP-15. |
| D6 | Mate cannot commit/push | `spec-mate.md §6.3`: re-enable through zcp once origin exists; `refs/zcp/*` exempt from thread-ref pruning (§6.4). |
| D7 | Bootstrap does not offer dev/stage + gitea + prod-with-CI as one default | bootstrap route option once D4 exists. |

Untouched by this direction: `GitPushState`, `BuildIntegration`, `CloseDeployMode`,
`git-push-setup`, GLC-1..6, DM-1..5, §10 — they stay and are extended. The farm gate set
stays; new G cells are added.

## 6. Decisions the owner still has to make

- Confirm "extend, not replace" (§5) — asked 2026-09-14, unanswered.
- P-1 relay placement: in the prod project (one token, one project, matches P-LP-14/15)
  vs a hub project (one relay, multi-project token). Recommendation: prod-local.
- P-2 platform asks for Aleš: `buildFromGit` host allowlist or a generic HMAC build
  trigger; commit sha on the appVersion DTO; token TTL. With these the relay disappears.
- P-3 managed Gitea placement: org hub project with subdomain vs per-project.
  Recommendation: hub, provisioned once per org.
- Whether slice 1's `sha` should become the default deploy path when the tree is clean.

## 7. How to continue safely

1. Read §3's spec sections first. Then `git log main..feat/git-foundation`.
2. Nothing is in flight; all worktrees are merged.
3. Run the G1/G3 scenarios once on the farm against a branch candidate
   (`farm push --candidate`, `--scenarios eval/behavioral/scenarios`, `farm run --set
   <ids>`) before adding any further code — nothing on the branch has been run live.
4. Every platform claim in a new brief goes through `platform-verifier` phrased as zcp's
   real command path, not as the design's wording.

## 8. Day 2 (2026-09-14 evening) — what changed, what was proven, what is next

Read this section before §4–§7; where they differ, §8 wins. Owner decisions taken today:
direction "extend, not replace" confirmed; evidence as **git tags** (owner's ask); `sha`
stays explicit, never a default; Gitea placement still open (recommendation: hub). Judge
review of the landing: 4 FIX-FIRST + 6 NOTES, all but one acted on (8.4 item 6).

### 8.1 Codex's review of this handoff — all six points verified against code and fixed

| # | Finding (verified) | Fix landed |
|---|---|---|
| 1 | No single place says which ref a target consumes: `main` hard-coded in the git-push default, the Actions template and the launch gate | Stated as GF-7 (OPEN) in spec §12; not built |
| 2 | Ledger was two custom refs keyed by hostname, written only on the sha path — stale after any ordinary deploy | Replaced by one annotated tag `zcp/deploy/<project>/<target>/<appVersionId>` per zcp deploy (sha path, working-tree path with `dirty`, batch); platform stays the authority for the active appVersion; "what runs" = join; no tag ⇒ source unknown (§4.9, GF-5) |
| 3 | sha deploy with source==target shipped `--no-git` (would delete the container's repo); validated the mount's `zerops.yaml`, not the commit's | Self-deploy refused before any SSH call (GF-3); `zerops.yaml` read from `git show <sha>:zerops.yaml` (GF-4) |
| 4 | Adopt tagged GLC-1's empty marker commit as the baseline; provenance hard-coded `source` | `AdoptBaseline` decides by content (empty-tree HEAD ⇒ snapshot commit with robot identity inline; content ⇒ tag only); provenance `snapshot` / `existing`, neither claims parity (GLC-7) |
| 5 | G5 demanded "no rebuild" but prescribed a rebuild; `appVersion` accepted only `latest` | `zerops_deploy appVersion=<id>` re-activates a BACKUP appVersion (`stack.deploy.backup`, no build) in SSH and local mode; `RedeployLastAppVersion` refuses ACTIVE (§8 R2, GF-8) |
| 6 | Relay was a premature architecture | Dropped: Gitea Actions proven (8.2) |

### 8.2 Verified live today (platform-verifier memory, sections dated 2026-09-14)

- **Rollback**: `PUT /app-version/{id}/deploy` on a `BACKUP` version → one `stack.deploy.backup`
  process, no build, ~50–70 s, repeatable; body ignored for BACKUP; ACTIVE → `appVersionInvalidStatus`.
- **Gitea Actions + `zcli push`**: Gitea 1.24.6 + act_runner 0.2.13 HOST mode in the same
  `ubuntu@24.04` container, push → ACTIVE in 83–105 s, survives a service restart; no node on
  the host (plain `git clone`, never `actions/checkout`); pass the token as step `env`, never
  `zcli login`. Recipe shape recorded in spec §12.5. **No zcp relay is needed.**
- **Farm — all five G cells PASSED live** (batches `gf-g-1`..`gf-g-5`, candidates from this
  branch): G1 `repo-always-bootstrap`; G2 `repo-always-adopt-baseline` (after fixing a wrong
  premise: a buildFromGit build DOES leave the clone's `.git` history, so adopt takes
  `existing` — scenario + spec §12.2 corrected); G3 `deploy-from-commit-stage` (sha →
  `zcli push --no-git --version-name`, tag written, result carries sha + appVersionId);
  G4 `dev-self-deploy-keeps-repo`; G5 `rollback-stage-from-ledger` — `appVersion=<id>`,
  no `latest`, no `sha`, no rebuild. G5 took four runs: preseed lacked `--setup prod`;
  preseed appended to an untracked file (now adds a tracked marker file); one fixture build
  failed on the platform; and one REAL zcp defect — the rollback body sent empty strings,
  which the platform rejects with `invalidUserInput` (null is what it ignores) — fixed in
  `platform.RedeployAppVersion` + `TestRedeployAppVersion_EmptyFields_SendNull`.
  Note from the passing run: the agent first called `appVersion="__lookup_probe__"` to
  harvest the candidate list from the error, then the real id — see 8.4.
- **Farm — full gate set on the branch candidate** (batch `gf-gate-1`, 32 cells): 22 passed;
  5 fail identically on the `main` baselines matrix-2/3/4 (`develop-add-managed-dep-to-existing`,
  `export-buildfromgit-self-snapshot`, `existing-simple-mode-node-add-endpoint`,
  `recipe-laravel-showcase-fullstack`, `greenfield-fullstack-multi-runtime`); 1 flip
  (`cross-deploy-stage-promote-from-dev`, mustOffer phrasing) passed on the second sample
  `gf-gate-1b`; 1 was G2 on the pre-fix scenario digest; 3 blocked by farm env not set for
  these batches (`ZCP_E2E_GITHUB_PAT`, `ZCP_E2E_EXISTING_PROJECT_ID`). **No regression
  attributable to the branch.**

### 8.3 What landed (commits 1fc1840d..HEAD)

- Hygiene: the branch was rebuilt without 376 swept-in plan files and 28 unrelated recipe
  `.import.yml` bumps (those live on `chore/recipe-import-bumps`); diff vs main is code/docs/eval only.
- Spec: §12 "Git Foundation" (vocabulary, entry routes, the change path, two checkouts over one
  origin, provider-agnostic delivery contract with the platform limits, GF-1..GF-10, open items);
  §4.9 rewritten to the tag model; GLC-7 rewritten; §8 R2 extended; §12.5 Gitea row verified.
- Code: `topology.DeployTagName/DeployTagPrefix`, `LedgerEntry.Dirty`; `ops/git.WriteLedger`
  (tag), `LastDeployOnRecord`, `HeadStatus`, `ReadFileAtCommit`, `ResolveSHA` hex-validated,
  `SSHRunner` surfaces git stderr; `ops.DeploySSH` self-guard + commit validation +
  `deployFromCommitPrep`; `ops.ValidateZeropsYmlContent`; `ops.ReactivateAppVersion`
  (`deploy_rollback.go`), `platform.BuildStatusBackup`; `ops/git.AdoptBaseline` content rule,
  `topology.RepoProvenance{snapshot,existing}`; tools: `runAppVersionRollback`, batch evidence,
  `Dirty` on `DeployResult`/`DeployAttempt`/`AttemptInfo`, envelope `repo.provenance`.
- Eval: G2 gated (+ gate-set.txt); G3/G4/G5 + preseeds on tags; G5 = appVersion-id rollback.
- `plans/git-foundation-platform-asks-2026-09-14.md`: the four asks for the platform team.

### 8.4 Open, in order

1. Rollback UX: the target's appVersion list (id, status, sequence, version name) is reachable
   only through the "does not belong" error today — expose it on the status envelope /
   `zerops_events` so the agent never has to probe with a fake id.
2. GF-7 tracked ref: record once per target, read by push default / CI template / launch gate.
3. GF-9/GF-10 evidence sharing: push `refs/tags/zcp/deploy/*` with the tracked ref, and
   `--version-name <sha>` on every zcp-driven build and CI template — one decision.
4. D2 `GitHostKind gitea|generic` + `git-push-setup` verified live against a Gitea.
5. Managed Gitea recipe (spec §12.5 shape) with act_runner; placement decision (owner).
6. Judge NOTE not yet acted on: exclude patterns for non-Node ecosystems (`vendor/`,
   `.venv/`, `target/`) in GLC-1's list; the two SSH round trips of the working-tree record
   (`HeadStatus` + `LastDeployOnRecord`) could be one script.
7. Mate commit/push through zcp (`spec-mate.md §6.3`) — z3 fork.


## 9. Day 3 (2026-09-15) — supersedes §8.4 where they differ

Landed on `feat/git-foundation` (`8a79c1d9`..`71966fe6`), every slice farm-proven on the final
candidate (batch `gf-cargo-7`, PAT cell `gf-cargo-8`; all seven cells passed):

- Tag ledger removed (GF-9); adopt records `provenance` only; user exclude lines preserved.
- Repo-cargo preservation proven: `preseed/lib-repo-cargo.sh` plants branch, user commit, tag,
  custom ref, dirty + untracked file, exclude line, identity, foreign origin; G2 adopt, G3
  deploy-from-commit, G4 self-deploy assert every item intact; G6 `repo-adopt-snapshot-no-git`
  proves the snapshot case (`.env` on disk, never committed). Matrix rows G1/G3/G4 → gate, G6 added.
- Rollback candidates: `zerops_events serviceHostname=<h>` lists appVersions (active/backup);
  status envelope carries `rollback: {active, backup[]}`; the "does not belong" error points
  there; result says "without a rebuild". G5 passed twice with no fake-id probe.
- GF-7 `ServiceMeta.TrackedRef` recorded at git-push-setup, read by push default, Actions
  template, launch gate. GF-10 `--version-name <sha>[-dirty]` on every zcp-driven deploy and
  `$GITHUB_SHA` in the Actions template; the §10 launch-production template still lacks it (OPEN).
- GF-11 (new): a rejected non-fast-forward push is classified (`GIT_PUSH_NON_FAST_FORWARD`,
  remoteAhead/localAhead/unrelated, options rebase | merge | replace-remote), git-push-setup
  reports `remote.state`; zcp never forces or merges. Live: the agent asked the user, chose
  nothing on its own (`gf-cargo-7`), and completed once the persona answered (`gf-cargo-8`).
- Farm: `ZCP_E2E_GITHUB_PAT` pass-through from the controller env to runs that declare it;
  `gitRepoReset:` resets `krls2020/eval2` to an orphan README before seed and after the run in
  every mode (FM-67); one lane per repo; PAT redacted from bundles. Trap: the evaluator must be
  re-pushed when eval code changes (`farm push --evaluator`), the candidate alone carries nothing.

Open: launch-production template `--version-name`; `versionName` on rollback candidates (ES
`SearchAppVersions.name`, not on the direct DTO); Gitea recipe + placement; mate commit/push.

### 9.1 Day 3, afternoon — two remote shapes, user-sim fix

- `gitRepoCreate: krls2020` (admin PAT `ZCP_E2E_GITHUB_PAT_ADMIN`, all repos of the test
  account): a private empty `zcp-farm-<ts>` repo per run, created before seed, deleted after
  the run in every mode, swept by `farm gc`. New gate scenario `git-push-setup-empty-remote`
  (setup + ONE git-push deploy, no CI of either kind): passed `gf-cargo-13` — `remote.state=empty`,
  no non-fast-forward question, origin `main` exists, repo gone afterwards.
- `git-push-setup-then-actions` (shared `eval2`, README baseline) passed `gf-cargo-10` with the
  GF-11 question answered by the persona (replace-remote).
- Eval bug fixed on the way (`28121489`): a Bash `tool_result` carries string `content`; the
  decoder dropped the whole user event, the classifier merged the burst into the prior
  tool_use and graded a closing question "working" — the user-sim never fired. Every scenario
  where the agent asks right after a Bash call was affected.
- Farm traps: push `--evaluator` on any eval-code change; a scenario persona must answer every
  decision zcp hands back (GF-11), or the run ends on the question.

### 9.2 Day 3, evening — zcp commits nothing, seeds nothing; self-deploy contract (GF-12)

- Adopt's snapshot commit and the `.git/info/exclude` seeding are gone (`45a73741`): adopt
  without a content HEAD = init + empty marker, files stay uncommitted, `provenance=initialized`.
  `.gitignore` and the baseline commit are the agent's (guided), never zcp's. Corpus audit:
  no recipe/atom/guide requires a `.env` at build or runtime (Laravel/NestJS say so explicitly).
- GF-12 (`64366035`): a dev checkout is reproducible from git; the self-deploy boundary is
  zcli's git archiver (facts cited in spec §8 DM-7/DM-8 from zcli v1.1.0-13-g9827852: ignored
  files never ship, no flag changes that; `-g` copies `.git/` raw; submodules arrive empty;
  `deployFiles` is server-side; `zcli deploy -g` is a no-op). Self-deploy result carries
  `notCarried`, `envFiles`, `repoState`; `.git`-as-file and `.gitmodules` are refused with
  `GIT_WORKTREE_UNSUPPORTED` / `GIT_SUBMODULES_UNSUPPORTED`. Atom
  `develop-self-deploy-reproducibility`.
- Farm `gf-cargo-14` on the merged branch: G1, G2, G3, G4 (ignored file gone, `notCarried` +
  `envFiles` in the result), G5, G6-initialized — all passed.

### 9.3 Day 3, night — the process must lead, not the prompt

Review of the merged branch (Karel's four questions: what changed on cross-deploy, is
production safe, is the code sound, do agents make the commits) found the code green and
production untouched, but the guidance claim false. Verified on `gf-cargo-14` transcripts: G1
(60 steps), G6 (11 steps) and G4 (32 steps) contain no `git commit`, no `git add`, no
`.gitignore` write — and all three cells PASSED because the oracles never asked. Three causes,
all in code: `develop-self-deploy-reproducibility` was `modes: [dev, simple]` while a pair's
dev half is `Mode = standard`; it rendered only in develop-active, after bootstrap/adopt close
had already ended; no atom mentioned `sha=` at all (G3 passed only because its prompt said
"by its sha"). Owner rule restated: farm prompts must not lead; the process must.

Landed (five parallel builder slices, merged):
- Atoms: `bootstrap-close-baseline-commit` (adopt/recipe close), `develop-self-deploy-reproducibility`
  now includes `standard`, the three stage-promotion atoms say commit-before-promote, `sha=` for an
  exact commit, `appVersion=<id>` for rollback. Goldens regenerated.
- Deploy result: a dirty working-tree deploy to a CROSS target appends a `Warnings` entry naming
  HEAD and pointing at `sha=` (`ops.dirtyCrossDeployWarning`); never on self-deploy or the sha path.
- Scenarios: G1/G6 oracles require a non-robot commit, tracked `.gitignore`, no `.env`/`node_modules`
  in HEAD (G6's cargo file must now BE tracked by the agent's commit); G3 prompt de-led ("Ship what's
  on appdev to appstage. Afterwards tell me exactly which commit is running…") with
  `toolArg always zerops_deploy{targetService=appstage,sha~^[0-9a-f]+$}`.
- Launch + release push-proof: a meta without `TrackedRef` compares `HEAD` (pre-GF-7 behaviour),
  not the literal `main` (`tools.launchPushProofRef`) — regression for legacy metas on non-main
  default branches removed.
- `action=status`: repo + rollback blocks read concurrently (bounded 4) — one round trip of wall
  time instead of N.

Still open from the review (not fixed, by choice): self-deploy refusals (worktree/submodules) are
fail-open on a preflight transport error; the local-mode provision gate ("project root must be a
git repo") is live-unverified (farm is container-only); without a `.gitignore` the zcli archiver
ships `node_modules` on a self-deploy — the guidance fix above is the mitigation.

Farm `gf-guide-1` (candidate 93a1fe62, six G cells): G2, G3, G4, G5 passed — G3 with the de-led
prompt, the agent chose `sha=` unprompted. G1 and G6 failed, both instructive:
- G1: the PROCESS worked — the agent wrote `.gitignore`, committed the baseline "per guidance"
  before deploying, then read the dirty cross-deploy warning (`recorded: HEAD … + uncommitted`),
  committed the stray `package-lock.json` and redeployed clean. The oracle failed only because it
  demanded a non-robot AUTHOR, which a container cannot produce before git-push-setup (GLC-3: the
  ambient identity is the robot's). Oracles now prove CONTENT: non-empty HEAD tree (G1), tracked
  cargo file (G6), tracked `.gitignore`, no `.env`/`node_modules` in HEAD.
- G6: adopt auto-skips the close step (`validateSkip`, `IsAllExisting`), so a close-step atom never
  renders there. Split: `bootstrap-close-baseline-commit` stays for recipe close;
  `bootstrap-adopt-baseline-commit` renders at adopt's provision step (the repo is initialized when
  provision completes) and the adopt transition message carries one line pointing at
  `repo.provenance: initialized`. `existing` trees are left untouched (G2 guards that).

Farm `gf-guide-2` (candidate a671587f; G1, G2, G6): all three passed. G6's agent SSHed into
appdev after provision, found the tree uncommitted and `.env` unignored, wrote the `.gitignore`
rule and made the baseline commit over SSH; G2's agent confirmed pre-existing history and left it
untouched. Observer finding on both adopt runs (real): the guidance points at `services[].repo`,
which only `action=status` attaches — the provision-complete response's envelope (`freshEnvelope`)
carries no `repo` block, so the agents improvised with `git status`. Fix in flight: the adopt
provision-complete response attaches the same block (`attachRepoStatus`), wording says "this
response's envelope".
