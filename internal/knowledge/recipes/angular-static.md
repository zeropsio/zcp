# Angular Static Site on Zerops

Angular application built with Angular CLI and deployed as a static site.

## Keywords
angular, static, spa, typescript, javascript

## TL;DR
Angular SPA built with `ng build` — builds on Node.js 20 and deploys the browser output directory to a static service.

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
- **Angular build output path varies** — default is `dist/<project-name>/browser/`; adjust `deployFiles` to match your `angular.json` `outputPath` setting
- **Deploy with tilde (`~`)** — deploys directory contents to webroot, not the folder itself
- **Builds on Node.js, runs on static** — Node.js is only used at build time; the runtime is a lightweight static file server
- **SPA routing** — for Angular Router with `PathLocationStrategy`, configure a fallback to `index.html` in Zerops static service settings
- **Uses npm** — Angular CLI projects typically use npm; switch to pnpm if your project has a `pnpm-lock.yaml`
