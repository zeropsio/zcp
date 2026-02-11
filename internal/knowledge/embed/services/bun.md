# Bun on Zerops

## Keywords
bun, javascript, typescript, bun runtime, bunx, bun.lockb, fast js, bun install

## TL;DR
Bun on Zerops is a drop-in Node.js replacement with native TypeScript support; uses `bun.lockb` lockfile and `bunx` instead of `npx`. Deploy compiled output, not node_modules.

## Zerops-Specific Behavior
- Versions: 1.2, 1.1.34 (Ubuntu only), nightly, canary
- Base: Alpine (default)
- Package manager: `bun install` (npm-compatible)
- Lockfile: `bun.lockb` (binary format)
- Working directory: `/var/www`
- No default port — must configure
- npx replacement: `bunx`

## Configuration
```yaml
zerops:
  - setup: api
    build:
      base: bun@1.1
      buildCommands:
        - bun install
        - bun run build
      deployFiles:
        - package.json
        - dist
      cache:
        - node_modules
    run:
      start: bun run start:prod
      ports:
        - port: 3000
          httpSupport: true
```

## Gotchas
1. **Don't deploy node_modules**: Unlike Node.js, Bun recipes deploy only `package.json` and `dist` — not `node_modules`
2. **npm-compatible but not identical**: Most npm packages work, but some with native Node.js APIs may not
3. **`bun.lockb` is binary**: Cannot be manually edited or diffed — regenerate with `bun install`
4. **Use `bun run start:prod`**: Recipes use npm scripts for production start, not direct file execution

## See Also
- zerops://services/_common-runtime
- zerops://services/nodejs
- zerops://services/deno
- zerops://examples/zerops-yml-runtimes
