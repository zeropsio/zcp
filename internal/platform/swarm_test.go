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
	"time"
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

// TestSwarmClient_ErrorEnvelope_KeepsThePlatformCause: an orchestrator that
// passes on the platform's own refusal (meta[].metadata.message) keeps it as
// Cause, so a caller tells a service at its maximum from any other refusal
// that leaves the pool without a free container. Bodies are the live ones
// (an 11th acquire on a pool of 10, a POST /container on a full pool).
func TestSwarmClient_ErrorEnvelope_KeepsThePlatformCause(t *testing.T) {
	t.Parallel()

	const fullMessage = "all 10 container(s) of the pool that can serve the request are taken and another could not be created - retry once one is released"
	tests := []struct {
		name       string
		body       string
		wantReason string
		wantCause  string
		wantAtMax  bool
	}{
		{
			name: "acquire on a full pool: the service is at its maximum",
			body: `{"error":{"code":"containerAction","message":"` + fullMessage + `","meta":[{"error":"` + fullMessage + `","code":"containerAction",` +
				`"metadata":{"message":["[containerAction] Service is already at maximum number of containers"],"reason":["all-reserved"],"retryable":["true"]}}]}}`,
			wantReason: "all-reserved",
			wantCause:  "[containerAction] Service is already at maximum number of containers",
			wantAtMax:  true,
		},
		{
			name: "acquire refused for another platform cause",
			body: `{"error":{"code":"containerAction","message":"` + fullMessage + `","meta":[{"error":"x","code":"containerAction",` +
				`"metadata":{"message":["[containerAction] There is not enough free capacity on the hardware"],"reason":["all-reserved"],"retryable":["true"]}}]}}`,
			wantReason: "all-reserved",
			wantCause:  "[containerAction] There is not enough free capacity on the hardware",
			wantAtMax:  false,
		},
		{
			name: "create on a full pool: the orchestrator's own refusal, no platform cause",
			body: `{"error":{"code":"containerAction","message":"the pool holds 10 of at most 10 containers, so 1 more cannot be created - raise the service's maximum first","meta":[` +
				`{"error":"x","code":"containerAction","metadata":{"reason":["pool-capacity-exceeded"],"retryable":["false"]}}]}}`,
			wantReason: "pool-capacity-exceeded",
			wantCause:  "",
			wantAtMax:  false,
		},
		{
			name:       "a cause echoing the bearer is scrubbed",
			body:       `{"error":{"code":"containerAction","message":"refused","meta":[{"metadata":{"message":["bad token orch-api-token-0001"],"reason":["all-reserved"]}}]}}`,
			wantReason: "all-reserved",
			wantCause:  "bad token [redacted]",
			wantAtMax:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			c := NewSwarmClient(srv.URL, "orch-api-token-0001", srv.Client())
			_, err := c.Acquire(context.Background(), SwarmAcquireRequest{Start: true, Clean: true})
			var se *SwarmError
			if !errors.As(err, &se) {
				t.Fatalf("err = %v (%T), want a *SwarmError", err, err)
			}
			if se.Reason != tt.wantReason {
				t.Errorf("Reason = %q, want %q", se.Reason, tt.wantReason)
			}
			if se.Cause != tt.wantCause {
				t.Errorf("Cause = %q, want %q", se.Cause, tt.wantCause)
			}
			if got := se.AtServiceMaximum(); got != tt.wantAtMax {
				t.Errorf("AtServiceMaximum() = %v, want %v", got, tt.wantAtMax)
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

// rolloutFixture is a Rollout as the orchestrator answers POST
// /container/rollout (field names from its OpenAPI, schema Rollout): two
// outdated free containers replaced, one retiring, a lease skipped as
// held-by-other, one removal refused, and one replacement that failed.
const rolloutFixture = `{"activeAppVersionId":"av-new",` +
	`"replaced":["c-1","c-2"],` +
	`"created":[{"id":"c-8","name":"node-id-8","number":8,"hostname":"node-id-8.runtime.pool.zerops","appVersionId":"av-new","current":true,"lockState":"","dirty":false},` +
	`{"id":"c-9","name":"node-id-9","number":9,"hostname":"node-id-9.runtime.pool.zerops","appVersionId":"av-new","current":true,"lockState":"","dirty":false}],` +
	`"retiring":["c-4"],` +
	`"skipped":[{"id":"c-3","reason":"held-by-other"},{"id":"c-5","reason":"removal-failed","message":"the platform refused the removal"}],` +
	`"createErrors":[{"containerName":"node-id-10","message":"no capacity"}]}`

func TestSwarmClient_Rollout_DecodesAndSendsNoForce(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                                    string
		body                                    string
		wantAppVersion                          string
		wantReplaced, wantCreated, wantRetiring int
		wantSkipped                             []SwarmRolloutSkip
		wantCreateErrors                        []SwarmRolloutCreateError
		wantCreatedNumbers                      []int64
	}{
		{
			name: "outdated containers replaced, a lease skipped", body: rolloutFixture,
			wantAppVersion: "av-new", wantReplaced: 2, wantCreated: 2, wantRetiring: 1,
			wantSkipped: []SwarmRolloutSkip{
				{ID: "c-3", Reason: "held-by-other"},
				{ID: "c-5", Reason: "removal-failed", Message: "the platform refused the removal"},
			},
			wantCreateErrors:   []SwarmRolloutCreateError{{ContainerName: "node-id-10", Message: "no capacity"}},
			wantCreatedNumbers: []int64{8, 9},
		},
		{
			// Live 2026-10-23: nothing outdated — every array present and empty.
			name:           "nothing outdated",
			body:           `{"activeAppVersionId":"SC1RLQ4ZTgK8l1iptsFfBg","replaced":[],"created":[],"retiring":[],"skipped":[],"createErrors":[]}`,
			wantAppVersion: "SC1RLQ4ZTgK8l1iptsFfBg",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var gotMethod, gotURI, gotAuth, gotLease, gotBody string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotURI = r.Method, r.URL.RequestURI()
				gotAuth, gotLease = r.Header.Get("Authorization"), r.Header.Get("X-Swarm-Lease")
				b, _ := io.ReadAll(r.Body)
				gotBody = string(b)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			c := NewSwarmClient(srv.URL, "orch-api-token-0001", srv.Client())
			got, err := c.Rollout(context.Background())
			if err != nil {
				t.Fatalf("Rollout: %v", err)
			}

			if gotMethod != http.MethodPost || gotURI != "/container/rollout" {
				t.Errorf("request = %s %s, want POST /container/rollout with no query (never force)", gotMethod, gotURI)
			}
			if strings.Contains(gotURI, "force") || strings.Contains(gotBody, "force") {
				t.Errorf("rollout sent force (uri %q, body %q): force takes leases from their holders", gotURI, gotBody)
			}
			if gotAuth != "Bearer orch-api-token-0001" {
				t.Errorf("Authorization = %q, want the pool's bearer API token", gotAuth)
			}
			if gotLease != "" {
				t.Errorf("X-Swarm-Lease = %q, want none: a rollout names no lease", gotLease)
			}
			if got.ActiveAppVersionID != tt.wantAppVersion {
				t.Errorf("ActiveAppVersionID = %q, want %q", got.ActiveAppVersionID, tt.wantAppVersion)
			}
			if len(got.Replaced) != tt.wantReplaced || len(got.Created) != tt.wantCreated || len(got.Retiring) != tt.wantRetiring {
				t.Errorf("replaced/created/retiring = %d/%d/%d, want %d/%d/%d",
					len(got.Replaced), len(got.Created), len(got.Retiring), tt.wantReplaced, tt.wantCreated, tt.wantRetiring)
			}
			for i, n := range tt.wantCreatedNumbers {
				if got.Created[i].Number != n || !got.Created[i].Current {
					t.Errorf("created[%d] = %+v, want current container #%d", i, got.Created[i], n)
				}
			}
			if len(got.Skipped) != len(tt.wantSkipped) {
				t.Fatalf("skipped = %+v, want %+v", got.Skipped, tt.wantSkipped)
			}
			for i, w := range tt.wantSkipped {
				if got.Skipped[i] != w {
					t.Errorf("skipped[%d] = %+v, want %+v", i, got.Skipped[i], w)
				}
			}
			if len(got.CreateErrors) != len(tt.wantCreateErrors) {
				t.Fatalf("createErrors = %+v, want %+v", got.CreateErrors, tt.wantCreateErrors)
			}
			for i, w := range tt.wantCreateErrors {
				if got.CreateErrors[i] != w {
					t.Errorf("createErrors[%d] = %+v, want %+v", i, got.CreateErrors[i], w)
				}
			}
		})
	}
}

func TestSwarmClient_Rollout_PoolFixedSize(t *testing.T) {
	t.Parallel()

	// The message is text for a person and never names the reason: only
	// meta[].metadata.reason says pool-fixed-size.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"containerAction","message":"nothing can be replaced in this pool","meta":[` +
			`{"error":"nothing can be replaced in this pool","code":"containerAction","metadata":{"reason":["pool-fixed-size"],"retryable":["false"]}}]}}`))
	}))
	defer srv.Close()

	c := NewSwarmClient(srv.URL, "orch-api-token-0001", srv.Client())
	_, err := c.Rollout(context.Background())
	var se *SwarmError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v (%T), want a *SwarmError", err, err)
	}
	if se.Reason != SwarmReasonPoolFixedSize {
		t.Errorf("Reason = %q, want %q", se.Reason, SwarmReasonPoolFixedSize)
	}
	if se.Retryable {
		t.Error("Retryable = true: a fixed-size pool needs its limits changed first")
	}
	if !strings.Contains(err.Error(), "swarm rollout") {
		t.Errorf("error %q does not name the rollout", err)
	}
}

