# CODEBASE_IG — examples

## Pass — concrete porter action + Zerops reason + diff

```
### 4. Bind to 0.0.0.0

Zerops routes traffic to the container via the L7 balancer on the
container's VXLAN IP. Binding to `127.0.0.1` makes the app unreachable.

```js
// server.js
// Before: app.listen(3000, '127.0.0.1')
app.listen(process.env.PORT, '0.0.0.0')
```
```

## Fail — describes the recipe's own scaffold code

```
### 4. Our api.ts wrapper

The recipe's `api.ts` helper centralizes fetch calls and handles SPA
fallback detection. Import it from `./lib/api.ts` and use it for every
request.
```

Fails the "porter bringing their own code" test: the porter has no
`api.ts`. The underlying fact (how the SPA fallback breaks) may belong
here; the helper's existence belongs in code comments, not IG.
