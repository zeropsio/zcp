// Tests for: integration — zerops_lease through the full MCP server, against
// a mock platform and a fake pool orchestrator.

package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/container/"):
			_, _ = w.Write([]byte(container + `}`))
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
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/container/"):
			_, _ = w.Write([]byte(container + `}`))
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

// orchestratorRoutes sends every request for one of its hosts to that host's
// target, leaving other traffic on the original transport (orchestratorRoute
// for several pools).
type orchestratorRoutes struct {
	targets map[string]*url.URL
	next    http.RoundTripper
}

func (o orchestratorRoutes) RoundTrip(r *http.Request) (*http.Response, error) {
	target, ok := o.targets[r.URL.Host]
	if !ok {
		return o.next.RoundTrip(r)
	}
	c := r.Clone(r.Context())
	c.URL.Scheme, c.URL.Host = target.Scheme, target.Host
	return o.next.RoundTrip(c)
}

// groupPoolOrchestrator is one pool's orchestrator holding one container.
func groupPoolOrchestrator(t *testing.T, pool, number string) (target *url.URL, acquires func() int) {
	t.Helper()
	target, acquires, _ = groupPoolOrchestratorReleasing(t, pool, number)
	return target, acquires
}

// groupPoolOrchestratorReleasing is groupPoolOrchestrator that also releases
// its container on the lease, and reports the reset query of each release.
func groupPoolOrchestratorReleasing(t *testing.T, pool, number string) (target *url.URL, acquires func() int, releases func() []string) {
	t.Helper()
	var mu sync.Mutex
	reserved, n := false, 0
	var resets []string
	lease := pool + "-" + leaseIntLease
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		container := `{"id":"c-` + pool + `","name":"node-id-` + number + `","number":` + number + `,"hostname":"node-id-` + number + `.runtime.` + pool + `.zerops","status":"ACTIVE",` +
			`"serviceId":"svc-` + pool + `","stackId":"stk","appVersionId":"av","current":true,"lockState":"` + lock + `","dirty":false`
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/container":
			_, _ = w.Write([]byte(`[` + container + `}]`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/container/"):
			_, _ = w.Write([]byte(container + `}`))
		case r.Method == http.MethodPost && r.URL.Path == "/container/acquire":
			reserved, n = true, n+1
			_, _ = w.Write([]byte(strings.Replace(container, `"lockState":""`, `"lockState":"reserved"`, 1) + `,"lease":"` + lease + `"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/container/c-"+pool+"/release" && r.Header.Get("X-Swarm-Lease") == lease:
			reserved = false
			resets = append(resets, r.URL.Query().Get("reset"))
			_, _ = w.Write([]byte(strings.Replace(container, `"lockState":"reserved"`, `"lockState":""`, 1) + `}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return target, func() int {
			mu.Lock()
			defer mu.Unlock()
			return n
		}, func() []string {
			mu.Lock()
			defer mu.Unlock()
			return slices.Clone(resets)
		}
}

// TestLease_Integration_GroupAcquire_OneLeasePerPoolInPoolsOrder: through the
// full MCP server, acquire with pools leases one container per pool on the
// branch, lists them in the pools' order with their acquiredAt, records each
// with the canonical repository, and an acquire of a pool the branch already
// holds returns that lease reused (DP-17).
func TestLease_Integration_GroupAcquire_OneLeasePerPoolInPoolsOrder(t *testing.T) {
	// Non-parallel: t.Chdir and a swap of http.DefaultTransport, as above.
	t.Chdir(t.TempDir())

	hqTarget, hqAcquires := groupPoolOrchestrator(t, "hqpool", "3")
	webTarget, webAcquires := groupPoolOrchestrator(t, "webpool", "7")
	orig := http.DefaultTransport
	http.DefaultTransport = orchestratorRoutes{targets: map[string]*url.URL{"orch.hqpool.zerops": hqTarget, "orch.webpool.zerops": webTarget}, next: orig}
	t.Cleanup(func() { http.DefaultTransport = orig })

	mock := defaultMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-hqpool", Name: "hqpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
			{ID: "svc-webpool", Name: "webpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
		}).
		WithServiceEnv("svc-hqpool", []platform.ServiceEnvVar{{ID: "env-h", Key: "API_TOKEN", Content: leaseIntToken, Sensitive: true}}).
		WithServiceEnv("svc-webpool", []platform.ServiceEnvVar{{ID: "env-w", Key: "API_TOKEN", Content: leaseIntToken, Sensitive: true}})
	session, cleanup := setupTestServer(t, mock, defaultLogFetcher())
	defer cleanup()

	type leaseResult struct {
		Leases []struct {
			Alias      string `json:"alias"`
			Pool       string `json:"pool"`
			Branch     string `json:"branch"`
			State      string `json:"state"`
			AcquiredAt string `json:"acquiredAt"`
			Reused     bool   `json:"reused"`
		} `json:"leases"`
	}
	call := func(args map[string]any) leaseResult {
		t.Helper()
		res := callAndGetResult(t, session, "zerops_lease", args)
		text := getTextContent(t, res)
		if res.IsError {
			t.Fatalf("zerops_lease %v failed: %s", args, text)
		}
		if strings.Contains(text, leaseIntLease) || strings.Contains(text, leaseIntToken) {
			t.Fatalf("result carries a secret: %s", text)
		}
		var out leaseResult
		if err := json.Unmarshal([]byte(text), &out); err != nil {
			t.Fatalf("result is not JSON: %v\n%s", err, text)
		}
		return out
	}

	got := call(map[string]any{"action": "acquire", "pools": []string{"webpool", "hqpool"}, "repo": "https://github.com/acme/app.git", "branch": "feat-x"})
	if len(got.Leases) != 2 || got.Leases[0].Alias != "webpool-7" || got.Leases[1].Alias != "hqpool-3" {
		t.Fatalf("acquire = %+v, want webpool-7 then hqpool-3", got.Leases)
	}
	for _, l := range got.Leases {
		if l.State != "held" || l.Branch != "feat-x" || l.AcquiredAt == "" || l.Reused {
			t.Errorf("lease = %+v, want held on feat-x with its acquiredAt", l)
		}
	}
	for _, alias := range []string{"webpool-7", "hqpool-3"} {
		raw, err := os.ReadFile(filepath.Join(".zcp", "state", "leases", alias+".json"))
		if err != nil {
			t.Fatalf("record %s: %v", alias, err)
		}
		var rec struct {
			Repo   string `json:"repo"`
			Branch string `json:"branch"`
		}
		if err := json.Unmarshal(raw, &rec); err != nil {
			t.Fatal(err)
		}
		if rec.Repo != "https://github.com/acme/app" || rec.Branch != "feat-x" {
			t.Errorf("%s record = %+v, want the canonical repository on feat-x", alias, rec)
		}
	}

	again := call(map[string]any{"action": "acquire", "pool": "webpool", "repo": "https://github.com/acme/app", "branch": "feat-x"})
	if len(again.Leases) != 1 || again.Leases[0].Alias != "webpool-7" || !again.Leases[0].Reused {
		t.Errorf("second acquire = %+v, want webpool-7 reused", again.Leases)
	}
	if hqAcquires() != 1 || webAcquires() != 1 {
		t.Errorf("acquires hq=%d web=%d, want one container per pool", hqAcquires(), webAcquires())
	}
}

// activityLeaseSSH is the leased container #3 of devpool whose checkout answers
// zcp's activity read as given (exit 255 — unreachable — when unreachable is
// set); it records every command and its host.
type activityLeaseSSH struct {
	mu          sync.Mutex
	answer      string
	unreachable bool
	calls       []sshCall
}

func (s *activityLeaseSSH) ExecSSH(ctx context.Context, hostname, command string) ([]byte, error) {
	s.mu.Lock()
	s.calls = append(s.calls, sshCall{hostname: hostname, command: command})
	answer, unreachable := s.answer, s.unreachable
	s.mu.Unlock()
	if !strings.Contains(command, "@@zcp-lease activity") {
		return []byte("ok"), nil
	}
	if unreachable {
		// ssh's own exit status when it cannot reach the host.
		exit := exec.CommandContext(ctx, "sh", "-c", "exit 255").Run()
		return nil, &platform.SSHExecError{Hostname: hostname, Output: "ssh: connect to host node-id-3.runtime.devpool.zerops port 22: Connection refused", Err: exit}
	}
	return []byte(answer), nil
}

func (s *activityLeaseSSH) ExecSSHBackground(ctx context.Context, hostname, command string, _ time.Duration) ([]byte, error) {
	return s.ExecSSH(ctx, hostname, command)
}

func (s *activityLeaseSSH) reads() []sshCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []sshCall
	for _, c := range s.calls {
		if strings.Contains(c.command, "@@zcp-lease activity") {
			out = append(out, c)
		}
	}
	return out
}

// TestLease_Integration_StatusReportsActivityAndUnsaved: through the full MCP
// server, zerops_lease action=status reads the held lease's checkout once, on
// its alias, and reports its activity and unsaved work; a container ssh cannot
// reach stays held and its activity says why, with no change time and no
// unsaved work (docs/spec-workflows.md §8 DP-21).
func TestLease_Integration_StatusReportsActivityAndUnsaved(t *testing.T) {
	// Non-parallel: t.Chdir and a swap of http.DefaultTransport, as above.
	t.Chdir(t.TempDir())

	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+leaseIntToken || r.Method != http.MethodGet || r.URL.Path != "/container" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`[{"id":"c-abc","name":"node-id-3","number":3,"hostname":"node-id-3.runtime.devpool.zerops","status":"ACTIVE",` +
			`"serviceId":"svc-pool","stackId":"stk","appVersionId":"av","current":true,"lockState":"reserved","dirty":false}]`))
	}))
	defer orch.Close()
	target, err := url.Parse(orch.URL)
	if err != nil {
		t.Fatal(err)
	}
	orig := http.DefaultTransport
	http.DefaultTransport = orchestratorRoute{host: "orch.devpool.zerops", target: target, next: orig}
	t.Cleanup(func() { http.DefaultTransport = orig })

	if err := os.MkdirAll(filepath.Join(".zcp", "state", "leases"), 0o700); err != nil {
		t.Fatal(err)
	}
	record := `{"poolHostname":"devpool","poolServiceId":"svc-pool","containerId":"c-abc","containerNumber":3,` +
		`"containerHostname":"node-id-3.runtime.devpool.zerops","lease":"` + leaseIntLease + `","branch":"feat-x","acquiredAt":"2026-10-23T12:00:00Z"}`
	if err := os.WriteFile(filepath.Join(".zcp", "state", "leases", "devpool-3.json"), []byte(record), 0o600); err != nil {
		t.Fatal(err)
	}
	changed := time.Date(2026, 10, 23, 14, 30, 0, 0, time.UTC)
	ssh := &activityLeaseSSH{answer: fmt.Sprintf("@@zcp-lease activity %d %d\n@@zcp-lease status\n M src/App.tsx\n?? notes.txt\n@@zcp-lease unpushed\n1\n",
		changed.Add(-time.Hour).Unix(), changed.Unix())}
	mock := defaultMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-pool", Name: "devpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
		}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{ID: "env-t", Key: "API_TOKEN", Content: leaseIntToken, Sensitive: true}})
	session, cleanup := setupTestServerWithDeploy(t, mock, defaultLogFetcher(), ssh)
	defer cleanup()

	type statusResult struct {
		Leases []struct {
			Alias    string `json:"alias"`
			State    string `json:"state"`
			Activity *struct {
				LastChangeAt     string `json:"lastChangeAt"`
				AcquiredAt       string `json:"acquiredAt"`
				ReadAt           string `json:"readAt"`
				Note             string `json:"note"`
				PreviewOpenConns *int   `json:"previewOpenConns"`
			} `json:"activity"`
			Unsaved *struct {
				Files           []string `json:"files"`
				FileCount       int      `json:"fileCount"`
				UnpushedCommits int      `json:"unpushedCommits"`
			} `json:"unsaved"`
		} `json:"leases"`
	}
	status := func() statusResult {
		t.Helper()
		res := callAndGetResult(t, session, "zerops_lease", map[string]any{"action": "status"})
		text := getTextContent(t, res)
		if res.IsError {
			t.Fatalf("status failed: %s", text)
		}
		if strings.Contains(text, leaseIntLease) || strings.Contains(text, leaseIntToken) {
			t.Fatalf("status result carries a secret: %s", text)
		}
		var out statusResult
		if err := json.Unmarshal([]byte(text), &out); err != nil {
			t.Fatalf("result is not JSON: %v\n%s", err, text)
		}
		if len(out.Leases) != 1 || out.Leases[0].Alias != "devpool-3" || out.Leases[0].State != "held" || out.Leases[0].Activity == nil {
			t.Fatalf("status = %s, want the held devpool-3 with its activity", text)
		}
		return out
	}

	got := status().Leases[0]
	if a := got.Activity; a.LastChangeAt != "2026-10-23T14:30:00Z" || a.AcquiredAt != "2026-10-23T12:00:00Z" || a.ReadAt == "" || a.Note != "" || a.PreviewOpenConns != nil {
		t.Errorf("activity = %+v, want lastChangeAt 2026-10-23T14:30:00Z, the record's acquiredAt, a readAt, no note and no preview facts", *a)
	}
	if u := got.Unsaved; u == nil || strings.Join(u.Files, "|") != " M src/App.tsx|?? notes.txt" || u.FileCount != 2 || u.UnpushedCommits != 1 {
		t.Errorf("unsaved = %+v, want src/App.tsx and notes.txt, 1 commit", u)
	}
	if reads := ssh.reads(); len(reads) != 1 || reads[0].hostname != "devpool-3" {
		t.Errorf("activity reads = %+v, want one, on devpool-3 (the envelope never reads it)", reads)
	}

	ssh.mu.Lock()
	ssh.unreachable = true
	ssh.mu.Unlock()
	got = status().Leases[0]
	if a := got.Activity; a.LastChangeAt != "" || a.AcquiredAt != "2026-10-23T12:00:00Z" || !strings.Contains(a.Note, "could not reach") {
		t.Errorf("unreachable activity = %+v, want no change time, acquiredAt kept and why in the note", *a)
	}
	if got.Unsaved != nil {
		t.Errorf("unreachable unsaved = %+v, want absent: nothing was read", got.Unsaved)
	}
}

