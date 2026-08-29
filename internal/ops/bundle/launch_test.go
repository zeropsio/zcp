package bundle

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

// launchYAMLNoDBRef is a runtime zerops.yaml whose run.envVariables does NOT
// reference any ${db_*} cross-service var.
const launchYAMLNoDBRef = `zerops:
  - setup: app
    build:
      base: nodejs@22
      buildCommands: ["npm ci"]
      deployFiles: ["./"]
    run:
      base: nodejs@22
      envVariables:
        NODE_ENV: production
`

// launchYAMLWithDBRef references the managed db via an explicit ${db_*} ref.
const launchYAMLWithDBRef = `zerops:
  - setup: app
    build:
      base: nodejs@22
      buildCommands: ["npm ci"]
      deployFiles: ["./"]
    run:
      base: nodejs@22
      envVariables:
        DATABASE_URL: ${db_connectionString}
`

const launchYAMLWithLocalStorageVolume = `zerops:
  - setup: app
    run:
      base: nodejs@22
      volume:
        hostname: data
        mountPath: /var/lib/app-data
`

func launchInputsWith(yamlBody string, projectEnvs []ProjectEnvVar) LaunchBundleInputs {
	return LaunchBundleInputs{
		SourceProjectID:   "src-proj",
		TargetProjectName: "prod-proj",
		Variant:           VariantLaunchNew,
		Runtimes: []LaunchRuntimeInput{{
			ProdHostname:   "app",
			ServiceType:    "nodejs@22",
			SetupName:      "app",
			RepoURL:        "https://github.com/example/app.git",
			GitCommitSHA:   "abc123",
			ZeropsYAMLBody: yamlBody,
		}},
		ManagedServices: []ManagedServiceEntry{{Hostname: "db", Type: "postgresql@16", Mode: "NON_HA"}},
		ProjectEnvs:     projectEnvs,
	}
}

func TestBuildLaunch_LocalStorageVolumeReference_NoUnreferencedWarning(t *testing.T) {
	t.Parallel()
	inputs := launchInputsWith(launchYAMLWithLocalStorageVolume, nil)
	inputs.ManagedServices = []ManagedServiceEntry{{Hostname: "data", Type: "local-storage:single@1"}}
	b, err := BuildLaunch(inputs, nil)
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	if len(b.ManagedDeps) != 1 || !b.ManagedDeps[0].Referenced {
		t.Fatalf("Local Storage volume reference not recognized: %+v", b.ManagedDeps)
	}
	if hasManagedUnreferencedWarning(b.Warnings, "data") {
		t.Fatalf("mounted Local Storage was reported unreferenced: %v", b.Warnings)
	}
}

func TestBuildLaunch_LocalStorageUnmounted_RecommendsRunVolumeNotConnectionEnv(t *testing.T) {
	t.Parallel()
	inputs := launchInputsWith(launchYAMLNoDBRef, nil)
	inputs.ManagedServices = []ManagedServiceEntry{{Hostname: "data", Type: "local-storage:single@1"}}
	b, err := BuildLaunch(inputs, nil)
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	warnings := strings.Join(b.Warnings, "\n")
	if !strings.Contains(warnings, "run.volume.hostname: data") {
		t.Fatalf("unmounted Local Storage warning lacks volume guidance: %s", warnings)
	}
	for _, forbidden := range []string{"data_connectionString", "data_hostname", "${data_*}"} {
		if strings.Contains(warnings, forbidden) {
			t.Errorf("Local Storage warning contains DB/env guidance %q: %s", forbidden, warnings)
		}
	}
}

