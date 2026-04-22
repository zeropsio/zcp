---
surface: gotcha
verdict: fail
reason: scaffold-decision
title: "api.ts content-type check catches SPA-fallback bugs (v28 appdev gotcha #4)"
---

> ### `api.ts`'s `application/json` content-type check is what catches the SPA-fallback class of bug
>
> When the Nginx SPA fallback returns `200 text/html` for a missed `/api/*`
> route, our `api.ts` wrapper notices the mismatch and surfaces it as a real
> error instead of a JSON parse failure downstream.

**Why this fails the gotcha test.**
`api.ts` is the recipe's own scaffold helper. A porter bringing their own
Svelte app has no `api.ts` — the bullet teaches nothing they can apply.
Spec §7 classification: scaffold-decision → this belongs on a different
surface.

**Correct routing**: split into two pieces —
- The **principle** (Nginx SPA fallback returns 200 text/html on unmatched
  `/api/*` routes; client must detect content-type before parsing as JSON)
  → IG item.
- The **specific implementation** (how our `api.ts` wrapper checks it) →
  inline code comment in `api.ts`. Neither belongs as a gotcha.