// TestSwarmClient_Rollout_OutlivesClientTimeout: a rollout returns only once
// its removals and creates are done (minutes for a VM pool), so the short
// budget of the client's other calls must not cut it.
func TestSwarmClient_Rollout_OutlivesClientTimeout(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte(`{"activeAppVersionId":"av","replaced":["c-1"],"created":[],"retiring":[],"skipped":[],"createErrors":[]}`))
	}))
	defer srv.Close()

	hc := srv.Client()
	hc.Timeout = 50 * time.Millisecond
	c := NewSwarmClient(srv.URL, "orch-api-token-0001", hc)
	got, err := c.Rollout(context.Background())
	if err != nil {
		t.Fatalf("Rollout cut by the client's %s budget: %v", hc.Timeout, err)
	}
	if len(got.Replaced) != 1 {
		t.Errorf("replaced = %v, want [c-1]", got.Replaced)
	}
	if _, err := c.Pool(context.Background()); err == nil {
		t.Error("Pool outlived the client's budget too: only the rollout gets the long one")
	}
}

// TestSwarmClient_Create_SendsCountAndBearerOnly: a create (POST /container)
// carries the count and the bearer, never a lease or a query, and decodes the
// containers it answers with — their snapshots reported null, as the live
// create does (only a read by id counts them).
func TestSwarmClient_Create_SendsCountAndBearerOnly(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath, gotQuery, gotAuth, gotLease, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.RawQuery
		gotAuth, gotLease = r.Header.Get("Authorization"), r.Header.Get("X-Swarm-Lease")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = w.Write([]byte(`[{"id":"c-new","number":5,"hostname":"node-id-5.runtime.pool.zerops","status":"ACTIVE","lockState":"","dirty":false,"current":true,"snapshots":null,"appVersionId":"av-1"}]`))
	}))
	defer srv.Close()

	c := NewSwarmClient(srv.URL, "api-token-xyz", srv.Client())
	got, err := c.Create(context.Background(), 1)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/container" || gotQuery != "" {
		t.Errorf("request = %s %s?%s, want POST /container with no query", gotMethod, gotPath, gotQuery)
	}
	if gotAuth != "Bearer api-token-xyz" || gotLease != "" {
		t.Errorf("Authorization = %q, X-Swarm-Lease = %q: want the bearer only", gotAuth, gotLease)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(gotBody), &body); err != nil {
		t.Fatalf("body %q is not JSON: %v", gotBody, err)
	}
	if len(body) != 1 || body["count"] != float64(1) {
		t.Errorf("body = %v, want exactly {count: 1}", body)
	}
	if len(got) != 1 || got[0].ID != "c-new" || got[0].Number != 5 || got[0].LockState != "" || !got[0].Current || got[0].Snapshots != 0 {
		t.Errorf("created = %+v, want the free current c-new #5 with no snapshot count", got)
	}
}

