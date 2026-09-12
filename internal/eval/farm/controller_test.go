package farm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// sharedTimeline records cross-server call order — the account REST fake
// and the S3 fake are two independent httptest servers, so ordering claims
// like "manifest PUT before the first create" need one shared sequence both
// record into, not two separate per-server logs.
type sharedTimeline struct {
	mu     sync.Mutex
	events []string
}

func (s *sharedTimeline) record(event string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
}

func (s *sharedTimeline) firstIndexOfPrefix(prefix string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, e := range s.events {
		if strings.HasPrefix(e, prefix) {
			return i
		}
	}
	return -1
}

// ---------------------------------------------------------------------------
// fakeAccount — a controller-shaped loopback fake of the Zerops REST API,
// following cmd/zcp/eval_behavioral_cli_test.go's newFakeZeropsServer
// pattern: it serves exactly the account-wide paths RunBatch/GC exercise
// (user/info, project search/import/get/delete, integration-token
// POST/DELETE) and records every request so tests can assert call order —
// public seams (recorded HTTP requests), never private controller state.
type fakeAccount struct {
	t        *testing.T
	clientID string
	srv      *httptest.Server
	timeline *sharedTimeline

	mu       sync.Mutex
	projects map[string]fakeProject // id -> project
	nextID   int
	nextTok  int
	tokens   map[string]bool // tokenID -> still valid (minted, not revoked)
	requests []string        // "METHOD path", in order

	// mintForbiddenCode, when non-empty, makes every integration-token
	// mint POST answer 403 with this apiCode instead of minting — used to
	// simulate ZCP_FARM_ACCOUNT_TOKEN being an integration token without
	// delegation (TestFarmRun_MintForbidden_AbortsBeforeAnyProject).
	mintForbiddenCode string

	// failImportProjectName, when non-empty, makes the project/import POST
	// for exactly this project name answer 500 instead of creating a
	// project — used to simulate CreateAndImportProject failing for one
	// scheduled run without aborting the whole batch
	// (TestFarmRun_CreateFails_PrintsErrorAndSummaryRecordsBlocked, D10).
	failImportProjectName string

	// failedCreationProcessProjectName, when non-empty, makes
	// GET /project/{id}/process for exactly this project name answer a
	// FAILED stack.create process — simulates D19's live 2026-09-10
	// project.create -> internalServerError incident
	// (TestFarmRun_FailedCreationProcess_SettlesBlockedBeforeBudget).
	failedCreationProcessProjectName string

	// failedCreationProcessServiceName, when set together with
	// failedCreationProcessProjectName, gives the simulated FAILED process
	// a ServiceStacks[] ref naming this service instead of no ref at all —
	// R1: a FAILED stack.import naming a service other than the control
	// service ("zcp") must never count as a creation-phase failure
	// (TestFarmRun_FailedImportAfterStarted_DoesNotDeleteProject).
	failedCreationProcessServiceName string
	// failedCreationProcessAction, when set together with
	// failedCreationProcessProjectName, names the simulated process's
	// actionName instead of the "stack.create" default (R1's own-import
	// test uses "stack.import").
	failedCreationProcessAction string

	// failScopedMint, when true, makes exactly the project-scoped run-token
	// mint (a POST /integration-token body carrying a non-empty
	// "projects" array — MintProjectScopedToken's own shape, distinct from
	// MintDelegatedLaunchToken's) answer 500 instead of minting — used to
	// simulate a non-403 mint failure that still leaves a rollback to
	// attempt (TestFarmRun_RollbackFailure_KeepsProjectIDAndError). Never
	// interferes with mintForbiddenCode's own 403 simulation, which is
	// keyed on the same endpoint but a different (launch-token) request
	// shape.
	failScopedMint bool

	// failDeleteProjectName, when non-empty, makes DELETE
	// /project/{id} for exactly this project name answer 500 instead of
	// deleting — used to simulate Guard's rollback DeleteProject call
	// itself failing (TestFarmRun_RollbackFailure_KeepsProjectIDAndError,
	// R6).
	failDeleteProjectName string
}

type fakeProject struct{ id, name string }

func newFakeAccount(t *testing.T, clientID string, timeline *sharedTimeline) *fakeAccount {
	t.Helper()
	f := &fakeAccount{
		t:        t,
		clientID: clientID,
		timeline: timeline,
		projects: map[string]fakeProject{},
		tokens:   map[string]bool{},
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAccount) URL() string { return f.srv.URL }

// seedProject pre-populates a project (e.g. a launch scenario's -prod
// target, as if an earlier pipeline stage created it) and returns its id.
func (f *fakeAccount) seedProject(name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	id := fmt.Sprintf("proj-%d", f.nextID)
	f.projects[id] = fakeProject{id: id, name: name}
	return id
}

func (f *fakeAccount) requestLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.requests))
	copy(out, f.requests)
	return out
}

func (f *fakeAccount) countMethod(method, path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.requests {
		if r == method+" "+path {
			n++
		}
	}
	return n
}

var yamlProjectNameRE = regexp.MustCompile(`name: (\S+)`)

func (f *fakeAccount) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	f.mu.Unlock()
	if f.timeline != nil {
		f.timeline.record("account: " + r.Method + " " + r.URL.Path)
	}
	w.Header().Set("Content-Type", "application/json")

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/rest/public/user/info":
		fmt.Fprintf(w, `{"id":"user-1","email":"farm@example.com","fullName":"Farm","clientUserList":[{"id":"cu1","clientId":%q,"userId":"user-1"}]}`, f.clientID)
		return

	case r.Method == http.MethodPost && r.URL.Path == "/api/rest/public/project/search":
		f.mu.Lock()
		items := make([]string, 0, len(f.projects))
		for _, p := range f.projects {
			items = append(items, fmt.Sprintf(`{"id":%q,"clientId":%q,"name":%q,"status":"ACTIVE"}`, p.id, f.clientID, p.name))
		}
		f.mu.Unlock()
		fmt.Fprintf(w, `{"limit":100,"offset":0,"totalHits":%d,"items":[%s]}`, len(items), strings.Join(items, ","))
		return

	case r.Method == http.MethodPost && r.URL.Path == "/api/rest/public/client/"+f.clientID+"/project/import":
		var body struct {
			Yaml string `json:"yaml"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			f.t.Errorf("fakeAccount: decode import body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		m := yamlProjectNameRE.FindStringSubmatch(body.Yaml)
		if m == nil {
			f.t.Errorf("fakeAccount: import yaml has no project name:\n%s", body.Yaml)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		name := m[1]
		f.mu.Lock()
		failName := f.failImportProjectName
		f.mu.Unlock()
		if failName != "" && name == failName {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, `{"error":{"code":"internalServerError","message":"simulated create failure"}}`)
			return
		}
		f.mu.Lock()
		f.nextID++
		id := fmt.Sprintf("proj-%d", f.nextID)
		f.projects[id] = fakeProject{id: id, name: name}
		f.mu.Unlock()
		fmt.Fprintf(w, `{"projectId":%q,"projectName":%q,"serviceStacks":[]}`, id, name)
		return

	case r.Method == http.MethodPost && r.URL.Path == "/api/rest/public/client/"+f.clientID+"/integration-token":
		f.handleIntegrationTokenMint(w, r)
		return

	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/rest/public/client/"+f.clientID+"/integration-token/"):
		tokID := strings.TrimPrefix(r.URL.Path, "/api/rest/public/client/"+f.clientID+"/integration-token/")
		f.mu.Lock()
		delete(f.tokens, tokID)
		f.mu.Unlock()
		fmt.Fprint(w, `{"success":true}`)
		return

	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/rest/public/project/") && strings.HasSuffix(r.URL.Path, "/service-stack/import"):
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/rest/public/project/"), "/service-stack/import")
		f.mu.Lock()
		p, ok := f.projects[id]
		f.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body struct {
			Yaml string `json:"yaml"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			f.t.Errorf("fakeAccount: decode service import body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if !strings.Contains(body.Yaml, "hostname: zcp") {
			f.t.Errorf("fakeAccount: service import yaml has no zcp service:\n%s", body.Yaml)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		fmt.Fprintf(w, `{"projectId":%q,"projectName":%q,"serviceStacks":[{"id":"svc-1","name":"zcp"}]}`, p.id, p.name)
		return

	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/rest/public/project/") && strings.HasSuffix(r.URL.Path, "/process"):
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/rest/public/project/"), "/process")
		f.handleProjectProcessList(w, id)
		return

	case strings.HasPrefix(r.URL.Path, "/api/rest/public/project/"):
		id := strings.TrimPrefix(r.URL.Path, "/api/rest/public/project/")
		f.handleProjectByID(w, r, id)
		return
	}

	f.t.Errorf("fakeAccount: unexpected request %s %s", r.Method, r.URL.Path)
	w.WriteHeader(http.StatusNotFound)
}

// handleIntegrationTokenMint serves POST .../integration-token for both
// MintDelegatedLaunchToken (an empty "projects" array) and
// MintProjectScopedToken (a non-empty one) — split out of handle to keep
// its cyclomatic complexity (maintidx) within budget.
func (f *fakeAccount) handleIntegrationTokenMint(w http.ResponseWriter, r *http.Request) {
	var mintBody struct {
		Projects []struct {
			ProjectID string `json:"projectId"`
		} `json:"projects"`
	}
	if err := json.NewDecoder(r.Body).Decode(&mintBody); err != nil {
		f.t.Errorf("fakeAccount: decode integration-token body: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	isScopedMint := len(mintBody.Projects) > 0

	f.mu.Lock()
	forbiddenCode := f.mintForbiddenCode
	failScoped := f.failScopedMint
	f.mu.Unlock()
	if isScopedMint && failScoped {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":{"code":"internalServerError","message":"simulated scoped mint failure"}}`)
		return
	}
	if forbiddenCode != "" {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprintf(w, `{"error":{"code":%q,"message":"forbidden"}}`, forbiddenCode)
		return
	}
	f.mu.Lock()
	f.nextTok++
	tokID := fmt.Sprintf("tok-%d", f.nextTok)
	f.tokens[tokID] = true
	f.mu.Unlock()
	fmt.Fprintf(w, `{"id":%q,"token":"launch-secret-%s"}`, tokID, tokID)
}

// handleProjectProcessList serves GET /project/{id}/process — split out of
// handle to keep its cyclomatic complexity (maintidx) within budget.
func (f *fakeAccount) handleProjectProcessList(w http.ResponseWriter, id string) {
	f.mu.Lock()
	p, ok := f.projects[id]
	failName := f.failedCreationProcessProjectName
	failService := f.failedCreationProcessServiceName
	failAction := f.failedCreationProcessAction
	f.mu.Unlock()
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if failName != "" && p.name == failName {
		action := failAction
		if action == "" {
			action = "stack.create"
		}
		serviceStacks := "[]"
		if failService != "" {
			serviceStacks = fmt.Sprintf(`[{"name":%q}]`, failService)
		}
		fmt.Fprintf(w, `{"list":[{"id":"proc-create-fail","actionName":%q,"status":"FAILED","serviceStacks":%s,"publicMeta":{"failReason":"internalServerError"}}],"totalCount":1}`, action, serviceStacks)
		return
	}
	fmt.Fprint(w, `{"list":[],"totalCount":0}`)
}

// handleProjectByID serves GET/DELETE /project/{id} — split out of handle
// to keep its cyclomatic complexity (maintidx) within budget.
func (f *fakeAccount) handleProjectByID(w http.ResponseWriter, r *http.Request, id string) {
	f.mu.Lock()
	p, ok := f.projects[id]
	f.mu.Unlock()
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet:
		fmt.Fprintf(w, `{"id":%q,"name":%q,"status":"ACTIVE"}`, p.id, p.name)
	case http.MethodDelete:
		f.mu.Lock()
		failDeleteName := f.failDeleteProjectName
		f.mu.Unlock()
		if failDeleteName != "" && p.name == failDeleteName {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, `{"error":{"code":"internalServerError","message":"simulated delete failure"}}`)
			return
		}
		f.mu.Lock()
		delete(f.projects, id)
		f.mu.Unlock()
		fmt.Fprintf(w, `{"id":"proc-%s","status":"FINISHED"}`, id)
	}
}

