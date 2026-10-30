package hq

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func observeProfile(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	path := ObserveCredentialPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}
func observeEnvelope(action, project, field string) string {
	return `{"action":"` + action + `","appId":"app","projectId":` + project + `,"coverage":{"source":"hq","observedAt":"2026-10-29T12:00:00Z","freshness":"fresh","scope":"authorized target","truncated":false},"` + field + `":[]}`
}
func TestObserveClient_Rotation_StrictDirectRequests(t *testing.T) {
	// non-parallel: fixed profile file follows HOME.
	path := observeProfile(t)
	for _, action := range []string{"environments", "status", "deployments", "processes", "process", "versions", "version", "logs", "build_logs"} {
		name := action
		if action == "status" {
			name = "environment status"
		}
		if action == "logs" {
			name = "environment logs"
		}
		t.Run(name, func(t *testing.T) {
			tokens := []string{"zobs.org." + strings.Repeat("A", 43), "zobs.org." + strings.Repeat("B", 42) + "A"}
			fields := map[string]string{"environments": "environments", "status": "services", "deployments": "deployments", "processes": "processes", "process": "processes", "versions": "versions", "version": "versions", "logs": "entries", "build_logs": "entries"}
			var calls int
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.String() != "/api/mate/observe" || r.Header.Get("Authorization") != "Observe "+tokens[calls] || r.Header.Get("X-Api-Key") != "project-key" {
					t.Error("wrong direct observe request or authorization")
				}
				var request map[string]any
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Fatal(err)
				}
				if request["action"] != action || request["limit"] != float64(2) || request["projectId"] != "target" {
					t.Errorf("wrong fixed inputs: %v", request)
				}
				if _, ok := request["token"]; ok {
					t.Error("model received credential")
				}
				calls++
				_, _ = w.Write([]byte(observeEnvelope(action, `"target"`, fields[action])))
			}))
			defer fake.Close()
			client, err := OpenObservation(fake.Client(), Identity{Origin: fake.URL, ProjectID: "source", Key: "project-key"})
			if err != nil {
				t.Fatal(err)
			}
			for _, token := range tokens {
				if err := os.WriteFile(path+".next", []byte(token), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(path+".next", path); err != nil {
					t.Fatal(err)
				}
				out, err := client.Observe(context.Background(), ObservationRequest{Action: action, ProjectID: "target", ServiceID: "service", ProcessID: "process", VersionID: "version", Limit: 2})
				if err != nil || string(out) != observeEnvelope(action, `"target"`, fields[action]) {
					t.Fatalf("bounded HQ facts did not reach consumer: %v", err)
				}
			}
			if calls != 2 {
				t.Fatalf("requests=%d want 2", calls)
			}
		})
	}
}

func TestObserveClient_AppDeployments_PreserveBoundedPushOutcomes(t *testing.T) {
	// non-parallel: fixed profile credential follows HOME.
	path := observeProfile(t)
	if err := os.WriteFile(path, []byte("zobs.org."+strings.Repeat("A", 43)), 0600); err != nil {
		t.Fatal(err)
	}
	push := `{"environment":"stage","kind":"push","service":"web","sha":"` + strings.Repeat("a", 40) + `","job":"push-job","state":"queued","processId":null,"behind":null,"reason":null}`
	for _, tt := range []struct{ name, pushes string }{
		{"empty app push page", `[]`},
		{"retained push outcome", `[` + push + `]`},
		{"maximum bounded push page", `[` + strings.Repeat(push+`,`, 99) + push + `]`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := strings.TrimSuffix(observeEnvelope("deployments", "null", "deployments"), "}") + `,"pushes":` + tt.pushes + `}`
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			defer fake.Close()
			client, err := OpenObservation(fake.Client(), Identity{Origin: fake.URL, ProjectID: "source", Key: "project-key"})
			if err != nil {
				t.Fatal(err)
			}
			got, err := client.Observe(context.Background(), ObservationRequest{Action: "deployments"})
			if err != nil || string(got) != body {
				t.Fatalf("app deployment and push facts must survive unchanged: error=%v unchanged=%v", err, string(got) == body)
			}
		})
	}
}

