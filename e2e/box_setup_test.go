//go:build e2e

// Tests for: e2e — a Zerops Box imported through zerops_import is set up by
// the time the call returns: a READY default base and its 8081 Zerops
// subdomain as the first sandbox domain, reached through zproxy (ui-access)
// with zcp's own Zerops token; zerops_box re-runs it without a mutation and
// reports subdomain access turned off with the calls that finish it.
//
// Box runs in prg1 only. A create the platform rolls back for hardware
// capacity skips the test with that reason rather than failing it.
//
// Prerequisites:
//   - ZCP_API_KEY set (a token with BasicUser or above on the project)
//   - ZCP_E2E_PROJECT_ID pins the project when the token reaches several
//
// Run: go test ./e2e/ -tags e2e -run TestE2E_Import_Box_FinishesSetup -v -timeout 1200s

package e2e_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/server"
)

// boxCapacityFailure is how the platform words a create it rolled back for
// lack of hardware (live, ny1, 2026-10-24).
const boxCapacityFailure = "not enough hardware capacity"

// newBoxHarness is newHarness with the project pinned by ZCP_E2E_PROJECT_ID
// when it is set: a personal token reaches several projects, and a Box test
// must land in the one it was given.
func newBoxHarness(t *testing.T) *e2eHarness {
	t.Helper()
	pinned := os.Getenv("ZCP_E2E_PROJECT_ID")
	if pinned == "" {
		return newHarness(t)
	}
	token := os.Getenv("ZCP_API_KEY")
	if token == "" {
		t.Skip("ZCP_API_KEY not set — skipping E2E test")
	}
	client, err := platform.NewZeropsClient(token, defaultAPIHost())
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	project, err := client.GetProject(ctx, pinned)
	if err != nil {
		t.Fatalf("read the pinned project: %v", err)
	}
	client.BindOrg(project.ClientID)
	region := os.Getenv("ZCP_REGION")
	if region == "" {
		region = "prg1"
	}
	authInfo := &auth.Info{Token: token, APIHost: defaultAPIHost(), Region: region, ClientID: project.ClientID, ProjectID: project.ID, ProjectName: project.Name}
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}
	srv := server.New(context.Background(), client, authInfo, store, platform.NewLogFetcher(), nil, nil, runtime.Info{}, nil)
	return &e2eHarness{t: t, client: client, projectID: project.ID, authInfo: authInfo, srv: srv}
}

// boxObservation is what an independent reader saw of the workspace while
// the import's setup ran: whether base-list shows a base during its pull, and
// whether operation-list shows the running pull — and how long after the
// import call started each was first seen.
type boxObservation struct {
	mu           sync.Mutex
	started      time.Time
	reads        int
	baseStatuses map[string]int
	runningPulls int
	firstPulling time.Duration
	firstRunning time.Duration
}

func (o *boxObservation) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return fmt.Sprintf("reads=%d baseStatusesSeen=%v readsWithARunningPull=%d firstPullingBaseAt=%s firstRunningPullOpAt=%s",
		o.reads, o.baseStatuses, o.runningPulls, o.firstPulling.Round(time.Second), o.firstRunning.Round(time.Second))
}

// record notes one read of base-list and operation-list.
func (o *boxObservation) record(bases []platform.BoxBase, pulls []platform.BoxOperation) {
	o.mu.Lock()
	defer o.mu.Unlock()
	since := time.Since(o.started)
	o.reads++
	for _, b := range bases {
		o.baseStatuses[b.Status]++
		if (b.Status == platform.BoxBaseStatusPulling || b.Status == platform.BoxBaseStatusFlattening) && o.firstPulling == 0 {
			o.firstPulling = since
		}
	}
	for _, p := range pulls {
		if p.Status == platform.BoxOperationRunning || p.Status == platform.BoxOperationPending {
			o.runningPulls++
			if o.firstRunning == 0 {
				o.firstRunning = since
			}
			break
		}
	}
}

// boxClientFor reaches the Box serviceID through its own zproxy accesses.
func boxClientFor(h *e2eHarness, serviceID string) *platform.BoxClient {
	return platform.NewBoxClient(func(ctx context.Context) (platform.BoxAccess, error) {
		return h.client.MintBoxAccess(ctx, serviceID)
	}, &http.Client{Timeout: 60 * time.Second})
}

