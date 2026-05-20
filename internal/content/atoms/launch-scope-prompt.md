---
id: launch-scope-prompt
priority: 2
phases: [launch-production-active]
title: "Launch scope — collect production target details"
references-fields: []
---

### Launch scope — collect production target details

**This workflow is stateless multi-call narrowing.** Every response's `inputs` block is the running accumulator: pass all previously-accepted parameters forward on every next `action="start"` call. `action="complete"` is reserved for bootstrap and returns `BOOTSTRAP_NOT_ACTIVE` here.

#### First — identify the launch path

launch-production has two mutation paths in one workflow. Pick which one matches the user's intent BEFORE collecting scope params; the choice surfaces in `inputs` and dispatches the right mutation at the `ready-to-launch` step.

| User intent signal | Path | Required token params |
|---|---|---|
| "Create new prod project", "launch to fresh project", or no existing project mentioned | **NEW-PROJECT** | `launchKey` (one-shot launch-window token with project-creation permission — surfaced at the `ready-to-launch` step via the launch-mutation-key-required atom) |
| "I have existing prod project", explicit project ID/token supplied, "deploy into project X" | **EXISTING-PROJECT** | `existingProjectId` + `existingProdToken` (project-scoped token from target project's dashboard) |

If the user explicitly hands you an existing project ID OR a project-scoped token, pass `existingProjectId` + `existingProdToken` on this first `action="start"` call alongside the scope params below — both will land in the `inputs` accumulator and the workflow will skip the `launchKey` prompt at `ready-to-launch`. Otherwise default to NEW-PROJECT and let the workflow ask for `launchKey` later.

#### Then — apply suggestions from `sourceContext`

- **`productionProjectName`** — `sourceContext.suggestedTargetName` (`<source>-dev` / `<source>-stage` → `<source>-prod`, else `<source>-prod` appended). Confirm name with user; don't silently rename.
- **`targetService`** — `sourceContext.suggestedRuntime` when single. For standard-mode pairs the headline is the stage hostname (validated last-known-good); `devHostname` field discloses the iteration half. The new prod project rebuilds fresh from git, so promotion is the dev/stage pair *as a unit*. Either half is accepted as input — the handler normalizes internally. Managed deps are bundled implicitly.
- **`region`** — optional, default `eu-central`.
- **`customDomain`** — optional; ZCP emits DNS records + verification probes, user attaches in Zerops UI.
- **`keepNonHA`** — optional `[]hostname` to keep at `NON_HA` (default: all managed deps go `HA`).
- **`envOverrides`** — optional plain-config overrides. No secret values; ZCP never receives them.

When `sourceContext.availableRuntimes` has multiple entries, the user must pick. Use `AskUserQuestion` if your harness exposes it (structured choice UI); else surface the choice inline and wait for the user's next turn.

After scope is complete, ZCP advances to `classify-prompt` for env classification.
