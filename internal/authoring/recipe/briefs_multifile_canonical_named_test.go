package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Run-40 fix-up #1 — multi-file composer parity for A1 + ENG-4.
//
// Production env-content + refinement dispatch routes through the
// MULTI-FILE composer path (briefs_subagent_prompt.go::buildSubagent
// DispatchForPhase dispatches BriefEnvContent + BriefRefinement to
// buildEnvContentBriefMultiFileWithFraming +
// buildRefinementBriefMultiFileWithFraming respectively, per the
// isMultiFileBriefKind predicate). The single-file BuildEnvContent
// Brief + BuildRefinementBrief paths exist only for tests.
//
// Pre-fix, ENG-4 (canonical-latest facts) and A1 (named constants)
// landed only on the single-file path. Tests passed; production
// agents never saw the substrate. Codex code review caught this
// integration gap. These tests pin the multi-file path emits both
// sections so the gap can't recur.

// TestRenderEnvContentContextBlock_CarriesCanonicalLatestSection
// pins the multi-file env-content path emits the canonical-latest
// section when aliased topics are present.
func TestRenderEnvContentContextBlock_CarriesCanonicalLatestSection(t *testing.T) {
	t.Parallel()
	plan := syntheticShowcasePlan()
	facts := []FactRecord{
		{Topic: "worker-nats-queue-group", RecordedAt: "2026-05-11T08:00:00Z", Kind: FactKindFieldRationale, FieldPath: "x", FieldValue: "showcase-workers"},
		{Topic: "worker-queue-group-renamed-workers", RecordedAt: "2026-05-11T09:00:00Z", Kind: FactKindFieldRationale, FieldPath: "y", FieldValue: "workers"},
	}
	got := renderEnvContentContextBlock(plan, nil, facts)
	if !strings.Contains(got, "Latest by canonical topic") {
		t.Errorf("multi-file env-content context missing canonical-latest section; body:\n%s", got)
	}
	if !strings.Contains(got, "canonical=nats-queue-group") {
		t.Errorf("multi-file env-content context missing canonical key entry")
	}
}

// TestRenderEnvContentContextBlock_CarriesNamedConstantsSection
// pins the multi-file env-content path emits the named-constants
// section when plan.NamedConstants is non-empty.
func TestRenderEnvContentContextBlock_CarriesNamedConstantsSection(t *testing.T) {
	t.Parallel()
	plan := syntheticShowcasePlan()
	plan.NamedConstants = map[string]string{
		"NATS_QUEUE_GROUP": "workers",
		"CACHE_PREFIX":     "ns0",
	}
	got := renderEnvContentContextBlock(plan, nil, nil)
	if !strings.Contains(got, "## Named constants") {
		t.Errorf("multi-file env-content context missing Named constants section; body tail:\n%s", got[len(got)/2:])
	}
	if !strings.Contains(got, "`NATS_QUEUE_GROUP` = `workers`") {
		t.Errorf("multi-file env-content context missing NATS_QUEUE_GROUP entry")
	}
	if !strings.Contains(got, "`CACHE_PREFIX` = `ns0`") {
		t.Errorf("multi-file env-content context missing CACHE_PREFIX entry")
	}
}

// TestRenderEnvContentContextBlock_OmitsSectionsWhenEmpty — when
// neither aliasing nor named-constants apply, neither section is
// rendered. Pins the noise-suppression contract.
func TestRenderEnvContentContextBlock_OmitsSectionsWhenEmpty(t *testing.T) {
	t.Parallel()
	plan := syntheticShowcasePlan()
	got := renderEnvContentContextBlock(plan, nil, nil)
	if strings.Contains(got, "## Named constants") {
		t.Errorf("expected no Named constants section without map")
	}
	if strings.Contains(got, "## Latest by canonical topic") {
		t.Errorf("expected no canonical-latest section without aliasing")
	}
}

// TestBuildRefinementBriefMultiFile_CarriesCanonicalAndNamedConstants
// pins the multi-file refinement composer emits the canonical-and-
// constants part when content is present.
func TestBuildRefinementBriefMultiFile_CarriesCanonicalAndNamedConstants(t *testing.T) {
	t.Parallel()
	outRoot := t.TempDir()
	plan := syntheticShowcasePlan()
	plan.NamedConstants = map[string]string{"NATS_QUEUE_GROUP": "workers"}
	facts := []FactRecord{
		{Topic: "worker-nats-queue-group", Scope: "worker", RecordedAt: "2026-05-11T08:00:00Z", Kind: FactKindFieldRationale, FieldPath: "x", FieldValue: "showcase-workers"},
		{Topic: "worker-queue-group-renamed-workers", Scope: "worker", RecordedAt: "2026-05-11T09:00:00Z", Kind: FactKindFieldRationale, FieldPath: "y", FieldValue: "workers"},
	}
	brief, err := BuildRefinementBriefMultiFile(plan, nil, outRoot, facts, outRoot, "", "")
	if err != nil {
		t.Fatalf("BuildRefinementBriefMultiFile: %v", err)
	}

	combined := concatPartBodies(t, brief.PartPaths)
	if !strings.Contains(combined, "Latest by canonical topic") {
		t.Errorf("multi-file refinement brief missing canonical-latest section")
	}
	if !strings.Contains(combined, "canonical=nats-queue-group") {
		t.Errorf("multi-file refinement brief missing canonical key entry")
	}
	if !strings.Contains(combined, "## Named constants") {
		t.Errorf("multi-file refinement brief missing Named constants section")
	}
	if !strings.Contains(combined, "`NATS_QUEUE_GROUP` = `workers`") {
		t.Errorf("multi-file refinement brief missing NATS_QUEUE_GROUP entry")
	}
}

// TestBuildRefinementBriefMultiFile_OmitsPartWhenNothingToEmit — no
// canonical aliasing AND no named-constants ⇒ the dedicated part is
// not opened. Catches regressions that would create an empty part.
func TestBuildRefinementBriefMultiFile_OmitsPartWhenNothingToEmit(t *testing.T) {
	t.Parallel()
	outRoot := t.TempDir()
	plan := syntheticShowcasePlan()
	plan.NamedConstants = nil
	brief, err := BuildRefinementBriefMultiFile(plan, nil, outRoot, nil, outRoot, "", "")
	if err != nil {
		t.Fatalf("BuildRefinementBriefMultiFile: %v", err)
	}
	for _, p := range brief.PartPaths {
		if strings.Contains(p, "canonical-and-constants") {
			t.Errorf("canonical-and-constants part should not exist when nothing to emit; got path %s", p)
		}
	}
}

// concatPartBodies reads every part file and concatenates the bodies
// so tests can grep the rendered output across part-file boundaries.
func concatPartBodies(t *testing.T, paths []string) string {
	t.Helper()
	var b strings.Builder
	for _, p := range paths {
		body, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read part %s: %v", p, err)
		}
		b.Write(body)
		b.WriteByte('\n')
	}
	_ = filepath.Base // silence linter — filepath import keeps test stable across go versions
	return b.String()
}
