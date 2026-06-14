package ops_test

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/topology"
)

// minimalLaunchInputs returns valid inputs for a standard nodejs runtime
// + postgres managed dep, used as the baseline across happy-path tests.
func minimalLaunchInputs() ops.LaunchBundleInputs {
	return ops.LaunchBundleInputs{
		SourceProjectID:   "source123",
		TargetProjectName: "myapp-prod",
		Runtimes: []ops.LaunchRuntimeInput{
			{
				ProdHostname: "app",
				ServiceType:  "nodejs@22",
				SetupName:    "prod",
				RepoURL:      "git@github.com:user/myapp.git",
				ZeropsYAMLBody: `zerops:
  - setup: prod
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
        - npm run build
    run:
      base: nodejs@22
      start: node dist/server.js
`,
				GitCommitSHA: "abc123def456",
			},
		},
		ProjectEnvs: []ops.ProjectEnvVar{
			{Key: "LOG_LEVEL", Value: "info"},
			{Key: "NODE_ENV", Value: "production"},
		},
		ManagedServices: []ops.ManagedServiceEntry{
			{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA"},
		},
	}
}

// classifyAllPlain buckets every env in the input as plain-config — used
// when classification isn't the test's concern.
func classifyAllPlain(envs []ops.ProjectEnvVar) map[string]topology.SecretClassification {
	out := make(map[string]topology.SecretClassification, len(envs))
	for _, e := range envs {
		out[e.Key] = topology.SecretClassPlainConfig
	}
	return out
}

// TestBuildLaunchBundle_HappyPath covers the basic compose flow.
func TestBuildLaunchBundle_HappyPath(t *testing.T) {
	t.Parallel()
	inputs := minimalLaunchInputs()
	cls := classifyAllPlain(inputs.ProjectEnvs)

	bundle, err := ops.BuildLaunchBundle(inputs, cls)
	if err != nil {
		t.Fatalf("BuildLaunchBundle: %v", err)
	}
	if bundle == nil {
		t.Fatal("nil bundle")
	}
	if bundle.ImportYAML == "" {
		t.Fatal("empty ImportYAML")
	}
	if len(bundle.Errors) != 0 {
		t.Fatalf("unexpected schema errors: %v", bundle.Errors)
	}
	if bundle.TargetProjectName != "myapp-prod" {
		t.Errorf("TargetProjectName: %q", bundle.TargetProjectName)
	}
	if bundle.SourceProjectID != "source123" {
		t.Errorf("SourceProjectID: %q", bundle.SourceProjectID)
	}
}

// TestBuildLaunchBundle_PromotesManagedToHA verifies P-PROD-1 — managed
// services HA-promote by default. HA is encoded in the type VARIANT
// (`postgresql:ha@16`), the authoritative form, NOT a sibling `mode:` field;
// the production tier defaults to oltp-staging (operator escalates higher).
func TestBuildLaunchBundle_PromotesManagedToHA(t *testing.T) {
	t.Parallel()
	inputs := minimalLaunchInputs()
	cls := classifyAllPlain(inputs.ProjectEnvs)

	bundle, err := ops.BuildLaunchBundle(inputs, cls)
	if err != nil {
		t.Fatalf("BuildLaunchBundle: %v", err)
	}

	doc := parseImportYAML(t, bundle.ImportYAML)
	managed := findService(t, doc, "db")
	if managed["type"] != "postgresql:ha@16" {
		t.Errorf("expected db type postgresql:ha@16 (HA via variant), got %v", managed["type"])
	}
	if _, ok := managed["mode"]; ok {
		t.Errorf("managed entry should omit legacy mode (variant is authoritative), got %v", managed["mode"])
	}
	if managed["profile"] != "oltp-staging" {
		t.Errorf("expected db production profile oltp-staging, got %v", managed["profile"])
	}
}

// TestBuildLaunchBundle_KeepNonHAOptOut verifies KeepNonHA respect: the dep
// stays single via the `:single` variant (not a `mode: NON_HA` field).
func TestBuildLaunchBundle_KeepNonHAOptOut(t *testing.T) {
	t.Parallel()
	inputs := minimalLaunchInputs()
	inputs.KeepNonHA = []string{"db"}
	cls := classifyAllPlain(inputs.ProjectEnvs)

	bundle, err := ops.BuildLaunchBundle(inputs, cls)
	if err != nil {
		t.Fatalf("BuildLaunchBundle: %v", err)
	}
	doc := parseImportYAML(t, bundle.ImportYAML)
	managed := findService(t, doc, "db")
	if managed["type"] != "postgresql:single@16" {
		t.Errorf("expected db type postgresql:single@16 (kept single via variant), got %v", managed["type"])
	}
	if _, ok := managed["mode"]; ok {
		t.Errorf("managed entry should omit legacy mode (variant is authoritative), got %v", managed["mode"])
	}
}

// TestBuildLaunchBundle_HAIncapableKeptSingle pins the launch break fix: a
// managed dep whose type has no `:ha` variant (e.g. meilisearch ships only
// `:single`) MUST stay single-node and surface a reason-bearing warning — the
// pre-fix composer blindly promoted every managed dep, emitting a fabricated
// `meilisearch:ha` the platform import rejects.
func TestBuildLaunchBundle_HAIncapableKeptSingle(t *testing.T) {
	t.Parallel()
	inputs := minimalLaunchInputs()
	inputs.ManagedServices = append(inputs.ManagedServices,
		ops.ManagedServiceEntry{Hostname: "search", Type: "meilisearch@1.20"})
	inputs.HAIncapable = []string{"search"}
	cls := classifyAllPlain(inputs.ProjectEnvs)

	bundle, err := ops.BuildLaunchBundle(inputs, cls)
	if err != nil {
		t.Fatalf("BuildLaunchBundle: %v", err)
	}
	doc := parseImportYAML(t, bundle.ImportYAML)

	search := findService(t, doc, "search")
	if search["type"] != "meilisearch:single@1.20" {
		t.Errorf("HA-incapable meilisearch must stay :single, got %v", search["type"])
	}
	if _, ok := search["mode"]; ok {
		t.Errorf("managed entry should omit legacy mode, got %v", search["mode"])
	}
	// The HA-capable db is unaffected — still promoted.
	db := findService(t, doc, "db")
	if db["type"] != "postgresql:ha@16" {
		t.Errorf("HA-capable db should still promote to :ha, got %v", db["type"])
	}
	// A reason-bearing warning must surface (distinct from a KeepNonHA opt-out).
	found := false
	for _, w := range bundle.Warnings {
		if strings.Contains(w, "search") && strings.Contains(w, "no HA variant") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an HA-incapable warning for search, got %v", bundle.Warnings)
	}
}

// TestBuildLaunchBundle_StripsSubdomainAccess verifies P-PROD-2 — runtime
// entries never carry enableSubdomainAccess regardless of source.
func TestBuildLaunchBundle_StripsSubdomainAccess(t *testing.T) {
	t.Parallel()
	inputs := minimalLaunchInputs()
	cls := classifyAllPlain(inputs.ProjectEnvs)

	bundle, err := ops.BuildLaunchBundle(inputs, cls)
	if err != nil {
		t.Fatalf("BuildLaunchBundle: %v", err)
	}
	doc := parseImportYAML(t, bundle.ImportYAML)
	runtime := findService(t, doc, "app")
	if _, ok := runtime["enableSubdomainAccess"]; ok {
		t.Error("expected enableSubdomainAccess absent on runtime entry")
	}
}

// TestBuildLaunchBundle_RuntimeMinContainersDefault verifies default 2.
func TestBuildLaunchBundle_RuntimeMinContainersDefault(t *testing.T) {
	t.Parallel()
	inputs := minimalLaunchInputs()
	cls := classifyAllPlain(inputs.ProjectEnvs)

	bundle, err := ops.BuildLaunchBundle(inputs, cls)
	if err != nil {
		t.Fatalf("BuildLaunchBundle: %v", err)
	}
	doc := parseImportYAML(t, bundle.ImportYAML)
	runtime := findService(t, doc, "app")
	mc, ok := runtime["minContainers"]
	if !ok {
		t.Fatal("minContainers missing on runtime")
	}
	if mc != 2 {
		t.Errorf("expected minContainers 2, got %v", mc)
	}
}

// TestBuildLaunchBundle_RuntimeMinContainersOverride verifies caller override.
func TestBuildLaunchBundle_RuntimeMinContainersOverride(t *testing.T) {
	t.Parallel()
	inputs := minimalLaunchInputs()
	inputs.Runtimes[0].MinContainers = 5
	cls := classifyAllPlain(inputs.ProjectEnvs)

	bundle, err := ops.BuildLaunchBundle(inputs, cls)
	if err != nil {
		t.Fatalf("BuildLaunchBundle: %v", err)
	}
	doc := parseImportYAML(t, bundle.ImportYAML)
	runtime := findService(t, doc, "app")
	if runtime["minContainers"] != 5 {
		t.Errorf("expected minContainers 5, got %v", runtime["minContainers"])
	}
}

// TestBuildLaunchBundle_RuntimeCPUModeDedicated verifies cpuMode DEDICATED
// on runtime verticalAutoscaling.
func TestBuildLaunchBundle_RuntimeCPUModeDedicated(t *testing.T) {
	t.Parallel()
	inputs := minimalLaunchInputs()
	cls := classifyAllPlain(inputs.ProjectEnvs)

	bundle, err := ops.BuildLaunchBundle(inputs, cls)
	if err != nil {
		t.Fatalf("BuildLaunchBundle: %v", err)
	}
	doc := parseImportYAML(t, bundle.ImportYAML)
	runtime := findService(t, doc, "app")
	autoscaling, ok := runtime["verticalAutoscaling"].(map[string]any)
	if !ok {
		t.Fatal("expected verticalAutoscaling map on runtime")
	}
	if autoscaling["cpuMode"] != "DEDICATED" {
		t.Errorf("expected cpuMode DEDICATED, got %v", autoscaling["cpuMode"])
	}
}

// TestBuildLaunchBundle_TagsIncludeProdMarkers verifies the canonical
// tag set: env:prod, source-project:<id>, managed-by:zcp-launch.
func TestBuildLaunchBundle_TagsIncludeProdMarkers(t *testing.T) {
	t.Parallel()
	inputs := minimalLaunchInputs()
	inputs.AdditionalTags = []string{"team:platform", "owner:karel"}
	cls := classifyAllPlain(inputs.ProjectEnvs)

	bundle, err := ops.BuildLaunchBundle(inputs, cls)
	if err != nil {
		t.Fatalf("BuildLaunchBundle: %v", err)
	}
	doc := parseImportYAML(t, bundle.ImportYAML)
	project, ok := doc["project"].(map[string]any)
	if !ok {
		t.Fatal("expected project map")
	}
	tagsRaw, ok := project["tags"].([]any)
	if !ok {
		t.Fatalf("expected tags array, got %T", project["tags"])
	}
	wantTags := []string{
		"env:prod",
		"source-project:source123",
		"managed-by:zcp-launch",
		"team:platform",
		"owner:karel",
	}
	if len(tagsRaw) != len(wantTags) {
		t.Fatalf("tag count: got %d want %d (got %v)", len(tagsRaw), len(wantTags), tagsRaw)
	}
	for i, want := range wantTags {
		if tagsRaw[i] != want {
			t.Errorf("tags[%d]: got %v want %q", i, tagsRaw[i], want)
		}
	}
}

// TestBuildLaunchBundle_DedupesTags verifies duplicate tags are dropped.
func TestBuildLaunchBundle_DedupesTags(t *testing.T) {
	t.Parallel()
	inputs := minimalLaunchInputs()
	inputs.AdditionalTags = []string{"env:prod", "team:platform", "env:prod"}
	cls := classifyAllPlain(inputs.ProjectEnvs)

	bundle, err := ops.BuildLaunchBundle(inputs, cls)
	if err != nil {
		t.Fatalf("BuildLaunchBundle: %v", err)
	}
	doc := parseImportYAML(t, bundle.ImportYAML)
	project, _ := doc["project"].(map[string]any)
	tagsRaw, _ := project["tags"].([]any)
	count := 0
	for _, t := range tagsRaw {
		if t == "env:prod" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 env:prod tag, got %d in %v", count, tagsRaw)
	}
}

// TestBuildLaunchBundle_ClassifiesEnvs verifies project envs flow through
// the same composeProjectEnvVariables machinery as export.
func TestBuildLaunchBundle_ClassifiesEnvs(t *testing.T) {
	t.Parallel()
	inputs := minimalLaunchInputs()
	inputs.ProjectEnvs = []ops.ProjectEnvVar{
		{Key: "LOG_LEVEL", Value: "info"},
		{Key: "JWT_SECRET", Value: "real-jwt-secret"},
		{Key: "STRIPE_KEY", Value: "sk_live_xxx"},
		{Key: "DB_HOST", Value: "${db_hostname}"},
	}
	cls := map[string]topology.SecretClassification{
		"LOG_LEVEL":  topology.SecretClassPlainConfig,
		"JWT_SECRET": topology.SecretClassAutoSecret,
		"STRIPE_KEY": topology.SecretClassExternalSecret,
		"DB_HOST":    topology.SecretClassInfrastructure,
	}

	bundle, err := ops.BuildLaunchBundle(inputs, cls)
	if err != nil {
		t.Fatalf("BuildLaunchBundle: %v", err)
	}

	doc := parseImportYAML(t, bundle.ImportYAML)
	project, _ := doc["project"].(map[string]any)
	envs, _ := project["envVariables"].(map[string]any)

	if envs["LOG_LEVEL"] != "info" {
		t.Errorf("plain-config: got %v", envs["LOG_LEVEL"])
	}
	if envs["JWT_SECRET"] != "<@generateRandomString(<32>)>" {
		t.Errorf("auto-secret: got %v", envs["JWT_SECRET"])
	}
	if envs["STRIPE_KEY"] != "REPLACE_ME" {
		t.Errorf("external-secret: got %v want literal REPLACE_ME (platform rejects JSON-array pickRandom syntax)", envs["STRIPE_KEY"])
	}
	if _, ok := envs["DB_HOST"]; ok {
		t.Error("expected DB_HOST DROPPED as infrastructure")
	}
}

// TestBuildLaunchBundle_SourceSnapshotDeterministic verifies same inputs
// produce same SourceSnapshot hashes.
func TestBuildLaunchBundle_SourceSnapshotDeterministic(t *testing.T) {
	t.Parallel()
	inputs := minimalLaunchInputs()
	cls := classifyAllPlain(inputs.ProjectEnvs)

	b1, _ := ops.BuildLaunchBundle(inputs, cls)
	b2, _ := ops.BuildLaunchBundle(inputs, cls)

	if b1.SourceSnapshot != b2.SourceSnapshot {
		t.Errorf("SourceSnapshot drift between identical inputs:\nb1=%+v\nb2=%+v", b1.SourceSnapshot, b2.SourceSnapshot)
	}
}

// TestBuildLaunchBundle_SourceSnapshotDetectsDrift verifies snapshot
// differs when any captured field changes — the immutability guard
// substrate.
func TestBuildLaunchBundle_SourceSnapshotDetectsDrift(t *testing.T) {
	t.Parallel()
	cls := classifyAllPlain(minimalLaunchInputs().ProjectEnvs)

	base, _ := ops.BuildLaunchBundle(minimalLaunchInputs(), cls)

	cases := []struct {
		name string
		mut  func(i *ops.LaunchBundleInputs)
	}{
		{"git SHA changed", func(i *ops.LaunchBundleInputs) { i.Runtimes[0].GitCommitSHA = "different-sha" }},
		{"zerops yaml body changed", func(i *ops.LaunchBundleInputs) {
			i.Runtimes[0].ZeropsYAMLBody += "\n# trailing comment\n"
		}},
		{"project env added", func(i *ops.LaunchBundleInputs) {
			i.ProjectEnvs = append(i.ProjectEnvs, ops.ProjectEnvVar{Key: "NEW", Value: "x"})
		}},
		{"managed service added", func(i *ops.LaunchBundleInputs) {
			i.ManagedServices = append(i.ManagedServices, ops.ManagedServiceEntry{Hostname: "cache", Type: "valkey@7", Mode: "NON_HA"})
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			inputs := minimalLaunchInputs()
			tc.mut(&inputs)
			drifted, _ := ops.BuildLaunchBundle(inputs, cls)
			if drifted.SourceSnapshot == base.SourceSnapshot {
				t.Errorf("expected snapshot drift after %s, got identical snapshot", tc.name)
			}
		})
	}
}

// TestBuildLaunchBundle_MissingRequiredFields verifies validation.
func TestBuildLaunchBundle_MissingRequiredFields(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		mut  func(i *ops.LaunchBundleInputs)
		want string
	}{
		{"missing TargetProjectName", func(i *ops.LaunchBundleInputs) { i.TargetProjectName = "" }, "TargetProjectName"},
		{"empty Runtimes", func(i *ops.LaunchBundleInputs) { i.Runtimes = nil }, "runtime"},
		{"missing ProdHostname", func(i *ops.LaunchBundleInputs) { i.Runtimes[0].ProdHostname = "" }, "ProdHostname"},
		{"missing ServiceType", func(i *ops.LaunchBundleInputs) { i.Runtimes[0].ServiceType = "" }, "ServiceType"},
		{"missing RepoURL", func(i *ops.LaunchBundleInputs) { i.Runtimes[0].RepoURL = "" }, "RepoURL"},
		{"missing ZeropsYAMLBody", func(i *ops.LaunchBundleInputs) { i.Runtimes[0].ZeropsYAMLBody = "" }, "ZeropsYAMLBody"},
		{"missing SourceProjectID", func(i *ops.LaunchBundleInputs) { i.SourceProjectID = "" }, "SourceProjectID"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			inputs := minimalLaunchInputs()
			tc.mut(&inputs)
			cls := classifyAllPlain(inputs.ProjectEnvs)
			_, err := ops.BuildLaunchBundle(inputs, cls)
			if err == nil {
				t.Fatalf("expected error mentioning %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q should mention %q", err, tc.want)
			}
		})
	}
}

