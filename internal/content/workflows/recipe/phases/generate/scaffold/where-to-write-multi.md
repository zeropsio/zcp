# Where to write — multi-codebase plans

Multi-codebase plans (any plan with more than one dev mount) treat each codebase as an independent source tree with its own mount, its own `zerops.yaml`, and its own README. The mount count follows from `sharesCodebaseWith` in `plan.Research.Targets`:

- **Two dev mounts** when the plan is dual-runtime with a shared-codebase worker (the worker rides inside its host codebase), or single-runtime with a separate-codebase worker.
- **Three dev mounts** when the plan is dual-runtime with a separate-codebase worker.

The authoritative enumeration of zerops.yaml files, the setup count per file, and the `sharesCodebaseWith` pattern per shape lives in the `zerops-yaml/entry.md` atom downstream in this phase.

## Per-codebase writes

Each codebase gets its own README at publish time — the writer sub-agent at deploy authors all per-codebase READMEs after stage verification. At scaffold time, only the project tree, the on-mount artifacts, and the `zerops.yaml` (written last in this phase, after smoke-test) land on the mount.

## Scaffold each codebase inside its own mount

Framework scaffolders write config files — `tsconfig.json`, `package.json`, `.npmrc`, `.vscode/`, `.gitignore`, framework-specific dotfiles — into whatever directory they run from, and they treat the process working directory as the project root. Keeping scaffolds isolated per codebase requires these rules.

1. **SSH into the dev service whose codebase you are scaffolding.** The API codebase is scaffolded from `ssh apidev`; the frontend from `ssh appdev`; a separate-codebase worker from `ssh workerdev`. Every scaffolder, install, and build invocation happens inside that SSH session. Inside the container, the codebase lives at `/var/www` — the container's own path — so the canonical shape is `ssh {hostname} "cd /var/www && <scaffolder>"`.
2. **When the target dev service's base image lacks the scaffolder's runtime** (common for a static-base frontend service with no Node interpreter in the base), write the scaffold files directly via `Write`/`Edit` against the SSHFS mount at `/var/www/{hostname}/` rather than invoking the scaffolder on the container. File writes via the mount are the only safe zcp-side path; command execution against another service's codebase stays SSH-only.
3. **Scaffold directly into the mount's working directory.** Running scaffolders into a temporary directory and copying the result loses hidden files that the scaffolder created in its footprint.
4. **One scaffold per SSH session, matched to that session's codebase.** Scaffolder processes honor the working directory of the shell they run in; crossing sessions silently overwrites another codebase's config.