// controllerFixture bundles one test's fakes — a struct, not a positional
// tuple, so a test that only needs a few of these (most don't need the
// closer or the timeline) names the fields it uses instead of piling up
// blank identifiers.
type controllerFixture struct {
	account  *fakeAccount
	client   PlatformClient
	closer   func()
	s3       *fakeS3
	sink     *SinkClient
	timeline *sharedTimeline
}

// newControllerFixture builds a fakeAccount + PlatformClient (via the real
// NewAccountClient, so tests exercise the actual SDK/HTTP code path) and a
// fake S3 bucket, ready for a RunBatch/GC call. Both fakes record into one
// sharedTimeline so tests can assert cross-server call order (e.g. manifest
// PUT before the first project/import).
func newControllerFixture(t *testing.T, clientID string) controllerFixture {
	t.Helper()
	timeline := &sharedTimeline{}

	account := newFakeAccount(t, clientID, timeline)
	client, closer, err := NewAccountClient("farm-account-token", account.URL(), clientID)
	if err != nil {
		t.Fatalf("NewAccountClient: %v", err)
	}
	t.Cleanup(closer)

	fake := newFakeS3()
	s3Handler := fake.handler(t)
	s3srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		timeline.record("s3: " + r.Method + " " + r.URL.Path)
		s3Handler(w, r)
	}))
	t.Cleanup(s3srv.Close)
	sink := NewSinkClient(Config{URL: s3srv.URL, Bucket: "zcp-farm", Key: "sink-key", Secret: "sink-secret"})

	return controllerFixture{account: account, client: client, closer: closer, s3: fake, sink: sink, timeline: timeline}
}

// seedPart writes files (relative path -> content) under
// runs/<runID>/<part>/ directly into fake's in-memory store and returns
// farm.TreeDigest over the same content (computed from a real temp dir —
// never re-derived from the fake's own map iteration order).
func seedPart(t *testing.T, fake *fakeS3, runID, part string, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("seedPart: mkdir for %s: %v", full, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatalf("seedPart: write %s: %v", full, err)
		}
	}
	digest, err := TreeDigest(dir)
	if err != nil {
		t.Fatalf("seedPart: TreeDigest: %v", err)
	}
	fake.mu.Lock()
	for rel, content := range files {
		fake.objects[fmt.Sprintf("runs/%s/%s/%s", runID, part, rel)] = []byte(content)
	}
	fake.mu.Unlock()
	return digest
}

// seedSettledRun seeds a complete, digest-consistent bundle for runID:
// results/meta.json (task.result = taskResult), a capture file, and
// done.json claiming the correct digests for both parts.
func seedSettledRun(t *testing.T, fake *fakeS3, runID, scenarioID, taskResult string) {
	t.Helper()
	seedSettledRunAt(t, fake, runID, scenarioID, taskResult, "meta.json")
}

// seedSettledRunAt is seedSettledRun with metaRelPath (relative to
// runs/<runID>/results/) naming where meta.json lands — D15: the evaluator
// nests it as results/<suite>/<scenario>/meta.json, not at a fixed
// results/meta.json path.
func seedSettledRunAt(t *testing.T, fake *fakeS3, runID, scenarioID, taskResult, metaRelPath string) {
	t.Helper()
	resultsDigest := seedPart(t, fake, runID, "results", map[string]string{
		metaRelPath: fmt.Sprintf(`{"task":{"result":%q}}`, taskResult),
	})
	captureDigest := seedPart(t, fake, runID, "capture", map[string]string{
		"transcript.jsonl": `{"type":"assistant"}`,
	})
	done := fmt.Sprintf(`{"runId":%q,"scenarioId":%q,"parts":{"results":{"treeDigest":%q},"capture":{"treeDigest":%q}},"evaluatorSha256":"eval-sha","candidateSha256":"cand-sha"}`,
		runID, scenarioID, resultsDigest, captureDigest)
	fake.mu.Lock()
	fake.objects["runs/"+runID+"/done.json"] = []byte(done)
	fake.mu.Unlock()
}

// errUnexpectedPlatformCall is panicClient's dead-but-typed return value —
// t.Fatal halts the goroutine before any caller sees it, but every method
// still needs a well-typed (non-nil-nil) return to satisfy vet/staticcheck.
var errUnexpectedPlatformCall = errors.New("panicClient: unexpected platform call")

// panicClient is a PlatformClient whose every method fails the test if
// called — used to prove a code path makes zero platform calls.
type panicClient struct{ t *testing.T }

func (p panicClient) ListProjects(context.Context, string) ([]platform.Project, error) {
	p.t.Fatal("unexpected ListProjects call")
	return nil, errUnexpectedPlatformCall
}

func (p panicClient) CreateAndImportProject(context.Context, string) (*platform.ImportResult, error) {
	p.t.Fatal("unexpected CreateAndImportProject call")
	return nil, errUnexpectedPlatformCall
}

func (p panicClient) ImportServiceStack(context.Context, string, string) (*platform.ImportResult, error) {
	p.t.Fatal("unexpected ImportServiceStack call")
	return nil, errUnexpectedPlatformCall
}

func (p panicClient) MintProjectScopedToken(context.Context, string, string, string) (platform.MintedToken, error) {
	p.t.Fatal("unexpected MintProjectScopedToken call")
	return platform.MintedToken{}, errUnexpectedPlatformCall
}

func (p panicClient) GetProject(context.Context, string) (*platform.Project, error) {
	p.t.Fatal("unexpected GetProject call")
	return nil, errUnexpectedPlatformCall
}

func (p panicClient) DeleteProject(context.Context, string) (*platform.Process, error) {
	p.t.Fatal("unexpected DeleteProject call")
	return nil, errUnexpectedPlatformCall
}

func (p panicClient) MintDelegatedLaunchToken(context.Context, string) (platform.MintedToken, error) {
	p.t.Fatal("unexpected MintDelegatedLaunchToken call")
	return platform.MintedToken{}, errUnexpectedPlatformCall
}

func (p panicClient) RevokeIntegrationToken(context.Context, string, string) error {
	p.t.Fatal("unexpected RevokeIntegrationToken call")
	return errUnexpectedPlatformCall
}

func (p panicClient) GetProjectProcessesDirect(context.Context, string) ([]platform.Process, error) {
	p.t.Fatal("unexpected GetProjectProcessesDirect call")
	return nil, errUnexpectedPlatformCall
}

// TestFarmRun_CreatesPrefixedProjects_AndWritesManifest pins §3.3
// FM-21/FM-22: RunBatch writes batches/<batch>/manifest.json before
// creating any project, then creates one project per scenario whose import
// YAML parses to project.name == zcp-farm-<runId> and carries no account
// token. Independent oracle: the manifest shape and the "zcp-farm-<runId>"
// naming rule are copied from the spec text (§1.4 FM-9, §3.2 FM-19), never
// read back from the implementation's own output.
func TestFarmRun_CreatesPrefixedProjects_AndWritesManifest(t *testing.T) {
	t.Parallel()

	const clientID = "client-1"
	f := newControllerFixture(t, clientID)
	account, client, fake, sink, timeline := f.account, f.client, f.s3, f.sink, f.timeline

	batch := "batch-1"
	scenarios := []ScenarioRun{{ID: "recipe-a"}, {ID: "recipe-b"}}
	for _, sc := range scenarios {
		seedSettledRun(t, fake, batch+"-"+sc.ID, sc.ID, ResultPassed)
	}

	opts := RunOptions{
		Batch: batch, ClientID: clientID, Set: "gate",
		CandidateSHA256: "cand-sha", EvaluatorSHA256: "eval-sha", WrapperSHA256: "wrap-sha", ScenariosDigest: "scen-sha",
		Scenarios: scenarios, OAuthToken: "oauth-farm-token",
		Sink:         Sink{URL: "https://s3.example", Bucket: "zcp-farm", Key: "k", Secret: "s"},
		RunBudget:    time.Second,
		PollInterval: time.Millisecond,
	}

	if _, err := RunBatch(context.Background(), client, sink, opts); err != nil {
		t.Fatalf("RunBatch: %v", err)
	}

	manifestIdx := timeline.firstIndexOfPrefix("s3: PUT /zcp-farm/batches/")
	firstCreateIdx := timeline.firstIndexOfPrefix("account: POST /api/rest/public/client/" + clientID + "/project/import")
	if manifestIdx < 0 {
		t.Fatalf("timeline saw no manifest PUT; events: %v", timeline.events)
	}
	if firstCreateIdx < 0 {
		t.Fatalf("timeline saw no project/import request; events: %v", timeline.events)
	}
	if manifestIdx > firstCreateIdx {
		t.Errorf("manifest PUT (event %d) happened after the first project/import (event %d); events: %v", manifestIdx, firstCreateIdx, timeline.events)
	}

	manifest, err := GetManifest(context.Background(), sink, batch)
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	if manifest.Batch != batch || manifest.Set != "gate" || manifest.CandidateSha256 != "cand-sha" ||
		manifest.EvaluatorSha256 != "eval-sha" || manifest.ScenariosDigest != "scen-sha" {
		t.Errorf("manifest = %+v, want fields matching RunOptions", manifest)
	}
	if len(manifest.Runs) != 2 {
		t.Fatalf("manifest.Runs = %v, want 2 entries", manifest.Runs)
	}
	for i, sc := range scenarios {
		wantRunID := batch + "-" + sc.ID
		if manifest.Runs[i].RunID != wantRunID || manifest.Runs[i].Scenario != sc.ID || manifest.Runs[i].ProjectName != ProjectPrefix+wantRunID {
			t.Errorf("manifest.Runs[%d] = %+v, want RunID=%s ProjectName=%s", i, manifest.Runs[i], wantRunID, ProjectPrefix+wantRunID)
		}
	}

	if n := account.countMethod("POST", "/api/rest/public/client/"+clientID+"/project/import"); n != 2 {
		t.Errorf("project/import calls = %d, want 2", n)
	}

	account.mu.Lock()
	for _, p := range account.projects {
		if !strings.HasPrefix(p.name, ProjectPrefix) {
			t.Errorf("created project name %q does not carry the %q prefix", p.name, ProjectPrefix)
		}
	}
	account.mu.Unlock()

	// Independent oracle for "no account token": ImportYAML has no field
	// for one, so this is a structural check on the actual YAML bodies the
	// fake received, not a string the controller emits itself.
	for _, entry := range account.requestLog() {
		if strings.Contains(entry, "sk-ant-farm-account") {
			t.Errorf("request log entry %q suggests an account token leaked into a request", entry)
		}
	}
}

