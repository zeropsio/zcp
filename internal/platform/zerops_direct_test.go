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
