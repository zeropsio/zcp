package tools

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestDescribeChange_LateSuccessPreservesTheExactSuccessorInvocation(t *testing.T) {
	t.Parallel()
	original := topology.Invocation{ID: strings.Repeat("a", 64), Scope: topology.InvocationScope{Kind: "task", TaskID: "11111111-1111-1111-1111-111111111111", Assignment: "9007199254740993"}}
	successor := original
	successor.ID = strings.Repeat("b", 64)
	nextGrant := original
	nextGrant.Scope.Assignment = "9007199254740994"
	for _, tt := range []struct {
		name, title string
		origin      *topology.Invocation
		keep        bool
	}{
		{name: "same accepted call", title: "Original", origin: &original},
		{name: "same scope different call", title: "Original", origin: &successor, keep: true},
		{name: "different assignment", title: "Original", origin: &nextGrant, keep: true},
		{name: "different title", title: "New title", origin: &original, keep: true},
		{name: "historical unknown", title: "Original", keep: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			ref := changeRef{AppID: "app-1", Repo: "appdev", Number: 1}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var sent struct {
					Origin struct {
						Assignment string `json:"assignment"`
					} `json:"originatingTask"`
				}
				if err := json.NewDecoder(r.Body).Decode(&sent); err != nil || sent.Origin.Assignment != original.Scope.Assignment {
					t.Errorf("wrong original HTTP authority: %+v %v", sent, err)
				}
				// A newer accepted call is kept while this older HTTP reply is in flight.
				err := workflow.UpsertHQChange(stateDir, ref.AppID, ref.Repo, ref.Number, func(c *workflow.HQChange, _ bool) error {
					c.Description = &workflow.ChangeDescription{Text: "Same words", Title: tt.title, Change: 1, Invocation: tt.origin}
					return nil
				})
				if err != nil {
					t.Error(err)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"number":1}`))
			}))
			defer server.Close()
			client, err := hq.Open(server.Client(), hq.Identity{Origin: server.URL, Key: "project-key", ProjectID: "mate", OrgID: "org"})
			if err != nil {
				t.Fatal(err)
			}
			ctx, err := hq.WithInvocation(t.Context(), original)
			if err != nil {
				t.Fatal(err)
			}
			if err := putChangeDescription(ctx, client, stateDir, ref, "Same words", "Original"); err != nil {
				t.Fatal(err)
			}
			rows := hqChanges(stateDir)
			if len(rows) != 1 {
				t.Fatal(rows)
			}
			if (rows[0].Description != nil) != tt.keep {
				t.Fatalf("late reply kept=%v, want%v; successor=%+v", rows[0].Description != nil, tt.keep, rows[0].Description)
			}
		})
	}
}

func TestOpenChange_TaskScopeRequiresHQAcceptanceEvenForAnExistingBranch(t *testing.T) {
	t.Parallel()
	lab := newHQLab(t)
	lab.wire()
	lab.hq.handOver("appdev", "Existing handed work", "work", nil)
	invocation := topology.Invocation{ID: strings.Repeat("a", 64), Scope: topology.InvocationScope{Kind: "task", TaskID: "11111111-1111-1111-1111-111111111111", Assignment: "9007199254740993"}}
	ctx, err := hq.WithInvocation(t.Context(), invocation)
	if err != nil {
		t.Fatal(err)
	}
	result, _, err := handleOpenChange(ctx, lab.hq.srv.Client(), lab.rt, lab.stateDir, WorkflowInput{Service: "appdev", Branch: "work", Title: "Existing handed work"})
	if err != nil || result.IsError {
		t.Fatalf("%+v %v", result, err)
	}
	if n := lab.hq.callCount("POST /api/mate/changes"); n != 1 {
		t.Fatalf("task-bound existing change was accepted without HQ proof: POST count%d", n)
	}
	lab.hq.mu.Lock()
	delete(lab.hq.handed, "appdev#1")
	lab.hq.mu.Unlock()
	hqc, _ := openHQ(lab.hq.srv.Client(), lab.rt)
	before, err := hqc.Self(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, refusal := openUnanswered(ctx, hqc, changeTarget{AppID: labApp, Repo: "appdev"}, "work", fmt.Errorf("lost response"), before, nil)
	if refusal == nil {
		t.Fatal("Self's branch is not evidence that this originating task was bound")
	}
}

func TestDescribeChange_KeptWordsRetainTheirOriginalInvocationAcrossLedgerReload(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	ref := changeRef{AppID: "app-1", Repo: "appdev", Number: 1}
	original := topology.Invocation{ID: strings.Repeat("a", 64), Scope: topology.InvocationScope{Kind: "task", TaskID: "11111111-1111-1111-1111-111111111111", Assignment: "9007199254740993"}}
	ctx, err := hq.WithInvocation(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	if err := keepChangeDescription(ctx, stateDir, ref, changePick{}, "Kept words", "Title"); err != nil {
		t.Fatal(err)
	}
	original.Scope.Assignment = "2"
	rows := hqChanges(stateDir)
	if len(rows) != 1 || rows[0].Description == nil || rows[0].Description.Invocation == nil || rows[0].Description.Invocation.Scope.Assignment != "9007199254740993" {
		t.Fatalf("original invocation was lost or relabelled: %+v", rows)
	}
	// An older ledger has no originating scope. Reading it must not turn that
	// absence into manual provenance for some future replay.
	if err := workflow.UpsertHQChange(stateDir, ref.AppID, ref.Repo, ref.Number, func(c *workflow.HQChange, _ bool) error { c.Description.Invocation = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	rows = hqChanges(stateDir)
	if rows[0].Description.Invocation != nil {
		t.Fatal("historical unknown scope became manual")
	}
}
