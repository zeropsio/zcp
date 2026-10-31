package hq

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

func TestClient_OriginatingInvocation_OnlyOpenAndDescribeCarryExactTask(t *testing.T) {
	t.Parallel()
	invocation := topology.Invocation{ID: strings.Repeat("a", 64), Scope: topology.InvocationScope{Kind: "task", TaskID: "11111111-1111-4111-8111-111111111111", Assignment: "9007199254740993"}}
	ctx, err := WithInvocation(context.Background(), invocation)
	if err != nil {
		t.Fatal(err)
	}
	// Mutating the caller's value cannot relabel an already captured call.
	invocation.Scope.Assignment = "2"
	client, calls := answering(t, openSample)
	if _, err := client.OpenChange(ctx, "appdev", "Actual call", "work"); err != nil {
		t.Fatal(err)
	}
	var body struct {
		Origin *taskOrigin `json:"originatingTask"`
	}
	if err := json.Unmarshal([]byte((*calls)[0].Body), &body); err != nil || body.Origin == nil || body.Origin.Assignment != "9007199254740993" {
		t.Fatal(body, err)
	}
	editor, edits := answering(t, describedSample)
	text := "Actual task text"
	if _, err := editor.EditChange(ctx, "appdev", 3, ChangeEdit{Body: &text}); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte((*edits)[0].Body), &body); err != nil || body.Origin == nil || body.Origin.Assignment != "9007199254740993" {
		t.Fatal(body, err)
	}
	manual, requests := answering(t, openSample)
	if _, err := manual.OpenChange(t.Context(), "appdev", "Manual", "work"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains((*requests)[0].Body, "originatingTask") {
		t.Fatal("ordinary manual work gained a task", (*requests)[0].Body)
	}
	if _, err := WithInvocation(t.Context(), topology.Invocation{Scope: topology.InvocationScope{Kind: "task"}}); err == nil {
		t.Fatal("unknown required scope became manual")
	}
}