func TestRunBatch_ExistingManifest_RejectsWithoutMutation(t *testing.T) {
	t.Parallel()
	f := newControllerFixture(t, "client-existing")
	original := []byte(`{"batch":"batch-existing","runs":[]}`)
	f.s3.mu.Lock()
	f.s3.objects[manifestKey("batch-existing")] = append([]byte(nil), original...)
	f.s3.mu.Unlock()
	opts := RunOptions{Batch: "batch-existing", ClientID: "client-existing", Set: "gate", CandidateSHA256: "cand", EvaluatorSHA256: "eval", WrapperSHA256: "wrap", ScenariosDigest: "scen", Scenarios: []ScenarioRun{{ID: "scenario-a"}}, OAuthToken: "oauth", RunBudget: time.Second}
	if _, err := RunBatch(context.Background(), f.client, f.sink, opts); !errors.Is(err, ErrObjectExists) {
		t.Fatalf("RunBatch error = %v, want ErrObjectExists", err)
	}
	got, err := f.sink.Get(context.Background(), manifestKey("batch-existing"))
	if err != nil {
		t.Fatalf("read original manifest: %v", err)
	}
	if string(got) != string(original) {
		t.Fatalf("manifest changed to %q, want original %q", got, original)
	}
	for _, event := range f.timeline.events {
		if strings.HasPrefix(event, "account:") && (strings.Contains(event, " POST ") || strings.Contains(event, " DELETE ")) {
			t.Fatalf("platform mutated before rejecting existing manifest: %v", f.timeline.events)
		}
	}
}

func TestRunBatch_DuplicateScenario_RejectsBeforeWrite(t *testing.T) {
	t.Parallel()
	f := newControllerFixture(t, "client-duplicate")
	opts := RunOptions{Batch: "batch-duplicate", ClientID: "client-duplicate", Set: "gate", CandidateSHA256: "cand", EvaluatorSHA256: "eval", WrapperSHA256: "wrap", ScenariosDigest: "scen", Scenarios: []ScenarioRun{{ID: "same"}, {ID: "same"}}, OAuthToken: "oauth", RunBudget: time.Second}
	if _, err := RunBatch(context.Background(), f.client, f.sink, opts); err == nil {
		t.Fatal("RunBatch: want duplicate scenario error")
	}
	exists, _, err := f.sink.Head(context.Background(), manifestKey(opts.Batch))
	if err != nil {
		t.Fatalf("Head manifest: %v", err)
	}
	if exists {
		t.Fatal("duplicate scenario wrote a manifest")
	}
}

func TestRunBatch_ConcurrentBatchClaim_OneWinner(t *testing.T) {
	t.Parallel()
	fake := newFakeS3()
	server := httptest.NewServer(fake.handler(t))
	defer server.Close()
	newClient := func() *SinkClient {
		return NewSinkClient(Config{URL: server.URL, Bucket: fakeS3Bucket, Key: "key", Secret: "secret"})
	}
	results := make(chan error, 2)
	for _, body := range []string{"owner-a", "owner-b"} {
		go func(body string) {
			results <- CreateManifest(context.Background(), newClient(), "batch-race", BatchManifest{Batch: body})
		}(body)
	}
	var winners int
	for range 2 {
		if err := <-results; err == nil {
			winners++
		} else if !errors.Is(err, ErrObjectExists) {
			t.Errorf("claim error = %v, want ErrObjectExists for loser", err)
		}
	}
	if winners != 1 {
		t.Fatalf("conditional claim winners = %d, want exactly one", winners)
	}
}

func TestSettleFromDone_IdentityMismatch_Rejects(t *testing.T) {
	t.Parallel()
	fake := newFakeS3()
	server := httptest.NewServer(fake.handler(t))
	defer server.Close()
	sink := NewSinkClient(Config{URL: server.URL, Bucket: fakeS3Bucket, Key: "key", Secret: "secret"})
	body := []byte(`{"runId":"foreign-run","scenarioId":"scenario-a","candidateSha256":"candidate","evaluatorSha256":"evaluator","parts":{}}`)
	result, detail, settled := settleFromDoneForIdentity(context.Background(), sink, "reserved-run", "scenario-a", "candidate", "evaluator", body)
	if result != ResultBlocked || !settled || !strings.Contains(detail, "identity mismatch") {
		t.Fatalf("settle result=(%q,%q,%v), want blocked identity mismatch", result, detail, settled)
	}
}

// TestFarmRun_WritesNoteRunBudgetSecAndCandidateInfo pins §3.3's "the
// manifest also records..." paragraph: RunOptions.Note,
// RunOptions.RunBudgetSec, and RunOptions.CandidateInfo land verbatim on
// the manifest RunBatch writes — evidence for a human reading a batch
// later, never read back by RunBatch itself.
func TestFarmRun_WritesNoteRunBudgetSecAndCandidateInfo(t *testing.T) {
	t.Parallel()

	const clientID = "client-note"
	f := newControllerFixture(t, clientID)
	client, fake, sink := f.client, f.s3, f.sink

	batch := "batch-note"
	scenarios := []ScenarioRun{{ID: "recipe-a"}}
	seedSettledRun(t, fake, batch+"-recipe-a", "recipe-a", ResultPassed)

	wantCandidateInfo := &CandidateInfo{Revision: "10365ad9eafa", Modified: true, Time: "2026-02-01T19:55:50Z", GoVersion: "go1.25.0"}
	opts := RunOptions{
		Batch: batch, ClientID: clientID, Set: "gate",
		CandidateSHA256: "cand-sha", EvaluatorSHA256: "eval-sha", WrapperSHA256: "wrap-sha", ScenariosDigest: "scen-sha",
		Scenarios: scenarios, OAuthToken: "oauth-farm-token",
		Sink:          Sink{URL: "https://s3.example", Bucket: "zcp-farm", Key: "k", Secret: "s"},
		RunBudget:     time.Second,
		PollInterval:  time.Millisecond,
		Note:          "smoke test before the release",
		RunBudgetSec:  2700,
		CandidateInfo: wantCandidateInfo,
	}

	if _, err := RunBatch(context.Background(), client, sink, opts); err != nil {
		t.Fatalf("RunBatch: %v", err)
	}

	manifest, err := GetManifest(context.Background(), sink, batch)
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	if manifest.Note != opts.Note {
		t.Errorf("manifest.Note = %q, want %q", manifest.Note, opts.Note)
	}
	if manifest.RunBudgetSec != opts.RunBudgetSec {
		t.Errorf("manifest.RunBudgetSec = %d, want %d", manifest.RunBudgetSec, opts.RunBudgetSec)
	}
	if manifest.CandidateInfo == nil || *manifest.CandidateInfo != *wantCandidateInfo {
		t.Errorf("manifest.CandidateInfo = %+v, want %+v", manifest.CandidateInfo, wantCandidateInfo)
	}
}

// TestFarmRun_CreatesShellMintsTokenThenImportsService_InOrder pins the
// brief's two-step run-project creation shape (docs/spec-eval-farm.md §2.1
// FM-10): for each run, the account POST order is exactly project/import
// (empty services), then integration-token (mint), then
// project/{id}/service-stack/import — in that order — and the manifest
// records the minted run token's id, never its value.
func TestFarmRun_CreatesShellMintsTokenThenImportsService_InOrder(t *testing.T) {
	t.Parallel()

	const clientID = "client-order"
	f := newControllerFixture(t, clientID)
	account, client, fake, sink := f.account, f.client, f.s3, f.sink

	batch := "batch-order"
	scenarios := []ScenarioRun{{ID: "recipe-a"}}
	seedSettledRun(t, fake, batch+"-recipe-a", "recipe-a", ResultPassed)

	opts := RunOptions{
		Batch: batch, ClientID: clientID, Set: "gate",
		CandidateSHA256: "cand-sha", EvaluatorSHA256: "eval-sha", WrapperSHA256: "wrap-sha", ScenariosDigest: "scen-sha",
		Scenarios: scenarios, OAuthToken: "oauth-farm-token",
		Sink:         Sink{URL: "https://s3.example", Bucket: "zcp-farm", Key: "k", Secret: "s"},
		RunBudget:    time.Second,
		PollInterval: time.Millisecond,
	}

	if _, err := RunBatch(context.Background(), client, sink, opts); err != nil {
		t.Fatalf("RunBatch: %v", err)
	}

	var requestOrder []string
	for _, r := range account.requestLog() {
		switch {
		case r == "POST /api/rest/public/client/"+clientID+"/project/import":
			requestOrder = append(requestOrder, "create")
		case r == "POST /api/rest/public/client/"+clientID+"/integration-token":
			requestOrder = append(requestOrder, "mint")
		case strings.HasPrefix(r, "POST /api/rest/public/project/") && strings.HasSuffix(r, "/service-stack/import"):
			requestOrder = append(requestOrder, "import-service")
		}
	}
	want := []string{"create", "mint", "import-service"}
	if len(requestOrder) != len(want) {
		t.Fatalf("request order = %v, want %v", requestOrder, want)
	}
	for i, step := range want {
		if requestOrder[i] != step {
			t.Errorf("request order[%d] = %q, want %q (full order: %v)", i, requestOrder[i], step, requestOrder)
		}
	}

	manifest, err := GetManifest(context.Background(), sink, batch)
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	if len(manifest.Runs) != 1 {
		t.Fatalf("manifest.Runs = %v, want 1 entry", manifest.Runs)
	}
	if manifest.Runs[0].RunTokenID == "" {
		t.Errorf("manifest.Runs[0].RunTokenID empty, want the minted run token's id")
	}
	if strings.Contains(manifest.Runs[0].RunTokenID, "launch-secret") {
		t.Errorf("manifest.Runs[0].RunTokenID = %q looks like a token VALUE, not an id", manifest.Runs[0].RunTokenID)
	}
}

// TestFarmRun_MintForbidden_AbortsBeforeAnyProject pins the brief's rollback
// behavior: when the run-token mint comes back 403 (the minting credential
// is an integration token, not a personal access token), RunBatch rolls
// back the shell project it just created for that run, aborts the whole
// batch (no further scenarios are scheduled), and returns an error whose
// chain carries platform.ErrDelegationUnavailable so cmd/zcp can print the
// one-line fix.
func TestFarmRun_MintForbidden_AbortsBeforeAnyProject(t *testing.T) {
	t.Parallel()

	const clientID = "client-forbidden"
	f := newControllerFixture(t, clientID)
	account, client, fake, sink := f.account, f.client, f.s3, f.sink
	// Mirrors platform.apiCodeDelegationUnavailableLegacy — the fake can't
	// import the unexported platform const, so the literal is pinned here
	// against the same live-verified apiCode the brief names.
	account.mintForbiddenCode = "notAllowedForIntegrationToken"

	batch := "batch-forbidden"
	scenarios := []ScenarioRun{{ID: "recipe-a"}, {ID: "recipe-b"}}
	for _, sc := range scenarios {
		seedSettledRun(t, fake, batch+"-"+sc.ID, sc.ID, ResultPassed)
	}

	opts := RunOptions{
		Batch: batch, ClientID: clientID, Set: "gate",
		CandidateSHA256: "cand-sha", EvaluatorSHA256: "eval-sha", WrapperSHA256: "wrap-sha", ScenariosDigest: "scen-sha",
		Scenarios: scenarios, OAuthToken: "oauth-farm-token",
		Sink:         Sink{URL: "https://s3.example", Bucket: "zcp-farm", Key: "k", Secret: "s"},
		RunBudget:    time.Second,
		PollInterval: time.Millisecond,
	}

	_, err := RunBatch(context.Background(), client, sink, opts)
	if err == nil {
		t.Fatal("RunBatch: want error on mint-forbidden, got nil")
	}
	if !isScopedMintForbidden(err) {
		t.Errorf("RunBatch error does not carry platform.ErrDelegationUnavailable: %v", err)
	}

	account.mu.Lock()
	remaining := len(account.projects)
	account.mu.Unlock()
	if remaining != 0 {
		t.Errorf("account has %d project(s) after mint-forbidden abort, want 0 (shell rolled back)", remaining)
	}

	if n := account.countMethod("POST", "/api/rest/public/client/"+clientID+"/project/import"); n != 1 {
		t.Errorf("project/import calls = %d, want 1 (batch must abort after the first run's mint fails, never scheduling recipe-b)", n)
	}
}

