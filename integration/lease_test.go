// Tests for: integration — zerops_lease through the full MCP server, against
// a mock platform and a fake pool orchestrator.

package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

const (
	leaseIntToken = "orch-api-token-0001"
	leaseIntLease = "0123456789abcdef0123456789abcdef"
)

// orchestratorRoute sends every request for host to target, leaving other
// traffic on the original transport. The server builds its orchestrator
// client over the default transport, so swapping it is the only seam the real
// wiring offers.
type orchestratorRoute struct {
	host   string
	target *url.URL
	next   http.RoundTripper
}

func (o orchestratorRoute) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != o.host {
		return o.next.RoundTrip(r)
	}
	c := r.Clone(r.Context())
	c.URL.Scheme, c.URL.Host = o.target.Scheme, o.target.Host
	return o.next.RoundTrip(c)
}

func TestLease_Integration_AcquireStatusRelease(t *testing.T) {
	// Non-parallel: t.Chdir (the state dir the server resolves from the cwd)
	// and a swap of http.DefaultTransport (the seam to the orchestrator host).
	t.Chdir(t.TempDir())

	var mu sync.Mutex
	reserved, resetOnRelease := false, ""
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+leaseIntToken {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"code":"notAuthorized","message":"missing or wrong API token"}}`))
			return
		}
		lock := ""
		if reserved {
			lock = "reserved"
		}
		container := `{"id":"c-abc","name":"node-id-3","number":3,"hostname":"node-id-3.runtime.devpool.zerops","status":"ACTIVE",` +
			`"serviceId":"svc-pool","stackId":"stk","appVersionId":"av","current":true,"lockState":"` + lock + `","dirty":false`
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/container":
			_, _ = w.Write([]byte(`[` + container + `}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/container/acquire":
			reserved = true
			_, _ = w.Write([]byte(strings.Replace(container, `"lockState":""`, `"lockState":"reserved"`, 1) + `,"lease":"` + leaseIntLease + `"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/container/c-abc/release" && r.Header.Get("X-Swarm-Lease") == leaseIntLease && !r.URL.Query().Has("lease"):
			reserved, resetOnRelease = false, r.URL.Query().Get("reset")
			_, _ = w.Write([]byte(container + `}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer orch.Close()
	target, err := url.Parse(orch.URL)
	if err != nil {
		t.Fatal(err)
	}
	orig := http.DefaultTransport
	http.DefaultTransport = orchestratorRoute{host: "orch.devpool.zerops", target: target, next: orig}
	t.Cleanup(func() { http.DefaultTransport = orig })

	mock := defaultMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-pool", Name: "devpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
			{ID: "svc-1", Name: "app", Status: "RUNNING", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{ID: "env-t", Key: "API_TOKEN", Content: leaseIntToken, Sensitive: true}})
	session, cleanup := setupTestServer(t, mock, defaultLogFetcher())
	defer cleanup()

	type leaseResult struct {
		Action string `json:"action"`
		Leases []struct {
			Alias             string `json:"alias"`
			Pool              string `json:"pool"`
			ContainerHostname string `json:"containerHostname"`
			Branch            string `json:"branch"`
			State             string `json:"state"`
		} `json:"leases"`
	}
	call := func(args map[string]any) leaseResult {
		t.Helper()
		res := callAndGetResult(t, session, "zerops_lease", args)
		text := getTextContent(t, res)
		if res.IsError {
			t.Fatalf("zerops_lease %v failed: %s", args["action"], text)
		}
		for _, secret := range []string{leaseIntLease, leaseIntToken} {
			if strings.Contains(text, secret) {
				t.Fatalf("zerops_lease %v result carries a secret: %s", args["action"], text)
			}
		}
		var out leaseResult
		if err := json.Unmarshal([]byte(text), &out); err != nil {
			t.Fatalf("result is not JSON: %v\n%s", err, text)
		}
		return out
	}

	got := call(map[string]any{"action": "acquire", "pool": "devpool", "repo": "https://github.com/acme/app", "branch": "feature-x"})
	if len(got.Leases) != 1 || got.Leases[0].Alias != "devpool-3" || got.Leases[0].State != "held" ||
		got.Leases[0].ContainerHostname != "node-id-3.runtime.devpool.zerops" || got.Leases[0].Branch != "feature-x" {
		t.Fatalf("acquire = %+v, want held devpool-3 on node-id-3.runtime.devpool.zerops for feature-x", got.Leases)
	}
	recPath := filepath.Join(".zcp", "state", "leases", "devpool-3.json")
	info, err := os.Stat(recPath)
	if err != nil {
		t.Fatalf("lease record missing: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("record mode = %o, want 600", info.Mode().Perm())
	}

	got = call(map[string]any{"action": "status"})
	if len(got.Leases) != 1 || got.Leases[0].Alias != "devpool-3" || got.Leases[0].State != "held" {
		t.Fatalf("status = %+v, want the one held devpool-3", got.Leases)
	}

	got = call(map[string]any{"action": "release", "lease": "devpool-3"})
	if len(got.Leases) != 1 || got.Leases[0].State != "released" {
		t.Fatalf("release = %+v, want devpool-3 released", got.Leases)
	}
	mu.Lock()
	gotReset := resetOnRelease
	mu.Unlock()
	if gotReset != "true" {
		t.Errorf("release reset = %q, want true (the default)", gotReset)
	}
	if _, err := os.Stat(recPath); !os.IsNotExist(err) {
		t.Errorf("record still present after release: %v", err)
	}

	got = call(map[string]any{"action": "status"})
	if len(got.Leases) != 0 {
		t.Errorf("status after release = %+v, want none", got.Leases)
	}
}

func TestLease_Integration_RolloutSkipsTheLease(t *testing.T) {
	// Non-parallel: t.Chdir and a swap of http.DefaultTransport, as above.
	t.Chdir(t.TempDir())

	var mu sync.Mutex
	reserved := false
	var rolloutQuery, rolloutLease string
	rollouts := 0
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+leaseIntToken {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"code":"notAuthorized","message":"missing or wrong API token"}}`))
			return
		}
		lock := ""
		if reserved {
			lock = "reserved"
		}
		container := `{"id":"c-abc","name":"node-id-3","number":3,"hostname":"node-id-3.runtime.devpool.zerops","status":"ACTIVE",` +
			`"serviceId":"svc-pool","stackId":"stk","appVersionId":"av-1","current":false,"lockState":"` + lock + `","dirty":false`
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/container":
			_, _ = w.Write([]byte(`[` + container + `}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/container/acquire":
			reserved = true
			_, _ = w.Write([]byte(strings.Replace(container, `"lockState":""`, `"lockState":"reserved"`, 1) + `,"lease":"` + leaseIntLease + `"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/container/rollout":
			rollouts++
			rolloutQuery, rolloutLease = r.URL.RawQuery, r.Header.Get("X-Swarm-Lease")
			_, _ = w.Write([]byte(`{"activeAppVersionId":"av-2","replaced":["c-old1","c-old2"],` +
				`"created":[{"id":"c-n1","number":5,"current":true},{"id":"c-n2","number":6,"current":true}],` +
				`"retiring":["c-busy"],"skipped":[{"id":"c-abc","reason":"held-by-other"}],"createErrors":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer orch.Close()
	target, err := url.Parse(orch.URL)
	if err != nil {
		t.Fatal(err)
	}
	orig := http.DefaultTransport
	http.DefaultTransport = orchestratorRoute{host: "orch.devpool.zerops", target: target, next: orig}
	t.Cleanup(func() { http.DefaultTransport = orig })

	mock := defaultMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-pool", Name: "devpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
		}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{ID: "env-t", Key: "API_TOKEN", Content: leaseIntToken, Sensitive: true}})
	session, cleanup := setupTestServer(t, mock, defaultLogFetcher())
	defer cleanup()

	if res := callAndGetResult(t, session, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": "https://github.com/acme/app", "branch": "feature-x"}); res.IsError {
		t.Fatalf("acquire failed: %s", getTextContent(t, res))
	}
	res := callAndGetResult(t, session, "zerops_lease", map[string]any{"action": "rollout", "pool": "devpool"})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("rollout failed: %s", text)
	}
	for _, secret := range []string{leaseIntLease, leaseIntToken} {
		if strings.Contains(text, secret) {
			t.Fatalf("rollout result carries a secret: %s", text)
		}
	}
	var got struct {
		Action  string `json:"action"`
		Rollout struct {
			Replaced int `json:"replaced"`
			Created  int `json:"created"`
			Retiring int `json:"retiring"`
			Skipped  int `json:"skipped"`
			Skips    []struct {
				Lease  string `json:"lease"`
				Reason string `json:"reason"`
			} `json:"skips"`
		} `json:"rollout"`
		Envelope json.RawMessage `json:"envelope"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	r := got.Rollout
	if got.Action != "rollout" || r.Replaced != 2 || r.Created != 2 || r.Retiring != 1 || r.Skipped != 1 {
		t.Errorf("rollout = %+v, want 2 replaced, 2 created, 1 retiring, 1 skipped", r)
	}
	if len(r.Skips) != 1 || r.Skips[0].Lease != "devpool-3" || r.Skips[0].Reason != "held-by-other" {
		t.Errorf("skips = %+v, want the lease devpool-3 skipped as held-by-other", r.Skips)
	}
	if len(got.Envelope) == 0 {
		t.Errorf("rollout result carries no envelope:\n%s", text)
	}
	mu.Lock()
	defer mu.Unlock()
	if rollouts != 1 || rolloutQuery != "" || rolloutLease != "" {
		t.Errorf("orchestrator saw %d rollout(s), query %q, lease header %q; want one, with no force and no lease", rollouts, rolloutQuery, rolloutLease)
	}
}

// stageLeaseSSH is a leased container whose checkout is on feat-x at a commit
// that is not on origin/main; it records every command and its host.
type stageLeaseSSH struct {
	mu    sync.Mutex
	calls []sshCall
}

func (s *stageLeaseSSH) ExecSSH(_ context.Context, hostname, command string) ([]byte, error) {
	s.mu.Lock()
	s.calls = append(s.calls, sshCall{hostname: hostname, command: command})
	s.mu.Unlock()
	switch {
	case strings.Contains(command, "merge-base --is-ancestor"):
		return []byte("ZCP:MAIN:off 9f8e7d6c5b4a39281706f5e4d3c2b1a098765432\n"), nil
	case strings.Contains(command, "rev-parse --verify HEAD") && strings.Contains(command, "git status --porcelain"):
		return []byte("a1b2c3d4e5f60718293a4b5c6d7e8f9012345678\nZCP:BRANCH:feat-x\n"), nil
	}
	return []byte("ok"), nil
}

func (s *stageLeaseSSH) ExecSSHBackground(ctx context.Context, hostname, command string, _ time.Duration) ([]byte, error) {
	return s.ExecSSH(ctx, hostname, command)
}

// first is the index of the first call whose command contains sub, -1 when none.
func (s *stageLeaseSSH) first(sub string) (int, sshCall) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.calls {
		if strings.Contains(c.command, sub) {
			return i, c
		}
	}
	return -1, sshCall{}
}

// TestLease_Integration_DeployToStage_SaysStageTracksMain: zerops_deploy from
// a lease to the part's stage, through the full MCP server, fetches
// origin/main in the leased container before it reads what ships, deploys a
// branch commit that is not on main, and says the stage tracks main
// (docs/spec-workflows.md §8 DP-8).
func TestLease_Integration_DeployToStage_SaysStageTracksMain(t *testing.T) {
	// Non-parallel: t.Chdir (the state dir the server resolves from the cwd).
	t.Chdir(t.TempDir())

	if err := os.MkdirAll(filepath.Join(".zcp", "state", "leases"), 0o700); err != nil {
		t.Fatal(err)
	}
	record := `{"poolHostname":"webpool","poolServiceId":"svc-pool","containerId":"c-3","containerNumber":3,` +
		`"containerHostname":"node-id-3.runtime.webpool.zerops","lease":"secret-lease-value","branch":"feat-x","acquiredAt":"2026-10-23T12:00:00Z"}`
	if err := os.WriteFile(filepath.Join(".zcp", "state", "leases", "webpool-3.json"), []byte(record), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll("webpool-3", 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := "zerops:\n  - setup: appstage\n    build:\n      base: nodejs@22\n      deployFiles: [.]\n    run:\n      start: node server.js\n"
	if err := os.WriteFile(filepath.Join("webpool-3", "zerops.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	mock := defaultMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-stage", Name: "appstage", Status: "RUNNING", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
			{ID: "svc-pool", Name: "webpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-stage", ProjectID: "proj-1", ServiceStackID: "svc-stage", Source: "CLI", Status: "ACTIVE", Sequence: 1, Build: &platform.BuildInfo{}},
		})
	ssh := &stageLeaseSSH{}
	session, cleanup := setupTestServerWithDeploy(t, mock, defaultLogFetcher(), ssh)
	defer cleanup()

	res := callAndGetResult(t, session, "zerops_deploy", map[string]any{"sourceService": "webpool-3", "targetService": "appstage"})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("a deploy from a lease to the stage is never refused for its commit: %s", text)
	}
	var got ops.DeployResult
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	if got.Status != "DEPLOYED" || got.SourceService != "webpool-3" || got.TargetService != "appstage" {
		t.Errorf("result = %s → %s %s, want webpool-3 → appstage DEPLOYED", got.SourceService, got.TargetService, got.Status)
	}
	if strings.Contains(text, "secret-lease-value") {
		t.Fatalf("the deploy result carries the lease: %s", text)
	}

	fetch, fetchCall := ssh.first("git fetch")
	head, _ := ssh.first("rev-parse --verify HEAD")
	push, _ := ssh.first("zcli push")
	switch {
	case fetch < 0:
		t.Fatal("origin/main was not fetched")
	case fetchCall.hostname != "webpool-3":
		t.Errorf("the fetch ran on %q, want the lease webpool-3", fetchCall.hostname)
	case head >= 0 && head < fetch:
		t.Errorf("what ships was read (call %d) before origin/main was fetched (call %d)", head, fetch)
	case push < 0:
		t.Fatal("the push did not run")
	}

	note := ""
	for _, w := range got.Warnings {
		if strings.Contains(w, "Stage tracks main") {
			note = w
		}
	}
	for _, want := range []string{"ships feat-x a1b2c3d", "not on origin/main (at 9f8e7d6)", "preview URL",
		`zerops_deploy sourceService="webpool-3" targetService="appstage" sha="origin/main"`} {
		if !strings.Contains(note, want) {
			t.Errorf("stage note %q lacks %q (warnings %q)", note, want, got.Warnings)
		}
	}
}
