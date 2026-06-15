package recipe

import (
	"path/filepath"
	"slices"
	"testing"
)

// Run-17 §8 — CodebaseGates split (R-16-1 closure). The scaffold/feature
// gate set is fact-quality only; content-surface validators move to
// the codebase-content phase.

func TestCodebaseScaffoldGates_OnlyFactQuality(t *testing.T) {
	t.Parallel()
	gates := CodebaseScaffoldGates()
	names := gateNames(gates)
	for _, want := range []string{"facts-recorded", "engine-shells-filled", "source-comment-voice"} {
		if !contains(names, want) {
			t.Errorf("CodebaseScaffoldGates missing %q; got %v", want, names)
		}
	}
	for _, forbidden := range []string{"codebase-surface-validators", "cross-surface-duplication", "cross-recipe-duplication"} {
		if contains(names, forbidden) {
			t.Errorf("CodebaseScaffoldGates should not contain %q (content-surface validator); got %v", forbidden, names)
		}
	}
}

func TestCodebaseContentGates_OwnsContentSurfaces(t *testing.T) {
	t.Parallel()
	gates := CodebaseContentGates()
	names := gateNames(gates)
	for _, want := range []string{"codebase-surface-validators", "cross-surface-duplication", "cross-recipe-duplication"} {
		if !contains(names, want) {
			t.Errorf("CodebaseContentGates missing %q; got %v", want, names)
		}
	}
	for _, forbidden := range []string{"facts-recorded", "engine-shells-filled"} {
		if contains(names, forbidden) {
			t.Errorf("CodebaseContentGates should not contain %q (fact-quality gate); got %v", forbidden, names)
		}
	}
}

func TestCodebaseGates_BackCompat_UnionsBoth(t *testing.T) {
	t.Parallel()
	gates := CodebaseGates()
	names := gateNames(gates)
	// Back-compat shim returns the union — every gate from both halves
	// must appear so existing callers keep producing the same result.
	for _, want := range []string{
		"facts-recorded",
		"engine-shells-filled",
		"source-comment-voice",
		"codebase-surface-validators",
		"cross-surface-duplication",
		"cross-recipe-duplication",
	} {
		if !contains(names, want) {
			t.Errorf("CodebaseGates back-compat union missing %q; got %v", want, names)
		}
	}
}

// gateFactsRecorded fires a notice per codebase that has no
// porter_change / field_rationale fact recorded — the scaffold-skip-
// to-finalize symptom that R-16-1 was meant to surface.

func TestCodebaseScaffoldGates_FactsRecorded_AllCodebasesPass_NoViolation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	plan := &Plan{
		Slug: "x",
		Codebases: []Codebase{
			{Hostname: "api", SourceRoot: filepath.Join(dir, "api")},
			{Hostname: "worker", SourceRoot: filepath.Join(dir, "worker"), IsWorker: true},
		},
	}
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	for _, host := range []string{"api", "worker"} {
		if err := log.Append(FactRecord{
			Topic:            host + "-bind",
			Kind:             FactKindPorterChange,
			Scope:            host + "/code",
			Why:              "test",
			CandidateClass:   "platform-invariant",
			CandidateSurface: "CODEBASE_IG",
		}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	ctx := GateContext{Plan: plan, FactsLog: log}
	vs := gateFactsRecorded(ctx)
	if len(vs) != 0 {
		t.Errorf("expected no violations when every codebase has facts; got %v", vs)
	}
}

func TestCodebaseScaffoldGates_FactsRecorded_MissingCodebase_RaisesNotice(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	plan := &Plan{
		Slug: "x",
		Codebases: []Codebase{
			{Hostname: "api", SourceRoot: filepath.Join(dir, "api")},
			{Hostname: "worker", SourceRoot: filepath.Join(dir, "worker"), IsWorker: true},
		},
	}
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	// Only api records a fact; worker is skipped.
	if err := log.Append(FactRecord{
		Topic:            "api-bind",
		Kind:             FactKindPorterChange,
		Scope:            "api/code",
		Why:              "test",
		CandidateClass:   "platform-invariant",
		CandidateSurface: "CODEBASE_IG",
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	ctx := GateContext{Plan: plan, FactsLog: log}
	vs := gateFactsRecorded(ctx)
	if len(vs) != 1 {
		t.Fatalf("expected 1 notice for the worker codebase; got %d (%v)", len(vs), vs)
	}
	if vs[0].Code != "codebase-no-facts-recorded" {
		t.Errorf("notice code = %q, want codebase-no-facts-recorded", vs[0].Code)
	}
	if vs[0].Severity != SeverityNotice {
		t.Errorf("notice severity = %v, want SeverityNotice", vs[0].Severity)
	}
	if vs[0].Path != "worker" {
		t.Errorf("notice path = %q, want worker", vs[0].Path)
	}
}

// Helpers — local to this test file to avoid name collisions with
// other test files.

func gateNames(gates []Gate) []string {
	out := make([]string, len(gates))
	for i, g := range gates {
		out[i] = g.Name
	}
	return out
}

func contains(haystack []string, needle string) bool {
	return slices.Contains(haystack, needle)
}
