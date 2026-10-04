package matesetup_test

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/matesetup"
	"github.com/zeropsio/zcp/internal/platform"
)

const planYAML = `services:
  - hostname: appdev
    type: nodejs@22
    startWithoutCode: true
  - hostname: appstage
    type: nodejs@22
`

func plan() string { return base64.StdEncoding.EncodeToString([]byte(planYAML)) }

var t0 = time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)

func stampAt(d time.Duration) string { return t0.Add(d).Format(time.RFC3339Nano) }

// fakeAPI is a project that imports what it is sent: each imported service
// appears with one create process, which finishes after `runFor` looks.
type fakeAPI struct {
	mu       sync.Mutex
	services []platform.ServiceStack
	procs    []platform.Process
	imports  []string
	looks    int
	runFor   int
	started  map[string]int // process id -> look it started at
	// importErr fails the next import calls; createAnyway still creates.
	importErr    []error
	createAnyway bool
	// refuse names services the import answers with an error.
	refuse map[string]string
	// failProcess names services whose create process ends FAILED.
	failProcess map[string]string
	// isolation is the project's variables as each read of them answers —
	// "closed" (envIsolation service), "open" (none), "keyed" (service, with
	// ZCP_API_KEY still project-wide), "unread" (no envIsolation yet: the
	// read trails), "<error>" — the last one staying; empty is "closed".
	// isolationReads counts the reads, and readsAtImport is that count when
	// the first import was sent.
	isolation      []string
	isolationReads int
	readsAtImport  int
	// taken names services another import creates first; takenCode is the
	// code the platform answers this import with for them.
	taken     map[string]bool
	takenCode string
	// takenWhole answers the whole call with this error after creating.
	takenWhole error
}

// GetProjectEnv is Zerops answering the project's variables, where whether
// it is closed off is read (ops.ProjectClosedOff).
func (f *fakeAPI) GetProjectEnv(context.Context, string) ([]platform.ProjectEnvVar, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	value := "closed"
	if len(f.isolation) > 0 {
		value = f.isolation[min(f.isolationReads, len(f.isolation)-1)]
	}
	f.isolationReads++
	isolation := func(content string) platform.ProjectEnvVar {
		return platform.ProjectEnvVar{ID: "e-iso", Key: "envIsolation", Content: content, Type: platform.ProjectEnvSystem}
	}
	key := platform.ProjectEnvVar{ID: "e-key", Key: "ZCP_API_KEY", Content: "k", Type: platform.ProjectEnvUser}
	switch value {
	case "<error>":
		return nil, errors.New("Zerops did not answer")
	case "open":
		return []platform.ProjectEnvVar{isolation("none"), key}, nil
	case "keyed":
		return []platform.ProjectEnvVar{isolation("service service@zcp"), key}, nil
	case "unread":
		return []platform.ProjectEnvVar{key}, nil
	}
	return []platform.ProjectEnvVar{isolation("service service@zcp")}, nil
}

func (f *fakeAPI) ListServicesDirect(context.Context, string) ([]platform.ServiceStack, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.looks++
	for i := range f.procs {
		p := &f.procs[i]
		if p.Status == platform.ProcessStatusRunning && f.looks-f.started[p.ID] >= f.runFor {
			p.Status = platform.ProcessStatusFinished
			if reason, ok := f.failProcess[p.ServiceStacks[0].Name]; ok {
				p.Status = platform.ProcessStatusFailed
				p.FailReason = &reason
			}
		}
	}
	return slices.Clone(f.services), nil
}

func (f *fakeAPI) GetProjectProcessesDirect(context.Context, string) ([]platform.Process, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.procs), nil
}

func (f *fakeAPI) GetProcess(_ context.Context, id string) (*platform.Process, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.procs {
		if p.ID == id {
			return &p, nil
		}
	}
	return nil, errors.New("not found")
}

func (f *fakeAPI) ImportServices(_ context.Context, _ string, body string) (*platform.ImportResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.imports) == 0 {
		f.readsAtImport = f.isolationReads
	}
	f.imports = append(f.imports, body)
	var failWith error
	if len(f.importErr) > 0 {
		failWith, f.importErr = f.importErr[0], f.importErr[1:]
		if !f.createAnyway {
			return nil, failWith
		}
	}
	result := &platform.ImportResult{}
	for _, host := range []string{"appdev", "appstage"} {
		if !strings.Contains(body, "hostname: "+host) {
			continue
		}
		if f.taken[host] {
			// Another import created it first: the platform answers the
			// name taken, and the service is there.
			f.services = append(f.services, platform.ServiceStack{ID: "id-" + host, Name: host, Status: "ACTIVE"})
			result.ServiceStacks = append(result.ServiceStacks, platform.ImportedServiceStack{Name: host, Error: &platform.APIError{Code: f.takenCode, Message: "service stack name is not available"}})
			continue
		}
		if msg, ok := f.refuse[host]; ok {
			result.ServiceStacks = append(result.ServiceStacks, platform.ImportedServiceStack{Name: host, Error: &platform.APIError{Code: "serviceStackTypeNotFound", Message: msg}})
			continue
		}
		id := "id-" + host
		proc := platform.Process{ID: "proc-" + host, ActionName: "stack.create", Status: platform.ProcessStatusRunning,
			Created: stampAt(time.Second), ServiceStacks: []platform.ServiceStackRef{{ID: id, Name: host}}}
		f.services = append(f.services, platform.ServiceStack{ID: id, Name: host, Status: "ACTIVE"})
		f.procs = append(f.procs, proc)
		f.started[proc.ID] = f.looks
		result.ServiceStacks = append(result.ServiceStacks, platform.ImportedServiceStack{ID: id, Name: host, Processes: []platform.Process{proc}})
	}
	if failWith != nil {
		return nil, failWith
	}
	if f.takenWhole != nil {
		return nil, f.takenWhole
	}
	return result, nil
}

