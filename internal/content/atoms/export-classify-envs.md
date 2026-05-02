---
id: export-classify-envs
priority: 2
phases: [export-active]
exportStatus: [classify-prompt]
environments: [container]
title: "Classify project envVariables before publishing the export bundle"
---
You are at `status="classify-prompt"`. Classify each project env into one of four buckets — `infrastructure`, `auto-secret`, `external-secret`, `plain-config` — before re-calling with `envClassifications` populated.

The export bundle's `project.envVariables` block holds the values that re-imported services see at boot. Each project env needs a bucket so the generator knows whether to drop it (managed services regenerate the value), inject a preprocessor directive (auto-secret or external-secret placeholder), or emit it verbatim. Classification is your job — `zerops_workflow` does NOT auto-bucket.

## The four buckets

| Bucket | Detection signal | Emit in `zerops-project-import.yaml` |
|---|---|---|
| `infrastructure` | Value (or a component thereof) comes from a managed-service reference (`${db_*}`, `${redis_*}`, `${mongo_*}`, plus documented per-service prefixes). Includes app-built compound URLs assembled in code from `${...}` components. | DROP from `project.envVariables`. The reference still lives in `zerops.yaml`'s `run.envVariables`, and the re-imported managed service emits a fresh value. |
| `auto-secret` | Source code or framework convention uses the var as a local encryption / signing key. Even when the encryption call lives inside the framework. | `<@generateRandomString(<32>)>`. Each re-import gets a fresh secret. |
| `external-secret` | Source calls a third-party SDK using the var (Stripe, OpenAI, Mailgun, GitHub, …). Includes aliased imports and webhook verification secrets. | Comment + `<@pickRandom(["REPLACE_ME"])>`. The new project's owner pastes the real key into the dashboard before deploying. |
| `plain-config` | Source uses the var as literal runtime config (LOG_LEVEL, NODE_ENV, FEATURE_FLAGS, …). | The literal value verbatim. |

`zerops_workflow workflow="export"` returns each unclassified env's key but NOT its value — fetch values via `zerops_discover hostname="{targetHostname}" includeEnvs=true includeEnvValues=true`, grep them against the source tree, then call back with an `envClassifications` map (key → bucket per env).

## Worked examples per bucket

### Infrastructure

```
DB_HOST=${db_hostname}
REDIS_URL=${redis_connectionString}
```

Both resolve from a managed-service reference. Bucket is `infrastructure` even though the source code reads them. The re-imported `db` and `redis` services emit fresh `${db_hostname}` / `${redis_connectionString}` values at boot.

Compound case: `DATABASE_URL` is built in app code from `${DB_USER}`, `${DB_PASSWORD}`, etc. The COMPONENT envs are `infrastructure`. The composed `DATABASE_URL` may itself be a project env or may be assembled in app code at runtime. If `DATABASE_URL` is a project env that resolves to a managed reference, bucket it `infrastructure`. If it's a project env you assembled manually with literal credentials, bucket it `external-secret` (the value is sensitive, not auto-derived).

### Auto-secret

```
APP_KEY=existing-key    # Laravel — encrypts cookies/session
SECRET_KEY=django…      # Django — signs sessions, CSRF, password tokens
JWT_SECRET=long-bytes   # Node/Express — signs tokens
```

Source code rarely shows the encryption call directly — the framework owns it. Detect via framework convention: Laravel `APP_KEY`, Django `SECRET_KEY`, Rails `SECRET_KEY_BASE`, Express `SESSION_SECRET` / `JWT_SECRET`. **Stability warning**: if any persisted state (encrypted cookies, signed session tokens, password reset links, encrypted DB columns) depends on the existing key, regenerating breaks it. When in doubt, ask the user before bucketing as `auto-secret` — the alternative is `plain-config` (carry the existing key forward).

### External secret

```
STRIPE_SECRET=sk_live_xyz…
OPENAI_API_KEY=sk-proj-…
MAILGUN_API_KEY=key-…
GITHUB_TOKEN=ghp_…    # also: GH_TOKEN, GH_PAT
```

Source code contains the SDK call (`stripe(env.STRIPE_SECRET)`, `OpenAI({apiKey: env.OPENAI_API_KEY})`, `Mailgun.client({key: env.MAILGUN_API_KEY})`). **Aliased imports** still count: `from stripe import Stripe as PaymentProvider; client = PaymentProvider(env.SECRET)` — the SDK is Stripe even if the local name isn't. **Webhook verification secrets** (`stripe.webhooks.constructEvent`) also bucket `external-secret`. **Empty / sentinel values** (`STRIPE_SECRET=`, `disabled`, `sk_test_*`, `pk_test_*`, `rk_test_*`, `test_xxx`, `none`, `null`, `false`, `off`, `n/a`, `noop`) are review-required — do NOT blindly substitute `REPLACE_ME` for them; bucket as `external-secret` only if the value is a real production secret. The generator surfaces a warning when it detects sentinel patterns. **Test-fixture values** like `TEST_API_KEY=test_xxx` (M6) used only by mocked tests usually want `plain-config` — verify by grepping whether the env is read at runtime; if every reference is inside a test file, drop or comment it out before publish unless source proves runtime dependency.

