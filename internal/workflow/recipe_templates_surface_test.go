package workflow

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
	"gopkg.in/yaml.v3"
)

// v39 Commit 1 Day 3 — three-way-equality gold test for env-README prose.
//
// The v38 editorial-review run produced 5 CRITs, 4 of which traced directly
// to hardcoded prose in recipe_templates.go claiming tier-specific behavior
// the yaml generator never emitted (expanded toolchain at env 1, stage DB
// aliasing to dev, managed-service scaling growing at env 4, factual errors
// about healthCheck tuning). The v39 refactor removed those claims; this
// test is the gold that prevents re-introduction.
//
// The test enforces three-way equality: every literal yaml field value a
// prose paragraph cites (e.g. "minContainers: 2", "mode: HA",
// "zeropsSetup: dev") must match what GenerateEnvImportYAML(plan, envIndex)
// actually emits for that env — OR for envIndex+1 when the claim lives in
// the Promoting section (which legitimately describes the target tier).
//
// Plus a forbidden-pattern list: every phrase v38 CRITs flagged as
// fabrication is asserted NOT to appear in any env README. New CRITs from
// future v-runs are added here as they surface.

// ── Part A: forbidden v38-CRIT fabrication patterns ──────────────────────

// v38CRITForbiddenPhrases enumerates phrasings editorial-review flagged as
// factually unsupported by the yaml generator in v38. Each is a case-
// insensitive substring match against the combined env README output.
//
// Adding a phrase here: the phrase must be (a) observed in a prior run's
// editorial-review findings AND (b) not corresponding to any field that
// GenerateEnvImportYAML emits. Anything that IS a plan-backed claim goes
// in Part B, not here.
var v38CRITForbiddenPhrases = []string{
	// Cluster A — env 0 vs env 1 toolchain/image differentiation.
	// No plan field or yaml output backs a "CDE toolchain" or "dev image
	// differs" distinction; env 0 and env 1 emit identical dev-slot yaml.
	"expanded toolchain",
	"cde toolchain",
	"dev container image differs",
	"full toolchain preinstalled",
	"ide remote server, shell customizations",

	// Cluster B — healthCheck / readinessCheck tier-tuning.
	// healthCheck lives in per-codebase zerops.yaml (writer-authored), not
	// env import.yaml. No tier-specific tuning is emitted.
	"healthcheck timing is relaxed",
	"healthcheck uses relaxed",
	"readiness thresholds",
	"tighter readiness-probe",
	"tighter readiness probe",

	// Cluster C — backup policy claims.
	// No BackupPolicy field in RecipePlan; no backup stanza in yaml output.
	// Backups are platform-admin-only.
	"daily backups",
	"daily snapshot",
	"daily db snapshot",
	"backup policy",
	"backups become meaningful",
	"backups matter",

	// Cluster D — env 3 → env 4 managed-service scaling claim.
	// writeAutoscaling emits IDENTICAL output for env 3 and env 4.
	"managed services scale up",
	"managed-service sizing grows",
	"db container size grows",
	"cache memory allocation grows",
	"search engine index capacity grows",
	"larger db, more cache memory",

	// Cluster E — stage-DB-aliasing-to-dev.
	// Each tier declares a distinct project.name; stage has its own Zerops
	// project with its own DB. v38 editorial-review CRIT #3 verbatim.
	"stage hits the same db",
}

func TestFinalizeOutput_NoV38CRITFabrications(t *testing.T) {
	t.Parallel()

	// Showcase plan exercises every non-runtime service kind (db, cache,
	// queue, storage, search, mailpit) + a separate worker. Runs the
	// check for both single-runtime and dual-runtime showcases.
	plans := map[string]*RecipePlan{
		"minimal":     testMinimalPlan(),
		"showcase":    testShowcasePlan(),
		"dualRuntime": testDualRuntimePlan(),
	}

	for planName, plan := range plans {
		for envIndex := 0; envIndex < EnvTierCount(); envIndex++ {
			t.Run(fmt.Sprintf("%s_env_%d", planName, envIndex), func(t *testing.T) {
				t.Parallel()
				readme := strings.ToLower(GenerateEnvREADME(plan, envIndex))
				for _, phrase := range v38CRITForbiddenPhrases {
					if strings.Contains(readme, strings.ToLower(phrase)) {
						t.Errorf("env %d README contains forbidden fabrication phrase %q\n"+
							"This phrase was flagged by editorial-review as content the yaml generator does not back.\n"+
							"See docs/zcprecipator2/plans/v39-commit1-bullet-audit.md §5 for the classification.",
							envIndex, phrase)
					}
				}
			})
		}
	}
}

