package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// fakeNodePostgresDB is the offline stand-in for NodePostgresDB: a map keyed
// on nonce, with an optional forced error for the "DB unreachable" cases.
type fakeNodePostgresDB struct {
	rowsByNonce map[string]fakeDBRow
	err         error
	calls       int
}

type fakeDBRow struct {
	id    string
	value string
}

func (f *fakeNodePostgresDB) QueryRecordByNonce(_ context.Context, _ NodePostgresConn, nonce string) (id, value string, count int, err error) {
	f.calls++
	if f.err != nil {
		return "", "", 0, f.err
	}
	row, ok := f.rowsByNonce[nonce]
	if !ok {
		return "", "", 0, nil
	}
	return row.id, row.value, 1, nil
}

// nodePostgresAppServer builds an httptest.NewServer standing in for the
// Node application: POST /records stores in memory and GET /records/<id>
// replays it, with knobs for the failure-shape tests.
type nodePostgresAppServer struct {
	environment   string
	numericIDs    bool
	nonceOverride string
	postCalls     int
	getCalls      int
	stored        map[string]storedRecord
}

type storedRecord struct {
	ID          string `json:"id"`
	Nonce       string `json:"nonce"`
	Value       string `json:"value"`
	Environment string `json:"environment"`
}

// encode renders a record the way the app under test would: string ids by
// default, JSON numbers when numericIDs is set (a SERIAL primary key).
func (s *nodePostgresAppServer) encode(rec storedRecord) any {
	if !s.numericIDs {
		return rec
	}
	var n int
	_, _ = fmt.Sscanf(rec.ID, "%d", &n)
	return map[string]any{"id": n, "nonce": rec.Nonce, "value": rec.Value, "environment": rec.Environment}
}

func newNodePostgresAppServer(environment string) *nodePostgresAppServer {
	return &nodePostgresAppServer{environment: environment, stored: map[string]storedRecord{}}
}

