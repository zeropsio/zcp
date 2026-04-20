# platform-principles / routable-bind

HTTP servers bind to `0.0.0.0`. The Zerops L7 balancer routes to the container's network interface by its container IP; a server that bound to `localhost` or `127.0.0.1` is only reachable from inside the container itself and is invisible to the balancer.

## Correct bind forms by framework

- **Node http / Express** — `app.listen(port, '0.0.0.0')`.
- **Fastify** — `app.listen({ port, host: '0.0.0.0' })`.
- **NestJS** — `app.listen(port, '0.0.0.0')` on the `INestApplication` returned from `NestFactory.create`.
- **Vite dev server** — `server: { host: '0.0.0.0' }` in `vite.config.*`, or start with `--host 0.0.0.0`.
- **SvelteKit dev** — `vite dev --host 0.0.0.0`, or the `host: '0.0.0.0'` form in the vite config the kit wraps.
- **Go net/http** — `http.ListenAndServe(":"+port, ...)` — the empty host in `:PORT` binds to all interfaces.
- **Python (uvicorn/gunicorn)** — `--host 0.0.0.0` on the CLI, or `host="0.0.0.0"` as a kwarg.

The principle generalizes: whatever the framework's bind-host argument is named, set it to `0.0.0.0`. Defaults vary — some default to `0.0.0.0` already (Node `http.listen(port)` with no host), some default to `localhost` (Vite, SvelteKit dev, some Nest starter templates). Assume you must set it explicitly.

## Pre-attest before returning

```
ssh {host} "grep -rnE 'listen\\(.*(localhost|127\\.0\\.0\\.1)' /var/www/src 2>/dev/null; test $? -eq 1"
```

Zero-match (exit 1 from grep, test flips to 0) means no localhost bind. Any match is a bind you must change.
