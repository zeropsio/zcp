// Tests for: box.go — zerops_box (setup / status of a Zerops Box) and the
// zerops_import hook that finishes a freshly created Box's setup.

package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

const (
	boxToolHost    = "box1"
	boxToolID      = "svc-box"
	boxToolIngress = "box1-3860-8081.prg1.zerops.app"
	boxToolDigest  = "sha256:da2f61de16533fd55d06058a5f72701d9c57ad65cf630f9d441ca5fbee38a21f"
	// boxToolToken is the sentinel zproxy access token the mock mints.
	boxToolToken = "zbxACCESSsentinel-tools-0001"
	// boxCapacityReason is how the platform words a Box create rolled back
	// for capacity (live, ny1, 2026-10-24).
	boxCapacityReason = "containerAction: Sorry, there is currently not enough hardware capacity to run your container. If the issue persists, please contact support."
)

// toolFakeBox is a Box workspace API: a fresh workspace (no base, no domain)
// unless seeded, whose pull finishes at the first poll. It mints through the
// minter it was built with and records what it is asked.
type toolFakeBox struct {
	mu        sync.Mutex
	mint      platform.BoxMinter
	calls     []string
	pingErr   error
	bases     []platform.BoxBase
	domains   []platform.BoxDomain
	builtWith int
}

func (f *toolFakeBox) factory() ops.BoxFactory {
	return func(mint platform.BoxMinter) ops.BoxAPI {
		f.mu.Lock()
		f.mint = mint
		f.builtWith++
		f.mu.Unlock()
		return f
	}
}

func (f *toolFakeBox) enter(ctx context.Context, call string) error {
	f.mu.Lock()
	f.calls = append(f.calls, call)
	mint := f.mint
	f.mu.Unlock()
	_, err := mint(ctx)
	return err
}

func (f *toolFakeBox) Ping(ctx context.Context) (platform.BoxPing, error) {
	if err := f.enter(ctx, "Ping"); err != nil {
		return platform.BoxPing{}, err
	}
	return platform.BoxPing{Version: "v"}, f.pingErr
}

