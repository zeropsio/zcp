package recipe

import (
	"context"
	"strings"
	"testing"
)

// TestValidator_RootREADME_FactualityMismatch — run-8-readiness §2.D:
// any framework name the root README claims must appear in at least
// one codebase manifest. Root asserts "NestJS 11" while every
// codebase manifest lists Svelte → fail.
func TestValidator_RootREADME_FactualityMismatch(t *testing.T) {
	t.Parallel()

	body := []byte(`# synth-showcase

<!-- #ZEROPS_EXTRACT_START:intro# -->
A NestJS application connected to PostgreSQL, running on Zerops.
<!-- #ZEROPS_EXTRACT_END:intro# -->
` + fakeDeployButtons() + `
- **AI Agent** [[info]](/0)
- **Remote (CDE)** [[info]](/1)
- **Local** [[info]](/2)
- **Stage** [[info]](/3)
- **Small Production** [[info]](/4)
- **Highly-available Production** [[info]](/5)
`)
	inputs := SurfaceInputs{Plan: &Plan{
		Framework: "svelte",
		Codebases: []Codebase{{Hostname: "app", Role: RoleFrontend}},
	}}
	// Manifest probe: the plan's Framework is svelte; body names "NestJS".
	vs, err := validateRootREADME(context.Background(), "README.md", body, inputs)
	if err != nil {
		t.Fatalf("validateRootREADME: %v", err)
	}
	if !containsCode(vs, "factuality-mismatch") {
		t.Errorf("expected factuality-mismatch violation, got %+v", vs)
	}
}

// TestValidator_EnvREADME_MetaAgentVoice — §2.D: env README is porter-
// facing; it MUST NOT narrate in meta-agent voice. "agent mounts SSHFS"
// is meta-voice; fails.
func TestValidator_EnvREADME_MetaAgentVoice(t *testing.T) {
	t.Parallel()

	body := []byte(`# Stage

<!-- #ZEROPS_EXTRACT_START:intro# -->
This tier is where the agent mounts SSHFS to iterate on deploys.
Promote from this tier when you outgrow single-replica.
<!-- #ZEROPS_EXTRACT_END:intro# -->
` + padEnvREADME() + `
`)
	inputs := SurfaceInputs{Plan: &Plan{Framework: "svelte"}}
	vs, err := validateEnvREADME(context.Background(), "3 — Stage/README.md", body, inputs)
	if err != nil {
		t.Fatalf("validateEnvREADME: %v", err)
	}
	if !containsCode(vs, "meta-agent-voice") {
		t.Errorf("expected meta-agent-voice violation, got %+v", vs)
	}
}

// TestValidator_EnvREADME_TierPromotionVerb — §2.D: env README must
// carry tier promotion vocabulary so the porter knows when to move
// up.
func TestValidator_EnvREADME_TierPromotionVerb(t *testing.T) {
	t.Parallel()

	body := []byte(`# Stage

<!-- #ZEROPS_EXTRACT_START:intro# -->
This tier runs your app in non-HA mode.
<!-- #ZEROPS_EXTRACT_END:intro# -->
` + padEnvREADME() + `
`)
	vs, err := validateEnvREADME(context.Background(), "3 — Stage/README.md", body, SurfaceInputs{Plan: &Plan{Framework: "svelte"}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !containsCode(vs, "tier-promotion-verb-missing") {
		t.Errorf("expected tier-promotion-verb-missing, got %+v", vs)
	}
}

// TestValidator_ImportComments_TemplatedOpening — §2.D: the first
// sentence of each runtime-service block's comment must differ from
// the others. All three same-opening → fail.
func TestValidator_ImportComments_TemplatedOpening(t *testing.T) {
	t.Parallel()

	plan := &Plan{
		Codebases: []Codebase{
			{Hostname: "api", Role: RoleAPI},
			{Hostname: "app", Role: RoleFrontend},
			{Hostname: "worker", Role: RoleWorker, IsWorker: true},
		},
		EnvComments: map[string]EnvComments{
			"4": {
				Project: "Small production tier.",
				Service: map[string]string{
					"api":    "Enables zero-downtime rolling deploys.",
					"app":    "Enables zero-downtime rolling deploys.",
					"worker": "Enables zero-downtime rolling deploys.",
				},
			},
		},
	}
	vs, err := validateEnvImportComments(context.Background(), "4 — Small Production/import.yaml", []byte("irrelevant"), SurfaceInputs{Plan: plan})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !containsCode(vs, "templated-opening") {
		t.Errorf("expected templated-opening violation, got %+v", vs)
	}
}

// TestValidator_ImportComments_CausalWordRequired — §2.D: every
// service-block comment must contain a causal word. Pure narration
// fails.
func TestValidator_ImportComments_CausalWordRequired(t *testing.T) {
	t.Parallel()

	plan := &Plan{
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI}},
		EnvComments: map[string]EnvComments{
			"4": {
				Service: map[string]string{
					"api": "The API runtime service lists Node 22 as base.",
				},
			},
		},
	}
	vs, _ := validateEnvImportComments(context.Background(), "4 — Small Production/import.yaml", nil, SurfaceInputs{Plan: plan})
	if !containsCode(vs, "missing-causal-word") {
		t.Errorf("expected missing-causal-word violation, got %+v", vs)
	}
}