func newFake() *fakeAPI { return &fakeAPI{runFor: 2, started: map[string]int{}} }

func importer(api *fakeAPI, path string) matesetup.Importer {
	return matesetup.Importer{
		API: api, ProjectID: "proj", StatusPath: path,
		Poll: time.Millisecond, Timeout: 5 * time.Second,
		Backoff:       []time.Duration{time.Millisecond, time.Millisecond},
		IsolationPoll: func(time.Duration) time.Duration { return time.Millisecond },
		OwnDeployPoll: time.Millisecond,
		Now:           func() time.Time { return t0.Add(time.Second) },
	}
}

func readStatus(t *testing.T, path string) mate.Status {
	t.Helper()
	s, err := mate.ReadStatus(path)
	if err != nil {
		t.Fatalf("ReadStatus: %v", err)
	}
	return s
}

func states(rs mate.RuntimesStatus) string {
	out := make([]string, 0, len(rs.Services))
	for _, s := range rs.Services {
		out = append(out, s.Hostname+"="+s.State)
	}
	return strings.Join(out, ",")
}

// TestRun_ImportsWhatIsMissing_AndFollowsItToTheEnd covers the boot import's
// way through a fresh Mate and through restarts: it imports only what the
// project does not hold, never what is there, and ends with every listed
// service's state.
func TestRun_ImportsWhatIsMissing_AndFollowsItToTheEnd(t *testing.T) {
	tests := []struct {
		name        string
		existing    []string
		prior       *mate.RuntimesStatus
		wantImports int
		wantSent    []string
		wantNotSent []string
		wantState   string
		wantDetail  string
	}{
		{
			name:        "fresh Mate imports every listed runtime",
			wantImports: 1, wantSent: []string{"appdev", "appstage"},
			wantState: mate.RuntimesDone, wantDetail: "appdev=running,appstage=running",
		},
		{
			name:     "a restart imports only what is still missing",
			existing: []string{"appdev"}, wantImports: 1,
			wantSent: []string{"appstage"}, wantNotSent: []string{"appdev"},
			wantState: mate.RuntimesDone, wantDetail: "appdev=running,appstage=running",
		},
		{
			name:     "everything there at the first look imports nothing",
			existing: []string{"appdev", "appstage"}, wantImports: 0,
			wantState: mate.RuntimesDone, wantDetail: "appdev=running,appstage=running",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "status.json")
			api := newFake()
			for _, h := range tt.existing {
				api.services = append(api.services, platform.ServiceStack{ID: "id-" + h, Name: h, Status: "ACTIVE"})
			}
			if tt.prior != nil {
				if err := mate.UpdateStatus(path, func(s *mate.Status) { s.Runtimes = *tt.prior }); err != nil {
					t.Fatal(err)
				}
			}
			importer(api, path).Run(context.Background(), plan())

			if len(api.imports) != tt.wantImports {
				t.Fatalf("imports = %d, want %d", len(api.imports), tt.wantImports)
			}
			for _, h := range tt.wantSent {
				if !strings.Contains(api.imports[0], "hostname: "+h) {
					t.Errorf("import did not carry %s:\n%s", h, api.imports[0])
				}
			}
			for _, h := range tt.wantNotSent {
				if strings.Contains(api.imports[0], "hostname: "+h) {
					t.Errorf("import carried %s, which the project already holds:\n%s", h, api.imports[0])
				}
			}
			got := readStatus(t, path).Runtimes
			if got.State != tt.wantState || states(got) != tt.wantDetail {
				t.Errorf("runtimes = %s [%s], want %s [%s]", got.State, states(got), tt.wantState, tt.wantDetail)
			}
			if got.State == mate.RuntimesDone && (got.StartedAt == "" || got.EndedAt == "") {
				t.Errorf("done without startedAt/endedAt: %+v", got)
			}
		})
	}
}

// TestRun_Failures: what failed reaches the status as one line per service —
// the import refusing a service, a service's process failing, the plan
// itself unreadable.
func TestRun_Failures(t *testing.T) {
	tests := []struct {
		name       string
		plan       string
		setup      func(*fakeAPI)
		wantDetail string
		wantError  string
	}{
		{
			name:       "the import refuses a service",
			plan:       plan(),
			setup:      func(f *fakeAPI) { f.refuse = map[string]string{"appstage": "service stack type not found"} },
			wantDetail: "appdev=running,appstage=failed",
			wantError:  "appstage: serviceStackTypeNotFound: service stack type not found",
		},
		{
			name:       "a service's process fails",
			plan:       plan(),
			setup:      func(f *fakeAPI) { f.failProcess = map[string]string{"appdev": "noFreeIp: no free IP address"} },
			wantDetail: "appdev=failed,appstage=running",
			wantError:  "appdev: stack.create ended FAILED: noFreeIp: no free IP address",
		},
		{
			name:       "the plan is not base64",
			plan:       "%%%",
			setup:      func(*fakeAPI) {},
			wantDetail: "",
			wantError:  "MATE_SETUP_RUNTIMES is not base64",
		},
		{
			name: "the key is refused",
			plan: plan(),
			setup: func(f *fakeAPI) {
				forbidden := platform.NewPlatformError(platform.ErrPermissionDenied, "forbidden", "")
				f.importErr = []error{forbidden, forbidden, forbidden}
			},
			// Nothing was imported, so every listed service is failed.
			wantDetail: "appdev=failed,appstage=failed",
			wantError:  "forbidden",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "status.json")
			api := newFake()
			tt.setup(api)
			importer(api, path).Run(context.Background(), tt.plan)

			got := readStatus(t, path).Runtimes
			if got.State != mate.RuntimesFailed {
				t.Errorf("state = %s, want failed", got.State)
			}
			if states(got) != tt.wantDetail {
				t.Errorf("services = [%s], want [%s]", states(got), tt.wantDetail)
			}
			if !strings.Contains(got.Error, tt.wantError) {
				t.Errorf("error = %q, want it to carry %q", got.Error, tt.wantError)
			}
		})
	}
}