### Plain config

```
LOG_LEVEL=info
NODE_ENV=production
FEATURE_FLAGS=experiments_v2,beta_signups
APP_URL=${zeropsSubdomainHost}
```

Literal runtime config. **Privacy flag**: real emails (`MAIL_FROM_ADDRESS=ops@acme.com`), customer names, internal domain names, webhook URLs, and sender identities are technically `plain-config` but emitting them verbatim into a public export bundle leaks PII. Surface the value to the user before bucketing — they may want to redact or rotate before publishing.

## Source-tree grep commands

Use `rg -n` (ripgrep) for paste-safe alternation; `grep -RInE` is the equivalent fallback. Both expand `(a|b)` without backslash quoting.

| Language | Find env read | Find SDK + encryption |
|---|---|---|
| Node | `rg -n 'process\.env\.<KEY>' src/` | `rg -nE '(stripe\|openai\|mailgun\|@octokit)' src/`; `rg -nE '(jwt\.sign\|bcrypt\|crypto\.create)' src/` |
| Python | `rg -n 'os\.(environ\|getenv)' .` | `rg -nE 'import (stripe\|openai\|mailgun)' .`; `rg -nE '(Fernet\|signing\.dumps\|cryptography\.fernet)' .` |
| PHP | `rg -n "env\('<KEY>'\)" app/ config/` | `rg -nE 'Stripe\\\|OpenAI\\\|Mailgun\\' app/`; `rg -nE 'Crypt::\|Hash::' app/` |
| Go | `rg -n 'os\.Getenv\("<KEY>"\)' .` | `rg -nE '(crypto/\|jwt\.New)' .` |

Trace one alias hop — wrapper modules that re-export an SDK still count. Beyond two hops, ask the user instead of guessing.

## The per-env review table

The Phase B response (`status="classify-prompt"`) carries a row per project env:

```
{ "key": "APP_KEY",    "currentBucket": "" },
{ "key": "DB_HOST",    "currentBucket": "" },
{ "key": "STRIPE_KEY", "currentBucket": "" }
```

Build your classification map from the keys, then call back with `envClassifications`:

```
zerops_workflow workflow="export" \
  targetService="{targetHostname}" \
  variant="dev" \
  envClassifications={"APP_KEY":"auto-secret","DB_HOST":"infrastructure","STRIPE_KEY":"external-secret"}
```

If you skip an env, the next response re-prompts with the remaining unclassified keys. Extra keys that don't match any project env are informational — the generator ignores them.

## Common mis-classification traps

- **APP_KEY across a stateful app** (M3): auto-generating breaks existing encrypted columns / session cookies. If state continuity matters, bucket `plain-config` and carry the existing value forward.
- **`STRIPE_SECRET=` empty in staging** (M4): the live value is empty because staging doesn't process payments. `REPLACE_ME` placeholder breaks startup if the app validates the key on init. Bucket `external-secret` only if a real value is needed; otherwise `plain-config` keeps the empty string.
- **Compound DATABASE_URL with literal credentials in source** (M2): the value LOOKS like infrastructure but it's a hand-rolled URL. Bucket `external-secret` so the new project owner replaces it after import.
- **`MAIL_FROM_ADDRESS=ops@acme.com`** (M5): literal config, but the email is real. Flag privacy concern; consider replacing with a placeholder before export.
- **`TEST_API_KEY=test_xxx` consumed only by tests** (M6): bucket `plain-config` only if the env is read at runtime; if every reference is inside a test file or a fixture loader, drop the env entirely from the bundle (delete the project env in dashboard before re-running export, or skip the row in `envClassifications` and let the unset warning prompt a follow-up).
- **Non-default managed-service prefixes** (M7): a custom Mongo/Postgres/MySQL service may emit envs as `${mongo_connectionString}` / `${postgres_password}` / `${mysql_dbName}` instead of the documented `${db_*}` shape. The protocol still buckets these `infrastructure` if the live `zerops_discover` shows the value resolving to a managed-service env — verify by inspecting the discover response's `services[].envs` array, not just the `${db_*}` sample. False-negative `plain-config` here would emit a literal hostname/password into the bundle.

If a row's bucket is genuinely ambiguous, the safest default is `plain-config` (carries the existing value) plus a follow-up review with the user — wrong-direction errors there are fixable post-import without breaking deploy.