// TestBuildLaunch_PipelineFirst_NoBuildFromGit pins the pipeline-first launch
// composition (plans/archive/launch-pipeline-first-2026-06-11.md P0): the production
// import NEVER carries buildFromGit — runtimes start empty via
// startWithoutCode: true and the first production build arrives through the
// production pipeline (tag-triggered), like every subsequent one. buildFromGit
// remains an export-only emission (TestComposeImportYAML_MinimalRuntimeOnly).
func TestBuildLaunch_PipelineFirst_NoBuildFromGit(t *testing.T) {
	t.Parallel()
	in := launchInputsWith(launchYAMLWithDBRef, nil)
	b, err := BuildLaunch(in, nil)
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	if strings.Contains(b.ImportYAML, "buildFromGit") {
		t.Errorf("launch import YAML must not carry buildFromGit:\n%s", b.ImportYAML)
	}
	// zeropsSetup must be absent too: the import API groups it under
	// pipelineConfig and REJECTS it without buildFromGit ("parameter is
	// required for use of pipelineConfig", live-verified 2026-06-11). The
	// setup name reaches production through the pipeline wiring instead
	// (zcli push --setup in the actions workflow; zeropsYamlSetup in the
	// webhook recommendation).
	if strings.Contains(b.ImportYAML, "zeropsSetup") {
		t.Errorf("launch import YAML must not carry zeropsSetup (pipelineConfig needs buildFromGit):\n%s", b.ImportYAML)
	}
	if !strings.Contains(b.ImportYAML, "startWithoutCode: true") {
		t.Errorf("import YAML missing startWithoutCode:\n%s", b.ImportYAML)
	}
}

// TestBuildLaunch_RepoURLStillRequired pins the input contract: RepoURL no
// longer lands in the YAML, but it remains REQUIRED — production still builds
// from the repo; the URL feeds pipeline wiring (the ZEROPS_TOKEN_PROD secret
// command's -R owner/repo, the webhook integration's repositoryFullName) and
// the source-control gate's identity comparison.
func TestBuildLaunch_RepoURLStillRequired(t *testing.T) {
	t.Parallel()
	in := launchInputsWith(launchYAMLWithDBRef, nil)
	in.Runtimes[0].RepoURL = ""
	if _, err := BuildLaunch(in, nil); err == nil || !strings.Contains(err.Error(), "RepoURL required") {
		t.Fatalf("expected RepoURL-required error, got %v", err)
	}
}

// TestBuildLaunch_ScalingMaxContainersClampAndCPUModeWarn pins the Codex-found
// fixes: a source minContainers/maxContainers of 1/1 must not emit min=2,max=1
// (invalid interval) — maxContainers is raised to the floor with a warning; and
// a SHARED source cpuMode flipped to DEDICATED is a NAMED warned transform.

func TestBuildLaunch_ScalingMaxContainersClampAndCPUModeWarn(t *testing.T) {
	t.Parallel()
	in := launchInputsWith(launchYAMLWithDBRef, nil)
	in.Runtimes[0].Scaling = &Scaling{MinContainers: 1, MaxContainers: 1, CPUMode: "SHARED"}
	b, err := BuildLaunch(in, nil)
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	// Invalid interval (min 2 > max 1) must NOT be emitted.
	if strings.Contains(b.ImportYAML, "maxContainers: 1") {
		t.Errorf("maxContainers must be raised to the min floor, not left at 1:\n%s", b.ImportYAML)
	}
	for _, want := range []string{"minContainers: 2", "maxContainers: 2"} {
		if !strings.Contains(b.ImportYAML, want) {
			t.Errorf("import YAML missing %q:\n%s", want, b.ImportYAML)
		}
	}
	maxWarn, cpuWarn := false, false
	for _, w := range b.Warnings {
		if strings.Contains(w, "maxContainers 1→2") {
			maxWarn = true
		}
		if strings.Contains(w, "cpuMode SHARED→DEDICATED") {
			cpuWarn = true
		}
	}
	if !maxWarn {
		t.Errorf("expected a named maxContainers-clamp warning, got %v", b.Warnings)
	}
	if !cpuWarn {
		t.Errorf("expected a named cpuMode SHARED→DEDICATED warning, got %v", b.Warnings)
	}
}

func hasManagedUnreferencedWarning(warnings []string, host string) bool {
	for _, w := range warnings {
		if strings.Contains(w, host) && strings.Contains(w, "service isolation") {
			return true
		}
	}
	return false
}

