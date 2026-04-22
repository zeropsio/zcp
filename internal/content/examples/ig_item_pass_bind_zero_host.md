---
surface: ig-item
verdict: pass
reason: concrete-action-ok
title: "Bind to 0.0.0.0 — Zerops L7 balancer reachability"
---

> ### Bind the HTTP server to `0.0.0.0`, not `127.0.0.1`
>
> **Why**: Zerops runtime containers sit behind an L7 balancer that
> reaches the service over the project VXLAN. An app listening on
> `localhost` / `127.0.0.1` is invisible to the balancer and the
> service hangs on readiness probes.
>
> **Change to make** (TypeScript / NestJS example):
>
> ```typescript
> // before
> await app.listen(3000);
>
> // after
> await app.listen(3000, '0.0.0.0');
> ```
>
> The same shape applies to every framework — bind to all interfaces,
> not loopback. See `zerops_knowledge topic=http-support` for L7
> balancer details.

**Why this passes the IG-item test.**
- Concrete action with a code diff the porter copies.
- One-sentence reason tied to a Zerops mechanism (L7 balancer + VXLAN).
- Platform-guide citation so the porter can read further.
- Framework-agnostic principle with a framework-specific example —
  clearly extractable to other frameworks.

Spec §4 classification: IG item, concrete-action. Include a fenced
code block, name the mechanism, cite the guide.
