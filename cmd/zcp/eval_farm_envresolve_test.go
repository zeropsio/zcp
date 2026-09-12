package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// Fake rig for TestFarmVerbs_UseTheResolvedLookup — a loopback REST server
// covering exactly the paths a `zcp eval farm status` run hits once
// ZCP_FARM_S3_* and ZCP_FARM_CLIENT_ID are missing from the environment and
// must be resolved from the farm/os services' env in ZCP_FARM_PROJECT_ID
// (docs/spec-eval-farm.md §3.1 FM-17): GET user/info, POST project/search
// (ListProjects), GET project/{id}/service-stack (the direct project read,
// once per hostname), GET service-stack/{id}/env (ops.FetchServiceEnv).
// The token belongs to two orgs and the farm project's is the second, as it
// is live: POST service-stack/search, scoped to the first org's clientId
// (GetUserInfo, D11), answers empty — so a resolver that reached for the
// search fails here exactly as it failed against the real API. The S3 side reuses newStatusFakeS3Server from
// eval_farm_run_test.go — same package, same shape.
// ---------------------------------------------------------------------------

const (
	envResolveProjectID     = "proj-envresolve-1"
	envResolveClientID      = "client-envresolve-1"
	envResolveFarmSvcID     = "svc-farm-envresolve-1"
	envResolveOSSvcID       = "svc-os-envresolve-1"
	envResolveResolvedS3Key = "resolved-sink-key"
)

// envResolveFakeAccount is the loopback fake described above.
type envResolveFakeAccount struct {
	t        *testing.T
	s3URL    string
	mu       sync.Mutex
	requests []string
}

func newEnvResolveFakeAccountServer(t *testing.T, s3URL string) (*httptest.Server, *envResolveFakeAccount) {
	t.Helper()
	f := &envResolveFakeAccount{t: t, s3URL: s3URL}
	srv := httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(srv.Close)
	return srv, f
}

func (f *envResolveFakeAccount) record(path string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, path)
}

func (f *envResolveFakeAccount) requestCount(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, p := range f.requests {
		if p == path {
			n++
		}
	}
	return n
}