// TestBuildLaunchBundle_RejectsMissingSetupBlock verifies the setup name
// must exist in the zerops yaml body — same gate as export.
func TestBuildLaunchBundle_RejectsMissingSetupBlock(t *testing.T) {
	t.Parallel()
	inputs := minimalLaunchInputs()
	inputs.Runtimes[0].SetupName = "prod"
	inputs.Runtimes[0].ZeropsYAMLBody = `zerops:
  - setup: dev
    build:
      base: nodejs@22
`
	cls := classifyAllPlain(inputs.ProjectEnvs)
	_, err := ops.BuildLaunchBundle(inputs, cls)
	if err == nil {
		t.Fatal("expected error when setup: prod not in yaml body")
	}
}

// TestBuildLaunchBundle_OmitsUserRolesAlways verifies project.userRoles
// is NOT emitted in import yaml. A.10 finding (verified 2026-05-11):
// PostClientProjectImport silently drops project.userRoles, so the
// bundle composer no longer emits it. Role assignment happens via
// ProjectAdminClient.GrantSelfRole AFTER create — separate API call.
func TestBuildLaunchBundle_OmitsUserRolesAlways(t *testing.T) {
	t.Parallel()
	inputs := minimalLaunchInputs()
	cls := classifyAllPlain(inputs.ProjectEnvs)

	bundle, err := ops.BuildLaunchBundle(inputs, cls)
	if err != nil {
		t.Fatalf("BuildLaunchBundle: %v", err)
	}
	doc := parseImportYAML(t, bundle.ImportYAML)
	project, _ := doc["project"].(map[string]any)
	if _, ok := project["userRoles"]; ok {
		t.Error("expected project.userRoles absent (platform silently drops; use GrantSelfRole post-create)")
	}
}

