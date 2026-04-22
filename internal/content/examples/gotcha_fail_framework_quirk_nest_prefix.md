---
surface: gotcha
verdict: fail
reason: framework-quirk
title: "setGlobalPrefix collides with @Controller decorators (v28 apidev gotcha #5)"
---

> ### `app.setGlobalPrefix('api')` collides with `@Controller('api/...')` decorators
>
> After setting `app.setGlobalPrefix('api')` in `main.ts`, controllers decorated
> with `@Controller('api/users')` produced routes at `/api/api/users`. Remove
> the `api/` prefix from the decorator OR drop `setGlobalPrefix`.

**Why this fails the gotcha test.**
This is pure NestJS framework behavior. Zerops has no involvement — the same
collision happens on any host, any deployment platform. A porter using NestJS
will hit this regardless of where they deploy. Spec §7 classification:
framework-quirk → DISCARD from recipe gotcha surface.

**Correct routing**: belongs in framework docs or in a code comment next to
the affected controller, not on the Zerops-recipe knowledge-base surface.
