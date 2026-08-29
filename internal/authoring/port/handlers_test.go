package port

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/schema"
)

// callPort connects to a test server and calls the zerops_port tool with the
// given arguments (in-memory transport — the same harness the core tools
// tests use).
func callPort(t *testing.T, srv *mcp.Server, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()

	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: toolName, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func getTextContent(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("no content in result")
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected *mcp.TextContent, got %T", result.Content[0])
	}
	return tc.Text
}

func decodePortJSON(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()
	text := getTextContent(t, result)
	var doc map[string]any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("decode port response: %v\nbody=%s", err, text)
	}
	return doc
}

// portTestServer registers the port tool against a fresh state dir and
// returns the server + dir. Non-parallel callers only: the PortSession is
// keyed by os.Getpid(), so two parallel tests in the SAME process sharing a
// stateDir would collide — each test uses its own t.TempDir() to stay
// isolated.
func portTestServer(t *testing.T) (*mcp.Server, string) {
	t.Helper()
	dir := t.TempDir()
	cache := schema.NewCache(15*time.Minute, "", nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	Register(srv, Deps{
		Schemas:     func() *schema.Schemas { return cache.Get(context.Background()) },
		StateDir:    dir,
		ProjectID:   "proj1",
		Environment: "container",
	})
	return srv, dir
}

func startPort(t *testing.T, srv *mcp.Server) {
	t.Helper()
	res := callPort(t, srv, map[string]any{
		"action": "start",
		"target": map[string]any{
			"name":            "strapi",
			"acquisitionHint": "source-repo",
			"dependencies":    []any{"postgresql"},
			"runtimes":        []any{"nodejs@22"},
		},
	})
	if res.IsError {
		t.Fatalf("port start failed: %s", getTextContent(t, res))
	}
}

// TestHandlePort_Start_ReturnsPopulatedPortPlan covers recon: the agent
// supplies a target descriptor, recon classifies it, the handler returns the
// PortPlan + band as the response with ZERO deploy.
func TestHandlePort_Start_ReturnsPopulatedPortPlan(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cache := schema.NewCache(15*time.Minute, "", nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	Register(srv, Deps{
		Schemas:     func() *schema.Schemas { return cache.Get(context.Background()) },
		StateDir:    dir,
		ProjectID:   "proj1",
		Environment: "container",
	})

	result := callPort(t, srv, map[string]any{
		"action": "start",
		"target": map[string]any{
			"name":            "strapi",
			"acquisitionHint": "source-repo",
			"dependencies":    []any{"postgresql", "object-storage"},
			"runtimes":        []any{"nodejs@22"},
		},
	})
	if result.IsError {
		t.Fatalf("port start should not error, got: %s", getTextContent(t, result))
	}

	body := decodePortJSON(t, result)
	if body["status"] != "recon" {
		t.Errorf("expected status=recon, got %v", body["status"])
	}
	plan, ok := body["portPlan"].(map[string]any)
	if !ok {
		t.Fatalf("expected portPlan object, got %T: %v", body["portPlan"], body["portPlan"])
	}
	if plan["target"] != "strapi" {
		t.Errorf("portPlan.target: got %v", plan["target"])
	}
	if plan["acquisition"] != string(AcquireSourceBuild) {
		t.Errorf("portPlan.acquisition: got %v", plan["acquisition"])
	}
	if plan["band"] != string(BandEasy) {
		t.Errorf("portPlan.band: got %v want %v", plan["band"], BandEasy)
	}
}

func TestHandlePort_Start_MissingDescriptor_Errors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	Register(srv, Deps{StateDir: dir})

	result := callPort(t, srv, map[string]any{
		"action": "start",
	})
	if !result.IsError {
		t.Fatalf("port start without a descriptor must error")
	}
}

func TestHandlePort_UnknownAction_Errors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	Register(srv, Deps{StateDir: dir})

	result := callPort(t, srv, map[string]any{"action": "reset"})
	if !result.IsError {
		t.Fatalf("unknown port action must error with the action menu")
	}
}

// TestPortInputSchema_FlexBoolPublished pins the published input schema:
// every FlexBool property — top-level AND nested (rubric, harden, glueRepo)
// — must accept both a JSON boolean and a stringified boolean, so an agent
// sending {"deploySucceeded": "true"} is tolerated at the unmarshal layer
// instead of rejected at the schema layer.
func TestPortInputSchema_FlexBoolPublished(t *testing.T) {
	srv, _ := portTestServer(t)
	startPort(t, srv)

	// Top-level FlexBool as a stringified boolean.
	res := callPort(t, srv, map[string]any{
		"action":          "iterate",
		"deploySucceeded": "true",
	})
	if res.IsError {
		t.Fatalf("stringified deploySucceeded must pass schema + unmarshal, got: %s", getTextContent(t, res))
	}

	// Nested FlexBool (rubric.buildSucceeded) as a stringified boolean.
	res = callPort(t, srv, map[string]any{
		"action": "harden",
		"rubric": map[string]any{
			"buildSucceeded": "true",
			"reachedActive":  true,
		},
	})
	if res.IsError {
		t.Fatalf("stringified nested rubric bool must pass schema + unmarshal, got: %s", getTextContent(t, res))
	}
}
