# Nginx on Zerops

Nginx runtime with SPA routing by default. Template variable `{{.DocumentRoot}}` resolves to configured document root. Prerender.io via `PRERENDER_TOKEN`.

### Default Behavior

SPA routing by default ($uri -> $uri.html -> $uri/index.html -> /index.html -> 404).

### Template Variables

`{{.DocumentRoot}}` resolves to configured document root.

### Prerender

Prerender.io: set `PRERENDER_TOKEN` env var for SEO-friendly rendering.
