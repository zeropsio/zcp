package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
)

func TestServer_ManagedInvocation_IsRequiredAndNeverAModelArgument(t *testing.T) {
	t.Chdir(t.TempDir())
	const key = "io.zerops.mate/invocation"
	task := map[string]any{"v": 1, "id": strings.Repeat("a", 64), "scope": map[string]any{"kind": "task", "taskId": "11111111-1111-1111-1111-111111111111", "assignment": "9007199254740993"}}
	manual := map[string]any{"v": 1, "id": strings.Repeat("b", 64), "scope": map[string]any{"kind": "manual"}}
	for _, managed := range []bool{false, true} {
		name := "ordinary"
		if managed {
			name = "managed"
		}
		t.Run(name, func(t *testing.T) {
			store, err := knowledge.GetEmbeddedStore()
			if err != nil {
				t.Fatal(err)
			}
			mock := platform.NewMock().WithProject(&platform.Project{ID: "p1", Name: "test"}).WithServices(nil)
			info := &auth.Info{ProjectID: "p1", Token: "test", APIHost: "localhost"}
			var srv *Server
			if managed {
				srv = NewManaged(t.Context(), mock, info, store, nil, nil, nil, runtime.Info{InContainer: true}, nil)
			} else {
				srv = New(t.Context(), mock, info, store, nil, nil, nil, runtime.Info{InContainer: true}, nil)
			}
			t.Cleanup(srv.Stop)
			requests := make(chan json.RawMessage, 16)
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var raw json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
					t.Error(err)
				}
				requests <- raw
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"change":{"number":1},"created":true}`))
			}))
			t.Cleanup(endpoint.Close)
			hqc, err := hq.Open(endpoint.Client(), hq.Identity{Origin: endpoint.URL, Key: "fake-project-key", ProjectID: "mate", OrgID: "org"})
			if err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			mcp.AddTool(srv.MCPServer(), &mcp.Tool{Name: "origin_probe", Description: "Read immutable test origin"}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, topology.Invocation, error) {
				calls.Add(1)
				v, e := hq.InvocationFromContext(ctx)
				if e == nil {
					_, e = hqc.OpenChange(ctx, "appdev", "Actual native call", "work")
				}
				return nil, v, e
			})
			st, ct := mcp.NewInMemoryTransports()
			ss, err := srv.MCPServer().Connect(t.Context(), st, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = ss.Close() })
			session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(t.Context(), ct, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = session.Close() })
			capability := session.InitializeResult().Capabilities.Experimental[key]
			if managed {
				if capability != float64(1) {
					t.Fatalf("managed capability = %#v", capability)
				}
			} else if capability != nil {
				t.Fatalf("ordinary advertises managed authority: %#v", capability)
			}
			cases := []struct {
				name           string
				meta           any
				present, allow bool
			}{
				{name: "absent", allow: !managed},
				{name: "task", meta: task, present: true, allow: managed},
				{name: "manual", meta: manual, present: true, allow: managed},
				{name: "null", present: true},
				{name: "unknown", meta: map[string]any{"v": 1, "id": strings.Repeat("c", 64), "scope": map[string]any{"kind": "unknown"}}, present: true},
				{name: "numeric grant", meta: map[string]any{"v": 1, "id": strings.Repeat("c", 64), "scope": map[string]any{"kind": "task", "taskId": "11111111-1111-1111-1111-111111111111", "assignment": 1}}, present: true},
				{name: "future version", meta: map[string]any{"v": 2, "id": strings.Repeat("c", 64), "scope": map[string]any{"kind": "manual"}}, present: true},
				{name: "case folded envelope", meta: map[string]any{"V": 1, "id": strings.Repeat("c", 64), "scope": map[string]any{"kind": "manual"}}, present: true},
				{name: "case folded scope", meta: map[string]any{"v": 1, "id": strings.Repeat("c", 64), "scope": map[string]any{"kind": "task", "TaskId": "11111111-1111-1111-1111-111111111111", "assignment": "1"}}, present: true},
				{name: "missing invocation id", meta: map[string]any{"v": 1, "scope": map[string]any{"kind": "manual"}}, present: true},
				{name: "extra authority", meta: map[string]any{"v": 1, "id": strings.Repeat("c", 64), "scope": map[string]any{"kind": "manual", "taskId": ""}}, present: true},
			}
			for _, tt := range cases {
				t.Run(tt.name, func(t *testing.T) {
					before := calls.Load()
					params := &mcp.CallToolParams{Name: "origin_probe", Arguments: map[string]any{}}
					if tt.present {
						params.Meta = mcp.Meta{key: tt.meta}
					}
					result, err := session.CallTool(t.Context(), params)
					if err != nil {
						t.Fatal(err)
					}
					if result.IsError == tt.allow {
						t.Fatalf("allow=%v result=%+v", tt.allow, result)
					}
					if !tt.allow && calls.Load() != before {
						t.Fatal("refused origin reached tool")
					}
					if tt.allow {
						var sent struct {
							Origin *struct {
								TaskID     string `json:"taskId"`
								Assignment string `json:"assignment"`
							} `json:"originatingTask"`
						}
						if err := json.Unmarshal(<-requests, &sent); err != nil {
							t.Fatal(err)
						}
						if tt.name == "task" {
							if sent.Origin == nil || sent.Origin.Assignment != "9007199254740993" {
								t.Fatalf("MCP to HTTP origin changed: %+v", sent)
							}
						} else if sent.Origin != nil {
							t.Fatal("manual call gained a task")
						}
					}
					if tt.allow && tt.name == "task" {
						text := result.Content[0].(*mcp.TextContent).Text
						if !strings.Contains(text, `"assignment":"9007199254740993"`) {
							t.Fatalf("origin lost: %s", text)
						}
					}
				})
			}
		})
	}
}
