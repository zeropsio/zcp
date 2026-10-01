package matesetup_test

import (
	"context"
	"encoding/base64"
	"errors"
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
	// isolation is envIsolation as each read answers it, the last one
	// staying; empty is "service". isolationReads counts the reads, and
	// readsAtImport is that count when the first import was sent.
	isolation      []string
	isolationReads int
	readsAtImport  int
}

func (f *fakeAPI) GetProjectEnv(context.Context, string) ([]platform.ProjectEnvVar, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	value := "service"
	if len(f.isolation) > 0 {
		value = f.isolation[min(f.isolationReads, len(f.isolation)-1)]
	}
	f.isolationReads++
	if value == "<error>" {
		return nil, errors.New("project env read failed")
	}
	return []platform.ProjectEnvVar{{Key: "zeropsSubdomainHost", Content: "x"}, {Key: "envIsolation", Content: value, Type: platform.ProjectEnvSystem}}, nil
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
	return result, nil
}

func newFake() *fakeAPI { return &fakeAPI{runFor: 2, started: map[string]int{}} }

func importer(api matesetup.API, path string) matesetup.Importer {
	return matesetup.Importer{
		API: api, ProjectID: "proj", StatusPath: path,
		Poll: time.Millisecond, Timeout: 5 * time.Second,
		Backoff:       []time.Duration{time.Millisecond, time.Millisecond},
		IsolationPoll: func(time.Duration) time.Duration { return time.Millisecond },
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
		{
			name:      "a finished import is left as it ended",
			existing:  []string{"appdev", "appstage"},
			prior:     &mate.RuntimesStatus{State: mate.RuntimesFailed, Error: "appstage: earlier"},
			wantState: mate.RuntimesFailed, wantDetail: "",
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
				f.importErr = []error{platform.NewPlatformError(platform.ErrPermissionDenied, "forbidden", "")}
			},
			// Nothing was imported, so every listed service is failed.
			wantDetail: "appdev=failed,appstage=failed",
			wantError:  "the import was refused: forbidden",
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
		{"a refused import is not", []error{platform.NewPlatformError(platform.ErrAPIError, "invalid yaml", "")}, 1, mate.RuntimesFailed},
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
		{"a plan an earlier boot finished", &mate.Status{Runtimes: mate.RuntimesStatus{State: mate.RuntimesDone}}, true, mate.RuntimesDone, mate.StandupIdle},
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
			matesetup.Boot(context.Background(), path, func(k string) string { return tt.env[k] })
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
// them. So nothing is imported until the project reads closed off; the wait
// says so in the status and has no end of its own (a later "Finish setup"
// closes it). A plan with nothing missing never waits.
func TestRun_ImportsOnlyIntoAProjectClosedOff(t *testing.T) {
	tests := []struct {
		name         string
		existing     []string
		isolation    []string
		wantImports  int
		wantReadsMin int
		wantState    string
	}{
		{"closed off from the start", nil, []string{"service"}, 1, 1, mate.RuntimesDone},
		{"closed off after a while", nil, []string{"none", "none", "<error>", "none", "service"}, 1, 5, mate.RuntimesDone},
		{"closed off with a per-service override", nil, []string{"service service@zcp"}, 1, 1, mate.RuntimesDone},
		{"nothing missing never waits", []string{"appdev", "appstage"}, []string{"none"}, 0, 0, mate.RuntimesDone},
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
	api.isolation = []string{"none"}
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
