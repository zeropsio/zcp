# React on Zerops

## Keywords
react, vite, create-react-app, spa, cra

## TL;DR
React SPA — build on Node.js, deploy `dist/~` to a static service. For SSR frameworks (Next.js, Remix), use their dedicated recipes.

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: nodejs@20
      buildCommands:
        - npm i
        - npm run build
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
- **`deployFiles: dist/~`** — tilde deploys directory contents to webroot, not the folder itself; without tilde the app is served from a subdirectory
- **`base: static` in run** — no `ports` or `start` needed; static service serves on port 80 automatically
- **SPA routing** — Angular Router / React Router with `PathLocationStrategy` requires a fallback to `index.html` in Zerops static service settings
- **Runtime environment variables are not supported** — static service has no process; inject config at build time or use a Node.js runtime instead
- **For SSR** — use the Next.js or Remix recipe; this recipe is for client-side SPA only
