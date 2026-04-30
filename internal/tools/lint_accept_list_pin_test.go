package tools

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/content"
)

// TestAtomLintAcceptedActionsMatchDispatcher pins the
// content.AcceptedWorkflowActions list to the actual dispatcher switch in
// handleWorkflow. If a new action lands in tools/workflow.go but not in
// content/atoms_lint.go, atoms can carry an unrecognized action="X" without
// the staleActionViolations lint catching it — this test fails so the lint
// list is updated alongside the dispatcher.
//
// The test parses internal/tools/workflow.go, finds the handleWorkflow
// function's outermost switch on input.Action, and extracts every
// `case "X":` literal. The resulting set must match
// content.AcceptedWorkflowActions exactly. Special-case action handlers
// that are matched BEFORE the switch (dispatch-brief-atom, record-deploy)
// are also added to the set so the lint accept-list covers them.
func TestAtomLintAcceptedActionsMatchDispatcher(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "workflow.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse workflow.go: %v", err)
	}

	got := map[string]bool{}
	// 1) Special-case actions matched before the switch (dispatch-brief-atom,
	// record-deploy) appear as `if input.Action == "X"` guards.
	ast.Inspect(f, func(n ast.Node) bool {
		bin, ok := n.(*ast.BinaryExpr)
		if !ok || bin.Op != token.EQL {
			return true
		}
		sel, ok := bin.X.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil || sel.Sel.Name != "Action" {
			return true
		}
		if lit, ok := bin.Y.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			got[strings.Trim(lit.Value, `"`)] = true
		}
		return true
	})
	// 2) Switch cases inside handleWorkflowAction.
	ast.Inspect(f, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Name.Name != "handleWorkflowAction" {
			return true
		}
		ast.Inspect(fn.Body, func(in ast.Node) bool {
			cc, ok := in.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, expr := range cc.List {
				if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					got[strings.Trim(lit.Value, `"`)] = true
				}
			}
			return true
		})
		return false
	})

	want := map[string]bool{}
	for _, a := range content.AcceptedWorkflowActions {
		want[a] = true
	}

	missing := diffKeys(want, got)
	extra := diffKeys(got, want)
	if len(missing) > 0 || len(extra) > 0 {
		t.Errorf("content.AcceptedWorkflowActions ↔ handleWorkflow drift\n  missing from lint list (in dispatcher, not in lint): %v\n  extra in lint list (in lint, not in dispatcher): %v\nUpdate content.AcceptedWorkflowActions in internal/content/atoms_lint.go.",
			missing, extra)
	}
}

// TestAtomLintAcceptedStrategiesMatchGate pins
// content.AcceptedDeployStrategies to validateDeployStrategyParam in
// deploy_strategy_gate.go. The lint list must enumerate every non-default
// strategy value the gate accepts.
func TestAtomLintAcceptedStrategiesMatchGate(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "deploy_strategy_gate.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse deploy_strategy_gate.go: %v", err)
	}

	got := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Name.Name != "validateDeployStrategyParam" {
			return true
		}
		ast.Inspect(fn.Body, func(in ast.Node) bool {
			cc, ok := in.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, expr := range cc.List {
				if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					v := strings.Trim(lit.Value, `"`)
					// Skip explicit-rejection cases: these branches reject the
					// value with a redirect, they don't accept it. "manual"
					// is a ServiceMeta declaration; "zcli" is the internal
					// Strategy label recorded into DeployAttempt — both are
					// known-bad-as-tool-arg labels surfaced to nudge the
					// agent toward the right form (close-mode action / omit
					// the parameter respectively). They MUST stay out of
					// `content.AcceptedDeployStrategies` because that list
					// gates atom-body lint of `strategy="X"` mentions.
					if v == "" || v == "manual" || v == "zcli" {
						continue
					}
					got[v] = true
				}
				if id, ok := expr.(*ast.Ident); ok && id.Name == "deployStrategyGitPush" {
					got["git-push"] = true
				}
			}
			return true
		})
		return false
	})

	want := map[string]bool{}
	for _, s := range content.AcceptedDeployStrategies {
		want[s] = true
	}

	missing := diffKeys(want, got)
	extra := diffKeys(got, want)
	if len(missing) > 0 || len(extra) > 0 {
		t.Errorf("content.AcceptedDeployStrategies ↔ validateDeployStrategyParam drift\n  missing from lint list: %v\n  extra in lint list: %v",
			missing, extra)
	}
}

func diffKeys(a, b map[string]bool) []string {
	var out []string
	for k := range a {
		if !b[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