// observeBox reads base-list and operation-list once a second through its
// own zproxy access until ctx ends. It never mutates.
func observeBox(ctx context.Context, h *e2eHarness, hostname string, obs *boxObservation) {
	var api *platform.BoxClient
	for ctx.Err() == nil {
		if api == nil {
			if services, err := h.client.ListServicesDirect(ctx, h.projectID); err == nil {
				for _, s := range services {
					if s.Name == hostname {
						api = boxClientFor(h, s.ID)
					}
				}
			}
		}
		if api != nil {
			bases, berr := api.Bases(ctx)
			pulls, perr := api.Operations(ctx, platform.BoxOperationKindPullBase)
			if berr == nil && perr == nil {
				obs.record(bases, pulls)
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
	}
}

type boxImportResult struct {
	Processes []struct {
		ActionName string  `json:"actionName"`
		Status     string  `json:"status"`
		Service    string  `json:"service"`
		ServiceID  string  `json:"serviceId"`
		FailReason *string `json:"failReason"`
	} `json:"processes"`
	BoxSetup []ops.BoxSetupResult `json:"boxSetup"`
}

type boxToolResult struct {
	BoxSetup  *ops.BoxSetupResult  `json:"boxSetup"`
	BoxStatus *ops.BoxStatusResult `json:"boxStatus"`
}

func TestE2E_Import_Box_FinishesSetup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E live-platform test in short mode: it imports a Box and pulls its base")
	}
	h := newBoxHarness(t)
	s := newSession(t, h.srv)
	host := "tmpbox" + randomSuffix()[:4]

	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()
		cleanupServices(cctx, h.client, h.projectID, host)
		s.callTool("zerops_workflow", map[string]any{"action": "reset"})
	})

	logStep(t, 1, "import %s as ubuntu/zbox@1 with enableSubdomainAccess", host)
	serviceID, importText, block := importBox(t, h, s, host)

	logStep(t, 2, "the import result carries %s's setup: a pulled READY default base, the 8081 domain added", host)
	ingress := ingressFromEnv(t, h, serviceID)
	assertFreshSetup(t, block, ingress)

	logStep(t, 3, "read the workspace independently through a fresh ui-access")
	api := boxClientFor(h, serviceID)
	pulls := assertIndependentReads(t, h, api, serviceID, importText, block, ingress)

	logStep(t, 4, "zerops_box action=setup again: nothing to do")
	assertSecondSetupChangesNothing(t, s, api, host, pulls)

	logStep(t, 5, "subdomain access off: setup reports it with the recovery calls and keeps the domain; enable on a Box works, then setup finds the domain")
	assertSubdomainCycle(t, h, s, api, host, serviceID, ingress)

	logStep(t, 6, "delete %s and read back serviceStackNotFound", host)
	deleteBoxAndReadBack(t, h, serviceID)
}

// importBox imports host as a Box through zerops_import while an independent
// reader watches the workspace, and answers the service id, the result text
// and the Box's setup block. A capacity rollback skips the test.
func importBox(t *testing.T, h *e2eHarness, s *e2eSession, host string) (string, string, ops.BoxSetupResult) {
	t.Helper()
	s.callTool("zerops_workflow", map[string]any{"action": "reset"})
	s.mustCallSuccess("zerops_workflow", map[string]any{"action": "start", "workflow": "bootstrap", "route": "classic", "intent": t.Name()})

	obs := &boxObservation{baseStatuses: map[string]int{}, started: time.Now()}
	octx, stopObserving := context.WithCancel(context.Background())
	observed := make(chan struct{})
	go func() {
		defer close(observed)
		observeBox(octx, h, host, obs)
	}()
	importText := s.mustCallSuccess("zerops_import", map[string]any{
		"content": "services:\n  - hostname: " + host + "\n    type: ubuntu/zbox@1\n    enableSubdomainAccess: true\n",
	})
	took := time.Since(obs.started)
	stopObserving()
	<-observed
	t.Logf("  zerops_import returned after %s; independent reads while it ran: %s", took.Round(time.Second), obs)

	var imported boxImportResult
	if err := json.Unmarshal([]byte(importText), &imported); err != nil {
		t.Fatalf("parse import: %v\n%s", err, importText)
	}
	serviceID := ""
	for _, p := range imported.Processes {
		if p.Service != host || p.ActionName != "stack.create" {
			continue
		}
		serviceID = p.ServiceID
		if p.Status == "FINISHED" {
			continue
		}
		if p.FailReason != nil && strings.Contains(*p.FailReason, boxCapacityFailure) {
			if len(imported.BoxSetup) != 0 {
				t.Errorf("a create rolled back for capacity got a setup block: %+v", imported.BoxSetup)
			}
			t.Skipf("Box create rolled back for hardware capacity: %s", *p.FailReason)
		}
		t.Fatalf("stack.create = %s, want FINISHED: %s", p.Status, importText)
	}
	if serviceID == "" {
		t.Fatalf("no stack.create for %s in %s", host, importText)
	}
	if len(imported.BoxSetup) != 1 {
		t.Fatalf("boxSetup = %+v, want one block: %s", imported.BoxSetup, importText)
	}
	block := imported.BoxSetup[0]
	t.Logf("  boxSetup: status=%s orchestrator=%q base=%+v domain=%+v problems=%+v next=%q", block.Status, block.Orchestrator, block.Base, block.Domain, block.Problems, block.Next)
	return serviceID, importText, block
}

