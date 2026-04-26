// Pin-density gate per atom-corpus-hygiene plan §7 Phase 0 step 3.
//
// Asserts every atom_id loaded by LoadAtomCorpus() is named as a wantID
// argument to requireAtomIDsContain or requireAtomIDsExact in
// scenarios_test.go (parsed via go/ast — substring search would count
// comments + the allowlist's own declaration text, neither of which
// represents a real assertion).
//
// The allowlist `knownUnpinnedAtoms` ratchets shrink-only via
// TestCorpusCoverage_PinDensity_StillUnpinned; Phase 8 EXIT empties
// the allowlist.
//
// File-isolation rule (Codex round 1 axis 1.2): the allowlist + tests
// live in this dedicated file, NOT in corpus_coverage_test.go. The
// allowlist's atom-IDs do not enter the AST-parsed haystack because
// the parser only reads scenarios_test.go.
package workflow

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

// knownUnpinnedAtoms is the Phase 0 starting allowlist — atoms that
// currently lack a scenarios_test.go atom-ID pin (verified via the
// AST-based pinnedAtomIDs scan against the 2026-04-26 corpus). Each
// Phase 1-8 commit that adds a new pin MUST remove the matching entry
// in the same commit (R5 mitigation). Phase 8 EXIT empties this map.
//
// Source: plans/audit-composition/unpinned-atoms-baseline.md (commit d642de60).
var knownUnpinnedAtoms = map[string]string{
	"bootstrap-adopt-discover":                      "(Phase 0): no scenarios_test pin.",
	"bootstrap-classic-plan-dynamic":                "(Phase 0): no scenarios_test pin.",
	"bootstrap-classic-plan-static":                 "(Phase 0): no scenarios_test pin.",
	"bootstrap-close":                               "(Phase 0): no scenarios_test pin.",
	"bootstrap-discover-local":                      "(Phase 0): no scenarios_test pin.",
	"bootstrap-env-var-discovery":                   "(Phase 0): no scenarios_test pin.",
	"bootstrap-mode-prompt":                         "(Phase 0): no scenarios_test pin.",
	"bootstrap-provision-local":                     "(Phase 0): no scenarios_test pin.",
	"bootstrap-provision-rules":                     "(Phase 0): no scenarios_test pin.",
	"bootstrap-recipe-close":                        "(Phase 0): no scenarios_test pin.",
	"bootstrap-recipe-match":                        "(Phase 0): no scenarios_test pin.",
	"bootstrap-resume":                              "(Phase 0): no scenarios_test pin.",
	"bootstrap-route-options":                       "(Phase 0): no scenarios_test pin.",
	"bootstrap-runtime-classes":                     "(Phase 0): no scenarios_test pin.",
	"bootstrap-verify":                              "(Phase 0): no scenarios_test pin.",
	"bootstrap-wait-active":                         "(Phase 0): no scenarios_test pin.",
	"develop-api-error-meta":                        "(Phase 0): no scenarios_test pin.",
	"develop-auto-close-semantics":                  "(Phase 0): no scenarios_test pin.",
	"develop-change-drives-deploy":                  "(Phase 0): no scenarios_test pin.",
	"develop-checklist-dev-mode":                    "(Phase 0): no scenarios_test pin.",
	"develop-checklist-simple-mode":                 "(Phase 0): no scenarios_test pin.",
	"develop-close-manual":                          "(Phase 0): no scenarios_test pin.",
	"develop-close-push-dev-dev":                    "(Phase 0): no scenarios_test pin.",
	"develop-close-push-dev-local":                  "(Phase 0): no scenarios_test pin.",
	"develop-close-push-dev-simple":                 "(Phase 0): no scenarios_test pin.",
	"develop-close-push-dev-standard":               "(Phase 0): no scenarios_test pin.",
	"develop-close-push-git-container":              "(Phase 0): no scenarios_test pin.",
	"develop-close-push-git-local":                  "(Phase 0): no scenarios_test pin.",
	"develop-deploy-files-self-deploy":              "(Phase 0): no scenarios_test pin.",
	"develop-deploy-modes":                          "(Phase 0): no scenarios_test pin.",
	"develop-dev-server-reason-codes":               "(Phase 0): no scenarios_test pin.",
	"develop-dev-server-triage":                     "(Phase 0): no scenarios_test pin.",
	"develop-dynamic-runtime-start-container":       "(Phase 0): no scenarios_test pin.",
	"develop-dynamic-runtime-start-local":           "(Phase 0): no scenarios_test pin.",
	"develop-env-var-channels":                      "(Phase 0): no scenarios_test pin.",
	"develop-first-deploy-asset-pipeline-container": "(Phase 0): no scenarios_test pin.",
	"develop-first-deploy-asset-pipeline-local":     "(Phase 0): no scenarios_test pin.",
	"develop-first-deploy-env-vars":                 "(Phase 0): no scenarios_test pin.",
	"develop-first-deploy-execute":                  "(Phase 0): no scenarios_test pin.",
	"develop-first-deploy-execute-cmds":             "(Phase 0): no scenarios_test pin.",
	"develop-first-deploy-intro":                    "(Phase 0): no scenarios_test pin.",
	"develop-first-deploy-promote-stage":            "(Phase 0): no scenarios_test pin.",
	"develop-first-deploy-scaffold-yaml":            "(Phase 0): no scenarios_test pin.",
	"develop-first-deploy-verify":                   "(Phase 0): no scenarios_test pin.",
	"develop-first-deploy-verify-cmds":              "(Phase 0): no scenarios_test pin.",
	"develop-first-deploy-write-app":                "(Phase 0): no scenarios_test pin.",
	"develop-http-diagnostic":                       "(Phase 0): no scenarios_test pin.",
	"develop-implicit-webserver":                    "(Phase 0): no scenarios_test pin.",
	"develop-intro":                                 "(Phase 0): no scenarios_test pin.",
	"develop-knowledge-pointers":                    "(Phase 0): no scenarios_test pin.",
	"develop-local-workflow":                        "(Phase 0): no scenarios_test pin.",
	"develop-manual-deploy":                         "(Phase 0): no scenarios_test pin.",
	"develop-mode-expansion":                        "(Phase 0): no scenarios_test pin.",
	"develop-platform-rules-common":                 "(Phase 0): no scenarios_test pin.",
	"develop-platform-rules-container":              "(Phase 0): no scenarios_test pin.",
	"develop-platform-rules-local":                  "(Phase 0): no scenarios_test pin.",
	"develop-push-dev-deploy-local":                 "(Phase 0): no scenarios_test pin.",
	"develop-push-dev-workflow-simple":              "(Phase 0): no scenarios_test pin.",
	"develop-push-git-deploy":                       "(Phase 0): no scenarios_test pin.",
	"develop-ready-to-deploy":                       "(Phase 0): no scenarios_test pin.",
	"develop-static-workflow":                       "(Phase 0): no scenarios_test pin.",
	"develop-strategy-awareness":                    "(Phase 0): no scenarios_test pin.",
	"develop-verify-matrix":                         "(Phase 0): no scenarios_test pin.",
	"idle-orphan-cleanup":                           "(Phase 0): no scenarios_test pin.",
	"strategy-push-git-push-container":              "(Phase 0): no scenarios_test pin.",
	"strategy-push-git-push-local":                  "(Phase 0): no scenarios_test pin.",
	"strategy-push-git-trigger-actions":             "(Phase 0): no scenarios_test pin.",
	"strategy-push-git-trigger-webhook":             "(Phase 0): no scenarios_test pin.",
}