// TestBuildLaunchBundle_SetupNameDefaultsToProd verifies the default
// setup name. Pipeline-first dropped zeropsSetup from the import YAML
// (the import API rejects it without buildFromGit), so the observable
// is the normalized runtime input — the identity the launched state's
// RuntimeProds (and the pipeline wiring) read after compose.
func TestBuildLaunchBundle_SetupNameDefaultsToProd(t *testing.T) {
	t.Parallel()
	inputs := minimalLaunchInputs()
	inputs.Runtimes[0].SetupName = "" // explicit empty to verify default kicks in

	cls := classifyAllPlain(inputs.ProjectEnvs)
	bundle, err := ops.BuildLaunchBundle(inputs, cls)
	if err != nil {
		t.Fatalf("BuildLaunchBundle: %v", err)
	}
	if got := inputs.Runtimes[0].SetupName; got != "prod" {
		t.Errorf("expected normalized SetupName 'prod', got %q", got)
	}
	doc := parseImportYAML(t, bundle.ImportYAML)
	runtime := findService(t, doc, "app")
	if _, present := runtime["zeropsSetup"]; present {
		t.Errorf("launch import YAML must not carry zeropsSetup (pipelineConfig needs buildFromGit), got %v", runtime["zeropsSetup"])
	}
}

