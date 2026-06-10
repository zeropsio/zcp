# Production Transition — final design (2026-06-10)

**Supersedes** the "Launch owns the journey" v2 synthesis (deleted) per Karel's correction: the
surfaces (git-push-setup, build-integration, export) have legitimate STANDALONE value with zero
production intent — someone wires GitHub purely for development; someone exports purely to get a
recipe-repo for later import. Production is a THIRD thing started later. The transition must not
absorb the flows — it must **read the recorded meta, verify the setups, and deliver absolutely clear
instructions** for whatever is missing. `plans/production-pipeline-impl-2026-06-09.md` Phases 1–3
remain valid build steps and are folded in below.

**Evidence base:** 6 exhaustive communication maps (develop+close-mode, git-push-setup,
build-integration, export, launch-production, servicemeta-ledger — every response shape, every meta
read/write, file:line), my own full read of workflow_git_push_setup.go + launch dispatch + launch
atoms + setup_resolver, the 8-agent credential/prod-management study, the green-field panel + 2×
Codex. Key surprises the maps produced:

- `deriveProdSetupBlock` EXISTS and is CALLED (workflow_launch_production.go:894) — the handler
  already derives a concrete prod setup proposal from the live source yaml; but the
  `launch-write-prod-setup` ATOM hands the agent an empty placeholder template. The handler is
  smarter than its own tell.
- `runReadinessRubric` (launch_readiness.go:62) is implemented + tested and has ZERO production
  callers — the empty-consent ready-to-launch is an unwired feature, not a missing one.
- The atom corpus contradicts the protocol: `launch-intro` + `launch-mutation-key-required` name a
  `publish` action that does not exist; `launch-scope-prompt` documents an `envOverrides` input with
  no WorkflowInput counterpart (SDK rejects it); the dev-tree-dirty blocker and its atom give
  CONTRADICTORY instructions (deploy-commits-for-you vs commit-is-yours).
- Read failures masquerade as state failures: an SSH outage renders as `remote-mismatch live=""` —
  the agent gets "fix your remote" instructions for a network problem.
- Promotables-only multi-runtime launch passes scope+classify then hard-fails on a missing
  TargetService requirement downstream — the multi-runtime contract is self-contradictory.
- The ledger has three holes for the transition: no ProdSetupName record ("prod" literal fallback at
  3 deferred-§P5 sites), no durable deploy-VERIFIED-setup evidence (work-session Deploys/Verifies die
  at close; FirstDeployedAt has no setup/commit identity), no post-launch back-reference (source meta
  never learns prodProjectID/launchedAt — develop can never say "this stage feeds production").
- BI noop re-call irrecoverably drops the setup instructions (workflow file, secrets, gh commands) —
  post-compaction the agent cannot re-fetch them.
- git-push-setup walkthrough is state-blind (hardcodes GitPushState:unconfigured), and export
  re-derives setup names heuristically instead of reading the recorded
  PrimarySetupName/StageSetupName (parallel-path).

---

## The model — three standalone tools + one transition; the meta ledger is the channel

```
develop ──┐
          │  writes proven facts:        ┌──────────────────────────────┐
git-push ─┼─► ServiceMeta ledger ───────►│ launch-production            │
setup     │  GitPushState, RemoteURL,    │ = TRANSITION INSTRUCTOR      │
          │  BuildIntegration(+earned),  │ 1 reads ledger               │
build-    │  Prim/Stage/ProdSetupName,   │ 2 live-verifies what it can  │
integr. ──┤  verified-setup evidence,    │ 3 emits ONE complete         │
          │  FirstDeployedAt             │   gap-report with prefilled  │
export ───┘  (standalone recipe-repo;    │   instructions per gap       │
             shares bundle machinery,    │ 4 informed-consent preview   │
             no project block)           │ 5 creates the SIDE project   │
                                         │   (SERIOUS|LIGHT + region,   │
                                         │    read-back verified)       │
                                         │ 6 bring-up management window │
                                         │ 7 done → revoke key          │
                                         └──────────────────────────────┘
```

