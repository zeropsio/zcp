// Tests for: gates.go — codebase + env surface validators must read
// freshly-stitched fragment bodies in memory, not race the SSHFS
// write-back pipeline by re-reading what stitch just wrote (R-13-1).
//
// Cluster A.1 — validator in-memory plumbing. The body-map argument
// to runSurfaceValidatorsForKinds carries the assembler's deterministic
// output for every fragment-backed surface; only file-only surfaces
// (codebase zerops.yaml, agent ssh-edited) fall through to disk.

package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCodebaseSurfaceValidators_UsesInMemoryBodies pins Cluster A.1's
// fix for R-13-1. The test models the SSHFS write-back race by writing
// 0-byte stale README + CLAUDE files to disk while keeping healthy
// fragments in memory; the validator must consume the in-memory body
// (assembled from the fragment map), not the stale disk view. Pre-fix,
// `claude-md-too-short` and `codebase-ig-marker-missing` fire on the
// 0-byte disk reads even though stitch stored the full content.
func TestCodebaseSurfaceValidators_UsesInMemoryBodies(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	plan := syntheticShowcasePlan()
	stageScaffoldYAMLs(t, dir, plan)

	// Plant 0-byte stale README + CLAUDE files at every codebase
	// SourceRoot — what features-2 saw after SSHFS write-back hadn't
	// completed (the run-13 R-13-1 race symptom).
	for _, cb := range plan.Codebases {
		for _, leaf := range []string{"README.md", "CLAUDE.md"} {
			if err := os.WriteFile(filepath.Join(cb.SourceRoot, leaf), nil, 0o600); err != nil {
				t.Fatalf("plant stale %s/%s: %v", cb.SourceRoot, leaf, err)
			}
		}
	}

	// In-memory fragments are healthy: every codebase carries enough IG
	// items + bulky CLAUDE service-facts so the assembler emits surfaces
	// that pass the validators.
	plan.Fragments = map[string]string{}
	for _, cb := range plan.Codebases {
		base := "codebase/" + cb.Hostname
		plan.Fragments[base+"/intro"] = "Codebase intro paragraph.\n"
		plan.Fragments[base+"/integration-guide"] = "### 2. Adding zerops.yaml — extend\n\nDescribe step.\n\n### 3. Bind to 0.0.0.0\n\nLoopback is unreachable from L7.\n"
		plan.Fragments[base+"/knowledge-base"] = "- **404 on Topic** — explanation that satisfies the bullet contract.\n"
		// Run-16 — CLAUDE.md is single-slot, /init-shaped, ≥ 200 bytes,
		// 2-4 ## sections, no Zerops content. The single fragment
		// satisfies validateCodebaseCLAUDE without legacy sub-slots.
		plan.Fragments[base+"/claude-md"] = "# " + cb.Hostname + "\n\nApplication scaffold authored by the framework's stock generator.\n\n## Build & run\n\n- npm install\n- npm test\n- npm run start:dev\n\n## Architecture\n\n- src/main.ts — bootstrap entry\n- src/app.module.ts — root module\n- src/items/ — items domain\n"
	}

	sess := &Session{
		Slug:       plan.Slug,
		Current:    PhaseScaffold,
		Plan:       plan,
		OutputRoot: filepath.Join(dir, "run"),
		Completed:  map[Phase]bool{},
	}
	blocking, _, err := sess.CompletePhase(CodebaseGates())
	if err != nil {
		t.Fatalf("CompletePhase: %v", err)
	}
	for _, v := range blocking {
		// These are the run-13 R-13-1 stitch-race symptoms — they fire
		// only when the validator reads the stale 0-byte disk file.
		switch v.Code {
		case "codebase-ig-marker-missing",
			"codebase-kb-marker-missing",
			"claude-md-too-short",
			"validator-read-failed":
			t.Errorf("validator hit stale disk view (in-memory body ignored): %+v", v)
		}
	}
}

// TestCompletePhaseScoped_VerdictEquivalentToFullPhaseSlice pins
// Cluster A.1 + R-13-2: per-codebase scoped close and the matching
// slice of full-phase close return the same verdict for that codebase's
// content. Both passes derive validator inputs from the same Plan +
// Fragments via collectCodebaseBodies — equivalence is structural.
func TestCompletePhaseScoped_VerdictEquivalentToFullPhaseSlice(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	plan := syntheticShowcasePlan()
	stageScaffoldYAMLs(t, dir, plan)

	// Author a violating IG on api (plain ordered list shape — fires
	// codebase-ig-plain-ordered-list) and clean fragments on the
	// other codebases. The full-phase close must surface api's
	// violation; the api scoped close must surface the same violation.
	plan.Fragments = map[string]string{
		"codebase/api/integration-guide": "1. plain ordered\n2. list shape\n",
	}
	for _, cb := range plan.Codebases {
		if cb.Hostname == "api" {
			continue
		}
		base := "codebase/" + cb.Hostname
		plan.Fragments[base+"/integration-guide"] = "### 2. Adding zerops.yaml — extend\n\nDescribe.\n\n### 3. Bind to 0.0.0.0\n\nReason.\n"
	}

	full := *plan
	sessFull := &Session{
		Slug: plan.Slug, Current: PhaseScaffold, Plan: &full,
		OutputRoot: filepath.Join(dir, "run-full"), Completed: map[Phase]bool{},
	}
	fullBlocking, _, err := sessFull.CompletePhase(CodebaseGates())
	if err != nil {
		t.Fatalf("full CompletePhase: %v", err)
	}

	scoped := *plan
	sessScoped := &Session{
		Slug: plan.Slug, Current: PhaseScaffold, Plan: &scoped,
		OutputRoot: filepath.Join(dir, "run-scoped"), Completed: map[Phase]bool{},
	}
	scopedBlocking, _, err := sessScoped.CompletePhaseScoped(CodebaseGates(), "api")
	if err != nil {
		t.Fatalf("scoped CompletePhase: %v", err)
	}

	apiRoot := plan.Codebases[0].SourceRoot
	apiSliceOfFull := violationsForPathPrefix(fullBlocking, apiRoot)
	if !sameViolationCodeSet(scopedBlocking, apiSliceOfFull) {
		t.Errorf("scoped pass for api ≠ full-phase api slice:\n  scoped: %v\n  full(api): %v",
			codeSet(scopedBlocking), codeSet(apiSliceOfFull))
	}
	// And in particular both must include the violating IG code.
	if !containsCode(scopedBlocking, "codebase-ig-plain-ordered-list") {
		t.Errorf("scoped api close should report codebase-ig-plain-ordered-list; got %v", codeSet(scopedBlocking))
	}
	if !containsCode(apiSliceOfFull, "codebase-ig-plain-ordered-list") {
		t.Errorf("full-phase api slice should report codebase-ig-plain-ordered-list; got %v", codeSet(apiSliceOfFull))
	}
}

func violationsForPathPrefix(vs []Violation, prefix string) []Violation {
	var out []Violation
	for _, v := range vs {
		if strings.HasPrefix(v.Path, prefix) {
			out = append(out, v)
		}
	}
	return out
}

func sameViolationCodeSet(a, b []Violation) bool {
	ac := codeSet(a)
	bc := codeSet(b)
	if len(ac) != len(bc) {
		return false
	}
	for k := range ac {
		if !bc[k] {
			return false
		}
	}
	return true
}

func codeSet(vs []Violation) map[string]bool {
	out := map[string]bool{}
	for _, v := range vs {
		out[v.Code] = true
	}
	return out
}