// TestValidator_KB_CitationRequired — §2.D: a KB bullet naming a
// topic that appears in CitationMap MUST reference the guide id.
// Missing reference → fail.
func TestValidator_KB_CitationRequired(t *testing.T) {
	t.Parallel()

	body := []byte(`# codebase/api

<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
### Gotchas

- **Missing env vars on the worker** — cross-service references do not
  self-shadow the way docs might suggest.
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`)
	vs, err := validateCodebaseKB(context.Background(), "codebases/api/README.md", body, SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !containsCode(vs, "kb-citation-missing") {
		t.Errorf("expected kb-citation-missing, got %+v", vs)
	}
}

// TestValidator_KB_BoldSymptom — §2.D: every KB bullet starts with a
// **bold** symptom phrase. Naked bullet fails.
func TestValidator_KB_BoldSymptom(t *testing.T) {
	t.Parallel()

	body := []byte(`<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
### Gotchas

- the object storage does not allow virtual-hosted style addressing
  (forcePathStyle: true required, env-var-model guide).
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`)
	vs, err := validateCodebaseKB(context.Background(), "codebases/api/README.md", body, SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !containsCode(vs, "kb-missing-bold-symptom") {
		t.Errorf("expected kb-missing-bold-symptom, got %+v", vs)
	}
}

// TestValidator_CrossSurfaceUniqueness — §2.D: a fact's Topic appears
// in exactly one stitched surface body. Same topic on two surfaces
// fails.
func TestValidator_CrossSurfaceUniqueness(t *testing.T) {
	t.Parallel()

	surfaces := map[string]string{
		"README.md":               "env-var-model self-shadow rule",
		"codebases/api/README.md": "env-var-model self-shadow rule is discussed here",
		"codebases/api/CLAUDE.md": "operational notes only",
	}
	facts := []FactRecord{
		{Topic: "env-var-model", Symptom: "x", Mechanism: "y", SurfaceHint: "platform-trap", Citation: "env-var-model"},
	}
	vs := validateCrossSurfaceUniqueness(surfaces, facts)
	if !containsCode(vs, "cross-surface-duplication") {
		t.Errorf("expected cross-surface-duplication, got %+v", vs)
	}
}

// TestValidator_CodebaseCLAUDE_MinimumSize — §2.D: CLAUDE.md must be
// ≥ 1200 bytes AND have ≥ 2 custom sections beyond the template.
func TestValidator_CodebaseCLAUDE_MinimumSize(t *testing.T) {
	t.Parallel()

	short := []byte(`# CLAUDE.md — api

## Zerops service facts

port 3000.

## Notes

none.
`)
	vs, err := validateCodebaseCLAUDE(context.Background(), "codebases/api/CLAUDE.md", short, SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !containsCode(vs, "claude-md-too-short") {
		t.Errorf("expected claude-md-too-short, got %+v", vs)
	}
}

// TestValidator_CodebaseYAML_CausalComment — §2.D: every comment in
// the committed zerops.yaml must contain a causal word. A "what the
// field does" narration comment fails.
func TestValidator_CodebaseYAML_CausalComment(t *testing.T) {
	t.Parallel()

	body := []byte(`zerops:
  - setup: dev
    # deployFiles ships the working tree to the runtime mount.
    deployFiles:
      - ./
    run:
      # Sets the base image for the container.
      base: nodejs@22
`)
	vs, err := validateCodebaseYAML(context.Background(), "codebases/api/zerops.yaml", body, SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !containsCode(vs, "yaml-comment-missing-causal-word") {
		t.Errorf("expected yaml-comment-missing-causal-word, got %+v", vs)
	}
}

// helpers

func containsCode(vs []Violation, code string) bool {
	for _, v := range vs {
		if v.Code == code {
			return true
		}
	}
	return false
}

func fakeDeployButtons() string {
	var b strings.Builder
	for range 6 {
		b.WriteString("\n[![Deploy on Zerops](https://x.svg)](https://app.zerops.io/recipes/x?environment=y)\n")
	}
	return b.String()
}

func padEnvREADME() string {
	// Pad to hit the 40-line floor without adding meta-voice words.
	var b strings.Builder
	for range 45 {
		b.WriteString("Filler line for length.\n")
	}
	return b.String()
}
