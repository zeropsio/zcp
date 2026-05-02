---
id: scaffold-zerops-yaml
priority: 6
phases: [export-active]
exportStatus: [scaffold-required]
environments: [container]
title: "Scaffold a minimal zerops.yaml when /var/www is missing one"
---
You hit `status="scaffold-required"`. The chosen container's `/var/www/zerops.yaml` (or `.yml`) is missing or empty. Bundle composition can't continue without a `setup:` block to reference at re-import — write a minimal valid yaml first, commit it, then re-call export.

## Detect what to put in the setup

The runtime container's image type tells you the build/run base:

```
zerops_discover hostname="{targetHostname}"
```

The response includes `type` (e.g. `nodejs@22`, `php-apache@8.4`, `static`). Map type to a minimal block:

| Runtime `type` | Minimal `build.base` | Minimal `run.base` | Build commands |
|---|---|---|---|
| `nodejs@22` | `nodejs@22` | `nodejs@22` | `npm install`, `npm run build` (drop the build line if there's no build script) |
| `php-apache@8.4` | `php@8.4` | `php-apache@8.4` | `composer install` |
| `static` | `nodejs@22` (or whatever produces `dist/`) | `static` | `npm install`, `npm run build` |
| `python@3.12` | `python@3.12` | `python@3.12` | `pip install -r requirements.txt` |
| `go@1` | `go@1` | `go@1` | `go build -o app ./...` |
| `bun@1.2` | `bun@1.2` | `bun@1.2` | `bun install`, `bun run build` |

## Write the file

The `setup:` name should match the runtime hostname for consistency — the export handler's setup-name resolver tries `{targetHostname}` first, then prefix-stripped variants. Use `{targetHostname}` here.

```
ssh {targetHostname} "cat > /var/www/zerops.yaml" <<'EOF'
zerops:
  - setup: {targetHostname}
    build:
      base: <build-base>
      buildCommands:
        - <build-command-1>
        - <build-command-2>
      deployFiles: ["./"]
    run:
      base: <run-base>
      ports:
        - port: <port>
          httpSupport: true
EOF
```

The `ports` entry is required for runtimes that serve HTTP — match the value from `zerops_discover` response's `ports` array. Static-runtime services and worker-style runtimes can omit `ports`.

## Commit the scaffolded yaml

```
ssh {targetHostname} "cd /var/www && git add zerops.yaml && git commit -m 'chore: scaffold zerops.yaml for export bundle'"
```

This commit lands inside the bundle when the export publishes — the destination project sees the same `zerops.yaml` you scaffolded.

## Re-call export

```
zerops_workflow workflow="export" \
  targetService="{targetHostname}" \
  variant="<your-pick>"
```

The handler re-reads `/var/www/zerops.yaml`, finds the new setup block, and proceeds to Phase B (classify-prompt) on the next call. If the runtime needs a different `setup:` name than `{targetHostname}`, edit the scaffolded yaml before re-running — the resolver also matches pair-suffix-stripped names (`appdev` → setup `app` → setup `dev`) and an exact hostname match.

## When scaffolding is not the right call

The handler chains here on an empty yaml, but the underlying problem may be a wrong path or a misconfigured working directory. Before scaffolding from scratch, sanity-check:

```
ssh {targetHostname} "ls -la /var/www/"
ssh {targetHostname} "find /var/www -maxdepth 3 -name zerops.yaml -o -name zerops.yml"
```

If a yaml exists at a non-standard path, copy or symlink it to `/var/www/zerops.yaml` rather than scaffolding a duplicate.
