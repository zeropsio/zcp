# Handoff: <slug>

Written when an AFK run halts mid-wave or a session boundary is crossed. A
fresh zero-context session must be able to resume from this file alone.

## Current state
- Phase: <frame|prove|shape|build|assemble|land>
- Integration SHA: <SHA>
- Slice Register snapshot: <copy of the current table, or "unchanged from plan">

## Material decisions
| Decision | Why | Evidence | Reopen only if |
|---|---|---|---|
| <decision> | <reason> | <artifact/command> | <the one condition that reopens it> |

## Changed paths
| Path | Purpose | State |
|---|---|---|
| <path> | <why touched> | <landed\|in-progress\|reverted> |

## Blockers
| Blocker | Evidence | Unblock condition |
|---|---|---|
| <what stopped the run> | <command/output proving it> | <what resolves it> |

## Exact resume
1. Read: <plan file, this handoff, the one slice brief in flight>
2. Run: <command to confirm current state, e.g. `git log --oneline <range>`>
3. Start with: <the single next bounded action>
4. Do not: <the one thing that would make this worse — e.g. re-run a landed
   slice, touch a path outside the blocked slice's scope>
