# Angular Static Site on Zerops

Angular application built with Angular CLI and deployed as a static site.

## Keywords
angular, angular-cli, ng-build, angular-router, standalone-components

## TL;DR
Angular SPA — build with `ng build` on Node.js 20, deploy `dist/<project>/browser/~` to a static service.

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: nodejs@20
      buildCommands:
        - npm i
        - npm run build
      deployFiles: dist/app/browser/~
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
- **Build output path varies** — default is `dist/<project-name>/browser/`; adjust `deployFiles` to match your `angular.json` `outputPath` setting
- **Tilde (`~`) required** — `dist/app/browser/~` deploys directory contents to webroot; without tilde the app is nested under a subdirectory path
- **`base: static` in run** — no `ports` or `start` needed; static service serves on port 80 automatically
- **SPA routing** — Angular Router with `PathLocationStrategy` (HTML5 history API) requires a fallback to `index.html` in Zerops static service settings; `HashLocationStrategy` works without it
- **Runtime environment variables are not supported** — static service has no process; inject config at build time via `environment.ts` files or use Angular's APP_INITIALIZER with a config endpoint
