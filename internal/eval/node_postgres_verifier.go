package eval

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// randomNodePostgresNonce is the production Nonce func: a fresh
// cryptographically random nonce and value per run (§10.3 "Independence").
func randomNodePostgresNonce() (nonce, value string) {
	return randomHex(16), randomHex(16)
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand.Read failing is effectively unrecoverable on any
		// supported platform; fall back to a fixed-but-unique-enough value
		// rather than panicking mid-verification.
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// NodePostgresConn carries the managed PostgreSQL connection parameters the
// oracle uses to prove a record landed in the database, in memory only.
// Deliberately carries no String()/GoString() — the default struct
// formatting for an unexported-method-free struct via %+v would print
// Password, so no caller may add one (docs/spec-testing-architecture.md
// §10.3 "Independence": credentials never enter rows, messages, meta, or
// the capture bundle).
type NodePostgresConn struct {
	Host     string
	Port     string
	User     string
	Password string
	DBName   string
}

// NodePostgresDB is the tiny read interface the node-postgres oracle uses.
// The pgx implementation is the production value; tests supply a fake.
type NodePostgresDB interface {
	QueryRecordByNonce(ctx context.Context, conn NodePostgresConn, nonce string) (id, value string, count int, err error)
}

// nodePostgresRecordQuery is the one SELECT the oracle runs. The id is cast
// to text so SERIAL, uuid and text ids all compare by their string form.
const nodePostgresRecordQuery = "SELECT id::text, value FROM records WHERE nonce = $1"

// PgxNodePostgresDB is the production NodePostgresDB: connects fresh per
// call, runs the SELECT in a read-only transaction, parameterised on nonce.
type PgxNodePostgresDB struct{}

func (PgxNodePostgresDB) QueryRecordByNonce(ctx context.Context, conn NodePostgresConn, nonce string) (id, value string, count int, err error) {
	pconn, err := pgx.Connect(ctx, nodePostgresDSN(conn))
	if err != nil {
		return "", "", 0, fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = pconn.Close(ctx) }()

	tx, err := pconn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return "", "", 0, fmt.Errorf("begin read-only tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, nodePostgresRecordQuery, nonce)
	if err != nil {
		return "", "", 0, fmt.Errorf("select: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		count++
		if count == 1 {
			if scanErr := rows.Scan(&id, &value); scanErr != nil {
				return "", "", 0, fmt.Errorf("scan: %w", scanErr)
			}
			continue
		}
		// Keep counting past the first row (a >1-row result is itself the
		// failure signal) without overwriting id/value.
		var extraID, extraValue string
		_ = rows.Scan(&extraID, &extraValue)
	}
	if err := rows.Err(); err != nil {
		return "", "", 0, fmt.Errorf("rows: %w", err)
	}
	return id, value, count, nil
}

// nodePostgresDSN builds the connection URL with every component escaped
// as a URL component (not as a query string): a platform-generated password
// containing '+', '/', '@' or a space must connect, not block db_row on a
// parse error.
func nodePostgresDSN(conn NodePostgresConn) string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(conn.User, conn.Password),
		Host:   net.JoinHostPort(conn.Host, conn.Port),
		Path:   "/" + conn.DBName,
	}
	return u.String()
}

// NodePostgresVerifier is the exported, standalone application oracle of
// docs/spec-testing-architecture.md §10.3. It holds its own nonce/value,
// resolves the three declared hostnames independently of anything the
// candidate printed, and proves the record landed in the managed database
// via a read keyed on the nonce.
type NodePostgresVerifier struct {
	Client platform.Client
	HTTP   ops.HTTPDoer
	DB     NodePostgresDB
	Nonce  func() (nonce, value string)
}

// NodePostgresInput is one Verify call's parameters.
// ExpectStageID/ExpectDatabaseID/ExpectUnrelatedID are optional cross-checks:
// when set, a resolved service id that disagrees is treated as a foreign
// binding and blocks every row before any data-plane call.
type NodePostgresInput struct {
	ProjectID                   string
	Stage, Database, Unrelated  string
	BaselineUnrelatedAppVersion string
	ExpectStageID               string
	ExpectDatabaseID            string
	ExpectUnrelatedID           string
}

const (
	checkNodePostgresRecord = "node_postgres_record"
	checkUnrelatedArtifact  = "unrelated_artifact"
)

func nodePostgresRoundtripRowID(stage string) string {
	return fmt.Sprintf("node_postgres_record/%s/record_roundtrip", stage)
}
func nodePostgresEnvironmentRowID(stage string) string {
	return fmt.Sprintf("node_postgres_record/%s/environment", stage)
}
func nodePostgresDBRowID(database string) string {
	return fmt.Sprintf("node_postgres_record/%s/db_row", database)
}
func unrelatedArtifactRowID(unrelated string) string {
	return fmt.Sprintf("unrelated_artifact/%s/unchanged", unrelated)
}

// blockedNodePostgresRows builds the blocked rows with a shared message and
// zero data-plane calls — used whenever resolution fails before any
// HTTP/SQL work is safe to attempt (§10.3 "Independence"). Three rows when
// in.Unrelated is empty (no unrelated host declared), four otherwise.
func blockedNodePostgresRows(in NodePostgresInput, now time.Time, message string) []RequiredCheck {
	rows := []RequiredCheck{
		blockedRow(nodePostgresRoundtripRowID(in.Stage), in.Stage, now, message),
		blockedRow(nodePostgresEnvironmentRowID(in.Stage), in.Stage, now, message),
		blockedRow(nodePostgresDBRowID(in.Database), in.Database, now, message),
	}
	if in.Unrelated != "" {
		rows = append(rows, RequiredCheck{ID: unrelatedArtifactRowID(in.Unrelated), Check: checkUnrelatedArtifact, Scope: in.Unrelated, Result: CheckBlocked, ObservedAt: now, Message: message})
	}
	return rows
}

// Verify evaluates the four §10.3 rows against a fresh nonce/value pair.
// Resolution order (§10.3 "Independence" + point 4 of the brief contract):
// resolve the three hostnames; a cross-check id mismatch blocks all four
// with zero data-plane calls; resolve the stage URL; fetch+validate the
// database connection; POST then GET (refusing redirects); then the
// nonce-keyed SELECT.
func (v NodePostgresVerifier) Verify(ctx context.Context, in NodePostgresInput) []RequiredCheck {
	now := time.Now().UTC()

	services, err := v.Client.ListServicesDirect(ctx, in.ProjectID)
	if err != nil {
		return blockedNodePostgresRows(in, now, fmt.Sprintf("ListServicesDirect failed: %v", err))
	}
	stageSvc := findServiceByHostname(services, in.Stage)
	dbSvc := findServiceByHostname(services, in.Database)
	var unrelatedSvc *platform.ServiceStack
	if in.Unrelated != "" {
		unrelatedSvc = findServiceByHostname(services, in.Unrelated)
	}

	if stageSvc == nil || dbSvc == nil || (in.Unrelated != "" && unrelatedSvc == nil) {
		return v.missingServiceRows(in, now, stageSvc, dbSvc, unrelatedSvc)
	}

	if (in.ExpectStageID != "" && in.ExpectStageID != stageSvc.ID) ||
		(in.ExpectDatabaseID != "" && in.ExpectDatabaseID != dbSvc.ID) ||
		(in.ExpectUnrelatedID != "" && unrelatedSvc != nil && in.ExpectUnrelatedID != unrelatedSvc.ID) {
		return blockedNodePostgresRows(in, now, "binding mismatch")
	}

	var unchangedRow *RequiredCheck
	if in.Unrelated != "" {
		row := v.evaluateUnchanged(in, unrelatedSvc, now)
		unchangedRow = &row
	}

	stageURL := ops.ResolveSubdomainURL(ctx, v.Client, in.ProjectID, stageSvc)
	if stageURL == "" {
		return appendUnchangedRow([]RequiredCheck{
			blockedRow(nodePostgresRoundtripRowID(in.Stage), in.Stage, now, "URL unresolvable"),
			blockedRow(nodePostgresEnvironmentRowID(in.Stage), in.Stage, now, "URL unresolvable"),
			blockedRow(nodePostgresDBRowID(in.Database), in.Database, now, "URL unresolvable"),
		}, unchangedRow)
	}

	envVars, err := ops.FetchServiceEnv(ctx, v.Client, dbSvc.ID)
	if err != nil {
		msg := "credentials unresolvable"
		return appendUnchangedRow([]RequiredCheck{
			blockedRow(nodePostgresRoundtripRowID(in.Stage), in.Stage, now, msg),
			blockedRow(nodePostgresEnvironmentRowID(in.Stage), in.Stage, now, msg),
			blockedRow(nodePostgresDBRowID(in.Database), in.Database, now, msg),
		}, unchangedRow)
	}
	conn := buildNodePostgresConn(envVars)

	nonce, value := v.Nonce()
	roundtripRow, envRow, gotID, httpErr := v.doHTTP(ctx, stageURL, in, nonce, value, now)

	var dbRow RequiredCheck
	switch {
	case conn.Host != in.Database:
		// Point 4: db host override refused — HTTP still runs normally, but
		// the SELECT never fires against an operator/user-set override.
		dbRow = blockedRow(nodePostgresDBRowID(in.Database), in.Database, now, "db host override refused")
	case httpErr != nil:
		dbRow = blockedRow(nodePostgresDBRowID(in.Database), in.Database, now, "HTTP unreachable, no id to verify against")
	default:
		dbRow = v.evaluateDBRow(ctx, in, conn, nonce, value, gotID, now)
	}

	return appendUnchangedRow([]RequiredCheck{roundtripRow, envRow, dbRow}, unchangedRow)
}

// appendUnchangedRow appends the unrelated_artifact row when the caller
// resolved one (in.Unrelated was non-empty); with no unrelated host
// declared, rows stays at three (§10.3: "the family simply has three
// sub-rows instead of four").
func appendUnchangedRow(rows []RequiredCheck, unchangedRow *RequiredCheck) []RequiredCheck {
	if unchangedRow == nil {
		return rows
	}
	return append(rows, *unchangedRow)
}

// missingServiceRows handles the case where at least one declared hostname
// didn't resolve: the rows tied to a missing host are failed ("not found");
// rows tied to a resolved host are blocked (a dependency is missing, so no
// data-plane call is safe). The unrelated row is omitted entirely when
// in.Unrelated is empty.
func (v NodePostgresVerifier) missingServiceRows(in NodePostgresInput, now time.Time, stageSvc, dbSvc, unrelatedSvc *platform.ServiceStack) []RequiredCheck {
	const depMissing = "dependent service missing"
	row := func(id, check, scope string, found bool) RequiredCheck {
		if !found {
			return RequiredCheck{ID: id, Check: check, Scope: scope, Result: CheckFailed, Expected: "exists", Observed: "not found", ObservedAt: now, Source: "ListServicesDirect", Message: fmt.Sprintf("service %q not found in project", scope)}
		}
		return RequiredCheck{ID: id, Check: check, Scope: scope, Result: CheckBlocked, ObservedAt: now, Message: depMissing}
	}
	rows := []RequiredCheck{
		row(nodePostgresRoundtripRowID(in.Stage), checkNodePostgresRecord, in.Stage, stageSvc != nil),
		row(nodePostgresEnvironmentRowID(in.Stage), checkNodePostgresRecord, in.Stage, stageSvc != nil),
		row(nodePostgresDBRowID(in.Database), checkNodePostgresRecord, in.Database, dbSvc != nil),
	}
	if in.Unrelated != "" {
		rows = append(rows, row(unrelatedArtifactRowID(in.Unrelated), checkUnrelatedArtifact, in.Unrelated, unrelatedSvc != nil))
	}
	return rows
}

// blockedRow builds one blocked node_postgres_record row — the only check
// family every blockedRow call site in this file needs.
func blockedRow(id, scope string, now time.Time, message string) RequiredCheck {
	return RequiredCheck{ID: id, Check: checkNodePostgresRecord, Scope: scope, Result: CheckBlocked, ObservedAt: now, Message: message}
}

// evaluateUnchanged decides the unrelated_artifact/<unrelated>/unchanged
// row: passed when the active app-version id at the freeze equals the one
// recorded at scenario start; blocked when no baseline was recorded.
func (v NodePostgresVerifier) evaluateUnchanged(in NodePostgresInput, unrelatedSvc *platform.ServiceStack, now time.Time) RequiredCheck {
	return gradeUnchangedRow(unrelatedArtifactRowID(in.Unrelated), in.Unrelated, in.BaselineUnrelatedAppVersion, unrelatedSvc, now)
}

// gradeUnchangedRow grades one "unrelated artifact unchanged" row
// (docs/spec-testing-architecture.md §10.3): passed when the active
// app-version id at the freeze equals the one recorded at scenario start,
// failed when it differs, blocked when no baseline was recorded. Shared by
// nodePostgresRecord's unrelated row (above) and the standalone
// verification.unchanged field (docs/spec-eval-farm.md §4.1 FM-29,
// internal/eval/verification.go) — one grading rule, two callers.
func gradeUnchangedRow(id, hostname, baselineAppVersion string, svc *platform.ServiceStack, now time.Time) RequiredCheck {
	if baselineAppVersion == "" {
		return RequiredCheck{ID: id, Check: checkUnrelatedArtifact, Scope: hostname, Result: CheckBlocked, ObservedAt: now, Source: "ListServicesDirect", Message: "no baseline recorded"}
	}
	current := ""
	if svc != nil && svc.ActiveAppVersion != nil {
		current = svc.ActiveAppVersion.ID
	}
	if current == baselineAppVersion {
		return RequiredCheck{ID: id, Check: checkUnrelatedArtifact, Scope: hostname, Result: CheckPassed, Expected: baselineAppVersion, Observed: current, ObservedAt: now, Source: "ListServicesDirect", Message: "unrelated artifact unchanged"}
	}
	return RequiredCheck{ID: id, Check: checkUnrelatedArtifact, Scope: hostname, Result: CheckFailed, Expected: baselineAppVersion, Observed: current, ObservedAt: now, Source: "ListServicesDirect", Message: "unrelated active app-version changed"}
}

// nodePostgresGETBody is the shape the record-roundtrip's GET /records/<id>
// is expected to answer with.
type nodePostgresGETBody struct {
	ID          jsonScalarString `json:"id"`
	Nonce       string           `json:"nonce"`
	Value       string           `json:"value"`
	Environment string           `json:"environment"`
}

// jsonScalarString accepts a JSON string or number for an id (a SERIAL
// primary key is a number; the comparison is by string form either way).
type jsonScalarString string

func (v *jsonScalarString) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "null" {
		*v = ""
		return nil
	}
	var asString string
	if err := json.Unmarshal(data, &asString); err == nil {
		*v = jsonScalarString(asString)
		return nil
	}
	var asNumber json.Number
	if err := json.Unmarshal(data, &asNumber); err != nil {
		return fmt.Errorf("id must be a JSON string or number: %w", err)
	}
	*v = jsonScalarString(asNumber.String())
	return nil
}

