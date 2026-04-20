# Scaffold substep — write project tree into each codebase mount

This substep ends when every codebase in `plan.Research.Targets` has its framework project tree on the mount, `git init` has run inside the container for each codebase, and the pre-ship assertion chain has passed per codebase. `zerops_workflow action=status` shows this substep's completion state.

## How many scaffolds and how they run

The number of scaffolds is the number of dev mounts declared by `plan.Research.Targets` after `sharesCodebaseWith` is applied. Minimal recipes and single-codebase showcase shapes have exactly one scaffold; multi-codebase showcase shapes have two or three.

| Tier and codebase shape | Scaffold count | How to run |
|---|---|---|
| Minimal or single-codebase showcase | 1 | Write the scaffold inline — no sub-agent dispatch |
| Multi-codebase showcase (2 mounts) | 2 | Dispatch one scaffold sub-agent per codebase in parallel, interpolating the shared `SymbolContract` JSON into each brief identically |
| Multi-codebase showcase (3 mounts) | 3 | Dispatch three scaffold sub-agents in parallel, same contract into each |

The contract is computed once at research-completion and frozen. Every scaffold sub-agent receives the identical JSON — env-var names, HTTP routes, NATS subjects, hostname conventions, DTO names, and the fix-recurrence rule list — so cross-codebase naming stays consistent without cross-agent coordination.

## Next action

Resolve the multi-codebase branch from `plan.Research.Targets`. Follow the `where-to-write-single` atom for single-mount recipes and the `where-to-write-multi` atom for multi-mount recipes. If the frontend target has a bundler-based dev server, read the `dev-server-host-check` atom before scaffolding so the host-check configuration goes in on the first write.
