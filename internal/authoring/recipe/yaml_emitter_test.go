package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// syntheticShowcasePlan builds a framework-agnostic plan with three
// codebases (api + app + worker) and four managed services (db, cache,
// broker, storage). Hostnames are generic so the fixture never teaches
// framework specifics.
func syntheticShowcasePlan() *Plan {
	return &Plan{
		Slug:      "synth-showcase",
		Framework: "synth",
		Tier:      "showcase",
		Research: ResearchResult{
			CodebaseShape:  "3",
			NeedsAppSecret: true,
			AppSecretKey:   "APP_SECRET",
			Description:    "synthetic showcase plan used as yaml-emitter fixture",
		},
		Codebases: []Codebase{
			{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"},
			{Hostname: "app", Role: RoleFrontend, BaseRuntime: "nodejs@22"},
			{Hostname: "worker", Role: RoleWorker, BaseRuntime: "nodejs@22", IsWorker: true},
		},
		Services: []Service{
			{Hostname: "db", Type: "postgresql@18", Kind: ServiceKindManaged, Priority: 10},
			{Hostname: "cache", Type: "valkey@7", Kind: ServiceKindManaged, Priority: 10},
			{Hostname: "broker", Type: "nats@2", Kind: ServiceKindManaged, Priority: 10},
			{Hostname: "storage", Type: "object-storage", Kind: ServiceKindStorage},
		},
		EnvComments: map[string]EnvComments{
			"0": {
				Project: "AI agent workspace — a dev slot per codebase for SSH iteration\nplus a stage slot that validates the production build path.",
				Service: map[string]string{
					"apidev":   "API dev — SSHFS-mounted source, hot reload.",
					"apistage": "API stage — prod build validation.",
					"db":       "Postgres for the greetings table.",
				},
			},
			"5": {
				Project: "HA production — two replicas per runtime, DEDICATED CPU.",
				Service: map[string]string{
					"api":     "API in HA — two replicas behind the L7 balancer.",
					"db":      "Postgres HA — managed failover.",
					"storage": "Object storage — private policy.",
				},
			},
		},
		ProjectEnvVars: map[string]map[string]string{
			"0": {"DEV_API_URL": "${api_zeropsSubdomainHost}"},
			"5": {"PROD_API_URL": "${api_zeropsSubdomainHost}"},
		},
	}
}

func TestYAMLEmitter_Tier0_Dev(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	got, err := EmitImportYAML(plan, 0)
	if err != nil {
		t.Fatalf("EmitImportYAML: %v", err)
	}

	// Preprocessor directive first line when secret present.
	if !strings.HasPrefix(got, "#zeropsPreprocessor=on") {
		t.Errorf("tier 0: missing preprocessor directive at BOF; got first line %q",
			firstLine(got))
	}
	// Secret field emitted at project level.
	mustContain(t, got, "APP_SECRET: <@generateRandomString(<32>)>")
	// Per-tier project var emitted.
	mustContain(t, got, "DEV_API_URL: ${api_zeropsSubdomainHost}")
	// Dev services emitted for each runtime codebase (worker always gets its own).
	mustContain(t, got, "- hostname: apidev")
	mustContain(t, got, "- hostname: apistage")
	mustContain(t, got, "- hostname: appdev")
	mustContain(t, got, "- hostname: appstage")
	mustContain(t, got, "- hostname: workerdev")
	mustContain(t, got, "- hostname: workerstage")
	// Managed services use the :single variant + cheapest profile at tier 0 (dev).
	mustContain(t, got, "type: postgresql:single@18")
	mustContain(t, got, "profile: oltp-hobby")
	if strings.Contains(got, "mode: NON_HA") {
		t.Errorf("tier 0: managed entry should encode single via the type variant, not a legacy mode field:\n%s", got)
	}
	// Agent comment landed on apidev block.
	mustContain(t, got, "API dev — SSHFS-mounted source, hot reload.")
	// Project name includes tier suffix.
	mustContain(t, got, "name: synth-showcase-agent")
}

func TestYAMLEmitter_Tier5_HAProd(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	got, err := EmitImportYAML(plan, 5)
	if err != nil {
		t.Fatalf("EmitImportYAML: %v", err)
	}

	mustContain(t, got, "name: synth-showcase-ha-prod")
	mustContain(t, got, "corePackage: SERIOUS")
	// HA encoded in the type variant + production profile (tier 5).
	mustContain(t, got, "type: postgresql:ha@18")
	mustContain(t, got, "profile: oltp-staging")
	mustContain(t, got, "cpuMode: DEDICATED")
	mustContain(t, got, "minContainers: 2")
	// No dev slots at tier 5.
	if strings.Contains(got, "hostname: apidev") {
		t.Errorf("tier 5 must not emit dev services")
	}
	// Base hostnames appear (single services, not dev+stage pairs).
	mustContain(t, got, "- hostname: api")
	mustContain(t, got, "- hostname: app")
	mustContain(t, got, "- hostname: worker")
	// Object storage fields appear.
	mustContain(t, got, "objectStorageSize: 1")
	mustContain(t, got, "objectStoragePolicy: private")
}

func TestYAMLEmitter_NoSecret_NoPreprocessor(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	plan.Research.NeedsAppSecret = false
	plan.Research.AppSecretKey = ""

	got, err := EmitImportYAML(plan, 0)
	if err != nil {
		t.Fatalf("EmitImportYAML: %v", err)
	}
	if strings.HasPrefix(got, "#zeropsPreprocessor=on") {
		t.Errorf("preprocessor must not appear when NeedsAppSecret=false")
	}
	if strings.Contains(got, "APP_SECRET:") {
		t.Errorf("secret env var must not appear when NeedsAppSecret=false")
	}
}

func TestYAMLEmitter_MatchesFixture(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()

	// Fixture: check all six tiers deterministic. Regenerate goldens with
	// `go test -run TestYAMLEmitter_MatchesFixture -update`.
	for tierIndex := range 6 {
		got, err := EmitImportYAML(plan, tierIndex)
		if err != nil {
			t.Fatalf("tier %d: EmitImportYAML: %v", tierIndex, err)
		}
		goldenPath := filepath.Join("testdata", "fixtures", "synth-showcase",
			tierFolder(tierIndex)+".yaml")
		if os.Getenv("UPDATE_FIXTURES") == "1" {
			if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(goldenPath)
		if err != nil {
			t.Fatalf("tier %d: read golden %s: %v", tierIndex, goldenPath, err)
		}
		if got != string(want) {
			t.Errorf("tier %d: output mismatches golden %s", tierIndex, goldenPath)
		}
	}
}

func tierFolder(i int) string {
	t, _ := TierAt(i)
	return t.Folder
}

// TestEmitWorkspaceYAML_ShapeContract pins the workspace-shape
// invariants. These guarantees are what make provision safe: no
// buildFromGit (repos don't exist yet), no zeropsSetup, no project
// block (project-level env vars arrive via zerops_env after import),
// dev slots with startWithoutCode:true, stage slots without it.
func TestEmitWorkspaceYAML_ShapeContract(t *testing.T) {
	t.Parallel()
	got, err := EmitWorkspaceYAML(syntheticShowcasePlan())
	if err != nil {
		t.Fatalf("EmitWorkspaceYAML: %v", err)
	}
	// Absences — workspace shape rejects these fields.
	for _, forbidden := range []string{
		"project:",
		"buildFromGit:",
		"zeropsSetup:",
		"<@generateRandomString",
		"#zeropsPreprocessor",
	} {
		if strings.Contains(got, forbidden) {
			t.Errorf("workspace yaml contains forbidden %q:\n%s", forbidden, got)
		}
	}
	// Presences — every non-shared runtime codebase gets a dev+stage pair.
	// Dev slots have startWithoutCode:true; stage slots omit it.
	mustContain(t, got, "hostname: apidev")
	mustContain(t, got, "hostname: apistage")
	mustContain(t, got, "hostname: appdev")
	mustContain(t, got, "hostname: appstage")
	mustContain(t, got, "hostname: workerdev")
	mustContain(t, got, "hostname: workerstage")
	mustContain(t, got, "startWithoutCode: true")
	// Managed services still present with priority + the :single type variant
	// (workspace shape is dev-grade — no legacy mode field).
	mustContain(t, got, "hostname: db")
	mustContain(t, got, "type: postgresql:single@18")
	if strings.Contains(got, "mode: NON_HA") {
		t.Errorf("workspace yaml should encode single via the type variant, not a legacy mode field:\n%s", got)
	}
}

// TestEmitDeliverable_Tier5_MeilisearchNonHA — run-12 §Y3. Tier 5 applies HA to
// every managed service uniformly; meilisearch is not HA-capable on Zerops, so
// its type variant must downgrade to `:single` when SupportsHA=false (while
// HA-capable postgres stays `:ha`). Run-11 fact #8. HA-ness now lives in the
// type VARIANT, not a legacy `mode:` field.
func TestEmitDeliverable_Tier5_MeilisearchNonHA(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	plan.Services = append(plan.Services,
		Service{Hostname: "search", Type: "meilisearch@1.20", Kind: ServiceKindManaged, Priority: 10},
	)
	for i, svc := range plan.Services {
		plan.Services[i].SupportsHA = managedServiceSupportsHA(svc.Type)
	}
	got, err := EmitImportYAML(plan, 5)
	if err != nil {
		t.Fatalf("EmitImportYAML: %v", err)
	}
	mustContain(t, got, "type: postgresql:ha@18\n    priority: 10\n    profile: oltp-staging")
	mustContain(t, got, "type: meilisearch:single@1.20\n    priority: 10")
	if strings.Contains(got, "mode: NON_HA") || strings.Contains(got, "mode: HA") {
		t.Errorf("tier 5 managed entries should encode HA-ness via the type variant, not a legacy mode field:\n%s", got)
	}
}

// TestEmitDeliverableYAML_GlueRepoOverride pins the D6 buildFromGit override
// (OSS port flow Stage B). When Plan.GlueRepoURL is set, BOTH emit sites —
// the runtime buildFromGit site AND the ServiceKindUtility branch — emit the
// override verbatim, canonicalized via topology.CanonicalRepoURL (a trailing
// `.git` is stripped). When the override is EMPTY the framework path stays
// byte-identical: runtime falls back to RecipeAppRepoBase+slug+suffix and the
// utility branch emits NO buildFromGit (its pre-port shape).
func TestEmitDeliverableYAML_GlueRepoOverride(t *testing.T) {
	t.Parallel()

	// Use a non-RecipeAppRepoBase host so the "hardcoded form absent" assertion
	// is meaningful (the real curated glue org coincidentally shares the base
	// prefix; a distinct host keeps the negative assertion honest).
	const glue = "https://github.com/fxck/recipe-posthog.git"
	const canonical = "https://github.com/fxck/recipe-posthog"

	t.Run("set — runtime + utility both emit the canonicalized override", func(t *testing.T) {
		t.Parallel()
		plan := syntheticShowcasePlan()
		plan.GlueRepoURL = glue
		// Add a utility service so the ServiceKindUtility emit site is exercised.
		plan.Services = append(plan.Services,
			Service{Hostname: "mailpit", Type: "go@1", Kind: ServiceKindUtility})

		got, err := EmitImportYAML(plan, 5)
		if err != nil {
			t.Fatalf("EmitImportYAML: %v", err)
		}
		// The override is emitted, canonicalized (.git stripped).
		mustContain(t, got, "buildFromGit: "+canonical+"\n")
		// The hardcoded framework form is NOT emitted for runtimes.
		if strings.Contains(got, RecipeAppRepoBase) {
			t.Errorf("override set: runtime must not emit hardcoded RecipeAppRepoBase form:\n%s", got)
		}
		// The trailing `.git` form never reaches the output.
		if strings.Contains(got, glue) {
			t.Errorf("override set: must emit canonical (.git-stripped) form, not the verbatim .git URL:\n%s", got)
		}
		// The utility service now carries the override buildFromGit (it emitted
		// none before the port flow).
		mustContain(t, got, "- hostname: mailpit")
		mustContain(t, got, "zeropsSetup: app")
	})

	t.Run("empty — framework path byte-identical", func(t *testing.T) {
		t.Parallel()
		plan := syntheticShowcasePlan()
		plan.Services = append(plan.Services,
			Service{Hostname: "mailpit", Type: "go@1", Kind: ServiceKindUtility})

		got, err := EmitImportYAML(plan, 5)
		if err != nil {
			t.Fatalf("EmitImportYAML: %v", err)
		}
		// Runtime falls back to the hardcoded form.
		mustContain(t, got, "buildFromGit: "+RecipeAppRepoBase+"synth-showcase-api\n")
		// Utility branch emits NO buildFromGit when the override is empty.
		idx := strings.Index(got, "- hostname: mailpit")
		if idx < 0 {
			t.Fatalf("mailpit utility service missing:\n%s", got)
		}
		utilBlock := got[idx:]
		if next := strings.Index(utilBlock[1:], "- hostname:"); next >= 0 {
			utilBlock = utilBlock[:next+1]
		}
		if strings.Contains(utilBlock, "buildFromGit:") {
			t.Errorf("override empty: utility branch must not emit buildFromGit (framework path):\n%s", utilBlock)
		}
	})
}

// TestManagedServiceModeForTier — the single mode-resolution owner. Framework
// services (ModeMeasured=false) get the family-table fallback; port services
// (ModeMeasured=true) get their MEASURED mode verbatim, family table NOT
// consulted — so an unmeasured HA-capable dep is NOT force-promoted at tier 5.
func TestManagedServiceModeForTier(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		tierMode string
		svc      Service
		want     string
	}{
		// Framework path (ModeMeasured=false): run-12 §Y3 family-table behavior.
		{"framework HA tier, HA-capable family → HA", modeHA, Service{Type: "postgresql@18"}, modeHA},
		{"framework HA tier, non-HA family → downgrade NON_HA", modeHA, Service{Type: "meilisearch@1"}, modeNonHA},
		{"framework HA tier, explicit SupportsHA → HA", modeHA, Service{Type: "meilisearch@1", SupportsHA: true}, modeHA},
		{"framework NON_HA tier → NON_HA", modeNonHA, Service{Type: "postgresql@18"}, modeNonHA},
		// Port path (ModeMeasured=true): measured mode wins, table ignored.
		{"measured HA dep at HA tier → HA", modeHA, Service{Type: "clickhouse@25.3", SupportsHA: true, ModeMeasured: true}, modeHA},
		{"measured-NON_HA HA-capable dep at HA tier → NON_HA (NOT force-promoted)", modeHA, Service{Type: "postgresql@18", SupportsHA: false, ModeMeasured: true}, modeNonHA},
		{"measured dep at NON_HA tier → NON_HA", modeNonHA, Service{Type: "clickhouse@25.3", SupportsHA: true, ModeMeasured: true}, modeNonHA},
	}
	for _, tc := range cases {
		if got := ManagedServiceModeForTier(tc.tierMode, tc.svc); got != tc.want {
			t.Errorf("%s: ManagedServiceModeForTier = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestManagedServiceSupportsHA_FamilyTable pins the schema-derived HA-capability
// (delegates to schema.Schemas.SupportsHAVariant): a family is HA-capable iff the
// platform catalog ships a `:ha` variant. This replaced a hand-maintained switch
// that DRIFTED — it omitted mariadb (which ships `:ha`) and wrongly classified
// kafka as non-HA (it ships `:ha` too). meilisearch is genuinely single-only.
func TestManagedServiceSupportsHA_FamilyTable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want bool
	}{
		{"postgresql@18", true},
		{"mariadb@10.6", true}, // ships :ha — the drift the hardcoded list missed
		{"valkey@7.2", true},
		{"nats@2.12", true},
		{"kafka@3", true},         // ships :ha — the hardcoded list wrongly said false
		{"clickhouse@25.3", true}, // HA on Zerops — mandatory for PostHog ON CLUSTER DDL
		{"meilisearch@1.20", false},
		{"unknown@1", false},
	}
	for _, tc := range cases {
		if got := managedServiceSupportsHA(tc.in); got != tc.want {
			t.Errorf("managedServiceSupportsHA(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestWriteRuntimeDev_FallsBackToBareCodebaseName — run-12 §Y2. Brief
// instructs agents to record env/<N>/import-comments/<bare codebase
// name>; emitter previously looked up only by slot host (apidev /
// apistage), missing the bare key entirely. Now falls back when the
// slot-keyed entry is absent.
func TestWriteRuntimeDev_FallsBackToBareCodebaseName(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	plan.EnvComments = map[string]EnvComments{
		"0": {Service: map[string]string{
			"api": "api comment authored under bare codebase name",
		}},
	}
	got, err := EmitImportYAML(plan, 0)
	if err != nil {
		t.Fatalf("EmitImportYAML: %v", err)
	}
	mustContain(t, got, "api comment authored under bare codebase name")
	apidevIdx := strings.Index(got, "- hostname: apidev")
	commentIdx := strings.Index(got, "api comment authored under bare codebase name")
	if commentIdx < 0 || apidevIdx < 0 || commentIdx > apidevIdx {
		t.Errorf("comment did not render above apidev block: commentIdx=%d apidevIdx=%d", commentIdx, apidevIdx)
	}
}

// TestWriteRuntimeDev_SlotKeyTakesPrecedence — run-12 §Y2. When both a
// slot-keyed (`apidev`) and bare-keyed (`api`) entry exist, the slot
// hostname wins for the dev slot.
func TestWriteRuntimeDev_SlotKeyTakesPrecedence(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	plan.EnvComments = map[string]EnvComments{
		"0": {Service: map[string]string{
			"api":    "bare-name comment",
			"apidev": "slot-keyed comment",
		}},
	}
	got, err := EmitImportYAML(plan, 0)
	if err != nil {
		t.Fatalf("EmitImportYAML: %v", err)
	}
	mustContain(t, got, "slot-keyed comment")
}

// TestWriteComment_StripsLeadingHashFromAuthoredFragment — run-12 §Y1.
// Agents author fragment bodies with leading `# ` per line; writeComment
// then re-prefixed producing `# # …`. 272 lines disfigured per recipe
// before the fix.
func TestWriteComment_StripsLeadingHashFromAuthoredFragment(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	writeComment(&b, "# This is a comment line\n# Second line", "  ")
	got := b.String()
	if strings.Contains(got, "# # ") {
		t.Errorf("doubled-prefix found in:\n%s", got)
	}
	mustContain(t, got, "  # This is a comment line")
	mustContain(t, got, "  # Second line")
}

// TestEmitDeliverableYAML_Tier0_SuppressesY2FallbackDuplicateOnStageSlot
// — run-13 §Y2D. Tier 0 dev-pair runtime services with a bare-codebase
// EnvComments entry (Y2 fallback) used to render the SAME comment
// above BOTH dev and stage slots. Y2D suppresses the stage slot
// rendering when the dev slot already emitted the same fallback text.
func TestEmitDeliverableYAML_Tier0_SuppressesY2FallbackDuplicateOnStageSlot(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	plan.EnvComments = map[string]EnvComments{
		"0": {Service: map[string]string{
			"api": "Two slots — apidev and apistage — share one source tree.",
		}},
	}
	got, err := EmitDeliverableYAML(plan, 0)
	if err != nil {
		t.Fatalf("EmitDeliverableYAML: %v", err)
	}
	occurrences := strings.Count(got, "share one source tree")
	if occurrences != 1 {
		t.Errorf("comment rendered %d times; expected 1 (Y2D dedup):\n%s", occurrences, got)
	}
}

// TestEmitDeliverableYAML_Tier0_DistinctSlotKeysBothEmit — run-13 §Y2D
// asymmetry guard. When the agent records DISTINCT comments under
// `apidev` + `apistage` keys, both render — Y2D only suppresses the
// fallback duplicate, not deliberately-distinct slot-keyed comments.
func TestEmitDeliverableYAML_Tier0_DistinctSlotKeysBothEmit(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	plan.EnvComments = map[string]EnvComments{
		"0": {Service: map[string]string{
			"apidev":   "Dev slot — hot iteration target.",
			"apistage": "Stage slot — stable demo target.",
		}},
	}
	got, err := EmitDeliverableYAML(plan, 0)
	if err != nil {
		t.Fatalf("EmitDeliverableYAML: %v", err)
	}
	mustContain(t, got, "Dev slot — hot iteration target")
	mustContain(t, got, "Stage slot — stable demo target")
}

// TestEmitDeliverableYAML_DevPairTier_StageSlotSuppressedToAvoidDuplicate
// — run-23 F-19 deferral guard. F-19 (stamping `import-comments/<bare>`
// above EVERY slot at dev-pair tiers) was deferred because the run-13
// §Y2D suppression at writeRuntimeStage:303-317 deliberately drops the
// stage slot's bare-codebase fallback when it would render byte-
// identical prose to what the dev slot already emitted. This test pins
// the Y2D contract so a future refactor can't accidentally undo it: at
// a dev-pair tier with a bare-codebase comment in EnvComments, the
// stage slot must NOT receive a duplicate comment block. See FIX_SPEC.md
// F-19 DEFERRED for the prerequisite (fragment-id model expansion to
// accept `env/<N>/import-comments/<slot>` per-slot ids).
func TestEmitDeliverableYAML_DevPairTier_StageSlotSuppressedToAvoidDuplicate(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	// Bare-codebase comment present (Y2 fallback shape). At dev-pair
	// tier 0 this would render above apidev; without Y2D suppression it
	// would also render above apistage, producing a visual duplicate.
	plan.EnvComments = map[string]EnvComments{
		"0": {Service: map[string]string{
			"api": "Two slots — apidev and apistage — share one source tree.",
		}},
	}
	got, err := EmitDeliverableYAML(plan, 0)
	if err != nil {
		t.Fatalf("EmitDeliverableYAML: %v", err)
	}
	occurrences := strings.Count(got, "share one source tree")
	if occurrences != 1 {
		t.Errorf("Y2D suppression broken — bare-codebase comment rendered %d times above the dev-pair runtime; expected 1 (stage slot suppressed):\n%s", occurrences, got)
	}
}

// TestWriteComment_BareProseUnchanged — run-12 §Y1. Plain prose without
// a leading `#` still gets prefixed once; no doubled-prefix regression.
func TestWriteComment_BareProseUnchanged(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	writeComment(&b, "Plain prose with no prefix", "  ")
	got := b.String()
	mustContain(t, got, "  # Plain prose with no prefix")
	if strings.Contains(got, "# # ") {
		t.Errorf("doubled-prefix found in:\n%s", got)
	}
}

func mustContain(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("output missing substring:\n  want: %q", want)
	}
}

func mustNotContain(t *testing.T, got, unwanted string) {
	t.Helper()
	if strings.Contains(got, unwanted) {
		t.Errorf("output contains forbidden substring:\n  unwanted: %q", unwanted)
	}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// TestEmitDeliverableYAML_DeclaresURLConstantsInProjectEnvVars — run-22
// R3-RC-3 part B. The emitter writes Plan.ProjectEnvVars[envKey(tier)]
// into the project.envVariables block. Pre-fix this contract was already
// honored end-to-end; the regression class targeted by R3 was the agent
// not populating ProjectEnvVars. Pin the existing emit so future
// refactors don't drop it.
func TestEmitDeliverableYAML_DeclaresURLConstantsInProjectEnvVars(t *testing.T) {
	t.Parallel()
	plan := syntheticShowcasePlan()
	plan.ProjectEnvVars = map[string]map[string]string{
		"0": {
			"API_URL":          "https://apistage-${zeropsSubdomainHost}-3000.prg1.zerops.app",
			"FRONTEND_URL":     "https://appstage-${zeropsSubdomainHost}.prg1.zerops.app",
			"DEV_API_URL":      "https://apidev-${zeropsSubdomainHost}-3000.prg1.zerops.app",
			"DEV_FRONTEND_URL": "https://appdev-${zeropsSubdomainHost}-5173.prg1.zerops.app",
		},
	}
	got, err := EmitDeliverableYAML(plan, 0)
	if err != nil {
		t.Fatalf("EmitDeliverableYAML: %v", err)
	}
	for _, want := range []string{
		"API_URL: https://apistage-${zeropsSubdomainHost}-3000.prg1.zerops.app",
		"FRONTEND_URL: https://appstage-${zeropsSubdomainHost}.prg1.zerops.app",
		"DEV_API_URL: https://apidev-${zeropsSubdomainHost}-3000.prg1.zerops.app",
		"DEV_FRONTEND_URL: https://appdev-${zeropsSubdomainHost}-5173.prg1.zerops.app",
	} {
		mustContain(t, got, want)
	}
}

// TestEmitDeliverableYAML_RewritesURLsForSingleSlotTiers — run-22 R3-RC-3.
// Tiers 2-5 collapse the dev/stage slot pair into a bare hostname
// (`api`/`app`/`worker`). The emitter must rewrite slot-named hostnames
// in URL values for these tiers and drop the DEV_* keys (single-slot
// tiers don't have a dev runtime). Preserves `${zeropsSubdomainHost}`.
func TestEmitDeliverableYAML_RewritesURLsForSingleSlotTiers(t *testing.T) {
	t.Parallel()
	plan := syntheticShowcasePlan()
	plan.ProjectEnvVars = map[string]map[string]string{
		"4": {
			"API_URL":          "https://apistage-${zeropsSubdomainHost}-3000.prg1.zerops.app",
			"FRONTEND_URL":     "https://appstage-${zeropsSubdomainHost}.prg1.zerops.app",
			"DEV_API_URL":      "https://apidev-${zeropsSubdomainHost}-3000.prg1.zerops.app",
			"DEV_FRONTEND_URL": "https://appdev-${zeropsSubdomainHost}-5173.prg1.zerops.app",
		},
	}
	got, err := EmitDeliverableYAML(plan, 4)
	if err != nil {
		t.Fatalf("EmitDeliverableYAML: %v", err)
	}
	// Single-slot tier rewrites apistage- → api-, appstage- → app-.
	mustContain(t, got, "API_URL: https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app")
	mustContain(t, got, "FRONTEND_URL: https://app-${zeropsSubdomainHost}.prg1.zerops.app")
	// Slot-named hostnames must NOT survive on tier 4.
	mustNotContain(t, got, "apistage-")
	mustNotContain(t, got, "appstage-")
	// DEV_* drops on single-slot tiers.
	mustNotContain(t, got, "DEV_API_URL")
	mustNotContain(t, got, "DEV_FRONTEND_URL")
	mustNotContain(t, got, "apidev-")
	mustNotContain(t, got, "appdev-")
	// `${zeropsSubdomainHost}` literal must survive untouched.
	mustContain(t, got, "${zeropsSubdomainHost}")
}

// TestEmitDeliverableYAML_PreservesAppSecretAlongsideURLConstants —
// run-22 R3-RC-3 regression guard. The single-slot rewrite must not
// accidentally swallow APP_SECRET or other unrelated project envs.
func TestEmitDeliverableYAML_PreservesAppSecretAlongsideURLConstants(t *testing.T) {
	t.Parallel()
	plan := syntheticShowcasePlan()
	plan.ProjectEnvVars = map[string]map[string]string{
		"4": {
			"API_URL": "https://apistage-${zeropsSubdomainHost}-3000.prg1.zerops.app",
		},
	}
	got, err := EmitDeliverableYAML(plan, 4)
	if err != nil {
		t.Fatalf("EmitDeliverableYAML: %v", err)
	}
	mustContain(t, got, "APP_SECRET: <@generateRandomString(<32>)>")
	mustContain(t, got, "API_URL: https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app")
}

// TestEmitDeliverableYAML_KeepsDevPairURLsForTiers0And1 — run-22 R3-RC-3
// guard. Tiers 0 and 1 are dev-pair tiers (RunsDevContainer=true) — the
// single-slot rewrite must NOT fire there; the dev/stage slot URLs are
// the load-bearing values.
func TestEmitDeliverableYAML_KeepsDevPairURLsForTiers0And1(t *testing.T) {
	t.Parallel()
	plan := syntheticShowcasePlan()
	plan.ProjectEnvVars = map[string]map[string]string{
		"1": {
			"DEV_API_URL": "https://apidev-${zeropsSubdomainHost}-3000.prg1.zerops.app",
			"API_URL":     "https://apistage-${zeropsSubdomainHost}-3000.prg1.zerops.app",
		},
	}
	got, err := EmitDeliverableYAML(plan, 1)
	if err != nil {
		t.Fatalf("EmitDeliverableYAML: %v", err)
	}
	mustContain(t, got, "DEV_API_URL: https://apidev-${zeropsSubdomainHost}-3000.prg1.zerops.app")
	mustContain(t, got, "API_URL: https://apistage-${zeropsSubdomainHost}-3000.prg1.zerops.app")
}

// TestEmitDeliverableYAML_SingleSlotSeedsFromDevPairWhenTierEmpty — F-42.
// When a single-slot tier (2-5) has no projectEnvVars entries of its own,
// the engine seeds from the dev-pair baseline (tier 0, falling back to
// tier 1) and passes through rewriteURLsForSingleSlot. Without this seed,
// agents that record URL constants only at tier 0/1 (per the established
// provision teaching at content/phase_entry/provision.md) ship single-
// slot tier yamls with missing URL keys and break the SPA build-time
// bake at porter deploy. Surfaced by run-29 dogfood evidence.
func TestEmitDeliverableYAML_SingleSlotSeedsFromDevPairWhenTierEmpty(t *testing.T) {
	t.Parallel()
	plan := syntheticShowcasePlan()
	plan.ProjectEnvVars = map[string]map[string]string{
		"0": {
			"API_URL":          "https://apistage-${zeropsSubdomainHost}-3000.prg1.zerops.app",
			"FRONTEND_URL":     "https://appstage-${zeropsSubdomainHost}.prg1.zerops.app",
			"DEV_API_URL":      "https://apidev-${zeropsSubdomainHost}-3000.prg1.zerops.app",
			"DEV_FRONTEND_URL": "https://appdev-${zeropsSubdomainHost}-5173.prg1.zerops.app",
		},
		// Tiers 1-5 deliberately not populated — engine seeds 2-5 from 0.
	}
	for _, tierIndex := range []int{2, 3, 4, 5} {
		got, err := EmitDeliverableYAML(plan, tierIndex)
		if err != nil {
			t.Fatalf("EmitDeliverableYAML(tier=%d): %v", tierIndex, err)
		}
		mustContain(t, got, "API_URL: https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app")
		mustContain(t, got, "FRONTEND_URL: https://app-${zeropsSubdomainHost}.prg1.zerops.app")
		mustNotContain(t, got, "DEV_API_URL")
		mustNotContain(t, got, "DEV_FRONTEND_URL")
		mustNotContain(t, got, "apistage-")
		mustNotContain(t, got, "appstage-")
		mustNotContain(t, got, "apidev-")
		mustNotContain(t, got, "appdev-")
	}
}

// TestEmitDeliverableYAML_SingleSlotPrefersOwnTierWhenSet — F-42 guard.
// When a single-slot tier has its own entries, the seed-from-dev-pair
// fallback must NOT fire — author intent at the per-tier index wins.
func TestEmitDeliverableYAML_SingleSlotPrefersOwnTierWhenSet(t *testing.T) {
	t.Parallel()
	plan := syntheticShowcasePlan()
	plan.ProjectEnvVars = map[string]map[string]string{
		"0": {
			"API_URL": "https://apistage-${zeropsSubdomainHost}-3000.prg1.zerops.app",
		},
		"4": {
			"API_URL": "https://api-custom-${zeropsSubdomainHost}-3000.prg1.zerops.app",
		},
	}
	got, err := EmitDeliverableYAML(plan, 4)
	if err != nil {
		t.Fatalf("EmitDeliverableYAML: %v", err)
	}
	mustContain(t, got, "API_URL: https://api-custom-${zeropsSubdomainHost}-3000.prg1.zerops.app")
	mustNotContain(t, got, "https://apistage-")
}

// TestEmitDeliverableYAML_SingleSlotSeedsFromTier1WhenTier0Empty —
// F-42 fallback. Both dev-pair tiers (0 and 1) carry the same shape per
// the provision teaching, but if an agent recorded only tier 1, single-
// slot tiers still seed correctly.
func TestEmitDeliverableYAML_SingleSlotSeedsFromTier1WhenTier0Empty(t *testing.T) {
	t.Parallel()
	plan := syntheticShowcasePlan()
	plan.ProjectEnvVars = map[string]map[string]string{
		"1": {
			"API_URL":      "https://apistage-${zeropsSubdomainHost}-3000.prg1.zerops.app",
			"FRONTEND_URL": "https://appstage-${zeropsSubdomainHost}.prg1.zerops.app",
		},
	}
	got, err := EmitDeliverableYAML(plan, 3)
	if err != nil {
		t.Fatalf("EmitDeliverableYAML: %v", err)
	}
	mustContain(t, got, "API_URL: https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app")
	mustContain(t, got, "FRONTEND_URL: https://app-${zeropsSubdomainHost}.prg1.zerops.app")
}

// TestEmitDeliverableYAML_DevPairTierEmpty_NoSeed — F-42 guard. Tier 1
// is a dev-pair tier (RunsDevContainer=true) — it's the SOURCE of the
// seed, never a sink. An empty tier 1 stays empty (no inherited URLs).
func TestEmitDeliverableYAML_DevPairTierEmpty_NoSeed(t *testing.T) {
	t.Parallel()
	plan := syntheticShowcasePlan()
	plan.ProjectEnvVars = map[string]map[string]string{
		"0": {
			"API_URL": "https://apistage-${zeropsSubdomainHost}-3000.prg1.zerops.app",
		},
	}
	got, err := EmitDeliverableYAML(plan, 1)
	if err != nil {
		t.Fatalf("EmitDeliverableYAML: %v", err)
	}
	mustNotContain(t, got, "API_URL: https://apistage-")
}
