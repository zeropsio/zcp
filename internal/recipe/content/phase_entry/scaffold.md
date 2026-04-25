# Scaffold phase — one sub-agent dispatch per codebase

Every codebase in `plan.codebases` gets ONE scaffold sub-agent dispatch.
The sub-agent writes source code + `zerops.yaml` for its codebase; the
main agent coordinates.

## Mount state at scaffold start

When your scaffold sub-agent receives control, the SSHFS mount at
`/var/www/<hostname>dev/` already has:

- `.git/` initialized — created by zcp's mount machinery
  (`ops.InitServiceGit`). Identity: `agent@zerops.io`,
  branch: `main`.
- One or more `deploy` commits — created by `zerops_deploy` if any
  prior deploy ran. Visible in `git log --oneline`.

Recovery for the scaffold commit:

```bash
cd /var/www/<hostname>dev
git reset --soft $(git rev-list --max-parents=0 HEAD) 2>/dev/null || \
  (rm -rf .git && git init -q -b main)
git config user.email recipe@zerops.io
git config user.name 'Recipe Author'
git add -A
git commit -q -m 'scaffold: initial structure + zerops.yaml'
```

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

1. **Compose brief**:
   `zerops_recipe action=build-brief slug=<slug>
   briefKind=scaffold codebase=<hostname>`

   The brief returns platform-obligation prose (bind 0.0.0.0, trust
   proxy, SIGTERM drain), the `content_authoring.md` placement rubric
   + tone rules, and — for any codebase whose `HasInitCommands` is
   true — the execOnce key-shape concept atom. It does NOT contain
   framework-specific instructions; the sub-agent brings that from
   its own knowledge.

2. **Dispatch the sub-agent** via the `Agent` tool. Pass the brief's
   `body` verbatim as the `prompt`. Description: `scaffold-<hostname>`.

3. **Sub-agent produces**: source tree under the Zerops service's
   SSHFS mount (`/var/www/<hostname>/` in-container, or equivalent
   local path), including `zerops.yaml`. It deploys to its service
   (`zerops_deploy targetService=<hostname>`) and runs the preship
   contract from its brief (HTTP reachable, X-Forwarded-For echoes,
   SIGTERM drain, migrations ran). Records facts for any deviation.

4. **Verify the dev deploy**: `zerops_verify targetService=<hostname>`.

5. **Start the dev server**: `zerops_dev_server action=start` (dynamic
   runtimes + any codebase with a frontend bundler). Dev slots run
   `start: zsc noop --silent` and do NOT auto-start — the long-running
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

The engine builds the brief; the main agent dispatches it. If the main
agent paraphrases or truncates the brief before dispatch, the sub-agent
misses critical platform rules. Always pass `brief.body` byte-identical.

`zerops_recipe action=verify-subagent-dispatch` is planned but not yet
implemented in v3 — for now, do not paraphrase.

## Content authored in-phase

The scaffold sub-agent records fragments at the moment of freshest
context: `codebase/<hostname>/intro`, `integration-guide`,
`knowledge-base`, `claude-md/service-facts`, `claude-md/notes`. The
sub-agent also writes inline comments into its committed `zerops.yaml`
— they ship byte-identical into the published deliverable. No
post-hoc writer sub-agent; no journal-then-writer pattern.

## Wrapper discipline — what main decides vs sub-agent discovers

The main agent decides: resource name, endpoint path, which codebase
owns which concern (api/worker/frontend split), which tier the plan
targets. The sub-agent discovers: library choice, client config shape,
package name, framework-specific import path. Do NOT pre-chew library
decisions in the dispatch wrapper — the sub-agent consults
`zerops_knowledge` and picks based on its framework expertise.

## Complete-phase gate

Every plan.codebase hostname must be deployed + verified on BOTH the
dev and stage slots, every scaffold-owned fragment id recorded, and
every codebase with initCommands must have attested that they ran
(success line + post-deploy data check). Facts recorded during the
phase flow into the classification gate at finalize.