// pinnedAtomIDs builds the set of atom IDs that scenarios_test.go pins
// via requireAtomIDsContain or requireAtomIDsExact. Both helpers have
// signature (t, label, matches, wantIDs ...string), so string-literal
// args from index 3 onward are the pinned atom IDs.
//
// Parsing scenarios_test.go via go/ast (rather than substring search)
// avoids two failure modes:
//  1. Comments or rationale strings that mention an atom-ID without
//     asserting on it would otherwise be counted as pins.
//  2. Recursive self-detection — substring search of the allowlist's
//     own declaration would always see those atom-IDs in the haystack.
//
// AST parsing reads scenarios_test.go ONLY; the allowlist lives in
// this file, so its declarations are not in the haystack.
func pinnedAtomIDs(t *testing.T) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "scenarios_test.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse scenarios_test.go: %v", err)
	}
	pinned := make(map[string]bool)
	ast.Inspect(f, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		ident, ok := ce.Fun.(*ast.Ident)
		if !ok {
			return true
		}
		if ident.Name != "requireAtomIDsContain" && ident.Name != "requireAtomIDsExact" {
			return true
		}
		if len(ce.Args) < 4 {
			return true
		}
		for _, arg := range ce.Args[3:] {
			bl, ok := arg.(*ast.BasicLit)
			if !ok || bl.Kind != token.STRING {
				continue
			}
			s, err := strconv.Unquote(bl.Value)
			if err != nil {
				continue
			}
			pinned[s] = true
		}
		return true
	})
	return pinned
}

// TestCorpusCoverage_PinDensity asserts every loaded atom is named by a
// scenarios_test.go pin call UNLESS allowlisted in knownUnpinnedAtoms.
// The allowlist ratchets shrink-only via _StillUnpinned below.
func TestCorpusCoverage_PinDensity(t *testing.T) {
	t.Parallel()
	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}
	pinned := pinnedAtomIDs(t)

	for _, atom := range corpus {
		if _, allowed := knownUnpinnedAtoms[atom.ID]; allowed {
			continue
		}
		if !pinned[atom.ID] {
			t.Errorf("atom %q has no scenarios_test.go pin "+
				"(requireAtomIDsContain or requireAtomIDsExact); "+
				"add a pin OR (last resort) allowlist via knownUnpinnedAtoms",
				atom.ID)
		}
	}
}

// TestCorpusCoverage_PinDensity_StillUnpinned mirrors
// TestCorpusCoverage_KnownOverflows_StillOverflow. Two checks:
//
//  1. stale-entry — every allowlist key MUST still exist in
//     LoadAtomCorpus(); deleting an atom requires removing its
//     allowlist row in the same commit.
//  2. ratchet — every allowlist entry MUST still be unpinned;
//     adding a pin requires removing the allowlist row in the same
//     commit (R5 mitigation: shrink-only ratchet).
func TestCorpusCoverage_PinDensity_StillUnpinned(t *testing.T) {
	t.Parallel()
	if len(knownUnpinnedAtoms) == 0 {
		t.Skip("allowlist empty — Phase 8 done")
	}
	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}
	corpusIDs := make(map[string]bool, len(corpus))
	for _, a := range corpus {
		corpusIDs[a.ID] = true
	}
	pinned := pinnedAtomIDs(t)

	for id, rationale := range knownUnpinnedAtoms {
		if !corpusIDs[id] {
			t.Errorf("knownUnpinnedAtoms lists %q but no such atom "+
				"exists — remove the stale entry (rationale was: %s)",
				id, rationale)
			continue
		}
		if pinned[id] {
			t.Errorf("atom %q is now pinned in scenarios_test.go "+
				"(rationale at acknowledgement: %s) — remove from "+
				"knownUnpinnedAtoms in the same commit that added the pin",
				id, rationale)
		}
	}
}
