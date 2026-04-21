# platform-principles / proxy-trust

Set framework-level trust for one proxy hop so the L7 balancer's `X-Forwarded-For` and `X-Forwarded-Proto` headers are honored. Without trust, `req.ip` returns the balancer's IP (not the client's) and `req.protocol` returns the internal scheme (not the externally-observed one). This breaks rate-limiting by IP, audit logging, protocol-sensitive redirects, and anything that reads the caller's identity from the request.

## Framework forms

- **Express** — `app.set('trust proxy', 1)`. The value `1` means "trust one hop"; this matches the single Zerops L7 balancer in front of the container.
- **Fastify** — construct with `Fastify({ trustProxy: true })` (or pass an IP / CIDR / number of hops to narrow).
- **NestJS** — Nest wraps Express or Fastify. For the Express adapter, call `app.set('trust proxy', 1)` on the underlying instance after `NestFactory.create`. For the Fastify adapter, pass `trustProxy: true` into the Fastify options when constructing the adapter.
- **Koa** — set `app.proxy = true`.
- **Go net/http with reverse-proxy-aware middleware** — configure your middleware (for example `httputil` custom handlers) to read `X-Forwarded-For` as the client IP.
- **Python (Flask / FastAPI behind uvicorn)** — enable `--proxy-headers` on uvicorn, or use `werkzeug.middleware.proxy_fix.ProxyFix` for Flask.

## Why "one hop"

Setting trust too wide (trusting any proxy in the chain) lets a caller inject a fake client IP via the `X-Forwarded-For` header. Setting it to `1` trusts exactly one hop — the Zerops balancer — which rewrites the header from a known upstream. This is the correct bound for the Zerops runtime.

## Pre-attest before returning

```
ssh {host} "grep -rnE 'trust[ _]proxy' /var/www/src 2>/dev/null | grep -q ."
```

Exit 0 means the trust directive is present somewhere in source. Inspect the match to confirm the value is `1` (or equivalent framework form), not a wide setting.
