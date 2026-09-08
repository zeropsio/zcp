# Node-postgres application oracle — calibration retest

Operator runbook for `TestE2E_EvalNodePostgres_KnownGood_Passes` and
`TestE2E_EvalNodePostgres_FakeWithoutRow_FailsDBRowOnly`
(`internal/eval/node_postgres_verifier_e2e_test.go`, `//go:build e2e`).
Contract: docs/spec-testing-architecture.md §10.3 "Calibration before any
agent". This is **not** part of the automated battery — a skipped
calibration is not acceptance; run it manually before trusting the oracle
against a live scenario, and after any change to
`internal/eval/node_postgres_verifier.go`.

The tests never call the runner, `Seed*`, or `CleanupProject` — they invoke
`eval.NodePostgresVerifier.Verify` directly against services you prepare by
hand on a **disposable** project. `ZCP_EVAL_ORACLE_ACK_DISPOSABLE_PROJECT` is
an operator assertion the code cannot verify: it only checks the string is
present and exactly `"yes"`, never that the project is actually disposable.
Treat the project as destroyed by this exercise.

## 1. Prepare a disposable project

Import `eval/behavioral/scenarios/fixtures/acceptance-node-postgres-record.yaml`
into a fresh project (any project you're willing to delete afterward — the
zcp-eval-clean convention or an equivalent throwaway). This gives you
`appdev`, `appstage`, `db`, and `other`, all `nodejs@22` /
`postgresql@18`.

Read `other`'s active app-version id now, before deploying anything else —
this is `ZCP_EVAL_ORACLE_OTHER_APPVERSION`, the baseline the "unchanged" row
compares against:

```
zcp discover  # or the GUI — note other's activeAppVersion.id
```

## 2. Deploy the known-good app to appstage

Implement (or reuse a prepared fixture app for) exactly what
`acceptance-node-postgres-record.md`'s prompt asks for:

- `POST /records` — body `{"nonce","value"}` → `201` with `{id, nonce,
  value, environment:"stage"}`, persisted in a `records` table.
- `GET /records/:id` — `200` with the same shape, read back from the
  database (not from an in-memory cache — the whole point of the db_row
  check is proving it isn't).

Deploy this to `appstage`. Do **not** touch `other` at any point in this
runbook — its id must stay equal to what you read in step 1.

## 3. Run the known-good calibration

```
export ZCP_EVAL_ORACLE_API_HOST=...        # ZCP_API_HOST equivalent for your target
export ZCP_EVAL_ORACLE_TOKEN=...           # a token valid for this project
export ZCP_EVAL_ORACLE_PROJECT_ID=...
export ZCP_EVAL_ORACLE_STAGE=appstage
export ZCP_EVAL_ORACLE_DATABASE=db
export ZCP_EVAL_ORACLE_UNRELATED=other
export ZCP_EVAL_ORACLE_STAGE_ID=...        # appstage's service id, from discover
export ZCP_EVAL_ORACLE_DB_ID=...           # db's service id
export ZCP_EVAL_ORACLE_OTHER_ID=...        # other's service id
export ZCP_EVAL_ORACLE_OTHER_APPVERSION=...# from step 1
export ZCP_EVAL_ORACLE_ACK_DISPOSABLE_PROJECT=yes

go test ./internal/eval -tags e2e -run '^TestE2E_EvalNodePostgres_KnownGood_Passes$' -v
```

Expect all four rows passed. Copy the test's `-v` output (row-by-row
Expected/Observed) as evidence before continuing — it will not be
re-derivable once you redeploy in step 4.

## 4. Redeploy the plausible fake

Replace `appstage`'s deployment with a version that answers `POST
/records` and `GET /records/:id` correctly (same status codes, same JSON
shape, same nonce/value roundtrip) but stores the record **only in an
in-memory map** — no write to `db`. This is the "plausible fake" §10.3
exists to catch.

## 5. Run the fake-app calibration

Same environment as step 3 (all values unchanged — `other` still hasn't
moved, so `ZCP_EVAL_ORACLE_OTHER_APPVERSION` stays valid):

```
go test ./internal/eval -tags e2e -run '^TestE2E_EvalNodePostgres_FakeWithoutRow_FailsDBRowOnly$' -v
```

Expect `node_postgres_record/<db>/db_row` failed and every other row
passed. Copy this output as evidence too.

## 6. Clean up

Delete the disposable project. Nothing in this runbook or the tests it
drives cleans up on your behalf — `ACK_DISPOSABLE_PROJECT` is the
acknowledgement that you own that step.