// expectedEnvironmentLiteral is the fixed string a well-formed stage
// deployment persists as `environment` (§10.3 row table).
const expectedEnvironmentLiteral = "stage"

// doHTTP performs POST /records then GET /records/<id> against baseURL,
// refusing redirects and never following a server-supplied absolute URL
// (the GET target is always built from baseURL + the POST-returned id).
// Returns the roundtrip/environment rows and the id the app returned, for
// the caller to cross-check against the database row.
func (v NodePostgresVerifier) doHTTP(ctx context.Context, baseURL string, in NodePostgresInput, nonce, value string, now time.Time) (roundtripRow, envRow RequiredCheck, gotID string, err error) {
	roundtripID := nodePostgresRoundtripRowID(in.Stage)
	envID := nodePostgresEnvironmentRowID(in.Stage)

	postBody, marshalErr := json.Marshal(map[string]string{"nonce": nonce, "value": value})
	if marshalErr != nil {
		msg := fmt.Sprintf("marshal POST body: %v", marshalErr)
		return blockedRow(roundtripID, in.Stage, now, msg), blockedRow(envID, in.Stage, now, msg), "", marshalErr
	}
	req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/records", bytes.NewReader(postBody))
	if reqErr != nil {
		msg := fmt.Sprintf("build POST request: %v", reqErr)
		return blockedRow(roundtripID, in.Stage, now, msg), blockedRow(envID, in.Stage, now, msg), "", reqErr
	}
	req.Header.Set("Content-Type", "application/json")
	resp, doErr := v.HTTP.Do(req)
	if doErr != nil {
		msg := fmt.Sprintf("POST %s: %v", baseURL, doErr)
		return blockedRow(roundtripID, in.Stage, now, msg), blockedRow(envID, in.Stage, now, msg), "", doErr
	}
	defer resp.Body.Close()
	if isRedirect(resp.StatusCode) {
		msg := fmt.Sprintf("POST %s returned redirect %d — refused", baseURL, resp.StatusCode)
		return blockedRow(roundtripID, in.Stage, now, msg), blockedRow(envID, in.Stage, now, msg), "", fmt.Errorf("redirect refused")
	}
	postBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := fmt.Sprintf("POST %s returned %d", baseURL, resp.StatusCode)
		row := RequiredCheck{ID: roundtripID, Check: checkNodePostgresRecord, Scope: in.Stage, Result: CheckFailed, Expected: "201", Observed: fmt.Sprintf("%d", resp.StatusCode), ObservedAt: now, Source: "HTTP POST " + baseURL, Message: msg}
		return row, blockedRow(envID, in.Stage, now, "POST did not succeed"), "", fmt.Errorf("non-2xx")
	}
	var postResp nodePostgresGETBody
	_ = json.Unmarshal(postBytes, &postResp)
	if string(postResp.ID) == "" {
		msg := "POST response carried no id"
		return failedRoundtripRow(roundtripID, in.Stage, now, msg), blockedRow(envID, in.Stage, now, msg), "", fmt.Errorf("no id")
	}

	getURL := strings.TrimRight(baseURL, "/") + "/records/" + string(postResp.ID)
	getReq, getReqErr := http.NewRequestWithContext(ctx, http.MethodGet, getURL, http.NoBody)
	if getReqErr != nil {
		msg := fmt.Sprintf("build GET request: %v", getReqErr)
		return blockedRow(roundtripID, in.Stage, now, msg), blockedRow(envID, in.Stage, now, msg), "", getReqErr
	}
	getResp, getErr := v.HTTP.Do(getReq)
	if getErr != nil {
		msg := fmt.Sprintf("GET %s: %v", getURL, getErr)
		return blockedRow(roundtripID, in.Stage, now, msg), blockedRow(envID, in.Stage, now, msg), "", getErr
	}
	defer getResp.Body.Close()
	if isRedirect(getResp.StatusCode) {
		msg := fmt.Sprintf("GET %s returned redirect %d — refused", getURL, getResp.StatusCode)
		return blockedRow(roundtripID, in.Stage, now, msg), blockedRow(envID, in.Stage, now, msg), "", fmt.Errorf("redirect refused")
	}
	getBytes, _ := io.ReadAll(io.LimitReader(getResp.Body, 4<<10))
	if getResp.StatusCode < 200 || getResp.StatusCode >= 300 {
		msg := fmt.Sprintf("GET %s returned %d", getURL, getResp.StatusCode)
		row := RequiredCheck{ID: roundtripID, Check: checkNodePostgresRecord, Scope: in.Stage, Result: CheckFailed, Expected: "200", Observed: fmt.Sprintf("%d", getResp.StatusCode), ObservedAt: now, Source: "HTTP GET " + getURL, Message: msg}
		return row, blockedRow(envID, in.Stage, now, "GET did not succeed"), "", fmt.Errorf("non-2xx")
	}
	var getBody nodePostgresGETBody
	_ = json.Unmarshal(getBytes, &getBody)

	roundtripPass := getBody.ID == postResp.ID && getBody.Nonce == nonce && getBody.Value == value
	roundtripRow = RequiredCheck{
		ID: roundtripID, Check: checkNodePostgresRecord, Scope: in.Stage,
		Expected:   fmt.Sprintf("id=%s nonce=%s value=%s", string(postResp.ID), nonce, value),
		Observed:   fmt.Sprintf("id=%s nonce=%s value=%s", string(getBody.ID), getBody.Nonce, getBody.Value),
		ObservedAt: now, Source: "HTTP GET " + getURL,
	}
	if roundtripPass {
		roundtripRow.Result = CheckPassed
		roundtripRow.Message = "POST/GET roundtrip matched the verifier's own nonce/value/id"
	} else {
		roundtripRow.Result = CheckFailed
		roundtripRow.Message = "POST/GET roundtrip did not match the verifier's own nonce/value/id"
	}

	envPass := getBody.Environment == expectedEnvironmentLiteral
	envRow = RequiredCheck{
		ID: envID, Check: checkNodePostgresRecord, Scope: in.Stage,
		Expected: expectedEnvironmentLiteral, Observed: getBody.Environment,
		ObservedAt: now, Source: "HTTP GET " + getURL,
	}
	if envPass {
		envRow.Result = CheckPassed
		envRow.Message = "GET body environment matched"
	} else {
		envRow.Result = CheckFailed
		envRow.Message = "GET body environment did not match"
	}

	return roundtripRow, envRow, string(postResp.ID), nil
}

