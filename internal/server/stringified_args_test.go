package server

import (
	"context"
	"maps"
	"reflect"
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

// TestDecodeStringifiedArguments_SA1_AnObjectOrArraySentAsJSONTextRunsOnceDecoded:
// an agent whose client sends an object or array argument JSON-encoded in a
// string — a Mate on the free agent sent closeMode that way and the SDK
// refused every try (2026-11-20) — gets its call run once, with the value
// decoded; a property the tool takes as a string keeps its JSON text; a bare
// word where an object belongs is still refused, and the handler never runs.
// witness: internal/server/stringified_args.go | if name == "" || decoded[name] { → if name == "" || decoded[name] || true {
func TestDecodeStringifiedArguments_SA1_AnObjectOrArraySentAsJSONTextRunsOnceDecoded(t *testing.T) {
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

type stringifiedScalarArgsInput struct {
	Count   int               `json:"count,omitempty"`
	Check   *int64            `json:"check,omitempty"`
	Ratio   float64           `json:"ratio,omitempty"`
	Enabled bool              `json:"enabled,omitempty"`
	Note    string            `json:"note,omitempty"`
	Object  map[string]string `json:"object,omitempty"`
	Nested  *struct {
		Count int `json:"count,omitempty"`
	} `json:"nested,omitempty"`
	Fail bool `json:"fail,omitempty"`
}

// witness: internal/server/stringified_args.go | case 't', 'f', '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9': → case '~':
func TestDecodeStringifiedArguments_SA2_ScalarsSentAsJSONTextRunOnceDecoded(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "zcp", Version: "test"}, nil)
	srv.AddReceivingMiddleware(decodeStringifiedArguments())
	var got []stringifiedScalarArgsInput
	mcp.AddTool(srv, &mcp.Tool{Name: "scalars"}, func(_ context.Context, _ *mcp.CallToolRequest, in stringifiedScalarArgsInput) (*mcp.CallToolResult, any, error) {
		got = append(got, in)
		if in.Fail {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "handler refused"}}}, nil, nil
		}
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

	id := int64(53211234567)
	tests := []struct {
		name         string
		args         map[string]any
		want         stringifiedScalarArgsInput
		refusal      string
		handlerError bool
	}{
		{name: "integer", args: map[string]any{"count": " 42 "}, want: stringifiedScalarArgsInput{Count: 42}},
		{name: "negative integer", args: map[string]any{"count": "-17"}, want: stringifiedScalarArgsInput{Count: -17}},
		{name: "64-bit id", args: map[string]any{"check": "53211234567"}, want: stringifiedScalarArgsInput{Check: &id}},
		{name: "fraction", args: map[string]any{"ratio": "0.125"}, want: stringifiedScalarArgsInput{Ratio: 0.125}},
		{name: "true", args: map[string]any{"enabled": "true"}, want: stringifiedScalarArgsInput{Enabled: true}},
		{name: "false", args: map[string]any{"enabled": "false"}},
		{name: "string kept during retry", args: map[string]any{"note": "42", "count": "42"}, want: stringifiedScalarArgsInput{Note: "42", Count: 42}},
		{name: "bare word", args: map[string]any{"count": "auto"}, refusal: "/properties/count: type"},
		{name: "null text", args: map[string]any{"check": "null"}, refusal: "/properties/check: type"},
		{name: "quoted string", args: map[string]any{"count": `"42"`}, refusal: "/properties/count: type"},
		{name: "leading zero", args: map[string]any{"count": "007"}, refusal: "/properties/count: type"},
		{name: "number where object belongs", args: map[string]any{"object": "42"}, refusal: "/properties/object: type"},
		{name: "nested number text", args: map[string]any{"nested": map[string]any{"count": "42"}}, refusal: "/properties/nested/properties/count: type"},
		{name: "handler refusal", args: map[string]any{"note": "42", "fail": true}, handlerError: true},
	}
	for _, tt := range tests {
		got = nil
		result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "scalars", Arguments: tt.args})
		if err != nil {
			t.Fatalf("%s: call: %v", tt.name, err)
		}
		message := ""
		if len(result.Content) > 0 {
			if content, ok := result.Content[0].(*mcp.TextContent); ok {
				message = content.Text
			}
		}
		if tt.refusal != "" {
			if !result.IsError || !strings.Contains(message, tt.refusal) || len(got) != 0 {
				t.Errorf("%s: want SDK refusal %q and no handler run, got isError=%v %q, %d runs", tt.name, tt.refusal, result.IsError, message, len(got))
			}
			continue
		}
		if tt.handlerError {
			if !result.IsError || message != "handler refused" || len(got) != 1 {
				t.Errorf("%s: want one handler refusal, got isError=%v %q, %d runs", tt.name, result.IsError, message, len(got))
			}
			continue
		}
		if result.IsError || len(got) != 1 {
			t.Errorf("%s: want one clean run, got isError=%v %q, %d runs", tt.name, result.IsError, message, len(got))
			continue
		}
		if !reflect.DeepEqual(got[0], tt.want) {
			t.Errorf("%s: got %+v, want %+v", tt.name, got[0], tt.want)
		}
	}

	arguments, decoded := decodeArgument([]byte(`{"check":"9223372036854775807"}`), "check")
	if !decoded || string(arguments) != `{"check":9223372036854775807}` {
		t.Errorf("64-bit token passed to SDK = %s, decoded=%v; want the exact JSON number", arguments, decoded)
	}
}
