# Scaffold phase — one sub-agent dispatch per codebase

**Next call:** `zerops_recipe action=enter-phase slug=<slug> phase=scaffold` (advances the pointer + mints fact shells — REQUIRED before any dispatch). THEN for each codebase in `plan.codebases`, `zerops_recipe action=build-subagent-prompt slug=<slug> briefKind=scaffold codebase=<hostname>`, then dispatch each returned prompt via the `Agent` tool in parallel (`subagent_type="general-purpose"`). After every codebase sub-agent self-closes, `zerops_recipe action=complete-phase slug=<slug> phase=scaffold` → `action=enter-phase slug=<slug> phase=feature`.

Every codebase in `plan.codebases` gets ONE scaffold sub-agent dispatch.
The sub-agent writes source code + `zerops.yaml` for its codebase; the
main agent coordinates.

## Mount state at scaffold start

When your scaffold sub-agent receives control, the SSHFS mount at
`/var/www/<hostname>dev/` NORMALLY already has:

- `.git/` initialized — created by zcp's mount machinery
  (`ops.InitServiceGit`). Identity is filled if absent (never stomped;
  default `agent@zerops.io` / `Zerops Agent` unless you already set your
  own), branch: `main`, and a single `zcp init` marker commit guarantees
  HEAD is reachable. This step is best-effort — its failure is swallowed
  at bootstrap — so don't assume it landed; see the identity-recovery line
  in the Git hygiene section of your brief before committing.
- `zerops_deploy` no longer creates commits — direct deploy snapshots the
  working tree without touching git history. The scaffold commit below is
  the first REAL commit in the repo.

Recovery for the scaffold commit:

```bash
cd /var/www/<hostname>dev
git reset --soft $(git rev-list --max-parents=0 HEAD) 2>/dev/null || \
  (rm -rf .git && git init -q -b main)
git config user.email recipe@zerops.io
git config user.name 'Recipe Author'
git add -A
[ -n "$(git status --porcelain)" ] && \
  git commit -q -m 'scaffold: initial structure + zerops.yaml' || \
  echo 'no changes to commit'
```

The `git status --porcelain` pre-check guards against an empty diff:
`git commit` exits 1 on a clean working tree and cancels every
parallel tool call in the same Claude message as collateral.

Pick the recovery once and apply consistently across all three scaffold
sub-agents — wipe-and-reinit is acceptable for a dogfood run; in
production, the publish path may want to preserve any meaningful deploy
history. For run 12, wipe-and-reinit.

## Dispatch every codebase scaffold IN PARALLEL

With 2 or 3 codebases, dispatch all sub-agents in a single message (one
`Agent` tool call per codebase, emitted in parallel). Each sub-agent's
`zerops_deploy` + `zerops_verify` calls queue naturally at the recipe
session mutex — you do NOT need to serialize the dispatch to serialize
the deploys. File authoring, `Bash` and `ssh` commands, `npm install`,
local builds, and `zerops_knowledge` consults run concurrently across
sidechains.

Net savings for a 3-codebase scaffold: 15-30 minutes. Serializing
dispatch is the wrong optimization — the sub-agents block on their own
framework work, not on each other.

## For each codebase

1. **Compose dispatch prompt**:
   `zerops_recipe action=build-subagent-prompt slug=<slug>
   briefKind=scaffold codebase=<hostname>`

   The response carries the FULL dispatch prompt — engine-owned
   recipe-level context (slug, framework, tier, codebase identity,
   sister codebases, managed services, your codebase block) +
   the engine brief body verbatim + closing notes naming the
   self-validate path. No hand-typed wrapper needed; the engine has
   every Plan-derivable fact already.

   (`build-brief` still works and returns the brief body alone — use
   it when you intend to compose your own wrapper, e.g. for a one-off
   debugging dispatch. Default path is `build-subagent-prompt`.)

2. **Dispatch the sub-agent** via the `Agent` tool. Pass
   `response.prompt` verbatim as the `prompt`. Description:
   `scaffold-<hostname>`. Pass `subagent_type="general-purpose"` —
   do NOT use `subagent_type="claude"` (FleetView's default when
   unspecified). `claude` triggers worktree isolation on dispatch,
   which fails on the non-git recipe-authoring outputRoot and
   breaks the shared `zerops_recipe` MCP state. When
   `response.prompt` is empty (multi-file pointer), dispatch the
   thin wrapper instructing the sub-agent to Read `response.briefPath`
   first — same `subagent_type="general-purpose"` rule applies.

