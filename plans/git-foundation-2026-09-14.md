# Git as the foundation — plan

Status: SUPERSEDED as the entry point by `git-foundation-handoff-2026-09-14.md` (read that first; §3 there lists what this plan got wrong). Kept for the platform ledger (§1) and the eval tables (§6).

## 0. Thesis (one sentence)

Every deploy is a commit going to a target service; the transport follows from who
triggered it (interactive → push from the commit, git event → origin-driven); each
environment either accepts pushes or tracks a ref. Origin is needed only for sharing
and for unattended delivery, never for "having a prod".

## 1. Proven live (evidence: platform-verifier memory, `verified-facts.md`)

| # | Claim | Verdict | Consequence for the design |
|---|---|---|---|
| P1 | `zcli push --version-name <sha>` accepted; name visible only in ES `SearchAppVersions` (`name`), absent from direct GET DTOs; no sha/branch on any appVersion, incl. `source: GIT` | VERIFIED | Ledger of record lives in git refs; platform name is a hint. `ListServiceAppVersions` cannot read it. |
| P2 | Self-deploy replaces the container: `/var/www` = artifact; ignored files (`.env`, `node_modules`) gone. **Correction 2026-09-14 (Karel):** zcp's self-deploy already passes `zcli push -g` (GLC-2), which ships `.git/` inside the artifact, so the repo — refs/zcp/* included — travels with the deploy. | VERIFIED, consequence WITHDRAWN | Dev self-deploy stays (DM-2 as today: `deployFiles: [.]` + `-g`). Dev-only topology unchanged. P10 (verifier) pins exactly what survives a `-g` self-deploy. |
| P3 | `zcli push` from a git dir = ls-files + untracked-unignored, excludes ignored + `.git`; non-git dir needs `--no-git` | VERIFIED | Push-from-commit = `git archive <sha>` → tmp outside workdir → `zcli push --no-git --version-name <sha>`. Exact, reproducible. |
| P4 | `buildFromGit` accepts only github.com / gitlab.com hosts (Gitea + Codeberg fail 36 ms after a successful ls-remote; `internalServerError`) | REFUTED for Gitea | Platform pull (`track` via Zerops integration) is GitHub/GitLab only. Any other forge needs a zcp-owned relay. |
| P5 | Webhook receivers exist only as `github-webhook` / `gitlab-webhook`, bound via account OAuth (`githubAuthorizationRequired`); no generic HMAC trigger. Authenticated trigger: `PUT /service-stack/{id}/trigger-pipeline` (host-restricted) or `zcli push` | VERIFIED | Same as P4: relay is the universal path. |
| P6 | `POST /client/{clientId}/integration-token` mints a token scoped to `projects:[{projectId, roleCode}]`; regenerate/delete exist; NO expiry/TTL/one-time | VERIFIED | One-time delegation = mint scoped → use → delete, implemented in zcp. Relay token = scoped to the prod project only. |
| P7 | Gitea runs on Zerops as `ubuntu@24.04` + binary (needs HOME, app.ini, ≥1 GB RAM); no managed type, no recipe | VERIFIED | "Managed gitea" = a first-class recipe + zcp provisioning, not a platform type. |

## 2. Model

### 2.1 Vocabulary (topology/)

- `Repo` — always present on a dev service. Created by bootstrap (`git init` + scaffold
  commit), import (`git clone`), adopt (`git init` + baseline commit tagged with the
  running appVersion id; marker `source` | `artifact-only` from the appVersion's
  sourceService/deployFiles). zcp seeds `.git/info/exclude`, never `.gitignore`.
- `Origin` — `none` | `managed` (gitea recipe in the org's hub project; zcp owns repo,
  token, webhook) | `external` (GitHub / GitLab / any URL through a `GitHost` adapter
  with capabilities: `createRepo`, `token|deployKey`, `platformPull` (GH/GL only),
  `webhook`, `ci`).
- `Environment` — a service with `source: push | track(origin, ref)`.
  - `push`: receives interactive deploys. Default for stage; for prod when origin is
    `none`.
  - `track`: every push/merge of `ref` deploys. Default for prod once origin exists;
    optional for stage.