// branchGuardSSH is the leased containers as zcp reaches them: dirty names
// the aliases whose checkout holds an untracked file; no container holds an
// unpushed commit; every other command (checkout, prepare, supervisor)
// answers ok.
type branchGuardSSH struct {
	mu    sync.Mutex
	dirty map[string]bool
}

func (s *branchGuardSSH) ExecSSH(_ context.Context, hostname, command string) ([]byte, error) {
	switch {
	case strings.Contains(command, "status --porcelain"):
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.dirty[hostname] {
			return []byte("?? notes.txt\n"), nil
		}
		return nil, nil
	case strings.Contains(command, "rev-list --count"):
		return []byte("0\n"), nil
	}
	return []byte("ok"), nil
}

func (s *branchGuardSSH) ExecSSHBackground(ctx context.Context, hostname, command string, _ time.Duration) ([]byte, error) {
	return s.ExecSSH(ctx, hostname, command)
}

// TestLease_Integration_ReleaseBranch_OneDirtyLeaseRefusesThenForceReleasesEvery:
// through the full MCP server, a release of a branch reads every lease of it;
// one lease with an untracked file refuses the whole branch — no orchestrator
// is asked, every record stays — and force=true then releases every lease of
// it with a reset, dropping each record (DP-23).
func TestLease_Integration_ReleaseBranch_OneDirtyLeaseRefusesThenForceReleasesEvery(t *testing.T) {
	// Non-parallel: t.Chdir and a swap of http.DefaultTransport, as above.
	t.Chdir(t.TempDir())

	hqTarget, _, hqReleases := groupPoolOrchestratorReleasing(t, "hqpool", "3")
	webTarget, _, webReleases := groupPoolOrchestratorReleasing(t, "webpool", "7")
	orig := http.DefaultTransport
	http.DefaultTransport = orchestratorRoutes{targets: map[string]*url.URL{"orch.hqpool.zerops": hqTarget, "orch.webpool.zerops": webTarget}, next: orig}
	t.Cleanup(func() { http.DefaultTransport = orig })

	mock := defaultMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-hqpool", Name: "hqpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
			{ID: "svc-webpool", Name: "webpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
		}).
		WithServiceEnv("svc-hqpool", []platform.ServiceEnvVar{{ID: "env-h", Key: "API_TOKEN", Content: leaseIntToken, Sensitive: true}}).
		WithServiceEnv("svc-webpool", []platform.ServiceEnvVar{{ID: "env-w", Key: "API_TOKEN", Content: leaseIntToken, Sensitive: true}})
	ssh := &branchGuardSSH{dirty: map[string]bool{"webpool-7": true}}
	session, cleanup := setupTestServerWithDeploy(t, mock, defaultLogFetcher(), ssh)
	defer cleanup()

	res := callAndGetResult(t, session, "zerops_lease", map[string]any{"action": "acquire", "pools": []string{"hqpool", "webpool"}, "repo": "https://github.com/acme/app", "branch": "feat-x"})
	if text := getTextContent(t, res); res.IsError {
		t.Fatalf("group acquire: %s", text)
	}
	records := func() []string {
		t.Helper()
		entries, err := os.ReadDir(filepath.Join(".zcp", "state", "leases"))
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, e := range entries {
			if !e.IsDir() {
				out = append(out, e.Name())
			}
		}
		return out
	}

	res = callAndGetResult(t, session, "zerops_lease", map[string]any{"action": "release", "branch": "feat-x"})
	text := getTextContent(t, res)
	if !res.IsError {
		t.Fatalf("want the branch refused while webpool-7 holds an untracked file, got: %s", text)
	}
	for _, want := range []string{"PREREQUISITE_MISSING", "webpool-7", "notes.txt", "hqpool-3", "branch=feat-x force=true"} {
		if !strings.Contains(text, want) {
			t.Errorf("refusal %s lacks %q", text, want)
		}
	}
	if len(hqReleases())+len(webReleases()) != 0 {
		t.Errorf("a refused branch reached an orchestrator: hq %v, web %v", hqReleases(), webReleases())
	}
	if got := records(); len(got) != 2 {
		t.Errorf("records after a refused release = %v, want both kept", got)
	}

	res = callAndGetResult(t, session, "zerops_lease", map[string]any{"action": "release", "branch": "feat-x", "force": true})
	text = getTextContent(t, res)
	if res.IsError {
		t.Fatalf("forced release of the branch: %s", text)
	}
	if strings.Contains(text, leaseIntLease) || strings.Contains(text, leaseIntToken) {
		t.Fatalf("result carries a secret: %s", text)
	}
	var got struct {
		Leases []struct {
			Alias string `json:"alias"`
			State string `json:"state"`
		} `json:"leases"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	if len(got.Leases) != 2 || got.Leases[0].Alias != "hqpool-3" || got.Leases[1].Alias != "webpool-7" ||
		got.Leases[0].State != "released" || got.Leases[1].State != "released" {
		t.Errorf("forced release = %+v, want hqpool-3 and webpool-7 released", got.Leases)
	}
	if hq, web := hqReleases(), webReleases(); len(hq) != 1 || hq[0] != "true" || len(web) != 1 || web[0] != "true" {
		t.Errorf("releases hq %v, web %v; want one each with reset=true", hq, web)
	}
	if left := records(); len(left) != 0 {
		t.Errorf("records left after the forced release: %v", left)
	}
}

// idleLeaseSSH is devpool's leased containers as zcp reads them: each alias's
// checkout answers the activity read from answers; anything else is "ok".
type idleLeaseSSH struct {
	answers map[string]string
}

func (s idleLeaseSSH) ExecSSH(_ context.Context, hostname, command string) ([]byte, error) {
	if a, ok := s.answers[hostname]; ok && strings.Contains(command, "@@zcp-lease activity") {
		return []byte(a), nil
	}
	return []byte("ok"), nil
}

func (s idleLeaseSSH) ExecSSHBackground(ctx context.Context, hostname, command string, _ time.Duration) ([]byte, error) {
	return s.ExecSSH(ctx, hostname, command)
}

// wholeCheckoutRead is a checkout's whole answer to zcp's read: last changed
// changed, porcelain ("" for none) and unpushed commits, its supervisor
// listing activity and running no dev server on a preview port.
func wholeCheckoutRead(changed time.Time, porcelain string, unpushed int) string {
	if porcelain != "" {
		porcelain += "\n"
	}
	return fmt.Sprintf("@@zcp-lease activity %d \n@@zcp-lease status\n%s@@zcp-lease unpushed\n%d\n", changed.Unix(), porcelain, unpushed) +
		"@@zcp-lease preview\n" + `{"version":"v9.500.0","formats":[1],"capabilities":["env","activity"]}` + "\n@@zcp-devsup version 0\n" +
		`{"format":1,"servers":{"worker":{"status":"running"}}}` + "\n@@zcp-devsup status 0\n"
}

// TestLease_Integration_IdleStatusAndFullPoolByActivity: through the full MCP
// server, zerops_lease action=status marks the leases nothing has happened in
// for 24 h or more idle — the clean one with its release, the one with unsaved
// work with what it holds and never force — and a full pool's refusal lists
// zcp's leases oldest activity first, naming the release of the oldest clean
// one; neither changes anything on the pool (docs/spec-workflows.md §8 DP-25,
// DP-9).
func TestLease_Integration_IdleStatusAndFullPoolByActivity(t *testing.T) {
	// Non-parallel: t.Chdir and a swap of http.DefaultTransport, as above.
	t.Chdir(t.TempDir())
	var mutations []string
	var mu sync.Mutex
	full := poolOrchestrator(4, 4, map[int]bool{1: true, 2: true, 3: true, 4: true})
	routePools(t, map[string]http.Handler{"orch.devpool.zerops": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.URL.Path != "/container/acquire" {
			mu.Lock()
			mutations = append(mutations, r.Method+" "+r.URL.Path)
			mu.Unlock()
		}
		full.ServeHTTP(w, r)
	})})
	now := time.Now()
	acquired := now.Add(-200 * time.Hour).UTC().Format(time.RFC3339)
	if err := os.MkdirAll(filepath.Join(".zcp", "state", "leases"), 0o700); err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= 3; n++ {
		record := fmt.Sprintf(`{"poolHostname":"devpool","poolServiceId":"svc-pool","containerId":"c-%d","containerNumber":%d,`+
			`"containerHostname":"node-id-%d.runtime.devpool.zerops","lease":"%s","branch":"feat-%d","acquiredAt":"%s"}`, n, n, n, leaseIntLease, n, acquired)
		if err := os.WriteFile(filepath.Join(".zcp", "state", "leases", fmt.Sprintf("devpool-%d.json", n)), []byte(record), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ssh := idleLeaseSSH{answers: map[string]string{
		"devpool-1": wholeCheckoutRead(now.Add(-72*time.Hour), "", 0),
		"devpool-2": wholeCheckoutRead(now.Add(-time.Hour), "", 1),
		"devpool-3": wholeCheckoutRead(now.Add(-50*time.Hour), "?? notes.txt", 0),
	}}
	mock := defaultMock().
		WithServices([]platform.ServiceStack{intPool("svc-pool", "devpool", "ACTIVE", 4)}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{ID: "env-t", Key: "API_TOKEN", Content: leaseIntToken, Sensitive: true}})
	session, cleanup := setupTestServerWithDeploy(t, mock, defaultLogFetcher(), ssh)
	defer cleanup()

	res := callAndGetResult(t, session, "zerops_lease", map[string]any{"action": "status"})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("status failed: %s", text)
	}
	var got struct {
		Leases []struct {
			Alias string `json:"alias"`
			Idle  *struct {
				Since string `json:"since"`
				Next  string `json:"next"`
			} `json:"idle"`
		} `json:"leases"`
		NextActions string `json:"nextActions"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("status is not JSON: %v\n%s", err, text)
	}
	idle := map[string]string{}
	for _, l := range got.Leases {
		if l.Idle != nil {
			idle[l.Alias] = l.Idle.Next
		}
	}
	if len(idle) != 2 || !strings.Contains(idle["devpool-1"], "zerops_lease action=release lease=devpool-1") ||
		!strings.Contains(idle["devpool-3"], "?? notes.txt") || strings.Contains(idle["devpool-3"], "force") || strings.Contains(idle["devpool-3"], "action=release") {
		t.Errorf("idle = %v, want devpool-1 with its release and devpool-3 with its unsaved file, never a release", idle)
	}
	if !strings.Contains(got.NextActions, "devpool-1") || !strings.Contains(got.NextActions, "devpool-3") || !strings.Contains(got.NextActions, "ask the person") {
		t.Errorf("nextActions = %q, want the idle leases and to ask the person", got.NextActions)
	}

	res = callAndGetResult(t, session, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": "https://github.com/acme/app", "branch": "feat-9"})
	text = getTextContent(t, res)
	if !res.IsError {
		t.Fatalf("an acquire on a full pool succeeded: %s", text)
	}
	at := -1
	for _, want := range []string{"devpool-1 — idle, last active 3 d ago, nothing unsaved", "devpool-3 — idle, last active 2 d ago, 1 uncommitted or untracked file(s)", "devpool-2 — last active 1 h ago, 1 commit(s) on no remote branch"} {
		i := strings.Index(text, want)
		if i < at || i < 0 {
			t.Errorf("refusal does not list %q in oldest-activity order:\n%s", want, text)
		}
		at = i
	}
	if !strings.Contains(text, "zerops_lease action=release lease=devpool-1") || strings.Contains(text, "force") {
		t.Errorf("refusal should name devpool-1's release and never force:\n%s", text)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(mutations) != 0 {
		t.Errorf("status and the refusal changed the pool: %v", mutations)
	}
}