func (f *toolFakeBox) Workspace(ctx context.Context) (platform.BoxWorkspace, error) {
	if err := f.enter(ctx, "Workspace"); err != nil {
		return platform.BoxWorkspace{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return platform.BoxWorkspace{Domains: slices.Clone(f.domains)}, nil
}

func (f *toolFakeBox) Bases(ctx context.Context) ([]platform.BoxBase, error) {
	if err := f.enter(ctx, "Bases"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.bases), nil
}

func (f *toolFakeBox) Operations(ctx context.Context, _ string) ([]platform.BoxOperation, error) {
	if err := f.enter(ctx, "Operations"); err != nil {
		return nil, err
	}
	return nil, nil
}

func (f *toolFakeBox) PullBase(ctx context.Context, _ platform.BoxPullBaseRequest, _ string) (platform.BoxOperation, error) {
	if err := f.enter(ctx, "PullBase"); err != nil {
		return platform.BoxOperation{}, err
	}
	return platform.BoxOperation{ID: "op-1", Kind: platform.BoxOperationKindPullBase, Status: platform.BoxOperationRunning}, nil
}

func (f *toolFakeBox) Operation(ctx context.Context, id string, _ time.Duration) (platform.BoxOperation, error) {
	if err := f.enter(ctx, "Operation"); err != nil {
		return platform.BoxOperation{}, err
	}
	f.mu.Lock()
	f.bases = []platform.BoxBase{{Name: "default", Status: platform.BoxBaseStatusReady, Digest: boxToolDigest}}
	f.mu.Unlock()
	return platform.BoxOperation{ID: id, Kind: platform.BoxOperationKindPullBase, Status: platform.BoxOperationFinished,
		Result: []byte(`{"digest":"` + boxToolDigest + `","name":"default"}`)}, nil
}

func (f *toolFakeBox) PutDomains(ctx context.Context, domains []platform.BoxDomain) (platform.BoxWorkspace, error) {
	if err := f.enter(ctx, "PutDomains"); err != nil {
		return platform.BoxWorkspace{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.domains = slices.Clone(domains)
	return platform.BoxWorkspace{Domains: slices.Clone(domains)}, nil
}

func (f *toolFakeBox) callList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *toolFakeBox) mutations() []string {
	var out []string
	for _, c := range f.callList() {
		if c == "PullBase" || c == "PutDomains" {
			out = append(out, c)
		}
	}
	return out
}

func boxToolService() platform.ServiceStack {
	return platform.ServiceStack{
		ID: boxToolID, Name: boxToolHost, ProjectID: "proj-1", Status: platform.ServiceStatusActive, SubdomainAccess: true,
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "ubuntu/zbox@1", ServiceStackTypeCategoryName: "STANDARD"},
	}
}

func boxToolMock(urlAPI string) *platform.Mock {
	return platform.NewMock().
		WithServices([]platform.ServiceStack{
			boxToolService(),
			{ID: "svc-api", Name: "api", Status: platform.ServiceStatusActive, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		}).
		WithServiceEnv(boxToolID, []platform.ServiceEnvVar{{Key: "zeropsSubdomain", Content: "https://box1-3860-8080.prg1.zerops.app\nhttps://" + boxToolIngress}}).
		WithBoxAccess(boxToolID, platform.BoxAccess{Token: boxToolToken, URLAPI: urlAPI})
}

// boxToolDeps wires box with budgets short enough for a test.
func boxToolDeps(mock *platform.Mock, box ops.BoxFactory) ops.BoxDeps {
	return ops.BoxDeps{
		Client: mock, ProjectID: "proj-1", Box: box,
		Timing: ops.BoxTiming{ReadyWait: 50 * time.Millisecond, ReadyEvery: time.Millisecond, PullWait: 200 * time.Millisecond, PollWait: time.Millisecond},
	}
}

type boxToolResponse struct {
	Action    string               `json:"action"`
	BoxSetup  *ops.BoxSetupResult  `json:"boxSetup"`
	BoxStatus *ops.BoxStatusResult `json:"boxStatus"`
}

func TestBoxTool_Setup_FinishesAFreshWorkspace(t *testing.T) {
	t.Parallel()
	fake := &toolFakeBox{}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	registerBox(srv, boxToolDeps(boxToolMock("https://proxy.example/service-api"), fake.factory()))

	res := callTool(t, srv, "zerops_box", map[string]any{"action": "setup", "serviceHostname": boxToolHost})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("unexpected error: %s", text)
	}
	var got boxToolResponse
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	if got.Action != "setup" || got.BoxSetup == nil {
		t.Fatalf("result = %s, want action setup with a boxSetup block", text)
	}
	b := got.BoxSetup
	if b.Hostname != boxToolHost || b.Status != ops.BoxSetupReady || b.Base == nil || b.Base.Digest != boxToolDigest ||
		b.Domain == nil || b.Domain.Domain != boxToolIngress || b.Domain.State != ops.BoxDomainAdded {
		t.Errorf("boxSetup = %s, want READY with the base and the ingress domain added", text)
	}
	if m := fake.mutations(); !slices.Equal(m, []string{"PullBase", "PutDomains"}) {
		t.Errorf("mutations = %v, want the pull then the domain", m)
	}

	// A second setup finds everything in place and changes nothing.
	again := callTool(t, srv, "zerops_box", map[string]any{"action": "setup", "serviceHostname": boxToolHost})
	if again.IsError {
		t.Fatalf("second setup: %s", getTextContent(t, again))
	}
	if m := fake.mutations(); len(m) != 2 {
		t.Errorf("mutations after the second setup = %v, want still the two of the first", m)
	}
}

func TestBoxTool_Status_ReadsOnly(t *testing.T) {
	t.Parallel()
	fake := &toolFakeBox{bases: []platform.BoxBase{{Name: "default", Status: platform.BoxBaseStatusReady, Digest: boxToolDigest}}}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	registerBox(srv, boxToolDeps(boxToolMock("https://proxy.example/service-api"), fake.factory()))

	res := callTool(t, srv, "zerops_box", map[string]any{"action": "status", "serviceHostname": boxToolHost})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("unexpected error: %s", text)
	}
	var got boxToolResponse
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("result is not JSON: %v", err)
	}
	if got.Action != "status" || got.BoxStatus == nil || len(got.BoxStatus.Bases) != 1 || !got.BoxStatus.Subdomain.Access || got.BoxStatus.Subdomain.Registered {
		t.Fatalf("result = %s, want status with the base, subdomain access on, the ingress host not registered", text)
	}
	if !strings.Contains(got.BoxStatus.Next, "zerops_box action=setup serviceHostname=box1") {
		t.Errorf("next = %q, want the setup call for the missing domain", got.BoxStatus.Next)
	}
	if m := fake.mutations(); len(m) != 0 {
		t.Errorf("mutations = %v, want none", m)
	}
}

