# App-code — minimal tier execution order

Minimal tiers write app code inline — no sub-agent dispatch. Comment ratio in `zerops.yaml` stays at or above 30 percent (target 35 percent) — comments explain decisions, and the writing happens after the smoke-test substep confirms the real install flow.

## Per-tier shape

- **Type 1 (runtime hello world):** a single-file HTTP server — a minimal handler (for example `/` returning `Hello from <framework>`, `/greetings` returning SELECT-all from a `greetings` table) plus a raw SQL migration. No framework, no ORM, no seeder beyond a migration INSERT, no feature sections.
- **Type 2a (frontend static):** an SPA or static site — the framework project (React, Vue, Svelte, …) with a page showing framework name, greeting, and environment indicator. Build-time env var injection. No DB connection.
- **Type 2b (frontend SSR):** an SSR framework project (Next, Nuxt, SvelteKit, …) with server-rendered pages backed by a DB connection and a framework-native health endpoint.
- **Type 3 (backend framework):** a full framework project with ORM-based migrations, a template-rendered dashboard, and the framework's CLI tools.

## Order of writes

1. Scaffold sits behind you — the project tree is on the mount.
2. Write app code per the tier shape above.
3. Move to the smoke-test substep next; `zerops.yaml` is written after smoke-test confirms the install flow.
