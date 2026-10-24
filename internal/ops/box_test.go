// Tests for: ops/box.go — finishing a Zerops Box workspace's setup (a READY
// default base, the 8081 sandbox domain) and reading it back.
package ops

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

const (
	testBoxHost    = "box1"
	testBoxID      = "svc-box"
	testBoxIngress = "box1-3860-8081.prg1.zerops.app"
	testBoxDigest  = "sha256:da2f61de16533fd55d06058a5f72701d9c57ad65cf630f9d441ca5fbee38a21f"
)

// fakeBox is a Box workspace API the tests control. It records every call in
// order and mints its access through the minter ops hands the factory, so a
// minter bound to the wrong service fails the test.
type fakeBox struct {
	mu     sync.Mutex
	calls  []string
	mint   platform.BoxMinter
	minted bool

	pingErr     error // every ping answers this
	pingErrOnce int   // the first n pings answer serviceStackNoActiveContainer

	bases    []platform.BoxBase
	basesErr error
	ops      []platform.BoxOperation

	pullErrs []error // the pulls answer these in order, then succeed
	pullOp   platform.BoxOperation
	pullReqs []platform.BoxPullBaseRequest
	pullKeys []string

	// opAnswers are what the polls answer in order; the last one repeats.
	opAnswers []platform.BoxOperation
	polled    []string

	workspace platform.BoxWorkspace
	puts      [][]platform.BoxDomain
}

func (f *fakeBox) record(call string) {
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()
}

// access mints once, as the real client keeps its access for its calls.
func (f *fakeBox) access(ctx context.Context) error {
	f.mu.Lock()
	minted := f.minted
	f.mu.Unlock()
	if minted {
		return nil
	}
	if _, err := f.mint(ctx); err != nil {
		return err
	}
	f.mu.Lock()
	f.minted = true
	f.mu.Unlock()
	return nil
}

