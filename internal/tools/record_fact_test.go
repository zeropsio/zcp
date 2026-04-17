package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
)

func TestRecordFact_AppendsToSessionLog(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_ = ctx
	engine := testEngine(t)

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterRecordFact(srv, engine)

	result := callTool(t, srv, "zerops_record_fact", map[string]any{
		"type":        ops.FactTypeGotchaCandidate,
		"title":       "module: nodenext + raw ts-node",
		"substep":     "deploy.deploy-dev",
		"codebase":    "workerdev",
		"mechanism":   "ts-node against module: nodenext",
		"failureMode": "Cannot find module",
		"fixApplied":  "Flip tsconfig to commonjs",
		"evidence":    "deploy log line 12:35",
	})
	if result.IsError {
		t.Fatalf("tool returned error: %s", getTextContent(t, result))
	}

	path := ops.FactLogPath(engine.SessionID())
	got, err := ops.ReadFacts(path)
	if err != nil {
		t.Fatalf("read facts: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 record, got %d", len(got))
	}
	if got[0].Title != "module: nodenext + raw ts-node" {
		t.Errorf("title: %q", got[0].Title)
	}
	if got[0].Codebase != "workerdev" {
		t.Errorf("codebase: %q", got[0].Codebase)
	}
}

func TestRecordFact_RejectsUnknownType(t *testing.T) {
	t.Parallel()
	engine := testEngine(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterRecordFact(srv, engine)

	result := callTool(t, srv, "zerops_record_fact", map[string]any{
		"type":  "wrong_kind",
		"title": "x",
	})
	text := getTextContent(t, result)
	if !strings.Contains(text, "unknown") {
		t.Errorf("expected unknown-type error, got: %s", text)
	}
}

func TestRecordFact_RequiresActiveSession(t *testing.T) {
	t.Parallel()
	engine := testEngine(t)
	if err := engine.Reset(); err != nil {
		t.Fatalf("reset: %v", err)
	}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterRecordFact(srv, engine)

	result := callTool(t, srv, "zerops_record_fact", map[string]any{
		"type":  ops.FactTypeGotchaCandidate,
		"title": "test",
	})
	text := getTextContent(t, result)
	if !strings.Contains(strings.ToLower(text), "session") {
		t.Errorf("expected session error, got: %s", text)
	}
}
