package recipe

import (
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Run-16 §6.5 / §6.7 — slotted IG concatenation, line-anchor zerops.yaml
// comment injection, single-slot CLAUDE.md stitch.

func TestMergeSlottedIGFragments_ConcatenatesNumeric(t *testing.T) {
	t.Parallel()
	input := map[string]string{
		"codebase/api/integration-guide/3":   "### 3. Drain on SIGTERM\nbody",
		"codebase/api/integration-guide/2":   "### 2. Trust the L7\nbody",
		"codebase/api/integration-guide/4":   "### 4. Read managed-service credentials\nbody",
		"codebase/api/integration-guide":     "(legacy fragment, should be overwritten)",
		"codebase/other/integration-guide/2": "### 2. Other codebase",
	}
	out := mergeSlottedIGFragments(input, "api")

	merged := out["codebase/api/integration-guide"]
	if !strings.Contains(merged, "### 2. Trust the L7") {
		t.Error("merged IG missing slot 2")
	}
	if !strings.Contains(merged, "### 3. Drain") {
		t.Error("merged IG missing slot 3")
	}
	if !strings.Contains(merged, "### 4. Read") {
		t.Error("merged IG missing slot 4")
	}
	// Numeric ordering: slot 2 must appear before slot 3.
	idx2 := strings.Index(merged, "### 2.")
	idx3 := strings.Index(merged, "### 3.")
	idx4 := strings.Index(merged, "### 4.")
	if idx2 >= idx3 || idx3 >= idx4 {
		t.Errorf("slots not in numeric order: 2@%d, 3@%d, 4@%d", idx2, idx3, idx4)
	}
	// Other codebase's slot must NOT be in api's merged output.
	if strings.Contains(merged, "Other codebase") {
		t.Error("merged IG leaked another codebase's slot")
	}
}

func TestMergeSlottedIGFragments_FallsBackToLegacy(t *testing.T) {
	t.Parallel()
	// No slotted entries → legacy stays.
	input := map[string]string{
		"codebase/api/integration-guide": "### 2. Legacy single-fragment",
	}
	out := mergeSlottedIGFragments(input, "api")
	if out["codebase/api/integration-guide"] != "### 2. Legacy single-fragment" {
		t.Errorf("legacy IG should be preserved when no slots present, got %q", out["codebase/api/integration-guide"])
	}
}

func TestInjectZeropsYamlComments_LineAnchor_BlockBoundaryDetection(t *testing.T) {
	t.Parallel()
	yamlBody := `zerops:
  - setup: api
    build:
      base: nodejs@22
    run:
      base: nodejs@22
      envVariables:
        NODE_ENV: production
`
	fragments := map[string]string{
		"codebase/api/zerops-yaml-comments/envVariables": "Why these env vars: framework needs NODE_ENV=production.",
	}
	out := injectZeropsYamlComments(yamlBody, fragments, "api")

	// Comment must appear directly above the envVariables: line.
	idxComment := strings.Index(out, "# Why these env vars")
	idxKey := strings.Index(out, "envVariables:")
	if idxComment < 0 {
		t.Fatal("comment not injected")
	}
	if idxComment > idxKey {
		t.Errorf("comment at %d should precede `envVariables:` at %d", idxComment, idxKey)
	}
}

func TestInjectZeropsYamlComments_LineAnchor_PreservesAllOriginalLines(t *testing.T) {
	t.Parallel()
	yamlBody := `zerops:
  - setup: api
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      initCommands:
        - command: zsc execOnce migrate-key npm run migrate
`
	fragments := map[string]string{
		"codebase/api/zerops-yaml-comments/initCommands": "Two execOnce keys keep migrate + seed independent.",
	}
	out := injectZeropsYamlComments(yamlBody, fragments, "api")

	// Every original key/value line still present.
	for _, want := range []string{
		"setup: api", "base: nodejs@22", "port: 3000", "httpSupport: true",
		"command: zsc execOnce migrate-key",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("post-injection yaml missing line %q", want)
		}
	}
}

func TestInjectZeropsYamlComments_LineAnchor_DroppedWhenBlockMissing(t *testing.T) {
	t.Parallel()
	yamlBody := `zerops:
  - setup: api
    run:
      base: nodejs@22
`
	fragments := map[string]string{
		"codebase/api/zerops-yaml-comments/nonexistent": "Comment that won't anchor.",
	}
	out := injectZeropsYamlComments(yamlBody, fragments, "api")
	if strings.Contains(out, "Comment that won't anchor") {
		t.Error("comment for missing block should be dropped silently")
	}
}

