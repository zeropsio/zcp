// Tests for: platform/zbox.go — the Zerops Box client: the zproxy ui-access
// mint (hand-rolled, not in the SDK) and the workspace API reached through it.
package platform

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// boxTestToken is a sentinel zproxy access token: no request URL, body, error
// text, formatted value or marshaled value may carry it.
const boxTestToken = "zbxACCESSsentinel0123456789"

// boxTestURLUI is how the live ui-access answer carries the token in urlUi's
// path (live, 2026-10-24): it is never kept.
const boxTestURLUI = "https://proxy.app-prg1.zerops.io/service-ui/" + boxTestToken + "/login"

func TestMintBoxAccess_PostsUIAccessAndDropsURLUI(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accessToken":"` + boxTestToken + `","expiration":"2026-10-24T17:13:00Z",` +
			`"urlApi":"https://proxy.app-prg1.zerops.io/service-api/","urlUi":"` + boxTestURLUI + `"}`))
	}))
	defer srv.Close()

	client, err := NewZeropsClient("zerops-token-1", srv.URL)
	if err != nil {
		t.Fatalf("NewZeropsClient: %v", err)
	}
	access, err := client.MintBoxAccess(context.Background(), "svc-box")
	if err != nil {
		t.Fatalf("MintBoxAccess: %v", err)
	}

	if gotMethod != http.MethodPost || gotPath != "/api/rest/public/service-stack/svc-box/ui-access" {
		t.Errorf("request = %s %s, want POST /api/rest/public/service-stack/svc-box/ui-access", gotMethod, gotPath)
	}
	if gotAuth != "Bearer zerops-token-1" {
		t.Errorf("Authorization = %q, want zcp's own Zerops token", gotAuth)
	}
	if access.Token.Secret() != boxTestToken {
		t.Error("the access token is not carried to the caller")
	}
	if access.URLAPI != "https://proxy.app-prg1.zerops.io/service-api" {
		t.Errorf("URLAPI = %q, want the urlApi without its trailing slash", access.URLAPI)
	}
	if access.Expiration != "2026-10-24T17:13:00Z" {
		t.Errorf("Expiration = %q", access.Expiration)
	}
	b, err := json.Marshal(access)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{string(b), fmtAll(access), fmtAll(access.Token)} {
		if strings.Contains(text, boxTestToken) || strings.Contains(text, "service-ui") {
			t.Errorf("a formatted access leaks the token or urlUi: %s", text)
		}
	}
}

func TestMintBoxAccess_Refusals_KeepTheAPICode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		status      int
		body        string
		wantCode    string
		wantAPICode string
	}{
		{
			name:   "a Box whose container is not active yet",
			status: http.StatusBadRequest, body: `{"error":{"code":"serviceStackNoActiveContainer","message":"Service stack has no active container."}}`,
			wantCode: ErrAPIError, wantAPICode: "serviceStackNoActiveContainer",
		},
		{
			name:   "a read-only role",
			status: http.StatusForbidden, body: `{"error":{"code":"insufficientPermissions","message":"Insufficient permissions."}}`,
			wantCode: ErrPermissionDenied, wantAPICode: "insufficientPermissions",
		},
		{
			name:   "a Box that is gone",
			status: http.StatusBadRequest, body: `{"error":{"code":"serviceStackNotFound","message":"Service stack not found."}}`,
			wantCode: ErrAPIError, wantAPICode: "serviceStackNotFound",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			client, err := NewZeropsClient("zerops-token-1", srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.MintBoxAccess(context.Background(), "svc-box")
			var pe *PlatformError
			if !errors.As(err, &pe) {
				t.Fatalf("err = %v (%T), want a *PlatformError", err, err)
			}
			if pe.Code != tt.wantCode || pe.APICode != tt.wantAPICode {
				t.Errorf("code = %s / apiCode = %s, want %s / %s", pe.Code, pe.APICode, tt.wantCode, tt.wantAPICode)
			}
		})
	}
}

