---
surface: gotcha
verdict: pass
reason: platform-invariant-ok
title: "Env-var self-shadow — never name a key the same on both sides of a ref"
---

> ### Self-shadow trap on `run.envVariables`
>
> **Symptom**: `${db_hostname}` reaches the container as the literal
> string `${db_hostname}` (8 characters, including the dollar-brace
> wrapping), not as the resolved hostname. App boot fails when the DB
> driver tries to parse that string as a hostname — varies by language
> (Node `getaddrinfo ENOTFOUND`, libpq parse error, JDBC malformed URL,
> etc.).
>
> **Mechanism**: a cross-service value is reached only by an explicit
> `${host_var}` ref. Declaring `db_hostname: ${db_hostname}` in
> `run.envVariables` makes the destination key identical to the source
> ref — the left-hand `db_hostname` resolves first and points back at
> the literal `${db_hostname}` string, so that literal is what the
> container receives. The line is redundant AND it breaks the container
> env.
>
> **Rule** (see `zerops_knowledge topic=env-var-model`): never declare
> `key: ${key}` in `run.envVariables`. For legitimate renames, use a
> different key: `DB_HOST: ${db_hostname}`.

**Why this passes the gotcha test.**
- Mechanism is platform-invariant (env-var interpolation semantics).
- Symptom is concrete (literal `${db_hostname}` string in the
  container's OS env, language-specific parse failure on first use).
- Platform rule is cited from the `env-var-model` guide, not invented.
- A reader who read Zerops docs AND the framework docs would still be
  surprised because the same-name self-reference interaction is
  non-obvious.

Spec §7 classification: platform-invariant. Route to gotcha, cite the
guide by name, use the guide's framing.
