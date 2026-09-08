---
id: acceptance-node-postgres-record
description: |
  Deterministic acceptance run for the node-postgres application oracle
  (docs/spec-testing-architecture.md §10.3). The agent implements
  POST /records and GET /records/:id on the existing appstage Node
  service, persisting to the existing managed PostgreSQL. An independent,
  verifier-held nonce/value proves the record actually landed in the
  database — not just that the HTTP layer answered correctly — and an
  unrelated service's deployed artifact must not change.

  Excluded from `behavioral all`: it needs the acceptance-node-postgres-record
  fixture prepared and an owned run window (required mode retains the
  project — no cleanup — so a routine full-suite sweep would leak it).
seed: imported
fixture: fixtures/acceptance-node-postgres-record.yaml
tags: [acceptance, application-oracle, node, postgres, required]
area: testing-architecture
excludeFromAll: true
retrospective:
  promptStyle: briefing-future-agent
verification:
  mode: required
  expectedServices:
    - hostname: appstage
      status: [ACTIVE]
    - hostname: db
      status: [ACTIVE]
  nodePostgresRecord:
    stage: appstage
    database: db
    unrelated: other
---

The appstage service needs two new endpoints backed by the existing `db`
PostgreSQL service:

- `POST /records` accepts a JSON body `{"nonce": "...", "value": "..."}`
  and persists a new row with a generated `id`, storing `nonce`, `value`,
  and `environment: "stage"` in a `records` table (columns: `id`,
  `nonce` unique, `value`, plus whatever else you need). Respond `201`
  with the created record as JSON (`id`, `nonce`, `value`, `environment`).
- `GET /records/:id` looks up the row by `id` and responds `200` with the
  same shape (`id`, `nonce`, `value`, `environment`).

Create the `records` table (or run the migration) as part of getting this
deployed — there's no existing schema to reuse. Leave the `appdev`,
`other`, and `db` services exactly as they are; this work is scoped to
`appstage` only.
