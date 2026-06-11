package recipe

import (
	"context"
	"strings"
	"testing"
)

// Run-29 Fix #2 — IG #1 validator scope correction.
//
// The legacy `codebase-ig-scaffold-filename` Blocking gate at
// validators_codebase.go:81-93 banned `migrate.ts`/`seed.ts`/`main.ts`/
// `api.ts` literals across ALL IG content, including engine-stamped
// IG #1 yaml that legitimately names the codebase's own initCommands
// sources; the agent's evasion (delete the engine emit) became the
// shape that shipped (system.md §4 catalog-drift). The Blocking gate is
// removed; the underlying signal is now a record-time Notice scoped to
// IG fragment text OUTSIDE any ```yaml fenced block.

// TestValidateCodebaseIG_ScaffoldFilenameAnywhere_NoBlockingViolation —
// IG body with `migrate.ts` anywhere → no
// `codebase-ig-scaffold-filename` Blocking violation. The Blocking
// gate is gone; only Notice surfaces (and only via the record-fragment
// emission path, not the validator).
func TestValidateCodebaseIG_ScaffoldFilenameAnywhere_NoBlockingViolation(t *testing.T) {
	t.Parallel()

	body := "<!-- #ZEROPS_EXTRACT_START:integration-guide# -->\n\n" +
		"### 1. Adding `zerops.yaml`\n\n" +
		"```yaml\nzerops:\n  - setup: api\n    run:\n      initCommands:\n        - npx ts-node -T src/migrate.ts\n```\n\n" +
		"### 2. Bind to 0.0.0.0\n\nThe porter's app must bind to 0.0.0.0; configure your migrate.ts script accordingly.\n\n" +
		"<!-- #ZEROPS_EXTRACT_END:integration-guide# -->\n"
	vs, err := validateCodebaseIG(context.Background(), "codebase/api/integration-guide", []byte(body), SurfaceInputs{})
	if err != nil {
		t.Fatalf("validateCodebaseIG: %v", err)
	}
	for _, v := range vs {
		if v.Code == "codebase-ig-scaffold-filename" {
			t.Errorf("expected no codebase-ig-scaffold-filename Blocking violation; got %+v", v)
		}
	}
}

// TestRecordFragment_IGScaffoldFilenameOutsideEngineYAML_EmitsNotice —
// agent records IG fragment body with `migrate.ts` in IG #2 prose →
// response carries non-blocking Notice; fragment lands. Mirrors the
// `refinement-replace-reverted` Notice path.
func TestRecordFragment_IGScaffoldFilenameOutsideEngineYAML_EmitsNotice(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	res := dispatch(context.Background(), store, RecipeInput{
		Action: "start", Slug: "synth-showcase", OutputRoot: dir + "/run",
	})
	if !res.OK {
		t.Fatalf("start: %s", res.Error)
	}
	plan := syntheticShowcasePlan()
	res = dispatch(context.Background(), store, RecipeInput{
		Action: "update-plan", Slug: "synth-showcase", Plan: plan,
	})
	if !res.OK {
		t.Fatalf("update-plan: %s", res.Error)
	}

	// IG body that names migrate.ts in PROSE outside any yaml block.
	body := "### 2. Migration entry\n\nThe porter wires `migrate.ts` into the boot path.\n"
	res = dispatch(context.Background(), store, RecipeInput{
		Action:         "record-fragment",
		Slug:           "synth-showcase",
		FragmentID:     "codebase/api/integration-guide",
		Fragment:       body,
		Classification: string(ClassPlatformInvariant),
	})
	if !res.OK {
		t.Fatalf("record-fragment: %s", res.Error)
	}
	if res.BodyBytes == 0 {
		t.Errorf("expected fragment to land (BodyBytes > 0); got %d", res.BodyBytes)
	}
	hasNotice := false
	for _, n := range res.Notices {
		if n.Code == "codebase-ig-scaffold-filename" && n.Severity == SeverityNotice {
			hasNotice = true
			if !strings.Contains(n.Message, "migrate.ts") {
				t.Errorf("Notice message should name the offending filename; got %q", n.Message)
			}
			break
		}
	}
	if !hasNotice {
		t.Errorf("expected codebase-ig-scaffold-filename Notice on out-of-yaml IG body; got %+v", res.Notices)
	}
}

// TestRecordFragment_IGScaffoldFilenameInsideEngineYAML_NoNotice — IG
// fragment body whose only `migrate.ts` mention sits inside a ```yaml
// fenced block (engine-emitted IG #1 shape) → no Notice. Engine-
// stamped yaml content never surfaces as "porter-doesn't-have-this"
// because the engine put it there based on the codebase's actual
// zerops.yaml.
func TestRecordFragment_IGScaffoldFilenameInsideEngineYAML_NoNotice(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	res := dispatch(context.Background(), store, RecipeInput{
		Action: "start", Slug: "synth-showcase", OutputRoot: dir + "/run",
	})
	if !res.OK {
		t.Fatalf("start: %s", res.Error)
	}
	plan := syntheticShowcasePlan()
	res = dispatch(context.Background(), store, RecipeInput{
		Action: "update-plan", Slug: "synth-showcase", Plan: plan,
	})
	if !res.OK {
		t.Fatalf("update-plan: %s", res.Error)
	}

	// IG body whose only migrate.ts mention is inside engine-stamped yaml.
	body := "### 1. Adding `zerops.yaml`\n\n" +
		"```yaml\nzerops:\n  - setup: api\n    run:\n      initCommands:\n        - npx ts-node -T src/migrate.ts\n```\n\n" +
		"Add this block to the root of your project before deploying.\n"
	res = dispatch(context.Background(), store, RecipeInput{
		Action:         "record-fragment",
		Slug:           "synth-showcase",
		FragmentID:     "codebase/api/integration-guide",
		Fragment:       body,
		Classification: string(ClassPlatformInvariant),
	})
	if !res.OK {
		t.Fatalf("record-fragment: %s", res.Error)
	}
	for _, n := range res.Notices {
		if n.Code == "codebase-ig-scaffold-filename" {
			t.Errorf("expected no codebase-ig-scaffold-filename Notice when scaffold filename only appears inside ```yaml block; got %+v", n)
		}
	}
}

// TestSynthesisWorkflowAtom_TeachesIG1IsEngineStamped — atom contains
// the IG #1 do-not-override teaching.
func TestSynthesisWorkflowAtom_TeachesIG1IsEngineStamped(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/codebase-content/synthesis_workflow.md")
	if err != nil {
		t.Fatalf("read synthesis_workflow.md: %v", err)
	}
	for _, want := range []string{
		"## IG #1 is engine-stamped — do NOT override",
		"DO NOT override IG #1",
		"engine-emitted from the\ncodebase's verbatim zerops.yaml",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("synthesis_workflow.md missing IG #1 do-not-override anchor %q", want)
		}
	}
}

// TestSynthesisWorkflowAtom_TeachesIG2NPorterTransferableScope — atom
// contains the porter-transferable scope rule for IG #2-N.
func TestSynthesisWorkflowAtom_TeachesIG2NPorterTransferableScope(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/codebase-content/synthesis_workflow.md")
	if err != nil {
		t.Fatalf("read synthesis_workflow.md: %v", err)
	}
	for _, want := range []string{
		"## IG #2-N covers porter-transferable mechanisms",
		"porter applies to their OWN code",
		"`migrate.ts`",
		"`seed.ts`",
		"`main.ts`",
		"`api.ts`",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("synthesis_workflow.md missing IG #2-N porter-transferable scope anchor %q", want)
		}
	}
}