func TestObserveClient_BadAnswers_FailClosedWithoutSecrets(t *testing.T) {
	// non-parallel: fixed profile file follows HOME.
	path := observeProfile(t)
	token := "zobs.org." + strings.Repeat("A", 43)
	if err := os.WriteFile(path, []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	valid := observeEnvelope("status", `"target"`, "services")
	for _, tt := range []struct {
		name, body string
		status     int
	}{
		{"bad sibling array", observeEnvelope("environments", "null", "environments")[:len(observeEnvelope("environments", "null", "environments"))-1] + `,"siblingMates":{"raw":"DTO"}}`, 200},
		{"bad extra array", strings.Replace(valid, `"services":[]`, `"services":[],"entries":{"raw":"DTO"}`, 1), 200},
		{"oversize extra array", strings.Replace(valid, `"services":[]`, `"services":[],"entries":[`+strings.Repeat(`{},`, 100)+`{}]`, 1), 200},
		{"bad push array", strings.Replace(valid, `"services":[]`, `"services":[],"pushes":{"raw":"DTO"}`, 1), 200},
		{"null push array", strings.Replace(valid, `"services":[]`, `"services":[],"pushes":null`, 1), 200},
		{"string push array", strings.Replace(valid, `"services":[]`, `"services":[],"pushes":"invalid"`, 1), 200},
		{"oversize push array", strings.Replace(valid, `"services":[]`, `"services":[],"pushes":[`+strings.Repeat(`{},`, 100)+`{}]`, 1), 200},
		{"null", "null", 200}, {"array", "[]", 200}, {"legacy", `{"services":[]}`, 200},
		{"trailing", valid + `{}`, 200}, {"wrong action", strings.Replace(valid, `"action":"status"`, `"action":"logs"`, 1), 200},
		{"wrong target", strings.Replace(valid, `"projectId":"target"`, `"projectId":"other"`, 1), 200},
		{"missing coverage", `{"action":"status","appId":"app","projectId":"target","services":[]}`, 200},
		{"raw DTO", strings.Replace(valid, `"services":[]`, `"services":[],"userData":"private"`, 1), 200},
		{"oversize", strings.Repeat(" ", 512*1024+1), 200},
		{"expired", `{"code":"observe_authorization_required"}`, 401},
		{"revoked", `{"code":"observe_authorization_required"}`, 401},
		{"reflected auth", `{"code":"` + token + `","reason":"` + token + `"}`, 401},
		{"reflected parent", `{"code":"project-key","reason":"project-key"}`, 403},
		{"reflected parent unavailable", `{"code":"project-key","reason":"project-key"}`, 503},
		{"reflected parent unauthorized", `{"code":"project-key","reason":"project-key"}`, 401},
		{"reflected denial", `{"code":"forbidden","reason":"` + token + `"}`, 403},
		{"reflected invalid envelope", strings.Replace(valid, `"services":[]`, `"services":"`+token+`"`, 1), 200},
		{"no content", "", 204}, {"reflected unavailable", `{"code":"` + token + `"}`, 503},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer fake.Close()
			client, err := OpenObservation(fake.Client(), Identity{Origin: fake.URL, ProjectID: "source", Key: "project-key"})
			if err != nil {
				t.Fatal(err)
			}
			input := ObservationRequest{Action: "status", ProjectID: "target"}
			if tt.name == "bad sibling array" {
				input = ObservationRequest{Action: "environments"}
			}
			out, err := client.Observe(context.Background(), input)
			if err == nil || len(out) != 0 || strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "project-key") {
				t.Fatalf("answer did not fail safely: %v", err)
			}
			if calls.Load() != 1 {
				t.Fatalf("requests=%d want one attempt", calls.Load())
			}
			if tt.status == 401 && !errors.Is(err, ErrNoObserveCredential) {
				t.Fatalf("401 needs clear authorization failure: %v", err)
			}
		})
	}
}

