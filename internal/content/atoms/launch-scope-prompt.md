---
id: launch-scope-prompt
priority: 2
phases: [launch-production-active]
title: "Launch scope — collect production target details"
references-fields: []
---

### Launch scope — collect production target details

**This workflow is stateless multi-call narrowing.** Every response's `inputs` block is the running accumulator: pass all previously-accepted parameters forward on every next `action="start"` call. `action="complete"` is reserved for bootstrap and returns `BOOTSTRAP_NOT_ACTIVE` here.

Apply suggestions from `sourceContext`:

- **`productionProjectName`** — `sourceContext.suggestedTargetName` (`<source>-dev` / `<source>-stage` → `<source>-prod`, else `<source>-prod` appended). Confirm name with user; don't silently rename.
- **`targetService`** — `sourceContext.suggestedRuntime` when single. For standard-mode pairs the headline is the stage hostname (validated last-known-good); `devHostname` field discloses the iteration half. The new prod project rebuilds fresh from git, so promotion is the dev/stage pair *as a unit*. Either half is accepted as input — the handler normalizes internally. Managed deps are bundled implicitly.
- **`region`** — optional, default `eu-central`.
- **`customDomain`** — optional; ZCP emits DNS records + verification probes, user attaches in Zerops UI.
- **`keepNonHA`** — optional `[]hostname` to keep at `NON_HA` (default: all managed deps go `HA`).
- **`envOverrides`** — optional plain-config overrides. No secret values; ZCP never receives them.

When `sourceContext.availableRuntimes` has multiple entries, the user must pick. Use `AskUserQuestion` if your harness exposes it (structured choice UI); else surface the choice inline and wait for the user's next turn.

After scope is complete, ZCP advances to `classify-prompt` for env classification.
