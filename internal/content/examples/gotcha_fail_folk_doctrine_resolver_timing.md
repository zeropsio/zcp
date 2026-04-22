---
surface: gotcha
verdict: fail
reason: folk-doctrine
title: "env-shadow resolver-timing fabrication (v28 workerdev gotcha #1)"
---

> ### The API codebase avoided the symptom because its resolver path happened to interpolate before the shadow formed; do not rely on that.
>
> The API codebase handled self-shadow correctly because its env-var
> resolver ran before the shadow formed. Workers, whose resolver runs
> later in the boot sequence, get a broken `${db_hostname}` value. Do not
> rely on load ordering to avoid self-shadow.

**Why this fails the gotcha test.**
The trap (env-shadow from declaring `db_hostname: ${db_hostname}` in
`run.envVariables`) is real. The **explanation is invented.** Both apidev
and workerdev shipped identical patterns; both were broken. The author
had access to the `env-var-model` guide (which covers self-shadow
explicitly) and chose to write a new mental model instead of citing it.

**Correct routing**: gotcha PASS if rewritten to cite `env-var-model`:
cross-service vars auto-inject project-wide; never declare `key: ${key}`
in `run.envVariables` — the line is redundant AND it breaks the
container env. See the `gotcha_pass_platform_invariant_env_shadow.md`
example in this bank for the fix.