// TestBuildLaunch_ScalingReflectWithFloorAndWarns pins R7-P4: launch reflects
// the source scaling then applies named production transforms — minContainers
// floored to the HA minimum WITH a warning when the source is below it, source
// bounds reflected, cpuMode forced to DEDICATED.
func TestBuildLaunch_ScalingReflectWithFloorAndWarns(t *testing.T) {
	t.Parallel()
	in := launchInputsWith(launchYAMLWithDBRef, nil)
	in.Runtimes[0].Scaling = &Scaling{
		MinContainers: 1, // below the prod floor of 2 → floored + warned
		MaxContainers: 4,
		MinCPU:        1, MaxCPU: 6,
		MinRAM: 0.5, MaxRAM: 8,
	}
	b, err := BuildLaunch(in, nil)
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	floorWarn := false
	for _, w := range b.Warnings {
		if strings.Contains(w, "minContainers 1→2") {
			floorWarn = true
		}
	}
	if !floorWarn {
		t.Errorf("expected a named minContainers floor warning, got %v", b.Warnings)
	}
	// The prod floor + reflected source bounds + DEDICATED land in the YAML.
	for _, want := range []string{"minContainers: 2", "maxContainers: 4", "maxCpu: 6", "maxRam: 8", "cpuMode: DEDICATED"} {
		if !strings.Contains(b.ImportYAML, want) {
			t.Errorf("import YAML missing %q:\n%s", want, b.ImportYAML)
		}
	}
}

// TestBuildLaunch_UnreferencedManagedDep_Warns — PR-4: a promoted managed dep
// that no runtime's run.envVariables (and no kept project env) references via
// ${host_*} is unreachable under the default service isolation. The composer
// must surface a launch warning (not hard-fail) so the operator wires it.
func TestBuildLaunch_UnreferencedManagedDep_Warns(t *testing.T) {
	t.Parallel()
	bundle, err := BuildLaunch(launchInputsWith(launchYAMLNoDBRef, nil), nil)
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	if !hasManagedUnreferencedWarning(bundle.Warnings, "db") {
		t.Fatalf("expected unreferenced-managed-dep warning for db, got warnings:\n%s", strings.Join(bundle.Warnings, "\n"))
	}
}

// TestBuildLaunch_PopulatesManagedDepReferences pins the structured
// per-dep wiring state on the bundle: every promoted managed dep carries
// Referenced derived from the same ref scan that drives the PR-4
// warning, so the ready-to-launch preview can mark referenced=false
// deps and recommend exclusion BEFORE the launchKey is spent.
func TestBuildLaunch_PopulatesManagedDepReferences(t *testing.T) {
	t.Parallel()
	inputs := launchInputsWith(launchYAMLWithDBRef, nil)
	inputs.ManagedServices = append(inputs.ManagedServices,
		ManagedServiceEntry{Hostname: "cache", Type: "valkey@7.2", Mode: "NON_HA"})
	bundle, err := BuildLaunch(inputs, nil)
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	got := map[string]bool{}
	for _, d := range bundle.ManagedDeps {
		got[d.Hostname] = d.Referenced
	}
	if len(bundle.ManagedDeps) != 2 {
		t.Fatalf("expected 2 managed dep references, got %v", bundle.ManagedDeps)
	}
	if !got["db"] {
		t.Errorf("db is referenced via ${db_connectionString} — Referenced must be true; got %v", bundle.ManagedDeps)
	}
	if got["cache"] {
		t.Errorf("nothing references ${cache_*} — Referenced must be false; got %v", bundle.ManagedDeps)
	}
}

// TestBuildLaunch_ManagedDepReferencedInYaml_NoWarn — an explicit ${db_*} ref
// in run.envVariables makes the dep reachable; no warning.
func TestBuildLaunch_ManagedDepReferencedInYaml_NoWarn(t *testing.T) {
	t.Parallel()
	bundle, err := BuildLaunch(launchInputsWith(launchYAMLWithDBRef, nil), nil)
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	if hasManagedUnreferencedWarning(bundle.Warnings, "db") {
		t.Fatalf("db is referenced via ${db_connectionString}; should not warn. warnings:\n%s", strings.Join(bundle.Warnings, "\n"))
	}
}

