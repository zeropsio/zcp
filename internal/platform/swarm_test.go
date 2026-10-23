// Tests for: platform/swarm.go — the Swarm pool orchestrator client.
package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// acquiredFixture is a Container as the live orchestrator answers an acquire
// (field names from the orchestrator's OpenAPI; the lease is returned only here).
const acquiredFixture = `{"id":"c-abc","name":"node-id-3","number":3,"hostname":"node-id-3.runtime.pool.zerops",` +
	`"status":"ACTIVE","serviceId":"svc-1","stackId":"stk-1","appVersionId":"av-1","current":true,` +
	`"lockState":"reserved","lockSince":"2026-10-23T10:00:00Z","dirty":false,` +
	`"lease":"0123456789abcdef0123456789abcdef"}`

func TestSwarmClient_Acquire_SendsCleanStartNoTTL(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath, gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = w.Write([]byte(acquiredFixture))
	}))
	defer srv.Close()

	c := NewSwarmClient(srv.URL, "api-token-xyz", srv.Client())
	got, err := c.Acquire(context.Background(), SwarmAcquireRequest{Start: true, Clean: true, LeaseTTLSeconds: 0})
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	if gotMethod != http.MethodPost || gotPath != "/container/acquire" {
		t.Errorf("request = %s %s, want POST /container/acquire", gotMethod, gotPath)
	}
	if gotAuth != "Bearer api-token-xyz" {
		t.Errorf("Authorization = %q, want the bearer API token", gotAuth)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(gotBody), &body); err != nil {
		t.Fatalf("body %q is not JSON: %v", gotBody, err)
	}
	want := map[string]any{"start": true, "clean": true, "leaseTtlSeconds": float64(0)}
	if len(body) != len(want) {
		t.Errorf("body = %v, want exactly %v", body, want)
	}
	for k, v := range want {
		if body[k] != v {
			t.Errorf("body[%q] = %v, want %v", k, body[k], v)
		}
	}
	if got.Container.Number != 3 || got.Container.ID != "c-abc" || got.Container.Hostname != "node-id-3.runtime.pool.zerops" {
		t.Errorf("container = %+v, want id c-abc, number 3, hostname node-id-3.runtime.pool.zerops", got.Container)
	}
	if got.Container.LockState != "reserved" {
		t.Errorf("LockState = %q, want reserved", got.Container.LockState)
	}
	if got.Lease.Secret() != "0123456789abcdef0123456789abcdef" {
		t.Errorf("lease secret not carried to the caller")
	}
}

func TestSwarmClient_ErrorEnvelope_TypedReasonRetryable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		status        int
		body          string
		wantCode      string
		wantReason    string
		wantRetryable bool
		wantMessage   string
	}{
		{
			name:   "unknown lease is not retryable",
			status: http.StatusConflict,
			body: `{"error":{"code":"containerAction","message":"the lease does not hold this container","meta":[` +
				`{"error":"the lease does not hold this container","code":"containerAction","metadata":{"reason":["unknown-lease"],"retryable":["false"]}}]}}`,
			wantCode: "containerAction", wantReason: "unknown-lease", wantRetryable: false,
			wantMessage: "the lease does not hold this container",
		},
		{
			name:   "all reserved is retryable",
			status: http.StatusConflict,
			body: `{"error":{"code":"containerAction","message":"every container of the pool is taken","meta":[` +
				`{"error":"x","code":"containerAction","metadata":{"reason":["all-reserved"],"retryable":["true"]}}]}}`,
			wantCode: "containerAction", wantReason: "all-reserved", wantRetryable: true,
			wantMessage: "every container of the pool is taken",
		},
		{
			name:     "missing token carries no meta",
			status:   http.StatusUnauthorized,
			body:     `{"error":{"code":"notAuthorized","message":"missing or wrong API token","meta":null}}`,
			wantCode: "notAuthorized", wantReason: "", wantRetryable: false,
			wantMessage: "missing or wrong API token",
		},
		{
			name:   "error passed on from the platform has neither reason nor retryable",
			status: http.StatusBadRequest,
			body: `{"error":{"code":"containerAction","message":"another container removal for this service is still in progress - retry in a moment","meta":[` +
				`{"error":"another container removal for this service is still in progress - retry in a moment","code":"serviceStackActionInProgress"}]}}`,
			wantCode: "containerAction", wantReason: "", wantRetryable: false,
			wantMessage: "another container removal for this service is still in progress - retry in a moment",
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

			c := NewSwarmClient(srv.URL, "orch-api-token-0001", srv.Client())
			_, err := c.Acquire(context.Background(), SwarmAcquireRequest{Start: true, Clean: true})
			var se *SwarmError
			if !errors.As(err, &se) {
				t.Fatalf("err = %v (%T), want a *SwarmError", err, err)
			}
			if se.Status != tt.status {
				t.Errorf("Status = %d, want %d", se.Status, tt.status)
			}
			if se.Code != tt.wantCode {
				t.Errorf("Code = %q, want %q", se.Code, tt.wantCode)
			}
			if se.Reason != tt.wantReason {
				t.Errorf("Reason = %q, want %q", se.Reason, tt.wantReason)
			}
			if se.Retryable != tt.wantRetryable {
				t.Errorf("Retryable = %v, want %v", se.Retryable, tt.wantRetryable)
			}
			if se.Message != tt.wantMessage {
				t.Errorf("Message = %q, want %q", se.Message, tt.wantMessage)
			}
		})
	}
}

