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
// ≥ 1200 bytes. Per run-10-readiness §P the too-few-custom-sections
// rule is deleted and replaced by a length cap + forbidden-subsection
// list, so the minimum-size floor stands alone.
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
	if containsCode(vs, "claude-md-too-few-custom-sections") {
		t.Errorf("too-few-custom-sections rule should be deleted per §P; got %+v", vs)
	}
}

// TestValidateCLAUDE_TooLong_Flagged — run-10-readiness §P. A CLAUDE.md
// over 60 lines emits `claude-md-too-long`. The reference
// (laravel-showcase-app/CLAUDE.md, 33 lines) sets the upper bound;
// run-9 shipped 99-line CLAUDE.md files because the old validator
// pressured authors to ADD sections during finalize iteration.
func TestValidateCLAUDE_TooLong_Flagged(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	b.WriteString("# CLAUDE.md — api\n\n## Zerops service facts\n\n")
	for range 70 {
		b.WriteString("filler fact line for bulk size\n")
	}
	b.WriteString("\n## Notes\n\none note.\n")
	body := []byte(b.String())
	vs, err := validateCodebaseCLAUDE(context.Background(),
		"/srv/apidev/CLAUDE.md", body, SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !containsCode(vs, "claude-md-too-long") {
		t.Errorf("expected claude-md-too-long for 70-line body, got %+v", vs)
	}
}

// TestValidateCLAUDE_UnderCap_Passes — run-10-readiness §P. A 45-line
// CLAUDE.md with service facts + notes passes cleanly.
func TestValidateCLAUDE_UnderCap_Passes(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	b.WriteString("# CLAUDE.md — api\n\n")
	b.WriteString("Nodejs 22 REST service on Zerops — HTTP port 3000 with PostgreSQL sibling, Valkey cache, and an NATS broker.\n\n")
	b.WriteString("## Zerops service facts\n\n")
	b.WriteString("- Hostname `api`, port 3000, DB host `db`, cache `cache`, broker `broker`.\n")
	b.WriteString("- Runtime base: `nodejs@22` (compiled) on the prod slot; dev slot runs `zsc noop --silent`.\n")
	b.WriteString("- Health endpoint `/health`; readiness probes it before traffic switches.\n")
	b.WriteString("- Cross-service env vars inject `${db_hostname}`, `${cache_connectionString}`, `${broker_connectionString}`.\n\n")
	b.WriteString("## Zerops dev\n\nDev slot is SSHFS-mounted at `/var/www/apidev/`. Run framework CLIs via SSH; never npm-install against the mount.\n\n")
	b.WriteString("## Notes\n\n")
	b.WriteString("- NATS `broker_connectionString` already encodes credentials — passing it as both `servers` and `auth` double-advertises and 403s.\n")
	b.WriteString("- Seed fires once per service lifetime via `zsc execOnce <slug>.seed.v1`; bump the version suffix to re-run.\n")
	b.WriteString("- Migrations run on every deploy via `${appVersionId}` execOnce — idempotent IF NOT EXISTS checks only.\n")
	b.WriteString("- Trust proxy must be enabled so `X-Forwarded-*` headers flow through the balancer correctly and the runtime reads real client IPs.\n")
	b.WriteString("- Uploads write to the `storage` sibling (S3-compatible); the bucket policy is private so signed URLs govern access.\n")
	body := []byte(b.String())
	// Confirm actual line count stays under 60 but over the byte floor.
	if got := strings.Count(string(body), "\n"); got >= 60 {
		t.Fatalf("test fixture accidentally > 60 lines: %d", got)
	}
	if len(body) < 1200 {
		t.Fatalf("test fixture accidentally < 1200 bytes: %d", len(body))
	}
	vs, err := validateCodebaseCLAUDE(context.Background(),
		"/srv/apidev/CLAUDE.md", body, SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if containsCode(vs, "claude-md-too-long") {
		t.Errorf("45-line CLAUDE.md should not trip too-long: %+v", vs)
	}
	if containsCode(vs, "claude-md-too-short") {
		t.Errorf("over-1200-byte CLAUDE.md should not trip too-short: %+v", vs)
	}
	if containsCode(vs, "claude-md-forbidden-subsection") {
		t.Errorf("normal sections should not trip forbidden-subsection: %+v", vs)
	}
}

// TestValidateCLAUDE_ForbiddenSubsection_Flagged — run-10-readiness §P.
// Cross-codebase operational content (`Quick curls`, `Smoke test`,
// `Local curl`, `In-container curls`, `Redeploy vs edit`, `Boot-time
// connectivity`) doesn't belong in a codebase-specific CLAUDE.md —
// it's identical across codebases and inflates each one's length.
func TestValidateCLAUDE_ForbiddenSubsection_Flagged(t *testing.T) {
	t.Parallel()

	for _, heading := range []string{
		"## Quick curls",
		"## Smoke test",
		"## Smoke tests",
		"### Local curl",
		"### In-container curls",
		"## Redeploy vs edit",
		"## Boot-time connectivity",
	} {
		var b strings.Builder
		b.WriteString("# CLAUDE.md — api\n\n")
		b.WriteString("Intro paragraph for the codebase explaining stack and runtime.\n\n")
		b.WriteString("## Zerops service facts\n\n")
		for range 30 {
			b.WriteString("- filler fact so the body clears the 1200 byte minimum with margin\n")
		}
		b.WriteString("\n")
		b.WriteString(heading + "\n\ncontent under the forbidden section.\n")
		body := []byte(b.String())
		vs, err := validateCodebaseCLAUDE(context.Background(),
			"/srv/apidev/CLAUDE.md", body, SurfaceInputs{Plan: &Plan{}})
		if err != nil {
			t.Fatalf("validate %q: %v", heading, err)
		}
		if !containsCode(vs, "claude-md-forbidden-subsection") {
			t.Errorf("heading %q should trip forbidden-subsection; got %+v", heading, vs)
		}
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

// TestValidateCodebaseYAML_DividerBanned — run-9-readiness §2.H.
// Decorative divider lines (runs of 4+ identical `-`, `=`, `*`, `#`,
// `_`) are banned. Emits `yaml-comment-divider-banned`.
func TestValidateCodebaseYAML_DividerBanned(t *testing.T) {
	t.Parallel()

	body := []byte(`# ----------------------------------------
zerops:
  - setup: dev
    # ====== DEV SETUP ======
    run:
      # ****
      base: nodejs@22
`)
	vs, err := validateCodebaseYAML(context.Background(),
		"codebases/api/zerops.yaml", body, SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !containsCode(vs, "yaml-comment-divider-banned") {
		t.Errorf("expected yaml-comment-divider-banned, got %+v", vs)
	}
	// Three divider lines → at least three violations with that code.
	var count int
	for _, v := range vs {
		if v.Code == "yaml-comment-divider-banned" {
			count++
		}
	}
	if count < 3 {
		t.Errorf("expected 3+ divider violations, got %d", count)
	}
}

// TestValidateCodebaseYAML_BlankCommentAllowed — a single bare `#` is
// a section transition, not a divider.
func TestValidateCodebaseYAML_BlankCommentAllowed(t *testing.T) {
	t.Parallel()

	body := []byte(`zerops:
  - setup: dev
    #
    # deployFiles ships everything because dev self-deploys need the tree.
    deployFiles:
      - ./
`)
	vs, err := validateCodebaseYAML(context.Background(),
		"codebases/api/zerops.yaml", body, SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if containsCode(vs, "yaml-comment-divider-banned") {
		t.Errorf("bare `#` flagged as divider: %+v", vs)
	}
}

// TestValidateCodebaseYAML_ShortRunsNotFlagged — `# --` (2-char run)
// is allowed; 4+ chars flag.
func TestValidateCodebaseYAML_ShortRunsNotFlagged(t *testing.T) {
	t.Parallel()

	body := []byte(`zerops:
  - setup: dev
    # -- below here because test harness wants two dashes specifically
    run:
      base: nodejs@22
`)
	vs, err := validateCodebaseYAML(context.Background(),
		"codebases/api/zerops.yaml", body, SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if containsCode(vs, "yaml-comment-divider-banned") {
		t.Errorf("2-char run flagged as divider: %+v", vs)
	}
}

// TestValidateKB_AllTripleFormat_FlagsAll — run-10-readiness §O.
// KB entries opening with `**symptom**:` / `**mechanism**:` / `**fix**:`
// triples belong in CLAUDE.md/notes, not in the porter-facing KB.
// Every triple-shaped bullet emits a
// `codebase-kb-triple-format-banned` violation.
func TestValidateKB_AllTripleFormat_FlagsAll(t *testing.T) {
	t.Parallel()

	body := []byte(`<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->

### Gotchas

- **symptom**: 502 at the L7 balancer. **mechanism**: NestJS bind defaults. **fix**: call app.listen(port, '0.0.0.0'). Cited guide: http-support.
- **symptom**: trust proxy not set. **mechanism**: headers ignored. **fix**: set app.set('trust proxy', true). Cited guide: http-support.
- **symptom**: NATS double auth. **mechanism**: credentials in both servers + auth. **fix**: pass connectionString only. Cited guide: env-var-model.

<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`)
	vs, err := validateCodebaseKB(context.Background(),
		"/srv/apidev/README.md", body, SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	var count int
	for _, v := range vs {
		if v.Code == "codebase-kb-triple-format-banned" {
			count++
		}
	}
	if count != 3 {
		t.Errorf("expected 3 triple-format violations, got %d: %+v", count, vs)
	}
}

// TestValidateKB_AllTopicFormat_Passes — run-10-readiness §O. Reference
// style — `**Topic** — explanation` bullets — triggers zero
// triple-format violations.
func TestValidateKB_AllTopicFormat_Passes(t *testing.T) {
	t.Parallel()

	body := []byte(`<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->

### Gotchas

- **No .env file** — Zerops injects environment variables as OS env vars. Creating a .env file with empty values shadows the OS vars. Cited guide: env-var-model.
- **Trust the reverse proxy** — the runtime sits behind an L7 balancer that sets X-Forwarded-*. Cited guide: http-support.

<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`)
	vs, err := validateCodebaseKB(context.Background(),
		"/srv/apidev/README.md", body, SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if containsCode(vs, "codebase-kb-triple-format-banned") {
		t.Errorf("topic-format KB should not trip triple validator: %+v", vs)
	}
}

// TestValidateKB_MixedFormat_FlagsOnlyTriples — run-10-readiness §O.
// A bimodal KB (run-9 shape) emits violations only for the triple
// entries — Topic-format bullets are unaffected.
func TestValidateKB_MixedFormat_FlagsOnlyTriples(t *testing.T) {
	t.Parallel()

	body := []byte(`<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->

### Gotchas

- **symptom**: 502 at the balancer. **mechanism**: bind default. **fix**: listen on 0.0.0.0. Cited guide: http-support.
- **Expose X-Cache via CORS** — browser fetch sees only Access-Control-Expose-Headers. Cited guide: http-support.
- **symptom**: NATS double auth. **mechanism**: credentials. **fix**: connectionString only. Cited guide: env-var-model.

<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`)
	vs, err := validateCodebaseKB(context.Background(),
		"/srv/apidev/README.md", body, SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	var count int
	for _, v := range vs {
		if v.Code == "codebase-kb-triple-format-banned" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("expected 2 triple violations (only the triple entries), got %d: %+v", count, vs)
	}
}

// TestPrinciples_InitCommandsCoversArbitraryStaticKey — run-10-readiness
// §Q4. init-commands-model.md now documents the third key shape
// (`<slug>.<operation>.<version>` static string, once-per-lifetime
// semantics + documented re-run lever). Run-9's feature sub-agent
// queried zerops_knowledge five times with rephrased queries because
// the atom didn't cover this case.
func TestPrinciples_InitCommandsCoversArbitraryStaticKey(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	plan.FeatureKinds = []string{"seed", "scout-import"}
	brief, err := BuildFeatureBrief(plan)
	if err != nil {
		t.Fatalf("BuildFeatureBrief: %v", err)
	}
	for _, anchor := range []string{
		"Three key shapes",
		"<slug>.<operation>.v1",
		"Arbitrary static",
	} {
		if !strings.Contains(brief.Body, anchor) {
			t.Errorf("feature brief missing init-commands-model anchor %q", anchor)
		}
	}
	if !strings.Contains(brief.Body, "key shape #3") {
		t.Errorf("content_extension.md pointer to key shape #3 missing from feature brief")
	}
}

// TestBrief_Scaffold_ContainsValidatorTripwires — run-10-readiness §Q3.
// The "Validator tripwires" section of content_authoring.md surfaces
// six finalize-time validator rules as author-time guidance so
// sub-agents pre-empt the iteration cost observed in run 9.
func TestBrief_Scaffold_ContainsValidatorTripwires(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	brief, err := BuildScaffoldBrief(plan, plan.Codebases[0], nil)
	if err != nil {
		t.Fatalf("BuildScaffoldBrief: %v", err)
	}
	for _, anchor := range []string{
		"Validator tripwires",
		"item #1 is engine-owned",
		"scaffold-only filenames",
		"porter voice",
		"Env READMEs target",
		"one causal word per block",
		"CLAUDE.md: 30–50 lines",
	} {
		if !strings.Contains(brief.Body, anchor) {
			t.Errorf("scaffold brief Validator tripwires missing anchor %q", anchor)
		}
	}
}

// TestBrief_Scaffold_ContainsSlotHostnameTripwire — run-11 gap N-2.
// Scaffold brief's Validator-tripwires section names the slot-vs-
// codebase distinction explicitly so sub-agents preempt the run-10
// scaffold-app mistake (5 fragments under codebase/appdev/*).
func TestBrief_Scaffold_ContainsSlotHostnameTripwire(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	brief, err := BuildScaffoldBrief(plan, plan.Codebases[0], nil)
	if err != nil {
		t.Fatalf("BuildScaffoldBrief: %v", err)
	}
	for _, anchor := range []string{
		"cb.Hostname",
		"slot hostname",
		"appdev",
	} {
		if !strings.Contains(brief.Body, anchor) {
			t.Errorf("scaffold brief slot tripwire missing anchor %q", anchor)
		}
	}
}

// TestBrief_Scaffold_ContainsSelfInflictedLitmus — run-11 gap V-5.
// The scaffold brief teaches the spec rule 4 self-inflicted litmus
// with three concrete run-10 anti-patterns so sub-agents pre-empt the
// over-rationalize-as-platform-trap pattern that produced 7 wrong-class
// KB bullets in run 10.
func TestBrief_Scaffold_ContainsSelfInflictedLitmus(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	brief, err := BuildScaffoldBrief(plan, plan.Codebases[0], nil)
	if err != nil {
		t.Fatalf("BuildScaffoldBrief: %v", err)
	}
	for _, anchor := range []string{
		"Self-inflicted litmus",
		"npx ts-node",
		".deployignore",
		"Trust proxy is per-framework",
		"would a porter",
	} {
		if !strings.Contains(brief.Body, anchor) {
			t.Errorf("scaffold brief Self-inflicted litmus missing anchor %q", anchor)
		}
	}
}

// TestBrief_Feature_ContainsSelfInflictedLitmus — V-5 cross-reference
// in the feature brief. Feature sub-agents face the same temptation;
// the litmus must follow them across phase boundaries.
func TestBrief_Feature_ContainsSelfInflictedLitmus(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	brief, err := BuildFeatureBrief(plan)
	if err != nil {
		t.Fatalf("BuildFeatureBrief: %v", err)
	}
	if !strings.Contains(brief.Body, "Self-inflicted litmus") {
		t.Errorf("feature brief missing Self-inflicted litmus reference, got body without anchor")
	}
}

// TestBrief_Scaffold_UnderCap_WithValidatorTripwires — run-10-readiness
// §Q3 + run-9 tranche-2 cap raise. The Validator-tripwires section
// keeps the scaffold brief under the 12 KB cap across all three
// synthetic codebases. Regression pin so future additions don't
// silently push any role over.
func TestBrief_Scaffold_UnderCap_WithValidatorTripwires(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	for _, cb := range plan.Codebases {
		brief, err := BuildScaffoldBrief(plan, cb, nil)
		if err != nil {
			t.Fatalf("BuildScaffoldBrief %s: %v", cb.Hostname, err)
		}
		if brief.Bytes > ScaffoldBriefCap {
			t.Errorf("brief %s: %d bytes > %d cap", cb.Hostname, brief.Bytes, ScaffoldBriefCap)
		}
	}
}

// TestBrief_Scaffold_HeaderIsBehavioralGate — run-10-readiness §Q2.
// The `# Pre-ship contract` header is renamed to `# Behavioral gate`
// so the brief's authoring vocabulary stops colliding with the
// voice-rule forbidden phrase (`"pre-ship contract"` stays in the
// forbidden list for source-code and fragment-body content).
func TestBrief_Scaffold_HeaderIsBehavioralGate(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	brief, err := BuildScaffoldBrief(plan, plan.Codebases[0], nil)
	if err != nil {
		t.Fatalf("BuildScaffoldBrief: %v", err)
	}
	if !strings.Contains(brief.Body, "# Behavioral gate") {
		t.Errorf("scaffold brief missing `# Behavioral gate` header")
	}
	if strings.Contains(brief.Body, "# Pre-ship contract") {
		t.Errorf("scaffold brief still carries `# Pre-ship contract` header")
	}
}

// TestBrief_Scaffold_OmitsHTTPSectionForNonHTTPRole — run-10-readiness
// §Q1. Scaffold brief for a role whose contract has ServesHTTP=false
// (worker / job-consumer) does not emit the `## HTTP` section; the
// section was previously emitted unconditionally and the sub-agent
// had to mentally skip it.
func TestBrief_Scaffold_OmitsHTTPSectionForNonHTTPRole(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	var worker Codebase
	for _, cb := range plan.Codebases {
		if cb.Role == RoleWorker {
			worker = cb
		}
	}
	if worker.Hostname == "" {
		t.Fatal("synthetic plan has no worker codebase")
	}
	brief, err := BuildScaffoldBrief(plan, worker, nil)
	if err != nil {
		t.Fatalf("BuildScaffoldBrief: %v", err)
	}
	if strings.Contains(brief.Body, "## HTTP") {
		t.Errorf("worker brief should omit ## HTTP section; got:\n%s", brief.Body)
	}
}

// TestBrief_Scaffold_IncludesHTTPSectionForHTTPRole — run-10-readiness
// §Q1. HTTP-serving roles (api, frontend, monolith) still see the
// `## HTTP` platform-obligations section, now with a plain header
// (the `(ServesHTTP=true)` annotation was noise — the section only
// exists when ServesHTTP is actually true).
func TestBrief_Scaffold_IncludesHTTPSectionForHTTPRole(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	var api Codebase
	for _, cb := range plan.Codebases {
		if cb.Role == RoleAPI {
			api = cb
		}
	}
	if api.Hostname == "" {
		t.Fatal("synthetic plan has no api codebase")
	}
	brief, err := BuildScaffoldBrief(plan, api, nil)
	if err != nil {
		t.Fatalf("BuildScaffoldBrief: %v", err)
	}
	if !strings.Contains(brief.Body, "## HTTP\n") {
		t.Errorf("api brief must include ## HTTP section (plain header); got:\n%s", brief.Body)
	}
	if strings.Contains(brief.Body, "## HTTP (ServesHTTP=true)") {
		t.Errorf("api brief should drop the `(ServesHTTP=true)` annotation; got:\n%s", brief.Body)
	}
}

// TestBrief_Scaffold_KBGuidanceMatchesTopicFormat — run-10-readiness §O.
// The scaffold brief body teaches `**Topic** — prose` and explicitly
// bans the `**symptom**:` triple for KB.
func TestBrief_Scaffold_KBGuidanceMatchesTopicFormat(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	brief, err := BuildScaffoldBrief(plan, plan.Codebases[0], nil)
	if err != nil {
		t.Fatalf("BuildScaffoldBrief: %v", err)
	}
	for _, anchor := range []string{
		"**Topic**",
		"Do NOT use `**symptom**:`",
		"claude-md/notes",
	} {
		if !strings.Contains(brief.Body, anchor) {
			t.Errorf("scaffold brief missing KB anchor %q", anchor)
		}
	}
}

// TestValidateCodebaseYAML_MultiLineBlockWithOneCausalWord_Passes —
// run-10-readiness §N. A multi-line comment block passes when ANY line
// in the block carries a causal word / em-dash; individual lines no
// longer each need their own causal word. Matches the reference
// style at /Users/fxck/www/laravel-showcase-app/zerops.yaml where
// comment blocks wrap natural prose across 2–5 lines.
func TestValidateCodebaseYAML_MultiLineBlockWithOneCausalWord_Passes(t *testing.T) {
	t.Parallel()

	// 6-line block; only line 2 carries a causal word. All six lines are
	// > 40 chars so the label short-circuit doesn't apply — the block
	// must actually pass via per-block causal detection.
	body := []byte(`zerops:
  - setup: prod
    run:
      # Config, route, and view caches MUST be built at runtime aaaaaaa.
      # Build runs at /build/source but runtime serves from /var/www, so
      # caching during build would bake paths the runtime never sees zz.
      # Migrations run exactly once per deploy via zsc execOnce tickets,
      # regardless of how many containers start in parallel at deploy y.
      # Seeder populates sample data on first deploy for the dashboard.
      initCommands:
        - zsc execOnce ${appVersionId} -- php artisan migrate --force
`)
	vs, err := validateCodebaseYAML(context.Background(),
		"/srv/apidev/zerops.yaml", body, SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if containsCode(vs, "yaml-comment-missing-causal-word") {
		t.Errorf("multi-line block with one causal word should pass; got %+v", vs)
	}
}

// TestValidateCodebaseYAML_MultiLineBlockNoCausalWord_OneViolationPerBlock
// — run-10-readiness §N. A 4-line block with no causal word anywhere
// emits exactly one violation, not four.
func TestValidateCodebaseYAML_MultiLineBlockNoCausalWord_OneViolationPerBlock(t *testing.T) {
	t.Parallel()

	body := []byte(`zerops:
  - setup: prod
    # This block narrates what fields do and has no rationale at all here
    # nor does it explain tradeoffs or alternatives for the reader either
    # and the third line keeps up the pure description of fields verbose
    # and the fourth line continues the same toneless description style.
    run:
      base: nodejs@22
`)
	vs, err := validateCodebaseYAML(context.Background(),
		"/srv/apidev/zerops.yaml", body, SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	var count int
	for _, v := range vs {
		if v.Code == "yaml-comment-missing-causal-word" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 block-level violation, got %d: %+v", count, vs)
	}
}

// TestValidateCodebaseYAML_ShortLabelComment_Passes — run-10-readiness §N.
// Single-line comments shorter than 40 characters after stripping `#`
// are treated as labels and pass unconditionally. Matches reference
// patterns like `# Base image` or `# Bucket policy`.
func TestValidateCodebaseYAML_ShortLabelComment_Passes(t *testing.T) {
	t.Parallel()

	body := []byte(`zerops:
  - setup: prod
    # Base image
    run:
      base: nodejs@22
`)
	vs, err := validateCodebaseYAML(context.Background(),
		"/srv/apidev/zerops.yaml", body, SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if containsCode(vs, "yaml-comment-missing-causal-word") {
		t.Errorf("short label comment should pass; got %+v", vs)
	}
}

// TestValidateCodebaseYAML_BareHashTransitionInBlock_Allowed —
// run-10-readiness §N. Bare `#` lines inside a comment block are
// paragraph separators (reference style); they do not end the block
// for the purposes of the causal-word check. A block spanning bare-#
// separated paragraphs passes if any line anywhere carries a causal
// word.
func TestValidateCodebaseYAML_BareHashTransitionInBlock_Allowed(t *testing.T) {
	t.Parallel()

	body := []byte(`zerops:
  - setup: prod
    run:
      # Config, route, and view caches MUST be built at runtime because
      # /build/source differs from /var/www, baking wrong paths otherwise.
      #
      # Second paragraph is pure description that wraps across lines and
      # continues the thought of the comment block without causal words.
      base: php-nginx@8.4
`)
	vs, err := validateCodebaseYAML(context.Background(),
		"/srv/apidev/zerops.yaml", body, SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if containsCode(vs, "yaml-comment-missing-causal-word") {
		t.Errorf("bare-# separator inside block should keep block unified; got %+v", vs)
	}
}

// TestValidate_CodebaseSurface_ReadsSourceRoot — run-10-readiness §L.
// resolveSurfacePaths for codebase-scoped surfaces returns
// <cb.SourceRoot>/<leaf>, not <outputRoot>/codebases/<h>/<leaf>.
// Validators read from the same tree that stitch writes to.
func TestValidate_CodebaseSurface_ReadsSourceRoot(t *testing.T) {
	t.Parallel()

	plan := &Plan{
		Codebases: []Codebase{
			{Hostname: "api", SourceRoot: "/srv/workspace/apidev"},
			{Hostname: "worker", SourceRoot: "/srv/workspace/workerdev"},
		},
	}
	cases := []struct {
		surface Surface
		leaf    string
	}{
		{SurfaceCodebaseIG, "README.md"},
		{SurfaceCodebaseKB, "README.md"},
		{SurfaceCodebaseCLAUDE, "CLAUDE.md"},
		{SurfaceCodebaseZeropsComments, "zerops.yaml"},
	}
	for _, c := range cases {
		got := resolveSurfacePaths("/never/used", c.surface, plan)
		want := []string{
			"/srv/workspace/apidev/" + c.leaf,
			"/srv/workspace/workerdev/" + c.leaf,
		}
		if len(got) != len(want) {
			t.Errorf("surface %s: len=%d want %d (%v)", c.surface, len(got), len(want), got)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("surface %s path[%d] = %q, want %q", c.surface, i, got[i], want[i])
			}
		}
	}
}

// TestBrief_Scaffold_IncludesYamlCommentStyle — run-9-readiness §2.H
// brief-side atom. Scaffold + feature briefs both inject.
func TestBrief_Scaffold_IncludesYamlCommentStyle(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	brief, err := BuildScaffoldBrief(plan, plan.Codebases[0], nil)
	if err != nil {
		t.Fatalf("BuildScaffoldBrief: %v", err)
	}
	for _, anchor := range []string{
		"YAML comment style",
		"No dividers",
		"No banners",
	} {
		if !strings.Contains(brief.Body, anchor) {
			t.Errorf("scaffold brief missing yaml-comment-style anchor %q", anchor)
		}
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
