package platform_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// Tests for: GetProject carries the project's location — the id and the name
// the platform gives it — as the live record answers it (GET /project/{id},
// primaryInstanceLocation; live-verified 2026-10-24 on a prg1 and a ny1
// project). The Box region pre-check reads both (docs/spec-workflows.md §8
// PS-16).

func TestGetProject_CarriesTheLocationIDAndName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, id, locName string
	}{
		{"prg1", "eu-central", "EU Central (prg1)"},
		{"ny1", "us-east-1", "US East (ny1)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/rest/public/project/proj-1" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"id":"proj-1","clientId":"org-1","name":"lab","status":"ACTIVE","mode":"LIGHT",`+
					`"primaryInstanceLocation":{"id":%q,"name":%q,"pingUrl":"https://proxy.example/api/rest/ping"}}`, tt.id, tt.locName)
			}))
			t.Cleanup(srv.Close)
			client, err := platform.NewZeropsClient("token", srv.URL)
			if err != nil {
				t.Fatalf("NewZeropsClient: %v", err)
			}
			got, err := client.GetProject(context.Background(), "proj-1")
			if err != nil {
				t.Fatalf("GetProject: %v", err)
			}
			if got.LocationID != tt.id || got.LocationName != tt.locName {
				t.Errorf("location = (%q, %q), want (%q, %q)", got.LocationID, got.LocationName, tt.id, tt.locName)
			}
		})
	}
}