// TestRun_ImportRetries: a call that failed on the way is sent again; one
// the platform refused for what it carried is not — it would answer the same.
func TestRun_ImportRetries(t *testing.T) {
	tests := []struct {
		name        string
		errs        []error
		wantImports int
		wantState   string
	}{
		{"a call that failed on the way is retried", []error{platform.NewPlatformError(platform.ErrNetworkError, "connection reset", "")}, 2, mate.RuntimesDone},
		{"a refused import is not", []error{refusal()}, 1, mate.RuntimesFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "status.json")
			api := newFake()
			api.importErr = tt.errs
			importer(api, path).Run(context.Background(), plan())
			if len(api.imports) != tt.wantImports {
				t.Errorf("imports = %d, want %d", len(api.imports), tt.wantImports)
			}
			if got := readStatus(t, path).Runtimes.State; got != tt.wantState {
				t.Errorf("state = %s, want %s", got, tt.wantState)
			}
		})
	}
}

// TestRun_ACallThatFailedButCreated_IsNotSentAgain: an import call that
// failed on its way back after the platform took it is not sent a second
// time — the retry looks first.
func TestRun_ACallThatFailedButCreated_IsNotSentAgain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	api := newFake()
	api.importErr = []error{platform.NewPlatformError(platform.ErrNetworkError, "connection reset", "")}
	api.createAnyway = true
	importer(api, path).Run(context.Background(), plan())

	if len(api.imports) != 1 {
		t.Errorf("imports = %d, want 1", len(api.imports))
	}
	if got := readStatus(t, path).Runtimes; got.State != mate.RuntimesDone {
		t.Errorf("state = %s (%s), want done", got.State, got.Error)
	}
}

// TestRun_ARestartMidImport_FollowsWithoutImporting: a restart while the
// import's processes run imports nothing and follows them to the end.
func TestRun_ARestartMidImport_FollowsWithoutImporting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	api := newFake()
	for _, h := range []string{"appdev", "appstage"} {
		api.services = append(api.services, platform.ServiceStack{ID: "id-" + h, Name: h})
		api.procs = append(api.procs, platform.Process{ID: "proc-" + h, ActionName: "stack.create", Status: platform.ProcessStatusRunning,
			Created: stampAt(time.Second), ServiceStacks: []platform.ServiceStackRef{{ID: "id-" + h, Name: h}}})
	}
	if err := mate.UpdateStatus(path, func(s *mate.Status) {
		s.Runtimes = mate.RuntimesStatus{State: mate.RuntimesImporting, StartedAt: t0.Format(time.RFC3339)}
	}); err != nil {
		t.Fatal(err)
	}
	importer(api, path).Run(context.Background(), plan())

	if len(api.imports) != 0 {
		t.Errorf("imports = %d, want 0", len(api.imports))
	}
	if got := readStatus(t, path).Runtimes; got.State != mate.RuntimesDone || states(got) != "appdev=running,appstage=running" {
		t.Errorf("runtimes = %s [%s], want done with both running", got.State, states(got))
	}
}

// TestServiceStates reads one look at the project into each listed service.
func TestServiceStates(t *testing.T) {
	since := t0
	reason := "noFreeIp: no free IP address"
	proc := func(id, action, status string, created time.Duration, av *platform.ProcessAppVersion) platform.Process {
		return platform.Process{ID: id, ActionName: action, Status: status, Created: stampAt(created), AppVersion: av,
			ServiceStacks: []platform.ServiceStackRef{{ID: "id-appdev"}}}
	}
	present := []platform.ServiceStack{{ID: "id-appdev", Name: "appdev"}}
	tests := []struct {
		name        string
		live        []platform.ServiceStack
		procs       []platform.Process
		importErrs  map[string]string
		want        mate.RuntimeService
		wantSettled bool
	}{
		{"not there yet", nil, nil, nil, mate.RuntimeService{Hostname: "appdev", State: "creating"}, false},
		{"refused by the import", nil, nil, map[string]string{"appdev": "bad type"}, mate.RuntimeService{Hostname: "appdev", State: "failed", Error: "bad type"}, true},
		{"its create runs", present, []platform.Process{proc("p1", "stack.create", "RUNNING", time.Second, nil)}, nil, mate.RuntimeService{Hostname: "appdev", State: "creating", ProcessID: "p1"}, false},
		{"its build builds", present, []platform.Process{proc("p2", "stack.build", "RUNNING", time.Second, &platform.ProcessAppVersion{Status: "BUILDING"})}, nil, mate.RuntimeService{Hostname: "appdev", State: "building", ProcessID: "p2"}, false},
		{"its build deploys", present, []platform.Process{proc("p2", "stack.build", "RUNNING", time.Second, &platform.ProcessAppVersion{Status: "DEPLOYING"})}, nil, mate.RuntimeService{Hostname: "appdev", State: "deploying", ProcessID: "p2"}, false},
		{"a deploy runs on it", present, []platform.Process{proc("p3", "stack.deploy", "RUNNING", time.Second, nil)}, nil, mate.RuntimeService{Hostname: "appdev", State: "deploying", ProcessID: "p3"}, false},
		{"its create finished", present, []platform.Process{proc("p1", "stack.create", "FINISHED", time.Second, nil)}, nil, mate.RuntimeService{Hostname: "appdev", State: "running"}, true},
		{"its create failed", present, []platform.Process{{ID: "p1", ActionName: "stack.create", Status: "FAILED", Created: stampAt(time.Second), FailReason: &reason, ServiceStacks: []platform.ServiceStackRef{{ID: "id-appdev"}}}}, nil, mate.RuntimeService{Hostname: "appdev", State: "failed", ProcessID: "p1", Error: "stack.create ended FAILED: " + reason}, true},
		{"a failure from before this import", present, []platform.Process{proc("p0", "stack.restart", "FAILED", -time.Hour, nil)}, nil, mate.RuntimeService{Hostname: "appdev", State: "running"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, settled := matesetup.ServiceStates([]string{"appdev"}, tt.live, tt.procs, tt.importErrs, since)
			if len(got) != 1 || got[0] != tt.want || settled != tt.wantSettled {
				t.Errorf("ServiceStates = %+v settled=%v, want %+v settled=%v", got, settled, tt.want, tt.wantSettled)
			}
		})
	}
}