// TestSwarmClient_Container_ReadsSnapshotsByID: the read by id (GET
// /container/{id}, the id path-escaped) is the one that counts a container's
// snapshots; it carries the bearer and no lease.
func TestSwarmClient_Container_ReadsSnapshotsByID(t *testing.T) {
	t.Parallel()

	var gotPath, gotLease string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotLease = r.URL.EscapedPath(), r.Header.Get("X-Swarm-Lease")
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer api-token-xyz" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"id":"c/1","number":5,"status":"ACTIVE","lockState":"","lockSince":null,"dirty":false,"current":true,"snapshots":1}`))
	}))
	defer srv.Close()

	c := NewSwarmClient(srv.URL, "api-token-xyz", srv.Client())
	got, err := c.Container(context.Background(), "c/1")
	if err != nil {
		t.Fatalf("Container: %v", err)
	}
	if gotPath != "/container/c%2F1" || gotLease != "" {
		t.Errorf("path = %q, lease = %q: want /container/c%%2F1 and no lease", gotPath, gotLease)
	}
	if got.ID != "c/1" || got.Snapshots != 1 || !got.Current || got.LockState != "" {
		t.Errorf("container = %+v, want the free current c/1 with 1 snapshot", got)
	}
}

// TestSwarmClient_StopStart_LeaseOnlyInHeader: a holder's stop and start
// (POST /container/{id}/stop, /start, the id path-escaped) carry the bearer
// and the lease in X-Swarm-Lease only — never in the URL, never with force —
// and decode the container as the orchestrator answers it: stopped and still
// reserved, then running again.
func TestSwarmClient_StopStart_LeaseOnlyInHeader(t *testing.T) {
	t.Parallel()

	const lease = "0a1b2c3d4e5f60718293a4b5c6d7e8f9"
	type seen struct{ method, uri, auth, lease, body string }
	var got []seen
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, seen{r.Method, r.URL.RequestURI(), r.Header.Get("Authorization"), r.Header.Get("X-Swarm-Lease"), string(b)})
		mu.Unlock()
		status := SwarmContainerActive
		if strings.HasSuffix(r.URL.Path, "/stop") {
			status = SwarmContainerStopped
		}
		_, _ = w.Write([]byte(`{"id":"c/1","number":3,"hostname":"node-id-3.runtime.pool.zerops","status":"` + status +
			`","lockState":"reserved","lockSince":"2026-10-23T22:00:00Z","dirty":false,"current":true}`))
	}))
	defer srv.Close()

	c := NewSwarmClient(srv.URL, "api-token-xyz", srv.Client())
	stopped, err := c.Stop(context.Background(), "c/1", lease)
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if stopped.Status != SwarmContainerStopped || stopped.LockState != "reserved" || stopped.Number != 3 {
		t.Errorf("stopped = %+v, want #3 STOPPED and still reserved", stopped)
	}
	started, err := c.Start(context.Background(), "c/1", lease)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if started.Status != SwarmContainerActive || started.LockState != "reserved" {
		t.Errorf("started = %+v, want ACTIVE and still reserved", started)
	}

	want := []string{"/container/c%2F1/stop", "/container/c%2F1/start"}
	if len(got) != len(want) {
		t.Fatalf("server saw %d requests, want %d", len(got), len(want))
	}
	for i, uri := range want {
		g := got[i]
		if g.method != http.MethodPost || g.uri != uri {
			t.Errorf("request %d = %s %s, want POST %s with no query (no force, no lease)", i, g.method, g.uri, uri)
		}
		if g.auth != "Bearer api-token-xyz" || g.lease != lease {
			t.Errorf("request %d: Authorization %q, X-Swarm-Lease %q: want the bearer and the lease", i, g.auth, g.lease)
		}
		if strings.Contains(g.body, lease) {
			t.Errorf("request %d body carries the lease", i)
		}
	}
}