func TestObserveClient_Redirect_NoCredentialForwarded(t *testing.T) {
	// non-parallel: fixed profile file follows HOME.
	path := observeProfile(t)
	if err := os.WriteFile(path, []byte("zobs.org."+strings.Repeat("A", 43)), 0600); err != nil {
		t.Fatal(err)
	}
	var forwarded atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	defer target.Close()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer fake.Close()
	transport := fake.Client()
	client, err := OpenObservation(transport, Identity{Origin: fake.URL, ProjectID: "source", Key: "project-key"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Observe(context.Background(), ObservationRequest{}); err == nil {
		t.Fatal("redirect must fail")
	}
	if forwarded.Load() != 0 || transport.CheckRedirect != nil {
		t.Fatal("redirect forwarded credential or mutated shared client")
	}
}

func TestObserveClient_Overload_RetryLaterWithoutRepeating(t *testing.T) {
	// non-parallel: profile credential follows HOME.
	path := observeProfile(t)
	if err := os.WriteFile(path, []byte("zobs.org."+strings.Repeat("A", 43)), 0600); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":"too_many_requests"}`))
	}))
	defer fake.Close()
	client, err := OpenObservation(fake.Client(), Identity{Origin: fake.URL, ProjectID: "source", Key: "project-key"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Observe(context.Background(), ObservationRequest{})
	var unavailable *UnavailableError
	if !errors.As(err, &unavailable) || unavailable.Code != "too_many_requests" || calls.Load() != 1 {
		t.Fatalf("overload must advise retry later after one attempt: error=%v calls=%d", err, calls.Load())
	}
}

func TestObserveClient_AuthorizedLogs_PreserveHQCoverageAndOpaqueRecords(t *testing.T) {
	// non-parallel: fixed profile credential follows HOME.
	path := observeProfile(t)
	token := "zobs.org." + strings.Repeat("A", 43)
	if err := os.WriteFile(path, []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"logs", "build_logs"} {
		for _, scenario := range []string{"HQ log coverage", "opaque source record", "escaped opaque source record"} {
			t.Run(action+"/"+scenario, func(t *testing.T) {
				body := observeEnvelope(action, `"target"`, "entries")
				if scenario == "HQ log coverage" {
					body = strings.Replace(body, `"truncated":false`, `"truncated":false,"logWindow":{"rawCount":0,"rawLimit":100,"rawWindowFull":false,"nextPage":false}`, 1)
				} else {
					record := `{"id":"native-record-1","tag":"zbuilder/version","hostname":"web","content":"` + token + strings.Repeat("x", 5000) + `","structuredData":{"vendor":{"nested":[false,null,42]}},"arbitrarySourceField":"unchanged"}`
					if scenario == "escaped opaque source record" {
						record = strings.Replace(record, "zobs.", `zobs\u002e`, 1)
					}
					body = strings.Replace(body, `"entries":[]`, `"entries":[`+record+`]`, 1)
				}
				fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					_, _ = w.Write([]byte(body))
				}))
				defer fake.Close()
				client, err := OpenObservation(fake.Client(), Identity{Origin: fake.URL, ProjectID: "source", Key: "project-key"})
				if err != nil {
					t.Fatal(err)
				}
				out, err := client.Observe(context.Background(), ObservationRequest{Action: action, ProjectID: "target", ServiceID: "service", VersionID: "version"})
				if err != nil || string(out) != body {
					t.Fatalf("authorized HQ log page must survive byte-for-byte: error=%v unchanged=%v", err, string(out) == body)
				}
			})
		}
	}
}

func TestObserveClient_LogBounds_ValidateAndForwardNativeScalars(t *testing.T) {
	// non-parallel: fixed profile credential follows HOME.
	path := observeProfile(t)
	if err := os.WriteFile(path, []byte("zobs.org."+strings.Repeat("A", 43)), 0600); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"environments", "status", "deployments", "processes", "process", "versions", "version", "logs", "build_logs"} {
		for _, boundary := range []struct {
			name, value string
			valid       bool
		}{
			{"native id", "record:id-123_ABC", true},
			{"ISO timestamp", "2026-10-29T12:00:00.123+02:00", true},
			{"maximum", strings.Repeat("r", 256), true},
			{"printable edges", " ~", true},
			{"empty", "", false},
			{"newline", "line\nbreak", false},
			{"tab", "tab\t", false},
			{"NUL", "null\x00", false},
			{"DEL", "delete\x7f", false},
			{"non ASCII", "nonascii-é", false},
			{"oversized", strings.Repeat("r", 257), false},
		} {
			for _, field := range []string{"from", "till"} {
				t.Run(action+"/"+field+"/"+boundary.name, func(t *testing.T) {
					wantValid := (action == "logs" || action == "build_logs") && boundary.valid
					request := map[string]any{"action": action, "projectId": "target", "serviceId": "service", "processId": "process", "versionId": "version", field: boundary.value}
					encoded, err := json.Marshal(request)
					if err != nil {
						t.Fatal(err)
					}
					var in ObservationRequest
					if err := json.Unmarshal(encoded, &in); err != nil {
						t.Fatal(err)
					}
					var calls int
					fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						calls++
						var got map[string]any
						if err := json.NewDecoder(r.Body).Decode(&got); err != nil || got[field] != boundary.value {
							t.Error("native log boundary was not forwarded exactly")
						}
						_, _ = w.Write([]byte(observeEnvelope(action, `"target"`, "entries")))
					}))
					defer fake.Close()
					client, err := OpenObservation(fake.Client(), Identity{Origin: fake.URL, ProjectID: "source", Key: "project-key"})
					if err != nil {
						t.Fatal(err)
					}
					_, err = client.Observe(context.Background(), in)
					if wantValid && (err != nil || calls != 1) || !wantValid && (err == nil || calls != 0) {
						t.Fatalf("boundary applicability/validation: valid=%v error=%v calls=%d", wantValid, err, calls)
					}
				})
			}
		}
	}
}