// TestMarkLaunch is the status every mate launch starts from.
func TestMarkLaunch(t *testing.T) {
	tests := []struct {
		name         string
		prior        *mate.Status
		planSet      bool
		wantRuntimes string
		wantStandup  string
	}{
		{"no plan", nil, false, mate.RuntimesNone, mate.StandupIdle},
		{"a plan, first boot", nil, true, mate.RuntimesPending, mate.StandupIdle},
		{"a plan whose import a restart cut", &mate.Status{Runtimes: mate.RuntimesStatus{State: mate.RuntimesImporting}}, true, mate.RuntimesImporting, mate.StandupIdle},
		{"a stand-up the restart stopped", &mate.Status{Standup: mate.StandupStatus{State: mate.StandupRunning}}, false, mate.RuntimesNone, mate.StandupFailed},
		{"a finished stand-up", &mate.Status{Standup: mate.StandupStatus{State: mate.StandupDone}}, false, mate.RuntimesNone, mate.StandupDone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "status.json")
			if tt.prior != nil {
				if err := mate.UpdateStatus(path, func(s *mate.Status) { *s = *tt.prior }); err != nil {
					t.Fatal(err)
				}
			}
			if err := matesetup.MarkLaunch(path, tt.planSet, t0); err != nil {
				t.Fatalf("MarkLaunch: %v", err)
			}
			got := readStatus(t, path)
			if got.Runtimes.State != tt.wantRuntimes || got.Standup.State != tt.wantStandup {
				t.Errorf("runtimes=%s standup=%s, want %s and %s", got.Runtimes.State, got.Standup.State, tt.wantRuntimes, tt.wantStandup)
			}
			if tt.wantStandup == mate.StandupFailed && got.Standup.Error == "" {
				t.Error("a stopped stand-up must say why")
			}
		})
	}
}

// TestBoot_WithoutWhatItActsWith: a plan the container cannot act on fails
// with what is missing, before any call; no plan does nothing at all.
func TestBoot_WithoutWhatItActsWith(t *testing.T) {
	tests := []struct {
		name      string
		env       map[string]string
		wantFile  bool
		wantError string
	}{
		{"no plan", map[string]string{"ZCP_API_KEY": "k", "projectId": "p"}, false, ""},
		{"no key", map[string]string{matesetup.EnvRuntimes: plan(), "projectId": "p"}, true, "ZCP_API_KEY is not on this service"},
		{"no project", map[string]string{matesetup.EnvRuntimes: plan(), "ZCP_API_KEY": "k"}, true, "projectId"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "status.json")
			matesetup.Boot(context.Background(), path, func() func(string) string { return func(k string) string { return tt.env[k] } })
			got, err := mate.ReadStatus(path)
			if (err == nil) != tt.wantFile {
				t.Fatalf("status file written = %v, want %v", err == nil, tt.wantFile)
			}
			if tt.wantFile && (got.Runtimes.State != mate.RuntimesFailed || !strings.Contains(got.Runtimes.Error, tt.wantError)) {
				t.Errorf("runtimes = %s %q, want failed carrying %q", got.Runtimes.State, got.Runtimes.Error, tt.wantError)
			}
		})
	}
}