func (s *nodePostgresAppServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/records":
			s.postCalls++
			var in struct {
				Nonce string `json:"nonce"`
				Value string `json:"value"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			id := fmt.Sprintf("rec-%d", len(s.stored)+1)
			nonce := in.Nonce
			if s.nonceOverride != "" {
				nonce = s.nonceOverride
			}
			if s.numericIDs {
				id = fmt.Sprintf("%d", len(s.stored)+1)
			}
			rec := storedRecord{ID: id, Nonce: nonce, Value: in.Value, Environment: s.environment}
			s.stored[id] = rec
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(s.encode(rec))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/records/"):
			s.getCalls++
			id := strings.TrimPrefix(r.URL.Path, "/records/")
			rec, ok := s.stored[id]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(s.encode(rec))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

// loopbackHTTPClient builds an *http.Client whose transport dials the given
// httptest.Server's actual listener regardless of the request's Host —
// ResolveSubdomainURL always synthesizes a public-looking
// https://<hostname>-<prefix>.<domain> URL (internal/ops/discover.go
// BuildSubdomainURL), so the offline harness redirects the TRANSPORT to
// loopback rather than trying to make the resolver emit a literal loopback
// string. Redirects are refused via http.ErrUseLastResponse (§10.3: "a
// client that refuses redirects").
func loopbackHTTPClient(server *httptest.Server) *http.Client {
	addr := server.Listener.Addr().String()
	dial := func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, addr)
	}
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{
			DialContext:    dial,
			DialTLSContext: dial,
		},
	}
}

// mockWithSubdomainFixture builds a platform.Mock wired so
// ops.ResolveSubdomainURL(ctx, client, projectID, stageSvc) resolves to a
// deterministic https URL an httptest server can be dialed through (see
// loopbackHTTPClient), for the "appstage" hostname every test in this file
// uses as its stage service.
func mockWithSubdomainFixture(extraServices ...platform.ServiceStack) *platform.Mock {
	svc := platform.ServiceStack{
		ID: "appstage-1", Name: "appstage", Status: "ACTIVE",
		SubdomainAccess: true,
		Ports:           []platform.Port{{Port: 80, Scheme: "http"}},
	}
	services := append([]platform.ServiceStack{svc}, extraServices...)
	return platform.NewMock().
		WithServicesDirect(services).
		WithProject(&platform.Project{ID: "proj-1", SubdomainHost: "testproj.example.com"})
}

// fixedNonce returns a Nonce func fixed at "nonce-1"/"value-1" — every test
// in this file that needs a deterministic pair uses the same one.
func fixedNonce() func() (string, string) {
	return func() (string, string) { return "nonce-1", "value-1" }
}

// TestNodePostgresVerifier_KnownGood_AllRowsPassed pins
// docs/spec-testing-architecture.md §10.3: a known-good app + a DB that
// agrees with the verifier's own nonce/value/id, plus an unchanged baseline,
// yields four passed rows with the §10.3 ids.
func TestNodePostgresVerifier_KnownGood_AllRowsPassed(t *testing.T) {
	app := newNodePostgresAppServer("stage")
	server := httptest.NewServer(app.handler())
	defer server.Close()

	client := mockWithSubdomainFixture(
		platform.ServiceStack{ID: "db-1", Name: "db", Status: "ACTIVE"},
		platform.ServiceStack{ID: "other-1", Name: "other", Status: "ACTIVE", ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-1"}},
	).WithServiceEnv("db-1", []platform.ServiceEnvVar{
		{Key: "hostname", Content: "db"},
		{Key: "port", Content: "5432"},
		{Key: "user", Content: "u"},
		{Key: "password", Content: "p"},
		{Key: "dbName", Content: "d"},
	})

	db := &fakeNodePostgresDB{rowsByNonce: map[string]fakeDBRow{"nonce-1": {id: "rec-1", value: "value-1"}}}

	v := NodePostgresVerifier{
		Client: client,
		HTTP:   loopbackHTTPClient(server),
		DB:     db,
		Nonce:  fixedNonce(),
	}
	rows := v.Verify(context.Background(), NodePostgresInput{
		ProjectID: "proj-1", Stage: "appstage", Database: "db", Unrelated: "other",
		BaselineUnrelatedAppVersion: "av-1",
	})

	wantIDs := map[string]CheckResult{
		"node_postgres_record/appstage/record_roundtrip": CheckPassed,
		"node_postgres_record/appstage/environment":      CheckPassed,
		"node_postgres_record/db/db_row":                 CheckPassed,
		"unrelated_artifact/other/unchanged":             CheckPassed,
	}
	if len(rows) != 4 {
		t.Fatalf("rows = %+v, want 4", rows)
	}
	for _, row := range rows {
		want, ok := wantIDs[row.ID]
		if !ok {
			t.Errorf("unexpected row id %q", row.ID)
			continue
		}
		if row.Result != want {
			t.Errorf("row %s = %s, want %s (message: %s)", row.ID, row.Result, want, row.Message)
		}
	}
	if app.postCalls != 1 || app.getCalls != 1 {
		t.Errorf("postCalls=%d getCalls=%d, want 1 and 1", app.postCalls, app.getCalls)
	}
	if db.calls != 1 {
		t.Errorf("db.calls = %d, want 1", db.calls)
	}
}

// TestNodePostgresVerifier_PlausibleHTTPWithoutManagedRow_Failed pins
// docs/spec-testing-architecture.md §10.3's central defense: a fake app that
// answers 201/200 correctly but stores only in memory fails db_row alone —
// the row table's "plausible in-memory fake fails HERE".
func TestNodePostgresVerifier_PlausibleHTTPWithoutManagedRow_Failed(t *testing.T) {
	app := newNodePostgresAppServer("stage")
	server := httptest.NewServer(app.handler())
	defer server.Close()

	client := mockWithSubdomainFixture(
		platform.ServiceStack{ID: "db-1", Name: "db", Status: "ACTIVE"},
		platform.ServiceStack{ID: "other-1", Name: "other", Status: "ACTIVE", ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-1"}},
	).WithServiceEnv("db-1", []platform.ServiceEnvVar{
		{Key: "hostname", Content: "db"}, {Key: "port", Content: "5432"},
		{Key: "user", Content: "u"}, {Key: "password", Content: "p"}, {Key: "dbName", Content: "d"},
	})
	db := &fakeNodePostgresDB{} // no rows at all — the plausible fake

	v := NodePostgresVerifier{Client: client, HTTP: loopbackHTTPClient(server), DB: db, Nonce: fixedNonce()}
	rows := v.Verify(context.Background(), NodePostgresInput{
		ProjectID: "proj-1", Stage: "appstage", Database: "db", Unrelated: "other",
		BaselineUnrelatedAppVersion: "av-1",
	})
	got := rowsByID(t, rows)
	assertResult(t, got, "node_postgres_record/appstage/record_roundtrip", CheckPassed)
	assertResult(t, got, "node_postgres_record/appstage/environment", CheckPassed)
	assertResult(t, got, "node_postgres_record/db/db_row", CheckFailed)
	assertResult(t, got, "unrelated_artifact/other/unchanged", CheckPassed)
}

func rowsByID(t *testing.T, rows []RequiredCheck) map[string]RequiredCheck {
	t.Helper()
	out := make(map[string]RequiredCheck, len(rows))
	for _, r := range rows {
		out[r.ID] = r
	}
	return out
}

func assertResult(t *testing.T, rows map[string]RequiredCheck, id string, want CheckResult) {
	t.Helper()
	row, ok := rows[id]
	if !ok {
		t.Errorf("missing row %q", id)
		return
	}
	if row.Result != want {
		t.Errorf("row %s = %s, want %s (message: %s)", id, row.Result, want, row.Message)
	}
}

func nodePostgresBaseInput() NodePostgresInput {
	return NodePostgresInput{ProjectID: "proj-1", Stage: "appstage", Database: "db", Unrelated: "other", BaselineUnrelatedAppVersion: "av-1"}
}

func nodePostgresFixtureClient() *platform.Mock {
	return mockWithSubdomainFixture(
		platform.ServiceStack{ID: "db-1", Name: "db", Status: "ACTIVE"},
		platform.ServiceStack{ID: "other-1", Name: "other", Status: "ACTIVE", ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-1"}},
	).WithServiceEnv("db-1", []platform.ServiceEnvVar{
		{Key: "hostname", Content: "db"}, {Key: "port", Content: "5432"},
		{Key: "user", Content: "u"}, {Key: "password", Content: "p"}, {Key: "dbName", Content: "d"},
	})
}

// TestNodePostgresVerifier_AppReturnsDifferentNonce_Failed pins §10.3: a GET
// reply that echoes a different nonce than the verifier's own fails
// record_roundtrip only.
func TestNodePostgresVerifier_AppReturnsDifferentNonce_Failed(t *testing.T) {
	app := newNodePostgresAppServer("stage")
	app.nonceOverride = "not-the-verifiers-nonce"
	server := httptest.NewServer(app.handler())
	defer server.Close()

	client := nodePostgresFixtureClient()
	db := &fakeNodePostgresDB{rowsByNonce: map[string]fakeDBRow{"not-the-verifiers-nonce": {id: "rec-1", value: "value-1"}}}

	v := NodePostgresVerifier{Client: client, HTTP: loopbackHTTPClient(server), DB: db, Nonce: fixedNonce()}
	rows := v.Verify(context.Background(), nodePostgresBaseInput())
	got := rowsByID(t, rows)
	assertResult(t, got, "node_postgres_record/appstage/record_roundtrip", CheckFailed)
}

// TestNodePostgresVerifier_WrongEnvironment_Failed pins §10.3: a GET body
// whose environment differs from the literal "stage" fails the environment
// row only.
func TestNodePostgresVerifier_WrongEnvironment_Failed(t *testing.T) {
	app := newNodePostgresAppServer("dev")
	server := httptest.NewServer(app.handler())
	defer server.Close()

	client := nodePostgresFixtureClient()
	db := &fakeNodePostgresDB{rowsByNonce: map[string]fakeDBRow{"nonce-1": {id: "rec-1", value: "value-1"}}}

	v := NodePostgresVerifier{Client: client, HTTP: loopbackHTTPClient(server), DB: db, Nonce: fixedNonce()}
	rows := v.Verify(context.Background(), nodePostgresBaseInput())
	got := rowsByID(t, rows)
	assertResult(t, got, "node_postgres_record/appstage/record_roundtrip", CheckPassed)
	assertResult(t, got, "node_postgres_record/appstage/environment", CheckFailed)
}

// TestNodePostgresVerifier_UnrelatedArtifactChanged_Failed pins §10.3's
// baseline: a differing active app-version id fails unchanged; a missing
// baseline blocks it instead.
func TestNodePostgresVerifier_UnrelatedArtifactChanged_Failed(t *testing.T) {
	app := newNodePostgresAppServer("stage")
	server := httptest.NewServer(app.handler())
	defer server.Close()
	db := &fakeNodePostgresDB{rowsByNonce: map[string]fakeDBRow{"nonce-1": {id: "rec-1", value: "value-1"}}}

	t.Run("changed", func(t *testing.T) {
		client := nodePostgresFixtureClient()
		v := NodePostgresVerifier{Client: client, HTTP: loopbackHTTPClient(server), DB: db, Nonce: fixedNonce()}
		in := nodePostgresBaseInput()
		in.BaselineUnrelatedAppVersion = "av-old"
		rows := v.Verify(context.Background(), in)
		got := rowsByID(t, rows)
		assertResult(t, got, "unrelated_artifact/other/unchanged", CheckFailed)
	})

	t.Run("missing baseline", func(t *testing.T) {
		client := nodePostgresFixtureClient()
		v := NodePostgresVerifier{Client: client, HTTP: loopbackHTTPClient(server), DB: db, Nonce: fixedNonce()}
		in := nodePostgresBaseInput()
		in.BaselineUnrelatedAppVersion = ""
		rows := v.Verify(context.Background(), in)
		got := rowsByID(t, rows)
		assertResult(t, got, "unrelated_artifact/other/unchanged", CheckBlocked)
	})
}

// TestNodePostgresVerifier_RequiredDBUnavailable_Blocked pins §10.3: a DB
// connection error blocks db_row only — the HTTP rows are unaffected.
func TestNodePostgresVerifier_RequiredDBUnavailable_Blocked(t *testing.T) {
	app := newNodePostgresAppServer("stage")
	server := httptest.NewServer(app.handler())
	defer server.Close()

	client := nodePostgresFixtureClient()
	db := &fakeNodePostgresDB{err: fmt.Errorf("dial tcp: connection refused")}

	v := NodePostgresVerifier{Client: client, HTTP: loopbackHTTPClient(server), DB: db, Nonce: fixedNonce()}
	rows := v.Verify(context.Background(), nodePostgresBaseInput())
	got := rowsByID(t, rows)
	assertResult(t, got, "node_postgres_record/appstage/record_roundtrip", CheckPassed)
	assertResult(t, got, "node_postgres_record/appstage/environment", CheckPassed)
	assertResult(t, got, "node_postgres_record/db/db_row", CheckBlocked)
	assertResult(t, got, "unrelated_artifact/other/unchanged", CheckPassed)
}

// TestNodePostgresVerifier_ForeignOrMissingBinding_ZeroDataPlaneCalls pins
// §10.3's independence rule: a cross-check id mismatch, an unresolvable URL,
// and a DB env hostname override all block the affected rows with zero HTTP
// and zero DB calls where the contract says zero.
func TestNodePostgresVerifier_ForeignOrMissingBinding_ZeroDataPlaneCalls(t *testing.T) {
	t.Run("cross-check id mismatch — zero HTTP and zero DB calls", func(t *testing.T) {
		app := newNodePostgresAppServer("stage")
		server := httptest.NewServer(app.handler())
		defer server.Close()
		client := nodePostgresFixtureClient()
		db := &fakeNodePostgresDB{rowsByNonce: map[string]fakeDBRow{"nonce-1": {id: "rec-1", value: "value-1"}}}

		v := NodePostgresVerifier{Client: client, HTTP: loopbackHTTPClient(server), DB: db, Nonce: fixedNonce()}
		in := nodePostgresBaseInput()
		in.ExpectStageID = "not-the-resolved-id"
		rows := v.Verify(context.Background(), in)
		got := rowsByID(t, rows)
		for _, id := range []string{
			"node_postgres_record/appstage/record_roundtrip", "node_postgres_record/appstage/environment",
			"node_postgres_record/db/db_row", "unrelated_artifact/other/unchanged",
		} {
			assertResult(t, got, id, CheckBlocked)
		}
		if app.postCalls != 0 || app.getCalls != 0 {
			t.Errorf("postCalls=%d getCalls=%d, want 0 and 0", app.postCalls, app.getCalls)
		}
		if db.calls != 0 {
			t.Errorf("db.calls = %d, want 0", db.calls)
		}
	})

	t.Run("unresolvable URL — zero HTTP and zero DB calls", func(t *testing.T) {
		app := newNodePostgresAppServer("stage")
		server := httptest.NewServer(app.handler())
		defer server.Close()
		// No SubdomainAccess — ResolveSubdomainURL yields "".
		client := platform.NewMock().WithServicesDirect([]platform.ServiceStack{
			{ID: "appstage-1", Name: "appstage", Status: "ACTIVE", SubdomainAccess: false},
			{ID: "db-1", Name: "db", Status: "ACTIVE"},
			{ID: "other-1", Name: "other", Status: "ACTIVE", ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-1"}},
		}).WithProject(&platform.Project{ID: "proj-1", SubdomainHost: "testproj.example.com"})
		db := &fakeNodePostgresDB{}

		v := NodePostgresVerifier{Client: client, HTTP: loopbackHTTPClient(server), DB: db, Nonce: fixedNonce()}
		rows := v.Verify(context.Background(), nodePostgresBaseInput())
		got := rowsByID(t, rows)
		assertResult(t, got, "node_postgres_record/appstage/record_roundtrip", CheckBlocked)
		assertResult(t, got, "node_postgres_record/appstage/environment", CheckBlocked)
		assertResult(t, got, "node_postgres_record/db/db_row", CheckBlocked)
		if app.postCalls != 0 || app.getCalls != 0 {
			t.Errorf("postCalls=%d getCalls=%d, want 0 and 0", app.postCalls, app.getCalls)
		}
		if db.calls != 0 {
			t.Errorf("db.calls = %d, want 0", db.calls)
		}
	})

	t.Run("db env hostname override — zero DB calls", func(t *testing.T) {
		app := newNodePostgresAppServer("stage")
		server := httptest.NewServer(app.handler())
		defer server.Close()
		client := mockWithSubdomainFixture(
			platform.ServiceStack{ID: "db-1", Name: "db", Status: "ACTIVE"},
			platform.ServiceStack{ID: "other-1", Name: "other", Status: "ACTIVE", ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-1"}},
		).WithServiceEnv("db-1", []platform.ServiceEnvVar{
			{Key: "hostname", Content: "user-overridden-host"}, {Key: "port", Content: "5432"},
			{Key: "user", Content: "u"}, {Key: "password", Content: "p"}, {Key: "dbName", Content: "d"},
		})
		db := &fakeNodePostgresDB{}

		v := NodePostgresVerifier{Client: client, HTTP: loopbackHTTPClient(server), DB: db, Nonce: fixedNonce()}
		rows := v.Verify(context.Background(), nodePostgresBaseInput())
		got := rowsByID(t, rows)
		assertResult(t, got, "node_postgres_record/db/db_row", CheckBlocked)
		if db.calls != 0 {
			t.Errorf("db.calls = %d, want 0", db.calls)
		}
	})
}

// TestNodePostgresVerifier_RedirectRefused_Blocked pins §10.3: a 302 answer
// to the POST blocks record_roundtrip without a second request.
func TestNodePostgresVerifier_RedirectRefused_Blocked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://attacker.example/records")
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()

	client := nodePostgresFixtureClient()
	db := &fakeNodePostgresDB{}

	v := NodePostgresVerifier{Client: client, HTTP: loopbackHTTPClient(server), DB: db, Nonce: fixedNonce()}
	rows := v.Verify(context.Background(), nodePostgresBaseInput())
	got := rowsByID(t, rows)
	assertResult(t, got, "node_postgres_record/appstage/record_roundtrip", CheckBlocked)
	if db.calls != 0 {
		t.Errorf("db.calls = %d, want 0 (POST never got past the redirect)", db.calls)
	}
}

// TestNodePostgresVerifier_UnsettledFreeze_BlockedWithoutCalls pins
// docs/spec-testing-architecture.md §10.2/§10.3 "Ordering and settle": an
// unsettled task-end freeze emits the four oracle rows blocked, through
// generateRequiredChecks, without constructing the verifier at all — zero
// HTTP and zero DB calls.
func TestNodePostgresVerifier_UnsettledFreeze_BlockedWithoutCalls(t *testing.T) {
	sc := &Scenario{Verification: &VerificationConfig{
		Mode:               VerificationRequired,
		NodePostgresRecord: &NodePostgresRecordConfig{Stage: "appstage", Database: "db", Unrelated: "other"},
	}}
	client := nodePostgresFixtureClient()
	guardHTTP := &noHTTPGuard{t: t}
	rows := generateRequiredChecks(context.Background(), sc, platformObservation{}, guardHTTP, time.Now(), "proj-1", client, false, &ScenarioBaseline{UnrelatedAppVersion: "av-1"})
	got := rowsByID(t, rows)
	for _, id := range []string{
		"node_postgres_record/appstage/record_roundtrip", "node_postgres_record/appstage/environment",
		"node_postgres_record/db/db_row", "unrelated_artifact/other/unchanged",
	} {
		assertResult(t, got, id, CheckBlocked)
	}
	if guardHTTP.calls != 0 {
		t.Errorf("HTTP calls = %d, want 0", guardHTTP.calls)
	}
}

// TestNodePostgresVerifier_SecretsInDriverError_NotExposed pins §10.3
// "Independence": a driver error embedding the password never reaches a row
// or message.
func TestNodePostgresVerifier_SecretsInDriverError_NotExposed(t *testing.T) {
	app := newNodePostgresAppServer("stage")
	server := httptest.NewServer(app.handler())
	defer server.Close()

	const password = "s3cr3t-password"
	client := mockWithSubdomainFixture(
		platform.ServiceStack{ID: "db-1", Name: "db", Status: "ACTIVE"},
		platform.ServiceStack{ID: "other-1", Name: "other", Status: "ACTIVE", ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-1"}},
	).WithServiceEnv("db-1", []platform.ServiceEnvVar{
		{Key: "hostname", Content: "db"}, {Key: "port", Content: "5432"},
		{Key: "user", Content: "u"}, {Key: "password", Content: password}, {Key: "dbName", Content: "d"},
	})
	db := &fakeNodePostgresDB{err: fmt.Errorf("dial postgres://u:%s@db:5432/d: connection refused", password)}

	v := NodePostgresVerifier{Client: client, HTTP: loopbackHTTPClient(server), DB: db, Nonce: fixedNonce()}
	rows := v.Verify(context.Background(), nodePostgresBaseInput())
	for _, row := range rows {
		if strings.Contains(row.Message, password) || strings.Contains(row.Expected, password) || strings.Contains(row.Observed, password) {
			t.Errorf("row %s leaked the password: %+v", row.ID, row)
		}
	}
}

// TestNodePostgresDSN_SpecialCharacters_Escaped pins that the production
// DSN escapes user, password and database name as URL components, so a
// platform-generated password with '+', ' ', '/' or '@' connects instead of
// blocking db_row on a parse error.
func TestNodePostgresDSN_SpecialCharacters_Escaped(t *testing.T) {
	t.Parallel()
	dsn := nodePostgresDSN(NodePostgresConn{Host: "db", Port: "5432", User: "u ser", Password: "p+a/s@s w", DBName: "d b"})
	// '+' is a literal in URL userinfo (RFC 3986), so it stays as-is; every
	// other reserved character is percent-encoded and round-trips exactly.
	want := "postgres://u%20ser:p+a%2Fs%40s%20w@db:5432/d%20b"
	if dsn != want {
		t.Fatalf("dsn = %q, want %q", dsn, want)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	if pw, _ := parsed.User.Password(); pw != "p+a/s@s w" || parsed.User.Username() != "u ser" || parsed.Path != "/d b" {
		t.Fatalf("round trip = user %q password %q path %q", parsed.User.Username(), pw, parsed.Path)
	}
}

// TestNodePostgresVerifier_NumericJSONID_Accepted pins that an application
// answering with a JSON number id (`{"id":1,...}`, what a SERIAL primary key
// naturally produces) is a valid roundtrip: the verifier compares ids as
// strings and never rejects a numeric id as "no id" (live S5 run 3, 2026-09-08).
func TestNodePostgresVerifier_NumericJSONID_Accepted(t *testing.T) {
	app := newNodePostgresAppServer("stage")
	app.numericIDs = true
	server := httptest.NewServer(app.handler())
	defer server.Close()

	client := mockWithSubdomainFixture(
		platform.ServiceStack{ID: "db-1", Name: "db", Status: "ACTIVE"},
		platform.ServiceStack{ID: "other-1", Name: "other", Status: "ACTIVE", ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-1"}},
	).WithServiceEnv("db-1", []platform.ServiceEnvVar{
		{Key: "hostname", Content: "db"}, {Key: "port", Content: "5432"}, {Key: "user", Content: "u"}, {Key: "password", Content: "p"}, {Key: "dbName", Content: "d"},
	})
	db := &fakeNodePostgresDB{rowsByNonce: map[string]fakeDBRow{"nonce-1": {id: "1", value: "value-1"}}}
	v := NodePostgresVerifier{Client: client, HTTP: loopbackHTTPClient(server), DB: db, Nonce: fixedNonce()}
	rows := v.Verify(context.Background(), NodePostgresInput{
		ProjectID: "proj-1", Stage: "appstage", Database: "db", Unrelated: "other", BaselineUnrelatedAppVersion: "av-1",
	})
	for _, row := range rows {
		if row.Result != CheckPassed {
			t.Errorf("row %s = %s (%s), want passed with a numeric JSON id", row.ID, row.Result, row.Message)
		}
	}
}

// TestNodePostgresRecordQuery_IDAsText pins that the production SELECT casts
// the id column to text: an app may use SERIAL, uuid or text ids, and the
// verifier compares string forms (live S5 run 4 scanned a uuid id as 16 raw
// bytes and failed a correct app).
func TestNodePostgresRecordQuery_IDAsText(t *testing.T) {
	t.Parallel()
	if nodePostgresRecordQuery != "SELECT id::text, value FROM records WHERE nonce = $1" {
		t.Fatalf("query = %q", nodePostgresRecordQuery)
	}
}
