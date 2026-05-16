**Surfaced**: 2026-05-16 — Karel's live manual launch-production test against the kanban app (`zAho8ZKISe6PLFUFpERsyw` parent failures). Session transcript saved at `/tmp/karel-launch-prod.jsonl` 263 events, 11 audit entries with `errorMessage: "source zerops.yaml lacks 'setup: prod' block"` over ~24h.

**Why deferred**: not a regression — Phase 2 mutation works correctly once the source yaml has the right setup. Karel hit the gate, manually edited zerops.yaml, then launch proceeded. Fixing the error UX is incremental DX, not a release blocker.

**Trigger to promote**: another live launch session hits the same gate, OR Phase 6b production-redesign work touches the source-state read path anyway. Either is the natural place to bundle this.

---

## What ZCP does today

Handler refuses with:

```
source zerops.yaml lacks `setup: prod` block
```

Karel's friction:

1. User has no idea what setups DO exist in their source yaml (`setup: appdev`, `setup: app`, etc.)
2. User doesn't know if there's a `--setup-name` / override knob
3. User edits zerops.yaml manually, commits, pushes, retries — repeats 11x until shape matches

## Sketch

Two-part UX upgrade:

1. **Diagnostic listing**: include the source yaml's actual setup names in the error.
   ```
   source zerops.yaml lacks `setup: prod` block.
   Found setups: appdev, app
   Override via WorkflowInput.ProdSetupNameOverride OR edit source zerops.yaml + retry.
   ```
2. **ProdSetupNameOverride field** on `WorkflowInput` (Phase 6b plan §6.4 already calls this out as a launch input — promote forward to here when convenient). Composer passes the override into `bundle.LaunchBundleInputs.SetupName`, replacing the hardcoded `"prod"`.

Mechanical change:
- `internal/tools/launch_source_read.go::readSourceState` (or wherever the gate fires) parses available setup names from the yaml body and includes them in the error message.
- `internal/tools/workflow.go::WorkflowInput` gains `ProdSetupNameOverride string`.
- `internal/tools/workflow_launch_production.go::executeLaunchMutation` reads the override before defaulting to `"prod"`.

## Risks

- Setup-name override interacts with the verify-step path in zerops.yaml (some setups may not have a runnable `run:` block). Phase 6b's launch-redesign yaml-validate gates already cover most of this; adding the override just shifts where the gate fires.
- If users override to a `dev`-style setup with `start: zsc noop`, the prod container will sit idle. Probably worth a warning when the chosen setup's `run.start` looks dev-ish.

## Refs

- Karel live session: `/tmp/karel-launch-prod.jsonl` (compacted from `/home/zerops/.claude/projects/-var-www/46080f33-0010-454d-9d1c-a932edaf440a.jsonl`)
- Audit entries: `/var/www/.zcp/state/launch-production/launch-audit-log.json` (11 `errorMessage: source zerops.yaml lacks 'setup: prod' block` between 2026-05-15 15:37 and 2026-05-16 09:13)
- Plan §6.4 already shapes `ProdSetupNameOverride` — Phase 6b natural home.