func TestBoxTool_Refusals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		args     map[string]any
		wantCode string
		wantIn   []string
	}{
		{"no hostname", map[string]any{"action": "setup"}, platform.ErrInvalidParameter, []string{"serviceHostname"}},
		{"an unknown action names the ones there are", map[string]any{"action": "sandbox", "serviceHostname": boxToolHost}, platform.ErrInvalidParameter, []string{"setup", "status"}},
		{"a service that is not a Box", map[string]any{"action": "setup", "serviceHostname": "api"}, platform.ErrInvalidParameter, []string{"nodejs@22", "not a Zerops Box"}},
		{"a hostname the project lacks", map[string]any{"action": "status", "serviceHostname": "nope"}, platform.ErrServiceNotFound, []string{"nope"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := &toolFakeBox{}
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			registerBox(srv, boxToolDeps(boxToolMock("https://proxy.example/service-api"), fake.factory()))
			res := callTool(t, srv, "zerops_box", tt.args)
			text := getTextContent(t, res)
			if !res.IsError {
				t.Fatalf("want a refusal, got %s", text)
			}
			if !strings.Contains(text, tt.wantCode) {
				t.Errorf("refusal %s, want code %s", text, tt.wantCode)
			}
			for _, want := range tt.wantIn {
				if !strings.Contains(text, want) {
					t.Errorf("refusal %s does not say %q", text, want)
				}
			}
			if len(fake.callList()) != 0 {
				t.Errorf("the Box API was asked %v for a refused call", fake.callList())
			}
		})
	}
}

// boxImportMock is an import that created box1 (stack.create FINISHED, its
// subdomain enabled), box2 (rolled back for capacity) and an app runtime.
func boxImportMock(urlAPI string) *platform.Mock {
	capacity := boxCapacityReason
	return boxToolMock(urlAPI).
		WithImportResult(&platform.ImportResult{
			ProjectID: "proj-1", ProjectName: "lab",
			ServiceStacks: []platform.ImportedServiceStack{
				{ID: boxToolID, Name: boxToolHost, Processes: []platform.Process{
					{ID: "p-box1-create", ActionName: "stack.create", Status: "PENDING"},
					{ID: "p-box1-sub", ActionName: "stack.enableSubdomainAccess", Status: "PENDING"},
				}},
				{ID: "svc-box2", Name: "box2", Processes: []platform.Process{{ID: "p-box2-create", ActionName: "stack.create", Status: "PENDING"}}},
				{ID: "svc-api", Name: "api", Processes: []platform.Process{{ID: "p-api", ActionName: "stack.create", Status: "PENDING"}}},
			},
		}).
		WithProcess(&platform.Process{ID: "p-box1-create", ActionName: "stack.create", Status: statusFinished}).
		WithProcess(&platform.Process{ID: "p-box1-sub", ActionName: "stack.enableSubdomainAccess", Status: statusFinished}).
		WithProcess(&platform.Process{ID: "p-box2-create", ActionName: "stack.create", Status: statusFailed, FailReason: &capacity}).
		WithProcess(&platform.Process{ID: "p-api", ActionName: "stack.create", Status: statusFinished})
}

