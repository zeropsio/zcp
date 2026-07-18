# Axis M cluster #1 — container concept per-occurrence ledger

Date: 2026-04-27
Round: CORPUS-SCAN (Phase 4)
Plan: §3 Axis M cluster #1 decision sub-table (post-Phase-0 amendment 3)

## Decision sub-table (recap from plan)

| Use this term | When the atom is talking about |
|---|---|
| dev container | Mutable push-dev / SSHFS context |
| runtime container | Running service instance generally |
| build container | Build-stage filesystem |
| Zerops container | Broad first-introduction framing |
| new container | Replacement container per deploy |

## Per-occurrence ledger

| # | atom-id | file:line | pre-edit phrase (≤80 chars context) | current term | proposed canonical | rationale |
|---|---|---|---|---|---|---|
| 1 | `bootstrap-classic-plan-static` | `internal/content/atoms/bootstrap-classic-plan-static.md:13` | Static containers (nginx) come up serving an empty document root — no | `containers` | runtime container | Running service instance generally. |
| 2 | `bootstrap-classic-plan-static` | `internal/content/atoms/bootstrap-classic-plan-static.md:19` | - whether a stage pair is wanted (dev/stage pattern) or single-container | `container` | runtime container | Conservative default for service-instance wording. |
| 3 | `bootstrap-close` | `internal/content/atoms/bootstrap-close.md:15` | (managed services `RUNNING`, runtimes registered, dev containers | `containers` | dev container | Mutable push-dev / SSHFS context or container-side workspace. |
| 4 | `bootstrap-env-var-discovery` | `internal/content/atoms/bootstrap-env-var-discovery.md:51` | **Dev-container caveat**: env vars resolve at deploy time, not OS env | `container` | runtime container | Conservative default for service-instance wording. |
| 5 | `bootstrap-env-var-discovery` | `internal/content/atoms/bootstrap-env-var-discovery.md:52` | vars on a `startWithoutCode: true` container. A dev container that | `container` | dev container | Mutable push-dev / SSHFS context or container-side workspace. |
| 6 | `bootstrap-env-var-discovery` | `internal/content/atoms/bootstrap-env-var-discovery.md:52` | vars on a `startWithoutCode: true` container. A dev container that | `container` | dev container | Mutable push-dev / SSHFS context or container-side workspace. |
| 7 | `bootstrap-mode-prompt` | `internal/content/atoms/bootstrap-mode-prompt.md:16` | - **dev** — single mutable container, SSHFS-mountable, no stage pair. | `container` | dev container | Mutable push-dev / SSHFS context or container-side workspace. |
| 8 | `bootstrap-mode-prompt` | `internal/content/atoms/bootstrap-mode-prompt.md:21` | - **simple** — single container that starts real code on every deploy; | `container` | runtime container | Running service instance generally. |
| 9 | `bootstrap-provision-local` | `internal/content/atoms/bootstrap-provision-local.md:16` | \| Standard \| `{name}stage` only (no dev on Zerops) \| Yes — shared with con... | `container` | KEEP-AS-IS | Environment qualifier, not the container-as-service-instance concept. |
| 10 | `bootstrap-provision-local` | `internal/content/atoms/bootstrap-provision-local.md:25` | - No `maxContainers: 1` — use defaults. | `Containers` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 11 | `bootstrap-provision-rules` | `internal/content/atoms/bootstrap-provision-rules.md:39` | `startWithoutCode`, `maxContainers`, and `enableSubdomainAccess` vary by | `Containers` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 12 | `bootstrap-provision-rules` | `internal/content/atoms/bootstrap-provision-rules.md:45` | \| `maxContainers` \| `1` \| omit \| omit \| | `Containers` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 13 | `bootstrap-runtime-classes` | `internal/content/atoms/bootstrap-runtime-classes.md:17` | (container) or via your harness background task primitive (local) after | `container` | KEEP-AS-IS | Environment qualifier, not the container-as-service-instance concept. |
| 14 | `bootstrap-wait-active` | `internal/content/atoms/bootstrap-wait-active.md:12` | After `zerops_import` completes, the Zerops engine provisions containers | `containers` | runtime container | Running service instance generally. |
| 15 | `develop-change-drives-deploy` | `internal/content/atoms/develop-change-drives-deploy.md:15` | - Dev-mode dynamic-runtime container: code-only changes pick up via | `container` | runtime container | Already names the running service instance. |
| 16 | `develop-checklist-dev-mode` | `internal/content/atoms/develop-checklist-dev-mode.md:6` | environments: [container] | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 17 | `develop-checklist-dev-mode` | `internal/content/atoms/develop-checklist-dev-mode.md:14` | `healthCheck`. The platform keeps the container idle; you start | `container` | runtime container | Running service instance generally. |
| 18 | `develop-checklist-simple-mode` | `internal/content/atoms/develop-checklist-simple-mode.md:6` | environments: [container] | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 19 | `develop-checklist-simple-mode` | `internal/content/atoms/develop-checklist-simple-mode.md:14` | - There is no dev+stage pair; `{hostname}` is the single runtime container. | `container` | runtime container | Already names the running service instance. |
| 20 | `develop-close-push-dev-dev` | `internal/content/atoms/develop-close-push-dev-dev.md:8` | environments: [container] | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 21 | `develop-close-push-dev-dev` | `internal/content/atoms/develop-close-push-dev-dev.md:11` | references-atoms: [develop-dev-server-reason-codes, develop-dynamic-runtime-s... | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 22 | `develop-close-push-dev-dev` | `internal/content/atoms/develop-close-push-dev-dev.md:16` | Dev mode has no stage pair: deploy the single runtime container, | `container` | runtime container | Already names the running service instance. |
| 23 | `develop-close-push-dev-dev` | `internal/content/atoms/develop-close-push-dev-dev.md:25` | Each deploy gives a new container with no dev server — check | `container` | new container | Replacement container created by deploy semantics. |
| 24 | `develop-close-push-dev-dev` | `internal/content/atoms/develop-close-push-dev-dev.md:27` | See `develop-dynamic-runtime-start-container` for parameters and | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 25 | `develop-close-push-dev-local` | `internal/content/atoms/develop-close-push-dev-local.md:14` | Local mode builds from your committed tree — no SSHFS, no dev container. | `container` | dev container | Mutable push-dev / SSHFS context or container-side workspace. |
| 26 | `develop-close-push-dev-local` | `internal/content/atoms/develop-close-push-dev-local.md:23` | dev container to cross-deploy from, so a single deploy+verify covers the close. | `container` | dev container | Mutable push-dev / SSHFS context or container-side workspace. |
| 27 | `develop-close-push-dev-standard` | `internal/content/atoms/develop-close-push-dev-standard.md:8` | environments: [container] | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 28 | `develop-close-push-dev-standard` | `internal/content/atoms/develop-close-push-dev-standard.md:10` | references-atoms: [develop-auto-close-semantics, develop-dynamic-runtime-star... | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 29 | `develop-close-push-git-container` | `internal/content/atoms/develop-close-push-git-container.md:2` | id: develop-close-push-git-container | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 30 | `develop-close-push-git-container` | `internal/content/atoms/develop-close-push-git-container.md:7` | environments: [container] | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 31 | `develop-close-push-git-container` | `internal/content/atoms/develop-close-push-git-container.md:11` | ### Closing the task — container + push-git | `container` | runtime container | Conservative default for service-instance wording. |
| 32 | `develop-close-push-git-container` | `internal/content/atoms/develop-close-push-git-container.md:14` | configured), commit on the dev container and push: | `container` | dev container | Mutable push-dev / SSHFS context or container-side workspace. |
| 33 | `develop-close-push-git-local` | `internal/content/atoms/develop-close-push-git-local.md:32` | uses your local git, not a container token). | `container` | KEEP-AS-IS | Token-location contrast; not a service-instance term. |
| 34 | `develop-deploy-files-self-deploy` | `internal/content/atoms/develop-deploy-files-self-deploy.md:18` | 1. Build container assembles the artifact from the upload + any | `container` | build container | Explicitly about the build-stage filesystem. |
| 35 | `develop-deploy-files-self-deploy` | `internal/content/atoms/develop-deploy-files-self-deploy.md:22` | 3. Runtime container's `/var/www/` is **overwritten** with that subset — | `container` | runtime container | Already names the running service instance. |
| 36 | `develop-deploy-modes` | `internal/content/atoms/develop-deploy-modes.md:18` | **immutable artifact** from the build container's post-`buildCommands` | `container` | build container | Explicitly about the build-stage filesystem. |
| 37 | `develop-deploy-modes` | `internal/content/atoms/develop-deploy-modes.md:31` | `deployFiles` is evaluated against the **build container's filesystem | `container` | build container | Explicitly about the build-stage filesystem. |
| 38 | `develop-dev-server-triage` | `internal/content/atoms/develop-dev-server-triage.md:22` | action — its `zsc noop` idle container waits for `zerops_dev_server | `container` | runtime container | Conservative default for service-instance wording. |
| 39 | `develop-dev-server-triage` | `internal/content/atoms/develop-dev-server-triage.md:29` | # container env | `container` | KEEP-AS-IS | Environment qualifier, not the container-as-service-instance concept. |
| 40 | `develop-dev-server-triage` | `internal/content/atoms/develop-dev-server-triage.md:54` | # container env | `container` | KEEP-AS-IS | Environment qualifier, not the container-as-service-instance concept. |
| 41 | `develop-dynamic-runtime-start-container` | `internal/content/atoms/develop-dynamic-runtime-start-container.md:2` | id: develop-dynamic-runtime-start-container | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 42 | `develop-dynamic-runtime-start-container` | `internal/content/atoms/develop-dynamic-runtime-start-container.md:6` | environments: [container] | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 43 | `develop-dynamic-runtime-start-container` | `internal/content/atoms/develop-dynamic-runtime-start-container.md:10` | references-atoms: [develop-dev-server-reason-codes, develop-platform-rules-co... | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 44 | `develop-dynamic-runtime-start-container` | `internal/content/atoms/develop-dynamic-runtime-start-container.md:15` | Dev-mode dynamic-runtime containers start running `zsc noop` after | `containers` | runtime container | Already names the running service instance. |
| 45 | `develop-dynamic-runtime-start-container` | `internal/content/atoms/develop-dynamic-runtime-start-container.md:37` | `develop-platform-rules-common` for the deploy-replaces-container | `container` | new container | Replacement container created by deploy semantics. |
| 46 | `develop-dynamic-runtime-start-container` | `internal/content/atoms/develop-dynamic-runtime-start-container.md:39` | `develop-platform-rules-container`. See `develop-dev-server-reason-codes` | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 47 | `develop-dynamic-runtime-start-local` | `internal/content/atoms/develop-dynamic-runtime-start-local.md:14` | container. ZCP does not spawn local processes — use your harness's | `container` | runtime container | Conservative default for service-instance wording. |
| 48 | `develop-dynamic-runtime-start-local` | `internal/content/atoms/develop-dynamic-runtime-start-local.md:60` | **Do NOT use `zerops_dev_server`** — that tool is container-only (it | `container` | KEEP-AS-IS | Environment qualifier, not the container-as-service-instance concept. |
| 49 | `develop-dynamic-runtime-start-local` | `internal/content/atoms/develop-dynamic-runtime-start-local.md:61` | SSHes into Zerops dev containers). In local env it is not registered | `containers` | dev container | Mutable push-dev / SSHFS context or container-side workspace. |
| 50 | `develop-env-var-channels` | `internal/content/atoms/develop-env-var-channels.md:16` | \| Service-level env \| `zerops_env action="set"` \| Response's `restartedSer... | `containers` | runtime container | Running service instance generally. |
| 51 | `develop-first-deploy-asset-pipeline-container` | `internal/content/atoms/develop-first-deploy-asset-pipeline-container.md:2` | id: develop-first-deploy-asset-pipeline-container | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 52 | `develop-first-deploy-asset-pipeline-container` | `internal/content/atoms/develop-first-deploy-asset-pipeline-container.md:7` | environments: [container] | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 53 | `develop-first-deploy-asset-pipeline-container` | `internal/content/atoms/develop-first-deploy-asset-pipeline-container.md:32` | The build writes `public/build/manifest.json` into the dev container; | `container` | dev container | Mutable push-dev / SSHFS context or container-side workspace. |
| 54 | `develop-first-deploy-asset-pipeline-container` | `internal/content/atoms/develop-first-deploy-asset-pipeline-container.md:45` | route assets through the running server. Containers restart on every | `Containers` | new container | Replacement container created by deploy semantics. |
| 55 | `develop-first-deploy-asset-pipeline-local` | `internal/content/atoms/develop-first-deploy-asset-pipeline-local.md:20` | stage container after the first deploy. Any view rendering | `container` | runtime container | Running service instance generally. |
| 56 | `develop-first-deploy-asset-pipeline-local` | `internal/content/atoms/develop-first-deploy-asset-pipeline-local.md:35` | so the manifest lands on the stage container and the next request | `container` | runtime container | Running service instance generally. |
| 57 | `develop-first-deploy-execute` | `internal/content/atoms/develop-first-deploy-execute.md:12` | The Zerops container is empty until the deploy call lands, so probing | `container` | Zerops container | Broad first-introduction framing for an empty service instance. |
| 58 | `develop-first-deploy-execute` | `internal/content/atoms/develop-first-deploy-execute.md:13` | its subdomain or (in container env) SSHing into it first will fail or | `container` | KEEP-AS-IS | Environment qualifier, not the container-as-service-instance concept. |
| 59 | `develop-first-deploy-execute` | `internal/content/atoms/develop-first-deploy-execute.md:15` | batches build + container provision + start; expect 30–90 seconds for | `container` | runtime container | Conservative default for service-instance wording. |
| 60 | `develop-first-deploy-intro` | `internal/content/atoms/develop-first-deploy-intro.md:27` | (container) or a configured git remote (local), neither ready yet. | `container` | KEEP-AS-IS | Environment qualifier, not the container-as-service-instance concept. |
| 61 | `develop-first-deploy-promote-stage` | `internal/content/atoms/develop-first-deploy-promote-stage.md:7` | environments: [container] | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 62 | `develop-first-deploy-scaffold-yaml` | `internal/content/atoms/develop-first-deploy-scaffold-yaml.md:42` | the container — no stage pair to worry about. | `container` | runtime container | Conservative default for service-instance wording. |
| 63 | `develop-first-deploy-write-app` | `internal/content/atoms/develop-first-deploy-write-app.md:5` | environments: [container] | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 64 | `develop-first-deploy-write-app` | `internal/content/atoms/develop-first-deploy-write-app.md:9` | references-atoms: [develop-platform-rules-container] | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 65 | `develop-first-deploy-write-app` | `internal/content/atoms/develop-first-deploy-write-app.md:24` | checks call the service over the container's external interface; a | `container` | runtime container | Conservative default for service-instance wording. |
| 66 | `develop-first-deploy-write-app` | `internal/content/atoms/develop-first-deploy-write-app.md:34` | frameworks (Streamlit, Gradio, Vite, Jupyter) are wrong-in-container | `container` | KEEP-AS-IS | Environment qualifier, not the container-as-service-instance concept. |
| 67 | `develop-first-deploy-write-app` | `internal/content/atoms/develop-first-deploy-write-app.md:38` | flags need to be pinned to container-correct values. Pin each in | `container` | KEEP-AS-IS | Environment qualifier, not the container-as-service-instance concept. |
| 68 | `develop-first-deploy-write-app` | `internal/content/atoms/develop-first-deploy-write-app.md:44` | `develop-platform-rules-container` for the split. Runtime CLIs | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 69 | `develop-first-deploy-write-app` | `internal/content/atoms/develop-first-deploy-write-app.md:49` | handlers manage the container-side git state; running `git init` on | `container` | runtime container | Conservative default for service-instance wording. |
| 70 | `develop-first-deploy-write-app` | `internal/content/atoms/develop-first-deploy-write-app.md:51` | container-side `git add`. Recovery: `ssh <hostname> "sudo rm -rf | `container` | runtime container | Conservative default for service-instance wording. |
| 71 | `develop-http-diagnostic` | `internal/content/atoms/develop-http-diagnostic.md:6` | references-atoms: [develop-platform-rules-container] | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 72 | `develop-http-diagnostic` | `internal/content/atoms/develop-http-diagnostic.md:22` | var (numeric, not the projectId) injected into every container. Read | `container` | runtime container | Running service instance generally. |
| 73 | `develop-http-diagnostic` | `internal/content/atoms/develop-http-diagnostic.md:31` | `develop-platform-rules-container` for the mount-vs-SSH split. | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 74 | `develop-http-diagnostic` | `internal/content/atoms/develop-http-diagnostic.md:33` | something container-local (e.g. worker-only service with no HTTP | `container` | KEEP-AS-IS | Environment qualifier, not the container-as-service-instance concept. |
| 75 | `develop-implicit-webserver` | `internal/content/atoms/develop-implicit-webserver.md:11` | Apache or nginx is already running inside the container and auto-serves | `container` | runtime container | Conservative default for service-instance wording. |
| 76 | `develop-manual-deploy` | `internal/content/atoms/develop-manual-deploy.md:25` | container env, or your harness background-task primitive in local | `container` | KEEP-AS-IS | Environment qualifier, not the container-as-service-instance concept. |
| 77 | `develop-manual-deploy` | `internal/content/atoms/develop-manual-deploy.md:29` | # container env | `container` | KEEP-AS-IS | Environment qualifier, not the container-as-service-instance concept. |
| 78 | `develop-mode-expansion` | `internal/content/atoms/develop-mode-expansion.md:42` | Bootstrap leaves the existing service's code and container untouched, | `container` | runtime container | Conservative default for service-instance wording. |
| 79 | `develop-platform-rules-common` | `internal/content/atoms/develop-platform-rules-common.md:11` | - **Container user is `zerops`, not root.** Package installs need `sudo` | `Container` | runtime container | Conservative default for service-instance wording. |
| 80 | `develop-platform-rules-common` | `internal/content/atoms/develop-platform-rules-common.md:13` | - **Deploy = new container.** Local files in the running container are | `container` | new container | Replacement container created by deploy semantics. |
| 81 | `develop-platform-rules-common` | `internal/content/atoms/develop-platform-rules-common.md:13` | - **Deploy = new container.** Local files in the running container are | `container` | new container | Replacement container created by deploy semantics. |
| 82 | `develop-platform-rules-common` | `internal/content/atoms/develop-platform-rules-common.md:18` | - **Build ≠ run container.** Runtime packages → `run.prepareCommands`; | `container` | runtime container | Conservative default for service-instance wording. |
| 83 | `develop-platform-rules-container` | `internal/content/atoms/develop-platform-rules-container.md:2` | id: develop-platform-rules-container | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 84 | `develop-platform-rules-container` | `internal/content/atoms/develop-platform-rules-container.md:5` | environments: [container] | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 85 | `develop-platform-rules-container` | `internal/content/atoms/develop-platform-rules-container.md:8` | references-atoms: [develop-dynamic-runtime-start-container, develop-dev-serve... | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 86 | `develop-platform-rules-container` | `internal/content/atoms/develop-platform-rules-container.md:13` | Mount basics in `claude_container.md` (boot shim). Container-only | `container` | KEEP-AS-IS | Environment qualifier, not the container-as-service-instance concept. |
| 87 | `develop-platform-rules-container` | `internal/content/atoms/develop-platform-rules-container.md:13` | Mount basics in `claude_container.md` (boot shim). Container-only | `Container` | KEEP-AS-IS | Environment qualifier, not the container-as-service-instance concept. |
| 88 | `develop-platform-rules-container` | `internal/content/atoms/develop-platform-rules-container.md:16` | - **Mount caveats.** Deploy rebuilds the container from mount; no | `container` | new container | Replacement container created by deploy semantics. |
| 89 | `develop-platform-rules-container` | `internal/content/atoms/develop-platform-rules-container.md:23` | `develop-dynamic-runtime-start-container` for actions, parameters, | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 90 | `develop-platform-rules-local` | `internal/content/atoms/develop-platform-rules-local.md:13` | container-only. | `container` | KEEP-AS-IS | Environment qualifier, not the container-as-service-instance concept. |
| 91 | `develop-platform-rules-local` | `internal/content/atoms/develop-platform-rules-local.md:26` | is container-only; whatever dev command your framework provides | `container` | KEEP-AS-IS | Environment qualifier, not the container-as-service-instance concept. |
| 92 | `develop-push-dev-deploy-container` | `internal/content/atoms/develop-push-dev-deploy-container.md:2` | id: develop-push-dev-deploy-container | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 93 | `develop-push-dev-deploy-container` | `internal/content/atoms/develop-push-dev-deploy-container.md:7` | environments: [container] | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 94 | `develop-push-dev-deploy-container` | `internal/content/atoms/develop-push-dev-deploy-container.md:10` | references-atoms: [develop-deploy-modes, develop-deploy-files-self-deploy, de... | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 95 | `develop-push-dev-deploy-container` | `internal/content/atoms/develop-push-dev-deploy-container.md:15` | The dev container uses SSH push — `zerops_deploy` uploads the | `container` | dev container | Mutable push-dev / SSHFS context or container-side workspace. |
| 96 | `develop-push-dev-deploy-container` | `internal/content/atoms/develop-push-dev-deploy-container.md:18` | using ZCP's container-internal key. The response's `mode` is `ssh`; | `container` | runtime container | Conservative default for service-instance wording. |
| 97 | `develop-push-dev-deploy-local` | `internal/content/atoms/develop-push-dev-deploy-local.md:16` | path passed as `workingDir`) — there's no dev container to cross-deploy | `container` | dev container | Mutable push-dev / SSHFS context or container-side workspace. |
| 98 | `develop-push-dev-workflow-dev` | `internal/content/atoms/develop-push-dev-workflow-dev.md:8` | environments: [container] | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 99 | `develop-push-dev-workflow-dev` | `internal/content/atoms/develop-push-dev-workflow-dev.md:11` | references-atoms: [develop-dev-server-reason-codes, develop-platform-rules-co... | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 100 | `develop-push-dev-workflow-dev` | `internal/content/atoms/develop-push-dev-workflow-dev.md:28` | `zerops_deploy` first; on the rebuilt container use `action=start` | `container` | runtime container | Conservative default for service-instance wording. |
| 101 | `develop-push-dev-workflow-simple` | `internal/content/atoms/develop-push-dev-workflow-simple.md:8` | environments: [container] | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 102 | `develop-push-dev-workflow-simple` | `internal/content/atoms/develop-push-dev-workflow-simple.md:10` | references-atoms: [develop-platform-rules-container] | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 103 | `develop-push-dev-workflow-simple` | `internal/content/atoms/develop-push-dev-workflow-simple.md:16` | container auto-starts with its `healthCheck`, no manual server start: | `container` | runtime container | Conservative default for service-instance wording. |
| 104 | `develop-push-git-deploy` | `internal/content/atoms/develop-push-git-deploy.md:12` | Push committed code from the dev container to an external git repository (Git... | `container` | dev container | Mutable push-dev / SSHFS context or container-side workspace. |
| 105 | `develop-ready-to-deploy` | `internal/content/atoms/develop-ready-to-deploy.md:6` | environments: [container] | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 106 | `develop-ready-to-deploy` | `internal/content/atoms/develop-ready-to-deploy.md:29` | this when you need the container ACTIVE *before* deploying real code. | `container` | runtime container | Running service instance generally. |
| 107 | `develop-static-workflow` | `internal/content/atoms/develop-static-workflow.md:13` | 1. Edit files locally, or on the SSHFS mount in container mode. | `container` | KEEP-AS-IS | Environment qualifier, not the container-as-service-instance concept. |
| 108 | `develop-static-workflow` | `internal/content/atoms/develop-static-workflow.md:24` | runs in the Zerops build container at deploy time. Local builds are | `container` | build container | Explicitly about the build-stage filesystem. |
| 109 | `develop-static-workflow` | `internal/content/atoms/develop-static-workflow.md:28` | site has CI; `push-dev` for fast iteration on a dev container over SSH. | `container` | dev container | Mutable push-dev / SSHFS context or container-side workspace. |
| 110 | `develop-strategy-awareness` | `internal/content/atoms/develop-strategy-awareness.md:13` | `push-dev` (SSH self-deploy from the dev container), `push-git` | `container` | dev container | Mutable push-dev / SSHFS context or container-side workspace. |
| 111 | `develop-strategy-review` | `internal/content/atoms/develop-strategy-review.md:16` | from your workspace: container dev container → stage, or local CWD → | `container` | dev container | Mutable push-dev / SSHFS context or container-side workspace. |
| 112 | `develop-strategy-review` | `internal/content/atoms/develop-strategy-review.md:16` | from your workspace: container dev container → stage, or local CWD → | `container` | dev container | Mutable push-dev / SSHFS context or container-side workspace. |
| 113 | `develop-strategy-review` | `internal/content/atoms/develop-strategy-review.md:19` | builds triggered by a webhook or GitHub Actions. Container push-git | `Container` | runtime container | Conservative default for service-instance wording. |
| 114 | `export` | `internal/content/atoms/export.md:5` | environments: [container] | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 115 | `export` | `internal/content/atoms/export.md:108` | So they start before runtime containers. | `containers` | runtime container | Already names the running service instance. |
| 116 | `export` | `internal/content/atoms/export.md:193` | Later pushes (remote already on container): | `container` | dev container | Mutable push-dev / SSHFS context or container-side workspace. |
| 117 | `export` | `internal/content/atoms/export.md:208` | \| `PREREQUISITE_MISSING: requires committed code` \| the container doesn't h... | `container` | runtime container | Conservative default for service-instance wording. |
| 118 | `strategy-push-git-intro` | `internal/content/atoms/strategy-push-git-intro.md:31` | Before picking a trigger, confirm the git remote exists. In container env: | `container` | KEEP-AS-IS | Environment qualifier, not the container-as-service-instance concept. |
| 119 | `strategy-push-git-push-container` | `internal/content/atoms/strategy-push-git-push-container.md:2` | id: strategy-push-git-push-container | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 120 | `strategy-push-git-push-container` | `internal/content/atoms/strategy-push-git-push-container.md:6` | environments: [container] | `container` | KEEP-AS-IS | Metadata, atom id, atom reference, or YAML field; not prose terminology. |
| 121 | `strategy-push-git-push-container` | `internal/content/atoms/strategy-push-git-push-container.md:13` | The container has no user credentials, so pushes to the external git | `container` | runtime container | Conservative default for service-instance wording. |
| 122 | `strategy-push-git-push-container` | `internal/content/atoms/strategy-push-git-push-container.md:39` | is `PREREQUISITE_MISSING: requires committed code`, the container's | `container` | runtime container | Conservative default for service-instance wording. |
| 123 | `strategy-push-git-push-local` | `internal/content/atoms/strategy-push-git-push-local.md:42` | ## What's different from container | `container` | runtime container | Conservative default for service-instance wording. |
| 124 | `strategy-push-git-trigger-actions` | `internal/content/atoms/strategy-push-git-trigger-actions.md:50` | - Container env: `ssh {targetHostname} "grep -E '^\s*- setup:' /var/www/zerop... | `Container` | KEEP-AS-IS | Environment qualifier, not the container-as-service-instance concept. |
| 125 | `strategy-push-git-trigger-actions` | `internal/content/atoms/strategy-push-git-trigger-actions.md:57` | In local env, create this file directly in your repo; in container env | `container` | KEEP-AS-IS | Environment qualifier, not the container-as-service-instance concept. |
| 126 | `strategy-push-git-trigger-actions` | `internal/content/atoms/strategy-push-git-trigger-actions.md:85` | - Container env: follow `strategy-push-git-push-container` to commit the | `Container` | KEEP-AS-IS | Environment qualifier, not the container-as-service-instance concept. |
| 127 | `strategy-push-git-trigger-actions` | `internal/content/atoms/strategy-push-git-trigger-actions.md:85` | - Container env: follow `strategy-push-git-push-container` to commit the | `container` | runtime container | Conservative default for service-instance wording. |

## Summary

Total occurrences in cluster #1: 127
Per-canonical breakdown:
- dev container: 17
- runtime container: 38
- build container: 4
- Zerops container: 1
- new container: 6
- KEEP-AS-IS: 61

Atoms with the most cluster-#1 occurrences:
- `develop-first-deploy-write-app` — 8 occurrences
- `develop-platform-rules-container` — 7 occurrences
- `develop-dynamic-runtime-start-container` — 6 occurrences
- `develop-close-push-dev-dev` — 5 occurrences
- `develop-push-dev-deploy-container` — 5 occurrences
- `develop-close-push-git-container` — 4 occurrences
- `develop-first-deploy-asset-pipeline-container` — 4 occurrences
- `develop-http-diagnostic` — 4 occurrences

## Codex skipped during apply (uncertainty)

| # | atom-id | file:line | pre-edit phrase (≤80 chars context) | proposed canonical | reason skipped |
|---|---|---|---|---|---|
| 31 | `develop-close-push-git-container` | `internal/content/atoms/develop-close-push-git-container.md:11` | `### Closing the task — container + push-git` | runtime container | Heading names the container environment variant, not a runtime service instance. |
| 113 | `develop-strategy-review` | `internal/content/atoms/develop-strategy-review.md:19` | `builds triggered by a webhook or GitHub Actions. Container push-git` | runtime container | `Container push-git` names the container-environment push-git path; runtime container would mislabel the strategy variant. |
| 123 | `strategy-push-git-push-local` | `internal/content/atoms/strategy-push-git-push-local.md:42` | `## What's different from container` | runtime container | Section compares local env against container env, not against a service instance. |
| 127 | `strategy-push-git-trigger-actions` | `internal/content/atoms/strategy-push-git-trigger-actions.md:85` | `follow strategy-push-git-push-container to commit the` | runtime container | Occurrence is inside an atom id/reference target, not prose service-instance terminology. |
