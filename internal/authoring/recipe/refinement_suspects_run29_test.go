package recipe

import (
	"strings"
	"testing"
)

// Run-29 Fix #4 — refinement-suspects extension for IG ↔ yaml-comment
// same-mechanism duplication (cross-surface duplication
// defense-in-depth; class name retired the legacy `criterion-6-` prefix
// alongside the rubric retirement in run-34 Fix A). The authoring-order
// teaching in synthesis_workflow.md is the primary fix; this predicate
// flags suspects when both surfaces teach the same canonical mechanism
// with overlapping prose context.

// TestCollectRefinementSuspects_IGYAMLCommentDup_Detected — fixture
// with same-mechanism teaching on IG #5 + zerops.yaml comment for the
// same codebase → suspect emitted with code ig-yamlcomment-dup.
func TestCollectRefinementSuspects_IGYAMLCommentDup_Detected(t *testing.T) {
	t.Parallel()
	plan := syntheticShowcasePlan()
	plan.Fragments = map[string]string{
		"codebase/api/integration-guide": "" +
			"### 1. Adding `zerops.yaml`\n\n" +
			"```yaml\nzerops:\n  - setup: api\n```\n\n" +
			"### 5. Own-key aliases for cross-service vars\n\n" +
			"Cross-service vars reach the app only via an alias in " +
			"`run.envVariables`. Declaring `DB_HOST: ${db_hostname}` " +
			"under your own key lets the app read `DB_HOST` directly " +
			"and keeps swapping the managed service a yaml-only edit.\n",
		"codebase/api/zerops-yaml": "" +
			"zerops:\n" +
			"  - setup: api\n" +
			"    envVariables:\n" +
			"      # DB_HOST aliases ${db_hostname} so app code reads its own\n" +
			"      # constant. Cross-service vars reach the app only via an\n" +
			"      # alias in run.envVariables; swapping the managed service\n" +
			"      # later is a yaml-only edit.\n" +
			"      DB_HOST: ${db_hostname}\n",
	}
	suspects := CollectRefinementSuspects(plan, nil)
	found := false
	for _, s := range suspects {
		if s.Class == "ig-yamlcomment-dup" && s.FragmentID == "codebase/api/integration-guide" {
			found = true
			if !strings.Contains(s.Reason, "${db_hostname}") {
				t.Errorf("expected suspect Reason to name the anchor; got %q", s.Reason)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected ig-yamlcomment-dup suspect on codebase/api; got %+v", suspects)
	}
}

// TestCollectRefinementSuspects_IGYAMLCommentDifferent_NoSuspect — IG
// teaches mechanism A, yaml teaches mechanism B → no suspect.
func TestCollectRefinementSuspects_IGYAMLCommentDifferent_NoSuspect(t *testing.T) {
	t.Parallel()
	plan := syntheticShowcasePlan()
	plan.Fragments = map[string]string{
		"codebase/api/integration-guide": "" +
			"### 1. Adding `zerops.yaml`\n\n" +
			"```yaml\nzerops:\n  - setup: api\n```\n\n" +
			"### 2. Bind to 0.0.0.0\n\nThe porter's app must bind to 0.0.0.0.\n",
		"codebase/api/zerops-yaml": "" +
			"zerops:\n" +
			"  - setup: api\n" +
			"    envVariables:\n" +
			"      # forcePathStyle is required for object-storage compatibility\n" +
			"      # in the s3 client; the platform's S3-compatible API requires it.\n" +
			"      AWS_S3_FORCE_PATH_STYLE: true\n",
	}
	suspects := CollectRefinementSuspects(plan, nil)
	for _, s := range suspects {
		if s.Class == "ig-yamlcomment-dup" {
			t.Errorf("expected no ig-yamlcomment-dup suspect when IG and yaml teach different mechanisms; got %+v", s)
		}
	}
}

// TestCollectRefinementSuspects_IGEngineYamlBlockOnly_NoSuspect — IG #1
// engine-stamped yaml legitimately contains anchor tokens (e.g. yaml
// references the same managed service variables); the predicate must
// strip the first ```yaml block before scanning so engine-emit alone
// never trips the suspect.
func TestCollectRefinementSuspects_IGEngineYamlBlockOnly_NoSuspect(t *testing.T) {
	t.Parallel()
	plan := syntheticShowcasePlan()
	plan.Fragments = map[string]string{
		"codebase/api/integration-guide": "" +
			"### 1. Adding `zerops.yaml`\n\n" +
			"```yaml\n" +
			"zerops:\n" +
			"  - setup: api\n" +
			"    envVariables:\n" +
			"      DB_HOST: ${db_hostname}\n" +
			"```\n\n" +
			"### 2. Bind to 0.0.0.0\n\nNothing here mentions the env tokens.\n",
		"codebase/api/zerops-yaml": "" +
			"zerops:\n" +
			"  - setup: api\n" +
			"    envVariables:\n" +
			"      # `DB_HOST` is the own-key alias.\n" +
			"      DB_HOST: ${db_hostname}\n",
	}
	suspects := CollectRefinementSuspects(plan, nil)
	for _, s := range suspects {
		if s.Class == "ig-yamlcomment-dup" {
			t.Errorf("expected no ig-yamlcomment-dup suspect when IG anchor only appears inside engine-stamped yaml block; got %+v", s)
		}
	}
}