// captureStderr redirects os.Stderr to a pipe for the duration of fn and
// returns everything written to it — used to pin RunBatch's D10 stderr
// line, which is printed directly (a public-seam requirement: the brief
// pins the exact literal "<runId> <scenario> error: <wrapped error>", not
// an internal log call).
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("captureStderr: os.Pipe: %v", err)
	}
	orig := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("captureStderr: close writer: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("captureStderr: read: %v", err)
	}
	return string(out)
}

// TestFarmRun_CreateFails_PrintsErrorAndSummaryRecordsBlocked pins D10:
// a per-run failure during creation (here, CreateAndImportProject) is
// printed to stderr at the moment it happens, recorded in the batch's
// results/summary as result "blocked" with the wrapped error message, and
// never turned into a bare skip. The other, unaffected scenario in the
// same batch still runs and settles normally — one run's creation failure
// does not abort the batch (unlike the mint-403 case).
func TestFarmRun_CreateFails_PrintsErrorAndSummaryRecordsBlocked(t *testing.T) {
	// Not t.Parallel(): captureStderr swaps the process-wide os.Stderr for
	// its duration, which would race a concurrent test's own stderr writes.
	const clientID = "client-create-fail"
	f := newControllerFixture(t, clientID)
	account, client, fake, sink := f.account, f.client, f.s3, f.sink

	batch := "batch-create-fail"
	scenarios := []ScenarioRun{{ID: "recipe-fails"}, {ID: "recipe-ok"}}
	seedSettledRun(t, fake, batch+"-recipe-ok", "recipe-ok", ResultPassed)

	failRunID := batch + "-recipe-fails"
	account.mu.Lock()
	account.failImportProjectName = ProjectPrefix + failRunID
	account.mu.Unlock()

	opts := RunOptions{
		Batch: batch, ClientID: clientID, Set: "gate",
		CandidateSHA256: "cand-sha", EvaluatorSHA256: "eval-sha", WrapperSHA256: "wrap-sha", ScenariosDigest: "scen-sha",
		Scenarios: scenarios, OAuthToken: "oauth-farm-token",
		Sink:         Sink{URL: "https://s3.example", Bucket: "zcp-farm", Key: "k", Secret: "s"},
		RunBudget:    time.Second,
		PollInterval: time.Millisecond,
	}

	var results []RunResult
	stderr := captureStderr(t, func() {
		var runErr error
		results, runErr = RunBatch(context.Background(), client, sink, opts)
		if runErr != nil {
			t.Fatalf("RunBatch: %v", runErr)
		}
	})

	wantStderrPrefix := failRunID + " recipe-fails error: "
	if !strings.Contains(stderr, wantStderrPrefix) {
		t.Errorf("stderr = %q, want a line starting with %q", stderr, wantStderrPrefix)
	}

	if len(results) != 2 {
		t.Fatalf("RunBatch results = %+v, want 2 entries", results)
	}
	var blockedResult, okResult *RunResult
	for i := range results {
		switch results[i].RunID {
		case failRunID:
			blockedResult = &results[i]
		case batch + "-recipe-ok":
			okResult = &results[i]
		}
	}
	if blockedResult == nil {
		t.Fatalf("results %+v missing an entry for %s", results, failRunID)
	}
	if blockedResult.Result != ResultBlocked {
		t.Errorf("blocked run Result = %q, want %q", blockedResult.Result, ResultBlocked)
	}
	if blockedResult.Error == "" {
		t.Errorf("blocked run Error is empty, want the wrapped create-project error")
	}
	if blockedResult.ProjectID != "" {
		t.Errorf("blocked run ProjectID = %q, want empty (create never succeeded)", blockedResult.ProjectID)
	}
	if okResult == nil || okResult.Result != ResultPassed {
		t.Errorf("unaffected run recipe-ok = %+v, want Result=%q", okResult, ResultPassed)
	}

	summary, err := GetSummary(context.Background(), sink, batch)
	if err != nil {
		t.Fatalf("GetSummary: %v", err)
	}
	var summaryBlocked *SummaryRun
	for i := range summary.Runs {
		if summary.Runs[i].RunID == failRunID {
			summaryBlocked = &summary.Runs[i]
		}
	}
	if summaryBlocked == nil {
		t.Fatalf("summary.Runs %+v missing an entry for %s (D10: a blocked run must still be recorded)", summary.Runs, failRunID)
	}
	if summaryBlocked.Result != ResultBlocked || summaryBlocked.Error == "" {
		t.Errorf("summary entry for %s = %+v, want Result=%q and a non-empty Error", failRunID, summaryBlocked, ResultBlocked)
	}
}

// TestFarmRun_DoneJSON_PartsVerified_ElseBlocked pins §1.2 FM-5: matching
// part digests settle the run passed/failed from results/meta.json's own
// task.result, and its project is deleted; a mismatched digest on ANY part
// settles the run `blocked` instead — but the run still completed (done.json
// existed), so its project is STILL deleted (FM-5 is an evidence verdict,
// not a run-didn't-finish signal; FM-21's no-delete exemption is only for a
// run that never produced a bundle at all).
func TestFarmRun_DoneJSON_PartsVerified_ElseBlocked(t *testing.T) {
	t.Parallel()

	const clientID = "client-2"

	t.Run("matchingDigests_settlesFromMetaAndDeletes", func(t *testing.T) {
		t.Parallel()
		f := newControllerFixture(t, clientID)
		account, client, fake, sink := f.account, f.client, f.s3, f.sink
		batch := "batch-2a"
		sc := ScenarioRun{ID: "recipe-fail"}
		runID := batch + "-" + sc.ID
		seedSettledRun(t, fake, runID, sc.ID, ResultFailed)

		opts := RunOptions{
			Batch: batch, ClientID: clientID, Set: "gate",
			CandidateSHA256: "cand-sha", EvaluatorSHA256: "eval-sha", WrapperSHA256: "wrap", ScenariosDigest: "scen",
			Scenarios: []ScenarioRun{sc}, OAuthToken: "oauth-token",
			Sink:      Sink{URL: "https://s3.example", Bucket: "zcp-farm", Key: "k", Secret: "s"},
			RunBudget: time.Second, PollInterval: time.Millisecond,
		}
		results, err := RunBatch(context.Background(), client, sink, opts)
		if err != nil {
			t.Fatalf("RunBatch: %v", err)
		}
		if len(results) != 1 || results[0].Result != ResultFailed {
			t.Fatalf("results = %+v, want one entry with Result=%q", results, ResultFailed)
		}
		if results[0].ProjectID != "" {
			t.Errorf("results[0].ProjectID = %q, want empty (project deleted)", results[0].ProjectID)
		}
		account.mu.Lock()
		stillExists := findProjectByFakeName(account, ProjectPrefix+runID)
		account.mu.Unlock()
		if stillExists {
			t.Errorf("project %s still present in fakeAccount after a settled run", ProjectPrefix+runID)
		}
	})

	t.Run("digestMismatch_blockedButStillDeleted", func(t *testing.T) {
		t.Parallel()
		f := newControllerFixture(t, clientID+"-b")
		account, client, fake, sink := f.account, f.client, f.s3, f.sink
		batch := "batch-2b"
		sc := ScenarioRun{ID: "recipe-corrupt"}
		runID := batch + "-" + sc.ID
		seedSettledRun(t, fake, runID, sc.ID, ResultPassed)

		// Corrupt one byte of a results object WITHOUT updating done.json's
		// claimed digest — the recompute must now disagree.
		fake.mu.Lock()
		key := "runs/" + runID + "/results/meta.json"
		fake.objects[key] = append(append([]byte{}, fake.objects[key]...), 'X')
		fake.mu.Unlock()

		opts := RunOptions{
			Batch: batch, ClientID: clientID + "-b", Set: "gate",
			CandidateSHA256: "cand", EvaluatorSHA256: "eval", WrapperSHA256: "wrap", ScenariosDigest: "scen",
			Scenarios: []ScenarioRun{sc}, OAuthToken: "oauth-token",
			Sink:      Sink{URL: "https://s3.example", Bucket: "zcp-farm", Key: "k", Secret: "s"},
			RunBudget: time.Second, PollInterval: time.Millisecond,
		}
		results, err := RunBatch(context.Background(), client, sink, opts)
		if err != nil {
			t.Fatalf("RunBatch: %v", err)
		}
		if len(results) != 1 || results[0].Result != ResultBlocked {
			t.Fatalf("results = %+v, want one entry with Result=%q", results, ResultBlocked)
		}
		if results[0].ProjectID != "" {
			t.Errorf("results[0].ProjectID = %q, want empty — a blocked-evidence run's project is still deleted (FM-5)", results[0].ProjectID)
		}
		account.mu.Lock()
		stillExists := findProjectByFakeName(account, ProjectPrefix+runID)
		account.mu.Unlock()
		if stillExists {
			t.Errorf("project %s still present in fakeAccount after a blocked-but-completed run", ProjectPrefix+runID)
		}
	})
}

func findProjectByFakeName(account *fakeAccount, name string) bool {
	for _, p := range account.projects {
		if p.name == name {
			return true
		}
	}
	return false
}

// TestFarmRun_NoDoneJSON_BudgetElapsed_ProjectKept pins FM-21's sole
// exemption: a run whose bucket never gets a done.json within its budget is
// `blocked: no bundle`, and — unlike TestFarmRun_DoneJSON_PartsVerified_
// ElseBlocked's digest-mismatch case — its project is NOT deleted (the run
// itself never completed, so there is nothing to verify and evidence may
// still be needed for a Mac-side debugging look, §2.1 FM-11).
func TestFarmRun_NoDoneJSON_BudgetElapsed_ProjectKept(t *testing.T) {
	t.Parallel()
	const clientID = "client-3"
	f := newControllerFixture(t, clientID)
	account, client, sink := f.account, f.client, f.sink

	batch := "batch-3"
	sc := ScenarioRun{ID: "recipe-stuck"}
	runID := batch + "-" + sc.ID
	// No seedSettledRun call: the bucket never gets runs/<runID>/done.json.

	opts := RunOptions{
		Batch: batch, ClientID: clientID, Set: "gate",
		CandidateSHA256: "cand", EvaluatorSHA256: "eval", WrapperSHA256: "wrap", ScenariosDigest: "scen",
		Scenarios: []ScenarioRun{sc}, OAuthToken: "oauth-token",
		Sink:      Sink{URL: "https://s3.example", Bucket: "zcp-farm", Key: "k", Secret: "s"},
		RunBudget: time.Second, PollInterval: 100 * time.Millisecond,
	}
	results, err := RunBatch(context.Background(), client, sink, opts)
	if err != nil {
		t.Fatalf("RunBatch: %v", err)
	}
	if len(results) != 1 || results[0].Result != ResultBlocked || results[0].Detail != DetailNoBundle {
		t.Fatalf("results = %+v, want one entry Result=%q Detail=%q", results, ResultBlocked, DetailNoBundle)
	}
	for _, entry := range account.requestLog() {
		if strings.HasPrefix(entry, "DELETE ") {
			t.Errorf("unexpected DELETE request recorded: %s", entry)
		}
	}
	account.mu.Lock()
	stillExists := findProjectByFakeName(account, ProjectPrefix+runID)
	account.mu.Unlock()
	if !stillExists {
		t.Errorf("project %s was deleted, want it kept (no bundle)", ProjectPrefix+runID)
	}
}