func failedRoundtripRow(id, stage string, now time.Time, message string) RequiredCheck {
	return RequiredCheck{ID: id, Check: checkNodePostgresRecord, Scope: stage, Result: CheckFailed, ObservedAt: now, Message: message}
}

func isRedirect(status int) bool { return status >= 300 && status < 400 }

// evaluateDBRow decides the node_postgres_record/<database>/db_row row: the
// SELECT is keyed on nonce only (never on the app-returned id — §10.3
// "Independence"); the id/value it returns are then compared against the
// verifier's own value and the GET-returned id.
func (v NodePostgresVerifier) evaluateDBRow(ctx context.Context, in NodePostgresInput, conn NodePostgresConn, nonce, value, gotID string, now time.Time) RequiredCheck {
	id := nodePostgresDBRowID(in.Database)
	dbID, dbValue, count, err := v.DB.QueryRecordByNonce(ctx, conn, nonce)
	if err != nil {
		return RequiredCheck{ID: id, Check: checkNodePostgresRecord, Scope: in.Database, Result: CheckBlocked, ObservedAt: now, Source: "SELECT records WHERE nonce=$1", Message: sanitizeSecretError(err.Error(), conn.Password)}
	}
	if count == 0 {
		return RequiredCheck{ID: id, Check: checkNodePostgresRecord, Scope: in.Database, Result: CheckFailed, Expected: "exactly 1 row", Observed: "0 rows", ObservedAt: now, Source: "SELECT records WHERE nonce=$1", Message: "no row found for the verifier's nonce"}
	}
	if count > 1 {
		return RequiredCheck{ID: id, Check: checkNodePostgresRecord, Scope: in.Database, Result: CheckFailed, Expected: "exactly 1 row", Observed: fmt.Sprintf("%d rows", count), ObservedAt: now, Source: "SELECT records WHERE nonce=$1", Message: "more than one row found for the verifier's nonce"}
	}
	if dbID != gotID || dbValue != value {
		return RequiredCheck{
			ID: id, Check: checkNodePostgresRecord, Scope: in.Database, Result: CheckFailed,
			Expected: fmt.Sprintf("id=%s value=%s", gotID, value), Observed: fmt.Sprintf("id=%s value=%s", dbID, dbValue),
			ObservedAt: now, Source: "SELECT records WHERE nonce=$1", Message: "database row does not match the app's response",
		}
	}
	return RequiredCheck{
		ID: id, Check: checkNodePostgresRecord, Scope: in.Database, Result: CheckPassed,
		Expected: fmt.Sprintf("id=%s value=%s", gotID, value), Observed: fmt.Sprintf("id=%s value=%s", dbID, dbValue),
		ObservedAt: now, Source: "SELECT records WHERE nonce=$1", Message: "database row matches",
	}
}

// sanitizeSecretError replaces an error string with a fixed sentence when it
// contains the database password or a full DSN — driver errors must never
// leak credentials into a row's Message (§10.3 "Independence").
func sanitizeSecretError(msg, password string) string {
	const sentinel = "database error (details withheld)"
	if password != "" && strings.Contains(msg, password) {
		return sentinel
	}
	if strings.Contains(msg, "postgres://") || strings.Contains(msg, "postgresql://") {
		return sentinel
	}
	return msg
}

// buildNodePostgresConn builds a NodePostgresConn from a service's env vars
// (hostname/port/user/password/dbName only — §10.3 "Independence").
func buildNodePostgresConn(envVars []platform.ServiceEnvVar) NodePostgresConn {
	var conn NodePostgresConn
	for _, e := range envVars {
		switch e.Key {
		case "hostname":
			conn.Host = e.Content
		case "port":
			conn.Port = e.Content
		case "user":
			conn.User = e.Content
		case "password":
			conn.Password = e.Content
		case "dbName":
			conn.DBName = e.Content
		}
	}
	return conn
}