Launch NEVER calls the other surfaces internally. It instructs (prefilled executable calls + typed
credentialsRequired blocks with wait-for-user discipline) and re-verifies the outcome from
ledger + live reads on the next call. The surfaces stay independently usable and ignorant of
production; the ledger is how they communicate.

## Pillar A — fix the standalone surfaces (also for their own sake)

- **git-push-setup:** state-aware walkthrough (read the real meta, show "already configured for
  <url>" instead of the full PAT collection); service-level GIT_TOKEN (Karel-locked; also a security
  fix — the project `sensitive` flag does not persist, service-level SECRET masks); keep probe-first
  + check-before-mutate exactly as is (verified solid).
- **build-integration:** earned state (declared → VerifiedAt; actions = workflow-file-at-pushed-HEAD,
  webhook = platform integration read); **noop re-call returns the FULL instruction set** (stateless
  recompute — compaction recovery); structured Recovery on the needsGitPushSetup bounce; live
  RemoteURL drift check before emitting owner/repo commands; reconcile the two contradictory PAT-scope
  claims on the same surface.
- **export:** reads recorded setup names from the ledger (kills the heuristic suffix cascade
  parallel-path); compose-ready atom + spec §9.1 row (the recipe-repo terminal Karel described is the
  one status with NO authored guidance today); fix the three wrong claims in
  export-publish-needs-setup; project.name override for the recipe-repo re-import case.

## Pillar B — complete the ledger (three missing records)

1. **ProdSetupName** on ServiceMeta — finish deferred §P5; the "prod" literal fallback at 3 sites
   dies; scope-prompt asks/derives once, the record is the cascade's first hit thereafter.
2. **Durable verified-setup evidence** — extract at session close (and deploy/verify time) a small
   per-setup record: setup name + commit SHA/appVersion + verifiedAt. Survives work-session deletion.
   Launch can finally ask "was the stage setup ever green-verified, on which commit" — Karel's
   "all setups are verifiable, written in the meta information" made real.
3. **Post-launch back-reference** — after a successful launch, write prodProjectID + prodHostname +
   launchedAt (NON-secret) onto the source pair's record. Develop status can then say "appstage feeds
   production project X"; a second machine at least knows the production exists.

## Pillar C — launch as the transition instructor

- **One complete gap-report** on entry (not a bounce-per-blocker): all gaps at once, each with a
  prefilled executable call; user-owned inputs (repoUrl, PAT, launchKey) as typed
  credentialsRequired blocks with the ask-and-WAIT contract (kills fabrication).
- **Read-failure ≠ state-failure:** SSH/exec errors render as "could not verify X (ssh failed: …) —
  retry/diagnose", never as remote-mismatch/head-not-pushed state instructions.
- **Prod setup tell==check:** the atom aligns with deriveProdSetupBlock — the agent receives the
  DERIVED concrete block (from the deploy-verified stage setup), never an empty placeholder template.
- **Informed consent:** wire runReadinessRubric (the existing orphan) + bundle preview (services, HA
  promotions, corePackage, region, env classifications, warnings) into ready-to-launch BEFORE the
  launchKey ask.
- **Mutation:** project.corePackage (default SERIOUS, LIGHT override with recommendation) +
  project.location (live 3-region menu: eu-central, us-east-1, us-west-1; embedded schema refresh —
  us-west-1 is missing today) + post-create READ-BACK (A.10 silent-drop precedent).
- **Fix the multi-runtime contract:** Promotables-only path either works end-to-end or scope-prompt
  requires targetService — no silent downstream hard-fail.
- **Bring-up management window** (`bringup-manageable`): prodOps actions — status, logs (GetProjectLog
  → existing LogFetcher; feed failures through ops.ClassifyDeployFailure), env keys, restart/stop/
  start, scale, delete-service (confirmDestructive), delete-project — launchKey per call, never
  persisted (P-LP-1 keeps), admin client constructed + Closed per call. GrantSelfRole(ADMIN) becomes
  a hard prerequisite of the window. Actions-CD prod secrets are wired HERE (post-create — a
  prod-capable ZEROPS_TOKEN cannot exist before the prod project does).
