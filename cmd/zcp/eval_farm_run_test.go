package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/zeropsio/zcp/internal/eval/farm"
)

// TestEvalFarmRun_Detach_ReexecsAndPrintsLogPath pins §3.1 FM-18: `farm run
// --detach` re-execs this same binary (minus --detach, --batch pinned) with
// stdout/stderr redirected to <cwd>/farm-<batch>.log, and prints the batch
// id + log path — without actually daemonising: detachStarter is swapped
// for a recording fake, so no process is really forked.
func TestEvalFarmRun_Detach_ReexecsAndPrintsLogPath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	var gotArgv []string
	var gotLogPath string
	origStarter := detachStarter
	detachStarter = func(argv []string, logPath string) error {
		gotArgv = argv
		gotLogPath = logPath
		return nil
	}
	t.Cleanup(func() { detachStarter = origStarter })

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	stdout, _ := captureOutput(t, func() {
		exitCode := runFarmRun([]string{
			"--candidate", "cand-sha", "--scenarios", "scen-sha", "--set", "gate",
			"--batch", "batch-detach-1", "--detach",
		})
		if exitCode != 0 {
			t.Errorf("runFarmRun --detach exit code = %d, want 0", exitCode)
		}
	})

	// Resolve both sides fresh at comparison time (e.g. macOS's
	// /tmp -> /private/tmp) rather than assuming which of t.TempDir()'s
	// raw path or the production code's os.Getwd() ends up canonicalized.
	wantDir := dir
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		wantDir = resolved
	}
	gotDir := filepath.Dir(gotLogPath)
	if resolved, err := filepath.EvalSymlinks(gotDir); err == nil {
		gotDir = resolved
	}
	if gotDir != wantDir {
		t.Errorf("logPath dir = %q, want %q (raw logPath: %q)", gotDir, wantDir, gotLogPath)
	}
	if filepath.Base(gotLogPath) != "farm-batch-detach-1.log" {
		t.Errorf("logPath base = %q, want %q", filepath.Base(gotLogPath), "farm-batch-detach-1.log")
	}

	wantArgv := []string{
		exe, "eval", "farm", "run",
		"--candidate", "cand-sha", "--scenarios", "scen-sha", "--set", "gate",
		"--batch", "batch-detach-1",
	}
	if len(gotArgv) != len(wantArgv) {
		t.Fatalf("argv = %v, want %v", gotArgv, wantArgv)
	}
	for i := range wantArgv {
		if gotArgv[i] != wantArgv[i] {
			t.Errorf("argv[%d] = %q, want %q (full argv: %v)", i, gotArgv[i], wantArgv[i], gotArgv)
		}
	}
	for _, a := range gotArgv {
		if a == "--detach" {
			t.Errorf("re-exec argv still carries --detach: %v", gotArgv)
		}
	}

	if !strings.Contains(stdout, "batch-detach-1") {
		t.Errorf("stdout missing batch id, got: %q", stdout)
	}
	if !strings.Contains(stdout, gotLogPath) {
		t.Errorf("stdout missing the computed log path %q, got: %q", gotLogPath, stdout)
	}
}

// ---------------------------------------------------------------------------
// Minimal loopback fakes for TestEvalFarmStatus_RecomputesFromBucketAndProjects
// — this file is package main (cmd/zcp), so it cannot reach internal/eval/
// farm's own private test fakes (fakeAccount/fakeS3 in controller_test.go);
// these are the same shape, scoped to exactly the paths runFarmStatus hits,
// following cmd/zcp/eval_behavioral_cli_test.go's newFakeZeropsServer
// pattern (§ "Test rig").
// ---------------------------------------------------------------------------

type statusFakeAccount struct {
	clientID string
	projects []struct{ id, name string }
}