// TestRun_ImportsOnlyIntoAProjectClosedOff: runtimes run code, and in a
// project whose env isolation is off they read the zcp service's variables —
// the Mate's key among them — while closing it off after they exist restarts
// them. So nothing is imported until Zerops reads the project closed off —
// isolated per service, its key no longer project-wide — whatever HQ holds
// of the Mate's birth (audit N1); the wait says so in the status and has no
// end of its own (a later "Finish setup" closes it). A read that fails or
// trails is one more look. A plan with nothing missing never waits.
func TestRun_ImportsOnlyIntoAProjectClosedOff(t *testing.T) {
	tests := []struct {
		name         string
		existing     []string
		isolation    []string
		wantImports  int
		wantReadsMin int
		wantState    string
	}{
		{"closed off from the start", nil, []string{"closed"}, 1, 1, mate.RuntimesDone},
		{"closed off after a while", nil, []string{"open", "open", "<error>", "open", "closed"}, 1, 5, mate.RuntimesDone},
		{"isolated once its key left the project", nil, []string{"keyed", "keyed", "closed"}, 1, 3, mate.RuntimesDone},
		{"a read that trails the project's birth", nil, []string{"unread", "closed"}, 1, 2, mate.RuntimesDone},
		{"nothing missing never waits", []string{"appdev", "appstage"}, []string{"open"}, 0, 0, mate.RuntimesDone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "status.json")
			api := newFake()
			api.isolation = tt.isolation
			for _, h := range tt.existing {
				api.services = append(api.services, platform.ServiceStack{ID: "id-" + h, Name: h, Status: "ACTIVE"})
			}
			importer(api, path).Run(context.Background(), plan())
			if len(api.imports) != tt.wantImports {
				t.Fatalf("imports = %d, want %d", len(api.imports), tt.wantImports)
			}
			if tt.wantImports > 0 && api.readsAtImport < tt.wantReadsMin {
				t.Errorf("imported after %d isolation reads, want only after the read that found it closed off (%d)", api.readsAtImport, tt.wantReadsMin)
			}
			if got := readStatus(t, path).Runtimes; got.State != tt.wantState {
				t.Errorf("state = %s (%q), want %s", got.State, got.Error, tt.wantState)
			}
		})
	}
}

// TestRun_WaitingToBeClosedOff_SaysSo: while the project is open, the
// runtimes section is pending with the reason, and nothing is imported —
// however long it stays open.
func TestRun_WaitingToBeClosedOff_SaysSo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	api := newFake()
	api.isolation = []string{"open"}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		importer(api, path).Run(ctx, plan())
	}()
	deadline := time.Now().Add(2 * time.Second)
	var got mate.RuntimesStatus
	for time.Now().Before(deadline) {
		if st, err := mate.ReadStatus(path); err == nil && st.Runtimes.Error != "" {
			got = st.Runtimes
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done
	if got.State != mate.RuntimesPending || !strings.Contains(got.Error, "closed off") {
		t.Errorf("while open: %s %q, want pending saying it waits to be closed off", got.State, got.Error)
	}
	if len(api.imports) != 0 {
		t.Errorf("imported %d times into a project that never closed off", len(api.imports))
	}
}

// TestIsolationPoll: a look every 10 s for the first half hour, then every
// minute, for as long as it takes.
func TestIsolationPoll(t *testing.T) {
	tests := []struct {
		waited time.Duration
		want   time.Duration
	}{
		{0, 10 * time.Second},
		{29 * time.Minute, 10 * time.Second},
		{30 * time.Minute, time.Minute},
		{48 * time.Hour, time.Minute},
	}
	for _, tt := range tests {
		if got := matesetup.IsolationPoll(tt.waited); got != tt.want {
			t.Errorf("IsolationPoll(%s) = %s, want %s", tt.waited, got, tt.want)
		}
	}
}

// deleteService has the person delete a service from the project.
func (f *fakeAPI) deleteService(host string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.services = slices.DeleteFunc(f.services, func(s platform.ServiceStack) bool { return s.Name == host })
}

// TestRun_ASettledImportIsNeverRepeated: once the boot import settled — done,
// or refused by the platform — no later launch imports again, so a service
// the person deleted stays deleted. Only an import a passing failure or the
// timeout stopped runs again on the next launch.
func TestRun_ASettledImportIsNeverRepeated(t *testing.T) {
	transient := platform.NewPlatformError(platform.ErrNetworkError, "connection reset", "")
	tests := []struct {
		name         string
		first        func(*fakeAPI)
		between      func(*fakeAPI)
		wantFirst    string
		wantRelaunch string // the runtimes state the next launch's mark leaves
		wantImports  int    // over both launches
		wantFinal    string
	}{
		{
			name:      "done, then the person deletes a service",
			between:   func(f *fakeAPI) { f.deleteService("appstage") },
			wantFirst: mate.RuntimesDone, wantRelaunch: mate.RuntimesDone, wantImports: 1, wantFinal: mate.RuntimesDone,
		},
		{
			name:      "a service refused by the platform",
			first:     func(f *fakeAPI) { f.refuse = map[string]string{"appstage": "service stack type not found"} },
			between:   func(f *fakeAPI) { f.refuse = nil },
			wantFirst: mate.RuntimesFailed, wantRelaunch: mate.RuntimesFailed, wantImports: 1, wantFinal: mate.RuntimesFailed,
		},
		{
			name: "the whole import refused",
			first: func(f *fakeAPI) {
				f.importErr = []error{refusal()}
			},
			wantFirst: mate.RuntimesFailed, wantRelaunch: mate.RuntimesFailed, wantImports: 1, wantFinal: mate.RuntimesFailed,
		},
		{
			name:      "a passing failure runs again",
			first:     func(f *fakeAPI) { f.importErr = []error{transient, transient, transient} },
			wantFirst: mate.RuntimesFailed, wantRelaunch: mate.RuntimesPending, wantImports: 4, wantFinal: mate.RuntimesDone,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "status.json")
			api := newFake()
			if tt.first != nil {
				tt.first(api)
			}
			importer(api, path).Run(context.Background(), plan())
			if got := readStatus(t, path).Runtimes.State; got != tt.wantFirst {
				t.Fatalf("first launch = %s, want %s", got, tt.wantFirst)
			}
			if tt.between != nil {
				tt.between(api)
			}
			if err := matesetup.MarkLaunch(path, true, t0); err != nil {
				t.Fatal(err)
			}
			if got := readStatus(t, path).Runtimes.State; got != tt.wantRelaunch {
				t.Errorf("after the next launch's mark = %s, want %s", got, tt.wantRelaunch)
			}
			importer(api, path).Run(context.Background(), plan())
			if len(api.imports) != tt.wantImports {
				t.Errorf("imports over both launches = %d, want %d", len(api.imports), tt.wantImports)
			}
			if got := readStatus(t, path).Runtimes.State; got != tt.wantFinal {
				t.Errorf("after the next launch = %s, want %s", got, tt.wantFinal)
			}
		})
	}
}

