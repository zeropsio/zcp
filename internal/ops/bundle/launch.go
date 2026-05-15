package bundle

import (
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/schema"
	"github.com/zeropsio/zcp/internal/topology"
)

// runtimeProductionMinContainers is the default minimum container
// count for runtime services in production — provides HA-via-
// replication for stateless apps.
const runtimeProductionMinContainers = 2

// runtimeProductionCPUMode is the default CPU mode for production
// runtime services. DEDICATED removes the noisy-neighbor variance of
// SHARED at the cost of higher per-container price.
const runtimeProductionCPUMode = "DEDICATED"

// BuildLaunch composes the production import yaml. Variant on inputs
// selects between launch-new (full project block — feeds
// PostClientProjectImport) and launch-existing (services-only yaml —
// feeds PostProjectServiceStackImport, which rejects project blocks).
// Zero (VariantExportDev) normalizes to VariantLaunchNew.
//
// Composition steps mirror the prior internal/ops/launch_bundle.go
// pipeline (preserved verbatim during Phase 1b refactor):
//
//  1. Verify SetupName exists in ZeropsYAMLBody.
//  2. Classify project envs via composeProjectEnvVariables.
//  3. Compose services array — runtime + managed entries with HA
//     promotion (per ServiceTypeRules; opt-out via KeepNonHA).
//  4. Compose project block — name + tags + envVariables (omitted
//     for VariantLaunchExisting).
//  5. Marshal yaml + add preprocessor header.
//  6. Schema-validate; surface errors on bundle.
//  7. Compute SourceSnapshot hashes (P-LP-3).
func BuildLaunch(
	inputs LaunchBundleInputs,
	classifications map[string]topology.SecretClassification,
) (*LaunchBundle, error) {
	if inputs.TargetProjectName == "" {
		return nil, fmt.Errorf("launch bundle: TargetProjectName required")
	}
	if inputs.TargetHostname == "" {
		return nil, fmt.Errorf("launch bundle: TargetHostname required")
	}
	if inputs.ServiceType == "" {
		return nil, fmt.Errorf("launch bundle: ServiceType required")
	}
	if inputs.SetupName == "" {
		inputs.SetupName = "prod"
	}
	if inputs.RepoURL == "" {
		return nil, fmt.Errorf("launch bundle: RepoURL required")
	}
	if inputs.ZeropsYAMLBody == "" {
		return nil, fmt.Errorf("launch bundle: ZeropsYAMLBody required")
	}
	if inputs.SourceProjectID == "" {
		return nil, fmt.Errorf("launch bundle: SourceProjectID required (audit + immutability guard)")
	}
	if err := verifyZeropsYAMLSetup(inputs.ZeropsYAMLBody, inputs.SetupName); err != nil {
		return nil, err
	}

	variant := inputs.Variant
	if !variant.IsLaunch() {
		variant = VariantLaunchNew
	}

	bundle := &LaunchBundle{
		TargetProjectName: inputs.TargetProjectName,
		SourceProjectID:   inputs.SourceProjectID,
		Classifications:   classifications,
	}

	projectEnvs, envWarnings := composeProjectEnvVariables(inputs.ProjectEnvs, classifications)
	bundle.Warnings = append(bundle.Warnings, envWarnings...)

	zeropsRefs := extractZeropsYAMLRunEnvRefs(inputs.ZeropsYAMLBody)
	bundle.Warnings = append(bundle.Warnings, detectIndirectInfraReferences(inputs.ProjectEnvs, classifications, zeropsRefs)...)

	minContainers := inputs.MinContainers
	if minContainers <= 0 {
		minContainers = runtimeProductionMinContainers
	}
	runtimeEntry := map[string]any{
		"hostname":      inputs.TargetHostname,
		"type":          inputs.ServiceType,
		"mode":          importModeNonHA,
		"buildFromGit":  inputs.RepoURL,
		"zeropsSetup":   inputs.SetupName,
		"minContainers": minContainers,
		"verticalAutoscaling": map[string]any{
			"cpuMode": runtimeProductionCPUMode,
		},
	}

	keepNonHASet := make(map[string]bool, len(inputs.KeepNonHA))
	for _, h := range inputs.KeepNonHA {
		keepNonHASet[h] = true
	}

	services := make([]any, 0, 1+len(inputs.ManagedServices))
	services = append(services, runtimeEntry)
	for _, m := range inputs.ManagedServices {
		entry := managedEntryWithRules(m, true /*launch*/, keepNonHASet[m.Hostname])
		services = append(services, entry)
	}

	doc := map[string]any{
		"services": services,
	}
	if variant == VariantLaunchNew {
		project := map[string]any{
			"name": inputs.TargetProjectName,
			"tags": composeLaunchTags(inputs.SourceProjectID, inputs.AdditionalTags),
		}
		if len(projectEnvs) > 0 {
			project["envVariables"] = projectEnvs
		}
		doc["project"] = project
	}

	out, err := yaml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("launch bundle: marshal yaml: %w", err)
	}
	body := string(out)
	// Preprocessor header still applies to VariantLaunchExisting —
	// services-only yaml may still reference cross-service envs that
	// need preprocessor preamble (zerops considers preprocessor a
	// document-level directive, not project-block-scoped).
	body = addPreprocessorHeader(body, projectEnvs)

	bundle.ImportYAML = body

	if errs := schema.ValidateImportYAML(body); len(errs) > 0 {
		bundle.Errors = errs
	}

	bundle.SourceSnapshot = computeSourceSnapshot(inputs)
	return bundle, nil
}
