package recipe

import (
	"os"
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

// Run-21 §1 — setup-scoped block matching closes the dual-setup
// leaf-collision bug. With a multi-setup yaml, fragments named
// `<setup>.<path>.<leaf>` must anchor INSIDE the named setup's range,
// not on the file's first occurrence of `<leaf>:`. Pre-fix, both
// `dev.run.envVariables` and `prod.run.envVariables` collapsed onto
// the first `envVariables:` line (always the dev block), leaving the
// prod block bare.
func TestInjectZeropsYamlComments_SetupScoped_AnchorsInsideNamedSetup(t *testing.T) {
	t.Parallel()
	yamlBody := `zerops:
  - setup: dev
    run:
      envVariables:
        NODE_ENV: development
      start: zsc noop --silent
  - setup: prod
    run:
      envVariables:
        NODE_ENV: production
      start: node dist/main.js
`
	fragments := map[string]string{
		"codebase/api/zerops-yaml-comments/dev.run.envVariables":  "Dev env-vars: NODE_ENV=development gates plugins.",
		"codebase/api/zerops-yaml-comments/prod.run.envVariables": "Prod env-vars: NODE_ENV=production strips dev tooling.",
		"codebase/api/zerops-yaml-comments/dev.run.start":         "Dev start: zsc noop hands the process to zerops_dev_server.",
		"codebase/api/zerops-yaml-comments/prod.run.start":        "Prod start: platform supervises the compiled entry.",
	}
	out := injectZeropsYamlComments(yamlBody, fragments, "api")

	devSetup := strings.Index(out, "- setup: dev")
	prodSetup := strings.Index(out, "- setup: prod")
	if devSetup < 0 || prodSetup < 0 {
		t.Fatalf("setup boundaries missing in output:\n%s", out)
	}

	devSlice := out[devSetup:prodSetup]
	prodSlice := out[prodSetup:]

	if !strings.Contains(devSlice, "NODE_ENV=development gates plugins") {
		t.Errorf("dev env-vars comment must land in dev setup range; got dev:\n%s", devSlice)
	}
	if strings.Contains(devSlice, "NODE_ENV=production strips dev tooling") {
		t.Errorf("prod env-vars comment leaked into dev setup range:\n%s", devSlice)
	}
	if !strings.Contains(prodSlice, "NODE_ENV=production strips dev tooling") {
		t.Errorf("prod env-vars comment must land in prod setup range; got prod:\n%s", prodSlice)
	}
	if !strings.Contains(devSlice, "zsc noop hands the process") {
		t.Error("dev start comment must land in dev setup")
	}
	if !strings.Contains(prodSlice, "platform supervises the compiled entry") {
		t.Error("prod start comment must land in prod setup")
	}
}

// Run-21 §1 — back-compat: a block name without a setup prefix still
// matches first-occurrence (single-setup yaml, or pre-run-21 fragments
// already in the wild). The setup-scope branch only fires when the
// first dot-segment matches a `- setup: <name>` line.
func TestInjectZeropsYamlComments_NoSetupPrefix_FirstMatchPreserved(t *testing.T) {
	t.Parallel()
	yamlBody := `zerops:
  - setup: app
    run:
      envVariables:
        FOO: bar
`
	fragments := map[string]string{
		"codebase/api/zerops-yaml-comments/run.envVariables": "Run-time env-vars are own-key aliases.",
	}
	out := injectZeropsYamlComments(yamlBody, fragments, "api")
	if !strings.Contains(out, "# Run-time env-vars are own-key aliases.") {
		t.Errorf("non-setup-prefixed block should still anchor on first match; got:\n%s", out)
	}
}

// Run-21 §4 — engine strips a leading `# ` (or bare `#`) from each
// fragment line before re-prefixing, so agents that pre-hash their
// fragment bodies (per yaml-comment-style atom) don't produce double-
// hash artifacts (`# #`) in the rendered yaml. Bare `#` paragraph
// separators emit as bare `#` (no trailing space) to match the
// laravel-showcase golden shape.
func TestInjectZeropsYamlComments_PrehashedBody_NormalizedToSingleHash(t *testing.T) {
	t.Parallel()
	yamlBody := `zerops:
  - setup: app
    run:
      envVariables:
        FOO: bar
`
	fragments := map[string]string{
		"codebase/api/zerops-yaml-comments/envVariables": "# Cross-service refs re-aliased under stable own-keys.\n#\n# Replace S3_REGION with whatever your library expects.",
	}
	out := injectZeropsYamlComments(yamlBody, fragments, "api")

	if strings.Contains(out, "# # Cross-service") {
		t.Errorf("double-hash artifact present in output (engine should strip leading `# `):\n%s", out)
	}
	if !strings.Contains(out, "# Cross-service refs re-aliased under stable own-keys.") {
		t.Errorf("normalized comment line missing:\n%s", out)
	}
	// Paragraph separator: bare `#` (no `# #`, no trailing space).
	lines := strings.Split(out, "\n")
	hasBareHash := false
	for _, ln := range lines {
		trimmed := strings.TrimLeft(ln, " ")
		if trimmed == "#" {
			hasBareHash = true
			break
		}
	}
	if !hasBareHash {
		t.Errorf("bare `#` paragraph separator missing in output:\n%s", out)
	}
	if strings.Contains(out, "# # Replace") || strings.Contains(out, "# #\n") {
		t.Errorf("double-hash artifact present (`# #`):\n%s", out)
	}
}

// Run-21 §4 — agents that DON'T pre-hash (write raw prose as the
// fragment body) still get correct output: the engine adds `# ` to
// each line, no double-hash, no missing prefix.
func TestInjectZeropsYamlComments_RawProseBody_PrefixedOnce(t *testing.T) {
	t.Parallel()
	yamlBody := `zerops:
  - setup: app
    run:
      envVariables:
        FOO: bar
`
	fragments := map[string]string{
		"codebase/api/zerops-yaml-comments/envVariables": "Two setups, two build shapes.\nDev ships source unfiltered.",
	}
	out := injectZeropsYamlComments(yamlBody, fragments, "api")
	if !strings.Contains(out, "# Two setups, two build shapes.") {
		t.Errorf("raw prose line should get single `# ` prefix:\n%s", out)
	}
	if !strings.Contains(out, "# Dev ships source unfiltered.") {
		t.Error("raw prose continuation line must also get `# ` prefix")
	}
	if strings.Contains(out, "# # Two setups") {
		t.Errorf("raw prose must not gain a double-hash:\n%s", out)
	}
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

// TestAssembleCodebaseREADME_RemovesDuplicateYAMLComments — Run-20 E1.
// AssembleCodebaseREADME reads the on-disk zerops.yaml when SourceRoot
// is set; after the first stitch round writes engine `# #`-prefixed
// comment blocks above directives, subsequent rounds must NOT
// re-inject duplicate blocks above the same directive. The fix is a
// strip-then-inject in AssembleCodebaseREADME mirroring
// WriteCodebaseYAMLWithComments. Without it, the IG #1 inline yaml
// shows the same block twice (run-19 §1).
func TestAssembleCodebaseREADME_RemovesDuplicateYAMLComments(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sourceRoot := dir + "/apidev"
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Pre-existing on-disk yaml with a prior engine-injected comment
	// block above `envVariables:` — exactly the shape the assembler
	// would re-encounter on a second stitchCodebases round.
	yamlBody := `zerops:
  - setup: api
    run:
      base: nodejs@22
      # NODE_ENV=production: framework needs production-mode at start.
      envVariables:
        NODE_ENV: production
`
	if err := os.WriteFile(sourceRoot+"/zerops.yaml", []byte(yamlBody), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	plan := &Plan{
		Slug:      "synth",
		Framework: "nest",
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22", SourceRoot: sourceRoot}},
		Fragments: map[string]string{
			"codebase/api/intro":             "Synthetic API.",
			"codebase/api/integration-guide": "### 2. Trust the L7\nbody",
			"codebase/api/knowledge-base":    "- **No `.env` file** — Zerops injects vars.",
			"codebase/api/claude-md":         "# api\n",
			// Recorded fragment matches the prior comment block on disk.
			"codebase/api/zerops-yaml-comments/envVariables": "NODE_ENV=production: framework needs production-mode at start.",
		},
	}

	body, _, err := AssembleCodebaseREADME(plan, "api")
	if err != nil {
		t.Fatalf("AssembleCodebaseREADME: %v", err)
	}
	// The exact comment line must appear EXACTLY once in IG #1.
	wantLine := "# NODE_ENV=production: framework needs production-mode at start."
	count := strings.Count(body, wantLine)
	if count != 1 {
		t.Errorf("comment line %q appears %d times in IG #1; want exactly 1\n---\n%s",
			wantLine, count, body)
	}
}