func (f *fakeBox) Ping(ctx context.Context) (platform.BoxPing, error) {
	f.record("Ping")
	if err := f.access(ctx); err != nil {
		return platform.BoxPing{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pingErr != nil {
		return platform.BoxPing{}, f.pingErr
	}
	if f.pingErrOnce > 0 {
		f.pingErrOnce--
		return platform.BoxPing{}, &platform.PlatformError{Code: platform.ErrAPIError, APICode: "serviceStackNoActiveContainer", Message: "Service stack has no active container."}
	}
	return platform.BoxPing{Version: "zerops-v3.1.0"}, nil
}

func (f *fakeBox) Workspace(ctx context.Context) (platform.BoxWorkspace, error) {
	f.record("Workspace")
	if err := f.access(ctx); err != nil {
		return platform.BoxWorkspace{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.workspace, nil
}

func (f *fakeBox) Bases(ctx context.Context) ([]platform.BoxBase, error) {
	f.record("Bases")
	if err := f.access(ctx); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bases, f.basesErr
}

func (f *fakeBox) Operations(ctx context.Context, kind string) ([]platform.BoxOperation, error) {
	f.record("Operations:" + kind)
	if err := f.access(ctx); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ops, nil
}

func (f *fakeBox) PullBase(ctx context.Context, req platform.BoxPullBaseRequest, key string) (platform.BoxOperation, error) {
	f.record("PullBase")
	if err := f.access(ctx); err != nil {
		return platform.BoxOperation{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pullReqs = append(f.pullReqs, req)
	f.pullKeys = append(f.pullKeys, key)
	if len(f.pullErrs) > 0 {
		err := f.pullErrs[0]
		f.pullErrs = f.pullErrs[1:]
		return platform.BoxOperation{}, err
	}
	return f.pullOp, nil
}

func (f *fakeBox) Operation(ctx context.Context, id string, _ time.Duration) (platform.BoxOperation, error) {
	f.record("Operation:" + id)
	if err := f.access(ctx); err != nil {
		return platform.BoxOperation{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.polled = append(f.polled, id)
	if len(f.opAnswers) == 0 {
		return platform.BoxOperation{ID: id, Status: platform.BoxOperationFailed, Error: "fakeBox: no poll was expected"}, nil
	}
	op := f.opAnswers[0]
	if len(f.opAnswers) > 1 {
		f.opAnswers = f.opAnswers[1:]
	}
	return op, nil
}

func (f *fakeBox) PutDomains(ctx context.Context, domains []platform.BoxDomain) (platform.BoxWorkspace, error) {
	f.record("PutDomains")
	if err := f.access(ctx); err != nil {
		return platform.BoxWorkspace{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.puts = append(f.puts, slices.Clone(domains))
	f.workspace.Domains = slices.Clone(domains)
	return f.workspace, nil
}

func (f *fakeBox) callList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeBox) mutations() []string {
	var out []string
	for _, c := range f.callList() {
		if c == "PullBase" || c == "PutDomains" {
			out = append(out, c)
		}
	}
	return out
}

// testBoxService is a Box as a direct service read returns it once its
// import finished: ACTIVE, subdomain access on.
func testBoxService() platform.ServiceStack {
	return platform.ServiceStack{
		ID: testBoxID, Name: testBoxHost, ProjectID: "proj-1", Status: platform.ServiceStatusActive, SubdomainAccess: true,
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "ubuntu/zbox@1", ServiceStackTypeCategoryName: "STANDARD"},
	}
}

// testBoxMock is a project holding the Box (and an app runtime), its
// zeropsSubdomain as the platform writes it (API host first, ingress second),
// and a ui-access for the Box only.
func testBoxMock(svc platform.ServiceStack) *platform.Mock {
	return platform.NewMock().
		WithServices([]platform.ServiceStack{
			svc,
			{ID: "svc-app", Name: "app", Status: platform.ServiceStatusActive, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		}).
		WithServiceEnv(testBoxID, []platform.ServiceEnvVar{
			{Key: "zeropsSubdomain", Content: "https://box1-3860-8080.prg1.zerops.app\nhttps://" + testBoxIngress},
			{Key: "ZBOX_WORKSPACE_API_KEY", Content: platform.MaskedEnvValue, Sensitive: true},
		}).
		WithBoxAccess(testBoxID, platform.BoxAccess{Token: "access-token-1", URLAPI: "https://proxy.app-prg1.zerops.io/service-api"})
}

// testBoxDeps wires the fake with budgets short enough for a unit test.
func testBoxDeps(mock *platform.Mock, fake *fakeBox) BoxDeps {
	return BoxDeps{
		Client: mock, ProjectID: "proj-1",
		Box: func(mint platform.BoxMinter) BoxAPI {
			fake.mint = mint
			return fake
		},
		Timing: BoxTiming{ReadyWait: 60 * time.Millisecond, ReadyEvery: time.Millisecond, PullWait: 60 * time.Millisecond, PollWait: time.Millisecond},
		NewKey: func() string { return "zcp-box-key-1" },
	}
}

func readyBase() platform.BoxBase {
	return platform.BoxBase{Name: BoxDefaultBaseName, Status: platform.BoxBaseStatusReady, Digest: testBoxDigest, ImageRef: BoxDefaultBaseImageRef}
}

func finishedPull(id string) platform.BoxOperation {
	return platform.BoxOperation{ID: id, Kind: platform.BoxOperationKindPullBase, Status: platform.BoxOperationFinished,
		Result: []byte(`{"digest":"` + testBoxDigest + `","name":"default"}`)}
}

func runningPull(id string) platform.BoxOperation {
	return platform.BoxOperation{ID: id, Kind: platform.BoxOperationKindPullBase, Status: platform.BoxOperationRunning}
}

func TestBoxSetup_FreshWorkspace_PullsBaseThenAddsDomain(t *testing.T) {
	t.Parallel()
	fake := &fakeBox{
		pullOp: runningPull("op-1"),
		// A replayed key answers the first answer verbatim, stale status
		// included (live, 2026-10-24): only the poll tells it finished.
		opAnswers: []platform.BoxOperation{runningPull("op-1"), finishedPull("op-1")},
	}
	got, err := BoxSetup(context.Background(), testBoxDeps(testBoxMock(testBoxService()), fake), testBoxHost)
	if err != nil {
		t.Fatalf("BoxSetup: %v", err)
	}

	want := []string{"Ping", "Bases", "Operations:PULL_BASE", "PullBase", "Operation:op-1", "Operation:op-1", "Workspace", "PutDomains"}
	if calls := fake.callList(); !slices.Equal(calls, want) {
		t.Errorf("calls = %v, want %v (wait for the orchestrator, pull, poll the operation, then add the domain)", calls, want)
	}
	if len(fake.pullReqs) != 1 || fake.pullReqs[0] != (platform.BoxPullBaseRequest{ImageRef: BoxDefaultBaseImageRef, Name: BoxDefaultBaseName}) {
		t.Errorf("pull = %+v, want the default image named default", fake.pullReqs)
	}
	if len(fake.pullKeys) == 0 || fake.pullKeys[0] == "" {
		t.Error("the pull carries no Idempotency-Key")
	}
	wantPut := []platform.BoxDomain{{Domain: testBoxIngress, Form: platform.BoxDomainFormDash}}
	if len(fake.puts) != 1 || !slices.Equal(fake.puts[0], wantPut) {
		t.Errorf("domains put = %v, want %v", fake.puts, wantPut)
	}

	if got.Hostname != testBoxHost || got.Status != BoxSetupReady {
		t.Errorf("result = %+v, want %s READY", got, testBoxHost)
	}
	if got.Base == nil || got.Base.Name != BoxDefaultBaseName || got.Base.Status != platform.BoxBaseStatusReady || got.Base.Digest != testBoxDigest || !got.Base.Pulled {
		t.Errorf("base = %+v, want default READY %s pulled by this call", got.Base, testBoxDigest)
	}
	if got.Domain == nil || got.Domain.Domain != testBoxIngress || got.Domain.Form != platform.BoxDomainFormDash || got.Domain.State != BoxDomainAdded {
		t.Errorf("domain = %+v, want %s DASH added", got.Domain, testBoxIngress)
	}
	if len(got.Problems) != 0 {
		t.Errorf("problems = %+v, want none", got.Problems)
	}
	if !strings.Contains(got.Orchestrator, "answering") {
		t.Errorf("orchestrator = %q, want it answering", got.Orchestrator)
	}
	for _, want := range []string{"/api/rest/sandbox", testBoxDigest, "${box1_ZBOX_WORKSPACE_API_KEY}", "zerops_box action=status serviceHostname=box1"} {
		if !strings.Contains(got.Next, want) {
			t.Errorf("next = %q, want it to name %q", got.Next, want)
		}
	}
}

func TestBoxSetup_AlreadySetUp_NoMutations(t *testing.T) {
	t.Parallel()
	fake := &fakeBox{
		bases:     []platform.BoxBase{readyBase()},
		workspace: platform.BoxWorkspace{Domains: []platform.BoxDomain{{Domain: testBoxIngress, Form: platform.BoxDomainFormDash}}},
	}
	got, err := BoxSetup(context.Background(), testBoxDeps(testBoxMock(testBoxService()), fake), testBoxHost)
	if err != nil {
		t.Fatalf("BoxSetup: %v", err)
	}
	if m := fake.mutations(); len(m) != 0 {
		t.Errorf("mutations = %v, want none on a workspace already set up", m)
	}
	if calls := fake.callList(); !slices.Equal(calls, []string{"Ping", "Bases", "Workspace"}) {
		t.Errorf("calls = %v, want Ping, Bases, Workspace only (no pull to look for once a READY base exists)", calls)
	}
	if got.Status != BoxSetupReady || got.Base == nil || got.Base.Pulled || got.Base.Digest != testBoxDigest {
		t.Errorf("result = %+v / base %+v, want READY with the existing base, not pulled", got, got.Base)
	}
	if got.Domain == nil || got.Domain.State != BoxDomainPresent || got.Domain.Domain != testBoxIngress {
		t.Errorf("domain = %+v, want %s present", got.Domain, testBoxIngress)
	}
}

func TestBoxSetup_ExistingDomains_Merged(t *testing.T) {
	t.Parallel()
	own := platform.BoxDomain{Domain: "sandboxes.example.com", Form: "DOT"}
	fake := &fakeBox{
		bases:     []platform.BoxBase{readyBase()},
		workspace: platform.BoxWorkspace{Domains: []platform.BoxDomain{own}},
	}
	got, err := BoxSetup(context.Background(), testBoxDeps(testBoxMock(testBoxService()), fake), testBoxHost)
	if err != nil {
		t.Fatalf("BoxSetup: %v", err)
	}
	want := []platform.BoxDomain{own, {Domain: testBoxIngress, Form: platform.BoxDomainFormDash}}
	if len(fake.puts) != 1 || !slices.Equal(fake.puts[0], want) {
		t.Errorf("domains put = %v, want %v (the PUT replaces the whole list: keep every domain already there)", fake.puts, want)
	}
	if got.Domain == nil || got.Domain.State != BoxDomainAdded {
		t.Errorf("domain = %+v, want added", got.Domain)
	}
}

func TestBoxSetup_PullStillRunning_ReportsPullingWithRecovery(t *testing.T) {
	t.Parallel()
	fake := &fakeBox{pullOp: runningPull("op-7"), opAnswers: []platform.BoxOperation{runningPull("op-7")}}
	got, err := BoxSetup(context.Background(), testBoxDeps(testBoxMock(testBoxService()), fake), testBoxHost)
	if err != nil {
		t.Fatalf("BoxSetup: %v", err)
	}
	if got.Status != BoxSetupPulling {
		t.Errorf("status = %s, want PULLING", got.Status)
	}
	if got.Base == nil || got.Base.Status != platform.BoxBaseStatusPulling || got.Base.Operation != "op-7" || !got.Base.Pulled {
		t.Errorf("base = %+v, want PULLING with operation op-7", got.Base)
	}
	for _, want := range []string{"op-7", "zerops_box action=setup serviceHostname=box1"} {
		if !strings.Contains(got.Next, want) {
			t.Errorf("next = %q, want it to name %q", got.Next, want)
		}
	}
	// The domain does not wait for the base: a re-run then only finishes the pull.
	if got.Domain == nil || got.Domain.State != BoxDomainAdded {
		t.Errorf("domain = %+v, want added while the pull runs", got.Domain)
	}
}

func TestBoxSetup_PullInProgress_PollsItInsteadOfPullingAgain(t *testing.T) {
	t.Parallel()
	pulling := platform.BoxBase{Name: BoxDefaultBaseName, Status: platform.BoxBaseStatusPulling, Digest: testBoxDigest}
	fake := &fakeBox{
		bases:     []platform.BoxBase{pulling},
		ops:       []platform.BoxOperation{runningPull("op-9"), {ID: "op-3", Kind: platform.BoxOperationKindPullBase, Status: platform.BoxOperationFailed}},
		opAnswers: []platform.BoxOperation{finishedPull("op-9")},
		workspace: platform.BoxWorkspace{Domains: []platform.BoxDomain{{Domain: testBoxIngress, Form: platform.BoxDomainFormDash}}},
	}
	got, err := BoxSetup(context.Background(), testBoxDeps(testBoxMock(testBoxService()), fake), testBoxHost)
	if err != nil {
		t.Fatalf("BoxSetup: %v", err)
	}
	if m := fake.mutations(); len(m) != 0 {
		t.Errorf("mutations = %v, want none: the running pull is polled, never started again", m)
	}
	if !slices.Equal(fake.polled, []string{"op-9"}) {
		t.Errorf("polled = %v, want the running op-9", fake.polled)
	}
	if got.Status != BoxSetupReady || got.Base == nil || got.Base.Digest != testBoxDigest || got.Base.Pulled {
		t.Errorf("result = %+v / base %+v, want READY with the finished pull's digest, not pulled by this call", got, got.Base)
	}
}

func TestBoxSetup_PullingBaseWithoutAnOperation_NeverPullsAgain(t *testing.T) {
	t.Parallel()
	fake := &fakeBox{
		bases:     []platform.BoxBase{{Name: BoxDefaultBaseName, Status: platform.BoxBaseStatusFlattening}},
		workspace: platform.BoxWorkspace{Domains: []platform.BoxDomain{{Domain: testBoxIngress, Form: platform.BoxDomainFormDash}}},
	}
	got, err := BoxSetup(context.Background(), testBoxDeps(testBoxMock(testBoxService()), fake), testBoxHost)
	if err != nil {
		t.Fatalf("BoxSetup: %v", err)
	}
	if m := fake.mutations(); len(m) != 0 {
		t.Errorf("mutations = %v, want none while a base is on its way", m)
	}
	if got.Status != BoxSetupPulling || !strings.Contains(got.Next, "zerops_box action=setup serviceHostname=box1") {
		t.Errorf("result = %+v, want PULLING naming the recovery call", got)
	}
}

func TestBoxSetup_PullTransportFailure_RetriesWithTheSameKey(t *testing.T) {
	t.Parallel()
	fake := &fakeBox{
		pullErrs:  []error{platform.NewPlatformError(platform.ErrNetworkError, "the Box API did not answer", "")},
		pullOp:    runningPull("op-1"),
		opAnswers: []platform.BoxOperation{finishedPull("op-1")},
	}
	got, err := BoxSetup(context.Background(), testBoxDeps(testBoxMock(testBoxService()), fake), testBoxHost)
	if err != nil {
		t.Fatalf("BoxSetup: %v", err)
	}
	if !slices.Equal(fake.pullKeys, []string{"zcp-box-key-1", "zcp-box-key-1"}) {
		t.Errorf("pull keys = %v, want the same key on the retry (a lost answer must not pull twice)", fake.pullKeys)
	}
	if got.Status != BoxSetupReady {
		t.Errorf("status = %s, want READY", got.Status)
	}
}

func TestBoxSetup_Failures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// svc edits the Box as read; fake and mock set the failure up.
		svc        func(*platform.ServiceStack)
		fake       *fakeBox
		mock       func(*platform.Mock)
		wantStatus string
		wantCalls  []string // nil: not checked
		wantIn     []string // problem + fix + orchestrator text
	}{
		{
			name:       "a stopped Box is not asked at all",
			svc:        func(s *platform.ServiceStack) { s.Status = platform.ServiceStatusStopped },
			fake:       &fakeBox{},
			wantStatus: BoxSetupFailed, wantCalls: []string{},
			wantIn: []string{"stopped", "zerops_manage action=start serviceHostname=box1", "zerops_box action=setup serviceHostname=box1"},
		},
		{
			name:       "the orchestrator never answers within the budget",
			fake:       &fakeBox{pingErr: &platform.PlatformError{Code: platform.ErrAPIError, APICode: "serviceStackNoActiveContainer", Message: "Service stack has no active container."}},
			wantStatus: BoxSetupFailed,
			wantIn:     []string{"did not answer", "serviceStackNoActiveContainer", "zerops_logs serviceHostname=box1", "zerops_box action=setup serviceHostname=box1"},
		},
		{
			name:       "zproxy refuses a freshly minted access",
			fake:       &fakeBox{pingErr: &platform.BoxError{Status: http.StatusUnauthorized, Code: platform.BoxErrorUnauthorized, Message: "Authorization: Bearer <access token> is required."}},
			wantStatus: BoxSetupFailed, wantCalls: []string{"Ping"},
			wantIn: []string{"401", "zerops_manage action=reload serviceHostname=box1", "zerops_box action=setup serviceHostname=box1"},
		},
		{
			name: "zcp's token may not reach the Box",
			fake: &fakeBox{},
			mock: func(m *platform.Mock) {
				m.WithError("MintBoxAccess", &platform.PlatformError{Code: platform.ErrPermissionDenied, APICode: "insufficientPermissions", Message: "Insufficient permissions."})
			},
			wantStatus: BoxSetupFailed, wantCalls: []string{"Ping"},
			wantIn: []string{"BasicUser"},
		},
		{
			name: "the Box is gone",
			fake: &fakeBox{},
			mock: func(m *platform.Mock) {
				m.WithError("MintBoxAccess", &platform.PlatformError{Code: platform.ErrAPIError, APICode: "serviceStackNotFound", Message: "Service stack not found."})
			},
			wantStatus: BoxSetupFailed, wantCalls: []string{"Ping"},
			wantIn: []string{"gone", "zerops_discover"},
		},
		{
			name: "the workspace has no room for the pull",
			fake: &fakeBox{pullErrs: []error{&platform.BoxError{Status: http.StatusConflict, Code: platform.BoxErrorWorkspaceFull, Message: "workspace full",
				Metadata: map[string]string{"resource": "memory", "requested": "536870912", "free": "0"}}}},
			wantStatus: BoxSetupFailed,
			wantIn:     []string{"workspaceFull", "memory", "zerops_scale", "zerops_box action=setup serviceHostname=box1"},
		},
		{
			name: "the pull fails",
			fake: &fakeBox{pullOp: runningPull("op-1"), opAnswers: []platform.BoxOperation{
				{ID: "op-1", Kind: platform.BoxOperationKindPullBase, Status: platform.BoxOperationFailed, Error: "fetch image: 404 Not Found"}}},
			wantStatus: BoxSetupFailed,
			wantIn:     []string{"fetch image: 404 Not Found", "zerops_box action=setup serviceHostname=box1"},
		},
		{
			name: "subdomain access on but no 8081 host written",
			fake: &fakeBox{bases: []platform.BoxBase{readyBase()}},
			mock: func(m *platform.Mock) {
				m.WithServiceEnv(testBoxID, []platform.ServiceEnvVar{{Key: "hostname", Content: testBoxHost}})
			},
			wantStatus: BoxSetupReady,
			wantIn:     []string{"zeropsSubdomain", "zerops_box action=setup serviceHostname=box1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := testBoxService()
			if tt.svc != nil {
				tt.svc(&svc)
			}
			mock := testBoxMock(svc)
			if tt.mock != nil {
				tt.mock(mock)
			}
			got, err := BoxSetup(context.Background(), testBoxDeps(mock, tt.fake), testBoxHost)
			if err != nil {
				t.Fatalf("BoxSetup: %v (a setup failure is named in the result, never an error)", err)
			}
			if got.Status != tt.wantStatus {
				t.Errorf("status = %s, want %s: %+v", got.Status, tt.wantStatus, got)
			}
			if len(got.Problems) == 0 {
				t.Fatalf("no problem named: %+v", got)
			}
			text := got.Orchestrator
			for _, p := range got.Problems {
				if p.Problem == "" || p.Fix == "" {
					t.Errorf("problem %+v does not name both what failed and its fix", p)
				}
				text += " | " + p.Problem + " | " + p.Fix
			}
			for _, want := range tt.wantIn {
				if !strings.Contains(text, want) {
					t.Errorf("the result does not say %q: %s", want, text)
				}
			}
			if tt.wantCalls != nil {
				if calls := tt.fake.callList(); !slices.Equal(calls, tt.wantCalls) {
					t.Errorf("calls = %v, want %v", calls, tt.wantCalls)
				}
			}
		})
	}
}

func TestBoxSetup_OrchestratorLate_WaitsForIt(t *testing.T) {
	t.Parallel()
	// Right after ACTIVE the ui-access mint can answer
	// serviceStackNoActiveContainer for a moment (the zbox CLI retries it).
	fake := &fakeBox{pingErrOnce: 3, bases: []platform.BoxBase{readyBase()},
		workspace: platform.BoxWorkspace{Domains: []platform.BoxDomain{{Domain: testBoxIngress, Form: platform.BoxDomainFormDash}}}}
	got, err := BoxSetup(context.Background(), testBoxDeps(testBoxMock(testBoxService()), fake), testBoxHost)
	if err != nil {
		t.Fatalf("BoxSetup: %v", err)
	}
	if got.Status != BoxSetupReady {
		t.Errorf("status = %s, want READY once the orchestrator answers: %+v", got.Status, got.Problems)
	}
	if n := strings.Count(strings.Join(fake.callList(), ","), "Ping"); n != 4 {
		t.Errorf("pinged %d times, want 4 (three refusals, then the answer)", n)
	}
}

func TestBoxSetup_SubdomainOff_ReportsRecovery(t *testing.T) {
	t.Parallel()
	svc := testBoxService()
	svc.SubdomainAccess = false
	fake := &fakeBox{bases: []platform.BoxBase{readyBase()}}
	got, err := BoxSetup(context.Background(), testBoxDeps(testBoxMock(svc), fake), testBoxHost)
	if err != nil {
		t.Fatalf("BoxSetup: %v", err)
	}
	if slices.Contains(fake.callList(), "PutDomains") {
		t.Error("setup registered a domain although subdomain access is off (and never turns it on)")
	}
	if got.Status != BoxSetupReady {
		t.Errorf("status = %s, want READY: the base is there, only the public URLs are missing", got.Status)
	}
	wantRecovery := []string{"zerops_subdomain action=enable serviceHostname=box1", "zerops_box action=setup serviceHostname=box1"}
	if got.Domain == nil || got.Domain.State != BoxDomainNone || !slices.Equal(got.Domain.Recovery, wantRecovery) || !strings.Contains(got.Domain.Reason, "subdomain access is off") {
		t.Errorf("domain = %+v, want none, why, and the recovery %v", got.Domain, wantRecovery)
	}
	if len(got.Problems) != 1 || !strings.Contains(got.Problems[0].Fix, wantRecovery[0]) {
		t.Errorf("problems = %+v, want the subdomain one with its fix", got.Problems)
	}
}

func TestBoxSetup_NotABox_Refused(t *testing.T) {
	t.Parallel()
	fake := &fakeBox{}
	_, err := BoxSetup(context.Background(), testBoxDeps(testBoxMock(testBoxService()), fake), "app")
	var pe *platform.PlatformError
	if !errors.As(err, &pe) || pe.Code != platform.ErrInvalidParameter {
		t.Fatalf("err = %v, want INVALID_PARAMETER", err)
	}
	if !strings.Contains(pe.Message, "nodejs@22") || !strings.Contains(pe.Message, "not a Zerops Box") {
		t.Errorf("message = %q, want it to name what the service is", pe.Message)
	}
	if len(fake.callList()) != 0 {
		t.Errorf("calls = %v, want none", fake.callList())
	}
}

func TestBoxSetupImported_OnlyABox(t *testing.T) {
	t.Parallel()
	fake := &fakeBox{bases: []platform.BoxBase{readyBase()},
		workspace: platform.BoxWorkspace{Domains: []platform.BoxDomain{{Domain: testBoxIngress, Form: platform.BoxDomainFormDash}}}}
	d := testBoxDeps(testBoxMock(testBoxService()), fake)
	if got := BoxSetupImported(context.Background(), d, "app", "svc-app"); got != nil {
		t.Errorf("an app runtime got a setup block: %+v", got)
	}
	got := BoxSetupImported(context.Background(), d, testBoxHost, testBoxID)
	if got == nil || got.Status != BoxSetupReady {
		t.Errorf("the Box's block = %+v, want READY", got)
	}
}

func TestBoxStatus_ReadsWithoutChangingAnything(t *testing.T) {
	t.Parallel()
	svc := testBoxService()
	svc.SubdomainAccess = false
	fake := &fakeBox{
		bases:     []platform.BoxBase{{Name: BoxDefaultBaseName, Status: platform.BoxBaseStatusPulling}},
		ops:       []platform.BoxOperation{runningPull("op-4")},
		workspace: platform.BoxWorkspace{Domains: []platform.BoxDomain{{Domain: "sandboxes.example.com", Form: "DOT"}}},
	}
	got, err := BoxStatus(context.Background(), testBoxDeps(testBoxMock(svc), fake), testBoxHost)
	if err != nil {
		t.Fatalf("BoxStatus: %v", err)
	}
	if m := fake.mutations(); len(m) != 0 {
		t.Errorf("mutations = %v, want none: status only reads", m)
	}
	if got.Hostname != testBoxHost || got.ServiceStatus != platform.ServiceStatusActive || !strings.Contains(got.Orchestrator, "answering") {
		t.Errorf("status = %+v", got)
	}
	if len(got.Bases) != 1 || got.Bases[0].Status != platform.BoxBaseStatusPulling {
		t.Errorf("bases = %+v, want the pulling base", got.Bases)
	}
	if got.Pull == nil || got.Pull.Operation != "op-4" {
		t.Errorf("pull = %+v, want the running op-4", got.Pull)
	}
	if len(got.Domains) != 1 || got.Domains[0].Domain != "sandboxes.example.com" {
		t.Errorf("domains = %+v", got.Domains)
	}
	if got.Subdomain.Access || got.Subdomain.Registered || got.Subdomain.IngressHost != testBoxIngress {
		t.Errorf("subdomain = %+v, want access off, the ingress host known, not registered", got.Subdomain)
	}
	if !strings.Contains(got.Next, "zerops_subdomain action=enable serviceHostname=box1") || !strings.Contains(got.Next, "zerops_box action=setup serviceHostname=box1") {
		t.Errorf("next = %q, want the calls that finish the setup", got.Next)
	}
}

func TestBoxStatus_StoppedBox_NotAsked(t *testing.T) {
	t.Parallel()
	svc := testBoxService()
	svc.Status = platform.ServiceStatusStopped
	fake := &fakeBox{}
	got, err := BoxStatus(context.Background(), testBoxDeps(testBoxMock(svc), fake), testBoxHost)
	if err != nil {
		t.Fatalf("BoxStatus: %v", err)
	}
	if len(fake.callList()) != 0 {
		t.Errorf("calls = %v, want none for a stopped Box", fake.callList())
	}
	if len(got.Problems) == 0 || !strings.Contains(got.Problems[0].Fix, "zerops_manage action=start serviceHostname=box1") {
		t.Errorf("problems = %+v, want the stopped Box named with its fix", got.Problems)
	}
}

func TestBoxIngressHost_FromZeropsSubdomain(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, value, want string
	}{
		{"API host first, ingress second", "https://box1-3860-8080.prg1.zerops.app\nhttps://box1-3860-8081.prg1.zerops.app", "box1-3860-8081.prg1.zerops.app"},
		{"the 8081 host wins whatever its line", "https://box1-3860-8081.prg1.zerops.app\nhttps://box1-3860-8080.prg1.zerops.app", "box1-3860-8081.prg1.zerops.app"},
		{"line 2 when no host names 8081", "https://a.example\r\nhttps://b.example/\n", "b.example"},
		{"one line names no ingress host", "https://box1-3860-8080.prg1.zerops.app", ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := boxIngressHost(tt.value); got != tt.want {
				t.Errorf("boxIngressHost(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestImportedBoxServices_BothSpellings(t *testing.T) {
	t.Parallel()
	yaml := "services:\n" +
		"  - hostname: box1\n    type: ubuntu/zbox@1\n" +
		"  - hostname: box2\n    type: zbox@1\n" +
		"  - hostname: app\n    type: nodejs@22\n" +
		"  - hostname: pool\n    type: swarm@1\n"
	got, err := ImportedBoxServices(yaml, "")
	if err != nil {
		t.Fatalf("ImportedBoxServices: %v", err)
	}
	if !slices.Equal(got, []string{"box1", "box2"}) {
		t.Errorf("boxes = %v, want [box1 box2]", got)
	}
}
