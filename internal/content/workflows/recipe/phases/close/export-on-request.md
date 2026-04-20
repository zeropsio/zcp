# Close — export and publish run only on explicit user request

Recipe creation ends at close complete. Export and publish are post-workflow CLI operations the user triggers when they want them. The workflow response after close carries both commands in `postCompletion.nextSteps[]` as reference material the agent MAY relay when the user asks — relaying is conditional on the user asking, never autonomous.

## The user-gate rule

Export and publish run only when the user explicitly asks for them. If the user has not asked, say nothing and do nothing about export or publish — the workflow is done and the next turn belongs to the user. The server-side close gate refuses `zcp sync recipe export` while close is incomplete (it returns a diagnostic); that gate is a defence-in-depth substrate detail, not a trigger — the gate opening does not mean the agent should run export.

## Export (local archive — relay only when asked)

Single-runtime recipe:

```
zcp sync recipe export {outputDir} --app-dir /var/www/appdev --include-timeline
```

Dual-runtime (API-first). Pass `--app-dir` once per distinct codebase — which directories to include depends on `worker.sharesCodebaseWith`:

- **Dual-runtime + shared worker** (worker shares the API): `apidev` + `appdev` — two `--app-dir`.
- **Dual-runtime + separate worker** (3-repo case, the default): `apidev` + `appdev` + `workerdev` — three `--app-dir`.
- **Single-app + separate worker**: `appdev` + `workerdev` — two `--app-dir`.
- **Single-app + shared worker** (Laravel / Rails / Django): `appdev` only.

Each `--app-dir` packs into its own subdirectory inside the archive named by `basename`. Duplicate basenames — rename one or pass a parent path.

If `TIMELINE.md` is missing, the command returns a prompt — write the TIMELINE documenting the session, then re-run export.

## Publish (open PR on zeropsio/recipes — relay only when asked)

Publish environments:

```
zcp sync recipe publish {slug} {outputDir}
```

Publish commits all environment folders as a PR on `zeropsio/recipes/{slug}/`.

## Per-codebase app repo push (relay only when asked)

Each codebase in the recipe maps to its own GitHub repo under `zerops-recipe-apps/`. The number of `create-repo` + `push-app` pairs equals the codebase count:

| Plan shape | Codebases | Publish calls |
|---|---|---|
| Single-runtime minimal (`app` + `db`) | 1 | `app` |
| Single-runtime + shared worker | 1 | `app` |
| Single-runtime + separate worker | 2 | `app`, `worker` |
| Dual-runtime + shared worker | 2 | `app`, `api` |
| Dual-runtime + separate worker (3-repo showcase) | 3 | `app`, `api`, `worker` |

Per-pair shape (`--repo-suffix` matches the codebase owner's hostname, and the `push-app` path is the mount for that codebase):

```
zcp sync recipe create-repo {slug} --repo-suffix {hostname}
zcp sync recipe push-app    {slug} /var/www/{hostname}dev --repo-suffix {hostname}
```

All pairs have no ordering constraint between each other — dispatch in parallel within a single message when the user runs them.

## Push knowledge + cache-clear + pull (relay only when asked)

```
zcp sync push recipes {slug}
zcp sync cache-clear {slug}
zcp sync pull recipes {slug}
```

The push opens a PR on the app repo with README fragments; cache-clear runs after the PR merges; pull retrieves the merged version.
