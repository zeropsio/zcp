---
id: export
priority: 2
phases: [export-active]
environments: [container]
title: "Export — turn a deployed service into a re-importable git repo"
references-fields: [ops.ExportResult.ExportYAML, ops.ExportResult.Services, ops.DeployResult.Status, ops.DeployResult.Warnings, ops.DeployResult.FailureClassification]
---

# Export procedure

Produce `import.yaml` in the service's git repo so a new project can
reproduce this infrastructure with:

```
zcli project project-import import.yaml
```

For `buildFromGit` imports, services need only `type`, `zeropsSetup`,
`buildFromGit`, and platform settings (scaling, subdomain, envs). Do NOT
copy build/run/deploy pipeline fields — those live in the repo's
zerops.yaml.

**Convert the task list below into your task tracker. Execute in order.**

## Tasks

1. Fetch raw state
2. Get repo URL
3. Filter services
4. Enrich runtime services
5. Enrich managed services
6. Scrub defaults
7. Process project envVariables
8. Add preprocessor header if needed
9. Write import.yaml + commit
10. Push via zerops_deploy
11. Report

---

## 1. Fetch raw state

```
zerops_export
zerops_discover includeEnvs=true
```

Save both outputs — referenced in tasks 3–7.

## 2. Get repo URL

```
ssh {targetHostname} "cd /var/www && git remote get-url origin 2>/dev/null"
```

Empty output → ask user: "Repo URL?" You'll pass it to `zerops_deploy` on
the first push.

## 3. Filter services

From `exportYaml.services[]` keep only:
- runtimes deployed from this repo
- their managed dependencies (db, cache, storage)

Drop everything else. If the set is unclear, list all services to the user
and ask which to include.

## 4. Enrich runtime services

For each kept runtime service, add these fields (and nothing else):

```
buildFromGit: {repoUrl}
zeropsSetup: <setup name>
enableSubdomainAccess: true    # only if discover.subdomainEnabled == true
```

`zeropsSetup` picks a `setup:` block from the repo's zerops.yaml. Standard
mapping:

| Hostname suffix | zeropsSetup |
|---|---|
| `*dev` (iteration) | `dev` |
| `*stage`, `*prod` | `prod` |
| `*worker` | `worker` |

If the repo's zerops.yaml uses non-standard names, check:

```
ssh {targetHostname} "grep -E '^\s*- setup:' /var/www/zerops.yaml"
```

Map by purpose: the setup with `zsc noop` / no readinessCheck → dev; the
one with `deployFiles` + `readinessCheck` → prod.

`enableSubdomainAccess` is lossy in platform export — reinstate from
discover output.

## 5. Enrich managed services

For each kept managed service (db, storage, cache, object-storage), add:

```
priority: 10
```

So they start before runtime containers.

## 6. Scrub defaults

Drop `verticalAutoscaling` fields that match `corePackage` defaults. For
`corePackage: LIGHT`:

| Drop if equal to | Keep |
|---|---|
| `minCpu: 1` | `minRam` (user-intentional) |
| `maxCpu: 8` | `cpuMode` if `DEDICATED` |
| `maxRam: 48` | |
| `minDisk: 1` | |
| `maxDisk: 250` | |

Drop from project section:
- `envIsolation`, `sshIsolation`, `sharedIpv4`
- `corePackage: LIGHT` (default). Keep `corePackage: SERIOUS`.

## 7. Process project envVariables

For each key in `exportYaml.project.envVariables`:

| Pattern | Action |
|---|---|
| `ZCP_*` | drop |
| Test fixtures (`PROBE_*`, `TEST_*`, `VERIFY_*`) | ask user → usually drop |
| Known secret key (table below) with random-looking value | replace with preprocessor directive |
| `${host_KEY}` cross-service reference | keep as-is |
| Unknown key + high-entropy value (≥32 chars, no spaces/URL) | ask user if preprocessor |
| Ordinary config (URL, flag, public constant) | keep literal |

Known secret keys (case-insensitive, suffix-tolerant):

| Key | Length | Directive |
|---|---|---|
| `APP_KEY` (Laravel) | 32 | `<@generateRandomString(<32>)>` |
| `SECRET_KEY_BASE` (Rails) | 64 | `<@generateRandomString(<64>)>` |
| `JWT_SECRET`, `SESSION_SECRET` | 64 | `<@generateRandomString(<64>)>` |
| `ENCRYPTION_KEY`, `CIPHER_KEY` | 32 | `<@generateRandomString(<32>)>` |
| `SECRET_KEY` (Django/Flask) | 50 | `<@generateRandomString(<50>)>` |

Other directives: `generateRandomInt(<min>,<max>)`,
`generateRandomBytes(<N>)`, `generateED25519Key(<name>)`,
`pickRandom(<a>,<b>,...)`, `setVar(<key>,<val>)`, `getVar(<key>)`.
Modifiers: `sha256`, `sha512`, `bcrypt`, `argon2id`, `toHex`, `upper`,
`lower`.

**Service-level envs**: trust platform export. It emits `envSecrets:` only
for user-set service envs (platform-injected keys like `hostname`, `PATH`,
`port`, `connectionString`, `password`, `ZEROPS_DEBUG_*` are already
omitted). Keep what's there as `envSecrets:` (never `envVariables:` at
service level — import API silently drops those). For managed services,
drop any surviving `envSecrets:` — platform regenerates credentials.

## 8. Add preprocessor header if needed

If the final YAML contains any `<@...>` directive, prepend as **line 1**:

```
#zeropsPreprocessor=on
```

Otherwise omit. Header must be line 1 or platform skips expansion.

## 9. Write import.yaml + commit

```
ssh {targetHostname} "cat > /var/www/import.yaml" <<'YAML'
<final yaml>
YAML

ssh {targetHostname} "cd /var/www && git add -A && git commit -m 'export: add import.yaml'"
```

Overwrite any existing import.yaml. "Nothing to commit" → continue to push.

## 10. Push via zerops_deploy

First push / re-point remote:

```
zerops_deploy targetService="{targetHostname}" strategy="git-push" remoteUrl="{repoUrl}" branch="main"
```

Later pushes (remote already on dev container):

```
zerops_deploy targetService="{targetHostname}" strategy="git-push"
```

The response's `status` confirms success; `warnings[]` surface
non-fatal issues. Do NOT run `git init`, `git config user.*`, or
`git remote add` yourself — the deploy tool owns the git-push shape.

On error, read `failureClassification`:

| `category` | When | Fix | Then |
|---|---|---|---|
| `credential` | `likelyCause` mentions `GIT_TOKEN` | ask user for token (GitHub: Contents R/W; GitLab: write_repository), run `zerops_env action="set" project=true variables=["GIT_TOKEN={token}"]` | retry task 10 |
| `config` | `likelyCause` mentions committed code | the runtime container doesn't have the `import.yaml` commit — go back to task 9 and ensure `ssh {targetHostname} "cd /var/www && git add import.yaml && git commit -m '...'"` actually committed | retry task 10 |

Optional — persist strategy in service meta so future guidance knows:

```
zerops_workflow action="strategy" strategies={"{targetHostname}":"push-git"}
```

## 11. Report

Tell the user:

```
Exported {targetHostname} to {repoUrl} (branch main).

Re-import on a new project:
  zcli project project-import import.yaml

Iterate:
  ssh {targetHostname} 'cd /var/www && ...'                          # edit
  zerops_deploy targetService="{targetHostname}" strategy="git-push" # push
```