- **Done boundary** (`done-ready-to-revoke`): prod exists + read-back matches + imports terminal +
  CD verified (platform integration read / workflow-file earned) or explicitly opted-out → stamp the
  DONE snapshot (the facts that become unreadable after revoke) → only now recommend key revocation
  (P-LP-4 rebuilt; the J5 revoke-vs-recall contradiction dies).

## Pillar D — protocol consistency (tell == check across the corpus)

Fix in one sweep, pin with lint where a class exists: the phantom `publish` action (2 atoms), the
phantom `envOverrides` input, the dev-tree-dirty contradiction (decide ONE owner of the commit step —
the deploy handler's auto-commit is the actual behavior; the atom yields), customDomain per Karel's
hard-delete decision, the `launch-pipeline-configure-dashboard` hardcoded "prod" vs cascade setupName,
the source-drift refusal pointing at manual state-file deletion instead of action="reset".

---

## Build order (each phase green + flow-eval; live tests on eval-zcp)

| # | Phase | Size |
|---|---|---|
| F0 | Security fix-now: P-LP-2 pin tightening (factory-var laundering), bundle-leak filter (IsClassifyInfrastructure in serviceSecretsToBundleEnvs — leaks TODAY), PAT rotation (Karel) | S |
| F1 | Earned BuildIntegration (locked Phase 1) + BI noop full recompute + needsGitPushSetup Recovery | M |
| F2 | Protocol consistency sweep (Pillar D) + prod-setup tell==check (atom ← deriveProdSetupBlock) + read-vs-state failure split in the gate | M |
| F3 | SERIOUS + region (locked Phase 3): corePackage/location emit, schema-sync (us-west-1), live read-back matrix 3 regions × SERIOUS + LIGHT override (Karel authorized billable) | S+live |
| F4 | Ledger completion: ProdSetupName (§P5 finish), durable verified-setup evidence, post-launch back-reference | M |
| F5 | GIT_TOKEN service-level relocation (after F0's leak filter; live SECRET-masking verification first) + git-push-setup state-aware walkthrough | M+live |
| F6 | Launch gap-report + credentialsRequired typed blocks + readiness rubric wiring + bundle preview + multi-runtime contract fix | L |
| F7 | Bring-up management window (prodOps) + done boundary + DONE snapshot + Actions prod-secrets post-create | L+live |
| F8 | Export unification (ledger setup names, compose-ready atom, spec rows) + post-launch surfaces (pipelineSummary, ImportedServices, back-ref in develop status) | M |
| F9 | Flow-eval scenarios (transition happy path Actions + webhook, gap-report recovery, divergent-repo, declared-unverified BI, compaction mid-launch, prodOps window, CD opt-out) + e2e matrix consolidation | M |

Estimate: F0–F3 ≈ 4 days; F4–F5 ≈ 3 days; F6–F7 ≈ 6–8 days; F8–F9 ≈ 3–4 days.

## Open decisions for Karel

1. **Approve the transition model** (this doc) as the target — replaces both the incremental-only
   plan and the rejected orchestrator-absorbs-everything shape.
2. **Durable verified-setup evidence location** (F4): new sidecar file per pair vs fields on
   ServiceMeta. Recommend sidecar (meta stays small; evidence is append-ish).
3. **dev-tree-dirty instruction owner** (F2): the deploy handler auto-commits — make the atom say so
   (recommend), or flip the handler to refuse-dirty and keep the atom's manual-commit instruction.
4. **Env VALUES in the bring-up window:** stay keys-only (P-LP-5 intact) — recommend confirm.
5. **customDomain:** hard-delete confirmed earlier — F2 executes it (flagged once more: stale saved
   calls will fail schema validation; Codex twice preferred tolerate-ignore).