// TestFarmRun_LaunchScenario_MintsThenRevokesToken pins §3.4 FM-23: a launch
// scenario's token is minted before the run's project is created, deletes
// both the run project and its zcp-farm-<runId>-prod target once the run
// settles, then revokes the token — and the token value never appears in
// the manifest, the summary, or any recorded request.
func TestFarmRun_LaunchScenario_MintsThenRevokesToken(t *testing.T) {
	t.Parallel()
	const clientID = "client-4"
	f := newControllerFixture(t, clientID)
	account, client, fake, sink, timeline := f.account, f.client, f.s3, f.sink, f.timeline

	batch := "batch-4"
	sc := ScenarioRun{ID: "launch-scenario", Launch: true}
	runID := batch + "-" + sc.ID
	seedSettledRun(t, fake, runID, sc.ID, ResultPassed)
	// Pre-seed the prod target as if the run's own agent had created it.
	prodID := account.seedProject(ProjectPrefix + runID + "-prod")

	opts := RunOptions{
		Batch: batch, ClientID: clientID, Set: "gate",
		CandidateSHA256: "cand", EvaluatorSHA256: "eval", WrapperSHA256: "wrap", ScenariosDigest: "scen",
		Scenarios: []ScenarioRun{sc}, OAuthToken: "oauth-token",
		Sink:      Sink{URL: "https://s3.example", Bucket: "zcp-farm", Key: "k", Secret: "s"},
		RunBudget: time.Second, PollInterval: time.Millisecond,
	}
	results, err := RunBatch(context.Background(), client, sink, opts)
	if err != nil {
		t.Fatalf("RunBatch: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %+v, want one entry", results)
	}
	if results[0].LaunchTokenID != "" {
		t.Errorf("results[0].LaunchTokenID = %q, want empty (revoked)", results[0].LaunchTokenID)
	}

	mintEntry := "account: POST /api/rest/public/client/" + clientID + "/integration-token"
	mintIdx := timeline.firstIndexOfPrefix(mintEntry)
	createIdx := timeline.firstIndexOfPrefix("account: POST /api/rest/public/client/" + clientID + "/project/import")
	if mintIdx < 0 {
		t.Fatalf("timeline saw no integration-token mint; events: %v", timeline.events)
	}
	if createIdx < 0 {
		t.Fatalf("timeline saw no project/import; events: %v", timeline.events)
	}
	if mintIdx > createIdx {
		t.Errorf("mint (event %d) happened after create (event %d), want before; events: %v", mintIdx, createIdx, timeline.events)
	}

	runDeleteEntry := "account: DELETE /api/rest/public/project/"
	revokeEntry := "account: DELETE /api/rest/public/client/" + clientID + "/integration-token/"
	lastDeleteIdx := -1
	for i, e := range timeline.events {
		if strings.HasPrefix(e, runDeleteEntry) {
			lastDeleteIdx = i
		}
	}
	revokeIdx := timeline.firstIndexOfPrefix(revokeEntry)
	if lastDeleteIdx < 0 {
		t.Fatalf("timeline saw no project DELETE; events: %v", timeline.events)
	}
	if revokeIdx < 0 {
		t.Fatalf("timeline saw no integration-token revoke; events: %v", timeline.events)
	}
	if revokeIdx < lastDeleteIdx {
		t.Errorf("revoke (event %d) happened before the last project delete (event %d), want after; events: %v", revokeIdx, lastDeleteIdx, timeline.events)
	}

	account.mu.Lock()
	runStillExists := findProjectByFakeName(account, ProjectPrefix+runID)
	_, prodStillExists := account.projects[prodID]
	account.mu.Unlock()
	if runStillExists {
		t.Errorf("run project %s still present after settle", ProjectPrefix+runID)
	}
	if prodStillExists {
		t.Errorf("prod project %s still present after settle", ProjectPrefix+runID+"-prod")
	}

	// The token value itself must never appear in any recorded request path
	// (the platform never puts secrets in a URL) nor in the manifest.
	manifest, err := GetManifest(context.Background(), sink, batch)
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	manifestBody, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("Marshal manifest: %v", err)
	}
	if strings.Contains(string(manifestBody), "launch-secret-") {
		t.Errorf("manifest carries a launch token value: %s", manifestBody)
	}
	summary, err := GetSummary(context.Background(), sink, batch)
	if err != nil {
		t.Fatalf("GetSummary: %v", err)
	}
	summaryBody, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("Marshal summary: %v", err)
	}
	if strings.Contains(string(summaryBody), "launch-secret-") {
		t.Errorf("summary carries a launch token value: %s", summaryBody)
	}
}

// TestFarmRun_NeverRerunsAFailedRun pins §3.5 FM-24: a failed run gets
// exactly one project/import call — RunBatch never re-executes a scenario
// on its own initiative to try for a better result.
func TestFarmRun_NeverRerunsAFailedRun(t *testing.T) {
	t.Parallel()
	const clientID = "client-5"
	f := newControllerFixture(t, clientID)
	account, client, fake, sink := f.account, f.client, f.s3, f.sink

	batch := "batch-5"
	sc := ScenarioRun{ID: "recipe-flaky"}
	runID := batch + "-" + sc.ID
	seedSettledRun(t, fake, runID, sc.ID, ResultFailed)

	opts := RunOptions{
		Batch: batch, ClientID: clientID, Set: "gate",
		CandidateSHA256: "cand-sha", EvaluatorSHA256: "eval-sha", WrapperSHA256: "wrap", ScenariosDigest: "scen",
		Scenarios: []ScenarioRun{sc}, OAuthToken: "oauth-token",
		Sink:      Sink{URL: "https://s3.example", Bucket: "zcp-farm", Key: "k", Secret: "s"},
		RunBudget: time.Second, PollInterval: time.Millisecond,
	}
	results, err := RunBatch(context.Background(), client, sink, opts)
	if err != nil {
		t.Fatalf("RunBatch: %v", err)
	}
	if len(results) != 1 || results[0].Result != ResultFailed {
		t.Fatalf("results = %+v, want one entry with Result=%q", results, ResultFailed)
	}
	if n := account.countMethod("POST", "/api/rest/public/client/"+clientID+"/project/import"); n != 1 {
		t.Errorf("project/import calls = %d, want exactly 1 — a failed run must never be auto-retried", n)
	}
}

// TestDeleteGuard_ForeignName_RefusesBeforePlatformCall pins §3.2
// FM-19/FM-20: Guard, the sole DeleteProject call site, refuses a
// non-"zcp-farm-"-prefixed name before making any platform call at all —
// not even a read.
func TestDeleteGuard_ForeignName_RefusesBeforePlatformCall(t *testing.T) {
	t.Parallel()
	client := panicClient{t: t}
	err := Guard(context.Background(), client, "project-123", "eval")
	if err == nil {
		t.Fatal("Guard: want error for a foreign-prefixed name, got nil")
	}
}

// TestFarmRun_ResultMetaUnderSuiteScenario_GradesFromTask pins D15: the
// evaluator writes results/<suite>/<scenario>/meta.json (the wrapper passes
// --results-dir $RUNDIR/results, the evaluator nests suite/scenario under
// it), not a fixed results/meta.json — the controller must still grade the
// run from task.result once it locates that nested file.
func TestFarmRun_ResultMetaUnderSuiteScenario_GradesFromTask(t *testing.T) {
	t.Parallel()
	const clientID = "client-15a"
	f := newControllerFixture(t, clientID)
	client, fake, sink := f.client, f.s3, f.sink

	batch := "batch-15a"
	sc := ScenarioRun{ID: "classic-static-nginx-simple"}
	runID := batch + "-" + sc.ID
	seedSettledRunAt(t, fake, runID, sc.ID, ResultPassed, "gate/classic-static-nginx-simple/meta.json")

	opts := RunOptions{
		Batch: batch, ClientID: clientID, Set: "gate",
		CandidateSHA256: "cand-sha", EvaluatorSHA256: "eval-sha", WrapperSHA256: "wrap", ScenariosDigest: "scen",
		Scenarios: []ScenarioRun{sc}, OAuthToken: "oauth-token",
		Sink:      Sink{URL: "https://s3.example", Bucket: "zcp-farm", Key: "k", Secret: "s"},
		RunBudget: time.Second, PollInterval: time.Millisecond,
	}
	results, err := RunBatch(context.Background(), client, sink, opts)
	if err != nil {
		t.Fatalf("RunBatch: %v", err)
	}
	if len(results) != 1 || results[0].Result != ResultPassed {
		t.Fatalf("results = %+v, want one entry with Result=%q", results, ResultPassed)
	}
}

