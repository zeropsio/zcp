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
// Composition pipeline:
//
//  1. Validate inputs (per-runtime + project-level).
//  2. Verify each runtime's SetupName exists in its ZeropsYAMLBody.
//  3. Classify project envs via composeProjectEnvVariables.
//  4. Loop runtimes — one services[] entry per LaunchRuntimeInput with
//     its own buildFromGit + zeropsSetup + minContainers.
//  5. Append managed deps (deduplicated by hostname so shared infra
//     across multiple promoted runtimes lands once) with HA promotion
//     per ServiceTypeRules; opt-out via KeepNonHA.
//  6. Compose project block — name + tags + envVariables (omitted
//     for VariantLaunchExisting).
//  7. Marshal yaml + add preprocessor header.
//  8. Schema-validate; surface errors on bundle.
//  9. Compute SourceSnapshot hashes (P-LP-3) over the first runtime's
//     git SHA + the merged zerops.yaml body digest (multi-runtime
//     SourceSnapshot reshape lands in a follow-up).
func BuildLaunch(
	inputs LaunchBundleInputs,
	classifications map[string]topology.SecretClassification,
) (*LaunchBundle, error) {
	if inputs.TargetProjectName == "" {
		return nil, fmt.Errorf("launch bundle: TargetProjectName required")
	}
	if inputs.SourceProjectID == "" {
		return nil, fmt.Errorf("launch bundle: SourceProjectID required (audit + immutability guard)")
	}
	if len(inputs.Runtimes) == 0 {
		return nil, fmt.Errorf("launch bundle: at least one runtime required (Runtimes is empty)")
	}
	for i := range inputs.Runtimes {
		if inputs.Runtimes[i].SetupName == "" {
			inputs.Runtimes[i].SetupName = "prod"
		}
		r := inputs.Runtimes[i]
		if r.ProdHostname == "" {
			return nil, fmt.Errorf("launch bundle: runtime[%d]: ProdHostname required", i)
		}
		if r.ServiceType == "" {
			return nil, fmt.Errorf("launch bundle: runtime[%d] (%s): ServiceType required", i, r.ProdHostname)
		}
		if r.RepoURL == "" {
			return nil, fmt.Errorf("launch bundle: runtime[%d] (%s): RepoURL required", i, r.ProdHostname)
		}
		if r.ZeropsYAMLBody == "" {
			return nil, fmt.Errorf("launch bundle: runtime[%d] (%s): ZeropsYAMLBody required", i, r.ProdHostname)
		}
		if err := verifyZeropsYAMLSetup(r.ZeropsYAMLBody, r.SetupName); err != nil {
			return nil, fmt.Errorf("launch bundle: runtime[%d] (%s): %w", i, r.ProdHostname, err)
		}
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

	// Cross-service env refs scan reads the first runtime's zerops.yaml
	// when all runtimes share a repo (monorepo) and reads each one when
	// they differ. Multi-runtime + separate-repo support concatenates
	// every body so detectIndirectInfraReferences sees all refs.
	mergedYAMLForRefs := mergeZeropsYAMLBodiesForRefs(inputs.Runtimes)
	zeropsRefs := extractZeropsYAMLRunEnvRefs(mergedYAMLForRefs)
	bundle.Warnings = append(bundle.Warnings, detectIndirectInfraReferences(inputs.ProjectEnvs, classifications, zeropsRefs)...)

	keepNonHASet := make(map[string]bool, len(inputs.KeepNonHA))
	for _, h := range inputs.KeepNonHA {
		keepNonHASet[h] = true
	}

	services := make([]any, 0, len(inputs.Runtimes)+len(inputs.ManagedServices))
	for _, r := range inputs.Runtimes {
		services = append(services, runtimeEntryFromInput(r))
	}
	for _, m := range dedupeManagedByHostname(inputs.ManagedServices) {
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

	bundle.SourceSnapshot = computeSourceSnapshotMulti(inputs)
	return bundle, nil
}

// runtimeEntryFromInput renders one services[] entry from a per-runtime
// LaunchRuntimeInput. Centralized so the YAML field shape stays
// consistent between every promoted runtime in the bundle.
func runtimeEntryFromInput(r LaunchRuntimeInput) map[string]any {
	minContainers := r.MinContainers
	if minContainers <= 0 {
		minContainers = runtimeProductionMinContainers
	}
	return map[string]any{
		"hostname":      r.ProdHostname,
		"type":          r.ServiceType,
		"mode":          importModeNonHA,
		"buildFromGit":  r.RepoURL,
		"zeropsSetup":   r.SetupName,
		"minContainers": minContainers,
		"verticalAutoscaling": map[string]any{
			"cpuMode": runtimeProductionCPUMode,
		},
	}
}

// dedupeManagedByHostname returns the input list with duplicate
// hostnames collapsed to the first occurrence. Multi-runtime
// promotion that shares managed deps (e.g. monorepo app + worker
// both pointing at the same db) must not emit duplicate services[]
// entries — the import API rejects them.
func dedupeManagedByHostname(in []ManagedServiceEntry) []ManagedServiceEntry {
	if len(in) <= 1 {
		return in
	}
	seen := make(map[string]bool, len(in))
	out := make([]ManagedServiceEntry, 0, len(in))
	for _, m := range in {
		if seen[m.Hostname] {
			continue
		}
		seen[m.Hostname] = true
		out = append(out, m)
	}
	return out
}

// mergeZeropsYAMLBodiesForRefs concatenates per-runtime zerops.yaml
// bodies for cross-service env reference scanning. Monorepo
// (all runtimes share the same body) folds to one copy via dedupe
// so duplicate scan results don't double-count.
func mergeZeropsYAMLBodiesForRefs(runtimes []LaunchRuntimeInput) string {
	if len(runtimes) == 0 {
		return ""
	}
	seen := make(map[string]bool, len(runtimes))
	var sb yamlBodyBuilder
	for _, r := range runtimes {
		if r.ZeropsYAMLBody == "" || seen[r.ZeropsYAMLBody] {
			continue
		}
		seen[r.ZeropsYAMLBody] = true
		sb.AppendBody(r.ZeropsYAMLBody)
	}
	return sb.String()
}

// yamlBodyBuilder is a tiny strings.Builder-equivalent that adds a
// separator newline between bodies so concatenated YAML still parses
// as one document for the reference scanner (which is line-based).
type yamlBodyBuilder struct {
	parts []string
}

func (b *yamlBodyBuilder) AppendBody(body string) {
	b.parts = append(b.parts, body)
}

func (b *yamlBodyBuilder) String() string {
	if len(b.parts) == 0 {
		return ""
	}
	if len(b.parts) == 1 {
		return b.parts[0]
	}
	total := 0
	for _, p := range b.parts {
		total += len(p) + 1
	}
	out := make([]byte, 0, total)
	for i, p := range b.parts {
		if i > 0 {
			out = append(out, '\n')
		}
		out = append(out, p...)
	}
	return string(out)
}