func (f *envResolveFakeAccount) handle(w http.ResponseWriter, r *http.Request) {
	f.record(r.Method + " " + r.URL.Path)
	w.Header().Set("Content-Type", "application/json")

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/rest/public/user/info":
		fmt.Fprintf(w, `{"id":"user-1","email":"farm@example.com","fullName":"Farm","clientUserList":[{"id":"cu0","clientId":"client-other-org","userId":"user-1"},{"id":"cu1","clientId":%q,"userId":"user-1"}]}`, envResolveClientID)

	case r.Method == http.MethodPost && r.URL.Path == "/api/rest/public/project/search":
		fmt.Fprint(w, `{"limit":100,"offset":0,"totalHits":0,"items":[]}`)

	case r.Method == http.MethodGet && r.URL.Path == "/api/rest/public/project/"+envResolveProjectID+"/service-stack":
		items := []string{
			f.serviceStackJSON(envResolveFarmSvcID, "farm"),
			f.serviceStackJSON(envResolveOSSvcID, "os"),
		}
		fmt.Fprintf(w, `{"list":[%s],"totalCount":%d}`, strings.Join(items, ","), len(items))

	case r.Method == http.MethodPost && r.URL.Path == "/api/rest/public/service-stack/search":
		fmt.Fprint(w, `{"limit":1000,"offset":0,"totalHits":0,"items":[]}`)

	case r.Method == http.MethodGet && r.URL.Path == "/api/rest/public/service-stack/"+envResolveFarmSvcID+"/env":
		fmt.Fprintf(w, `{"items":[%s]}`, strings.Join([]string{
			f.envItemJSON("ZCP_FARM_S3_URL", "${os_apiUrl}"),
			f.envItemJSON("ZCP_FARM_S3_BUCKET", "${os_bucketName}"),
			f.envItemJSON("ZCP_FARM_S3_KEY", "${os_accessKeyId}"),
			f.envItemJSON("ZCP_FARM_S3_SECRET", "${os_secretAccessKey}"),
			f.envItemJSON("ZCP_FARM_CLIENT_ID", envResolveClientID),
		}, ","))

	case r.Method == http.MethodGet && r.URL.Path == "/api/rest/public/service-stack/"+envResolveOSSvcID+"/env":
		fmt.Fprintf(w, `{"items":[%s]}`, strings.Join([]string{
			f.envItemJSON("apiUrl", f.s3URL),
			f.envItemJSON("bucketName", "zcp-farm"), // matches statusFakeS3's hardcoded bucket (newStatusFakeS3ServerWithFake)
			f.envItemJSON("accessKeyId", envResolveResolvedS3Key),
			f.envItemJSON("secretAccessKey", "resolved-sink-secret"),
		}, ","))

	default:
		f.t.Errorf("envResolveFakeAccount: unexpected request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *envResolveFakeAccount) serviceStackJSON(id, name string) string {
	return fmt.Sprintf(
		`{"id":%q,"name":%q,"projectId":%q,"serviceStackTypeId":"runtime@1","serviceStackTypeInfo":{"serviceStackTypeVersionName":"v1","serviceStackTypeCategory":"CORE"},"status":"ACTIVE","subdomainAccess":false,"ports":[],"created":"2026-01-01T00:00:00Z","lastUpdate":"2026-01-01T00:00:00Z"}`,
		id, name, envResolveProjectID,
	)
}

func (f *envResolveFakeAccount) envItemJSON(key, content string) string {
	return fmt.Sprintf(`{"id":%q,"clientId":%q,"projectId":%q,"serviceStackId":%q,"key":%q,"content":%q,"type":"USER","sensitive":false,"created":"2026-01-01T00:00:00Z","lastUpdate":"2026-01-01T00:00:00Z"}`,
		"userdata-"+key, envResolveClientID, envResolveProjectID, envResolveFarmSvcID, key, content)
}

// TestFarmVerbs_UseTheResolvedLookup pins docs/spec-eval-farm.md §3.1 FM-17:
// with only ZCP_FARM_ACCOUNT_TOKEN, ZCP_API_HOST, and the non-secret
// ZCP_FARM_PROJECT_ID set — ZCP_FARM_S3_* and ZCP_FARM_CLIENT_ID entirely
// absent from the environment — `zcp eval farm status` resolves them from
// the farm/os services' env and reaches the bucket with the resolved
// config.
func TestFarmVerbs_UseTheResolvedLookup(t *testing.T) {
	s3Srv, s3Fake := newStatusFakeS3ServerWithFake(t)
	restSrv, f := newEnvResolveFakeAccountServer(t, s3Srv.URL)

	t.Setenv("ZCP_AUTHORING", "1")
	t.Setenv("ZCP_FARM_ACCOUNT_TOKEN", "farm-account-token")
	t.Setenv("ZCP_API_HOST", restSrv.URL)
	t.Setenv("ZCP_FARM_PROJECT_ID", envResolveProjectID)
	// Deliberately NOT set: ZCP_FARM_S3_URL/BUCKET/KEY/SECRET, ZCP_FARM_CLIENT_ID.

	var exitCode int
	_, stderr := captureOutput(t, func() {
		exitCode = runEvalFarm([]string{"status"})
	})

	if exitCode != 0 {
		t.Fatalf("runEvalFarm status exit code = %d, want 0 (stderr: %s)", exitCode, stderr)
	}

	// A successful exit already proves the sink call (ListBatches, over
	// the S3 fake) succeeded against the resolved ZCP_FARM_S3_* — an empty
	// or wrong URL/bucket would have failed the request and exited
	// nonzero. These counts additionally prove the resolution path itself
	// ran: both services were found by the direct project read (one per
	// hostname), never the search, and both envs fetched (one GET .../env
	// per service).
	if n := f.requestCount(http.MethodGet + " /api/rest/public/project/" + envResolveProjectID + "/service-stack"); n != 2 {
		t.Errorf("project service-stack reads = %d, want 2 (one per hostname: farm, os)", n)
	}
	if n := f.requestCount(http.MethodPost + " /api/rest/public/service-stack/search"); n != 0 {
		t.Errorf("service-stack/search requests = %d, want 0 — the search is scoped to the token's first org, not the farm's", n)
	}
	if n := f.requestCount(http.MethodGet + " /api/rest/public/service-stack/" + envResolveFarmSvcID + "/env"); n != 1 {
		t.Errorf("farm service env requests = %d, want 1", n)
	}
	if n := f.requestCount(http.MethodGet + " /api/rest/public/service-stack/" + envResolveOSSvcID + "/env"); n != 1 {
		t.Errorf("os service env requests = %d, want 1", n)
	}
	if n := f.requestCount(http.MethodPost + " /api/rest/public/project/search"); n != 1 {
		t.Errorf("project/search requests = %d, want 1 (ListProjects with the resolved ZCP_FARM_CLIENT_ID)", n)
	}

	s3Fake.mu.Lock()
	defer s3Fake.mu.Unlock()
	if s3Fake.objects == nil {
		t.Error("s3 fake was never initialized")
	}
}