func assertFreshSetup(t *testing.T, block ops.BoxSetupResult, ingress string) {
	t.Helper()
	if block.Status != ops.BoxSetupReady || block.Base == nil || block.Base.Name != "default" || block.Base.Status != platform.BoxBaseStatusReady ||
		!strings.HasPrefix(block.Base.Digest, "sha256:") || !block.Base.Pulled {
		t.Fatalf("block = %+v / base %+v, want READY with a pulled default base", block, block.Base)
	}
	if block.Domain == nil || block.Domain.Domain != ingress || block.Domain.Form != platform.BoxDomainFormDash || block.Domain.State != ops.BoxDomainAdded {
		t.Errorf("domain = %+v, want %s DASH added (the 8081 host of zeropsSubdomain)", block.Domain, ingress)
	}
}

// assertIndependentReads checks the workspace as a fresh access reads it,
// and answers how many pulls it lists.
func assertIndependentReads(t *testing.T, h *e2eHarness, api *platform.BoxClient, serviceID, importText string, block ops.BoxSetupResult, ingress string) int {
	t.Helper()
	access, err := h.client.MintBoxAccess(context.Background(), serviceID)
	if err != nil {
		t.Fatalf("mint ui-access: %v", err)
	}
	if code := rawPing(t, access.URLAPI, ""); code != http.StatusUnauthorized {
		t.Errorf("zproxy ping without the token = %d, want 401", code)
	}
	if code := rawPing(t, access.URLAPI, access.Token.Secret()); code != http.StatusOK {
		t.Errorf("zproxy ping with the token = %d, want 200", code)
	}
	bases := mustBases(t, api)
	if len(bases) != 1 || bases[0].Name != "default" || bases[0].Status != platform.BoxBaseStatusReady || bases[0].Digest != block.Base.Digest {
		t.Errorf("base-list = %+v, want one READY default %s", bases, block.Base.Digest)
	}
	domains := mustDomains(t, api)
	if len(domains) != 1 || domains[0] != (platform.BoxDomain{Domain: ingress, Form: platform.BoxDomainFormDash}) {
		t.Errorf("domains = %+v, want [{%s DASH}]", domains, ingress)
	}
	for _, secret := range []string{access.Token.Secret(), "accessToken", "service-ui"} {
		if strings.Contains(importText, secret) {
			t.Errorf("the import result carries %q", secret)
		}
	}
	return mustPullCount(t, api)
}

func assertSecondSetupChangesNothing(t *testing.T, s *e2eSession, api *platform.BoxClient, host string, pulls int) {
	t.Helper()
	again := decodeBox(t, s.mustCallSuccess("zerops_box", map[string]any{"action": "setup", "serviceHostname": host}))
	if again.BoxSetup == nil || again.BoxSetup.Status != ops.BoxSetupReady || again.BoxSetup.Base == nil || again.BoxSetup.Base.Pulled ||
		again.BoxSetup.Domain == nil || again.BoxSetup.Domain.State != ops.BoxDomainPresent {
		t.Errorf("second setup = %+v, want READY, nothing pulled, the domain present", again.BoxSetup)
	}
	if n := mustPullCount(t, api); n != pulls {
		t.Errorf("pull operations = %d after the second setup, want %d (no POST /base)", n, pulls)
	}
	if n := len(mustBases(t, api)); n != 1 {
		t.Errorf("bases = %d after the second setup, want 1", n)
	}
}

