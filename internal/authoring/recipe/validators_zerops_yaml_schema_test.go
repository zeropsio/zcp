package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/schema"
)

// TestGateZeropsYamlSchema_LiveBaseFreshness pins variant (b): the gate checks
// build/run base existence against the LIVE schema snapshot in GateContext, so
// (1) a hallucinated base still blocks (via the embedded floor when no live
// snapshot is attached), and (2) a brand-new platform base that the live
// snapshot carries is NOT false-rejected. Structure violations are unaffected.
func TestGateZeropsYamlSchema_LiveBaseFreshness(t *testing.T) {
	t.Parallel()

	writeYAML := func(base string) string {
		dir := t.TempDir()
		body := "zerops:\n  - setup: api\n    build:\n      base: " + base +
			"\n      buildCommands: [\"npm run build\"]\n      deployFiles: .\n" +
			"    run:\n      base: " + base + "\n      start: node dist/main.js\n"
		if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte(body), 0o600); err != nil {
			t.Fatalf("write yaml: %v", err)
		}
		return dir
	}
	planFor := func(dir string) *Plan {
		return &Plan{Slug: "t", Codebases: []Codebase{{Hostname: "api", SourceRoot: dir}}}
	}

	// (1) Hallucinated base blocks even with no live snapshot (embedded floor).
	if vs := gateZeropsYamlSchema(GateContext{Plan: planFor(writeYAML("zigzag@99"))}); len(vs) == 0 {
		t.Error("hallucinated base must block (embedded-floor base check)")
	}

	// (2) A brand-new base the LIVE snapshot carries must pass.
	fresh := freshSchemasWithBase(t, "bun@999")
	if vs := gateZeropsYamlSchema(GateContext{Plan: planFor(writeYAML("bun@999")), Schemas: fresh}); len(vs) != 0 {
		t.Errorf("brand-new base in the live snapshot must pass, got: %+v", vs)
	}

	// (3) That same brand-new base, WITHOUT the live snapshot, is rejected by
	// the stale embedded floor — proving (2) is the live snapshot's doing.
	if vs := gateZeropsYamlSchema(GateContext{Plan: planFor(writeYAML("bun@999"))}); len(vs) == 0 {
		t.Error("expected the embedded floor to reject a base it does not yet know")
	}
}

func freshSchemasWithBase(t *testing.T, base string) *schema.Schemas {
	t.Helper()
	raw := `{"properties":{"zerops":{"items":{"properties":{` +
		`"build":{"properties":{"base":{"oneOf":[{"type":"string","enum":["` + base + `"]}]}}},` +
		`"run":{"properties":{"base":{"type":"string","enum":["` + base + `"]}}}}}}}}`
	zs, err := schema.ParseZeropsYmlSchema([]byte(raw))
	if err != nil {
		t.Fatalf("parse fresh schema: %v", err)
	}
	return &schema.Schemas{ZeropsYml: zs}
}

// TestGateZeropsYamlSchema_RejectsUnknownRunField — Run-21-prep §RC1.
// The live zerops-yml-json-schema.json closes `run.properties` with
// `additionalProperties: false`; the embedded schema in
// internal/schema/testdata mirrors the live shape. A yaml that emits
// `run.verticalAutoscaling:` (a valid import.yaml service-level field
// but NOT a zerops.yaml run-level field) must produce a blocking
// violation. Real symptom: sim 21-input-1 emitted the field at
// apidev/zerops.yaml:190-192 + workerdev/zerops.yaml:130-132 and the
// finalize gate set never noticed.
func TestGateZeropsYamlSchema_RejectsUnknownRunField(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "zerops.yaml")
	if err := os.WriteFile(yamlPath, []byte(`zerops:
  - setup: api
    build:
      base: nodejs@22
      buildCommands:
        - npm run build
      deployFiles: .
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      start: node dist/main.js
      verticalAutoscaling:
        cpuMode: DEDICATED
`), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	plan := &Plan{
		Slug: "test",
		Codebases: []Codebase{
			{Hostname: "api", SourceRoot: dir},
		},
	}

	vs := gateZeropsYamlSchema(GateContext{Plan: plan})
	if len(vs) == 0 {
		t.Fatal("expected violation for verticalAutoscaling under run:; got none")
	}
	var found bool
	for _, v := range vs {
		if v.Code != "zerops-yaml-schema-violation" {
			continue
		}
		if v.Severity != SeverityBlocking {
			t.Errorf("schema violation should be blocking; got %v", v.Severity)
		}
		if !strings.Contains(strings.ToLower(v.Message), "verticalautoscaling") &&
			!strings.Contains(v.Message, "additionalProperties") {
			t.Errorf("violation message should name the offending field; got %q", v.Message)
		}
		found = true
	}
	if !found {
		t.Fatalf("expected zerops-yaml-schema-violation; got %+v", vs)
	}
}

// TestGateZeropsYamlSchema_AcceptsValidYaml — sanity: a well-formed
// minimal yaml produces no schema violations. Pinned alongside the
// negative case so a future schema bump that breaks the validator path
// surfaces here, not as a phantom blocking gate in production.
func TestGateZeropsYamlSchema_AcceptsValidYaml(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "zerops.yaml")
	if err := os.WriteFile(yamlPath, []byte(`zerops:
  - setup: api
    build:
      base: nodejs@22
      buildCommands:
        - npm run build
      deployFiles: .
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      start: node dist/main.js
`), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	plan := &Plan{
		Slug: "test",
		Codebases: []Codebase{
			{Hostname: "api", SourceRoot: dir},
		},
	}

	vs := gateZeropsYamlSchema(GateContext{Plan: plan})
	for _, v := range vs {
		if v.Code == "zerops-yaml-schema-violation" {
			t.Errorf("unexpected schema violation on valid yaml: %+v", v)
		}
	}
}

// TestGateZeropsYamlSchema_FiresAtScaffold — Run-21-prep RC2. The
// schema gate is registered in CodebaseScaffoldGates so a scaffold-
// authored yaml with a schema-invalid field (e.g. `verticalAutoscaling`
// under `run:`) is refused at scaffold complete-phase, not deferred to
// codebase-content / finalize. Catches the producer in its same-context
// window. Pinning the gate set's membership keeps it from drifting out.
func TestGateZeropsYamlSchema_FiresAtScaffold(t *testing.T) {
	t.Parallel()
	var found bool
	for _, g := range CodebaseScaffoldGates() {
		if g.Name == "zerops-yaml-schema" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("zerops-yaml-schema gate must be in CodebaseScaffoldGates so scaffold catches schema violations at the producer")
	}
}

// TestGateZeropsYamlSchema_NoSourceRoot_Skips — codebases without an
// on-disk SourceRoot (chain-parent, pre-scaffold) must not error; the
// gate silently skips them.
func TestGateZeropsYamlSchema_NoSourceRoot_Skips(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug: "test",
		Codebases: []Codebase{
			{Hostname: "api", SourceRoot: ""},
		},
	}
	vs := gateZeropsYamlSchema(GateContext{Plan: plan})
	if len(vs) != 0 {
		t.Fatalf("expected no violations for empty SourceRoot; got %+v", vs)
	}
}
