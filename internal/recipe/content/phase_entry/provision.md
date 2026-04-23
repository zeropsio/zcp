# Provision phase — create services from the plan

The research phase produced a typed Plan. Provision creates the Zerops
services for the AI-agent tier (tier 0) so scaffold and feature phases
have a live platform to deploy against.

## Steps

1. **Emit the AI-agent tier import.yaml**:
   `zerops_recipe action=emit-yaml slug=<slug> tierIndex=0`

2. **Write it to disk** under
   `<outputRoot>/0 — AI Agent/import.yaml` (engine will regenerate it
   at finalize; this is the working copy).

3. **Provision via zerops_import**:
   `zerops_import file="<path to tier-0 import.yaml>"`.
   This creates the project + services on Zerops. Save the project ID.

4. **Verify provisioning** via `zerops_discover` — every plan.Codebase
   hostname + plan.Service hostname must appear with status=active.

5. **Inject project-level secrets** (if `Research.NeedsAppSecret=true`):
   set `<AppSecretKey>` as a project-level env via `zerops_env`. The
   yaml emitter already includes the preprocessor directive; the
   secret's actual value gets generated at import time.

6. Complete the phase: `zerops_recipe action=complete-phase slug=<slug>`.

## What NOT to do here

- Do NOT provision tier 1-5 now. Those tiers exist on paper (the
  engine emits their import.yaml at finalize) but the AI-agent tier is
  the only one that gets a live project for the duration of this run.
- Do NOT modify the plan from within provision. If you discover the
  plan is wrong, `action=reset` and rerun research — don't drift.
- Do NOT call `zerops_import` with a hand-written yaml. Use the
  engine-emitted output. If the emitter produces invalid yaml, record a
  fact and fix the emitter via PR — that's an engine defect, not a
  platform workaround.

## Gate at complete-phase

Checks: the project exists, every plan hostname resolved to an active
service, zero hostname drift between plan and Zerops state.
