# Fresh-context premise

You have no memory of the run that brought the recipe to this point. Your context is intentionally clean of the debug rounds, the scaffold decisions, the feature-implementation spirals, and any prior sub-agent's internal state. Reader-facing content is written from the reader's perspective, not the author's, and the fastest way to keep the reader's perspective intact is to start cold.

Three inputs carry everything you need. Treat them as the canonical substrate.

## Input 1 — The facts log

Path: `{{.FactsLogPath}}`.

Read it with `cat` or any JSON-line parser. Each line is one FactRecord. Fields you consume:

- `title` — the fact identifier. One manifest entry per distinct title.
- `type` — one of gotcha_candidate, ig_item_candidate, verified_behavior, platform_observation, fix_applied, cross_codebase_contract.
- `codebase` + `substep` — where the fact came from.
- `mechanism`, `failureMode`, `fixApplied`, `evidence` — the reasoning substrate.
- `scope` — filter this. `content` or unset means published content candidate; `downstream` means the fact is scratch knowledge for later sub-agents and you skip it; `both` means consider for publication and also pass through.
- `routeTo` — if set, the recording sub-agent pre-routed this fact; honor that routing unless you document an `override_reason` in the manifest.

## Input 2 — The deploy manifest and recipe state

Path root: `{{.ProjectRoot}}`.

The plan snapshot at `{{.PlanPath}}` tells you which codebases exist, which managed services are wired, and which environment tiers the recipe defines. The recipe state on the mount is read-only to you except for files you own (see the canonical-output-tree atom that follows). Use the plan to decide which files are worth reading. Do not crawl the whole tree.

## Input 3 — Platform topic knowledge

Calls to `mcp__zerops__zerops_knowledge` with a topic id. The citation-map atom lists every topic area that triggers a mandatory consultation.

## Inputs you do NOT have

You do not have the run transcript, the main-agent context, memory of what broke during deploy, or any prior sub-agent's internal reasoning. Asking for them is out of scope. If you think you need them, the answer is in the facts log or the recipe state on disk.

## Env folder names are NOT your vocabulary

Writing env-tier README files is finalize's job, not yours. You do NOT create or modify any file or directory under `environments/`. The six env-tier READMEs are emitted at finalize time from the plan snapshot; they are not part of your scope.

When you reference an env tier in prose (for example, inside a per-codebase integration-guide item that mentions tier-promotion), use the tier's prettyName from the plan — "AI Agent", "Remote Development", "Local Development", "Stage", "Small Production", "HA Production". Never invent a slug; never author a directory named after one. Env folder naming is the step-above's responsibility, and your slug guesses do not match it.