// TestBuildLaunch_ManagedDepReferencedViaProjectEnv_NoWarn — a kept project
// env value referencing ${db_*} also makes the dep reachable (project envs
// auto-inject and refs resolve regardless of isolation — spec §3); no warning.
func TestBuildLaunch_ManagedDepReferencedViaProjectEnv_NoWarn(t *testing.T) {
	t.Parallel()
	bundle, err := BuildLaunch(
		launchInputsWith(launchYAMLNoDBRef, []ProjectEnvVar{{Key: "DB_URL", Value: "${db_hostname}"}}),
		map[string]topology.SecretClassification{"DB_URL": topology.SecretClassPlainConfig},
	)
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	if hasManagedUnreferencedWarning(bundle.Warnings, "db") {
		t.Fatalf("db is referenced via project env DB_URL=${db_hostname}; should not warn. warnings:\n%s", strings.Join(bundle.Warnings, "\n"))
	}
}

// TestBuildLaunch_CorePackageSerious pins the F3 production default: a
// launch-new project block carries corePackage SERIOUS unless the caller
// overrides — the schema default is LIGHT (shared core), which is the
// wrong tier for production and was what every launched project silently
// got while the composer emitted no corePackage at all.
func TestBuildLaunch_CorePackageSerious(t *testing.T) {
	t.Parallel()
	inputs := launchInputsWith(launchYAMLNoDBRef, nil)
	b, err := BuildLaunch(inputs, nil)
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	if !strings.Contains(b.ImportYAML, "corePackage: SERIOUS") {
		t.Errorf("launch-new project block must default corePackage: SERIOUS; yaml:\n%s", b.ImportYAML)
	}
}

// TestBuildLaunch_CorePackageLightOverride pins the explicit cheaper
// choice: LIGHT is allowed (recommendation stays SERIOUS, but the user
// owns the trade-off) and must land verbatim.
func TestBuildLaunch_CorePackageLightOverride(t *testing.T) {
	t.Parallel()
	inputs := launchInputsWith(launchYAMLNoDBRef, nil)
	inputs.CorePackage = "LIGHT"
	b, err := BuildLaunch(inputs, nil)
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	if !strings.Contains(b.ImportYAML, "corePackage: LIGHT") {
		t.Errorf("explicit LIGHT override must be emitted; yaml:\n%s", b.ImportYAML)
	}
}

// TestBuildLaunch_Location pins the region emit: the user-selected region
// reaches project.location (it was previously computed and silently
// dropped — every prod project landed in the account-default region).
// Empty Location defaults to eu-central so the destination region is
// always explicit in the bundle the user previews.
func TestBuildLaunch_Location(t *testing.T) {
	t.Parallel()
	inputs := launchInputsWith(launchYAMLNoDBRef, nil)
	inputs.Location = "us-west-1"
	b, err := BuildLaunch(inputs, nil)
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	if !strings.Contains(b.ImportYAML, "location: us-west-1") {
		t.Errorf("selected region must be emitted as project.location; yaml:\n%s", b.ImportYAML)
	}

	inputs.Location = ""
	b2, err := BuildLaunch(inputs, nil)
	if err != nil {
		t.Fatalf("BuildLaunch (default): %v", err)
	}
	if !strings.Contains(b2.ImportYAML, "location: eu-central") {
		t.Errorf("empty Location must default to eu-central; yaml:\n%s", b2.ImportYAML)
	}
}

// TestBuildLaunch_ExistingVariantOmitsProjectBlock re-pins that the
// launch-existing variant (services-only yaml) carries neither
// corePackage nor location — the destination project already exists and
// PostProjectServiceStackImport rejects project blocks.
func TestBuildLaunch_ExistingVariantOmitsProjectBlock(t *testing.T) {
	t.Parallel()
	inputs := launchInputsWith(launchYAMLNoDBRef, nil)
	inputs.Variant = VariantLaunchExisting
	inputs.CorePackage = "SERIOUS"
	inputs.Location = "eu-central"
	b, err := BuildLaunch(inputs, nil)
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	for _, forbidden := range []string{"corePackage", "location:", "project:"} {
		if strings.Contains(b.ImportYAML, forbidden) {
			t.Errorf("launch-existing yaml must not carry %q; yaml:\n%s", forbidden, b.ImportYAML)
		}
	}
}

