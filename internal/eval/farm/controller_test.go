package farm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
		f.nextID++
		id := fmt.Sprintf("proj-%d", f.nextID)
		f.projects[id] = fakeProject{id: id, name: name}
		f.mu.Unlock()
		fmt.Fprintf(w, `{"projectId":%q,"projectName":%q,"serviceStacks":[]}`, id, name)
		return

	case r.Method == http.MethodPost && r.URL.Path == "/api/rest/public/client/"+f.clientID+"/integration-token":
		f.mu.Lock()
		f.nextTok++
		tokID := fmt.Sprintf("tok-%d", f.nextTok)
		f.tokens[tokID] = true
		f.mu.Unlock()
		fmt.Fprintf(w, `{"id":%q,"token":"launch-secret-%s"}`, tokID, tokID)
		return

	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/rest/public/client/"+f.clientID+"/integration-token/"):
		tokID := strings.TrimPrefix(r.URL.Path, "/api/rest/public/client/"+f.clientID+"/integration-token/")
		f.mu.Lock()
		delete(f.tokens, tokID)
		f.mu.Unlock()
		fmt.Fprint(w, `{"success":true}`)
		return

	case strings.HasPrefix(r.URL.Path, "/api/rest/public/project/"):
		id := strings.TrimPrefix(r.URL.Path, "/api/rest/public/project/")
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
			return
		case http.MethodDelete:
			f.mu.Lock()
			delete(f.projects, id)
			f.mu.Unlock()
			fmt.Fprintf(w, `{"id":"proc-%s","status":"FINISHED"}`, id)
			return
		}
	}

	f.t.Errorf("fakeAccount: unexpected request %s %s", r.Method, r.URL.Path)
	w.WriteHeader(http.StatusNotFound)
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
	client, closer, err := NewAccountClient("farm-account-token", account.URL())
	if err != nil {
		t.Fatalf("NewAccountClient: %v", err)
	}
	t.Cleanup(closer)

	fake := newFakeS3("zcp-farm")
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
	resultsDigest := seedPart(t, fake, runID, "results", map[string]string{
		"meta.json": fmt.Sprintf(`{"task":{"result":%q}}`, taskResult),
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
		CandidateSHA256: "cand-sha", EvaluatorSHA256: "eval-sha", ScenariosDigest: "scen-sha",
		Scenarios: scenarios, CredentialMode: CredentialAPIKey, Credential: "sk-ant-farm",
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
		manifest.EvaluatorSha256 != "eval-sha" || manifest.ScenariosDigest != "scen-sha" || manifest.CredentialMode != string(CredentialAPIKey) {
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
			CandidateSHA256: "cand", EvaluatorSHA256: "eval", ScenariosDigest: "scen",
			Scenarios: []ScenarioRun{sc}, CredentialMode: CredentialAPIKey, Credential: "sk-ant",
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
			CandidateSHA256: "cand", EvaluatorSHA256: "eval", ScenariosDigest: "scen",
			Scenarios: []ScenarioRun{sc}, CredentialMode: CredentialAPIKey, Credential: "sk-ant",
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
		CandidateSHA256: "cand", EvaluatorSHA256: "eval", ScenariosDigest: "scen",
		Scenarios: []ScenarioRun{sc}, CredentialMode: CredentialAPIKey, Credential: "sk-ant",
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
		CandidateSHA256: "cand", EvaluatorSHA256: "eval", ScenariosDigest: "scen",
		Scenarios: []ScenarioRun{sc}, CredentialMode: CredentialAPIKey, Credential: "sk-ant",
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
		CandidateSHA256: "cand", EvaluatorSHA256: "eval", ScenariosDigest: "scen",
		Scenarios: []ScenarioRun{sc}, CredentialMode: CredentialAPIKey, Credential: "sk-ant",
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
