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

// TestBuildLaunch_ScalingMaxContainersClampAndCPUModeWarn pins the Codex-found
// fixes: a source minContainers/maxContainers of 1/1 must not emit min=2,max=1
// (invalid interval) — maxContainers is raised to the floor with a warning; and
// a SHARED source cpuMode flipped to DEDICATED is a NAMED warned transform.
// TestBuildLaunch_BuildFromGitStripsDotGit pins the launch sink: the seed
// RepoURL carries a trailing ".git" (launchInputsWith), and the emitted
// buildFromGit MUST drop it — a ".git" suffix fails Zerops' clone-preflight
// (terminal FAILED in ~0.3s, no logs). Parity with the export sink
// (TestComposeImportYAML_MinimalRuntimeOnly).
func TestBuildLaunch_BuildFromGitStripsDotGit(t *testing.T) {
	t.Parallel()
	in := launchInputsWith(launchYAMLWithDBRef, nil)
	if !strings.HasSuffix(in.Runtimes[0].RepoURL, ".git") {
		t.Fatalf("fixture precondition: RepoURL must carry .git to pin the strip, got %q", in.Runtimes[0].RepoURL)
	}
	b, err := BuildLaunch(in, nil)
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	if !strings.Contains(b.ImportYAML, "buildFromGit: https://github.com/example/app\n") {
		t.Errorf("expected canonical buildFromGit without .git:\n%s", b.ImportYAML)
	}
	if strings.Contains(b.ImportYAML, "buildFromGit: https://github.com/example/app.git") {
		t.Errorf("buildFromGit must NOT carry .git:\n%s", b.ImportYAML)
	}
}

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
