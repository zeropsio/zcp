# Substep: cross-deploy-stage

This substep completes when every `*stage` target in the plan returns `ACTIVE` from a cross-deploy against its dev source. Stage is the final product — deploy it once with the complete codebase (skeleton plus features).

## Verify prod setup in zerops.yaml

The prod setup block was written to `zerops.yaml` during the generate phase. Before cross-deploying, confirm it matches what a real user building from git will need — the generate phase wrote it from the recipe shape, and any later edits (during deploy-dev iteration) may have drifted. Read `zerops.yaml` from the mount and confirm:

- `deployFiles` lists every path the start command and framework need at runtime. Run `ls` on the mount and cross-reference. When the plan cherry-picks paths (rather than `.`), missing one path produces `DEPLOY_FAILED` at first request.
- `healthCheck` and `deploy.readinessCheck` are present on the prod setup. Prod requires them — unresponsive containers are restarted, broken builds are gated from traffic.
- `initCommands` on prod covers framework cache warming and migrations. These belong in `run.initCommands`, not `build.buildCommands` — `/build/source/...` paths break at `/var/www/...`.
- Mode flags differ from dev (`APP_ENV`, `NODE_ENV`, `DEBUG`, `LOG_LEVEL`).

If any of those needs a correction, edit `zerops.yaml` on the mount now — the change propagates to the integration-guide fragment (which mirrors the file content) the `readmes` substep will author.

## Parallel dispatch — one message, one batch

Every `*stage` target is an independent cross-deploy. Each targets a different container, runs a different build pipeline, and shares nothing with its siblings. Dispatch every stage deploy in a single message as parallel tool calls — serializing ~2 minutes of work back-to-back buys nothing.

What the plan enumerates as parallel:

- **Minimal single-runtime** — `appstage` only (nothing to parallelize).
- **Showcase single-runtime** — `appstage` plus `workerstage` (both cross-deploy from `appdev`, different setups). Two parallel calls.
- **Minimal dual-runtime (API-first)** — `appstage` plus `apistage`. Two parallel calls.
- **Showcase dual-runtime (API-first)** — `appstage` plus `apistage` plus `workerstage`. Three parallel calls.

Example call shape (dispatch as parallel tool calls in one message):

```
zerops_deploy sourceService="apidev" targetService="apistage" setup="prod"
zerops_deploy sourceService="apidev" targetService="workerstage" setup="worker"
zerops_deploy sourceService="appdev" targetService="appstage" setup="prod"
```

- `setup="prod"` maps to `setup: prod` in the target's `zerops.yaml`. The server auto-starts via the prod `start` command, or via Nginx for a static build.
- `setup="worker"` maps to `setup: worker` in the host target's `zerops.yaml` — used only for a shared-codebase worker (`sharesCodebaseWith` is set). Source and target are the same host (`appdev` / `apidev`), just a different setup name. Same build pipeline, different start command.
- **Separate-codebase worker** (`sharesCodebaseWith` empty, including the 3-repo same-runtime case): source is `workerdev`, target is `workerstage`, setup is `prod` (its own `zerops.yaml`). Still parallel with the other cross-deploys.

Cross-deploys do not mutate their source service, do not share build containers, and the platform schedules them on separate target containers. Sibling stage targets have no ordering constraint between them. The only ordering constraints in this whole substep are: (a) dev targets are healthy before their stage cross-deploys (satisfied by reaching this substep), and (b) the subdomain-enable and verify calls in `verify-stage` run after the deploys return.

## Attestation shape

One line per stage target: target name, deploy status (`ACTIVE`). A failing target at cross-deploy blocks progression; the fix loop returns to the dev source (dev-side edit, dev re-deploy through `snapshot-dev` for showcase or `deploy-dev` for minimal, then re-attempt cross-deploy).