func TestSwarmClient_LeaseNeverInErrorText(t *testing.T) {
	t.Parallel()

	const lease = "feedfacefeedfacefeedfacefeedface"
	const token = "api-token-secret-123"

	t.Run("orchestrator echoes the lease and token in its message", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":{"code":"containerAction","message":"lease ` + lease + ` (token ` + token + `) does not hold c-abc","meta":[` +
				`{"error":"lease ` + lease + `","code":"containerAction","metadata":{"reason":["unknown-lease"],"retryable":["false"]}}]}}`))
		}))
		defer srv.Close()

		c := NewSwarmClient(srv.URL, token, srv.Client())
		_, err := c.Renew(context.Background(), "c-abc", lease)
		if err == nil {
			t.Fatal("want an error")
		}
		assertNoSecret(t, err, lease, token)
		var se *SwarmError
		if !errors.As(err, &se) {
			t.Fatalf("err = %T, want a *SwarmError", err)
		}
		assertNoSecret(t, errors.New(se.Message), lease, token)
	})

	t.Run("transport failure", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		url := srv.URL
		srv.Close() // nothing answers

		c := NewSwarmClient(url, token, http.DefaultClient)
		for name, call := range map[string]func() error{
			"release": func() error { _, e := c.Release(context.Background(), "c-abc", lease, true); return e },
			"renew":   func() error { _, e := c.Renew(context.Background(), "c-abc", lease); return e },
			"delete":  func() error { return c.Delete(context.Background(), "c-abc", lease) },
		} {
			err := call()
			if err == nil {
				t.Fatalf("%s: want a transport error", name)
			}
			assertNoSecret(t, err, lease, token)
			var pe *PlatformError
			if !errors.As(err, &pe) || pe.Code != ErrNetworkError {
				t.Errorf("%s: err = %v, want a *PlatformError with code %s", name, err, ErrNetworkError)
			}
		}
	})

	t.Run("a SwarmLease prints as a placeholder", func(t *testing.T) {
		t.Parallel()
		acq := SwarmAcquired{Lease: SwarmLease(lease)}
		b, err := json.Marshal(acq)
		if err != nil {
			t.Fatal(err)
		}
		for _, text := range []string{string(b), fmtAll(acq), fmtAll(acq.Lease)} {
			if strings.Contains(text, lease) {
				t.Errorf("formatted lease leaks the secret: %s", text)
			}
		}
	})
}

func assertNoSecret(t *testing.T, err error, secrets ...string) {
	t.Helper()
	for _, s := range secrets {
		if strings.Contains(err.Error(), s) {
			t.Errorf("error text carries a secret %q...: %v", s[:4], err)
		}
	}
}

func fmtAll(v any) string {
	return fmt.Sprintf("%v|%+v|%#v|%s", v, v, v, v)
}

func TestSwarmClient_LeaseOnlyInHeader(t *testing.T) {
	t.Parallel()

	const lease = "0a1b2c3d4e5f60718293a4b5c6d7e8f9"
	type seen struct {
		method, uri, header string
		body                []byte
	}
	var got []seen
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, seen{r.Method, r.URL.RequestURI(), r.Header.Get("X-Swarm-Lease"), b})
		mu.Unlock()
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		_, _ = w.Write([]byte(`{"id":"c-abc","number":3,"lockState":""}`))
	}))
	defer srv.Close()

	c := NewSwarmClient(srv.URL, "orch-api-token-0001", srv.Client())
	ctx := context.Background()
	if _, err := c.Release(ctx, "c-abc", lease, true); err != nil {
		t.Fatalf("Release(reset): %v", err)
	}
	if _, err := c.Release(ctx, "c-abc", lease, false); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, err := c.Renew(ctx, "c-abc", lease); err != nil {
		t.Fatalf("Renew: %v", err)
	}
	if err := c.Delete(ctx, "c-abc", lease); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	want := []struct{ method, uri string }{
		{http.MethodPost, "/container/c-abc/release?reset=true"},
		{http.MethodPost, "/container/c-abc/release"},
		{http.MethodPost, "/container/c-abc/renew"},
		{http.MethodDelete, "/container/c-abc"},
	}
	if len(got) != len(want) {
		t.Fatalf("server saw %d requests, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].method != w.method || got[i].uri != w.uri {
			t.Errorf("request %d = %s %s, want %s %s", i, got[i].method, got[i].uri, w.method, w.uri)
		}
		if strings.Contains(got[i].uri, lease) || strings.Contains(got[i].uri, "lease=") {
			t.Errorf("request %d URL carries the lease: %s", i, got[i].uri)
		}
		if got[i].header != lease {
			t.Errorf("request %d X-Swarm-Lease = %q, want the lease", i, got[i].header)
		}
		if strings.Contains(string(got[i].body), lease) {
			t.Errorf("request %d body carries the lease", i)
		}
	}
}

func TestSwarmClient_PoolAndContainers_DecodeLiveShapes(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/pool":
			_, _ = w.Write([]byte(`{"minimumContainerCount":0,"maximumContainerCount":4}`))
		case "/container":
			_, _ = w.Write([]byte(`[` + acquiredFixture + `,{"id":"c-def","number":4,"hostname":"node-id-4.runtime.pool.zerops","lockState":"","dirty":true}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewSwarmClient(srv.URL+"/", "orch-api-token-0001", srv.Client()) // trailing slash tolerated
	pool, err := c.Pool(context.Background())
	if err != nil {
		t.Fatalf("Pool: %v", err)
	}
	if pool.MinimumContainerCount != 0 || pool.MaximumContainerCount != 4 {
		t.Errorf("pool = %+v, want min 0 max 4", pool)
	}
	list, err := c.Containers(context.Background())
	if err != nil {
		t.Fatalf("Containers: %v", err)
	}
	if len(list) != 2 || list[0].LockState != "reserved" || list[1].Number != 4 || !list[1].Dirty || list[1].LockState != "" {
		t.Errorf("containers = %+v, want a reserved #3 and a free dirty #4", list)
	}
}
