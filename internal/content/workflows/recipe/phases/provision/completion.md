# Provision — completion summary

Provision closes with the plan's workspace shape fully realised on the platform:

- Every runtime target has a `{name}dev` + `{name}stage` pair (with the shared-codebase-worker exception where the worker gets stage only).
- Every managed service is RUNNING and its env var catalog is recorded.
- Every dev mount is present and its `.git/` is container-side-initialised with an initial commit.
- Dual-runtime recipes have their project-level `DEV_*` + `STAGE_*` URL constants set.
- Framework secrets are placed at project level or per-service per the research decision.

Once the attestation lands, provision is closed. Generate starts with the workspace state this phase produced: dev containers RUNNING on their base images, SSHFS mounts writable, env-var catalog recorded, git repositories ready for the first scaffolded commit. No provisioning work happens after this point — any later service shape change (project-level URL re-sets, additional managed services) is a separate explicit operation, not a provision-step follow-up.
