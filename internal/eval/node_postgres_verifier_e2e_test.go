//go:build e2e

package eval

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// nodePostgresOracleInputs is the manually-prepared, disposable-fixture
// calibration input set (docs/spec-testing-architecture.md §10.3
// "Calibration before any agent"). This test invokes the exported verifier
// directly — never the runner, Seed*, CleanupProject, or the ./e2e package
// (whose TestMain deletes prefixed services). See
// eval/behavioral/oracle-retest.md for the exact operator steps.
type nodePostgresOracleInputs struct {
	apiHost, token, projectID             string
	stage, database, unrelated            string
	stageID, databaseID, unrelatedID      string
	otherAppVersion, ackDisposableProject string
	dbResolveDomain                       string
}

// readNodePostgresOracleInputs reads the ZCP_EVAL_ORACLE_* environment
// variables. found reports whether ANY of them was set — used to
// distinguish "no calibration input at all" (skip) from "some input,
// incomplete" (fatal before any network call).
func readNodePostgresOracleInputs(t *testing.T) (in nodePostgresOracleInputs, found bool) {
	t.Helper()
	vars := map[string]*string{
		"ZCP_EVAL_ORACLE_API_HOST":               &in.apiHost,
		"ZCP_EVAL_ORACLE_TOKEN":                  &in.token,
		"ZCP_EVAL_ORACLE_PROJECT_ID":             &in.projectID,
		"ZCP_EVAL_ORACLE_STAGE":                  &in.stage,
		"ZCP_EVAL_ORACLE_DATABASE":               &in.database,
		"ZCP_EVAL_ORACLE_UNRELATED":              &in.unrelated,
		"ZCP_EVAL_ORACLE_STAGE_ID":               &in.stageID,
		"ZCP_EVAL_ORACLE_DB_ID":                  &in.databaseID,
		"ZCP_EVAL_ORACLE_OTHER_ID":               &in.unrelatedID,
		"ZCP_EVAL_ORACLE_OTHER_APPVERSION":       &in.otherAppVersion,
		"ZCP_EVAL_ORACLE_ACK_DISPOSABLE_PROJECT": &in.ackDisposableProject,
		"ZCP_EVAL_ORACLE_DB_RESOLVE_DOMAIN":      &in.dbResolveDomain,
	}
	for name, dst := range vars {
		v, ok := os.LookupEnv(name)
		if ok && v != "" {
			found = true
		}
		*dst = v
	}
	return in, found
}

// requireCompleteNodePostgresOracleInputs fails BEFORE any network call
// (client construction included) when any input is missing or the
// disposable-project acknowledgement isn't exactly "yes" — §10.3's
// "either has all of them ... or fails before any network call".
func requireCompleteNodePostgresOracleInputs(t *testing.T, in nodePostgresOracleInputs) {
	t.Helper()
	missing := []string{}
	check := func(name, v string) {
		if v == "" {
			missing = append(missing, name)
		}
	}
	check("ZCP_EVAL_ORACLE_API_HOST", in.apiHost)
	check("ZCP_EVAL_ORACLE_TOKEN", in.token)
	check("ZCP_EVAL_ORACLE_PROJECT_ID", in.projectID)
	check("ZCP_EVAL_ORACLE_STAGE", in.stage)
	check("ZCP_EVAL_ORACLE_DATABASE", in.database)
	check("ZCP_EVAL_ORACLE_UNRELATED", in.unrelated)
	check("ZCP_EVAL_ORACLE_STAGE_ID", in.stageID)
	check("ZCP_EVAL_ORACLE_DB_ID", in.databaseID)
	check("ZCP_EVAL_ORACLE_OTHER_ID", in.unrelatedID)
	check("ZCP_EVAL_ORACLE_OTHER_APPVERSION", in.otherAppVersion)
	if len(missing) > 0 {
		t.Fatalf("incomplete oracle calibration inputs, missing: %v — the acknowledgement below is an operator assertion the code cannot verify; every other input IS checked before any network call", missing)
	}
	if in.ackDisposableProject != "yes" {
		t.Fatalf("ZCP_EVAL_ORACLE_ACK_DISPOSABLE_PROJECT must be exactly \"yes\" (got %q) — this is an operator assertion the code cannot verify that %s is a disposable project safe to mutate", in.ackDisposableProject, in.projectID)
	}
}

