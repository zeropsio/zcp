# Codex Round P0 Prework: Env Classification Field Test

## 1. Verdict

**NEEDS-REVISION** — §3.4 as written (`plans/export-buildfromgit-2026-04-28.md:135-150`) is directionally better than the old hardcoded list (`internal/content/atoms/export.md:140-148`), but one example per bucket plus "grep over source code" is not enough to classify common Laravel, Node, and Django env shapes reliably.

## 2. Per-app field test

### Laravel 11 skeleton

Representative patterns: `.env.example`; `config/app.php` reads `env('APP_KEY')`; `config/database.php` reads `DB_*`; `config/cache.php` / `config/session.php` read Redis; mail config reads `MAIL_*`; app code usually does not call `Crypt` directly even though framework cookies/sessions are encrypted by `APP_KEY`.

| env var | predicted bucket | correct bucket | match? | reason for mismatch |
|---|---|---|---|---|
| `APP_NAME` | Plain config | Plain config | yes | Literal app label in `config/app.php`. |
| `APP_ENV` / `APP_DEBUG` / `LOG_LEVEL` | Plain config | Plain config | yes | Literal runtime flags. |
| `APP_KEY` | Plain config or auto-secret | Auto-generatable secret, with stability warning | partial | Source grep may not see direct `Crypt::encrypt(... env('APP_KEY'))`; framework uses it indirectly. If examples mention Laravel key names, it becomes name heuristic; if not, it can miss. |
| `DB_CONNECTION=mysql` | Plain config | Plain config | yes | Driver literal, not a credential. |
| `DB_HOST=${db_hostname}` / `DB_PORT=${db_port}` / `DB_DATABASE=${db_dbName}` / `DB_USERNAME=${db_user}` / `DB_PASSWORD=${db_password}` in `zerops.yaml` | Infrastructure-derived | Infrastructure-derived | yes | Direct managed-service reference shape matches §3.4. |
| `DATABASE_URL` constructed in app from `DB_*` | Plain config or External | Infrastructure-derived | no | Indirect resolution: no `${db_connectionString}` value exists for grep to see; correct handling requires tracing `DB_*` inputs into framework database config. |
| `REDIS_HOST=${redis_hostname}` / `REDIS_PASSWORD=${redis_password}` | Infrastructure-derived | Infrastructure-derived | yes | Direct `${redis_*}` references. |
| `MAIL_MAILER=smtp`, `MAIL_FROM_ADDRESS=support@example.com`, `MAIL_FROM_NAME="${APP_NAME}"` | Plain config | Plain config, privacy-sensitive | partial | Verbatim export can leak real sender identity; classification says plain config but emit policy needs redaction review. |

### Express / Node + Mongo + Stripe

Representative patterns: `process.env.MONGO_URI` passed to `mongoose.connect`; `stripe = require('stripe')(process.env.STRIPE_SECRET)`; `jwt.sign(payload, process.env.JWT_SECRET)`; `connect-redis` reads `REDIS_URL` or host/port; config module may alias envs.

| env var | predicted bucket | correct bucket | match? | reason for mismatch |
|---|---|---|---|---|
| `NODE_ENV` / `PORT` / `LOG_LEVEL` | Plain config | Plain config | yes | Literal runtime flags. |
| `MONGO_URI=${mongo_connectionString}` | Infrastructure-derived | Infrastructure-derived | yes if reference exists | Direct managed reference shape is bucketable. |
| `MONGO_URI=mongodb://${MONGO_USER}:${MONGO_PASSWORD}@${MONGO_HOST}:${MONGO_PORT}/app` | Plain config or External | Infrastructure-derived | no | Compound construction lacks the single `${db_connectionString}` shape; classifier needs service-ref provenance, not SDK-call detection. |
| `MONGO_HOST=${mongo_hostname}` / `MONGO_PASSWORD=${mongo_password}` | Infrastructure-derived or Plain config | Infrastructure-derived | maybe | §3.4 examples mention `db_*`/`redis_*`, not custom `mongo_*`; without a recognized service env map this is a false negative risk. |
| `STRIPE_SECRET` | External secret | External secret | yes | Direct `require('stripe')(process.env.STRIPE_SECRET)` fits the example. |
| `STRIPE_WEBHOOK_SECRET` | Auto-generatable or External | External secret | maybe | Used by `stripe.webhooks.constructEvent(body, sig, env.STRIPE_WEBHOOK_SECRET)`: signing/verification shape can be confused with local signing key, but the key is provisioned by Stripe. |
| `JWT_SECRET` | Auto-generatable secret | Auto-generatable secret | yes | Direct `jwt.sign(_, process.env.JWT_SECRET)` matches §3.4. |
| `SESSION_SECRET` | Auto-generatable secret | Auto-generatable secret | yes | `express-session({ secret: process.env.SESSION_SECRET })` is local signing. |
| `REDIS_URL=${redis_connectionString}` or `REDIS_HOST=${redis_hostname}` | Infrastructure-derived | Infrastructure-derived | yes | Direct managed reference shape. |
| `TEST_API_KEY=test_xxx` | Plain config | Drop, ask, or external placeholder depending on code path | no | Fixture-looking value is not handled by §3.4; old atom explicitly called out test fixtures at `internal/content/atoms/export.md:133-135`. |

