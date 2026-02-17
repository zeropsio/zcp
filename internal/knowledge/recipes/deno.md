# Deno on Zerops

Deno runtime. Ubuntu only — no Alpine support.

## Keywords
deno, typescript, javascript, ubuntu, web server

## TL;DR
Deno runtime on Ubuntu — requires `os: ubuntu` in both build and run, explicit permission flags.

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: deno@latest
      os: ubuntu
      buildCommands:
        - deno cache main.ts
      deployFiles: /
    run:
      os: ubuntu
      ports:
        - port: 8000
          httpSupport: true
      start: deno run --allow-net --allow-env --allow-read main.ts
```

## import.yml
```yaml
services:
  - hostname: app
    type: deno@2
    enableSubdomainAccess: true
```

## Gotchas
- **Ubuntu only** — Deno has no Alpine support, must set `os: ubuntu` in both build and run
- **Explicit permission flags** — Deno requires `--allow-net --allow-env --allow-read` (or `--allow-all` for dev)
- **Bind 0.0.0.0** — use `Deno.serve({ port: 8000, hostname: "0.0.0.0" }, handler)`
- **Cache dependencies in build** — `deno cache main.ts` pre-downloads dependencies