const boxImportYAML = "services:\n" +
	"  - hostname: box1\n    type: ubuntu/zbox@1\n    enableSubdomainAccess: true\n" +
	"  - hostname: box2\n    type: zbox@1\n" +
	"  - hostname: api\n    type: nodejs@22\n    startWithoutCode: true\n"

type importBoxResponse struct {
	ops.ImportResult
	BoxSetup []ops.BoxSetupResult `json:"boxSetup"`
}

func TestImportTool_BoxSetup_OnlyForFinishedBoxes(t *testing.T) {
	t.Parallel()
	mock := boxImportMock("https://proxy.example/service-api")
	fake := &toolFakeBox{}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterImport(srv, mock, "proj-1", testEngine(t), "", nil, runtime.Info{}, withBoxSetupDeps(boxToolDeps(mock, fake.factory())))

	res := callTool(t, srv, "zerops_import", map[string]any{"content": boxImportYAML})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("unexpected error: %s", text)
	}
	var got importBoxResponse
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("result is not JSON: %v", err)
	}
	if len(got.BoxSetup) != 1 || got.BoxSetup[0].Hostname != boxToolHost || got.BoxSetup[0].Status != ops.BoxSetupReady {
		t.Fatalf("boxSetup = %+v, want exactly box1's block, READY (box2 failed on capacity, api is no Box)", got.BoxSetup)
	}
	if fake.builtWith != 1 || mock.CallCount("MintBoxAccess") == 0 {
		t.Errorf("Box clients built = %d, mints = %d: want one client, for box1 only", fake.builtWith, mock.CallCount("MintBoxAccess"))
	}
	for _, p := range got.Processes {
		if p.Service == "box2" && (p.Status != statusFailed || p.FailReason == nil || *p.FailReason != boxCapacityReason) {
			t.Errorf("box2's process = %+v, want its FAILED capacity reason kept as it was", p)
		}
	}
	if !strings.Contains(got.Summary, "1 failed") {
		t.Errorf("summary = %q, want the failed create still counted", got.Summary)
	}
}

func TestImportTool_BoxSetupFailure_ImportStillSucceeds(t *testing.T) {
	t.Parallel()
	mock := boxImportMock("https://proxy.example/service-api").
		WithError("MintBoxAccess", &platform.PlatformError{Code: platform.ErrAPIError, APICode: "serviceStackNoActiveContainer", Message: "Service stack has no active container."})
	fake := &toolFakeBox{}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterImport(srv, mock, "proj-1", testEngine(t), "", nil, runtime.Info{}, withBoxSetupDeps(boxToolDeps(mock, fake.factory())))

	res := callTool(t, srv, "zerops_import", map[string]any{"content": boxImportYAML})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("a failed Box setup failed the import: %s", text)
	}
	var got importBoxResponse
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("result is not JSON: %v", err)
	}
	if len(got.BoxSetup) != 1 || got.BoxSetup[0].Status != ops.BoxSetupFailed || len(got.BoxSetup[0].Problems) == 0 {
		t.Fatalf("boxSetup = %+v, want box1 FAILED with its problem named", got.BoxSetup)
	}
	if !strings.Contains(got.BoxSetup[0].Problems[0].Fix, "zerops_box action=setup serviceHostname=box1") {
		t.Errorf("fix = %q, want the recovery call", got.BoxSetup[0].Problems[0].Fix)
	}
}