### Django + Celery + Redis + OpenAI

Representative patterns: `SECRET_KEY = env("SECRET_KEY")`; `DATABASES = dj_database_url.config(default=env("DATABASE_URL"))`; `CELERY_BROKER_URL = env("REDIS_URL")`; `openai.OpenAI(api_key=os.environ["OPENAI_API_KEY"])`.

| env var | predicted bucket | correct bucket | match? | reason for mismatch |
|---|---|---|---|---|
| `DJANGO_SETTINGS_MODULE` / `DEBUG` / `ALLOWED_HOSTS` | Plain config | Plain config | yes | Literal framework config. |
| `SECRET_KEY` | Plain config or auto-secret | Auto-generatable secret, with stability warning | partial | Django uses the key for signing sessions, CSRF, password reset tokens; source grep often sees only `env("SECRET_KEY")`, not `Signer(... env.SECRET_KEY)`. |
| `DATABASE_URL=${db_connectionString}` | Infrastructure-derived | Infrastructure-derived | yes | Direct reference. |
| `DB_HOST=${db_hostname}` with `DATABASE_URL` assembled in settings | Plain config or Infrastructure-derived | Infrastructure-derived | maybe | Correct only if agent traces `DB_HOST`/`DB_PASSWORD` into URL assembly. |
| `REDIS_URL=${redis_connectionString}` / `CELERY_BROKER_URL=${redis_connectionString}` | Infrastructure-derived | Infrastructure-derived | yes | Direct Redis managed reference. |
| `OPENAI_API_KEY` | External secret | External secret | yes | Direct `OpenAI(api_key=...)` pattern. |
| `MAILGUN_API_KEY` | External secret | External secret | yes | Direct Mailgun client call if present. |
| `MAILGUN_FROM="Acme Support <support@acme.com>"` | Plain config | Plain config, privacy-sensitive | partial | Literal config, but verbatim emit can leak a real support address in a public export bundle. |

## 3. Failure-mode evaluation

**M1 aliased imports — medium.** Pattern: `from stripe import Stripe as PaymentProvider; client = PaymentProvider(os.environ["SECRET"])`. A literal grep for `Stripe` may find the import line but not connect `SECRET` to the SDK unless the agent follows aliases. Required wording: trace imports and constructor aliases before deciding.

**M2 indirect resolution — critical.** Pattern: `DB_HOST=${db_hostname}` in `zerops.yaml`; app code builds `DATABASE_URL = f"postgres://{DB_USER}:{DB_PASSWORD}@{DB_HOST}:{DB_PORT}/{DB_NAME}"`. §3.4 only says managed-service-emitted shape. It needs provenance from `zerops.yaml` references and recognized service env names, otherwise infrastructure credentials become plain literals or placeholders.

**M3 multi-purpose vars — medium.** Pattern: Laravel `APP_KEY=base64:...` used for encrypted cookies, but test imports require the same stable key to validate old encrypted fixture cookies. Auto-generating is correct for a fresh app, wrong for stateful/test fixture portability. Needs a "state continuity" warning, not blind generation.

**M4 empty / sentinel external values — critical.** Pattern: `STRIPE_SECRET=` in staging while code still imports Stripe and validates env at startup. Protocol emits `<@pickRandom(["REPLACE_ME"])>`, so re-import may boot with a non-empty invalid key and fail differently than empty staging. Empty external secrets should emit empty/commented placeholder or require user confirmation.