// TestSwarmClient_StopStart_RefusalNeverCarriesTheLease: a refused stop or
// start keeps the orchestrator's reason and scrubs the lease and the token it
// may echo, and a transport failure is a NETWORK_ERROR naming neither.
func TestSwarmClient_StopStart_RefusalNeverCarriesTheLease(t *testing.T) {
	t.Parallel()

	const lease = "feedfacefeedfacefeedfacefeedface"
	const token = "api-token-secret-123"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"containerAction","message":"lease ` + lease + ` (token ` + token + `): container busy","meta":[` +
			`{"metadata":{"reason":["container-busy"],"retryable":["true"]}}]}}`))
	}))
	defer srv.Close()
	down := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	downURL := down.URL
	down.Close()

	refusing := NewSwarmClient(srv.URL, token, srv.Client())
	unreachable := NewSwarmClient(downURL, token, http.DefaultClient)
	for name, call := range map[string]func(*SwarmClient) error{
		"stop":  func(c *SwarmClient) error { _, e := c.Stop(context.Background(), "c-abc", lease); return e },
		"start": func(c *SwarmClient) error { _, e := c.Start(context.Background(), "c-abc", lease); return e },
	} {
		err := call(refusing)
		var se *SwarmError
		if !errors.As(err, &se) || se.Reason != "container-busy" || !se.Retryable {
			t.Errorf("%s refused: err = %v, want a *SwarmError with reason container-busy, retryable", name, err)
		}
		assertNoSecret(t, err, lease, token)

		err = call(unreachable)
		var pe *PlatformError
		if !errors.As(err, &pe) || pe.Code != ErrNetworkError {
			t.Errorf("%s unreachable: err = %v, want a *PlatformError with code %s", name, err, ErrNetworkError)
		}
		if err != nil {
			assertNoSecret(t, err, lease, token)
		}
	}
}

// TestSwarmClient_HeldByOther_EveryLeaseCallReadsTheLiveRefusal: a call
// carrying a lease that does not hold the reserved container — release,
// delete, stop, start — is refused with the live body (2026-10-24, the
// release of a lease another consumer took over): 400 containerAction,
// reason held-by-other, retryable "true". Every lease call reads it as
// SwarmReasonHeldByOther, which zcp takes for "another holder has it", never
// for "retry in a moment".
func TestSwarmClient_HeldByOther_EveryLeaseCallReadsTheLiveRefusal(t *testing.T) {
	t.Parallel()

	const lease = "feedfacefeedfacefeedfacefeedface"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"containerAction","message":"container is reserved by another consumer - pass its lease, or ?force=true to take it over",` +
			`"meta":[{"error":"container is reserved by another consumer - pass its lease, or ?force=true to take it over","code":"containerAction",` +
			`"metadata":{"reason":["held-by-other"],"retryable":["true"]}}]}}`))
	}))
	defer srv.Close()
	c := NewSwarmClient(srv.URL, "api-token", srv.Client())
	for name, call := range map[string]func() error{
		"release": func() error { _, e := c.Release(context.Background(), "c-abc", lease, true); return e },
		"delete":  func() error { return c.Delete(context.Background(), "c-abc", lease) },
		"stop":    func() error { _, e := c.Stop(context.Background(), "c-abc", lease); return e },
		"start":   func() error { _, e := c.Start(context.Background(), "c-abc", lease); return e },
	} {
		var se *SwarmError
		if err := call(); !errors.As(err, &se) || se.Reason != SwarmReasonHeldByOther || !se.Retryable || se.Status != http.StatusBadRequest || se.Code != "containerAction" {
			t.Errorf("%s: err = %v, want a 400 containerAction *SwarmError, reason %s, retryable", name, err, SwarmReasonHeldByOther)
		}
	}
}