- `Ledger` — `refs/zcp/env/<service>` (what runs where) + `refs/zcp/deploy/<n>`
  (sha ↔ appVersion id ↔ target). `versionName = sha` as the platform-side hint.
  `refs/zcp/*` are never pruned by mate.
- `Trigger` — `interactive` (agent/human in dev context or laptop) | `git-event`.

### 2.2 Transports

| Trigger | Target | Transport |
|---|---|---|
| interactive | stage (same project) | `git archive sha` → tmp → `zcli push --no-git --version-name sha` |
| interactive | prod (other project) | same, with a one-time delegation: mint project-scoped token (P6) → push → delete. Agent never sees it. |
| git-event, origin GH/GL | any | platform pull (`track` via Zerops integration) — zero zcp runtime |
| git-event, origin managed/other | any | **deploy relay**: a small zcp process in the target project holding a token scoped to that project (P6); receives the forge webhook, `git archive sha` from origin, `zcli push`. One mechanism for every forge. |

Rollback = platform redeploy of the appVersion from the ledger; rebuild from sha is the
fallback.

### 2.3 Close

Close = commit (always, on `refs/zcp/wip` if the tree is dirty; `main` moves only by an
explicit close/promote with one ordinary commit) + push to origin (if any) + deploy to
every `push` environment the user marked as auto. Two switches replace close-mode ×
GitPushState × BuildIntegration.

### 2.4 Situations

| Situation | Repo | Origin | Prod |
|---|---|---|---|
| new, solo | init | none | push + delegation |
| new + managed gitea | init | managed | track via relay |
| new + own GH/GL | init | external (platformPull) | track via Zerops integration |
| new + own Gitea/other | init | external | track via relay |
| import from repo | clone (user token) | external | track |
| adopt, no git | init + baseline | none | push + delegation |
| adopt, buildFromGit | clone discovered repo | external | track |
| laptop local mode | laptop is the tree | origin = how laptop meets container | push from laptop |
| multi-runtime | repo per runtime; monorepo for imports | N tracked refs (managed automates) | per repo |
| mate1 + mate2 | one repo, two dev services | REQUIRED (the only thing that forces it) | — |

## 3. What changes in zcp (from the change inventory)

### 3.1 Delete
- `GitPushState`, `DeliveryState`, `DeriveDeliveryState` (`topology/delivery*.go`, ~250 lines + tests).
- `CloseDeployMode` delivery derivation (`workflow/deploy_intent.go`, `build_plan.go` parts).
- `BuildIntegration` + `workflow_build_integration.go` (807) — folded into `GitHost` capabilities.
- `workflow_record_deploy.go` (267) + work-session deploy history in `work_session.go`, `compute_envelope.go` — the ledger replaces them. `record-deploy` survives only as a ledger-append for platform-pull builds (P1: no sha on the DTO) until the relay covers all forges.
- `deploy_git_push.go` (706) `strategy=git-push` — delivery is never a deploy strategy.
- Atoms: 11 close-mode + 2 build-integration + `develop-record-external-deploy` + `develop-git-push-*` (4) → rewritten as `origin-*`, `environment-source-*`, `deploy-from-commit`.

### 3.2 Add
- `topology/`: `Origin`, `OriginKind`, `EnvSource`, `GitHostCapabilities`, `Ledger` types.
- `ops/git/`: repo init/clone/baseline, exclude seeding, wip commit, archive-to-tmp push, ledger read/write (`refs/zcp/*`), `GitHost` adapters (`github`, `gitlab`, `gitea`, `generic`; `gh`/`glab` CLI where present — same shape T3 uses).
- `ops/delegation.go`: mint scoped integration token → use → delete (P6), with a stamped audit entry and a leak guard (token never crosses response/state).
- `cmd/zcp relay`: the deploy relay (webhook receiver, HMAC per forge, archive + push). Deployed as a recipe into the target project by `launch-production`. Gated like every prod credential: token scoped to that one project.
- Recipe `gitea` (ubuntu@24.04 + binary, HOME, app.ini, 1 GB) + `ops/gitea/` provisioning (admin, org, repo, token, webhook) — this is `managed`.
- Tools: `zerops_workflow action=origin` (set/none/managed/external), `action=env-source`, `zerops_deploy` gains `sha` (defaults to HEAD/wip), loses `strategy`. `launch-production` becomes: create prod project → choose prod source (push+delegation | track) → deploy relay if tracking a non-GH/GL origin.