// ── Part B: plan-backed claim three-way-equality ─────────────────────────

// literalYAMLFieldClaim captures a field:value literal the prose may cite.
// The regex extracts the value so the test can compare it against the yaml
// emission.
type literalYAMLFieldClaim struct {
	Name        string         // human-readable name for test output
	Re          *regexp.Regexp // regex capturing the field value
	YAMLField   func(svc serviceShape) (string, bool)
	RuntimeOnly bool // claim applies only to runtime services (e.g. minContainers)
}

// serviceShape is the minimal yaml parsing shape we care about for prose-
// vs-yaml comparisons.
type serviceShape struct {
	Type                string `yaml:"type"`
	Mode                string `yaml:"mode,omitempty"`
	MinContainers       *int   `yaml:"minContainers,omitempty"`
	ZeropsSetup         string `yaml:"zeropsSetup,omitempty"`
	Hostname            string `yaml:"hostname,omitempty"`
	VerticalAutoscaling struct {
		CPUMode string `yaml:"cpuMode,omitempty"`
	} `yaml:"verticalAutoscaling,omitempty"`
}

type envYAMLShape struct {
	Project struct {
		Name        string `yaml:"name,omitempty"`
		CorePackage string `yaml:"corePackage,omitempty"`
	} `yaml:"project,omitempty"`
	Services []serviceShape `yaml:"services,omitempty"`
}

// parseEnvYAML parses the import.yaml emitted by GenerateEnvImportYAML into
// a typed shape the test can walk. Handles the top-level comment lines by
// letting yaml.Unmarshal ignore leading `#`-prefixed lines naturally.
func parseEnvYAML(t *testing.T, raw string) envYAMLShape {
	t.Helper()
	var shape envYAMLShape
	if err := yaml.Unmarshal([]byte(raw), &shape); err != nil {
		t.Fatalf("parse env yaml: %v\nyaml:\n%s", err, raw)
	}
	return shape
}

// stripFencedAndInlineCode returns prose with fenced code blocks removed so
// illustrative yaml snippets don't count as prose claims. Inline-code spans
// (`...`) are kept — that's where the prose pins are.
func stripFencedAndInlineCode(s string) string {
	fenced := regexp.MustCompile("(?s)```[^`]*```")
	return fenced.ReplaceAllString(s, "")
}

// emittedYAMLValues returns the set of distinct literal values a yaml field
// takes across all services in the env (or across both envs when a second
// shape is given, for promotion-path prose). Runtime-only claims filter
// services via IsRuntimeType.
func emittedYAMLValues(
	shapes []envYAMLShape,
	get func(svc serviceShape) (string, bool),
	runtimeOnly bool,
) map[string]bool {
	out := map[string]bool{}
	for _, shape := range shapes {
		for _, svc := range shape.Services {
			if runtimeOnly && !topology.IsRuntimeType(svc.Type) {
				continue
			}
			if v, ok := get(svc); ok {
				out[v] = true
			}
		}
	}
	return out
}

// extractProseClaims returns every captured value from a regex applied to
// prose (with fenced code already stripped). Used to pull e.g. the integer
// N from each `minContainers: N` claim.
func extractProseClaims(prose string, re *regexp.Regexp) []string {
	stripped := stripFencedAndInlineCode(prose)
	var out []string
	for _, m := range re.FindAllStringSubmatch(stripped, -1) {
		if len(m) >= 2 {
			out = append(out, m[1])
		}
	}
	return out
}

