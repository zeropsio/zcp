# Generate — write app code and configuration onto dev mounts

Generate takes populated plan state plus provisioned services and writes the source tree, on-container smoke test, and zerops.yaml onto every dev mount. This phase completes when every substep's predicate holds on the mount; `zerops_workflow action=status` is the authoritative substep list.

## Container state during generate

The dev services are RUNNING (the provision step used `startWithoutCode`) but no `zerops.yaml` has been deployed yet. The distinction between what is available and what is not drives every rule in this phase.

| Available right now | Activates only after the first `zerops_deploy` |
|---|---|
| Base image tools — runtime and package manager | Secondary build bases added in `buildCommands` |
| Platform vars — `hostname`, `serviceId`, project-scope `zeropsSubdomainHost` | `run.envVariables` — cross-service references |
| SSHFS access to `/var/www/` for file writes | Managed-service connectivity from inside the container |
| Implicit webservers auto-serving from the mount | Application configuration as the recipe expects it |

## What is safe to run on the container during generate

Scaffold-class commands are safe over SSH — project creation, `git init`, file operations — because they use only the base image and need no environment variables. The smoke-test substep (later in this phase) runs `{packageManager} install`, compile/type-check, and a "process binds to port" check. Every one of those commands lives entirely in the base image.

## What generate does not do

Generate does not bootstrap the framework end-to-end: no migrations, no cache warming, no health checks that probe managed services, no CLI tools that attempt service connections. Those activities require `run.envVariables`, which activate only when `zerops_deploy` runs. If a command during generate fails with `connection refused`, `driver not found`, or an equivalent service-connectivity error, that is the expected signal — continue writing files rather than modifying code, changing drivers, or creating `.env` files to paper over it. The deploy step activates the environment and those same connections succeed.

## Substeps in this phase

The substep list is authoritative in `zerops_workflow action=status`. Each substep carries its own entry atom and completion predicate. In order: scaffold, app-code, smoke-test, zerops-yaml, then the phase-level completion predicate.
