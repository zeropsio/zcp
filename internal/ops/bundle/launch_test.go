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
