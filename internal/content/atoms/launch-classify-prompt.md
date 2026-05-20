---
id: launch-classify-prompt
priority: 2
phases: [launch-production-active]
title: "Launch classify — bucket source envs before production publish"
references-fields: []
---

### Launch classify — bucket source envs before production publish

You are at `status="classify-prompt"`. The launch composer needs every source `project.envVariables` entry classified into one of four buckets — `infrastructure`, `auto-secret`, `external-secret`, `plain-config` — before it can emit the production import bundle.

**Call shape — `action="start"` always.** Launch-production is stateless multi-call narrowing: every advance is another `zerops_workflow action="start" workflow="launch-production"` with the FULL accumulated `inputs` block from the prior response plus `envClassifications`. There is NO `action="classify"` step (that's the recipe-fact workflow — wrong tool). There is NO `action="complete"` step (that's bootstrap). Re-call `action="start"` with the accumulated inputs and the new classification map:

```
zerops_workflow action="start" workflow="launch-production" \
  productionProjectName="<from inputs>" \
  targetService="<from inputs>" \
  region="<from inputs>" \
  envClassifications={"APP_KEY":"auto-secret","DB_HOST":"infrastructure","STRIPE_KEY":"external-secret"}
```

If you skip an env, the next response re-prompts with the remaining unclassified keys. Extra keys that don't match any source env are informational — the composer ignores them.

## The four buckets

| Bucket | Detection signal | Emit in production project |
|---|---|---|
| `infrastructure` | Value (or component) resolves from a managed-service reference (`${db_*}`, `${redis_*}`, `${mongo_*}`, plus per-service prefixes). Includes app-built compound URLs assembled at runtime from `${...}` components. | DROP from `project.envVariables`. The reference still lives in `zerops.yaml`'s `run.envVariables`; the re-imported managed service emits a fresh value at boot. |
| `auto-secret` | Source code uses the var as a local encryption / signing key (framework owns the call; rarely visible in app code). | `<@generateRandomString(<32>)>`. Each launch gets a fresh secret. |
| `external-secret` | Source calls a third-party SDK with the var (Stripe, OpenAI, Mailgun, GitHub, …). Includes aliased imports + webhook verification secrets. | Comment + `<@pickRandom(["REPLACE_ME"])>`. New project's owner pastes the real key into the dashboard before deploy. |
| `plain-config` | Source uses the var as literal runtime config (LOG_LEVEL, NODE_ENV, FEATURE_FLAGS, …). | Literal value verbatim. |

`zerops_workflow` returns each unclassified env's key but NOT its value — fetch values via `zerops_discover service="{targetHostname}" includeEnvs=true includeEnvValues=true`, then grep them against the mounted source tree (when accessible) before bucketing.

Every row carries `suggestedBucket` + `rationale` computed server-side from the env key NAME alone (never the value, per the no-leak invariant). Treat the suggestion as a starting point — the four-bucket detection table below remains authoritative when you override. Common reasons to override: a credential-pattern match (`*_KEY`, `*_TOKEN`) that's actually plain-config in your app, or a plain-config name (`DB_HOST`) whose value resolves to a managed-service reference (`${db_*}`) and should bucket `infrastructure`.

## Worked examples per bucket

### Infrastructure

```
DB_HOST=${db_hostname}
REDIS_URL=${redis_connectionString}
```

Both resolve from managed-service references — bucket `infrastructure`. The new prod project's `db` and `redis` services emit fresh values at boot. Compound case: `DATABASE_URL` assembled in app code from `${DB_USER}`, `${DB_PASSWORD}` — the COMPONENT envs are `infrastructure`. If `DATABASE_URL` is itself a project env resolving to managed refs, bucket it `infrastructure`; if assembled manually with literal credentials, bucket `external-secret`.

### Auto-secret

```
APP_KEY=existing-key    # Laravel — encrypts cookies/session
SECRET_KEY=django…      # Django — signs sessions, CSRF
JWT_SECRET=long-bytes   # Node — signs tokens
```

Framework convention drives detection: Laravel `APP_KEY`, Django `SECRET_KEY`, Rails `SECRET_KEY_BASE`, Express `SESSION_SECRET` / `JWT_SECRET`. **Stability warning**: if persisted state (encrypted cookies, signed tokens, encrypted DB columns) depends on the existing key, regenerating breaks it. Ask the user before bucketing `auto-secret` for a non-greenfield prod migration — the alternative is `plain-config` (carry the existing key forward).

### External secret

```
STRIPE_SECRET=sk_live_xyz…
OPENAI_API_KEY=sk-proj-…
MAILGUN_API_KEY=key-…
GITHUB_TOKEN=ghp_…
```

Source contains the SDK call (`stripe(env.STRIPE_SECRET)`, etc.). Aliased imports still count: `from stripe import Stripe as PaymentProvider; PaymentProvider(env.SECRET)`. Webhook-verification secrets (`stripe.webhooks.constructEvent`) also bucket `external-secret`. Empty / sentinel values (`STRIPE_SECRET=`, `disabled`, `sk_test_*`, `test_xxx`, `none`) are review-required — `REPLACE_ME` breaks startup if the app validates on init. Bucket `external-secret` only if a real prod value is needed; otherwise `plain-config` keeps the existing.

### Plain config

```
LOG_LEVEL=info
NODE_ENV=production
FEATURE_FLAGS=experiments_v2,beta_signups
APP_URL=${zeropsSubdomainHost}
```

Literal runtime config. Privacy flag: real emails (`MAIL_FROM_ADDRESS=ops@acme.com`), customer names, internal domain names, sender identities are technically `plain-config` but emitting them into a fresh prod project leaks PII. Surface to the user before bucketing — they may want to redact or rotate.

## Platform-injected tokens

`GIT_TOKEN` and `ZCP_API_KEY` appear in source-project envs but are ZCP-side infrastructure (re-injected by the launch handler for the new project's git push + MCP session). Bucket both as `infrastructure` — they will be DROPPED from `project.envVariables` and the prod project re-receives them via its own launch flow. Do NOT bucket them as `external-secret` (`REPLACE_ME` would break the prod project's first git push).

## Common mis-classification traps

- **APP_KEY across a stateful app** (M3): auto-generating breaks existing encrypted columns / session cookies. If state continuity matters, bucket `plain-config` and carry the existing value forward.
- **`STRIPE_SECRET=` empty in staging** (M4): `REPLACE_ME` placeholder breaks startup if the app validates on init. Bucket `external-secret` only if a real prod value is needed; otherwise `plain-config`.
- **Compound `DATABASE_URL` with literal credentials** (M2): looks like infrastructure but it's a hand-rolled URL. Bucket `external-secret`.
- **`MAIL_FROM_ADDRESS=ops@acme.com`** (M5): literal config, but the email is real. Flag privacy; consider placeholder before launch.
- **Test-fixture values** (`TEST_API_KEY=test_xxx` consumed only by tests, M6): bucket `plain-config` only if read at runtime; if every reference is inside a test file, drop the env entirely before launch.
- **Non-default managed-service prefixes** (M7): a custom Mongo/Postgres/MySQL may emit envs as `${mongo_connectionString}` / `${postgres_*}` / `${mysql_*}` instead of `${db_*}`. Inspect the discover response's `services[].envs` array — false-negative `plain-config` here emits literal hostname/password into the prod project.

If a row is genuinely ambiguous, the safest default is `plain-config` (carries the existing value) plus a follow-up review with the user — wrong-direction errors there are fixable post-launch without breaking deploy.