func TestInjectZeropsYamlComments_CorpusYamlValidity(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		yamlBody string
		comments map[string]string
	}{
		{
			name: "showcase-shape",
			yamlBody: `zerops:
  - setup: apidev
    build:
      base: nodejs@22
      buildCommands:
        - npm install
        - npm run build
    deploy:
      readiness:
        path: /healthz
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      envVariables:
        NODE_ENV: production
        S3_REGION: us-east-1
      initCommands:
        - command: zsc execOnce migrate npm run migrate
        - command: zsc execOnce seed npm run seed
      start: node dist/main.js
`,
			comments: map[string]string{
				"codebase/apidev/zerops-yaml-comments/envVariables": "S3_REGION=us-east-1: only region MinIO accepts.",
				"codebase/apidev/zerops-yaml-comments/initCommands": "Two execOnce keys decouple migrate + seed.",
			},
		},
		{
			name: "minimal-shape",
			yamlBody: `zerops:
  - setup: app
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      start: node main.js
`,
			comments: map[string]string{
				"codebase/app/zerops-yaml-comments/ports": "Single HTTP-supporting port.",
			},
		},
		{
			name: "stage-pair-shape",
			yamlBody: `zerops:
  - setup: appdev
    run:
      base: nodejs@22
      start: zsc noop --silent
  - setup: appstage
    run:
      base: nodejs@22
      start: node dist/main.js
`,
			comments: map[string]string{
				"codebase/appdev/zerops-yaml-comments/run": "Dev runs zsc noop; agent owns process.",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			hostname := strings.SplitN(strings.TrimPrefix(firstFragKey(tc.comments), "codebase/"), "/", 2)[0]
			out := injectZeropsYamlComments(tc.yamlBody, tc.comments, hostname)

			// Post-injection yaml must still parse cleanly.
			var parsed any
			if err := yaml.Unmarshal([]byte(out), &parsed); err != nil {
				t.Fatalf("post-injection yaml does not parse: %v\n---\n%s", err, out)
			}

			// Original yaml must also parse (sanity).
			var orig any
			if err := yaml.Unmarshal([]byte(tc.yamlBody), &orig); err != nil {
				t.Fatalf("original yaml does not parse (test fixture broken): %v", err)
			}
		})
	}
}

func firstFragKey(m map[string]string) string {
	for k := range m {
		return k
	}
	return ""
}

// Run-16 reviewer D-6 — the legacy `claude-md/{service-facts,notes}`
// sub-slot back-compat synthesizer was dropped because its synthesized
// body opened with the very `## Zerops service facts` heading that the
// run-16 slot-shape refusal + finalize validator both reject. Recipes
// still on the legacy form fail loudly at stitch with a "missing
// fragment codebase/<h>/claude-md" error — that's the migration signal.

func TestAssembleClaudeMD_LegacySubslotsOnly_FailsWithMissingFragment(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug:      "synth",
		Framework: "nest",
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}},
		Fragments: map[string]string{
			"codebase/api/claude-md/service-facts": "Port 3000",
			"codebase/api/claude-md/notes":         "Dev loop",
		},
	}
	_, missing, err := AssembleCodebaseClaudeMD(plan, "api")
	if err != nil {
		t.Fatalf("AssembleCodebaseClaudeMD should not error on missing single-slot (returns it as missing): %v", err)
	}
	if !slices.Contains(missing, "codebase/api/claude-md") {
		t.Errorf("expected `codebase/api/claude-md` in missing list when only legacy sub-slots present; got missing=%v", missing)
	}
}

func TestAssembleClaudeMD_SingleSlot_StitchesCleanly(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug:      "synth",
		Framework: "nest",
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}},
		Fragments: map[string]string{
			"codebase/api/claude-md": "# api\n\nNestJS REST API.\n\n## Build & run\n\n- npm test\n\n## Architecture\n\n- src/main.ts",
		},
	}
	body, missing, err := AssembleCodebaseClaudeMD(plan, "api")
	if err != nil {
		t.Fatalf("AssembleCodebaseClaudeMD: %v", err)
	}
	if len(missing) != 0 {
		t.Errorf("single-slot fragment present → no missing; got %v", missing)
	}
	if !strings.Contains(body, "# api") {
		t.Error("single-slot fragment body should be substituted into CLAUDE.md")
	}
}
