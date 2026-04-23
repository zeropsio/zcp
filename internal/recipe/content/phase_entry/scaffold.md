# Scaffold phase — one sub-agent dispatch per codebase

Every codebase in `plan.codebases` gets ONE scaffold sub-agent dispatch.
The sub-agent writes source code + `zerops.yaml` for its codebase; the
main agent coordinates.

## For each codebase (parallelize when shape is 2 or 3)

1. **Compose brief**:
   `zerops_recipe action=build-brief slug=<slug>
   briefKind=scaffold codebase=<hostname>`

   The brief returns platform-obligation prose (bind 0.0.0.0, trust
   proxy, execOnce for migrations, SIGTERM drain) plus the parent
   codebase's README excerpt if the chain resolver found one. It does
   NOT contain framework-specific instructions — the sub-agent brings
   that from its own knowledge.

2. **Dispatch the sub-agent** via the `Agent` tool. Pass the brief's
   `body` verbatim as the `prompt`. Description: `scaffold-<hostname>`.

3. **Sub-agent produces**: source tree under the Zerops service's
   SSHFS mount (`/var/www/<hostname>/` in-container, or equivalent
   local path), including `zerops.yaml`. It deploys to its service
   (`zerops_deploy targetService=<hostname>`) and runs the preship
   contract from its brief (HTTP reachable, X-Forwarded-For echoes,
   SIGTERM drain, migrations ran). Records facts for any deviation.

4. **Verify the deploy**: `zerops_verify targetService=<hostname>`.
   Every scaffold codebase must be green before the phase completes.

## Dispatch integrity

The engine builds the brief; the main agent dispatches it. If the main
agent paraphrases or truncates the brief before dispatch, the sub-agent
misses critical platform rules. Always pass `brief.body` byte-identical.

`zerops_recipe action=verify-subagent-dispatch` is planned but not yet
implemented in v3 — for now, do not paraphrase.

## Complete-phase gate

Every plan.codebase hostname must have a deployed + verified service.
Scaffold facts recorded during the phase flow into the writer brief at
finalize — more facts = richer content. Under-record is visible; over-
record is not.
