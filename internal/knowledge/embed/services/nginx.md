# Nginx on Zerops

## Keywords
nginx, web server, reverse proxy, spa, static files, custom config, prerender, document root, nginx.conf

## TL;DR
Nginx on Zerops defaults to SPA routing (`$uri` → `$uri.html` → `$uri/index.html` → `/index.html` → 404), supports custom `nginx.conf` via template variables, and has built-in Prerender.io support.

## Zerops-Specific Behavior
- Default routing: SPA-friendly (tries `$uri`, `$uri.html`, `$uri/index.html`, `/index.html`, then 404)
- Template variable: `{{.DocumentRoot}}` — resolves to configured document root path
- Custom config: Full `nginx.conf` override supported
- Prerender.io: Built-in support via `PRERENDER_TOKEN` env var
- CORS: Configurable via `zerops.yaml`
- Custom headers: Configurable via `zerops.yaml`
- Redirects: Relative, absolute, wildcard with path/query preservation

## Configuration
```yaml
zerops:
  - setup: web
    build:
      base: nodejs@22
      buildCommands:
        - pnpm i && pnpm build
      deployFiles:
        - dist/~
      cache:
        - node_modules
    run:
      base: nginx@1
      documentRoot: .
      envVariables:
        PRERENDER_TOKEN: my-prerender-token
```

### Custom nginx.conf
```nginx
server {
    listen 80;
    root {{.DocumentRoot}};

    location / {
        try_files $uri $uri/ /index.html;
    }

    location /api {
        proxy_pass http://api:8080;
    }
}
```

## Gotchas
1. **SPA routing works by default**: No custom config needed for React/Vue/Angular — `/index.html` fallback is automatic
2. **`{{.DocumentRoot}}` template**: Must use this in custom nginx.conf — hardcoded paths won't work after document root changes
3. **Quote escaping in headers**: Custom header values with quotes need proper escaping in `zerops.yaml`
4. **`preservePath` with wildcards**: Requires trailing `/` in target URL

## See Also
- zerops://services/static
- zerops://services/php
- zerops://config/zerops-yml