// TestCollectZeropsYAMLRunEnvRefs_MultiRuntimeSeparateBodies pins B16: each
// runtime's zerops.yaml is its own document with a top-level `zerops:` key,
// so the refs must be collected per-body and unioned. Concatenating the two
// bodies (the prior behavior) produced a duplicate-key YAML the scanner
// rejected, returning an empty ref set and blinding the infra-ref detector.
func TestCollectZeropsYAMLRunEnvRefs_MultiRuntimeSeparateBodies(t *testing.T) {
	t.Parallel()

	bodyA := "zerops:\n  - setup: api\n    run:\n      envVariables:\n        DB: ${db_connectionString}\n"
	bodyB := "zerops:\n  - setup: worker\n    run:\n      envVariables:\n        CACHE: ${cache_host}\n"

	refs := collectZeropsYAMLRunEnvRefs([]LaunchRuntimeInput{
		{ZeropsYAMLBody: bodyA},
		{ZeropsYAMLBody: bodyB},
	})

	if !refs["db_connectionString"] {
		t.Error("missing db_connectionString — first body's refs were lost")
	}
	if !refs["cache_host"] {
		t.Error("missing cache_host — second body's refs were lost (concatenation regression)")
	}
}

// TestBuildLaunch_AdoptsSoleSetupWhenRequestedAbsent pins the reconcile-and-adopt
// fix: when the cascade-resolved setup name is absent from the source
// zerops.yaml but the file declares exactly ONE setup block, BuildLaunch adopts
// that block (a single setup is unambiguous) and warns — instead of aborting a
// healthy, deployable repo. Reject-healthy-state correctness.
func TestBuildLaunch_AdoptsSoleSetupWhenRequestedAbsent(t *testing.T) {
	t.Parallel()
	const soleSetupYAML = `zerops:
  - setup: production
    build:
      base: nodejs@22
      buildCommands: ["npm ci"]
      deployFiles: ["./"]
    run:
      base: nodejs@22
`
	inputs := LaunchBundleInputs{
		SourceProjectID:   "src-proj",
		TargetProjectName: "prod-proj",
		Variant:           VariantLaunchNew,
		Runtimes: []LaunchRuntimeInput{{
			ProdHostname:   "app",
			ServiceType:    "nodejs@22",
			SetupName:      "prod", // legacy default — NOT present in the yaml
			RepoURL:        "https://github.com/me/app",
			ZeropsYAMLBody: soleSetupYAML,
		}},
	}
	b, err := BuildLaunch(inputs, nil)
	if err != nil {
		t.Fatalf("BuildLaunch should adopt the sole setup, got error: %v", err)
	}
	if !strings.Contains(strings.Join(b.Warnings, "\n"), "adopted the only declared setup \"production\"") {
		t.Errorf("expected an adoption warning naming 'production', warnings: %v", b.Warnings)
	}
	// The adopted name propagates to the caller's runtime input (shared slice)
	// so the downstream pipeline wiring uses it, not the absent "prod".
	if inputs.Runtimes[0].SetupName != "production" {
		t.Errorf("SetupName = %q, want adopted %q", inputs.Runtimes[0].SetupName, "production")
	}
}

// TestBuildLaunch_RejectsAmbiguousSetupMismatch keeps the gate where it is a
// genuine ambiguity: requested setup absent AND multiple blocks declared → no
// silent pick, the error names the candidates.
func TestBuildLaunch_RejectsAmbiguousSetupMismatch(t *testing.T) {
	t.Parallel()
	const twoSetupYAML = `zerops:
  - setup: stage
    build:
      base: nodejs@22
      buildCommands: ["npm ci"]
      deployFiles: ["./"]
    run:
      base: nodejs@22
  - setup: prod-eu
    build:
      base: nodejs@22
      buildCommands: ["npm ci"]
      deployFiles: ["./"]
    run:
      base: nodejs@22
`
	inputs := LaunchBundleInputs{
		SourceProjectID:   "src-proj",
		TargetProjectName: "prod-proj",
		Variant:           VariantLaunchNew,
		Runtimes: []LaunchRuntimeInput{{
			ProdHostname:   "app",
			ServiceType:    "nodejs@22",
			SetupName:      "prod",
			RepoURL:        "https://github.com/me/app",
			ZeropsYAMLBody: twoSetupYAML,
		}},
	}
	if _, err := BuildLaunch(inputs, nil); err == nil {
		t.Fatal("expected error on ambiguous setup mismatch, got nil")
	}
}
