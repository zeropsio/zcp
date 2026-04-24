# Scaffold phase — one sub-agent dispatch per codebase

Every codebase in `plan.codebases` gets ONE scaffold sub-agent dispatch.
The sub-agent writes source code + `zerops.yaml` for its codebase; the
main agent coordinates.

## For each codebase (parallelize when shape is 2 or 3)

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

4. **Verify the deploy**: `zerops_verify targetService=<hostname>`.
   Every scaffold codebase must be green before the phase completes.

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

## Complete-phase gate

Every plan.codebase hostname must have a deployed + verified service,
and every scaffold-owned fragment id must be recorded. Facts recorded
during the phase flow into the classification gate at finalize.