func TestFinalizeOutput_ProseClaimsMatchEmittedYAML(t *testing.T) {
	t.Parallel()

	plan := testShowcasePlan()

	// Pre-render all six env yamls so promotion-path prose at env i can be
	// pinned against env i+1's emission.
	yamls := make([]envYAMLShape, EnvTierCount())
	for i := 0; i < EnvTierCount(); i++ {
		yamls[i] = parseEnvYAML(t, GenerateEnvImportYAML(plan, i))
	}

	// Literal claims the prose may cite. Each paired with a yaml-field
	// extractor and a "runtime-only" flag. When a prose bullet claims a
	// value, that value must appear either in this env's yaml OR in the
	// adjacent-tier yaml (promotion-path prose describes the target tier).
	claims := []literalYAMLFieldClaim{
		{
			Name:        "minContainers",
			Re:          regexp.MustCompile("`?minContainers:\\s*(\\d+)`?"),
			RuntimeOnly: true,
			YAMLField: func(svc serviceShape) (string, bool) {
				if svc.MinContainers == nil {
					// Zerops platform default is 1 — include so prose
					// claims about single-replica tier 2-3 runtime pass.
					return "1", true
				}
				return strconv.Itoa(*svc.MinContainers), true
			},
		},
		{
			Name: "mode",
			Re:   regexp.MustCompile("`?mode:\\s*(HA|NON_HA)`?"),
			YAMLField: func(svc serviceShape) (string, bool) {
				if svc.Mode == "" {
					return "", false
				}
				return svc.Mode, true
			},
		},
		{
			Name: "zeropsSetup",
			Re:   regexp.MustCompile("`?zeropsSetup:\\s*(dev|prod|app)`?"),
			YAMLField: func(svc serviceShape) (string, bool) {
				if svc.ZeropsSetup == "" {
					return "", false
				}
				return svc.ZeropsSetup, true
			},
		},
		{
			Name: "cpuMode",
			Re:   regexp.MustCompile("`?cpuMode:\\s*(DEDICATED)`?"),
			YAMLField: func(svc serviceShape) (string, bool) {
				if svc.VerticalAutoscaling.CPUMode == "" {
					return "", false
				}
				return svc.VerticalAutoscaling.CPUMode, true
			},
		},
	}

	for envIndex := 0; envIndex < EnvTierCount(); envIndex++ {
		for _, claim := range claims {
			t.Run(fmt.Sprintf("env_%d_%s", envIndex, claim.Name), func(t *testing.T) {
				t.Parallel()

				readme := GenerateEnvREADME(plan, envIndex)
				proseValues := extractProseClaims(readme, claim.Re)
				if len(proseValues) == 0 {
					return
				}

				// Union of this tier + next tier (if any) yaml values.
				shapes := []envYAMLShape{yamls[envIndex]}
				if envIndex+1 < EnvTierCount() {
					shapes = append(shapes, yamls[envIndex+1])
				}
				declared := emittedYAMLValues(shapes, claim.YAMLField, claim.RuntimeOnly)

				for _, v := range proseValues {
					if !declared[v] {
						t.Errorf(
							"env %d README claims %s=%q but neither env %d nor env %d yaml emits that value.\n"+
								"declared (union): %v\n"+
								"If the prose is correct, the yaml generator or the target env needs the field; "+
								"if the yaml is correct, the prose is fabricating — drop or reword.",
							envIndex, claim.Name, v, envIndex, envIndex+1, declared,
						)
					}
				}
			})
		}
	}
}

// ── Part C: Cluster G derived-hostnames pin ──────────────────────────────

// TestEnvOperationalConcerns_Env2HostnamesDerivedFromPlan pins the Cluster G
// fix (v39 Commit 1): env-2's VPN hostname bullet lists every non-runtime
// target's hostname from the plan, not a hardcoded set. Regression here
// would re-introduce the "db, cache, queue, storage, search" hardcode that
// the audit flagged for plans that use different hostnames.
func TestEnvOperationalConcerns_Env2HostnamesDerivedFromPlan(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		plan *RecipePlan
	}{
		{"minimal", testMinimalPlan()},
		{"showcase", testShowcasePlan()},
		{"dualRuntime", testDualRuntimePlan()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			prose := envOperationalConcerns(tt.plan, 2)
			for _, target := range tt.plan.Targets {
				if topology.IsRuntimeType(target.Type) {
					continue
				}
				wantToken := "`" + target.Hostname + "`"
				if !strings.Contains(prose, wantToken) {
					t.Errorf("env 2 operational concerns missing hostname token %s for plan %s\nprose:\n%s",
						wantToken, tt.name, prose)
				}
			}
		})
	}
}

// TestEnvOperationalConcerns_NilPlanSafe ensures the managedServiceHostnameList
// helper degrades gracefully when called with nil — the workflow state can
// reach envOperationalConcerns before the plan is populated in error paths.
func TestEnvOperationalConcerns_NilPlanSafe(t *testing.T) {
	t.Parallel()
	prose := envOperationalConcerns(nil, 2)
	if prose == "" {
		t.Error("envOperationalConcerns(nil, 2) returned empty string, expected fallback prose")
	}
	if !strings.Contains(prose, "managed-service hostnames") {
		t.Errorf("expected fallback phrase in prose, got:\n%s", prose)
	}
}
