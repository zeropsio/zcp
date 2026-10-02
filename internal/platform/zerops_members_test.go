package platform

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
)

const membersPath = "/api/rest/public/client/org-1/user/list"

func TestListOrgMembers_PeopleAndTokens_Mapped(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != membersPath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"clientUserList":[
			{"id":"cu-1","clientId":"org-1","userId":"u-1","status":"ACTIVE","roleCode":"OWNER",
			 "canCreateProjects":true,"user":{"id":"u-1","email":"owner@example.com","fullName":"Org Owner"}},
			{"id":"cu-2","clientId":"org-1","userId":"u-2","status":"ACTIVE","roleCode":"ADMIN",
			 "canCreateProjects":true,"user":{"id":"u-2","email":"token-abc@zerops.io","fullName":"mate-hq:P1:https://hq.example"}}
		]}`))
	}))
	defer srv.Close()

	z, err := NewZeropsClient("key", srv.URL)
	if err != nil {
		t.Fatalf("NewZeropsClient: %v", err)
	}
	got, err := z.ListOrgMembers(context.Background(), "org-1")
	if err != nil {
		t.Fatalf("ListOrgMembers: %v", err)
	}
	want := []OrgMember{
		{ID: "cu-1", UserID: "u-1", Status: "ACTIVE", RoleCode: "OWNER", CanCreateProjects: true,
			Email: "owner@example.com", FullName: "Org Owner"},
		{ID: "cu-2", UserID: "u-2", Status: "ACTIVE", RoleCode: "ADMIN", CanCreateProjects: true,
			Email: "token-abc@zerops.io", FullName: "mate-hq:P1:https://hq.example"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("members:\n got %+v\nwant %+v", got, want)
	}
}

// The member list answers a passing 400 userNotFound for a token that reads
// it (measured from HQ's own reads); a retry gets the list.
func TestListOrgMembers_PassingRefusal_Retried(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		status    int
		code      string
		wantCalls int32
		wantErr   bool
	}{
		{"userNotFound is retried", http.StatusBadRequest, "userNotFound", 2, false},
		{"a 5xx is retried", http.StatusBadGateway, "badGateway", 2, false},
		{"a refusal is final", http.StatusForbidden, "insufficientPermissions", 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if calls.Add(1) == 1 {
					w.WriteHeader(tt.status)
					_, _ = w.Write([]byte(`{"error":{"code":"` + tt.code + `","message":"no"}}`))
					return
				}
				_, _ = w.Write([]byte(`{"clientUserList":[]}`))
			}))
			defer srv.Close()

			z, err := NewZeropsClient("key", srv.URL)
			if err != nil {
				t.Fatalf("NewZeropsClient: %v", err)
			}
			_, err = z.ListOrgMembers(context.Background(), "org-1")
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if calls.Load() != tt.wantCalls {
				t.Errorf("calls = %d, want %d", calls.Load(), tt.wantCalls)
			}
		})
	}
}
