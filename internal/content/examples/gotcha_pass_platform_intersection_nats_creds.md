---
surface: gotcha
verdict: pass
reason: platform-invariant-ok
title: "Zerops NATS creds vs nats.js URL-embedded auth (platform × library)"
---

> ### NATS credentials arrive as separate env vars, not URL-embedded
>
> **Symptom**: `nats.connect()` with `servers: process.env.QUEUE_CONNECTIONSTRING`
> returns an authentication-failed error on first publish even though the
> broker is reachable.
>
> **Mechanism**: Zerops injects NATS credentials as separate env vars
> (`queue_user`, `queue_password`) rather than embedding them in the
> connection string. `nats.js@2.x` silently strips URL-embedded credentials
> in some code paths and ignores them even when present — it expects the
> `user` / `pass` options on the connect object.
>
> **Rule**: pass credentials as separate `user` / `pass` options in the
> `nats.connect({...})` call; do not rely on URL-embedded auth. See
> `zerops_knowledge topic=env-var-model` for the platform side and the
> `nats.js` v2 docs for the library side.

**Why this passes the gotcha test.**
- Both sides named: platform (separate-var credential injection) ×
  library (nats.js URL-credential-stripping). Neither side alone would
  surface the symptom.
- Concrete symptom with a specific error class.
- Rule is actionable (the exact option names to pass).

Spec §7 classification: platform × framework intersection. Route to
gotcha naming both sides clearly.