func assertSubdomainCycle(t *testing.T, h *e2eHarness, s *e2eSession, api *platform.BoxClient, host, serviceID, ingress string) {
	t.Helper()
	s.mustCallSuccess("zerops_subdomain", map[string]any{"action": "disable", "serviceHostname": host})
	off := decodeBox(t, s.mustCallSuccess("zerops_box", map[string]any{"action": "setup", "serviceHostname": host}))
	if off.BoxSetup == nil || off.BoxSetup.Domain == nil || off.BoxSetup.Domain.State != ops.BoxDomainNone || len(off.BoxSetup.Domain.Recovery) != 2 ||
		!strings.Contains(off.BoxSetup.Domain.Recovery[0], "zerops_subdomain action=enable serviceHostname="+host) {
		t.Errorf("setup with subdomain access off = %+v, want domain none with the enable + setup recovery", off.BoxSetup)
	}
	if d := mustDomains(t, api); len(d) != 1 || d[0].Domain != ingress {
		t.Errorf("domains after setup with access off = %+v, want the ingress domain kept", d)
	}
	offStatus := decodeBox(t, s.mustCallSuccess("zerops_box", map[string]any{"action": "status", "serviceHostname": host}))
	if offStatus.BoxStatus == nil || offStatus.BoxStatus.Subdomain.Access {
		t.Errorf("status with access off = %+v, want access false", offStatus.BoxStatus)
	}

	enableText := s.mustCallSuccess("zerops_subdomain", map[string]any{"action": "enable", "serviceHostname": host})
	t.Logf("  zerops_subdomain enable on the Box: %.400s", enableText)
	on := decodeBox(t, s.mustCallSuccess("zerops_box", map[string]any{"action": "setup", "serviceHostname": host}))
	if on.BoxSetup == nil || on.BoxSetup.Status != ops.BoxSetupReady || on.BoxSetup.Domain == nil || on.BoxSetup.Domain.State != ops.BoxDomainPresent {
		t.Errorf("setup after enable = %+v, want READY with the domain present", on.BoxSetup)
	}
	svc, err := h.client.GetService(context.Background(), serviceID)
	if err != nil || !svc.SubdomainAccess {
		t.Errorf("service after enable: %+v, %v; want subdomainAccess on", svc, err)
	}
}

func deleteBoxAndReadBack(t *testing.T, h *e2eHarness, serviceID string) {
	t.Helper()
	ctx := context.Background()
	proc, err := h.client.DeleteService(ctx, serviceID)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if proc != nil {
		waitForProcessDirect(ctx, h.client, proc.ID)
	}
	var gone *platform.PlatformError
	for range 30 {
		_, err = h.client.GetService(ctx, serviceID)
		if errors.As(err, &gone) {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if gone == nil || (gone.APICode != "serviceStackNotFound" && gone.Code != platform.ErrServiceNotFound) {
		t.Errorf("GetService after delete = %v, want serviceStackNotFound", err)
		return
	}
	t.Logf("  read back: %s (%s)", gone.APICode, gone.Code)
}

// ingressFromEnv reads the 8081 host the platform wrote into zeropsSubdomain.
func ingressFromEnv(t *testing.T, h *e2eHarness, serviceID string) string {
	t.Helper()
	value := getZeropsSubdomainEnv(t, h, context.Background(), serviceID)
	for line := range strings.SplitSeq(value, "\n") {
		host := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(line), "https://"), "/")
		if strings.Contains(host, "-8081.") {
			return host
		}
	}
	t.Fatalf("zeropsSubdomain %q names no 8081 host", value)
	return ""
}

// rawPing GETs the zproxy ping with bearer (none when empty) and answers the
// status.
func rawPing(t *testing.T, urlAPI, bearer string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, urlAPI+"/api/rest/ping", nil)
	if err != nil {
		t.Fatal(err)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("ping: %v", err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func mustBases(t *testing.T, api *platform.BoxClient) []platform.BoxBase {
	t.Helper()
	bases, err := api.Bases(context.Background())
	if err != nil {
		t.Fatalf("base-list: %v", err)
	}
	return bases
}

func mustDomains(t *testing.T, api *platform.BoxClient) []platform.BoxDomain {
	t.Helper()
	ws, err := api.Workspace(context.Background())
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	return ws.Domains
}

func mustPullCount(t *testing.T, api *platform.BoxClient) int {
	t.Helper()
	pulls, err := api.Operations(context.Background(), platform.BoxOperationKindPullBase)
	if err != nil {
		t.Fatalf("operation-list: %v", err)
	}
	return len(pulls)
}

func decodeBox(t *testing.T, text string) boxToolResult {
	t.Helper()
	var r boxToolResult
	if err := json.Unmarshal([]byte(text), &r); err != nil {
		t.Fatalf("parse zerops_box: %v\n%s", err, text)
	}
	return r
}