**M5 compound / templated values — medium.** Pattern: `MAILGUN_FROM="Acme Support <support@acme.com>"`. Plain config is technically right, but verbatim public bundle leaks PII/customer identity. Needs privacy-sensitive plain config review.

**M6 test-fixture values — medium.** Pattern: `TEST_API_KEY=test_xxx` used by mocked tests only. §3.4 lacks a fixture/sentinel rule, so likely Plain config. Correct action is ask/drop/comment unless source proves runtime dependency.

**M7 non-default managed prefixes — critical.** Pattern: custom Mongo service exposes `${mongo_connectionString}` or `${mongodb_hostname}`, not `${db_*}`. Without an explicit recognized managed-service env map from Zerops docs/live export metadata, the classifier will false-negative infrastructure-derived secrets.

## 4. Protocol adequacy / gaps

One example per category is insufficient. The bucket boundary depends on provenance and usage: framework-level secrets (`APP_KEY`, `SECRET_KEY`) are not always visible as encryption calls, external webhook secrets look like signing keys, and infrastructure-derived values can appear as components rather than connection strings.

The recovery path is underspecified. Phase B says "Surface diff for user review" (`plans/export-buildfromgit-2026-04-28.md:171-173`), but §3.4 does not require an explicit per-env classification table for the user to inspect or override before Phase C. If the only confirmation happens before publish, users see YAML effects but not the classifier rationale, making mistakes hard to spot.

The protocol needs an explicit review-and-override step in Phase B: show env var, source evidence, bucket, emitted value, and risk note; accept user corrections before schema validation and before writing/publishing.

## 5. Recommended amendments to plan §3.4

```diff
- For every env var ... the agent classifies via grep over source code:
+ For every env var ... the agent classifies by combining source grep, zerops.yaml
+ reference provenance, and framework/config file reads. Grep is evidence, not the
+ whole classifier.
```

```diff
- Infrastructure-derived | resolves to managed-service-emitted shape (`${db_connectionString}`, `${redis_hostname}`)
+ Infrastructure-derived | value or component comes from a recognized managed-service
+ reference in zerops.yaml or platform export metadata (`${db_*}`, `${redis_*}`,
+ plus documented service-specific prefixes such as Mongo/Postgres/MySQL variants)
```

```diff
- Auto-generatable secret | source uses var as encryption/signing key
+ Auto-generatable secret | source or framework convention uses var as local
+ encryption/signing key; warn before regenerating when state/test fixture continuity
+ depends on the old value
```

```diff
- External secret | source calls third-party SDK ...
+ External secret | source calls third-party SDK, including aliased imports and
+ webhook verification secrets; preserve empty/sentinel values as review-required
+ rather than blindly substituting `REPLACE_ME`
```

```diff
- Plain config | source uses literal string-shaped ...
+ Plain config | source uses literal string-shaped config; flag privacy-sensitive
+ literals (emails, domains, customer names, URLs) for user review before verbatim emit
```

```diff
+ Phase B must emit a per-env review table: env var, evidence, bucket, emitted value,
+ risk note, and user override status. Do not proceed to Phase C until user accepts
+ or corrects classifications.
```

## 6. Recommended atom-prose patterns for `export-classify-envs.md`

- "Do not classify from names alone. For each env, record the strongest evidence: direct `${service_field}` provenance, config-file framework convention, SDK call site, or literal-only use."
- "Trace aliases one hop: `Stripe as PaymentProvider`, `const payments = require('stripe')`, and wrapper modules still count as third-party SDK use."
- "Classify infrastructure values from zerops.yaml provenance even when app code consumes components such as `DB_HOST`, `DB_PASSWORD`, or `REDIS_HOST` and builds the URL itself."
- "Framework secrets count even when the encryption call is inside the framework: Laravel `APP_KEY`, Django `SECRET_KEY`, Rails `SECRET_KEY_BASE`, Express session/JWT secrets. Add a stability warning when imported data, cookies, sessions, or tests may rely on the old value."
- "External empty or sentinel values (`''`, `disabled`, `test_xxx`, `sk_test_*`) are review-required. Do not replace an empty external value with a non-empty `REPLACE_ME` without user approval."
- "Plain config is not always public. Flag emails, real domains, customer names, webhook URLs, and sender identities before verbatim emission."
- "Show the classification table to the user in Phase B and accept per-env overrides before schema validation and publish."