// TestBuildLaunchBundle_SourceSnapshotHashesRawEnvs pins P-LP-3
// preservation across the F3 auto-classification work: SourceSnapshot
// digests the raw input env list, NOT the post-classification view.
// Two bundles built from inputs that differ ONLY in one env value
// must produce DIFFERENT ProjectEnvsDigest values — even when both
// envs would auto-bucket the same way.
//
// Without this guarantee, the workflow handler's drift-rejection
// before mutation would not see env changes that escaped
// classification (a real source-state mutation between compose and
// publish would slip past the source-immutability guard).
func TestBuildLaunchBundle_SourceSnapshotHashesRawEnvs(t *testing.T) {
	t.Parallel()

	base := minimalLaunchInputs()
	bundleA, err := ops.BuildLaunchBundle(base, classifyAllPlain(base.ProjectEnvs))
	if err != nil {
		t.Fatalf("BuildLaunchBundle A: %v", err)
	}

	mutated := minimalLaunchInputs()
	// Same key, different value — the bundle composer might handle them
	// identically, but the immutability guard must distinguish them.
	if len(mutated.ProjectEnvs) == 0 {
		t.Fatal("minimalLaunchInputs must include at least one env")
	}
	mutated.ProjectEnvs[0].Value += "-mutated"
	bundleB, err := ops.BuildLaunchBundle(mutated, classifyAllPlain(mutated.ProjectEnvs))
	if err != nil {
		t.Fatalf("BuildLaunchBundle B: %v", err)
	}

	if bundleA.SourceSnapshot.ProjectEnvsDigest == "" {
		t.Fatal("ProjectEnvsDigest must be populated")
	}
	if bundleA.SourceSnapshot.ProjectEnvsDigest == bundleB.SourceSnapshot.ProjectEnvsDigest {
		t.Errorf("digest unchanged despite env mutation; raw envs must reach the hash\nA=%s\nB=%s",
			bundleA.SourceSnapshot.ProjectEnvsDigest, bundleB.SourceSnapshot.ProjectEnvsDigest)
	}
}

// --- helpers ---

func parseImportYAML(t *testing.T, body string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("parse yaml: %v\nbody:\n%s", err, body)
	}
	return doc
}

func findService(t *testing.T, doc map[string]any, hostname string) map[string]any {
	t.Helper()
	servicesRaw, ok := doc["services"].([]any)
	if !ok {
		t.Fatal("services array missing")
	}
	for _, s := range servicesRaw {
		m, ok := s.(map[string]any)
		if !ok {
			continue
		}
		if m["hostname"] == hostname {
			return m
		}
	}
	t.Fatalf("service %q not found in import yaml", hostname)
	return nil
}