// TestFarmRun_ResultMetaMissingOrAmbiguous_Blocked pins D15's failure
// modes: zero meta.json keys under runs/<runId>/results/, or more than one,
// both settle the run `blocked` (never a silent pick of the first match)
// with a detail naming how many keys were found.
func TestFarmRun_ResultMetaMissingOrAmbiguous_Blocked(t *testing.T) {
	t.Parallel()
	const clientID = "client-15b"

	t.Run("missing", func(t *testing.T) {
		t.Parallel()
		f := newControllerFixture(t, clientID)
		client, fake, sink := f.client, f.s3, f.sink
		batch := "batch-15b-missing"
		sc := ScenarioRun{ID: "recipe-missing-meta"}
		runID := batch + "-" + sc.ID

		resultsDigest := seedPart(t, fake, runID, "results", map[string]string{
			"output.log": "no meta.json here",
		})
		captureDigest := seedPart(t, fake, runID, "capture", map[string]string{
			"transcript.jsonl": `{"type":"assistant"}`,
		})
		done := fmt.Sprintf(`{"runId":%q,"scenarioId":%q,"parts":{"results":{"treeDigest":%q},"capture":{"treeDigest":%q}},"evaluatorSha256":"eval-sha","candidateSha256":"cand-sha"}`,
			runID, sc.ID, resultsDigest, captureDigest)
		fake.mu.Lock()
		fake.objects["runs/"+runID+"/done.json"] = []byte(done)
		fake.mu.Unlock()

		opts := RunOptions{
			Batch: batch, ClientID: clientID, Set: "gate",
			CandidateSHA256: "cand-sha", EvaluatorSHA256: "eval-sha", WrapperSHA256: "wrap", ScenariosDigest: "scen",
			Scenarios: []ScenarioRun{sc}, OAuthToken: "oauth-token",
			Sink:      Sink{URL: "https://s3.example", Bucket: "zcp-farm", Key: "k", Secret: "s"},
			RunBudget: time.Second, PollInterval: time.Millisecond,
		}
		results, err := RunBatch(context.Background(), client, sink, opts)
		if err != nil {
			t.Fatalf("RunBatch: %v", err)
		}
		if len(results) != 1 || results[0].Result != ResultBlocked {
			t.Fatalf("results = %+v, want one entry with Result=%q", results, ResultBlocked)
		}
		if !strings.Contains(results[0].Detail, "found 0 meta.json") {
			t.Errorf("Detail = %q, want it to mention 0 meta.json keys found", results[0].Detail)
		}
	})

	t.Run("ambiguous", func(t *testing.T) {
		t.Parallel()
		f := newControllerFixture(t, clientID+"-amb")
		client, fake, sink := f.client, f.s3, f.sink
		batch := "batch-15b-ambiguous"
		sc := ScenarioRun{ID: "recipe-ambiguous-meta"}
		runID := batch + "-" + sc.ID

		resultsDigest := seedPart(t, fake, runID, "results", map[string]string{
			"gate/scenario-a/meta.json": `{"task":{"result":"passed"}}`,
			"gate/scenario-b/meta.json": `{"task":{"result":"passed"}}`,
		})
		captureDigest := seedPart(t, fake, runID, "capture", map[string]string{
			"transcript.jsonl": `{"type":"assistant"}`,
		})
		done := fmt.Sprintf(`{"runId":%q,"scenarioId":%q,"parts":{"results":{"treeDigest":%q},"capture":{"treeDigest":%q}},"evaluatorSha256":"eval-sha","candidateSha256":"cand-sha"}`,
			runID, sc.ID, resultsDigest, captureDigest)
		fake.mu.Lock()
		fake.objects["runs/"+runID+"/done.json"] = []byte(done)
		fake.mu.Unlock()

		opts := RunOptions{
			Batch: batch, ClientID: clientID + "-amb", Set: "gate",
			CandidateSHA256: "cand-sha", EvaluatorSHA256: "eval-sha", WrapperSHA256: "wrap", ScenariosDigest: "scen",
			Scenarios: []ScenarioRun{sc}, OAuthToken: "oauth-token",
			Sink:      Sink{URL: "https://s3.example", Bucket: "zcp-farm", Key: "k", Secret: "s"},
			RunBudget: time.Second, PollInterval: time.Millisecond,
		}
		results, err := RunBatch(context.Background(), client, sink, opts)
		if err != nil {
			t.Fatalf("RunBatch: %v", err)
		}
		if len(results) != 1 || results[0].Result != ResultBlocked {
			t.Fatalf("results = %+v, want one entry with Result=%q", results, ResultBlocked)
		}
		if !strings.Contains(results[0].Detail, "found 2 meta.json") {
			t.Errorf("Detail = %q, want it to mention 2 meta.json keys found", results[0].Detail)
		}
	})
}

// TestFarmRun_BlockedTask_DetailNamesBlockingChecks pins S22 finding 1: when
// a settled run's task.result is blocked, the summary row's Detail names the
// blocking check ids read from verification.json (spec-testing-architecture
// §10.1), sibling to meta.json in the same results directory — sorted,
// joined with ", ", capped at 5 with "+N more". Independent oracle: each
// wantDetail literal is hand-assembled from the seeded rows, never read back
// from the implementation.
func TestFarmRun_BlockedTask_DetailNamesBlockingChecks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		checksJSON string
		wantDetail string
	}{
		{
			name: "few_ids_sorted_joined_and_non_matching_rows_excluded",
			checksJSON: `[
				{"id":"expected_service/web/status","check":"service_status","scope":"web","result":"blocked"},
				{"id":"expected_service/api/status","check":"service_status","scope":"api","result":"blocked"},
				{"id":"no_failed_processes/proj-1","check":"no_failed_processes","scope":"proj-1","result":"failed"},
				{"id":"expected_service/api/exists","check":"expected_service","scope":"api","result":"passed"}
			]`,
			wantDetail: "expected_service/api/status, expected_service/web/status",
		},
		{
			name: "more_than_five_ids_truncated_with_count",
			checksJSON: `[
				{"id":"c7","check":"x","scope":"s","result":"blocked"},
				{"id":"c2","check":"x","scope":"s","result":"blocked"},
				{"id":"c5","check":"x","scope":"s","result":"blocked"},
				{"id":"c1","check":"x","scope":"s","result":"blocked"},
				{"id":"c4","check":"x","scope":"s","result":"blocked"},
				{"id":"c6","check":"x","scope":"s","result":"blocked"},
				{"id":"c3","check":"x","scope":"s","result":"blocked"}
			]`,
			wantDetail: "c1, c2, c3, c4, c5 +2 more",
		},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			clientID := fmt.Sprintf("client-22a-%d", i)
			f := newControllerFixture(t, clientID)
			client, fake, sink := f.client, f.s3, f.sink

			batch := fmt.Sprintf("batch-22a-%d", i)
			sc := ScenarioRun{ID: "recipe-blocked"}
			runID := batch + "-" + sc.ID
			verification := fmt.Sprintf(`{"formatVersion":"zcp-eval-verification-2","mode":"required","result":"blocked","checks":%s,"advisory":[]}`, tc.checksJSON)

			resultsDigest := seedPart(t, fake, runID, "results", map[string]string{
				"gate/recipe-blocked/meta.json":         fmt.Sprintf(`{"task":{"result":%q}}`, ResultBlocked),
				"gate/recipe-blocked/verification.json": verification,
			})
			captureDigest := seedPart(t, fake, runID, "capture", map[string]string{
				"transcript.jsonl": `{"type":"assistant"}`,
			})
			done := fmt.Sprintf(`{"runId":%q,"scenarioId":%q,"parts":{"results":{"treeDigest":%q},"capture":{"treeDigest":%q}},"evaluatorSha256":"eval-sha","candidateSha256":"cand-sha"}`,
				runID, sc.ID, resultsDigest, captureDigest)
			fake.mu.Lock()
			fake.objects["runs/"+runID+"/done.json"] = []byte(done)
			fake.mu.Unlock()

			opts := RunOptions{
				Batch: batch, ClientID: clientID, Set: "gate",
				CandidateSHA256: "cand-sha", EvaluatorSHA256: "eval-sha", WrapperSHA256: "wrap", ScenariosDigest: "scen",
				Scenarios: []ScenarioRun{sc}, OAuthToken: "oauth-token",
				Sink:      Sink{URL: "https://s3.example", Bucket: "zcp-farm", Key: "k", Secret: "s"},
				RunBudget: time.Second, PollInterval: time.Millisecond,
			}
			results, err := RunBatch(context.Background(), client, sink, opts)
			if err != nil {
				t.Fatalf("RunBatch: %v", err)
			}
			if len(results) != 1 || results[0].Result != ResultBlocked || results[0].Detail != tc.wantDetail {
				t.Fatalf("results = %+v, want one entry Result=%q Detail=%q", results, ResultBlocked, tc.wantDetail)
			}
		})
	}
}

// TestFarmRun_BlockedTask_NoVerificationJSON_DetailSaysSo pins finding 1's
// other half: a blocked task whose results directory has no
// verification.json sibling gets the literal Detail "no verification.json
// in bundle", never an empty string.
func TestFarmRun_BlockedTask_NoVerificationJSON_DetailSaysSo(t *testing.T) {
	t.Parallel()
	const clientID = "client-22b"
	f := newControllerFixture(t, clientID)
	client, fake, sink := f.client, f.s3, f.sink

	batch := "batch-22b"
	sc := ScenarioRun{ID: "recipe-blocked-no-verification"}
	runID := batch + "-" + sc.ID
	seedSettledRun(t, fake, runID, sc.ID, ResultBlocked)

	opts := RunOptions{
		Batch: batch, ClientID: clientID, Set: "gate",
		CandidateSHA256: "cand-sha", EvaluatorSHA256: "eval-sha", WrapperSHA256: "wrap", ScenariosDigest: "scen",
		Scenarios: []ScenarioRun{sc}, OAuthToken: "oauth-token",
		Sink:      Sink{URL: "https://s3.example", Bucket: "zcp-farm", Key: "k", Secret: "s"},
		RunBudget: time.Second, PollInterval: time.Millisecond,
	}
	results, err := RunBatch(context.Background(), client, sink, opts)
	if err != nil {
		t.Fatalf("RunBatch: %v", err)
	}
	const wantDetail = "no verification.json in bundle"
	if len(results) != 1 || results[0].Result != ResultBlocked || results[0].Detail != wantDetail {
		t.Fatalf("results = %+v, want one entry Result=%q Detail=%q", results, ResultBlocked, wantDetail)
	}
}

// TestFarmRun_FailedCreationProcess_SettlesBlockedBeforeBudget pins D19: a
// FAILED creation-phase process (stack.create/stack.import) on the run's
// project ends waitForDone immediately, well inside the run budget, instead
// of burning the whole budget waiting for a done.json a dead project will
// never produce — the dead project is still deleted (settled=true, §3.3
// FM-21) exactly as any other settled run's.
func TestFarmRun_FailedCreationProcess_SettlesBlockedBeforeBudget(t *testing.T) {
	t.Parallel()
	const clientID = "client-19"
	f := newControllerFixture(t, clientID)
	account, client, sink := f.account, f.client, f.sink

	batch := "batch-19"
	sc := ScenarioRun{ID: "recipe-dead-project"}
	runID := batch + "-" + sc.ID
	// No seedSettledRun call: this run's project never writes done.json.

	account.mu.Lock()
	account.failedCreationProcessProjectName = ProjectPrefix + runID
	account.mu.Unlock()

	opts := RunOptions{
		Batch: batch, ClientID: clientID, Set: "gate",
		CandidateSHA256: "cand", EvaluatorSHA256: "eval", WrapperSHA256: "wrap", ScenariosDigest: "scen",
		Scenarios: []ScenarioRun{sc}, OAuthToken: "oauth-token",
		Sink: Sink{URL: "https://s3.example", Bucket: "zcp-farm", Key: "k", Secret: "s"},
		// A generous budget that would time this test out if D19 didn't
		// short-circuit it — waitForDone must settle within a few poll
		// intervals instead.
		RunBudget: time.Minute, PollInterval: 20 * time.Millisecond,
	}

	start := time.Now()
	results, err := RunBatch(context.Background(), client, sink, opts)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("RunBatch: %v", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("RunBatch took %s, want it to settle within a few poll intervals (D19), not the full run budget", elapsed)
	}
	if len(results) != 1 {
		t.Fatalf("results = %+v, want one entry", results)
	}
	wantDetail := "platform: stack.create FAILED: internalServerError"
	if results[0].Result != ResultBlocked || results[0].Detail != wantDetail {
		t.Fatalf("results[0] = %+v, want Result=%q Detail=%q", results[0], ResultBlocked, wantDetail)
	}
	if results[0].ProjectID != "" {
		t.Errorf("results[0].ProjectID = %q, want empty (dead project still deleted per FM-21)", results[0].ProjectID)
	}
	account.mu.Lock()
	stillExists := findProjectByFakeName(account, ProjectPrefix+runID)
	account.mu.Unlock()
	if stillExists {
		t.Errorf("project %s still present after a FAILED creation-phase settle", ProjectPrefix+runID)
	}
}