// TestSwarmClient_Renew_ReadsTheLiveAnswers: a renew with the holder's lease,
// the lease only in its header, answers the container as it stands (live
// 2026-10-24: 200 with lockState reserved, or deleting while a delete runs);
// any other lease, or any on a free container, is refused unknown-lease; a
// container gone is refused container-not-found — the orchestrator names
// that reason only once a listing has caught up, and passes the platform's
// bare "Container not found" (no metadata) through before, which reads as
// container-not-found too.
func TestSwarmClient_Renew_ReadsTheLiveAnswers(t *testing.T) {
	t.Parallel()
	const lease = "feedfacefeedfacefeedfacefeedface"
	tests := []struct {
		name       string
		status     int
		body       string
		wantState  string
		wantReason string
		// wantNoReason: refused with no reason the caller acts on.
		wantNoReason bool
	}{
		{name: "the holder's lease", status: 200, body: `{"id":"c-abc","number":1,"status":"STOPPED","lockState":"reserved","lockSince":"2026-10-24T12:42:40.306795242Z","dirty":false}`, wantState: "reserved"},
		{name: "a delete running", status: 200, body: `{"id":"c-abc","number":1,"status":"DELETING","lockState":"deleting","lockSince":"2026-10-24T12:48:45.395604967Z","dirty":false}`, wantState: "deleting"},
		{name: "another lease, or a free container", status: 400, body: `{"error":{"code":"containerAction","message":"no reservation of this container is held by that lease - acquire it again","meta":[{"error":"no reservation of this container is held by that lease - acquire it again","code":"containerAction","metadata":{"reason":["unknown-lease"],"retryable":["false"]}}]}}`, wantReason: SwarmReasonUnknownLease},
		{name: "gone, after a listing", status: 400, body: `{"error":{"code":"containerAction","message":"no container c-abc in the pool","meta":[{"error":"no container c-abc in the pool","code":"containerAction","metadata":{"reason":["container-not-found"],"retryable":["false"]}}]}}`, wantReason: SwarmReasonContainerNotFound},
		{name: "gone, before a listing", status: 400, body: `{"error":{"code":"containerAction","message":"Container not found","meta":[{"error":"Container not found","code":"containerAction","metadata":null}]}}`, wantReason: SwarmReasonContainerNotFound},
		// Only the 400 a renew answers right after a delete was observed: the
		// same words in a 5xx say nothing of the container.
		{name: "the same words in a 5xx", status: 502, body: `{"error":{"code":"containerAction","message":"Container not found","meta":[{"error":"Container not found","code":"containerAction","metadata":null}]}}`, wantNoReason: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var gotPath, gotLease, gotQuery string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath, gotLease, gotQuery = r.Method+" "+r.URL.Path, r.Header.Get("X-Swarm-Lease"), r.URL.RawQuery
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			c, err := NewSwarmClient(srv.URL, "api-token", srv.Client()).Renew(context.Background(), "c-abc", lease)
			if gotPath != "POST /container/c-abc/renew" || gotLease != lease || gotQuery != "" {
				t.Errorf("request = %s ?%s lease %q, want POST /container/c-abc/renew with the lease only in X-Swarm-Lease", gotPath, gotQuery, gotLease)
			}
			if tt.wantNoReason {
				var se *SwarmError
				if !errors.As(err, &se) || se.Reason != "" {
					t.Errorf("err = %v, want a *SwarmError with no reason", err)
				}
				return
			}
			if tt.wantReason == "" {
				if err != nil || c.LockState != tt.wantState {
					t.Errorf("renew = %+v, %v; want lockState %s", c, err, tt.wantState)
				}
				return
			}
			var se *SwarmError
			if !errors.As(err, &se) || se.Reason != tt.wantReason {
				t.Errorf("err = %v, want a *SwarmError with reason %s", err, tt.wantReason)
			}
		})
	}
}

