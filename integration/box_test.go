// Tests for: integration — a Zerops Box imported through zerops_import is
// set up before the call returns, and zerops_box re-runs and reads that setup,
// through the full MCP server against a mock platform and a fake Box API.

package integration_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

const (
	boxIntToken   = "zbxACCESSsentinel-integration-01"
	boxIntIngress = "box1-3860-8081.prg1.zerops.app"
	boxIntDigest  = "sha256:da2f61de16533fd55d06058a5f72701d9c57ad65cf630f9d441ca5fbee38a21f"
)

// fakeBoxAPI is a Box workspace behind zproxy: it starts empty (no base, no
// domain), as an imported workspace does, accepts only the access token as
// its bearer, and counts the mutations it is asked for.
type fakeBoxAPI struct {
	mu      sync.Mutex
	pulls   int
	puts    int
	ready   bool
	domains []platform.BoxDomain
}

func (f *fakeBoxAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if r.Header.Get("Authorization") != "Bearer "+boxIntToken {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"Authorization: Bearer <access token> is required."}}`))
		return
	}
	switch {
	case r.URL.Path == "/service-api/api/rest/ping":
		_, _ = w.Write([]byte(`{"time":"2026-10-24T16:12:44Z","version":"zerops-v3.1.0"}`))
	case r.URL.Path == "/service-api/api/rest/base-list":
		if !f.ready {
			_, _ = w.Write([]byte(`{"items":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"items":[{"digest":"` + boxIntDigest + `","imageRef":"` + ops.BoxDefaultBaseImageRef + `","name":"default","status":"READY"}]}`))
	case r.URL.Path == "/service-api/api/rest/operation-list":
		_, _ = w.Write([]byte(`{"items":[]}`))
	case r.URL.Path == "/service-api/api/rest/base" && r.Method == http.MethodPost:
		if r.Header.Get("Idempotency-Key") == "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":"invalidUserInput","message":"Invalid user input"}}`))
			return
		}
		f.pulls++
		_, _ = w.Write([]byte(`{"id":"op-1","kind":"PULL_BASE","status":"RUNNING","created":"2026-10-24T16:13:22Z"}`))
	case r.URL.Path == "/service-api/api/rest/operation/op-1":
		f.ready = true
		_, _ = w.Write([]byte(`{"id":"op-1","kind":"PULL_BASE","status":"FINISHED","created":"2026-10-24T16:13:22Z","result":{"digest":"` + boxIntDigest + `","name":"default"}}`))
	case r.URL.Path == "/service-api/api/rest/workspace":
		b, _ := json.Marshal(map[string]any{"domains": f.domains, "version": "v"})
		_, _ = w.Write(b)
	case r.URL.Path == "/service-api/api/rest/workspace/domains" && r.Method == http.MethodPut:
		var body struct {
			Domains []platform.BoxDomain `json:"domains"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		f.puts++
		f.domains = body.Domains
		b, _ := json.Marshal(map[string]any{"domains": f.domains, "version": "v"})
		_, _ = w.Write(b)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeBoxAPI) counts() (pulls, puts int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pulls, f.puts
}

func TestBox_Integration_ImportFinishesSetup_ThenSetupChangesNothing(t *testing.T) {
	t.Parallel()

	box := &fakeBoxAPI{}
	api := httptest.NewServer(box)
	defer api.Close()

	mock := defaultMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-box", Name: "box1", Status: "ACTIVE", SubdomainAccess: true,
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "ubuntu/zbox@1", ServiceStackTypeCategoryName: "STANDARD"}},
			{ID: "svc-1", Name: "app", Status: "RUNNING", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		}).
		WithServiceEnv("svc-box", []platform.ServiceEnvVar{{Key: "zeropsSubdomain", Content: "https://box1-3860-8080.prg1.zerops.app\nhttps://" + boxIntIngress}}).
		WithBoxAccess("svc-box", platform.BoxAccess{Token: boxIntToken, URLAPI: api.URL + "/service-api"}).
		WithImportResult(&platform.ImportResult{ProjectID: "proj-1", ProjectName: "test-project", ServiceStacks: []platform.ImportedServiceStack{
			{ID: "svc-box", Name: "box1", Processes: []platform.Process{
				{ID: "proc-box-create", ActionName: "stack.create", Status: "PENDING"},
				{ID: "proc-box-sub", ActionName: "stack.enableSubdomainAccess", Status: "PENDING"},
			}},
		}}).
		WithProcess(&platform.Process{ID: "proc-box-create", ActionName: "stack.create", Status: "FINISHED"}).
		WithProcess(&platform.Process{ID: "proc-box-sub", ActionName: "stack.enableSubdomainAccess", Status: "FINISHED"})
	session, cleanup := setupTestServer(t, mock, defaultLogFetcher())
	defer cleanup()

	callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "route": "classic", "intent": "box integration test",
	})
	importText := callAndGetText(t, session, "zerops_import", map[string]any{
		"content": "services:\n  - hostname: box1\n    type: zbox@1\n    enableSubdomainAccess: true\n",
	})

	var imported struct {
		Summary  string               `json:"summary"`
		BoxSetup []ops.BoxSetupResult `json:"boxSetup"`
	}
	if err := json.Unmarshal([]byte(importText), &imported); err != nil {
		t.Fatalf("parse import result: %v\n%s", err, importText)
	}
	if len(imported.BoxSetup) != 1 {
		t.Fatalf("boxSetup = %+v, want box1's block: %s", imported.BoxSetup, importText)
	}
	b := imported.BoxSetup[0]
	if b.Hostname != "box1" || b.Status != ops.BoxSetupReady || b.Base == nil || b.Base.Name != "default" || b.Base.Digest != boxIntDigest ||
		b.Domain == nil || b.Domain.Domain != boxIntIngress || b.Domain.Form != platform.BoxDomainFormDash || b.Domain.State != ops.BoxDomainAdded {
		t.Errorf("box1's block = %s, want READY, base default %s, %s DASH added", importText, boxIntDigest, boxIntIngress)
	}
	if pulls, puts := box.counts(); pulls != 1 || puts != 1 {
		t.Errorf("the import made %d pulls and %d domain writes, want one of each", pulls, puts)
	}

	setupText := callAndGetText(t, session, "zerops_box", map[string]any{"action": "setup", "serviceHostname": "box1"})
	if pulls, puts := box.counts(); pulls != 1 || puts != 1 {
		t.Errorf("a second setup mutated (pulls %d, domain writes %d), want none: %s", pulls, puts, setupText)
	}
	if !strings.Contains(setupText, `"present"`) || !strings.Contains(setupText, `"READY"`) {
		t.Errorf("second setup = %s, want READY with the domain present", setupText)
	}

	statusText := callAndGetText(t, session, "zerops_box", map[string]any{"action": "status", "serviceHostname": "box1"})
	var status struct {
		BoxStatus ops.BoxStatusResult `json:"boxStatus"`
	}
	if err := json.Unmarshal([]byte(statusText), &status); err != nil {
		t.Fatalf("parse status: %v", err)
	}
	if !status.BoxStatus.Subdomain.Access || !status.BoxStatus.Subdomain.Registered || len(status.BoxStatus.Bases) != 1 {
		t.Errorf("status = %s, want the base and the registered ingress domain", statusText)
	}

	for name, text := range map[string]string{"import": importText, "setup": setupText, "status": statusText} {
		if strings.Contains(text, boxIntToken) || strings.Contains(text, "service-ui") {
			t.Errorf("%s result carries the access token: %s", name, text)
		}
	}
}