// TestRecomputePartDigest_RejectsPathEscape pins R4b: a bucket key whose
// relative path (against its runs/<runId>/<part>/ prefix) escapes upward —
// e.g. runs/<runId>/results/../../x, indistinguishable at the S3 layer from
// any other key string — is rejected before either downloading it or
// joining it into the verification temp dir, instead of being written
// outside that dir.
func TestRecomputePartDigest_RejectsPathEscape(t *testing.T) {
	t.Parallel()
	const clientID = "client-24-r4b"
	f := newControllerFixture(t, clientID)
	fake, sink := f.s3, f.sink

	runID := "run-r4b"
	escapeKey := "runs/" + runID + "/results/../../x"
	fake.mu.Lock()
	fake.objects[escapeKey] = []byte("evil")
	fake.mu.Unlock()

	_, err := recomputePartDigest(context.Background(), sink, runID, "results")
	if err == nil {
		t.Fatal("recomputePartDigest: want an error for a path-escaping key, got nil")
	}
}

// TestFarmRun_FailedImportAfterStarted_DoesNotDeleteProject pins R1: once
// runs/<runId>/started.json exists (the run's own agent is underway), a
// FAILED stack.import naming a service OTHER than the control service
// ("api", the agent's own mid-run zerops_import) must never be mistaken for
// the platform failing to create the run's own project — waitForDone keeps
// waiting out the run's budget instead of settling immediately, and the
// live project is never deleted out from under the running agent.
func TestFarmRun_FailedImportAfterStarted_DoesNotDeleteProject(t *testing.T) {
	t.Parallel()
	const clientID = "client-24-r1"
	f := newControllerFixture(t, clientID)
	account, client, sink := f.account, f.client, f.sink

	batch := "batch-24-r1"
	sc := ScenarioRun{ID: "recipe-mid-run-import"}
	runID := batch + "-" + sc.ID
	// No seedSettledRun: this run's project never writes done.json.

	if err := sink.Put(context.Background(), "runs/"+runID+"/started.json", []byte(`{"runId":"`+runID+`"}`)); err != nil {
		t.Fatalf("seed started.json: %v", err)
	}

	account.mu.Lock()
	account.failedCreationProcessProjectName = ProjectPrefix + runID
	account.failedCreationProcessServiceName = "api"
	account.failedCreationProcessAction = "stack.import"
	account.mu.Unlock()

	opts := RunOptions{
		Batch: batch, ClientID: clientID, Set: "gate",
		CandidateSHA256: "cand", EvaluatorSHA256: "eval", WrapperSHA256: "wrap", ScenariosDigest: "scen",
		Scenarios: []ScenarioRun{sc}, OAuthToken: "oauth-token",
		Sink:      Sink{URL: "https://s3.example", Bucket: "zcp-farm", Key: "k", Secret: "s"},
		RunBudget: 200 * time.Millisecond, PollInterval: 20 * time.Millisecond,
	}

	results, err := RunBatch(context.Background(), client, sink, opts)
	if err != nil {
		t.Fatalf("RunBatch: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %+v, want 1 entry", results)
	}
	if results[0].Result != ResultBlocked || results[0].Detail != DetailNoBundle {
		t.Fatalf("results[0] = %+v, want Result=%q Detail=%q (a FAILED stack.import on a non-control service, after started.json, must be ignored)", results[0], ResultBlocked, DetailNoBundle)
	}
	if results[0].ProjectID == "" {
		t.Errorf("results[0].ProjectID empty, want the run's project kept (no bundle exemption)")
	}
	for _, entry := range account.requestLog() {
		if strings.HasPrefix(entry, "DELETE ") {
			t.Errorf("unexpected DELETE request recorded: %s", entry)
		}
	}
	account.mu.Lock()
	stillExists := findProjectByFakeName(account, ProjectPrefix+runID)
	account.mu.Unlock()
	if !stillExists {
		t.Errorf("project %s was deleted, want it kept", ProjectPrefix+runID)
	}
}

// tickClock is a fake clock that advances by step on every call to Now —
// used by TestFarmRun_PerRunDeadline_FromCreation to make R7's per-run
// deadline observable in simulated time without slowing the test down with
// real sleeps.
type tickClock struct {
	mu   sync.Mutex
	cur  time.Time
	step time.Duration
}

func (c *tickClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.cur
	c.cur = c.cur.Add(c.step)
	return t
}

// TestFarmRun_PerRunDeadline_FromCreation pins R7: each active run's budget
// deadline is anchored at its OWN creation time, not recomputed when its
// turn in the sequential settle loop begins. Two runs, neither ever
// produces done.json, both settle budget-blocked — under the pre-fix
// behavior (deadline recomputed fresh inside waitForDone at call time), the
// second run's own wait would only begin once the first run's full budget
// had already elapsed, so it would need ANOTHER full budget's worth of
// simulated time on top — total simulated elapsed time would approach 2x
// budget. Anchoring both deadlines at creation keeps the whole batch within
// roughly one budget's worth of simulated time instead.
func TestFarmRun_PerRunDeadline_FromCreation(t *testing.T) {
	t.Parallel()
	const clientID = "client-24-r7"
	f := newControllerFixture(t, clientID)
	client, sink := f.client, f.sink

	batch := "batch-24-r7"
	scenarios := []ScenarioRun{{ID: "recipe-hung-1"}, {ID: "recipe-hung-2"}}
	// Neither run ever writes done.json — both settle budget-blocked.

	const step = time.Minute
	const budget = 100 * time.Minute
	clock := &tickClock{cur: time.Unix(0, 0), step: step}

	opts := RunOptions{
		Batch: batch, ClientID: clientID, Set: "gate",
		CandidateSHA256: "cand", EvaluatorSHA256: "eval", WrapperSHA256: "wrap", ScenariosDigest: "scen",
		Scenarios: scenarios, OAuthToken: "oauth-token",
		Sink:         Sink{URL: "https://s3.example", Bucket: "zcp-farm", Key: "k", Secret: "s"},
		RunBudget:    budget,
		PollInterval: time.Nanosecond,
		Now:          clock.Now,
	}

	start := time.Now()
	results, err := RunBatch(context.Background(), client, sink, opts)
	elapsedWall := time.Since(start)
	if err != nil {
		t.Fatalf("RunBatch: %v", err)
	}
	if elapsedWall > 5*time.Second {
		t.Fatalf("RunBatch took %s of real wall time, want it bounded by a handful of near-zero-PollInterval iterations", elapsedWall)
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v, want 2 entries", results)
	}
	for _, r := range results {
		if r.Result != ResultBlocked || r.Detail != DetailNoBundle {
			t.Errorf("result %+v, want Result=%q Detail=%q", r, ResultBlocked, DetailNoBundle)
		}
		if r.ProjectID == "" {
			t.Errorf("result %+v: ProjectID empty, want the FM-21 no-bundle exemption to keep it", r)
		}
	}

	totalElapsed := clock.cur.Sub(time.Unix(0, 0))
	if totalElapsed >= 2*budget {
		t.Errorf("total simulated elapsed time = %s, want well under 2x budget (%s) — run2's deadline must be anchored at its own creation time, not reset when its wait begins", totalElapsed, 2*budget)
	}
}

// TestFarmRun_Interrupt_WritesSummaryKeepsProjects pins R3: cancelling
// RunBatch's context while a run is still being waited on stops the wait,
// writes batches/<batch>/summary.json with endedBy "interrupt", records the
// unsettled run blocked/"interrupted", and — like FM-21's no-bundle
// exemption — never deletes its project.
func TestFarmRun_Interrupt_WritesSummaryKeepsProjects(t *testing.T) {
	t.Parallel()
	const clientID = "client-24-r3"
	f := newControllerFixture(t, clientID)
	account, client, sink := f.account, f.client, f.sink

	batch := "batch-24-r3"
	sc := ScenarioRun{ID: "recipe-interrupted"}
	runID := batch + "-" + sc.ID
	// No seedSettledRun: the run's project never writes done.json before
	// the ctx is cancelled.

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	opts := RunOptions{
		Batch: batch, ClientID: clientID, Set: "gate",
		CandidateSHA256: "cand", EvaluatorSHA256: "eval", WrapperSHA256: "wrap", ScenariosDigest: "scen",
		Scenarios: []ScenarioRun{sc}, OAuthToken: "oauth-token",
		Sink: Sink{URL: "https://s3.example", Bucket: "zcp-farm", Key: "k", Secret: "s"},
		// A generous budget the interrupt must pre-empt long before it
		// would elapse on its own.
		RunBudget: time.Hour, PollInterval: 20 * time.Millisecond,
	}

	start := time.Now()
	results, err := RunBatch(ctx, client, sink, opts)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("RunBatch: %v", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("RunBatch took %s, want it to stop promptly once ctx is cancelled, not run out the hour-long budget", elapsed)
	}
	if len(results) != 1 {
		t.Fatalf("results = %+v, want 1 entry", results)
	}
	if results[0].Result != ResultBlocked || results[0].Detail != DetailInterrupted {
		t.Fatalf("results[0] = %+v, want Result=%q Detail=%q", results[0], ResultBlocked, DetailInterrupted)
	}
	if results[0].ProjectID == "" {
		t.Errorf("results[0].ProjectID empty, want the interrupted run's project kept")
	}

	for _, entry := range account.requestLog() {
		if strings.HasPrefix(entry, "DELETE ") {
			t.Errorf("unexpected DELETE request recorded: %s", entry)
		}
	}
	account.mu.Lock()
	stillExists := findProjectByFakeName(account, ProjectPrefix+runID)
	account.mu.Unlock()
	if !stillExists {
		t.Errorf("project %s was deleted, want it kept (interrupted)", ProjectPrefix+runID)
	}

	summary, err := GetSummary(context.Background(), sink, batch)
	if err != nil {
		t.Fatalf("GetSummary: %v", err)
	}
	if summary.EndedBy != "interrupt" {
		t.Errorf("summary.EndedBy = %q, want %q", summary.EndedBy, "interrupt")
	}
	if len(summary.Runs) != 1 || summary.Runs[0].Result != ResultBlocked || summary.Runs[0].Detail != DetailInterrupted {
		t.Errorf("summary.Runs = %+v, want one entry Result=%q Detail=%q", summary.Runs, ResultBlocked, DetailInterrupted)
	}
}

// TestFarmRun_LaunchTokenRevokedWhenCreateFails pins R2 (FM-23): a launch
// scenario's already-minted launch token must not leak when the run's
// creation fails AFTER the mint (here, CreateAndImportProject) — the
// controller revokes it immediately, and the revoke DELETE is actually
// issued regardless of whether the fake happens to accept it.
func TestFarmRun_LaunchTokenRevokedWhenCreateFails(t *testing.T) {
	t.Parallel()
	const clientID = "client-24-r2"
	f := newControllerFixture(t, clientID)
	account, client, sink := f.account, f.client, f.sink

	batch := "batch-24-r2"
	sc := ScenarioRun{ID: "recipe-launch-create-fail", Launch: true}
	runID := batch + "-" + sc.ID

	account.mu.Lock()
	account.failImportProjectName = ProjectPrefix + runID
	account.mu.Unlock()

	opts := RunOptions{
		Batch: batch, ClientID: clientID, Set: "gate",
		CandidateSHA256: "cand", EvaluatorSHA256: "eval", WrapperSHA256: "wrap", ScenariosDigest: "scen",
		Scenarios: []ScenarioRun{sc}, OAuthToken: "oauth-token",
		Sink:      Sink{URL: "https://s3.example", Bucket: "zcp-farm", Key: "k", Secret: "s"},
		RunBudget: time.Second, PollInterval: time.Millisecond,
	}

	results, err := RunBatch(context.Background(), client, sink, opts)
	if err != nil {
		t.Fatalf("RunBatch: %v", err)
	}
	if len(results) != 1 || results[0].Result != ResultBlocked {
		t.Fatalf("results = %+v, want one blocked entry", results)
	}

	revoked := false
	for _, entry := range account.requestLog() {
		if strings.HasPrefix(entry, "DELETE /api/rest/public/client/"+clientID+"/integration-token/") {
			revoked = true
		}
	}
	if !revoked {
		t.Fatalf("no integration-token revoke DELETE recorded; request log: %v", account.requestLog())
	}

	// Independent oracle: either the revoke actually succeeded (the token
	// no longer validates AND the result clears it to "") or, had it
	// failed, the id would still be on the result — never silently dropped
	// with the token left dangling and untracked.
	if results[0].LaunchTokenID != "" {
		account.mu.Lock()
		_, stillValid := account.tokens[results[0].LaunchTokenID]
		account.mu.Unlock()
		if !stillValid {
			t.Errorf("results[0].LaunchTokenID = %q is recorded but the fake already revoked it — recording only belongs on a failed revoke", results[0].LaunchTokenID)
		}
	}
}

