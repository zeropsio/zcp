package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm"
)

// TestEvalFarmRun_Detach_ReexecsAndPrintsLogPath pins §3.1 FM-18: `farm run
// --detach` re-execs this same binary (minus --detach, --batch pinned) with
// stdout/stderr redirected to <cwd>/farm-<batch>.log, and prints the batch
// id + log path — without actually daemonising: it calls runFarmRunDetach
// directly with a recording fake starter (R10a — an injected dependency,
// not a package-level mutable var), so no process is really forked.
func TestEvalFarmRun_Detach_ReexecsAndPrintsLogPath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	var gotArgv []string
	var gotLogPath string
	fakeStarter := func(argv []string, logPath string) error { //nolint:unparam // matches the runFarmRunDetach starter signature; this test only exercises the success path
		gotArgv = argv
		gotLogPath = logPath
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	stdout, _ := captureOutput(t, func() {
		exitCode := runFarmRunDetach([]string{
			"--candidate", "cand-sha", "--scenarios", "scen-sha", "--set", "gate",
			"--batch", "batch-detach-1", "--detach",
		}, "batch-detach-1", fakeStarter)
		if exitCode != 0 {
			t.Errorf("runFarmRunDetach exit code = %d, want 0", exitCode)
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

// TestEvalFarmRun_IntegrationTokenPreflight_NamesTheFix pins the brief's
// preflight message: when the run-token mint comes back 403 (the minting
// credential — ZCP_FARM_ACCOUNT_TOKEN — is itself an integration token
// without delegation), `zcp eval farm run` prints one line naming the exact
// fix and exits nonzero, rather than a generic "farm run: ..." error.
func TestEvalFarmRun_IntegrationTokenPreflight_NamesTheFix(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	t.Chdir(repoRoot)

	const clientID = "client-preflight-1"
	restSrv := newMintForbiddenFakeAccountServer(t, clientID)
	s3Srv, s3Fake := newStatusFakeS3ServerWithFake(t)

	scenarioBody, err := os.ReadFile(filepath.Join(repoRoot, "eval", "behavioral", "scenarios", "api-node-postgres-classic-dev.md"))
	if err != nil {
		t.Fatalf("read fixture scenario: %v", err)
	}
	s3Fake.mu.Lock()
	s3Fake.objects["scenarios/scen-sha/api-node-postgres-classic-dev.md"] = scenarioBody
	s3Fake.mu.Unlock()

	t.Setenv("ZCP_FARM_ACCOUNT_TOKEN", "integration-token-value")
	t.Setenv("ZCP_FARM_CLIENT_ID", clientID)
	t.Setenv("ZCP_API_HOST", restSrv.URL)
	t.Setenv("ZCP_FARM_S3_URL", s3Srv.URL)
	t.Setenv("ZCP_FARM_S3_BUCKET", "zcp-farm")
	t.Setenv("ZCP_FARM_S3_KEY", "sink-key")
	t.Setenv("ZCP_FARM_S3_SECRET", "sink-secret")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "oauth-farm-token")
	t.Setenv("ANTHROPIC_API_KEY", "")

	var exitCode int
	_, stderr := captureOutput(t, func() {
		exitCode = runFarmRun([]string{
			"--candidate", "cand-sha", "--scenarios", "scen-sha", "--evaluator", "eval-sha",
			"--set", "api-node-postgres-classic-dev", "--batch", "batch-preflight-1",
		})
	})

	if exitCode != 1 {
		t.Errorf("runFarmRun exit code = %d, want 1 (stderr: %s)", exitCode, stderr)
	}
	const want = "ZCP_FARM_ACCOUNT_TOKEN must be a personal access token: integration tokens cannot mint run tokens"
	if !strings.Contains(stderr, want) {
		t.Errorf("stderr = %q, want it to contain %q", stderr, want)
	}
}

// newMintForbiddenFakeAccountServer serves exactly the account-wide paths a
// single-run farm run exercises up to (and including) the token mint:
// user/info, project/import (create succeeds), and integration-token (mint
// always answers 403 notAllowedForIntegrationToken — simulating
// ZCP_FARM_ACCOUNT_TOKEN being an integration token). Also serves the
// per-project GET/DELETE the rollback (farm.Guard) issues after the mint
// fails.
func newMintForbiddenFakeAccountServer(t *testing.T, clientID string) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	projects := map[string]string{} // id -> name
	nextID := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/rest/public/user/info":
			fmt.Fprintf(w, `{"id":"user-1","email":"farm@example.com","fullName":"Farm","clientUserList":[{"id":"cu1","clientId":%q,"userId":"user-1"}]}`, clientID)

		case r.Method == http.MethodPost && r.URL.Path == "/api/rest/public/client/"+clientID+"/project/import":
			mu.Lock()
			nextID++
			id := fmt.Sprintf("proj-%d", nextID)
			name := farm.ProjectPrefix + "batch-preflight-1-api-node-postgres-classic-dev"
			projects[id] = name
			mu.Unlock()
			fmt.Fprintf(w, `{"projectId":%q,"projectName":%q,"serviceStacks":[]}`, id, name)

		case r.Method == http.MethodPost && r.URL.Path == "/api/rest/public/client/"+clientID+"/integration-token":
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"error":{"code":"notAllowedForIntegrationToken","message":"forbidden"}}`)

		case strings.HasPrefix(r.URL.Path, "/api/rest/public/project/"):
			id := strings.TrimPrefix(r.URL.Path, "/api/rest/public/project/")
			mu.Lock()
			name, ok := projects[id]
			mu.Unlock()
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			switch r.Method {
			case http.MethodGet:
				fmt.Fprintf(w, `{"id":%q,"name":%q,"status":"ACTIVE"}`, id, name)
			case http.MethodDelete:
				mu.Lock()
				delete(projects, id)
				mu.Unlock()
				fmt.Fprintf(w, `{"id":"proc-%s","status":"FINISHED"}`, id)
			default:
				t.Errorf("mintForbiddenFakeAccount: unexpected method %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}

		default:
			t.Errorf("mintForbiddenFakeAccount: unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
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

func newStatusFakeS3Server(t *testing.T) *httptest.Server {
	t.Helper()
	srv, _ := newStatusFakeS3ServerWithFake(t)
	return srv
}

// newStatusFakeS3ServerWithFake is newStatusFakeS3Server plus access to the
// underlying fake, for tests that need to seed bucket objects (e.g. an
// evaluators/current pointer or a scenario body) before calling farm run.
func newStatusFakeS3ServerWithFake(t *testing.T) (*httptest.Server, *statusFakeS3) {
	t.Helper()
	const bucket = "zcp-farm"
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
	return srv, f
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
	s3Srv := newStatusFakeS3Server(t)

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

// TestFarmRun_ZeroRunsCreated_ExitNonzero pins D10's zero-runs case: a
// --set that resolves to no scenario ids at all schedules and creates zero
// runs; RunBatch returns (nil-or-empty results, nil error), and `zcp eval
// farm run` must not read that as success — it prints "error: no run was
// created" to stderr and exits nonzero, matching every other
// not-fully-passed batch.
func TestFarmRun_ZeroRunsCreated_ExitNonzero(t *testing.T) {
	const clientID = "client-zero-runs"
	restSrv := newStatusFakeAccountServer(t, clientID, nil)
	s3Srv := newStatusFakeS3Server(t)

	t.Setenv("ZCP_FARM_ACCOUNT_TOKEN", "farm-account-token")
	t.Setenv("ZCP_FARM_CLIENT_ID", clientID)
	t.Setenv("ZCP_API_HOST", restSrv.URL)
	t.Setenv("ZCP_FARM_S3_URL", s3Srv.URL)
	t.Setenv("ZCP_FARM_S3_BUCKET", "zcp-farm")
	t.Setenv("ZCP_FARM_S3_KEY", "sink-key")
	t.Setenv("ZCP_FARM_S3_SECRET", "sink-secret")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "oauth-farm-token")
	t.Setenv("ANTHROPIC_API_KEY", "")

	var exitCode int
	_, stderr := captureOutput(t, func() {
		// A --set of comma-only separators resolves (resolveScenarios'
		// default branch, trimming and skipping empty entries) to zero
		// scenario ids without touching the bucket at all — RunBatch then
		// schedules and creates nothing.
		exitCode = runFarmRun([]string{
			"--candidate", "cand-sha", "--scenarios", "scen-sha", "--evaluator", "eval-sha",
			"--set", " , ,", "--batch", "batch-zero-runs",
		})
	})

	if exitCode != 1 {
		t.Errorf("runFarmRun exit code = %d, want 1 (stderr: %s)", exitCode, stderr)
	}
	const want = "error: no run was created"
	if !strings.Contains(stderr, want) {
		t.Errorf("stderr = %q, want it to contain %q", stderr, want)
	}
}

// newDeletingProjectFakeAccountServer serves user/info + project/search for
// exactly one project whose live status is DELETING — status must label
// its run "deleting", never "present" (a project mid async-delete is still
// in the list; live-verified 2026-09-10).
func newDeletingProjectFakeAccountServer(t *testing.T, clientID, projectName string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/rest/public/user/info":
			fmt.Fprintf(w, `{"id":"user-1","email":"farm@example.com","fullName":"Farm","clientUserList":[{"id":"cu1","clientId":%q,"userId":"user-1"}]}`, clientID)
		case r.Method == http.MethodPost && r.URL.Path == "/api/rest/public/project/search":
			fmt.Fprintf(w, `{"limit":100,"offset":0,"totalHits":1,"items":[{"id":"proj-1","clientId":%q,"name":%q,"status":"DELETING"}]}`, clientID, projectName)
		default:
			t.Errorf("deletingProjectFakeAccount: unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestFarmStatus_DeletingProject_LabelledDeleting pins the brief's minor
// fix: a project the platform reports status DELETING is labelled
// "deleting", not "present" — it is still in the live list (the async
// delete has not finished), so bare list-membership would misreport it.
func TestFarmStatus_DeletingProject_LabelledDeleting(t *testing.T) {
	const clientID = "client-deleting-1"
	runID := "deleting-run"
	projectName := farm.ProjectPrefix + runID
	restSrv := newDeletingProjectFakeAccountServer(t, clientID, projectName)
	s3Srv := newStatusFakeS3Server(t)

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

	batch := "batch-deleting-1"
	manifest := farm.BatchManifest{
		Batch: batch, Set: "gate",
		Runs: []farm.ManifestRun{
			{RunID: runID, Scenario: "recipe-a", ProjectName: projectName},
		},
	}
	if err := farm.PutManifest(ctx, sink, batch, manifest); err != nil {
		t.Fatalf("PutManifest: %v", err)
	}
	if err := sink.Put(ctx, "runs/"+runID+"/done.json", []byte(`{"runId":"`+runID+`"}`)); err != nil {
		t.Fatalf("Put done.json: %v", err)
	}
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
	if !strings.Contains(stdout, runID) || !strings.Contains(stdout, "project=deleting") {
		t.Errorf("stdout missing %q labelled project=deleting, got:\n%s", runID, stdout)
	}
	if strings.Contains(stdout, "project=present") {
		t.Errorf("stdout labelled the DELETING project present, got:\n%s", stdout)
	}
}

// TestFarmStatus_BudgetBlockedRun_ReportsSummaryVerdict pins the brief:
// once a batch has a summary.json, a run with no done.json is reported with
// the summary's verdict and detail plus how the batch ended, instead of the
// bare bucket-only "running" label that contradicts a settled batch (FM-9:
// the summary is evidence layered on top of the bucket+project recompute,
// not a replacement for it).
func TestFarmStatus_BudgetBlockedRun_ReportsSummaryVerdict(t *testing.T) {
	const clientID = "client-budget-1"
	runID := "budget-run"
	projectName := farm.ProjectPrefix + runID
	restSrv := newStatusFakeAccountServer(t, clientID, []string{projectName})
	s3Srv := newStatusFakeS3Server(t)

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

	batch := "batch-budget-1"
	manifest := farm.BatchManifest{
		Batch: batch, Set: "gate",
		Runs: []farm.ManifestRun{
			{RunID: runID, Scenario: "recipe-a", ProjectName: projectName},
		},
	}
	if err := farm.PutManifest(ctx, sink, batch, manifest); err != nil {
		t.Fatalf("PutManifest: %v", err)
	}
	// No done.json for runID: the batch ended by budget before this run
	// settled.
	summary := farm.BatchSummary{
		Batch: batch, FinishedAt: "2026-01-01T00:00:00Z", EndedBy: "budget",
		Runs: []farm.SummaryRun{
			{RunID: runID, Scenario: "recipe-a", Result: "blocked", Detail: "no bundle"},
		},
	}
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
	const want = "budget-run recipe-a blocked: no bundle (batch ended by budget) project=present"
	if !strings.Contains(stdout, want) {
		t.Errorf("stdout = %q, want it to contain %q", stdout, want)
	}
	if strings.Contains(stdout, "budget-run running") {
		t.Errorf("stdout still reported the summary-covered run as bare \"running\", got:\n%s", stdout)
	}
}

// TestFarmStatus_NoSummaryYet_ReportsRunning guards today's behaviour: a
// batch with no summary.json yet (still in flight) keeps reporting a
// done.json-less run as "running" — only a settled batch's summary
// overrides that label.
func TestFarmStatus_NoSummaryYet_ReportsRunning(t *testing.T) {
	const clientID = "client-inflight-1"
	runID := "inflight-run"
	projectName := farm.ProjectPrefix + runID
	restSrv := newStatusFakeAccountServer(t, clientID, []string{projectName})
	s3Srv := newStatusFakeS3Server(t)

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

	batch := "batch-inflight-1"
	manifest := farm.BatchManifest{
		Batch: batch, Set: "gate",
		Runs: []farm.ManifestRun{
			{RunID: runID, Scenario: "recipe-a", ProjectName: projectName},
		},
	}
	if err := farm.PutManifest(ctx, sink, batch, manifest); err != nil {
		t.Fatalf("PutManifest: %v", err)
	}
	// No summary.json at all: the batch has not ended yet.

	var exitCode int
	stdout, stderr := captureOutput(t, func() {
		exitCode = runFarmStatus(nil)
	})
	if exitCode != 0 {
		t.Errorf("runFarmStatus exit code = %d, want 0 (stderr: %s)", exitCode, stderr)
	}
	if !strings.Contains(stdout, "inflight-run") || !strings.Contains(stdout, "recipe-a running project=present") {
		t.Errorf("stdout = %q, want it to still report the in-flight run as \"running\"", stdout)
	}
}

// TestFarmRun_SetGate_ReadsListFromBucket pins outcome 2 of the S15 brief:
// `--set gate` reads the scenario id list from sets/<scenariosDigest>/gate.txt
// in the bucket, never from eval/farm/gate-set.txt on disk — nothing is
// staged to disk for this test.
func TestFarmRun_SetGate_ReadsListFromBucket(t *testing.T) {
	s3Srv, s3Fake := newStatusFakeS3ServerWithFake(t)
	t.Setenv("ZCP_FARM_S3_URL", s3Srv.URL)
	t.Setenv("ZCP_FARM_S3_BUCKET", "zcp-farm")
	t.Setenv("ZCP_FARM_S3_KEY", "sink-key")
	t.Setenv("ZCP_FARM_S3_SECRET", "sink-secret")

	const digest = "scen-gate-1"
	s3Fake.mu.Lock()
	s3Fake.objects["sets/"+digest+"/gate.txt"] = []byte("scenario-a\nscenario-b\n\nscenario-c\n")
	s3Fake.objects["scenarios/"+digest+"/scenario-a.md"] = bucketScenarioFixture(t, "scenario-a", "bootstrap")
	s3Fake.objects["scenarios/"+digest+"/scenario-b.md"] = bucketScenarioFixture(t, "scenario-b", "bootstrap")
	s3Fake.objects["scenarios/"+digest+"/scenario-c.md"] = bucketScenarioFixture(t, "scenario-c", "bootstrap")
	s3Fake.mu.Unlock()

	cfg, err := farm.ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv: %v", err)
	}
	sink := farm.NewSinkClient(cfg)

	scenarios, err := resolveScenarios(t.Context(), sink, digest, "gate")
	if err != nil {
		t.Fatalf("resolveScenarios: %v", err)
	}
	ids := make([]string, 0, len(scenarios))
	for _, s := range scenarios {
		ids = append(ids, s.ID)
	}
	want := []string{"scenario-a", "scenario-b", "scenario-c"}
	if len(ids) != len(want) {
		t.Fatalf("scenario ids = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Errorf("scenario ids = %v, want %v", ids, want)
			break
		}
	}
}

// TestFarmRun_LaunchFlag_FromBucketScenario pins outcome 2 of the S15 brief:
// scenarioIsLaunch reads scenarios/<digest>/<id>.md from the bucket and
// marks a scenario Launch when its front matter area starts with "launch"
// (here area: launch-production-recovery, one of the two gate-set launch
// scenarios' actual area values).
func TestFarmRun_LaunchFlag_FromBucketScenario(t *testing.T) {
	s3Srv, s3Fake := newStatusFakeS3ServerWithFake(t)
	t.Setenv("ZCP_FARM_S3_URL", s3Srv.URL)
	t.Setenv("ZCP_FARM_S3_BUCKET", "zcp-farm")
	t.Setenv("ZCP_FARM_S3_KEY", "sink-key")
	t.Setenv("ZCP_FARM_S3_SECRET", "sink-secret")

	const digest = "scen-launch-1"
	s3Fake.mu.Lock()
	s3Fake.objects["scenarios/"+digest+"/launch-scenario.md"] = bucketScenarioFixture(t, "launch-scenario", "launch-production-recovery")
	s3Fake.mu.Unlock()

	cfg, err := farm.ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv: %v", err)
	}
	sink := farm.NewSinkClient(cfg)

	scenarios, err := resolveScenarios(t.Context(), sink, digest, "launch-scenario")
	if err != nil {
		t.Fatalf("resolveScenarios: %v", err)
	}
	if len(scenarios) != 1 {
		t.Fatalf("scenarios = %v, want exactly one", scenarios)
	}
	if !scenarios[0].Launch {
		t.Errorf("scenarios[0].Launch = false, want true for area: launch-production-recovery")
	}
}

// TestFarmRun_EvaluatorPin_FlagOrCurrentPointer pins outcome 3 of the S15
// brief: --evaluator wins when given; otherwise evaluators/current is read;
// when neither is available, the error names both --evaluator and
// evaluators/current.
func TestFarmRun_EvaluatorPin_FlagOrCurrentPointer(t *testing.T) {
	s3Srv, s3Fake := newStatusFakeS3ServerWithFake(t)
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

	t.Run("flag wins over pointer", func(t *testing.T) {
		s3Fake.mu.Lock()
		s3Fake.objects["evaluators/current"] = []byte("pointer-sha")
		s3Fake.mu.Unlock()

		got, err := resolveEvaluatorSHA(ctx, sink, "flag-sha")
		if err != nil {
			t.Fatalf("resolveEvaluatorSHA: %v", err)
		}
		if got != "flag-sha" {
			t.Errorf("resolveEvaluatorSHA = %q, want %q", got, "flag-sha")
		}
	})

	t.Run("falls back to pointer", func(t *testing.T) {
		s3Fake.mu.Lock()
		s3Fake.objects["evaluators/current"] = []byte("pointer-sha\n")
		s3Fake.mu.Unlock()

		got, err := resolveEvaluatorSHA(ctx, sink, "")
		if err != nil {
			t.Fatalf("resolveEvaluatorSHA: %v", err)
		}
		if got != "pointer-sha" {
			t.Errorf("resolveEvaluatorSHA = %q, want %q (trimmed)", got, "pointer-sha")
		}
	})

	t.Run("neither given names both in the error", func(t *testing.T) {
		s3Fake.mu.Lock()
		delete(s3Fake.objects, "evaluators/current")
		s3Fake.mu.Unlock()

		_, err := resolveEvaluatorSHA(ctx, sink, "")
		if err == nil {
			t.Fatal("resolveEvaluatorSHA: want an error when neither --evaluator nor evaluators/current is available")
		}
		if !strings.Contains(err.Error(), "--evaluator") || !strings.Contains(err.Error(), "evaluators/current") {
			t.Errorf("error = %q, want it to name both --evaluator and evaluators/current", err.Error())
		}
	})
}

// bucketScenarioFixture builds a minimal valid scenario markdown body (id,
// area, and every field ParseScenario's validate() requires) for tests that
// seed the fake bucket directly rather than reading a real scenario off
// disk.
func bucketScenarioFixture(t *testing.T, id, area string) []byte {
	t.Helper()
	return fmt.Appendf(nil, `---
id: %s
area: %s
seed: empty
---
Test prompt body.
`, id, area)
}

// newHangingRunFakeAccountServer serves exactly the account-wide paths one
// non-launch run's full creation needs (user/info, project/import,
// integration-token mint, project/{id}/service-stack/import,
// project/{id}/process) — the process list stays empty forever, so the run
// never produces a FAILED creation-phase process and never gets a
// done.json: it hangs in waitForDone until something ends the batch (the
// RED test's SIGTERM, via runFarmRun's signal.NotifyContext wiring, R3).
func newHangingRunFakeAccountServer(t *testing.T, clientID, runProjectName string) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	projects := map[string]string{} // id -> name
	nextID := 0
	nextTok := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/rest/public/user/info":
			fmt.Fprintf(w, `{"id":"user-1","email":"farm@example.com","fullName":"Farm","clientUserList":[{"id":"cu1","clientId":%q,"userId":"user-1"}]}`, clientID)

		case r.Method == http.MethodPost && r.URL.Path == "/api/rest/public/client/"+clientID+"/project/import":
			mu.Lock()
			nextID++
			id := fmt.Sprintf("proj-%d", nextID)
			projects[id] = runProjectName
			mu.Unlock()
			fmt.Fprintf(w, `{"projectId":%q,"projectName":%q,"serviceStacks":[]}`, id, runProjectName)

		case r.Method == http.MethodPost && r.URL.Path == "/api/rest/public/client/"+clientID+"/integration-token":
			mu.Lock()
			nextTok++
			tokID := fmt.Sprintf("tok-%d", nextTok)
			mu.Unlock()
			fmt.Fprintf(w, `{"id":%q,"token":"run-secret-%s"}`, tokID, tokID)

		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/rest/public/project/") && strings.HasSuffix(r.URL.Path, "/service-stack/import"):
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/rest/public/project/"), "/service-stack/import")
			mu.Lock()
			name := projects[id]
			mu.Unlock()
			fmt.Fprintf(w, `{"projectId":%q,"projectName":%q,"serviceStacks":[{"id":"svc-1","name":"zcp"}]}`, id, name)

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/rest/public/project/") && strings.HasSuffix(r.URL.Path, "/process"):
			fmt.Fprint(w, `{"list":[],"totalCount":0}`)

		default:
			t.Errorf("hangingRunFakeAccount: unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestEvalFarmRun_SIGTERM_EndsBatchByInterrupt pins R3's end-to-end wiring:
// runFarmRun installs signal.NotifyContext for SIGTERM/os.Interrupt, so a
// real SIGTERM delivered to this process while a run is hung waiting for
// done.json stops the batch promptly (well inside the run's hour-long
// budget) instead of running it out, exits nonzero, and names the batch's
// summary key on stderr.
func TestEvalFarmRun_SIGTERM_EndsBatchByInterrupt(t *testing.T) {
	const clientID = "client-sigterm-1"
	const batch = "batch-sigterm-1"
	const scenarioID = "recipe-hang"
	runProjectName := farm.ProjectPrefix + batch + "-" + scenarioID

	restSrv := newHangingRunFakeAccountServer(t, clientID, runProjectName)
	s3Srv, s3Fake := newStatusFakeS3ServerWithFake(t)

	const digest = "scen-sigterm-1"
	s3Fake.mu.Lock()
	s3Fake.objects["scenarios/"+digest+"/"+scenarioID+".md"] = bucketScenarioFixture(t, scenarioID, "bootstrap")
	s3Fake.mu.Unlock()

	t.Setenv("ZCP_FARM_ACCOUNT_TOKEN", "farm-account-token")
	t.Setenv("ZCP_FARM_CLIENT_ID", clientID)
	t.Setenv("ZCP_API_HOST", restSrv.URL)
	t.Setenv("ZCP_FARM_S3_URL", s3Srv.URL)
	t.Setenv("ZCP_FARM_S3_BUCKET", "zcp-farm")
	t.Setenv("ZCP_FARM_S3_KEY", "sink-key")
	t.Setenv("ZCP_FARM_S3_SECRET", "sink-secret")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "oauth-farm-token")
	t.Setenv("ANTHROPIC_API_KEY", "")

	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
	}()

	var exitCode int
	start := time.Now()
	_, stderr := captureOutput(t, func() {
		exitCode = runFarmRun([]string{
			"--candidate", "cand-sha", "--scenarios", digest, "--evaluator", "eval-sha",
			"--set", scenarioID, "--batch", batch,
			"--run-budget", "1h",
		})
	})
	elapsed := time.Since(start)

	if elapsed > 5*time.Second {
		t.Fatalf("runFarmRun took %s, want it to stop promptly on SIGTERM, not run out the hour-long budget", elapsed)
	}
	if exitCode != 1 {
		t.Errorf("runFarmRun exit code = %d, want 1 (an interrupted run is never all-passed)", exitCode)
	}
	wantStderr := "batches/" + batch + "/summary.json"
	if !strings.Contains(stderr, "interrupted") || !strings.Contains(stderr, wantStderr) {
		t.Errorf("stderr = %q, want it to mention \"interrupted\" and name %q", stderr, wantStderr)
	}
}