func newStatusFakeAccountServer(t *testing.T, clientID string, projectNames []string) *httptest.Server {
	t.Helper()
	f := &statusFakeAccount{clientID: clientID}
	for i, name := range projectNames {
		f.projects = append(f.projects, struct{ id, name string }{id: fmt.Sprintf("proj-%d", i+1), name: name})
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/rest/public/user/info":
			fmt.Fprintf(w, `{"id":"user-1","email":"farm@example.com","fullName":"Farm","clientUserList":[{"id":"cu1","clientId":%q,"userId":"user-1"}]}`, f.clientID)
		case r.Method == http.MethodPost && r.URL.Path == "/api/rest/public/project/search":
			items := make([]string, 0, len(f.projects))
			for _, p := range f.projects {
				items = append(items, fmt.Sprintf(`{"id":%q,"clientId":%q,"name":%q,"status":"ACTIVE"}`, p.id, f.clientID, p.name))
			}
			fmt.Fprintf(w, `{"limit":100,"offset":0,"totalHits":%d,"items":[%s]}`, len(items), strings.Join(items, ","))
		default:
			t.Errorf("statusFakeAccount: unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// statusFakeS3 is a minimal in-memory path-style S3 double covering exactly
// PUT/GET/HEAD/list-type=2 with no pagination — the small fixed dataset this
// test seeds never spans more than one page.
type statusFakeS3 struct {
	mu      sync.Mutex
	bucket  string
	objects map[string][]byte
}

func newStatusFakeS3Server(t *testing.T, bucket string) *httptest.Server {
	t.Helper()
	f := &statusFakeS3{bucket: bucket, objects: map[string][]byte{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefix := "/" + f.bucket
		if r.URL.Path == prefix && r.URL.Query().Get("list-type") == "2" {
			f.serveList(w, r)
			return
		}
		key := strings.TrimPrefix(r.URL.Path, prefix+"/")
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.Method {
		case http.MethodPut:
			body := make([]byte, r.ContentLength)
			_, _ = r.Body.Read(body)
			f.objects[key] = body
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			data, ok := f.objects[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write(data)
		case http.MethodHead:
			data, ok := f.objects[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (f *statusFakeS3) serveList(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	prefix := r.URL.Query().Get("prefix")
	var keys []string
	for key := range f.objects {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	f.mu.Unlock()

	var body strings.Builder
	body.WriteString("<ListBucketResult>")
	for _, key := range keys {
		fmt.Fprintf(&body, "<Contents><Key>%s</Key></Contents>", key)
	}
	body.WriteString("<IsTruncated>false</IsTruncated></ListBucketResult>")
	w.Header().Set("Content-Type", "application/xml")
	_, _ = w.Write([]byte(body.String()))
}

// TestEvalFarmStatus_RecomputesFromBucketAndProjects pins §3.2/§1.4: `farm
// status` recomputes entirely from the bucket listing (batches/,
// runs/*/done.json) and the live project list — never from a cached local
// state file. Two runs in one finished batch: one settled (done.json
// present, project still live) and one whose project has already been
// deleted (no local state records that fact — status must read it fresh
// from the live project list).
func TestEvalFarmStatus_RecomputesFromBucketAndProjects(t *testing.T) {
	const clientID = "client-status-1"
	restSrv := newStatusFakeAccountServer(t, clientID, []string{farm.ProjectPrefix + "done-run"})
	s3Srv := newStatusFakeS3Server(t, "zcp-farm")

	t.Setenv("ZCP_FARM_ACCOUNT_TOKEN", "farm-account-token")
	t.Setenv("ZCP_FARM_CLIENT_ID", clientID)
	t.Setenv("ZCP_API_HOST", restSrv.URL)
	t.Setenv("ZCP_FARM_S3_URL", s3Srv.URL)
	t.Setenv("ZCP_FARM_S3_BUCKET", "zcp-farm")
	t.Setenv("ZCP_FARM_S3_KEY", "sink-key")
	t.Setenv("ZCP_FARM_S3_SECRET", "sink-secret")

	cfg, err := farm.ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv: %v", err)
	}
	sink := farm.NewSinkClient(cfg)
	ctx := t.Context()

	batch := "batch-status-1"
	manifest := farm.BatchManifest{
		Batch: batch, Set: "gate",
		Runs: []farm.ManifestRun{
			{RunID: "done-run", Scenario: "recipe-a", ProjectName: farm.ProjectPrefix + "done-run"},
			{RunID: "gone-run", Scenario: "recipe-b", ProjectName: farm.ProjectPrefix + "gone-run"},
		},
	}
	if err := farm.PutManifest(ctx, sink, batch, manifest); err != nil {
		t.Fatalf("PutManifest: %v", err)
	}
	if err := sink.Put(ctx, "runs/done-run/done.json", []byte(`{"runId":"done-run"}`)); err != nil {
		t.Fatalf("Put done.json: %v", err)
	}
	// "gone-run" never gets a done.json AND its project is not in the fake
	// account's live list — status must report it as still running (no
	// bundle) with its project deleted, entirely from these two facts.
	summary := farm.BatchSummary{Batch: batch, FinishedAt: "2026-01-01T00:00:00Z", EndedBy: "settled"}
	if err := farm.PutSummary(ctx, sink, batch, summary); err != nil {
		t.Fatalf("PutSummary: %v", err)
	}

	var exitCode int
	stdout, stderr := captureOutput(t, func() {
		exitCode = runFarmStatus(nil)
	})
	if exitCode != 0 {
		t.Errorf("runFarmStatus exit code = %d, want 0 (stderr: %s)", exitCode, stderr)
	}

	if !strings.Contains(stdout, "done-run") || !strings.Contains(stdout, "done") || !strings.Contains(stdout, "project=present") {
		t.Errorf("stdout missing the settled+present run's line, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "gone-run") || !strings.Contains(stdout, "running") || !strings.Contains(stdout, "project=deleted") {
		t.Errorf("stdout missing the no-bundle+deleted run's line, got:\n%s", stdout)
	}
}