// TestSwarmClient_ErrorEnvelope_SaysWhatItsMetadataSays: a refusal whose
// message is the platform's placeholder "See metadata" carries its words only
// in meta[] — each item's code and error and every metadata key — so the
// error says what they say, scrubbed of the bearer and the lease, and never
// the bare placeholder (live, Kara's run 2026-10-25: a release of a container
// that could start no process answered "See metadata" and the agent learned
// nothing). A metadata value that is not a list of strings still leaves the
// envelope readable.
func TestSwarmClient_ErrorEnvelope_SaysWhatItsMetadataSays(t *testing.T) {
	t.Parallel()

	const lease = "feedfacefeedfacefeedfacefeedface"
	const token = "orch-api-token-0001"
	tests := []struct {
		name        string
		body        string
		wantCode    string
		wantReason  string
		wantSays    []string
		wantNotSays []string
	}{
		{
			name: "See metadata: the item's code, error and metadata are what it says",
			body: `{"error":{"code":"containerAction","message":"See metadata","meta":[{"code":"commandExec",` +
				`"error":"fork/exec /opt/zerops/bin/zsc: resource temporarily unavailable",` +
				`"metadata":{"containerId":["c-5"],"echo":["bearer ` + token + ` lease ` + lease + `"]}}]}}`,
			wantCode: "containerAction",
			wantSays: []string{"commandExec", "fork/exec /opt/zerops/bin/zsc: resource temporarily unavailable", "containerId: c-5", "[redacted]"},
			wantNotSays: []string{"See metadata", token, lease},
		},
		{
			name: "metadata values that are no list of strings",
			body: `{"error":{"code":"containerAction","message":"See metadata","meta":[{"code":"containerAction",` +
				`"error":"the container is in state ACTION_FAILED","metadata":{"reason":"container-failed","containerId":"c-5","attempts":3}}]}}`,
			wantCode:    "containerAction",
			wantReason:  "container-failed",
			wantSays:    []string{"the container is in state ACTION_FAILED", "containerId: c-5", "attempts: 3"},
			wantNotSays: []string{"See metadata"},
		},
		{
			name: "a message of its own keeps it, and the metadata it does not repeat follows",
			body: `{"error":{"code":"containerAction","message":"release failed","meta":[{"code":"containerAction",` +
				`"error":"release failed","metadata":{"reason":["reset-failed"],"retryable":["false"],"step":["restore snapshot"]}}]}}`,
			wantCode:   "containerAction",
			wantReason: "reset-failed",
			wantSays:   []string{"release failed", "step: restore snapshot"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			c := NewSwarmClient(srv.URL, token, srv.Client())
			_, err := c.Release(context.Background(), "c-5", lease, true)
			var se *SwarmError
			if !errors.As(err, &se) {
				t.Fatalf("err = %v (%T), want a *SwarmError", err, err)
			}
			if se.Code != tt.wantCode {
				t.Errorf("Code = %q, want %q", se.Code, tt.wantCode)
			}
			if se.Reason != tt.wantReason {
				t.Errorf("Reason = %q, want %q", se.Reason, tt.wantReason)
			}
			text := err.Error()
			for _, want := range tt.wantSays {
				if !strings.Contains(text, want) {
					t.Errorf("error %q does not say %q", text, want)
				}
			}
			for _, not := range tt.wantNotSays {
				if strings.Contains(text, not) {
					t.Errorf("error %q says %q", text, not)
				}
			}
		})
	}
}
