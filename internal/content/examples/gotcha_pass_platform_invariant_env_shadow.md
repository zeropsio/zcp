---
surface: gotcha
verdict: pass
reason: platform-invariant-ok
title: "Env-var self-shadow — cross-service vars auto-inject (post-v28 correction)"
---

> ### Self-shadow trap on `run.envVariables`
>
> **Symptom**: `${db_hostname}` resolves to an empty string at runtime
> even though the `db` service is healthy; containers crash in boot with
> `ECONNREFUSED` on hostname resolution.
>
> **Mechanism**: Zerops auto-injects cross-service env vars
> project-wide — declaring `db_hostname: ${db_hostname}` in
> `run.envVariables` creates a key that references itself, and the
> platform resolves it to empty. The line is redundant AND it breaks
> the container env.
>
> **Rule** (see `zerops_knowledge topic=env-var-model`): never declare
> `key: ${key}` in `run.envVariables`. For legitimate renames, use a
> different key: `DB_HOST: ${db_hostname}`.

**Why this passes the gotcha test.**
- Mechanism is platform-invariant (env-var auto-inject semantics).
- Symptom is concrete (empty string, `ECONNREFUSED`, boot crash).
- Platform rule is cited from the `env-var-model` guide, not invented.
- A reader who read Zerops docs AND the framework docs would still be
  surprised because the auto-inject + self-reference interaction is
  non-obvious.

Spec §7 classification: platform-invariant. Route to gotcha, cite the
guide by name, use the guide's framing.