func TestImportTool_NoBox_NoSetupBlock(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithImportResult(&platform.ImportResult{ProjectID: "proj-1", ServiceStacks: []platform.ImportedServiceStack{
			{ID: "svc-api", Name: "api", Processes: []platform.Process{{ID: "p-api", ActionName: "stack.create", Status: "PENDING"}}},
		}}).
		WithProcess(&platform.Process{ID: "p-api", ActionName: "stack.create", Status: statusFinished})
	fake := &toolFakeBox{}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterImport(srv, mock, "proj-1", testEngine(t), "", nil, runtime.Info{}, withBoxSetupDeps(boxToolDeps(mock, fake.factory())))

	text := getTextContent(t, callTool(t, srv, "zerops_import", map[string]any{"content": "services:\n  - hostname: api\n    type: nodejs@22\n"}))
	if strings.Contains(text, "boxSetup") || fake.builtWith != 0 || mock.CallCount("MintBoxAccess") != 0 {
		t.Errorf("an import with no Box touched Box setup (built %d, mints %d): %s", fake.builtWith, mock.CallCount("MintBoxAccess"), text)
	}
}

// TestImportTool_BoxSetup_ResultNeverCarriesToken runs the real Box client
// against a fake Box API that echoes the bearer it is sent in a refusal.
// non-parallel: it swaps os.Stdout and os.Stderr to read every line zcp
// prints while the import runs.
func TestImportTool_BoxSetup_ResultNeverCarriesToken(t *testing.T) {
	var mu sync.Mutex
	var auths []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auths = append(auths, r.Header.Get("Authorization"))
		mu.Unlock()
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/service-api/api/rest/ping":
			_, _ = w.Write([]byte(`{"time":"t","version":"v"}`))
		case "/service-api/api/rest/base-list", "/service-api/api/rest/operation-list":
			_, _ = w.Write([]byte(`{"items":[]}`))
		case "/service-api/api/rest/base":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":{"code":"workspaceFull","message":"no room for ` + r.Header.Get("Authorization") + `",` +
				`"meta":[{"code":"workspaceFull","metadata":{"resource":["memory"],"requested":["536870912"],"free":["0"]}}]}}`))
		case "/service-api/api/rest/workspace":
			_, _ = w.Write([]byte(`{"domains":[],"version":"v"}`))
		case "/service-api/api/rest/workspace/domains":
			_, _ = w.Write([]byte(`{"domains":[{"domain":"` + boxToolIngress + `","form":"DASH"}],"version":"v"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer api.Close()

	mock := boxImportMock(api.URL + "/service-api")
	deps := boxToolDeps(mock, ops.NewBoxFactory(api.Client()))
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterImport(srv, mock, "proj-1", testEngine(t), "", nil, runtime.Info{}, withBoxSetupDeps(deps))
	registerBox(srv, deps)

	printed := captureStdio(t, func() {
		texts := []string{
			getTextContent(t, callTool(t, srv, "zerops_import", map[string]any{"content": boxImportYAML})),
			getTextContent(t, callTool(t, srv, "zerops_box", map[string]any{"action": "setup", "serviceHostname": boxToolHost})),
			getTextContent(t, callTool(t, srv, "zerops_box", map[string]any{"action": "status", "serviceHostname": boxToolHost})),
		}
		for i, text := range texts {
			if strings.Contains(text, boxToolToken) || strings.Contains(text, "service-ui") || strings.Contains(text, "accessToken") {
				t.Errorf("result %d carries the access token: %s", i, text)
			}
			if !strings.Contains(text, boxToolHost) {
				t.Errorf("result %d does not name the Box: %s", i, text)
			}
		}
		if !strings.Contains(texts[0], "workspaceFull") {
			t.Errorf("the import's block does not name the refusal it relays: %s", texts[0])
		}
	})
	if strings.Contains(printed, boxToolToken) {
		t.Errorf("zcp printed the access token: %s", printed)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(auths) == 0 || !slices.Contains(auths, "Bearer "+boxToolToken) {
		t.Errorf("the Box API never saw the access token as the bearer: %v", auths)
	}
}

// captureStdio runs fn with os.Stdout and os.Stderr read into the returned
// text.
func captureStdio(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = w, w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	defer func() {
		os.Stdout, os.Stderr = origOut, origErr
	}()
	fn()
	os.Stdout, os.Stderr = origOut, origErr
	_ = w.Close()
	return <-done
}