func TestMintBoxAccess_UnusableAnswer_NeverEchoesIt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{"not JSON, the token inside", `<html>` + boxTestToken + `</html>`},
		{"no urlApi, only the urlUi that carries the token", `{"accessToken":"` + boxTestToken + `","urlUi":"` + boxTestURLUI + `"}`},
		{"no access token", `{"urlApi":"https://proxy.app-prg1.zerops.io/service-api"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			client, err := NewZeropsClient("zerops-token-1", srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.MintBoxAccess(context.Background(), "svc-box")
			if err == nil {
				t.Fatal("want an error for an unusable answer")
			}
			assertNoSecret(t, err, boxTestToken, "service-ui")
		})
	}
}

// boxSeen is one request the fake Box API received.
type boxSeen struct {
	method, uri, auth, idempotencyKey string
	body                              []byte
}

// boxRecorder is a fake Box API behind zproxy: every route answers its live
// shape (2026-10-24) and every request is recorded.
type boxRecorder struct {
	mu   sync.Mutex
	seen []boxSeen
}

func (b *boxRecorder) handler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		b.mu.Lock()
		b.seen = append(b.seen, boxSeen{r.Method, r.URL.RequestURI(), r.Header.Get("Authorization"), r.Header.Get("Idempotency-Key"), body})
		b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/service-api/api/rest/ping":
			_, _ = w.Write([]byte(`{"time":"2026-10-24T16:12:44Z","version":"zerops-v3.1.0-193-gb65379eeb021"}`))
		case r.URL.Path == "/service-api/api/rest/workspace":
			_, _ = w.Write([]byte(boxWorkspaceFixture))
		case r.URL.Path == "/service-api/api/rest/base-list":
			_, _ = w.Write([]byte(`{"items":[{"created":"2026-10-24T16:13:22Z","digest":"sha256:da2f","exposePorts":[],"failedReason":null,` +
				`"imageRef":"https://storage.cdn.zerops.app/4ga3k-boxstorage/zbox/zbox-base.tar.gz","labels":{},"launchContract":{},` +
				`"name":"default","sizeBytes":519391774,"status":"READY"}]}`))
		case r.URL.Path == "/service-api/api/rest/operation-list":
			_, _ = w.Write([]byte(`{"items":[{"id":"op-1","kind":"PULL_BASE","status":"RUNNING","created":"2026-10-24T16:13:22Z"}]}`))
		case r.URL.Path == "/service-api/api/rest/base" && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"id":"op-1","kind":"PULL_BASE","status":"RUNNING","created":"2026-10-24T16:13:22Z","error":null,"result":null}`))
		case strings.HasPrefix(r.URL.Path, "/service-api/api/rest/operation/"):
			_, _ = w.Write([]byte(`{"id":"op-1","kind":"PULL_BASE","status":"FINISHED","created":"2026-10-24T16:13:22Z",` +
				`"finished":"2026-10-24T16:13:45Z","error":null,"result":{"digest":"sha256:da2f","name":"default"}}`))
		case r.URL.Path == "/service-api/api/rest/workspace/domains" && r.Method == http.MethodPut:
			_, _ = w.Write([]byte(boxWorkspaceFixture))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"notFound","message":"no route"}}`))
		}
	})
}

// boxWorkspaceFixture is GET /workspace as the live orchestrator answers it
// once its first domain is set.
const boxWorkspaceFixture = `{"alwaysDenied":[],"capacity":{"cpus":2},"domains":[{"domain":"lvzbox-3860-8081.prg1.zerops.app","form":"DASH"}],` +
	`"host":{},"imageRelease":"runtime_ubuntu_26_04_vm_zbox_1_1","policy":{},"substrate":"FIRECRACKER","usage":{},"version":"zerops-v3.1.0"}`

// staticMint hands out one access to the fake at base.
func staticMint(base string, mints *int, mu *sync.Mutex) BoxMinter {
	return func(context.Context) (BoxAccess, error) {
		mu.Lock()
		*mints++
		mu.Unlock()
		return BoxAccess{Token: boxTestToken, URLAPI: base + "/service-api"}, nil
	}
}

func TestBoxClient_BearerOnlyInHeader(t *testing.T) {
	t.Parallel()

	rec := &boxRecorder{}
	srv := httptest.NewServer(rec.handler(t))
	defer srv.Close()
	var mints int
	var mu sync.Mutex
	c := NewBoxClient(staticMint(srv.URL, &mints, &mu), srv.Client())
	ctx := context.Background()

	if _, err := c.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if _, err := c.Workspace(ctx); err != nil {
		t.Fatalf("Workspace: %v", err)
	}
	if _, err := c.Bases(ctx); err != nil {
		t.Fatalf("Bases: %v", err)
	}
	if _, err := c.Operations(ctx, BoxOperationKindPullBase); err != nil {
		t.Fatalf("Operations: %v", err)
	}
	if _, err := c.PullBase(ctx, BoxPullBaseRequest{ImageRef: "https://storage.cdn.zerops.app/4ga3k-boxstorage/zbox/zbox-base.tar.gz", Name: "default"}, "zcp-key-1"); err != nil {
		t.Fatalf("PullBase: %v", err)
	}
	if _, err := c.Operation(ctx, "op-1", 30*time.Second); err != nil {
		t.Fatalf("Operation: %v", err)
	}
	if _, err := c.PutDomains(ctx, []BoxDomain{{Domain: "lvzbox-3860-8081.prg1.zerops.app", Form: BoxDomainFormDash}}); err != nil {
		t.Fatalf("PutDomains: %v", err)
	}

	want := []struct{ method, uri, key, body string }{
		{http.MethodGet, "/service-api/api/rest/ping", "", ""},
		{http.MethodGet, "/service-api/api/rest/workspace", "", ""},
		{http.MethodGet, "/service-api/api/rest/base-list", "", ""},
		{http.MethodGet, "/service-api/api/rest/operation-list?kind=PULL_BASE&limit=20", "", ""},
		{http.MethodPost, "/service-api/api/rest/base", "zcp-key-1", `{"imageRef":"https://storage.cdn.zerops.app/4ga3k-boxstorage/zbox/zbox-base.tar.gz","name":"default"}`},
		{http.MethodGet, "/service-api/api/rest/operation/op-1?waitSeconds=30", "", ""},
		{http.MethodPut, "/service-api/api/rest/workspace/domains", "", `{"domains":[{"domain":"lvzbox-3860-8081.prg1.zerops.app","form":"DASH"}]}`},
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.seen) != len(want) {
		t.Fatalf("the Box API saw %d requests, want %d: %+v", len(rec.seen), len(want), rec.seen)
	}
	for i, w := range want {
		got := rec.seen[i]
		if got.method != w.method || got.uri != w.uri {
			t.Errorf("request %d = %s %s, want %s %s", i, got.method, got.uri, w.method, w.uri)
		}
		if got.auth != "Bearer "+boxTestToken {
			t.Errorf("request %d Authorization = %q, want the access token as the bearer", i, got.auth)
		}
		if got.idempotencyKey != w.key {
			t.Errorf("request %d Idempotency-Key = %q, want %q", i, got.idempotencyKey, w.key)
		}
		if w.body != "" && strings.TrimSpace(string(got.body)) != w.body {
			t.Errorf("request %d body = %s, want %s", i, got.body, w.body)
		}
		if strings.Contains(got.uri, boxTestToken) || strings.Contains(string(got.body), boxTestToken) {
			t.Errorf("request %d carries the token outside its Authorization header", i)
		}
	}
	if mints != 1 {
		t.Errorf("minted %d accesses, want 1 (the access is kept for the client's calls)", mints)
	}
}

func TestBoxClient_DecodesLiveShapes(t *testing.T) {
	t.Parallel()

	rec := &boxRecorder{}
	srv := httptest.NewServer(rec.handler(t))
	defer srv.Close()
	var mints int
	var mu sync.Mutex
	c := NewBoxClient(staticMint(srv.URL, &mints, &mu), srv.Client())
	ctx := context.Background()

	ping, err := c.Ping(ctx)
	if err != nil || ping.Version != "zerops-v3.1.0-193-gb65379eeb021" {
		t.Errorf("Ping = %+v, %v", ping, err)
	}
	ws, err := c.Workspace(ctx)
	if err != nil || len(ws.Domains) != 1 || ws.Domains[0] != (BoxDomain{Domain: "lvzbox-3860-8081.prg1.zerops.app", Form: BoxDomainFormDash}) {
		t.Errorf("Workspace = %+v, %v", ws, err)
	}
	bases, err := c.Bases(ctx)
	if err != nil || len(bases) != 1 {
		t.Fatalf("Bases = %+v, %v", bases, err)
	}
	if b := bases[0]; b.Name != "default" || b.Status != BoxBaseStatusReady || b.Digest != "sha256:da2f" || b.SizeBytes != 519391774 {
		t.Errorf("base = %+v, want default READY sha256:da2f of 519391774 bytes", b)
	}
	opsList, err := c.Operations(ctx, BoxOperationKindPullBase)
	if err != nil || len(opsList) != 1 || opsList[0].ID != "op-1" || opsList[0].Status != BoxOperationRunning {
		t.Errorf("Operations = %+v, %v", opsList, err)
	}
	op, err := c.Operation(ctx, "op-1", 30*time.Second)
	if err != nil || op.Status != BoxOperationFinished || op.BaseDigest() != "sha256:da2f" {
		t.Errorf("Operation = %+v (digest %q), %v", op, op.BaseDigest(), err)
	}
}

func TestBoxClient_SecretNeverInErrorText(t *testing.T) {
	t.Parallel()

	t.Run("the API echoes the bearer in its refusal", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":{"code":"workspaceFull","message":"no room for ` + r.Header.Get("Authorization") + `",` +
				`"meta":[{"code":"workspaceFull","error":"x","metadata":{"resource":["memory"],"requested":["536870912"],"free":["0"],"echo":["` + boxTestToken + `"]}}]}}`))
		}))
		defer srv.Close()
		var mints int
		var mu sync.Mutex
		c := NewBoxClient(staticMint(srv.URL, &mints, &mu), srv.Client())
		_, err := c.PullBase(context.Background(), BoxPullBaseRequest{ImageRef: "x", Name: "default"}, "k-1")
		if err == nil {
			t.Fatal("want a refusal")
		}
		assertNoSecret(t, err, boxTestToken)
		var be *BoxError
		if !errors.As(err, &be) {
			t.Fatalf("err = %T, want a *BoxError", err)
		}
		if be.Status != http.StatusConflict || be.Code != BoxErrorWorkspaceFull || be.Metadata["resource"] != "memory" {
			t.Errorf("BoxError = %+v, want 409 workspaceFull for memory", be)
		}
		for k, v := range be.Metadata {
			if strings.Contains(v, boxTestToken) {
				t.Errorf("metadata %s carries the token", k)
			}
		}
		assertNoSecret(t, errors.New(be.Message), boxTestToken)
	})

	t.Run("nothing answers", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		base := srv.URL
		srv.Close()
		var mints int
		var mu sync.Mutex
		c := NewBoxClient(staticMint(base, &mints, &mu), http.DefaultClient)
		for name, call := range map[string]func() error{
			"ping": func() error { _, e := c.Ping(context.Background()); return e },
			"pull": func() error {
				_, e := c.PullBase(context.Background(), BoxPullBaseRequest{ImageRef: "x"}, "k-1")
				return e
			},
			"operation": func() error { _, e := c.Operation(context.Background(), "op-1", 0); return e },
		} {
			err := call()
			if err == nil {
				t.Fatalf("%s: want a transport error", name)
			}
			assertNoSecret(t, err, boxTestToken)
			var pe *PlatformError
			if !errors.As(err, &pe) || pe.Code != ErrNetworkError {
				t.Errorf("%s: err = %v, want a *PlatformError with code %s", name, err, ErrNetworkError)
			}
		}
	})

	t.Run("a body that is not the envelope", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`<html>bad gateway ` + boxTestToken + `</html>`))
		}))
		defer srv.Close()
		var mints int
		var mu sync.Mutex
		c := NewBoxClient(staticMint(srv.URL, &mints, &mu), srv.Client())
		_, err := c.Ping(context.Background())
		var be *BoxError
		if !errors.As(err, &be) || be.Status != http.StatusBadGateway {
			t.Fatalf("err = %v, want a *BoxError with status 502", err)
		}
		assertNoSecret(t, err, boxTestToken)
	})

	t.Run("the mint fails", func(t *testing.T) {
		t.Parallel()
		mintErr := &PlatformError{Code: ErrAPIError, APICode: "serviceStackNoActiveContainer", Message: "Service stack has no active container."}
		c := NewBoxClient(func(context.Context) (BoxAccess, error) { return BoxAccess{}, mintErr }, http.DefaultClient)
		_, err := c.Ping(context.Background())
		var pe *PlatformError
		if !errors.As(err, &pe) || pe.APICode != "serviceStackNoActiveContainer" {
			t.Errorf("err = %v, want the mint's refusal with its API code", err)
		}
	})
}

