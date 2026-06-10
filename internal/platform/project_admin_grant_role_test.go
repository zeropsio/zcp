package platform

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGrantSelfRole_APIErrorOnPut_Surfaces pins the P0-2 fix: when the
// role-write PUT is rejected with an API error (HTTP 4xx/5xx), GrantSelfRole
// must return that error. zerops-go reports API errors only through the
// response's Output()/Err(); the function's own error return covers
// transport failures alone. Before the fix the discarded response swallowed
// the 400 and GrantSelfRole returned nil — a silent missing-ADMIN bug.
func TestGrantSelfRole_APIErrorOnPut_Surfaces(t *testing.T) {
	t.Parallel()

	const userID = "cu-x"
	rolesPath := "/api/rest/public/client-user/" + userID + "/roles"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == rolesPath:
			// Existing roles read succeeds with an empty list.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"projectRoleList":[]}`))
		case r.Method == http.MethodPut && r.URL.Path == rolesPath:
			// The write is rejected by the API.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"message":"insufficient permissions"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	z, err := NewZeropsClient("fake-launch-key", srv.URL)
	if err != nil {
		t.Fatalf("NewZeropsClient: %v", err)
	}
	p := &projectAdminClient{zerops: z, clientID: "client-x", clientUserID: userID}

	err = p.GrantSelfRole(context.Background(), "proj-1", "ADMIN")
	if err == nil {
		t.Fatal("GrantSelfRole returned nil on a 403 PUT — the API error was swallowed (P0-2 regression)")
	}
	if !strings.Contains(err.Error(), "put roles") {
		t.Errorf("error %q should be wrapped as a put-roles failure", err)
	}
}

// TestGrantSelfRole_Success pins the happy path: both the read and the write
// succeed, so GrantSelfRole returns nil.
func TestGrantSelfRole_Success(t *testing.T) {
	t.Parallel()

	const userID = "cu-y"
	rolesPath := "/api/rest/public/client-user/" + userID + "/roles"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == rolesPath {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"projectRoleList":[]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	z, err := NewZeropsClient("fake-launch-key", srv.URL)
	if err != nil {
		t.Fatalf("NewZeropsClient: %v", err)
	}
	p := &projectAdminClient{zerops: z, clientID: "client-y", clientUserID: userID}

	if err := p.GrantSelfRole(context.Background(), "proj-1", "ADMIN"); err != nil {
		t.Fatalf("GrantSelfRole on all-200 path returned %v, want nil", err)
	}
}
