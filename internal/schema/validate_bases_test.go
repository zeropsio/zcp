package schema

import (
	"strings"
	"testing"
)

func TestEmbedded_Exported(t *testing.T) {
	if Embedded() == nil {
		t.Fatal("Embedded() returned nil")
	}
}

func TestCheckZeropsBasesLive(t *testing.T) {
	emb := Embedded()

	realBuild := emb.ZeropsYml.BuildBases[0] // a base known to the embedded floor
	realRun := emb.ZeropsYml.RunBases[0]

	yamlWith := func(buildBase, runBase string) string {
		return "zerops:\n  - setup: app\n    build:\n      base: " + buildBase +
			"\n    run:\n      base: " + runBase + "\n"
	}

	t.Run("real bases pass against embedded", func(t *testing.T) {
		if errs := CheckZeropsBasesLive(yamlWith(realBuild, realRun), emb); len(errs) != 0 {
			t.Fatalf("real bases rejected: %v", errs)
		}
	})

	t.Run("hallucinated build base rejected", func(t *testing.T) {
		errs := CheckZeropsBasesLive(yamlWith("zigzag@99", realRun), emb)
		if len(errs) == 0 {
			t.Fatal("expected violation for hallucinated build base")
		}
		if !strings.Contains(errs[0].Error(), "zigzag@99") {
			t.Errorf("violation should name the bad base, got %v", errs[0])
		}
	})

	t.Run("hallucinated run base rejected", func(t *testing.T) {
		if errs := CheckZeropsBasesLive(yamlWith(realBuild, "zigzag@99"), emb); len(errs) == 0 {
			t.Fatal("expected violation for hallucinated run base")
		}
	})

	t.Run("brand-new base accepted when the live schema carries it", func(t *testing.T) {
		// Simulate a fresher live schema that added "bun@999".
		fresh := &Schemas{ZeropsYml: &ZeropsYmlSchema{
			BuildBases: []string{"bun@999"},
			RunBases:   []string{"bun@999"},
		}}
		fresh.ZeropsYml = mustParseFresh(t)
		if errs := CheckZeropsBasesLive(yamlWith("bun@999", "bun@999"), fresh); len(errs) != 0 {
			t.Fatalf("brand-new base in the live schema must pass, got %v", errs)
		}
	})

	t.Run("array build base flags only the bad one", func(t *testing.T) {
		y := "zerops:\n  - setup: app\n    build:\n      base: [" + realBuild + ", zigzag@99]\n    run:\n      base: " + realRun + "\n"
		errs := CheckZeropsBasesLive(y, emb)
		if len(errs) != 1 {
			t.Fatalf("expected exactly 1 violation (the bad array element), got %d: %v", len(errs), errs)
		}
	})

	t.Run("nil schemas → no check", func(t *testing.T) {
		if errs := CheckZeropsBasesLive(yamlWith("zigzag@99", "zigzag@99"), nil); errs != nil {
			t.Errorf("nil schemas must skip the check, got %v", errs)
		}
	})
}

// mustParseFresh builds a *ZeropsYmlSchema whose enum sets include "bun@999" by
// constructing it through the real Parse path so the precomputed sets exist.
func mustParseFresh(t *testing.T) *ZeropsYmlSchema {
	t.Helper()
	raw := `{"properties":{"zerops":{"items":{"properties":{` +
		`"build":{"properties":{"base":{"oneOf":[{"type":"string","enum":["bun@999"]}]}}},` +
		`"run":{"properties":{"base":{"type":"string","enum":["bun@999"]}}}}}}}}`
	s, err := ParseZeropsYmlSchema([]byte(raw))
	if err != nil {
		t.Fatalf("parse fresh schema: %v", err)
	}
	return s
}