func TestObserveClient_MissingParent_NoRequest(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer fake.Close()
	_, err := OpenObservation(fake.Client(), Identity{Origin: fake.URL, ProjectID: "source"})
	if !errors.Is(err, ErrNoIdentity) || calls.Load() != 0 {
		t.Fatalf("missing native parent must refuse before HTTP: error=%v calls=%d", err, calls.Load())
	}
}

func TestObserveClient_HeaderTrace_HidesBothProofs(t *testing.T) {
	// non-parallel: fixed credential follows HOME.
	path := observeProfile(t)
	const child = "zobs.org.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	const parent = "native-parent-secret"
	if err := os.WriteFile(path, []byte(child), 0600); err != nil {
		t.Fatal(err)
	}
	for _, protocol := range []string{"http1", "http2"} {
		t.Run(protocol, func(t *testing.T) {
			fake := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Observe "+child || r.Header.Get("X-Api-Key") != parent {
					t.Error("both proofs must reach HQ unchanged")
				}
				_, _ = w.Write([]byte(observeEnvelope("status", `"target"`, "services")))
			}))
			fake.EnableHTTP2 = protocol == "http2"
			fake.StartTLS()
			defer fake.Close()
			client, err := OpenObservation(fake.Client(), Identity{Origin: fake.URL, ProjectID: "source", Key: parent})
			if err != nil {
				t.Fatal(err)
			}
			var headers strings.Builder
			var connected atomic.Bool
			trace := &httptrace.ClientTrace{
				WroteHeaderField: func(key string, values []string) { headers.WriteString(key + ": " + strings.Join(values, ",") + "\n") },
				GotConn:          func(httptrace.GotConnInfo) { connected.Store(true) },
			}
			ctx := httptrace.WithClientTrace(context.Background(), trace)
			out, err := client.Observe(ctx, ObservationRequest{Action: "status", ProjectID: "target"})
			if err != nil || string(out) != observeEnvelope("status", `"target"`, "services") {
				t.Fatalf("traced request must preserve facts: %v", err)
			}
			got := headers.String()
			if strings.Contains(got, child) || strings.Contains(got, parent) {
				t.Fatal("HTTP header tracing exposed an Observe proof")
			}
			if !strings.Contains(strings.ToLower(got), "content-type: application/json") || !connected.Load() || strings.Count(got, "[redacted]") != 2 {
				t.Fatal("non-secret tracing must remain available")
			}
			trace.WroteHeaderField("X-Api-Key", []string{"original-hook"})
			if !strings.Contains(headers.String(), "original-hook") {
				t.Fatal("Observe mutated the caller's trace")
			}
		})
	}
}

type observeErrorDoer struct{}

func (observeErrorDoer) Do(req *http.Request) (*http.Response, error) {
	return nil, errors.New(req.Header.Get("Authorization") + " " + req.Header.Get("X-Api-Key"))
}

func TestObserveClient_TransportEcho_NoProofInError(t *testing.T) {
	// non-parallel: fixed credential follows HOME.
	path := observeProfile(t)
	const child = "zobs.org.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	const parent = "native-parent-secret"
	if err := os.WriteFile(path, []byte(child), 0600); err != nil {
		t.Fatal(err)
	}
	client, err := OpenObservation(observeErrorDoer{}, Identity{Origin: "https://hq.test", ProjectID: "source", Key: parent})
	if err != nil {
		t.Fatal(err)
	}
	out, err := client.Observe(context.Background(), ObservationRequest{})
	if !IsUnavailable(err) || len(out) != 0 || strings.Contains(err.Error(), child) || strings.Contains(err.Error(), parent) {
		t.Fatal("transport errors must expose neither proof nor source data")
	}
}
