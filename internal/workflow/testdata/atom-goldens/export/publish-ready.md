---
id: export/publish-ready
atomIds: [export-intro, export-publish]
description: "Export workflow, bundle composed and validation clean — agent writes yamls, commits, pushes via git-push."
---
You are exporting a deployed runtime so a fresh Zerops project can reproduce the same infrastructure from a single git repo. The output is one repository at the chosen runtime's `/var/www` containing source code, `zerops.yaml` (build/run/deploy pipeline), and `zerops-project-import.yaml` (project + service definitions with `buildFromGit:` pointing back at the same repo). Re-import on a new project happens via `zcli project project-import zerops-project-import.yaml` or the dashboard.

The export workflow is a three-call narrowing — probe, generate, publish — and `zerops_workflow workflow="export"` carries each call. Some companion atoms refer to these as **Phase A** (probe — scope/variant prompts), **Phase B** (generate — classify/validate), and **Phase C** (publish — bundle + push).

## Pick the runtime

If the project has multiple runtime services, the first call returns a `scope-prompt` listing hostnames; pass `targetService=<hostname>` on the next call. For a project with a single runtime, the first call can already include `targetService` and skip this step.

## Pick the variant (pair modes only)

For `mode=standard` and `mode=local-stage` pairs, pick `variant=dev` (packages the dev hostname's tree + zerops.yaml) or `variant=stage` (packages the stage hostname's tree). Both bundle entries emit Zerops scaling `mode=NON_HA` — the destination project's topology Mode is established by ZCP's bootstrap at re-import, not embedded in the bundle.

Single-half source modes (`dev`, `simple`, `local-only`) skip this prompt — the variant is forced.

## What the next calls do

| Call | Inputs you add | Response |
|---|---|---|
| 2 | `targetService` + `variant` (if pair) | Generated bundle + per-env classification table (only env keys; values fetched separately via `zerops_discover` to keep secrets out of the response). |
| 3 | + `envClassifications` map (key → bucket per env) | `publish-ready` body with `importYaml`/`zeropsYaml` contents + `nextSteps` (write yamls, commit, push). |

If `/var/www/zerops.yaml` is missing or git remote is unconfigured, the response carries a status that walks the prereq (zerops.yaml scaffold or `git-push-setup`) instead — complete the prereq, then re-call export.

---

You are at `status="publish-ready"`. Bundle composed: classifications are accepted, `meta.GitPushState=configured`, schema validation clean. Three commands land the bundle: write the two yamls, commit, push via `zerops_deploy strategy="git-push"`.

## 1. Write the yamls into `/var/www`

Use the bundle bodies from the response — do NOT regenerate or hand-edit. The order of operations matters: write `zerops-project-import.yaml` first (the new file the bundle adds), then overwrite `zerops.yaml` only if the bundle's body differs from what's already in the repo.

```
ssh {targetHostname} "cat > /var/www/zerops-project-import.yaml" <<'EOF'
<bundle.importYaml verbatim>
EOF

ssh {targetHostname} "cat > /var/www/zerops.yaml" <<'EOF'
<bundle.zeropsYaml verbatim>
EOF
```

The second write is a pass-through when `bundle.zeropsYamlSource="live"` and the body is byte-identical to the live one — skip it to avoid noise in the commit. When `zeropsYamlSource="scaffolded"`, write it (the bundle generated a minimal yaml; review it first).

## 2. Commit

```
ssh {targetHostname} "cd /var/www && git add -A && git commit -m 'export: zerops-project-import.yaml + zerops.yaml for buildFromGit re-import'"
```

"Nothing to commit" → the yamls already match what's in the repo from a prior export. Continue to step 3 — there's still nothing to push if the working tree is clean and HEAD is already at the remote.

## 3. Push via `zerops_deploy strategy="git-push"`

```
zerops_deploy targetService="{targetHostname}" strategy="git-push"
```

The deploy command handles `git init`, `.netrc` configuration, and `git remote add origin <repoUrl>` internally — these are not separate manual steps. `bundle.repoUrl` is what `meta.RemoteURL` will be cached as after a successful push.

On error, read `failureClassification.category`:

| Category | Likely cause | Fix |
|---|---|---|
| `credential` | `GIT_TOKEN` missing or rejected | Re-run `zerops_workflow action="git-push-setup" service="{targetHostname}"` to refresh the token + scope. |
| `config` | The runtime container's `/var/www` does not have the bundle commit | Re-run step 2; verify `git log -1` shows the export commit. |
| `network` | Remote unreachable | Confirm `bundle.repoUrl` resolves; check VPN / firewall. |
| `build` / `start` | Re-import on the destination project failed at build/start | These do NOT come from the push — only from re-import. The push itself succeeded; the destination project's build/start logs are where to look. |

## 4. Verify the bundle re-imports

The push succeeds before the destination project actually builds. After the push lands, validate end-to-end by re-importing on a fresh project:

```
zcli project project-create --name <fresh-name> --org <your-org>
zcli project project-import --working-dir /tmp/<fresh-clone> --file zerops-project-import.yaml
```

The destination project should boot with the same managed services, the same envs (with classifications applied), and the runtime built from `buildFromGit:`. If the destination project fails at runtime with an unresolved `${...}` reference, the bundle missed an env — re-classify and re-publish.

## After publish

`record-deploy` is not required for export — the export workflow doesn't establish a develop session. The pushed remote is now the source of truth for both this project AND any downstream re-imports. Subsequent edits flow through `zerops_deploy strategy="git-push"` as usual.