// TestBoxClient_AnswerCutShort_IsTheNetworks: a 2xx whose body the connection
// cut short is the network failing, not the API refusing: a POST /base whose
// answer was lost this way is sent once more with the same Idempotency-Key
// (PS-13), so the error is transient and still never carries the bearer.
func TestBoxClient_AnswerCutShort_IsTheNetworks(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "200")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"op-1","status":"PEN`))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		panic(http.ErrAbortHandler)
	}))
	defer srv.Close()
	var mints int
	var mu sync.Mutex
	c := NewBoxClient(staticMint(srv.URL, &mints, &mu), srv.Client())
	_, err := c.PullBase(context.Background(), BoxPullBaseRequest{ImageRef: "x", Name: "default"}, "k-1")
	if err == nil {
		t.Fatal("want an error for an answer cut short")
	}
	if !IsTransient(err) {
		t.Errorf("err = %v, want a transient (network) error so the pull is sent again with its key", err)
	}
	assertNoSecret(t, err, boxTestToken)
}

func TestBoxClient_RemintsOnceOn401(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		accepted  string // the token the fake accepts; "" accepts none
		wantErr   bool
		wantMints int
	}{
		{name: "a fresh access is accepted", accepted: "token-2", wantMints: 2},
		{name: "the fresh access is refused too", accepted: "", wantErr: true, wantMints: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.accepted == "" || r.Header.Get("Authorization") != "Bearer "+tt.accepted {
					w.WriteHeader(http.StatusUnauthorized)
					_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"Authorization: Bearer <access token> is required."}}`))
					return
				}
				_, _ = w.Write([]byte(`{"time":"2026-10-24T16:12:44Z","version":"v"}`))
			}))
			defer srv.Close()
			var mu sync.Mutex
			mints := 0
			mint := func(context.Context) (BoxAccess, error) {
				mu.Lock()
				defer mu.Unlock()
				mints++
				return BoxAccess{Token: BoxAccessToken("token-" + string(rune('0'+mints))), URLAPI: srv.URL + "/service-api"}, nil
			}
			c := NewBoxClient(mint, srv.Client())
			_, err := c.Ping(context.Background())
			if tt.wantErr {
				var be *BoxError
				if !errors.As(err, &be) || be.Status != http.StatusUnauthorized {
					t.Errorf("err = %v, want the 401 as a *BoxError", err)
				}
			} else if err != nil {
				t.Errorf("Ping: %v", err)
			}
			if mints != tt.wantMints {
				t.Errorf("minted %d accesses, want %d (one re-mint on a 401, never more)", mints, tt.wantMints)
			}
		})
	}
}

func TestBoxAccessToken_FormatsRedacted(t *testing.T) {
	t.Parallel()
	tok := BoxAccessToken(boxTestToken)
	b, err := json.Marshal(struct {
		T BoxAccessToken `json:"t"`
	}{tok})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{string(b), fmtAll(tok)} {
		if strings.Contains(text, boxTestToken) {
			t.Errorf("a formatted token leaks: %s", text)
		}
	}
	if tok.Secret() != boxTestToken {
		t.Error("Secret() must return the token")
	}
}
