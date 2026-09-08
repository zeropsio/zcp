package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
			rec := storedRecord{ID: id, Nonce: nonce, Value: in.Value, Environment: s.environment}
			s.stored[id] = rec
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(rec)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/records/"):
			s.getCalls++
			id := strings.TrimPrefix(r.URL.Path, "/records/")
			rec, ok := s.stored[id]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(rec)
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
// loopbackHTTPClient). hostname is the service (e.g. "appstage").
func mockWithSubdomainFixture(hostname string, extraServices ...platform.ServiceStack) *platform.Mock {
	svc := platform.ServiceStack{
		ID: hostname + "-1", Name: hostname, Status: "ACTIVE",
		SubdomainAccess: true,
		Ports:           []platform.Port{{Port: 80, Scheme: "http"}},
	}
	services := append([]platform.ServiceStack{svc}, extraServices...)
	return platform.NewMock().
		WithServicesDirect(services).
		WithProject(&platform.Project{ID: "proj-1", SubdomainHost: "testproj.example.com"})
}

func fixedNonce(nonce, value string) func() (string, string) {
	return func() (string, string) { return nonce, value }
}

// TestNodePostgresVerifier_KnownGood_AllRowsPassed pins
// docs/spec-testing-architecture.md §10.3: a known-good app + a DB that
// agrees with the verifier's own nonce/value/id, plus an unchanged baseline,
// yields four passed rows with the §10.3 ids.
func TestNodePostgresVerifier_KnownGood_AllRowsPassed(t *testing.T) {
	app := newNodePostgresAppServer("stage")
	server := httptest.NewServer(app.handler())
	defer server.Close()

	client := mockWithSubdomainFixture("appstage",
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
		Nonce:  fixedNonce("nonce-1", "value-1"),
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
