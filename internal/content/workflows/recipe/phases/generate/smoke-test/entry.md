# Smoke-test substep — validate scaffold and code on the dev container

This substep ends when each dev mount has passed the three-step validation (install, compile, process-binds) locally inside its own dev container. `zerops_workflow action=status` shows completion state.

## Why this substep exists before zerops.yaml

`zerops_deploy` triggers a full build cycle (30 seconds to three minutes depending on runtime). Catching dependency errors, type errors, and startup crashes on the live dev container takes seconds. Smoke-test first means the `buildCommands`, `cache`, and `deployFiles` fields later written into `zerops.yaml` are derived from the install flow that actually worked — not from a research-time assumption.

## Where the smoke test runs

Entirely on the dev container via SSH. Every command uses only the base image's tools — the framework's runtime and package manager. `run.envVariables` are not active yet; the smoke test does not need them. The rule "do not run commands that bootstrap the framework" means "do not connect to databases", not "do not validate your code compiles."

## Next action

Read the `on-container-smoke-test` atom for the three validation steps and the per-runtime shape. For multi-codebase plans, run the smoke test on each dev container independently before proceeding.
