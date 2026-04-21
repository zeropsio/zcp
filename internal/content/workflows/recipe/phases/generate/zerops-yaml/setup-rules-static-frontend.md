# Serve-only prod — dev-override rules for static frontends

Serve-only runtimes (`static`, standalone `nginx`, any future serve-only base) host no toolchain — `run.base: static` is a prod-only concern. Dev still needs a runtime that can host the framework's dev toolchain, and the built assets need to land directly at the server root.

## Dev uses a different run.base from prod

For `setup: dev`, pick a `run.base` that hosts the framework's dev toolchain — typically the same base that already exists under `build.base` for that setup (for example `nodejs@22` for a Vite or Svelte SPA whose prod is `static`). `run.base` may differ between setups inside the same zerops.yaml; the platform supports this and it is the intended pattern for serve-only prod artifacts. `deployFiles: [.]` still applies on dev regardless of the `run.base` choice.

## Prod deployFiles — the tilde-suffix convention

When `setup: prod` uses `run.base: static` (or `nginx`), the build step compiles assets into a subdirectory (for example `./dist/`). Nginx serves from `/var/www/`, so `deployFiles: ./dist` would ship the directory wrapper and files would land at `/var/www/dist/index.html` — a 404 at the site root. The tilde suffix (`./dist/~`) strips the parent directory prefix: files land directly at `/var/www/index.html`.

Use `./dist/~` (or the equivalent output path with tilde suffix) for every static-base prod setup. This is a platform convention; framework guides do not describe it.
