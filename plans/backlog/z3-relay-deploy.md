# z3 activity relay — deploy to Zerops (project `z3-relay`)

**Surfaced**: 2026-08-29, S5-4a/b landed on the fork's `main` (`infra/relay` re-shelled as a Node service
over Postgres with Zerops-token auth and project-bound links; deploy YAMLs schema-valid). Karel: "nasadíme
to pak do Zeropsu … uschovejme na později; možná to předtím otestuju lokálně, spíš ne."

**Why deferred**: deploying creates a lasting resource in the org (a project with a PostgreSQL service and
APNs secrets) and nothing can show its value yet — the mobile app has no Zerops session (S5-3) and push
delivery needs a paid Apple Developer account (a personal-team build cannot receive APNs). Until then the
relay's only observable behaviour is `/health`, a device registration with a Zerops token, and a publish from
a container landing in its DB.

**Trigger to promote**:
- S5-3 (mobile Zerops session + picker) is about to land, OR
- the owner has the paid Apple Developer account and wants to see a push / Live Activity end to end, OR
- S5-4b's Settings → Notifications link trigger needs a live relay to be tested.

## Sketch — what the deploy consists of

1. Import `../z3/infra/relay/zerops-import.yml` as project `z3-relay` (services: `db` `postgresql:single@16`,
   `relay` `ubuntu/nodejs@22`); fill the placeholder `envSecrets`: `RELAY_ISSUER` (the relay's own public origin,
   known only after the first deploy — set it, redeploy), `CLOUD_MINT_PRIVATE_KEY` / `CLOUD_MINT_PUBLIC_KEY`,
   `APNS_DELIVERY_JOB_SIGNING_SECRET`, `APNS_{TEAM_ID,KEY_ID,BUNDLE_ID,PRIVATE_KEY,ENVIRONMENT}`,
   `ZEROPS_API_HOST` (default `api.app-prg1.zerops.io`). `DATABASE_URL` maps from `db`'s generated vars.
2. Deploy `infra/relay` with its `zerops.yml` (`initCommands` runs `pnpm run migrate`; port 8080, httpSupport).
3. Prove: `GET /health` (does `SELECT 1`); a device registration with a Zerops token (`RelayMobileGroup`);
   a `publishAgentActivity` from `z3-eval`'s z3 server landing in `relay_apns_delivery_jobs` — the link
   handshake needs the S5-4b UI trigger or a scripted `linkEnvironment` with the owner's token.
4. Wire the consumers: z3 server gets the relay URL through the link handshake (secret-store entries
   `cloud-relay-url` / `-issuer` / `-environment-credential`), mobile gets `T3CODE_RELAY_URL` at build time;
   check the `cloudLinkProofHandler` loopback gate behind nginx (`docs/spec-z3.md` §9.2, known gap).
5. Optional local dry run first (owner's call, "spíš ne"): a local Postgres, `infra/relay/.env` from
   `.env.example`, `pnpm run migrate && pnpm run dev`, `GET http://localhost:8080/health`.

**Risks**: `RELAY_ISSUER` self-reference cannot resolve at import time (placeholder + second deploy);
`GetProject.zeropsSubdomainHost` is sometimes a bare prefix — the project-binding check fails closed for such a
project (the ledger's S5-4 row); lease exclusivity of the APNs queue is proven with fakes, not under two racing
connections.

## Refs
- `docs/spec-z3.md` §9.2 (the relay on Zerops), `plans/z3-s5-plan-2026-08-29.md` D5–D8, rows S5-4a/b
- `../z3/infra/relay/README.md` (config, migrate, run), `zerops.yml`, `zerops-import.yml`
- ledger `../z3/docs/internals/zerops/verified.md` "S5-4 — the activity relay re-shelled for Zerops"