### 3.3 Rewrite
- `spec-workflows.md` §4.2–4.4 (close), §8 DM (DM-2 → "dev never self-deploys", P2), §9 export (N repos), §10 launch (transports, delegation, relay), §1133 git lifecycle.
- `spec-work-session.md` §7.1, §9 (deploy history → ledger).
- `spec-mate.md` §6.3 (stacked actions re-enabled through zcp), §6.4 (`refs/zcp/*` prune exemption).
- `spec-scenarios.md` + eval scenarios: `git-push-setup-then-actions`, `delivery-git-push-actions-setup`, `launch-*` (6), `close-mode-git-push-setup` fixtures.
- CLAUDE.md traps: replace the `GitPushState`/close-mode line and DM-2 line.

### 3.4 Untouched
- Recipes' `.import.yml` (no git-delivery fields exist). Platform layer except new endpoints (`integration-token`, `trigger-pipeline`).

## 4. Slices (each green + landable alone)

1. **Ledger + push-from-commit** — `refs/zcp/*`, `versionName=sha`, `zerops_deploy sha=`, stage only. Deletes nothing yet. Tests: ledger round-trip, archive push, P3 semantics.
2. **Repo-always** — GLC-1 already inits at bootstrap/adopt; this slice adds exclude seeding, adopt baseline tag + provenance, `repo:` envelope block. (2b dropped, see §6.3.)
3. **Origin + GitHost adapters** — replaces `GitPushState`/`git-push-setup`; GH/GL adapters first (port T3's `gh`/`glab` shape), `generic` third.
4. **Env source + close switches** — replaces close-mode derivation + `BuildIntegration`; `track` for GH/GL via Zerops integration.
5. **Delegation** — prod push with mint→use→delete; `launch-production` push mode without any origin.
6. **Relay** — `zcp relay` + recipe; `track` for every other forge. Deletes `record-deploy`.
7. **Managed gitea** — recipe + provisioning + hub-project discovery. (Depends on 6.)
8. **Mate** — re-enable commit/push/PR through zcp, prune exemption, ledger-aware checkpoints.

Slices 1–2 are safe to land now and change no user-visible flow. 3–4 are the breaking
migration (meta migration: `GitPushState=configured` → `origin: external`, close-mode →
env sources). 5–7 are the product bet.

## 5. Decisions for the owner

- **P-1 Relay in the prod project vs. in the hub project.** Prod-local means one token
  scoped to one project and no cross-project trust; hub means one relay serves all
  projects with a multi-project token. Recommendation: prod-local (matches P-LP-14/15).
- **P-2 Platform asks** (not blockers, but they shrink slices 5–6): token TTL/one-time on
  `integration-token`; `buildFromGit` host allowlist opened, or a generic HMAC build
  trigger; commit sha on the appVersion DTO. With all three, the relay disappears for
  every forge and delegation needs no delete step.
- **P-3 Managed gitea placement.** Org hub project with subdomain (cross-project reach)
  vs. per-project. Recommendation: hub, provisioned once per org.

## 6. Eval scenarios (branch `feat/git-foundation`)

Farm mechanics: scenarios are pushed as their own content-addressed tree
(`farm push --scenarios`), independent of the candidate. The branch pushes its own
scenario digest + candidate; `main` batches keep running main's digest. Gate-set
edits live in the branch (`gate-set.txt` ⇔ spec §9.3, pinned by
`TestEvalMatrix_GateSetMatchesSpec_ExactIDs`).

### 6.1 New cells (G = git foundation)

| cell | scenario id | pre-state · task | oracle families | lands with |
|---|---|---|---|---|
| G1 | `repo-always-bootstrap` | brand-new classic pair; "build me a small API" | containerCheck(`git -C /var/www rev-parse HEAD`; `git log --oneline` ≥1; `.git/info/exclude` non-empty; no zcp-written `.gitignore`) expectedServices never | slice 2 |
| G2 | `repo-always-adopt-baseline` | unmanaged pair deployed without git; "connect to what I have" | containerCheck(`git tag -l 'zcp/baseline/*'` = running appVersion id) meta(repo.baseline) unchanged never | slice 2 |
| G3 | `deploy-from-commit-stage` | pair, repo present; "ship this to stage" | toolArg(zerops_deploy targetService=appstage) containerCheck(`git rev-parse refs/zcp/env/appstage` == HEAD) toolResult(versionName==sha) liveness never | slice 1 |
| G4 | `dev-never-self-deploys` | pair; "deploy my dev service" | toolArg(max 0 zerops_deploy target=appdev source=appdev) containerCheck(`.git` present after) liveness(dev server) mustOffer(stage) never | slice 2b |
| G5 | `rollback-stage-from-ledger` | stage with 2 ledger entries; "roll stage back to the previous version" | artifactPromotion-shaped redeploy of prior appVersion; containerCheck(env ref moved back) mustOffer(no rebuild) never | slice 1 |
| G6 | `origin-github-track-main` | pair + user's GitHub PAT; "connect my repo, stage follows main" | meta(origin.kind=external, source=track) toolArg(no deploy after push) askWhen(GIT_TOKEN_MISSING) never | slice 3–4 |
| G7 | `launch-prod-no-origin-delegation` | pair, origin none; "launch production" | launchShape noFabricatedSecret toolArg(max 0 GIT_TOKEN ask) meta(prod.source=push) never | slice 5 |
| G8 | `origin-gitea-track-via-relay` | pair + Gitea URL; "connect and auto-deploy stage on push" | meta(origin external, relay present) containerCheck(relay service) never | slice 6 |
| G9 | `origin-managed-gitea` | brand-new, hub project; "give me a shared repo" | expectedServices(gitea) meta(origin managed) never | slice 7 |

### 6.2 Existing gate cells — impact

| cell | scenario | verdict | when |
|---|---|---|---|
| C1 | `git-push-setup-then-actions` | DELETE → G6 | slice 3–4 |
| C5 | `export-buildfromgit-self-snapshot` | REWRITE (repo-always premise; no git-push-setup ordering) | slice 3 |
| C2, C3 | `launch-production-*` | ORACLE-ONLY until slice 5, then REWRITE → G7 shape | slice 5 |
| D3 | `launch-failure-build-stuck` | REWRITE (rationale keyed to CD-after-launch) | slice 5 |
| D6, B14 | preseed scaffolds plant `gitPushState`/`buildIntegration` | ORACLE-ONLY: preseed JSON fields renamed | slice 3–4 |
| B6 | `mount-edit-deploy` "ship dev only" | REWRITE: dev = dev server, ship = stage | slice 2b |
| B8 | `cross-deploy-stage-promote-from-dev` | superseded by G3/G5 (dev holds no artifact) | slice 2b |
| A1 + every dev-only cell | dev-only topology self-deploys today | depends on the P2b verification (§6.3) | slice 2b |
| rest (22) | UNAFFECTED | — |

Non-gate: `delivery-git-push-actions-setup` DELETE; `launch-production-{existing-with-webhook,new-project-push-mode,existing-project-token}` + `launch-with-existing-cicd` REWRITE at slice 5; `eval_scenario_drift_test.go` guards for `CICDMethod`/`closeMode=git-push`/`.netrc` rewritten at slice 3–4.

### 6.3 P2b — REFUTED, and moot (live 2026-09-14, probes gitv8n/u/s)

A self-deploy creates a new container on a new ZFS root: `/home/zerops` is reset
too. But the container's own self-deploy ships `.git/` via `zcli push -g` (GLC-2),
so the repo survives INSIDE the artifact. Slice 2b (absolute dev gate) is
**dropped**; B6/A1/B8 and the dev-only cells stay as they are; G4 becomes
`dev-self-deploy-keeps-repo` (containerCheck: refs/zcp/* + exclude present after a
dev self-deploy). Existing git lifecycle rules GLC-1..6 (spec §11.33) stay the
owner of init/identity/HEAD; slice 2a is consolidated onto them.

Two more facts: P8 — zcli has NO tarball input (`--archive-file-path` is an output
tee joined onto the workdir); push-from-commit = `git archive sha | tar -x -C tmp` +
`zcli push --working-dir tmp --no-git --version-name sha`. P9 — zcli default
`--workspace-state all` exits 128 on a `gitdir:` pointer-file `.git` (temp index
inside `.git`); `clean` works. git + zcli present on nodejs@22, ubuntu@24.04, static.
