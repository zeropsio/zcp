package platform

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestClassifyAppVersionUserData_NewModel_UserIsRunEnvSystemIsIntrinsic pins the 2026-08 classifier (the
// single classifier shared by the real client + mock) against the model
// docs/spec-zerops-env-lifecycle.md §1 describes: app-version userDataList
// records are typed USER (yaml-baked run.envVariables, editable:false) or
// SYSTEM (intrinsics) — the legacy READ_ONLY|EDITABLE|SECRET|INTERNAL|ENV
// enum is retired on the wire. ZEROPS_YAML is dropped by key regardless of
// Type; unknown/empty Type is fail-safe intrinsic (never admitted as a
// yaml-baked ref target).
func TestClassifyAppVersionUserData_NewModel_UserIsRunEnvSystemIsIntrinsic(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		key      string
		typeStr  string
		wantKind userDataKind
	}{
		{"USER run var", "FOO", "USER", kindRunEnvVariable},
		{"SYSTEM intrinsic", "zeropsSubdomain", "SYSTEM", kindIntrinsic},
		{"empty Type → intrinsic (fail-safe)", "MYSTERY", "", kindIntrinsic},
		{"unknown Type → intrinsic (fail-safe)", "WHATEVER", "FUTURE_TYPE", kindIntrinsic},
		{"ZEROPS_YAML dropped by key even when USER-typed", "ZEROPS_YAML", "USER", kindZeropsYaml},
		{"ZEROPS_YAML dropped by key even when SYSTEM-typed", "ZEROPS_YAML", "SYSTEM", kindZeropsYaml},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			kind := classifyAppVersionUserData(tt.key, tt.typeStr)
			if kind != tt.wantKind {
				t.Errorf("kind = %d, want %d", kind, tt.wantKind)
			}
		})
	}
}

// TestGetAppVersionUserData_NewModel_ReturnsUserOnly pins F7/E5/E6 under the
// new model: the mock shares the real classifier, so it returns ONLY
// USER-typed yaml-baked records (SYSTEM intrinsics + the ZEROPS_YAML blob
// filtered out), with Sensitive always false — the SDK's AppVersionUserData
// DTO carries no Sensitive field at all (spec §1), so nothing derives it —
// and the returned Type is the closed ServiceEnvUser value. A test cannot
// model a shape the real API can't produce.
func TestGetAppVersionUserData_NewModel_ReturnsUserOnly(t *testing.T) {
	t.Parallel()
	mock := NewMock().WithAppVersionUserData("av1", []ServiceEnvVar{
		{Key: "FOO", Content: "bar", Type: ServiceEnvUser},                          // yaml-baked run var
		{Key: "hostname", Content: "api", Type: ServiceEnvSystem},                   // intrinsic → filtered
		{Key: "ZEROPS_YAML", Content: "build:\n  os: ubuntu", Type: ServiceEnvUser}, // blob → filtered by key
	})
	got, err := mock.GetAppVersionUserData(context.Background(), "av1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d run vars, want 1 (FOO): %+v", len(got), got)
	}
	if got[0].Key != "FOO" {
		t.Errorf("Key = %q, want FOO", got[0].Key)
	}
	if got[0].Type != ServiceEnvUser {
		t.Errorf("Type = %q, want %q", got[0].Type, ServiceEnvUser)
	}
	if got[0].Sensitive {
		t.Error("Sensitive must be false — the app-version DTO carries no Sensitive field to derive it from")
	}
}

// TestListServiceAppVersions_MapsListNewestFirst pins the DIRECT (non-ES)
// GET /service-stack/{id}/app-version read (docs/spec-workflows.md §8 R2):
// ComputeRecoveryState needs a lag-free view of the newest appVersion right
// after a mutation, unlike SearchAppVersions (ES-backed). Regardless of the
// order the platform emits `list` in, the wrapper returns newest-first by
// Sequence so callers can always key off index 0.
func TestListServiceAppVersions_MapsListNewestFirst(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/rest/public/service-stack/svc-1/app-version" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"list": [
				{"id": "av-1", "serviceStackId": "svc-1", "source": "GIT", "status": "ACTIVE", "sequence": 1, "created": "2026-09-01T00:00:00Z", "lastUpdate": "2026-09-01T00:00:00Z"},
				{"id": "av-2", "serviceStackId": "svc-1", "source": "GIT", "status": "DEPLOY_FAILED", "sequence": 2, "created": "2026-09-14T00:00:00Z", "lastUpdate": "2026-09-14T00:00:00Z", "publicGitSource": {"gitUrl": "https://github.com/example/repo", "branchName": "main"}}
			],
			"totalCount": 2
		}`))
	}))
	t.Cleanup(srv.Close)

	z, err := NewZeropsClient("fake-token", srv.URL)
	if err != nil {
		t.Fatalf("NewZeropsClient: %v", err)
	}

	events, err := z.ListServiceAppVersions(context.Background(), "svc-1")
	if err != nil {
		t.Fatalf("ListServiceAppVersions: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].ID != "av-2" {
		t.Errorf("events[0].ID = %q, want av-2 (newest by sequence)", events[0].ID)
	}
	if events[0].Status != "DEPLOY_FAILED" {
		t.Errorf("events[0].Status = %q, want DEPLOY_FAILED", events[0].Status)
	}
	if events[0].PublicGitSource == nil || events[0].PublicGitSource.GitURL != "https://github.com/example/repo" {
		t.Errorf("events[0].PublicGitSource = %+v, want gitUrl set", events[0].PublicGitSource)
	}
	if events[1].ID != "av-1" {
		t.Errorf("events[1].ID = %q, want av-1", events[1].ID)
	}
}

// TestRedeployAppVersion_SendsYamlAndSetup_ReturnsProcess pins the live-
// verified wire contract (docs/spec-workflows.md §8 R2): PUT /app-version/
// {id}/deploy MUST carry BOTH zeropsYaml and zeropsYamlSetup — the platform
// does not reuse the stored yaml (a partial body 400s with
// zeropsYamlSetupNotFound).
func TestRedeployAppVersion_SendsYamlAndSetup_ReturnsProcess(t *testing.T) {
	t.Parallel()

	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/rest/public/app-version/av-2/deploy" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"proc-redeploy-1","status":"PENDING","actionName":"stack.deploy"}`))
	}))
	t.Cleanup(srv.Close)

	z, err := NewZeropsClient("fake-token", srv.URL)
	if err != nil {
		t.Fatalf("NewZeropsClient: %v", err)
	}

	proc, err := z.RedeployAppVersion(context.Background(), "av-2", "run:\n  start: npm start\n", "prod")
	if err != nil {
		t.Fatalf("RedeployAppVersion: %v", err)
	}
	if proc.ID != "proc-redeploy-1" {
		t.Errorf("proc.ID = %q, want proc-redeploy-1", proc.ID)
	}
	if proc.Status != "PENDING" {
		t.Errorf("proc.Status = %q, want PENDING", proc.Status)
	}

	if capturedBody == nil {
		t.Fatal("request body was not captured")
	}
	if capturedBody["zeropsYaml"] != "run:\n  start: npm start\n" {
		t.Errorf("zeropsYaml = %v, want the full yaml text", capturedBody["zeropsYaml"])
	}
	if capturedBody["zeropsYamlSetup"] != "prod" {
		t.Errorf("zeropsYamlSetup = %v, want %q", capturedBody["zeropsYamlSetup"], "prod")
	}
}