// TestRun_ATimedOutImportIsFollowedAgain: an import the 20-minute bound cut
// is failed, and the next launch follows it again rather than leaving it so.
func TestRun_ATimedOutImportIsFollowedAgain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	api := newFake()
	api.runFor = 1 << 30
	im := importer(api, path)
	im.Timeout = 50 * time.Millisecond
	im.Run(context.Background(), plan())
	if got := readStatus(t, path).Runtimes; got.State != mate.RuntimesFailed || !strings.Contains(got.Error, "in flight") {
		t.Fatalf("first launch = %s %q, want failed in flight", got.State, got.Error)
	}
	api.mu.Lock()
	api.runFor = 0
	api.mu.Unlock()
	if err := matesetup.MarkLaunch(path, true, t0); err != nil {
		t.Fatal(err)
	}
	importer(api, path).Run(context.Background(), plan())
	if got := readStatus(t, path).Runtimes; got.State != mate.RuntimesDone || len(api.imports) != 1 {
		t.Errorf("next launch = %s with %d imports, want done with the one import", got.State, len(api.imports))
	}
}

// TestRun_ANameTakenIsAServiceThatExists: a quick restart can send the
// import while the earlier one's services are being created; the platform
// answers that the name is taken, and that is the service existing, never a
// failure.
func TestRun_ANameTakenIsAServiceThatExists(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*fakeAPI)
	}{
		{"per service, name unavailable", func(f *fakeAPI) {
			f.taken, f.takenCode = map[string]bool{"appstage": true}, "serviceStackNameUnavailable"
		}},
		{"per service, already exists", func(f *fakeAPI) {
			f.taken, f.takenCode = map[string]bool{"appstage": true}, "serviceStackAlreadyExists"
		}},
		{"the whole call, name unavailable", func(f *fakeAPI) {
			f.taken, f.takenCode = map[string]bool{"appdev": true, "appstage": true}, "serviceStackNameUnavailable"
			pe := platform.NewPlatformError(platform.ErrAPIError, "service stack name is not available", "")
			pe.APICode = "serviceStackNameUnavailable"
			f.takenWhole = pe
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "status.json")
			api := newFake()
			tt.setup(api)
			importer(api, path).Run(context.Background(), plan())
			if got := readStatus(t, path).Runtimes; got.State != mate.RuntimesDone {
				t.Errorf("runtimes = %s %q [%s], want done", got.State, got.Error, states(got))
			}
		})
	}
}

// refusal is the platform refusing an import for what it carried: a 4xx
// with the platform's own error code.
func refusal() error {
	pe := platform.NewPlatformError(platform.ErrAPIError, "invalid yaml", "")
	pe.APICode = "projectImportInvalidYaml"
	return pe
}

// TestRun_OnlyARealRefusalSettles: an import settles for good only on the
// platform's own refusal of what it carried. A key the platform does not
// take (401, 403 — rotated, or not granted yet) or an answer whose outcome is
// unknown (EOF, a body that does not decode, a cancelled request) is sent
// again, and runs again on the next launch.
func TestRun_OnlyARealRefusalSettles(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		wantAttempts int
		wantNext     int // imports the next launch sends
	}{
		{"a 4xx refusal", refusal(), 1, 0},
		{"a 403", platform.NewPlatformError(platform.ErrPermissionDenied, "forbidden", ""), 3, 1},
		{"a 401", platform.NewPlatformError(platform.ErrAuthTokenExpired, "unauthorized", ""), 3, 1},
		{"an EOF", errors.New("unexpected EOF"), 3, 1},
		{"a body that does not decode", platform.NewPlatformError(platform.ErrAPIError, "invalid character '<'", ""), 3, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "status.json")
			api := newFake()
			api.importErr = []error{tt.err, tt.err, tt.err}
			importer(api, path).Run(context.Background(), plan())
			if len(api.imports) != tt.wantAttempts {
				t.Errorf("attempts = %d, want %d", len(api.imports), tt.wantAttempts)
			}
			if err := matesetup.MarkLaunch(path, true, t0); err != nil {
				t.Fatal(err)
			}
			before := len(api.imports)
			importer(api, path).Run(context.Background(), plan())
			if got := len(api.imports) - before; got != tt.wantNext {
				t.Errorf("the next launch sent %d imports, want %d", got, tt.wantNext)
			}
		})
	}
}

// TestRun_AnUnreadableProjectSaysSo: a read of the project that fails is
// never read as "not closed off yet": the runtimes section says the project
// could not be read, and why.
func TestRun_AnUnreadableProjectSaysSo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	api := newFake()
	api.isolation = []string{"<error>"}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		importer(api, path).Run(ctx, plan())
	}()
	var got mate.RuntimesStatus
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if st, err := mate.ReadStatus(path); err == nil && st.Runtimes.Error != "" {
			got = st.Runtimes
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if got.State != mate.RuntimesPending || !strings.Contains(got.Error, "could not read whether the project is closed off") || !strings.Contains(got.Error, "Zerops did not answer") {
		t.Errorf("runtimes = %s %q, want pending saying the project could not be read and why", got.State, got.Error)
	}
	if len(api.imports) != 0 {
		t.Errorf("imported %d times into a project it could not read", len(api.imports))
	}
}

