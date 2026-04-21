# todowrite-mirror-only

The authoritative plan lives on the server. `zerops_workflow action=status` at any moment returns the definitive substep list, their ordering, their completion state, and the attest predicate for the currently open substep. Treat that state as the source of truth; do not maintain a parallel plan.

## TodoWrite is a read-side mirror

TodoWrite reflects the server's substep list so you (and the user) can see progress at a glance. You use it in one shape:

- **Check off a substep as it completes.** When a substep's attest predicate holds and the server acknowledges the transition, mark the corresponding Todo item done.

That is the whole interaction surface.

## The flow

1. At step-entry, the server emits the substep list for the current phase.
2. You mirror that list into TodoWrite once (if not already mirrored) so progress is visible.
3. You work through substeps in the server-declared order. As each substep's predicate holds, you mark its Todo done.
4. The server advances to the next substep on its own schedule; you mirror the next-substep framing from the server's output.

## What this rules out

Rewriting the whole Todo list at every step-entry. Rewriting duplicates the server's substep list with a parallel representation that drifts. If you find yourself re-typing the substep list at step-entry, stop — the server already has it.

Authoring Todos that do not correspond to a server substep. Ad-hoc personal tasks ("run the build once more to confirm") may live in TodoWrite inside an open substep if they help you, but they are your own scratchpad and do not affect workflow state.

## The short version

Server substep list = the plan. TodoWrite = a view of the plan. Treat TodoWrite as a reflection, not an authoring surface.