// newNodePostgresOracleVerifier constructs the production verifier: the
// real platform client, the real pgx-backed DB, a redirect-refusing HTTP
// client, and a cryptographically random nonce per run.
func newNodePostgresOracleVerifier(t *testing.T, in nodePostgresOracleInputs) (NodePostgresVerifier, platform.Client) {
	t.Helper()
	client, err := platform.NewZeropsClient(in.token, in.apiHost)
	if err != nil {
		t.Fatalf("construct platform client: %v", err)
	}
	httpClient := &http.Client{
		Timeout:       30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return NodePostgresVerifier{
		Client: client,
		HTTP:   httpClient,
		DB:     resolveDomainDB{Domain: in.dbResolveDomain, Inner: PgxNodePostgresDB{}},
		Nonce:  randomNodePostgresNonce,
	}, client
}

// resolveDomainDB is a calibration-harness-only dial adapter. The verifier
// has already required the managed env `hostname` to equal the declared
// database hostname before this is reached; the adapter only qualifies that
// host for DNS when the operator runs the calibration over a multi-project
// VPN, whose resolver knows `<host>.<routingDomain>.zerops-project` and not
// the bare `<host>` (single-project VPN and in-project containers resolve
// the bare name, so Domain stays empty there). It never changes which host
// is compared, only how the same host is dialled — verified live 2026-09-08.
type resolveDomainDB struct {
	Domain string
	Inner  NodePostgresDB
}

func (d resolveDomainDB) QueryRecordByNonce(ctx context.Context, conn NodePostgresConn, nonce string) (id, value string, count int, err error) {
	if d.Domain != "" {
		conn.Host = conn.Host + "." + d.Domain
	}
	return d.Inner.QueryRecordByNonce(ctx, conn, nonce)
}

func nodePostgresOracleInput(in nodePostgresOracleInputs) NodePostgresInput {
	return NodePostgresInput{
		ProjectID: in.projectID, Stage: in.stage, Database: in.database, Unrelated: in.unrelated,
		BaselineUnrelatedAppVersion: in.otherAppVersion,
		ExpectStageID:               in.stageID,
		ExpectDatabaseID:            in.databaseID,
		ExpectUnrelatedID:           in.unrelatedID,
	}
}

// TestE2E_EvalNodePostgres_KnownGood_Passes pins
// docs/spec-testing-architecture.md §10.3 "Calibration before any agent":
// against a manually prepared, known-good app + managed PostgreSQL, all
// four rows pass.
func TestE2E_EvalNodePostgres_KnownGood_Passes(t *testing.T) {
	in, found := readNodePostgresOracleInputs(t)
	if !found {
		t.Skip("no ZCP_EVAL_ORACLE_* input — skipping is NOT acceptance of the §10.3 calibration; see eval/behavioral/oracle-retest.md")
	}
	requireCompleteNodePostgresOracleInputs(t, in)

	verifier, _ := newNodePostgresOracleVerifier(t, in)
	rows := verifier.Verify(context.Background(), nodePostgresOracleInput(in))
	logNodePostgresRows(t, rows)
	for _, row := range rows {
		if row.Result != CheckPassed {
			t.Errorf("row %s = %s, want passed against the known-good fixture (message: %s)", row.ID, row.Result, row.Message)
		}
	}
}

// TestE2E_EvalNodePostgres_FakeWithoutRow_FailsDBRowOnly pins
// docs/spec-testing-architecture.md §10.3's central defense: against a
// manually prepared "plausible fake" app (answers 201/200 with matching
// JSON but stores only in memory), db_row fails and nothing else.
func TestE2E_EvalNodePostgres_FakeWithoutRow_FailsDBRowOnly(t *testing.T) {
	in, found := readNodePostgresOracleInputs(t)
	if !found {
		t.Skip("no ZCP_EVAL_ORACLE_* input — skipping is NOT acceptance of the §10.3 calibration; see eval/behavioral/oracle-retest.md")
	}
	requireCompleteNodePostgresOracleInputs(t, in)

	verifier, _ := newNodePostgresOracleVerifier(t, in)
	rows := verifier.Verify(context.Background(), nodePostgresOracleInput(in))
	logNodePostgresRows(t, rows)
	for _, row := range rows {
		wantFail := row.Check == checkNodePostgresRecord && row.Scope == in.database
		if wantFail && row.Result != CheckFailed {
			t.Errorf("row %s = %s, want failed (the fake stores only in memory)", row.ID, row.Result)
		}
		if !wantFail && row.Result != CheckPassed {
			t.Errorf("row %s = %s, want passed (only db_row should fail against the plausible fake)", row.ID, row.Result)
		}
	}
}

// logNodePostgresRows prints every row so a calibration run leaves the
// decided evidence in the test log, not only a pass/fail line. Rows never
// carry credentials (§10.3 "Independence"), so logging them is safe.
func logNodePostgresRows(t *testing.T, rows []RequiredCheck) {
	t.Helper()
	for _, row := range rows {
		t.Logf("row %s = %s (expected %q, observed %q, source %s): %s", row.ID, row.Result, row.Expected, row.Observed, row.Source, row.Message)
	}
}