// TestFreshAPI_FollowsTheKey: the boot import asks the live env store for
// the Mate's key at every look, so a key rotated while it waits is the one
// the next look uses; an unchanged key keeps its client.
func TestFreshAPI_FollowsTheKey(t *testing.T) {
	keys := []string{"k" + "1", "k" + "1", "k" + "2", "k" + "2"}
	look := 0
	env := func() func(string) string {
		key := keys[min(look, len(keys)-1)]
		look++
		return func(k string) string {
			if k == "ZCP_API_KEY" {
				return key
			}
			return ""
		}
	}
	var built []string
	fresh := matesetup.FreshAPI(env, func(token, _ string) (matesetup.API, error) {
		built = append(built, token)
		return newFake(), nil
	})
	for range keys {
		if fresh() == nil {
			t.Fatal("no client")
		}
	}
	if !slices.Equal(built, []string{"k1", "k2"}) {
		t.Errorf("clients built for %v, want one per key: [k1 k2]", built)
	}
}

// TestMarkLaunch_RepairsTheSectionFromTheSettledRecord: the record that the
// import settled is written before the section that says so; a launch that
// finds the record but a section still importing or pending (a crash between
// the two writes) puts the section back as the import ended.
func TestMarkLaunch_RepairsTheSectionFromTheSettledRecord(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(*fakeAPI)
		torn      string
		wantState string
		wantError string
	}{
		{"done, section left importing", nil, mate.RuntimesImporting, mate.RuntimesDone, ""},
		{"done, section left pending", nil, mate.RuntimesPending, mate.RuntimesDone, ""},
		{"refused, section left importing", func(f *fakeAPI) { f.importErr = []error{refusal()} }, mate.RuntimesImporting, mate.RuntimesFailed, "invalid yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "status.json")
			api := newFake()
			if tt.setup != nil {
				tt.setup(api)
			}
			importer(api, path).Run(context.Background(), plan())
			if err := mate.UpdateRuntimes(path, func(r *mate.RuntimesStatus) { r.State, r.Error, r.EndedAt = tt.torn, "", "" }); err != nil {
				t.Fatal(err)
			}
			if err := matesetup.MarkLaunch(path, true, t0); err != nil {
				t.Fatal(err)
			}
			got := readStatus(t, path).Runtimes
			if got.State != tt.wantState || !strings.Contains(got.Error, tt.wantError) || got.EndedAt == "" {
				t.Errorf("runtimes = %s %q ended %q, want %s carrying %q, with its end", got.State, got.Error, got.EndedAt, tt.wantState, tt.wantError)
			}
		})
	}
}

// TestSettledRecord_ATruncatedRecordIsNotSettled: a record that does not
// parse (a write a crash cut short) is no proof the import settled: the
// launch treats it as a repair — the section runs again, and the run that
// ends it writes a whole record.
func TestSettledRecord_ATruncatedRecordIsNotSettled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	api := newFake()
	importer(api, path).Run(context.Background(), plan())
	record, err := os.ReadFile(matesetup.SettledPath(path))
	if err != nil {
		t.Fatalf("no settled record: %v", err)
	}
	if err := os.WriteFile(matesetup.SettledPath(path), record[:len(record)/2], 0o600); err != nil {
		t.Fatal(err)
	}
	if matesetup.Settled(path) {
		t.Error("a truncated record reads as settled")
	}
	if err := matesetup.MarkLaunch(path, true, t0); err != nil {
		t.Fatal(err)
	}
	if got := readStatus(t, path).Runtimes.State; got != mate.RuntimesPending {
		t.Errorf("after the launch = %s, want pending (to run again)", got)
	}
	importer(api, path).Run(context.Background(), plan())
	if !matesetup.Settled(path) || readStatus(t, path).Runtimes.State != mate.RuntimesDone {
		t.Errorf("the next run did not settle again: settled=%v state=%s", matesetup.Settled(path), readStatus(t, path).Runtimes.State)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("left behind %s", e.Name())
		}
	}
}

// TestRun_AnEmptyPlanSettlesAsNone: the press always sets the plan, empty
// when the tier has no runtimes it can import ("Finish setup" on a Mate whose
// runtimes cannot be recovered). An empty plan is nothing to import, not a
// failure: it settles as none at once — no look at the project, no wait for
// it to be closed off — and stays so on every later launch.
func TestRun_AnEmptyPlanSettlesAsNone(t *testing.T) {
	b64 := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	tests := []struct {
		name string
		plan string
	}{
		{"an empty list", b64("services: []\n")},
		{"no list", b64("services:\n")},
		{"an empty document", b64("{}\n")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "status.json")
			if err := matesetup.MarkLaunch(path, true, t0); err != nil {
				t.Fatal(err)
			}
			// No API: an empty plan never reaches the platform.
			matesetup.Importer{StatusPath: path, ProjectID: "proj", Now: func() time.Time { return t0 }}.Run(context.Background(), tt.plan)
			got := readStatus(t, path).Runtimes
			if got.State != mate.RuntimesNone || got.Error != "" {
				t.Errorf("runtimes = %s %q, want none", got.State, got.Error)
			}
			if err := matesetup.MarkLaunch(path, true, t0); err != nil {
				t.Fatal(err)
			}
			if got := readStatus(t, path).Runtimes.State; got != mate.RuntimesNone || !matesetup.Settled(path) {
				t.Errorf("the next launch = %s (settled=%v), want none for good", got, matesetup.Settled(path))
			}
		})
	}
}