3. **Sub-agent produces**: source tree under the Zerops service's
   SSHFS mount (`/var/www/<hostname>/` in-container, or equivalent
   local path), including `zerops.yaml`. It deploys to its service
   (`zerops_deploy targetService=<hostname>`) and runs the preship
   contract from its brief (HTTP reachable, X-Forwarded-For echoes,
   SIGTERM drain, migrations ran). Records facts for any deviation.

4. **Verify the dev deploy**: `zerops_verify targetService=<hostname>`.

5. **Start the dev server**: `zerops_dev_server action=start` (dynamic
   runtimes + any codebase with a frontend bundler). Dev slots omit
   `run.start` and do NOT auto-start any app process — the long-running
   process is owned by the agent so code edits don't force a redeploy.
   Implicit-webserver backends skip this for their own process, but
   run the tool for a compiled frontend (Vite, esbuild) when applicable.
   See `principles/dev-loop.md` in the brief.

6. **Verify initCommands ran** (when the scaffold authored any):
   - `zerops_logs serviceHostname=<hostname> severity=INFO since=10m` —
     confirm the framework's success lines (applied-migration rows,
     "N rows seeded", "indexed N documents"). The sub-agent knows what
     its framework's success output looks like.
   - Query application state directly: rows in the DB, documents in
     the search index, objects in storage. Do NOT infer "initCommands
     ran" from "deploy ACTIVE" alone — a prior failed deploy can burn
     the execOnce key silently and the next deploy will skip it.
   - **Burned-key recovery**: if data is missing after a successful
     deploy, touch any source file and redeploy — the new deploy
     version makes per-deploy execOnce keys re-fire. Hand-run the
     command only when recovery-by-redeploy is not available.

7. **Cross-deploy dev → stage**:
   `zerops_deploy sourceService=<hostname>dev targetService=<hostname>stage`,
   then `zerops_verify targetService=<hostname>stage`. This proves the
   prod setup path (optimized build, `npm ci --omit=dev`, `./dist/~`
   deployFiles) works, not just the dev self-deploy. Both slots must
   be green before the phase completes.

## Dispatch integrity

The engine composes the full dispatch prompt deterministically from
Plan + Research.Description via `build-subagent-prompt`. Pass
`response.prompt` to the `Agent` tool byte-identical — the prompt IS
the engine output, so paraphrase + truncation risk is mathematically
zero. There is no separate verify step in the prescribed flow; the
recipe action list still carries a recovery primitive in the engine
for hand-composed dispatches, but the byte-identical pass-through
path here doesn't need it.

## What you author vs what you record (run-16)

**You author**: source code + the committed `zerops.yaml` for your
codebase. That's the deploy artifact — it has to exist for
`zerops_deploy` to work.

**You record (`zerops_recipe action=record-fact`)**: structured facts
naming every non-obvious decision at densest context — the moment you
make the change. Two subtypes cover scaffold scope:

- `porter_change` — code or library decisions a porter would have to
  make (bind 0.0.0.0, install a library, configure CORS, write a
  proxy). See `briefs/scaffold/decision_recording.md`.
- `field_rationale` — non-obvious yaml field decisions
  (`S3_REGION=us-east-1` because MinIO requires it; two `execOnce`
  keys to decouple migrate + seed).

**You do NOT author** documentation surfaces during scaffold. No IG /
KB / CLAUDE.md fragment recording. No `zerops.yaml` block comments
(those are written above the yaml at codebase-content stitch). Two
content sub-agents at phase 5 (`codebase-content` + `claudemd-author`)
read your recorded facts + on-disk source / yaml / spec and synthesize
all documentation surfaces.

This is the run-16 architecture pivot: deploy phases capture the WHY
at densest context; content phases author the prose with full context
+ cross-surface awareness. Closes R-15-4 (CLAUDE.md bleed-through),
R-15-6 (cross-surface dup), R-15-7 (classification reach).

The gate set running at scaffold complete-phase (`CodebaseScaffoldGates`,
introduced in run-17 §8) checks fact-recording quality only — it does
NOT check IG / KB / CLAUDE.md / zerops.yaml comment fragments.
Recording a fragment to "clear the gate" is wrong: the gate is already
satisfied by your fact-recording work. Fragment authoring runs at
codebase-content phase 5 with a different sub-agent.

