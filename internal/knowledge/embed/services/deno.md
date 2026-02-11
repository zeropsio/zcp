# Deno on Zerops

## Keywords
deno, javascript, typescript, deno deploy, permissions, deno.json, deno.jsonc, deno task, secure runtime

## TL;DR
Deno on Zerops uses `deno@2` (latest) or `deno@1`. Use `deno task` commands defined in `deno.jsonc` for build and start, and deploy specific output files.

## Zerops-Specific Behavior
- Versions: 2, 1
- Base: Alpine (default)
- Config: `deno.jsonc` (tasks, imports)
- Working directory: `/var/www`
- No default port — must configure
- npm compatibility: `npm:` specifier for npm packages
- Permission model: Explicit flags required

## Configuration
```yaml
zerops:
  - setup: api
    build:
      base: deno@1
      buildCommands:
        - deno task build
      deployFiles:
        - dist
        - deno.jsonc
    run:
      start: deno task start
      ports:
        - port: 8000
          httpSupport: true
```

### Without deno.jsonc Tasks
```yaml
zerops:
  - setup: api
    build:
      base: deno@1
      buildCommands:
        - deno cache main.ts
      deployFiles: ./
    run:
      start: deno run --allow-net --allow-env --allow-read main.ts
      ports:
        - port: 8000
          httpSupport: true
```

## Gotchas
1. **Use `deno@1`**: Recipes use Deno 1.x, not 2.x
2. **Prefer `deno task`**: Define build/start scripts in `deno.jsonc` and use `deno task build` / `deno task start`
3. **Deploy specific files**: Deploy `dist` + `deno.jsonc`, not the entire directory
4. **Permissions are mandatory**: Without `--allow-net`, the app cannot open network ports — always set permissions
5. **`deno.jsonc` not `deno.json`**: Recipes use `.jsonc` (JSON with comments)
6. **npm compat via `npm:` prefix**: Import npm packages with `import express from "npm:express"` — works out of the box

## See Also
- zerops://services/_common-runtime
- zerops://services/nodejs
- zerops://services/bun
- zerops://examples/zerops-yml-runtimes