// TestBoot_AnEmptyPlanNeedsNoKey: an empty plan settles as none even where
// the key or the project id is missing — there is nothing to act on.
func TestBoot_AnEmptyPlanNeedsNoKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	env := map[string]string{matesetup.EnvRuntimes: base64.StdEncoding.EncodeToString([]byte("services: []\n"))}
	matesetup.Boot(context.Background(), path, func() func(string) string { return func(k string) string { return env[k] } })
	if got := readStatus(t, path).Runtimes; got.State != mate.RuntimesNone || got.Error != "" {
		t.Errorf("runtimes = %s %q, want none", got.State, got.Error)
	}
}

// TestBoot_AnEmptyPlanOverridesAFailedRecord: v9.187.0 settled an empty plan
// as failed ("lists no services"). Whatever the settled record says, an
// empty plan's outcome is none: the next launch rewrites the record and the
// section, with no file surgery on the Mate.
func TestBoot_AnEmptyPlanOverridesAFailedRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	record := `{"state":"failed","error":"failed: MATE_SETUP_RUNTIMES lists no services","endedAt":"2026-10-01T18:00:00Z","services":null}`
	if err := os.WriteFile(matesetup.SettledPath(path), []byte(record), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := mate.UpdateRuntimes(path, func(r *mate.RuntimesStatus) {
		r.State, r.Error = mate.RuntimesFailed, "MATE_SETUP_RUNTIMES lists no services"
	}); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{matesetup.EnvRuntimes: base64.StdEncoding.EncodeToString([]byte("services: []\n"))}
	for launch := range 2 {
		if err := matesetup.MarkLaunch(path, true, t0); err != nil {
			t.Fatal(err)
		}
		matesetup.Boot(context.Background(), path, func() func(string) string { return func(k string) string { return env[k] } })
		if got := readStatus(t, path).Runtimes; got.State != mate.RuntimesNone || got.Error != "" {
			t.Errorf("after launch %d: runtimes = %s %q, want none", launch+1, got.State, got.Error)
		}
	}
	if !matesetup.Settled(path) {
		t.Error("the rewritten record does not read as settled")
	}
}

// ownDeployAPI is the project while zcp's own first deploy still runs: the
// zcp service reads `status` and its stack.build process stays live until
// the look `doneAt`; an import sent before that is recorded as overlapping.
type ownDeployAPI struct {
	*fakeAPI
	status      string
	doneAt      int
	overlapping int
}

func (o *ownDeployAPI) ownLive() bool { return o.looks < o.doneAt }

func (o *ownDeployAPI) ListServicesDirect(ctx context.Context, id string) ([]platform.ServiceStack, error) {
	live, err := o.fakeAPI.ListServicesDirect(ctx, id)
	o.mu.Lock()
	defer o.mu.Unlock()
	status := "ACTIVE"
	if o.ownLive() {
		status = o.status
	}
	return append(live, platform.ServiceStack{ID: "id-zcp", Name: "zcp", Status: status}), err
}

func (o *ownDeployAPI) GetProjectProcessesDirect(ctx context.Context, id string) ([]platform.Process, error) {
	procs, err := o.fakeAPI.GetProjectProcessesDirect(ctx, id)
	o.mu.Lock()
	defer o.mu.Unlock()
	status := platform.ProcessStatusFinished
	if o.ownLive() {
		status = platform.ProcessStatusRunning
	}
	return append(procs, platform.Process{ID: "proc-own", ActionName: "stack.build", Status: status, Created: stampAt(0),
		ServiceStacks: []platform.ServiceStackRef{{ID: "id-zcp", Name: "zcp"}}}), err
}

func (o *ownDeployAPI) ImportServices(ctx context.Context, id, body string) (*platform.ImportResult, error) {
	o.mu.Lock()
	if o.ownLive() {
		o.overlapping++
	}
	o.mu.Unlock()
	return o.fakeAPI.ImportServices(ctx, id, body)
}

// TestRun_NothingIsImportedWhileZcpsOwnDeployRuns: a services import sent
// into the project while zcp's own first deploy was still running left
// zcp's app version without its user data, and the next restart ran no init
// and refused mate. The boot import waits — runtimes pending, saying why —
// until the zcp service reads ACTIVE with no live process on it.
func TestRun_NothingIsImportedWhileZcpsOwnDeployRuns(t *testing.T) {
	tests := []struct {
		name   string
		status string
	}{
		{"the service is still being created", "CREATING"},
		{"the service reads active while its build process runs", "ACTIVE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "status.json")
			api := &ownDeployAPI{fakeAPI: newFake(), status: tt.status, doneAt: 6}
			im := importer(api.fakeAPI, path)
			im.API = api
			im.SelfServiceID = "id-zcp"
			var sawWaiting bool
			stop := make(chan struct{})
			watched := make(chan struct{})
			go func() {
				defer close(watched)
				for {
					if st, err := mate.ReadStatus(path); err == nil && st.Runtimes.State == mate.RuntimesPending && st.Runtimes.Error == mate.RuntimesWaitingOwnDeploy {
						sawWaiting = true
					}
					select {
					case <-stop:
						return
					case <-time.After(time.Millisecond):
					}
				}
			}()
			im.Run(context.Background(), plan())
			close(stop)
			<-watched
			if api.overlapping != 0 {
				t.Errorf("%d imports were sent while zcp's own deploy ran", api.overlapping)
			}
			if len(api.imports) != 1 {
				t.Errorf("imports = %d, want 1 once the deploy ended", len(api.imports))
			}
			if got := readStatus(t, path).Runtimes.State; got != mate.RuntimesDone {
				t.Errorf("runtimes = %s, want done", got)
			}
			if !sawWaiting {
				t.Error("the runtimes section never said it waits for zcp's own deploy")
			}
		})
	}
}
