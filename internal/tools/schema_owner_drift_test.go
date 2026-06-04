package tools

import (
	"os"
	"reflect"
	"regexp"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestToolSchemaEnumsPinnedToOwners is the P6 tell==check tripwire for
// tool-schema enum prose. Each closed-vocabulary value that a Go owner
// defines (topology enums, the validXxx tables, the ops fact taxonomy, the
// subdomain-eligibility predicate) MUST be surfaced verbatim in the schema
// text the agent reads. If an owner gains a value and the schema is not
// updated, this test fails — the new platform value cannot silently go
// unsurfaced. (Forward direction only: it does not flag a stale value the
// owner dropped; that is a lower-risk lingering-prose case.)
//
// This pins the prose to its owner WITHOUT changing schema generation —
// struct tags cannot interpolate constants, so the values stay hand-authored
// and this lint guarantees they cannot drift. Single owner per concept.
func TestToolSchemaEnumsPinnedToOwners(t *testing.T) {
	tag := func(v any, field string) string {
		f, ok := reflect.TypeOf(v).FieldByName(field)
		if !ok {
			t.Fatalf("field %s not found on %T", field, v)
		}
		return f.Tag.Get("jsonschema")
	}
	keys := func(m map[topology.CloseDeployMode]bool) []string {
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, string(k))
		}
		return out
	}
	biKeys := func(m map[topology.BuildIntegration]bool) []string {
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, string(k))
		}
		return out
	}
	cases := []struct {
		name   string
		schema string
		values []string
	}{
		{"closeMode → validCloseModes", tag(WorkflowInput{}, "CloseModes"), keys(validCloseModes)},
		{"buildIntegration → validBuildIntegrations", tag(WorkflowInput{}, "Integration"), biKeys(validBuildIntegrations)},
		{"bootstrapMode → workflow.ValidBootstrapModes", tag(WorkflowInput{}, "Plan"), workflow.ValidBootstrapModes()},
		{"envClassification → topology.SecretClassificationValues", tag(WorkflowInput{}, "EnvClassifications"), topology.SecretClassificationValues()},
		{"factType (workflow) → ops.KnownFactTypes", tag(WorkflowInput{}, "FactType"), ops.KnownFactTypes()},
		{"factType (record_fact) → ops.KnownFactTypes", tag(RecordFactInput{}, "Type"), ops.KnownFactTypes()},
		{"factScope → ops.KnownFactScopes", tag(RecordFactInput{}, "Scope"), ops.KnownFactScopes()},
		{"factRouteTo → ops.KnownFactRouteTos", tag(RecordFactInput{}, "RouteTo"), ops.KnownFactRouteTos()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if len(tc.values) == 0 {
				t.Fatalf("%s: owner value set is empty — bad test wiring", tc.name)
			}
			for _, v := range tc.values {
				if !mentionsToken(tc.schema, v) {
					t.Errorf("%s: owner value %q is not surfaced in the tool-schema prose.\n"+
						"An owner gained a value the schema didn't reflect — update the schema text so the agent sees it (single-owner tell==check).",
						tc.name, v)
				}
			}
		})
	}
}

// TestSubdomainSchemaModesPinnedToPredicate pins the subdomain tool's
// eligible-mode prose to modeAllowsSubdomain (the real owner of the
// auto-enable decision). The mode list lives in the tool Description string in
// subdomain.go, so the test reads that source and asserts every predicate-true
// mode appears in it.
func TestSubdomainSchemaModesPinnedToPredicate(t *testing.T) {
	src, err := os.ReadFile("subdomain.go")
	if err != nil {
		t.Fatalf("read subdomain.go: %v", err)
	}
	allModes := []topology.Mode{
		topology.ModeDev, topology.ModeStandard, topology.ModeStage,
		topology.ModeSimple, topology.ModeLocalStage, topology.ModeLocalOnly,
	}
	for _, m := range allModes {
		if !modeAllowsSubdomain(m) {
			continue
		}
		if !mentionsToken(string(src), string(m)) {
			t.Errorf("subdomain.go tool description omits mode %q which modeAllowsSubdomain accepts — "+
				"the eligible-mode prose drifted from the predicate (single-owner tell==check).", m)
		}
	}
}

// TestSchemaOwnerDriftHasTeeth proves the tripwire is not vacuous: an absent
// value is reported absent, a present one present, and an embedded-substring
// is not a false positive.
func TestSchemaOwnerDriftHasTeeth(t *testing.T) {
	f, _ := reflect.TypeFor[WorkflowInput]().FieldByName("CloseModes")
	schema := f.Tag.Get("jsonschema")
	if mentionsToken(schema, "teleport-mode") {
		t.Error("teeth: false positive — reported an absent owner value as surfaced")
	}
	if !mentionsToken(schema, "git-push") {
		t.Error("teeth: false negative — missed a value that IS in the schema")
	}
	if mentionsToken("autobahn manualism", "auto") || mentionsToken("autobahn manualism", "manual") {
		t.Error("teeth: matched a token embedded inside a larger word")
	}
}

// mentionsToken reports whether tok appears in s as a standalone token
// (bounded by anything other than [a-z0-9_-]), so "none" matches inside
// "'none'" or "auto/none" but not inside "noneSuch".
func mentionsToken(s, tok string) bool {
	re := regexp.MustCompile(`(?i)(^|[^a-z0-9_-])` + regexp.QuoteMeta(tok) + `([^a-z0-9_-]|$)`)
	return re.MatchString(s)
}
