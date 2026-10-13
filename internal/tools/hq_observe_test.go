package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/hq"
)

func TestObserve_HQEnrollment_ReadsAndFailures(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	registerObserve(srv, nil, filepath.Join(t.TempDir(), "missing.json"), "P_MATE")
	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	listed, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil || len(listed.Tools) != 1 {
		t.Fatalf("list observe tool: result=%+v err=%v", listed, err)
	}
	tool := listed.Tools[0]
	wantDescription := "Observe this project's stage and production through HQ. A stage or production that Mate delivers to a Git repository is not listed here. environments lists project IDs; status reads service IDs, status and active versions; logs reads up to 100 service entries. Read only: no sibling Zerops grants or secrets. Failures end the call; retry manually after fixing them."
	if tool == nil || tool.Description != wantDescription {
		t.Fatalf("observe tool = %+v, want description %q", tool, wantDescription)
	}
	for _, tt := range []struct {
		name, action, project, service, path, body string
		status                                     int
	}{
		{"environments", "environments", "", "", "/api/mate/environments", `{"appId":"a1","environments":[{"projectId":"P_STAGE","name":"stage","tier":"stage"}]}`, 200},
		{"status", "status", "P_STAGE", "", "/api/mate/environments/P_STAGE", `{"environment":{"projectId":"P_STAGE","name":"stage","tier":"stage"},"services":[{"id":"S1","name":"web","status":"ACTIVE","activeVersion":{"id":"V1","name":"v1"}}]}`, 200},
		{"logs", "logs", "P_STAGE", "S1", "/api/mate/environments/P_STAGE/services/S1/logs?limit=2", `{"projectId":"P_STAGE","serviceId":"S1","entries":[{"timestamp":"now","severity":"info","message":"serving"}]}`, 200},
		{"forbidden", "status", "P_STAGE", "", "/api/mate/environments/P_STAGE", `{"code":"observation_refused","reason":"forbidden"}`, 403},
		{"unavailable", "status", "P_STAGE", "", "/api/mate/environments/P_STAGE", `{"code":"zerops_unavailable"}`, 503},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var reads atomic.Int32
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reads.Add(1)
				if r.Method != http.MethodGet || r.URL.String() != tt.path || r.Header.Get("Authorization") != "Mate good" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL)
				}
				w.WriteHeader(tt.status)
				if _, err := w.Write([]byte(tt.body)); err != nil {
					t.Error(err)
				}
			}))
			defer fake.Close()
			path := filepath.Join(t.TempDir(), "enrollment.json")
			if err := hq.SaveEnrollment(path, hq.Enrollment{HQ: fake.URL, ProjectID: "P_MATE", Credential: "good"}); err != nil {
				t.Fatal(err)
			}
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
			registerObserve(srv, fake.Client(), path, "P_MATE")
			result := callTool(t, srv, "zerops_observe", map[string]any{"action": tt.action, "projectId": tt.project, "serviceId": tt.service, "limit": 2})
			if reads.Load() != 1 {
				t.Fatalf("reads = %d, want one attempt", reads.Load())
			}
			if result.StructuredContent != nil {
				t.Fatal("typed content must stay nil")
			}
			text := result.Content[0].(*mcp.TextContent).Text
			if strings.Contains(text, "good") {
				t.Fatal("credential leaked")
			}
			if tt.status == 200 {
				if result.IsError || !json.Valid([]byte(text)) {
					t.Fatalf("success: %+v", result)
				}
			} else if !result.IsError || !strings.Contains(text, "again") {
				t.Fatalf("failure must be visible with manual retry: %s", text)
			}
		})
	}
}

func TestObserve_InvalidInputOrEnrollment_NoRequest(t *testing.T) {
	t.Parallel()
	for _, args := range []map[string]any{
		{"action": "status"}, {"action": "logs", "projectId": "P"}, {"action": "delete", "projectId": "P"},
		{"action": "logs", "projectId": "P", "serviceId": "S", "limit": 101}, {"action": "status", "projectId": "../P"},
		{"action": "environments"},
	} {
		srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
		registerObserve(srv, nil, filepath.Join(t.TempDir(), "missing.json"), "P_MATE")
		result := callTool(t, srv, "zerops_observe", args)
		if !result.IsError {
			t.Errorf("input %v should fail visibly", args)
		}
	}
}
