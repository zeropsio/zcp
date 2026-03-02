# React Static on Zerops

React static site built with Vite. Client-side SPA, no server-side rendering.

## Keywords
react, static, vite, ssg, spa, javascript, typescript

## TL;DR
React static site with Vite — build to `dist/` and deploy `dist/~` to a static service.

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: nodejs@20
      buildCommands:
        - pnpm i
        - pnpm build
      deployFiles: dist/~
      cache: node_modules
    run:
      base: static
```

## import.yml
```yaml
services:
  - hostname: app
    type: static
    enableSubdomainAccess: true
```

## Gotchas
- **Deploy `dist/~`** (tilde deploys directory contents to webroot, not the `dist/` folder itself)
- **No `ports` or `start` needed** — the `static` base serves files on port 80 automatically
- **Build command uses `tsc -b && vite build`** by default in the repo — simplify to `pnpm build` in zerops.yml and let `package.json` scripts handle the details
- **For React SSR** (Next.js, Remix), use the `nextjs-ssr` or `remix-nodejs` recipes instead
- **Build cache** should include `node_modules` for faster rebuilds
