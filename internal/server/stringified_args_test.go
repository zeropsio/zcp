package server

import (
	"context"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type stringifiedArgsInput struct {
	CloseMode map[string]string `json:"closeMode,omitempty"`
	Commands  [][]string        `json:"commands,omitempty"`
	Note      string            `json:"note,omitempty"`
}

// TestDecodeStringifiedArguments_AnObjectOrArraySentAsJSONTextRunsOnceDecoded:
// an agent whose client sends an object or array argument JSON-encoded in a
// string — a Mate on the free agent sent closeMode that way and the SDK
// refused every try (2026-11-20) — gets its call run once, with the value
// decoded; a property the tool takes as a string keeps its JSON text; a bare
// word where an object belongs is still refused, and the handler never runs.
// witness: internal/server/stringified_args.go | if name == "" || decoded[name] { → if name == "" || decoded[name] || true {
func TestDecodeStringifiedArguments_AnObjectOrArraySentAsJSONTextRunsOnceDecoded(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "zcp", Version: "test"}, nil)
	srv.AddReceivingMiddleware(decodeStringifiedArguments())
	var got []stringifiedArgsInput
	mcp.AddTool(srv, &mcp.Tool{Name: "workflow"}, func(_ context.Context, _ *mcp.CallToolRequest, in stringifiedArgsInput) (*mcp.CallToolResult, any, error) {
		got = append(got, in)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "ok"}}}, nil, nil
	})
	st, ct := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(t.Context(), st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.1"}, nil).Connect(t.Context(), ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	tests := []struct {
		name    string
		args    map[string]any
		refused bool
		check   func(t *testing.T, in stringifiedArgsInput)
	}{
		{
			name: "an object sent as JSON text",
			args: map[string]any{"closeMode": `{"appdev":"auto"}`},
			check: func(t *testing.T, in stringifiedArgsInput) {
				t.Helper()
				if !maps.Equal(in.CloseMode, map[string]string{"appdev": "auto"}) {
					t.Errorf("closeMode = %v, want map[appdev:auto]", in.CloseMode)
				}
			},
		},
		{
			name: "an object and an array both sent as JSON text",
			args: map[string]any{"closeMode": ` {"appdev":"manual"} `, "commands": `[["wait","1000"],["get","url"]]`},
			check: func(t *testing.T, in stringifiedArgsInput) {
				t.Helper()
				if in.CloseMode["appdev"] != "manual" || len(in.Commands) != 2 || !slices.Equal(in.Commands[1], []string{"get", "url"}) {
					t.Errorf("decoded = %+v", in)
				}
			},
		},
		{
			name: "a string property holding JSON text",
			args: map[string]any{"note": `{"keep":"as text"}`, "closeMode": map[string]any{"appdev": "auto"}},
			check: func(t *testing.T, in stringifiedArgsInput) {
				t.Helper()
				if in.Note != `{"keep":"as text"}` {
					t.Errorf("note = %q, want the JSON text kept", in.Note)
				}
			},
		},
		{
			name:    "a bare word where an object belongs",
			args:    map[string]any{"closeMode": "auto"},
			refused: true,
		},
	}
	for _, tt := range tests {
		got = nil
		result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "workflow", Arguments: tt.args})
		if err != nil {
			t.Fatalf("%s: call: %v", tt.name, err)
		}
		if tt.refused {
			text := ""
			if len(result.Content) > 0 {
				text = result.Content[0].(*mcp.TextContent).Text
			}
			if !result.IsError || !strings.Contains(text, "/properties/closeMode: type") || len(got) != 0 {
				t.Errorf("%s: want the SDK's refusal and no handler run, got isError=%v %q, %d runs", tt.name, result.IsError, text, len(got))
			}
			continue
		}
		if result.IsError || len(got) != 1 {
			t.Errorf("%s: want one clean run, got isError=%v, %d runs: %+v", tt.name, result.IsError, len(got), result.Content)
			continue
		}
		tt.check(t, got[0])
	}
}