## Subdomain auto-enable — happens inside `zerops_deploy`

Every `zerops_deploy` of a non-worker codebase auto-enables the L7
subdomain on first deploy when `zerops.yaml` has `httpSupport: true` on
a port. The deploy result carries `SubdomainAccessEnabled: true` plus the
URL in the response payload; ZCP probes HTTP-readiness before returning
so the next `zerops_verify` doesn't race port propagation.

Do NOT preemptively call `zerops_subdomain action=enable` inside the
scaffold sub-agent or the main agent. The deploy handler owns the L7
activation step on first deploy. Manual enable is a recovery path only,
to be used when a deploy result returns a warning indicating auto-enable
failed (`auto-enable subdomain failed: ...`).

Eligibility derives from REST-authoritative state via two ORed signals:
`detail.SubdomainAccess` (end-user click-deploy path; set after the
deliverable yaml has provisioned a subdomain) OR `detail.Ports[].HTTPSupport`
(recipe-authoring path; workspace yaml carries `enableSubdomainAccess: true`
but the platform doesn't flip `detail.SubdomainAccess` from import alone,
so the deploy-time port signal is the only intent visible during scaffold).
Run-15 R-15-1 surfaced the gap: every recipe-authoring scaffold-app
dispatch had to manually call `zerops_subdomain action=enable` on
appdev/appstage; run-16 closes it by ORing both signals.

## Wrapper discipline — what main decides vs sub-agent discovers

The main agent decides: resource name, endpoint path, which codebase
owns which concern (api/worker/frontend split), which tier the plan
targets. The sub-agent discovers: library choice, client config shape,
package name, framework-specific import path. Do NOT pre-chew library
decisions in the dispatch wrapper — the sub-agent consults
`zerops_knowledge` and picks based on its framework expertise.

## Scaffold close — main-agent action sequence

After all scaffold sub-agents have terminated:

1. `zerops_deploy` for each codebase (cross-deploy dev → stage if not
   already done by the sub-agent).
2. `zerops_verify` for each cross-deployed service.
3. `zerops_recipe action=complete-phase phase=scaffold` (no codebase
   parameter). The gate requires every codebase deployed + verified on
   dev + stage before it returns `ok:true`. Calling complete-phase
   before deploy + verify wastes a turn — the gate fails on missing
   verifications and you re-run the same sequence anyway.

The per-codebase pre-termination self-validate (sub-agent's call
during scaffold) is a different action — the sub-agent already
self-validates before terminating. Main's no-codebase call is the
final phase-advance gate.

## Complete-phase gate

Every plan.codebase hostname must be deployed + verified on BOTH the
dev and stage slots, every scaffold-owned fragment id recorded, and
every codebase with initCommands must have attested that they ran
(success line + post-deploy data check). Facts recorded during the
phase flow into the classification gate at finalize.

## Self-validate before terminating (sub-agent)

Before you terminate, call:

    zerops_recipe action=complete-phase phase=scaffold codebase=<your-host>

This runs the codebase-scoped validators (IG / KB / CLAUDE / yaml-
comment / source-comment-voice) against your codebase's surfaces only
— peer codebases are NOT validated, so you only see your own work.

If `ok:true`: all your work passes the gate; safe to terminate.

If `ok:false` with violations:
- Violations on `codebase/<host>/{integration-guide,knowledge-base,
  claude-md/*}` ids → fix via `record-fragment mode=replace
  fragmentId=codebase/<host>/<name> fragment=<corrected body>`.
- Violations on `<SourceRoot>/zerops.yaml` (yaml-comment-missing-
  causal-word, etc.) → ssh-edit the yaml file directly; it's not a
  fragment, it's the committed source. After ssh-edit, the engine's
  IG item-1 generator will re-read the yaml body on next stitch.
- Re-call `complete-phase phase=scaffold codebase=<your-host>` to
  verify the fix.
- Repeat until `ok:true`, then terminate.

The phase-level `complete-phase` (no codebase parameter) is the main
agent's responsibility after all sub-agents return — it advances the
phase state. Your job is just to ensure your own codebase's gate
passes before you exit.
