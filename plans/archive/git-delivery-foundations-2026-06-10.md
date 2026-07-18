# Git & Delivery Foundations — the ground-up model (2026-06-10)

**Why this exists:** Karel's verdict on the first remediation plan ("to jsou záplaty, ne
fundamentální věci — chybí mentální modely, matice, rozpad na všechny možnosti, z toho vydestilované
scénáře"). This document is the foundation: the verified model of how filesystems, git state, and
credentials actually behave across every ZCP delivery path, the divergence sites, the scenario
catalog derived from the matrices, and the fix program that follows FROM the model. Built from a
3-agent deep map (git lifecycle, credentials, platform semantics — all claims file:line-cited),
live verification on eval-zcp (`zcli push --help`, container inspection, `zcp version`), and the
prod.txt session replay. Supersedes the phase framing of
`plans/archive/launch-prod-stage-ha-cicd-2026-06-10.md` (kept as the gap-evidence record).

---

## 1. The model — three laws

**LAW 1 — Filesystem lifecycle.** RESTART = same container, disk fully preserved (incl. /var/www
and home dir); env.json re-read at boot. DEPLOY / horizontal scale-up / failure-replacement = NEW
container whose /var/www is repopulated SOLELY from the stored build artifact. Env/userData
(project env + service secrets like GIT_TOKEN) are platform-stored and zembed-injected at every
boot — appVersion-independent, survive everything. Home-dir state (~/.netrc, gh config) survives
restart, dies on deploy. (zerops-docs object-storage-integration.mdx:143, pipeline.mdx:327-331;
spec-zerops-env-lifecycle.md:116-128; live-confirmed by the prod.txt timeline: env-set restart at
17:23 preserved .git, the CI deploy at 17:25 wiped it.)

**LAW 2 — What enters the artifact is decided at zcli pack time.** zcli is git-aware by default and
EXCLUDES .git unless `-g/--deploy-git-folder`; `--no-git` uploads the tree as-is; deployFiles then
filters the BUILD output (no built-in dotfile exclusion; .deployignore is the only user filter).
Therefore `.git` on a runtime container exists only when the deploying path packed it:

| Path | zcli shape | .git on new container | identity | origin |
|---|---|---|---|---|
| ZCP ssh SELF-deploy (source==target) | `commit -m 'deploy'` + `zcli push … -g` | YES (artifact) | agent@zerops.io re-asserted | whatever source .git had |
| ZCP CROSS-deploy dev→stage | no `-g` | NO (by design — stage is never git-read) | — | — |
| ZCP local-mode deploy | `--no-git` always | NO (GLC-6, user's git stays local) | — | — |
| `strategy=git-push` | push only, no artifact | container unchanged; BUILD that follows is one of the other rows | | |
| platform buildFromGit / webhook rebuild | platform clone+build | EMPIRICALLY yes (B13 evidence: clone identity, template origin) — UNDOCUMENTED | clone's | clone URL |
| **ZCP-emitted GitHub Actions** | `zcli push --service-id … --setup …` — **no `-g`** | **NO** | — | — |

The rule ZCP itself encodes: `includeGit := sourceService == targetService` (deploy_ssh.go:103,
".git must stay" for self-deploys). The emitted Actions template ignores this rule — same YAML for
every topology. For dev/stage pairs that is CORRECT (CI targets the stage half = cross-deploy
semantics; ZCP never reads git from stage). For simple/single/standalone-dev — where the CI build
target IS the push source — it is the **root cause of the prod.txt git wipe** and of the
degradation spiral: .git wiped → gate `remote-mismatch (live="")` → recovery git-push-setup
re-inits an EMPTY repo → `head-not-pushed` with LocalHead="" → `strategy=git-push` refuses (no
committed code) → agent hand-commits the artifact tree → divergent root commit. One missing flag,
four misleading diagnostics. (GLC invariants: spec-workflows.md:1043-1050; GLC-2 spec text is STALE
vs code — config moved outside the OR branch in the B13 fix.)

**LAW 3 — Every credential has exactly one validated home; copies divergence silently.** The
journey involves 9 credentials; 7 sites hold the same logical credential in 2+ stores with no
reconciliation:

| # | Credential | Validated home | Divergeable copy | Reconciliation today |
|---|---|---|---|---|
| 1 | GIT_TOKEN | service-scope SECRET env (probe-first write) | container SHELL copy (live only post-restart) | XCUT-2 restart poll; `test -n` preflight checks PRESENCE not value |
| 2 | GIT_TOKEN | service env | **gh hosts.yml on zcp container** | **NONE — `gh auth status \|\| login` is write-once; no identity/scope check ever** |
| 3 | GIT_TOKEN | service env | ~/.netrc residue (trap fails open on SIGKILL/drop; survives restart) | overwritten on next ZCP git op; shadows MANUAL git on the container |
| 4 | ZCP_API_KEY | control-container env / .mcp.json | ZEROPS_TOKEN GitHub secret (verbatim copy) | NONE — validated only by the first CI run |
| 5 | meta.RemoteURL | state dir | live /var/www/.git/config origin | ACTIVELY CHECKED (gate hard-block, BI drift warn, export refresh) — the one done right |
| 6 | GitPushState=configured | meta | PAT's live validity at GitHub | failure-driven only (degrade-to-broken after a push fails) |
| 7 | platform GitHub OAuth grant | per-clientUser, platform side | — | launch-created machine clientUser PROVABLY has no grant (spike B.0) |

Row 7 yields the **private-repo landmine**: ZCP's own reads (probe, push-proof ls-remote) all
authenticate via GIT_TOKEN, so a private repo passes EVERY gate; the production project's
buildFromGit clone then has NO credential path (machine clientUser, no OAuth grant, credential-free
URL by design) and fails in the known clone-preflight shape — terminal FAILED in ~0.3 s, no build
container, **no logs** — after the user already minted the key and the project was created. Private
repos are exactly what the fine-grained-PAT collection model encourages. Documented nowhere
(zerops-docs has zero private-repo mentions for buildFromGit; no ZCP atom/gate covers visibility).

**Rotation truth (completes Law 3):** GIT_TOKEN has NO proactive rotation path — the O3
short-circuit (`configured && same canonical remote` → "already-configured") sits BEFORE the
gitToken check and silently ignores a freshly supplied token; rotation unlocks only after the old
token fails a push (degrade-to-broken) or by lying about the remote. The raw `zerops_env set`
fallback echoes the PAT verbatim in `stored[]` (RedactCredentialValue is wired into get/discover
only) and the export-publish-needs-setup atom still instructs the worst combination (project-scope
raw set + "confirm does not verify" — both false post-F5).

---

## 2. Where the gate's evidence model meets the laws

The launch source-control gate (P-LP-10/11) reads: recorded meta (GitPushState, RemoteURL) + live
origin + porcelain + local HEAD + authenticated remote HEAD — all on the PUSH hostname. Under the
laws this means:

- **Pairs (standard/local-stage):** push host = dev half; dev-half deploys are self-deploys (-g) so
  git state persists; CI targets stage and never touches it. The gate's evidence model is COHERENT
  here. This is the structurally safe topology — an argument for the stage recommendation that is
  now grounded in the model, not just in "verify first" hygiene.
- **Self-targets (simple/single):** any non-ZCP build path that omits -g destroys the gate's
  evidence substrate. Fix at the SOURCE of artifacts (template parity), not by softening the gate.
- **`fatal: not a git repository` reads as `remote-mismatch live=""`** — the `|| true` readers
  cannot distinguish absent-git from drifted-remote; the honest distinct state is missing.
- **After every ZCP self-deploy, LocalHead is a never-pushed `deploy` commit** → `head-not-pushed`
  is the EXPECTED state until a `strategy=git-push`; the natural pre-launch sequence is therefore
  always edit → … → git-push → launch. (By design — prod builds pushed code — but the guidance
  should say so instead of letting the blocker be the teacher.)
- **Artifact-built trees can show untracked build outputs** → `dev-tree-dirty` on a container that
  was never hand-edited. Known exposure, same class: the gate's evidence assumes a
  working-tree-like /var/www; artifacts are not working trees.

---

## 3. Scenario catalog (derived from the matrices)

| ID | Scenario | Status under the model | What must hold |
|---|---|---|---|
| S1 | dev/stage pair → prod; Actions CI (push→stage), tag→prod | SAFE topology; tag→prod half missing (was F7 silent cut) | template stays no-g for stage job; prod job added at launch |
| S2 | simple/single → prod; Actions CI | BROKEN today (CI wipes push-source .git) | template adds `-g` + `persist-credentials: false` when build target == push source; DM-2 already forces deployFiles [.] |
| S3 | local mode → prod | coherent (user-owned git, no container git) | no change; gate reads CWD |
| S4 | token lifecycle: collect → rotate → revoke | rotation path MISSING; gh copy write-once; env-set echoes secrets | O3 treats non-empty gitToken as rotation intent; emitted gh command asserts identity (`gh auth token` vs $GIT_TOKEN, or logout+login); redact set-path echo |
| S5 | private repo → prod (buildFromGit) | UNDIAGNOSABLE failure after key minting | new pre-publish visibility check: unauthenticated `git ls-remote` (no .netrc) vs authenticated — private → blocker BEFORE ready-to-launch with explicit options (decision #5 below) |
| S6 | recovery of a git-less container (historic CI builds, non-ZCP CI) | improvised by agents today | git-state-missing blocker + handler-side reconstruction in git-push-setup (init+remote+fetch+reset, refuse on divergent tree) |
| S7 | webhook/GUI CD variants (stage push-webhook, prod TAG-webhook) | works (Path B) | unchanged; remains the supported alternative |
| S8 | dev-only source entering launch | no stage question; dev setup promoted silently; ModeDev deadlock | the consent layer (stage recommendation, setup provenance, LP-5) from the gap plan |
| S9 | production scope & HA | whole-project bundling; HA fait accompli | the profile layer (reference-scoped deps, HA/container consent, readiness checklist) from the gap plan |

---

## 4. Fix program (replaces the prior plan's phase list)

Ordered so that TRUTH fixes (the model holding) land before the CONSENT layer (asking the right
questions) and the TOPOLOGY layer (tag→prod).

**FP-1 — Artifact & git parity [S].** Actions template: `-g` + `persist-credentials: false` when
anticipatedBuildTarget == push source (mirror of `includeGit`, single owner — derive both from one
predicate); single-setup zeropsio/actions variant cannot express -g → drop it for self-targets.
Gate: distinct `git-state-missing` blocker (absent .git ≠ remote-mismatch). git-push-setup:
handler-side reconstruction for recorded-configured pairs with missing .git (S6). Spec: GLC table
gains the artifact law + the GLC-2 stale text fix.

**FP-2 — Credential truth [M].** Rotation: O3 short-circuit keys on (remote, token-present) —
non-empty gitToken = rotation intent → full probe/write/restart. gh identity assertion in the
emitted setupCommand (compare `gh auth token` output to $GIT_TOKEN; mismatch → logout+login) — the
hosts.yml write-once hazard dies. `zerops_env set` response redacts credentialValueKeys in
`stored[]`. Atom/doc drift sweep: export-publish-needs-setup (project-scope set + "does not
verify" claims), ops/env.go GitTokenEnvKey comment, ghAuthFailureSymptom gains the wrong-identity
403 case. ZEROPS_TOKEN divergence: note at minimum (rotation link = backlog candidate).

**FP-3 — Private-repo visibility gate [S+live].** Pre-publish check (S5): unauthenticated
ls-remote on the push host; private + new-project path → blocker BEFORE the launchKey ask, with the
option set per decision #5. Live e2e on eval-zcp with a private test repo (the failure shape is
already characterized — verify the gate catches it pre-key).

**FP-4 — Consent & scope layer [L].** The gap plan's P1+P2 unchanged in substance, now
model-grounded: stage recommendation for no-stage sources (S8 — justified by §2: pairs are the
structurally safe topology), setup provenance + derived prod block, verified-evidence read,
production profile (reference-scoped managed deps + orphan-env exclude recommendation, HA consent,
container-count consent with HA-readiness checklist), bundle preview with scaling, prod-ops scale,
LP-5 mode-unsupported.

**FP-5 — Tag→prod Actions + CD declaration [L].** The gap plan's P3: second workflow file
(`on: push: tags`) emitted at launch with concrete prod service IDs + ZEROPS_TOKEN_PROD secret
instructions; build-integration tells the two-track story up front; webhook path unchanged (S7).
Template rule from FP-1 applies (prod services are never git-read by ZCP → no -g in the prod job).

**FP-6 — Eval round-trip [M].** prod.txt replay scenario (S2+S4+S9 combined), pair happy path
(S1), private-repo gate (S5), reconstruction (S6), rotation (S4), plus the gap plan's consent
scenarios. Each FP lands green + its scenario before the next.

Estimate: FP-1..3 ≈ 3–4 d; FP-4 ≈ 4 d; FP-5 ≈ 3 d; FP-6 ≈ 2 d. Total ≈ 12–13 d.

## 5. Decisions for Karel

1. **FP-1 self-target `-g`**: confirm shipping .git into self-target artifacts via CI (matches
   ZCP's own self-deploy; persist-credentials:false handles the checkout token; shallow .git is
   gate-safe — verified command-by-command).
2. **gh identity assertion shape** (FP-2): compare-and-relogin (recommended; keeps gh as executor)
   vs always logout+login (simpler, destroys any user-managed gh state on the container).
3. **Stage recommendation strength** (FP-4): dismissible warn+ack (recommended) vs hard gate —
   unchanged question from the gap plan, now with the §2 structural argument for stage.
4. **Container-count consent** (FP-4): allow consented minContainers=1 with warn (recommended) vs
   hard floor 2 — unchanged question.
5. **Private-repo option set** (FP-3, NEW): when detected, offer (a) make repo public (temporary or
   permanent) + proceed, (b) existing-project path where the user pre-creates the project and
   OAuth-connects it in the dashboard before launch, (c) abort with explanation. Embedding the PAT
   in the buildFromGit URL is REJECTED (lands the secret in import history + dashboard).
   Which do we recommend first?
6. **Unreferenced managed deps default** (FP-4): exclude-by-default with opt-in (recommended) vs
   include-by-default — unchanged question.
7. **zerops-docs upstream notes** (FYI, not ZCP scope): buildFromGit private-repo behavior +
   startWithoutCode undocumented; pipeline.mdx:405 flag mislabel; `--no-git` .git semantics
   unstated. Worth a docs issue.

## 6. Evidence index

Full maps with file:line for every claim: workflow run `wf_0dcb4135-15f` (3 agents, 169 tool uses)
— git lifecycle (GLC-1..6, truth table, Actions parity analysis incl. shallow-clone safety),
credentials (9-credential matrix, 7 divergence sites, .netrc lifecycle, private-repo absence
proof), platform semantics (restart/deploy law, SSHFS reconnect drift init vs tool path, zembed env
injection). Prior gap analysis: workflow `wf_366eb671-849` (10 agents) in
`plans/archive/launch-prod-stage-ha-cicd-2026-06-10.md`. Live: eval-zcp weather container inspection,
`zcli push --help` (v9.112.1-25-g20b5737b container build), prod.txt timeline (15:16–17:35).
