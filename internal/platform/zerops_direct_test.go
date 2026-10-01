// Tests for: internal/platform/zerops_direct.go — GetProjectProcessesDirect
// folding the process DTO's `error.code` into Process.FailReason when
// PublicMeta carries no failReason (docs/spec-workflows.md §8 O3: a
// redundant enable-subdomain-access request ends FAILED with
// publicMeta: null and error: {"code":"noSubdomainPorts", ...}, so today's
// PublicMeta-only mapping leaves FailReason empty).
package platform

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestGetProjectProcessesDirect_ErrorCodeWithoutPublicMeta_FailReasonCarriesCode
// pins the fold: a FAILED process with publicMeta: null and a populated
// error subfield carries "<code>: <message>" in FailReason; a sibling
// process whose publicMeta already carries failReason keeps today's value
// (regression — the error.code fold must never override an existing
// PublicMeta-derived reason).
func TestGetProjectProcessesDirect_ErrorCodeWithoutPublicMeta_FailReasonCarriesCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		id             string
		wantFailReason string
	}{
		{
			name:           "publicMeta null, error.code present",
			id:             "proc-1",
			wantFailReason: "noSubdomainPorts: no http ports found for subdomain support",
		},
		{
			name:           "publicMeta present keeps today's value",
			id:             "proc-2",
			wantFailReason: "publicMeta failure",
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"list": [
				{
					"id": "proc-1",
					"status": "FAILED",
					"actionName": "stack.enableSubdomain",
					"publicMeta": null,
					"error": {"code": "noSubdomainPorts", "message": "no http ports found for subdomain support"}
				},
				{
					"id": "proc-2",
					"status": "FAILED",
					"actionName": "stack.build",
					"publicMeta": {"failReason": "publicMeta failure"},
					"error": {"code": "someOtherCode", "message": "should be ignored"}
				}
			],
			"totalCount": 2
		}`))
	}))
	t.Cleanup(srv.Close)

	z, err := NewZeropsClient("fake-token", srv.URL)
	if err != nil {
		t.Fatalf("NewZeropsClient: %v", err)
	}

	procs, err := z.GetProjectProcessesDirect(context.Background(), "proj-1")
	if err != nil {
		t.Fatalf("GetProjectProcessesDirect: %v", err)
	}
	if len(procs) != len(tests) {
		t.Fatalf("expected %d processes, got %d", len(tests), len(procs))
	}

	byID := make(map[string]Process, len(procs))
	for _, p := range procs {
		byID[p.ID] = p
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, ok := byID[tt.id]
			if !ok {
				t.Fatalf("process %q not found in result", tt.id)
			}
			if p.FailReason == nil {
				t.Fatalf("FailReason = nil, want %q", tt.wantFailReason)
			}
			if *p.FailReason != tt.wantFailReason {
				t.Errorf("FailReason = %q, want %q", *p.FailReason, tt.wantFailReason)
			}
		})
	}
}

// TestGetProcess_ErrorCodeFoldsIntoFailReason: the by-id read every poll ends
// on (ops.PollProcess, behind zerops_import's results) carries a failed
// process's `error` the same way the project list does — the model reading an
// import that failed was told FAILED and nothing else.
func TestGetProcess_ErrorCodeFoldsIntoFailReason(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		body           string
		wantStatus     string
		wantFailReason string
	}{
		{
			name:           "publicMeta null, error present",
			body:           `{"id":"proc-1","status":"FAILED","actionName":"stack.create","publicMeta":null,"error":{"code":"serviceStackTypeNotFound","message":"service stack type not found"}}`,
			wantStatus:     "FAILED",
			wantFailReason: "serviceStackTypeNotFound: service stack type not found",
		},
		{
			name:           "publicMeta reason wins",
			body:           `{"id":"proc-1","status":"FAILED","actionName":"stack.build","publicMeta":{"failReason":"publicMeta failure"},"error":{"code":"other","message":"ignored"}}`,
			wantStatus:     "FAILED",
			wantFailReason: "publicMeta failure",
		},
		{
			name:       "finished process, no reason",
			body:       `{"id":"proc-1","status":"DONE","actionName":"stack.create","publicMeta":null,"error":null}`,
			wantStatus: "FINISHED",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/rest/public/process/proc-1" {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(srv.Close)
			z, err := NewZeropsClient("fake-token", srv.URL)
			if err != nil {
				t.Fatalf("NewZeropsClient: %v", err)
			}
			p, err := z.GetProcess(context.Background(), "proc-1")
			if err != nil {
				t.Fatalf("GetProcess: %v", err)
			}
			if p.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", p.Status, tt.wantStatus)
			}
			got := ""
			if p.FailReason != nil {
				got = *p.FailReason
			}
			if got != tt.wantFailReason {
				t.Errorf("FailReason = %q, want %q", got, tt.wantFailReason)
			}
		})
	}
}

// TestGetProcess_NotFound_IsAPlatformError: a non-2xx by-id read still maps
// to the platform's error, as the SDK handler did.
func TestGetProcess_NotFound_IsAPlatformError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"processNotFound","message":"process not found"}}`))
	}))
	t.Cleanup(srv.Close)
	z, err := NewZeropsClient("fake-token", srv.URL)
	if err != nil {
		t.Fatalf("NewZeropsClient: %v", err)
	}
	if _, err := z.GetProcess(context.Background(), "proc-1"); err == nil {
		t.Fatal("GetProcess on a 404 returned no error")
	}
}

// TestGetProject_CarriesTags: the project read carries its tags — the press
// writes mate:closed-off there once it has closed the project off, and a
// Mate reads it with its own key.
func TestGetProject_CarriesTags(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/rest/public/project/proj-1" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"proj-1","clientId":"c1","name":"probe","status":"ACTIVE","mode":"LIGHT","tagList":["mate","mate:closed-off"],"primaryInstanceLocation":{"id":"eu-central"}}`))
	}))
	t.Cleanup(srv.Close)
	z, err := NewZeropsClient("fake-token", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	p, err := z.GetProject(context.Background(), "proj-1")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if len(p.Tags) != 2 || p.Tags[1] != "mate:closed-off" {
		t.Errorf("Tags = %q, want [mate mate:closed-off]", p.Tags)
	}
}