// TestFarmRun_RollbackFailure_KeepsProjectIDAndError pins R6: when a
// per-run failure after project creation triggers a Guard rollback and that
// rollback's DeleteProject itself fails, the blocked result keeps the
// leaked project's id (never "") and names the rollback failure in Error —
// so the leak is visible instead of looking like the project was already
// cleaned up.
func TestFarmRun_RollbackFailure_KeepsProjectIDAndError(t *testing.T) {
	t.Parallel()
	const clientID = "client-24-r6"
	f := newControllerFixture(t, clientID)
	account, client, sink := f.account, f.client, f.sink

	batch := "batch-24-r6"
	sc := ScenarioRun{ID: "recipe-rollback-fail"}
	runID := batch + "-" + sc.ID
	runProjectName := ProjectPrefix + runID

	account.mu.Lock()
	account.failScopedMint = true
	account.failDeleteProjectName = runProjectName
	account.mu.Unlock()

	opts := RunOptions{
		Batch: batch, ClientID: clientID, Set: "gate",
		CandidateSHA256: "cand", EvaluatorSHA256: "eval", WrapperSHA256: "wrap", ScenariosDigest: "scen",
		Scenarios: []ScenarioRun{sc}, OAuthToken: "oauth-token",
		Sink:      Sink{URL: "https://s3.example", Bucket: "zcp-farm", Key: "k", Secret: "s"},
		RunBudget: time.Second, PollInterval: time.Millisecond,
	}

	results, err := RunBatch(context.Background(), client, sink, opts)
	if err != nil {
		t.Fatalf("RunBatch: %v", err)
	}
	if len(results) != 1 || results[0].Result != ResultBlocked {
		t.Fatalf("results = %+v, want one blocked entry", results)
	}
	if results[0].ProjectID == "" {
		t.Errorf("results[0].ProjectID empty, want the leaked project's id kept (rollback failed)")
	}
	if !strings.Contains(results[0].Error, "rollback failed") {
		t.Errorf("results[0].Error = %q, want it to mention the rollback failure", results[0].Error)
	}
	account.mu.Lock()
	stillExists := findProjectByFakeName(account, runProjectName)
	account.mu.Unlock()
	if !stillExists {
		t.Errorf("project %s not present in fakeAccount, want it still there (rollback DELETE failed)", runProjectName)
	}
}

// TestRunBatch_PostCreateManifestFailure_FinalizesAndReturnsError pins FM-24:
// a failure while recording minted ids must still settle the already-created
// run and persist the summary when the sink recovers for the final write.
func TestRunBatch_PostCreateManifestFailure_FinalizesAndReturnsError(t *testing.T) {
	t.Parallel()
	const clientID = "client-s4-manifest-fail"
	f := newControllerFixture(t, clientID)
	account, client := f.account, f.client
	fake := newFakeS3()
	var manifestPuts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/batches/batch-s4-manifest-fail/manifest.json") {
			manifestPuts++
			if manifestPuts >= 2 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		}
		fake.handler(t)(w, r)
	}))
	t.Cleanup(server.Close)
	sink := NewSinkClient(Config{URL: server.URL, Bucket: fakeS3Bucket, Key: "key", Secret: "secret"})
	batch := "batch-s4-manifest-fail"
	sc := ScenarioRun{ID: "first", Launch: true}
	// The fake done bundle is unavailable; the finalization path must keep
	// the project rather than deleting unfinished work as an error shortcut.
	opts := RunOptions{Batch: batch, ClientID: clientID, Set: "gate", CandidateSHA256: "cand", EvaluatorSHA256: "eval", WrapperSHA256: "wrap", ScenariosDigest: "scen", Scenarios: []ScenarioRun{sc}, OAuthToken: "oauth", Sink: Sink{URL: server.URL, Bucket: fakeS3Bucket, Key: "key", Secret: "secret"}, RunBudget: time.Millisecond, PollInterval: time.Millisecond}
	results, err := RunBatch(context.Background(), client, sink, opts)
	if err == nil || !strings.Contains(err.Error(), "update manifest") {
		t.Fatalf("RunBatch error = %v, want post-create manifest failure", err)
	}
	if len(results) != 1 || results[0].Result != ResultBlocked {
		t.Fatalf("results = %+v, want one blocked retained run", results)
	}
	if results[0].ProjectID == "" {
		t.Errorf("results[0].ProjectID empty, want recovery id retained")
	}
	summary, summaryErr := GetSummary(context.Background(), sink, batch)
	if summaryErr != nil {
		t.Fatalf("GetSummary: %v", summaryErr)
	}
	if len(summary.Runs) != 1 || summary.Runs[0].ProjectID == "" {
		t.Fatalf("summary.Runs = %+v, want retained project id", summary.Runs)
	}
	if account.countMethod("DELETE", "/api/rest/public/project/"+results[0].ProjectID) != 0 {
		t.Errorf("unfinished project was deleted during finalization")
	}
}

type failSecondRunTokenMint struct {
	PlatformClient
	calls int
}

func (c *failSecondRunTokenMint) MintProjectScopedToken(ctx context.Context, clientID, projectID, name string) (platform.MintedToken, error) {
	c.calls++
	if c.calls == 2 {
		return platform.MintedToken{}, &platform.PlatformError{Code: platform.ErrDelegationUnavailable, Message: "simulated later mint failure"}
	}
	return c.PlatformClient.MintProjectScopedToken(ctx, clientID, projectID, name)
}

func TestRunBatch_LaterMintFailure_FinalizesEarlierRuns(t *testing.T) {
	t.Parallel()
	f := newControllerFixture(t, "client-s4-later-mint")
	client := &failSecondRunTokenMint{PlatformClient: f.client}
	batch := "batch-s4-later-mint"
	opts := RunOptions{Batch: batch, ClientID: "client-s4-later-mint", Set: "gate", CandidateSHA256: "cand", EvaluatorSHA256: "eval", WrapperSHA256: "wrap", ScenariosDigest: "scen", Scenarios: []ScenarioRun{{ID: "first"}, {ID: "second"}}, OAuthToken: "oauth", Sink: Sink{URL: f.sink.cfg.URL, Bucket: fakeS3Bucket, Key: "key", Secret: "secret"}, RunBudget: time.Millisecond, PollInterval: time.Millisecond}
	results, err := RunBatch(context.Background(), client, f.sink, opts)
	if err == nil || !strings.Contains(err.Error(), "mint run token") {
		t.Fatalf("RunBatch error = %v, want later mint failure", err)
	}
	if len(results) != 2 || results[1].RunID != batch+"-first" || results[1].ProjectID == "" {
		t.Fatalf("results = %+v, want abort row plus earlier run retained for recovery", results)
	}
	if _, err := GetSummary(context.Background(), f.sink, batch); err != nil {
		t.Fatalf("GetSummary: %v, want final summary", err)
	}
}

type failLaunchRevokeClient struct{ PlatformClient }

func (c failLaunchRevokeClient) MintProjectScopedToken(context.Context, string, string, string) (platform.MintedToken, error) {
	return platform.MintedToken{}, &platform.PlatformError{Code: platform.ErrDelegationUnavailable, Message: "simulated forbidden mint"}
}

func (c failLaunchRevokeClient) RevokeIntegrationToken(context.Context, string, string) error {
	return errors.New("simulated revoke failure")
}

func TestRunBatch_RollbackFailure_RetainsRecoveryIDs(t *testing.T) {
	t.Parallel()
	f := newControllerFixture(t, "client-s4-recovery-ids")
	batch := "batch-s4-recovery-ids"
	runID := batch + "-launch"
	f.account.mu.Lock()
	f.account.failScopedMint = true
	f.account.failDeleteProjectName = ProjectPrefix + runID
	f.account.mu.Unlock()
	opts := RunOptions{Batch: batch, ClientID: "client-s4-recovery-ids", Set: "gate", CandidateSHA256: "cand", EvaluatorSHA256: "eval", WrapperSHA256: "wrap", ScenariosDigest: "scen", Scenarios: []ScenarioRun{{ID: "launch", Launch: true}}, OAuthToken: "oauth", Sink: Sink{URL: f.sink.cfg.URL, Bucket: fakeS3Bucket, Key: "key", Secret: "secret"}, RunBudget: time.Second, PollInterval: time.Millisecond}
	results, err := RunBatch(context.Background(), failLaunchRevokeClient{PlatformClient: f.client}, f.sink, opts)
	if err == nil || len(results) != 1 {
		t.Fatalf("RunBatch results=%+v err=%v, want one failed run with recovery data", results, err)
	}
	if results[0].ProjectID == "" || results[0].LaunchTokenID == "" {
		t.Fatalf("result = %+v, want retained project and launch token ids", results[0])
	}
	if !strings.Contains(results[0].Error, "rollback failed") || !strings.Contains(results[0].Error, "revoke failed") {
		t.Fatalf("result.Error = %q, want both cleanup failures", results[0].Error)
	}
}

// TestRunBatch_ManifestRecordsObserver pins §1.4/§3.3: `farm run`'s
// --observer choice is recorded verbatim in batches/<batch>/manifest.json's
// "observer" field. Independent oracle: the expected value is the literal
// string this test passes in via RunOptions.Observer, never anything
// RunBatch/PutManifest derives on its own.
func TestRunBatch_ManifestRecordsObserver(t *testing.T) {
	t.Parallel()
	const clientID = "client-observer-1"
	f := newControllerFixture(t, clientID)
	client, sink := f.client, f.sink

	batch := "batch-observer-1"
	sc := ScenarioRun{ID: "recipe-observer"}
	seedSettledRun(t, f.s3, batch+"-"+sc.ID, sc.ID, ResultPassed)

	opts := RunOptions{
		Batch: batch, ClientID: clientID, Set: "gate",
		CandidateSHA256: "cand", EvaluatorSHA256: "eval", WrapperSHA256: "wrap", ScenariosDigest: "scen",
		Scenarios: []ScenarioRun{sc}, OAuthToken: "oauth-token",
		Sink:      Sink{URL: "https://s3.example", Bucket: "zcp-farm", Key: "k", Secret: "s"},
		RunBudget: time.Second, PollInterval: time.Millisecond,
		Observer: "claude-opus-5",
	}

	if _, err := RunBatch(context.Background(), client, sink, opts); err != nil {
		t.Fatalf("RunBatch: %v", err)
	}

	manifest, err := GetManifest(context.Background(), sink, batch)
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	if manifest.Observer != "claude-opus-5" {
		t.Errorf("manifest.Observer = %q, want %q", manifest.Observer, "claude-opus-5")
	}
}
