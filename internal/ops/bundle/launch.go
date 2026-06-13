package bundle

import (
	"fmt"
	"maps"
	"strings"

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

// productionDefaultCorePackage is the corePackage the composer emits when
// the caller does not override: SERIOUS (dedicated core) is the production
// default per the platform spike A.7 — the schema default is LIGHT, which
// every launched project silently got while the composer emitted no
// corePackage at all. LIGHT stays a legitimate explicit override.
const productionDefaultCorePackage = "SERIOUS"

// productionDefaultLocation is the region emitted when the caller supplies
// none. Explicit-always: the previewed bundle must show WHERE production
// will land (input.Region used to be computed and silently dropped).
const productionDefaultLocation = "eu-central"

// legacyDefaultSetupName is the deferred plan §P5 fallback the composer
// uses when the caller-supplied SetupName is empty. The handler-side
// cascade (resolveLaunchSetupName + launchTargetSetupName) keeps this
// constant as the tail entry so existing tests + flow-eval scenarios
// continue to work; a follow-up that seeds PrimarySetupName /
// StageSetupName on launch test fixtures will let us flip the empty
// case into a structured blocker.
const legacyDefaultSetupName = "prod"

// BuildLaunch composes the production import yaml. Variant on inputs
// selects between launch-new (full project block — feeds
// PostClientProjectImport) and launch-existing (services-only yaml —
// feeds PostProjectServiceStackImport, which rejects project blocks).
// The zero value is VariantLaunchNew; any non-launch value normalizes to it.
//
// Composition pipeline:
//
//  1. Validate inputs (per-runtime + project-level).
//  2. Verify each runtime's SetupName exists in its ZeropsYAMLBody.
//  3. Classify project envs via composeProjectEnvVariables.
//  4. Loop runtimes — one services[] entry per LaunchRuntimeInput with
//     startWithoutCode + minContainers (pipeline-first: no buildFromGit
//     and no zeropsSetup — both are pipelineConfig, which the import API
//     rejects without a repo; the first prod build arrives via the
//     production pipeline, which owns the setup reference).
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
			// Plan §P5 deferred: empty SetupName here should surface
			// as a structured requiresSetupInput blocker to the caller
			// instead of defaulting. Until the launch handler test
			// fixtures are updated to seed PrimarySetupName /
			// StageSetupName on the source meta, the composer keeps
			// the "prod" legacy default. The conventional fallback is
			// now ONLY here — every other site reads from cascade or
			// explicit caller-supplied value.
			inputs.Runtimes[i].SetupName = legacyDefaultSetupName
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

	// Cross-service env refs scan: scan EACH distinct runtime zerops.yaml
	// body separately and union the ref sets. The scanner yaml.Unmarshals
	// each body into a top-level `zerops:` mapping, so concatenating two
	// bodies produces a duplicate-key document the parser rejects — the old
	// merge-then-scan returned an empty ref set for any multi-runtime +
	// separate-repo launch, blinding detectDroppedEnvReferences and
	// emitting false "nothing references ${x_*}" warnings.
	zeropsRefs := collectZeropsYAMLRunEnvRefs(inputs.Runtimes)
	bundle.Warnings = append(bundle.Warnings, detectDroppedEnvReferences(inputs.ProjectEnvs, classifications, zeropsRefs)...)

	// PR-4: a promoted managed dep no runtime references via ${host_*} is
	// unreachable under the default service isolation. Scan run.envVariables
	// refs ∪ kept project-env refs so wiring via either surface counts.
	// The structured per-dep state lands on the bundle; the warning text
	// derives from it (single owner of the prefix-match).
	allEnvRefs := unionEnvRefs(zeropsRefs, projectEnvs)
	bundle.ManagedDeps = ManagedDepReferences(inputs.ManagedServices, allEnvRefs)
	bundle.Warnings = append(bundle.Warnings, unreferencedManagedDepWarnings(bundle.ManagedDeps)...)

	keepNonHASet := make(map[string]bool, len(inputs.KeepNonHA))
	for _, h := range inputs.KeepNonHA {
		keepNonHASet[h] = true
	}
	excludeSet := make(map[string]bool, len(inputs.ExcludeManaged))
	for _, h := range inputs.ExcludeManaged {
		excludeSet[h] = true
	}

	services := make([]any, 0, len(inputs.Runtimes)+len(inputs.ManagedServices))
	allServiceSecrets := map[string]string{}
	for _, r := range inputs.Runtimes {
		entry, svcWarnings := runtimeEntryFromInput(r, classifications)
		bundle.Warnings = append(bundle.Warnings, svcWarnings...)
		if es, ok := entry["envSecrets"].(map[string]string); ok {
			maps.Copy(allServiceSecrets, es)
		}
		services = append(services, entry)
	}
	for _, m := range dedupeManagedByHostname(inputs.ManagedServices) {
		if excludeSet[m.Hostname] {
			// Gap plan P2.0: an explicit per-dep exclusion replaces the
			// provision-then-destroy workaround (launch with the unwanted
			// dep → prod-ops delete-service).
			bundle.Warnings = append(bundle.Warnings, fmt.Sprintf("managed dep %q EXCLUDED from the production bundle by user decision", m.Hostname))
			continue
		}
		entry := managedEntryWithRules(m, true /*launch*/, keepNonHASet[m.Hostname])
		services = append(services, entry)
	}

	doc := map[string]any{
		"services": services,
	}
	if variant == VariantLaunchNew {
		corePackage := strings.TrimSpace(inputs.CorePackage)
		if corePackage == "" {
			corePackage = productionDefaultCorePackage
		}
		location := strings.TrimSpace(inputs.Location)
		if location == "" {
			location = productionDefaultLocation
		}
		project := map[string]any{
			"name":        inputs.TargetProjectName,
			"tags":        composeLaunchTags(inputs.SourceProjectID, inputs.AdditionalTags),
			"corePackage": corePackage,
			"location":    location,
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
	body = addPreprocessorHeader(body, projectEnvs, allServiceSecrets)

	bundle.ImportYAML = body

	// Structure-only: promoted runtime types come from the source project's
	// live Discover (already platform-valid) — see ValidateImportYAMLStructure.
	if errs := schema.ValidateImportYAMLStructure(body); len(errs) > 0 {
		bundle.Errors = errs
	}

	bundle.SourceSnapshot = computeSourceSnapshotMulti(inputs)
	return bundle, nil
}

// runtimeEntryFromInput renders one services[] entry from a per-runtime
// LaunchRuntimeInput. Centralized so the YAML field shape stays
// consistent between every promoted runtime in the bundle. classifications
// buckets the per-runtime ServiceEnvs into the runtime's envSecrets
// (GAP0-1); svcWarnings carries any per-env review advisories.
func runtimeEntryFromInput(r LaunchRuntimeInput, classifications map[string]topology.SecretClassification) (map[string]any, []string) {
	var warnings []string
	// Pipeline-first (plans/launch-pipeline-first-2026-06-11.md): the
	// production import never carries buildFromGit — the platform's
	// credential-less clone takes public repos only and fails private ones
	// with no logs. Runtimes start empty (startWithoutCode keeps the
	// service ACTIVE with a booted container) and the first production
	// build arrives through the production pipeline, like every later one.
	// r.RepoURL stays a required INPUT: it feeds the pipeline wiring
	// (secret command, webhook repositoryFullName), not the YAML.
	// No zeropsSetup either: the import API groups it under
	// pipelineConfig and rejects it without buildFromGit ("parameter is
	// required for use of pipelineConfig", live-verified 2026-06-11).
	// r.SetupName feeds the pipeline wiring instead (zcli push --setup
	// in the actions workflow, zeropsYamlSetup in the webhook
	// recommendation) — the import carries only the empty runtime.
	// No `mode`: runtimes are always HA on the platform (a mode/variant on a
	// runtime is ignored), so emitting a legacy `mode: NON_HA` is vestigial
	// noise — replica count is the minContainers axis below.
	entry := map[string]any{
		"hostname":         r.ProdHostname,
		"type":             r.ServiceType,
		"startWithoutCode": true,
	}
	// Replace-acked conflicts: the platform overwrites an existing service
	// only when override is set on the entry.
	if r.Override {
		entry["override"] = true
	}
	// Reflect the live source scaling (identity), then apply the named
	// production transforms below — never a silent override.
	if r.Scaling != nil {
		projectScaling(entry, r.Scaling) // minContainers/maxContainers + verticalAutoscaling bounds
	}

	// Transform 1 — minContainers: CONSENT over floor (gap plan P2.1).
	// A consented value (user explicitly chose, incl. 1) is honored
	// verbatim — the rubric reports a consented sub-floor as warn, never
	// silently raises it. Without consent the production HA floor of 2
	// applies as the DEFAULT, reflect-with-floor: keep a higher source
	// count; raise a lower one with a NAMED warning (incl. the
	// no-scaling-snapshot case, which used to floor silently).
	var minContainers int
	switch {
	case r.MinContainers > 0:
		minContainers = r.MinContainers
		if minContainers < runtimeProductionMinContainers {
			warnings = append(warnings, fmt.Sprintf("prod consent: %s minContainers=%d (below the HA floor of 2 by explicit user decision — no failover, brief downtime per deploy)", r.ProdHostname, minContainers))
		}
	default:
		minContainers = runtimeProductionMinContainers
		cur, ok := entry["minContainers"].(int)
		switch {
		case ok && cur > minContainers:
			minContainers = cur
		case ok && cur > 0 && cur < runtimeProductionMinContainers:
			warnings = append(warnings, fmt.Sprintf("prod policy: %s minContainers %d→%d (HA floor)", r.ProdHostname, cur, runtimeProductionMinContainers))
		case !ok:
			warnings = append(warnings, fmt.Sprintf("prod policy: %s minContainers defaulted to %d (HA floor; source scaling snapshot unavailable)", r.ProdHostname, runtimeProductionMinContainers))
		}
	}
	entry["minContainers"] = minContainers
	if r.MaxContainers > 0 {
		entry["maxContainers"] = r.MaxContainers
	}

	// Keep the scaling interval valid: a source maxContainers below the floored
	// minContainers (e.g. source 1/1 → min floored to 2) is an invalid interval
	// the platform rejects. Raise maxContainers to the floor with a named warning
	// rather than emit min>max.
	if mc, ok := entry["maxContainers"].(int); ok && mc < minContainers {
		warnings = append(warnings, fmt.Sprintf("prod policy: %s maxContainers %d→%d (kept ≥ minContainers HA floor)", r.ProdHostname, mc, minContainers))
		entry["maxContainers"] = minContainers
	}

	// Transform 2 — cpuMode DEDICATED for production (a named transform: warn
	// when it overrides a non-DEDICATED source rather than silently flipping it).
	va, _ := entry["verticalAutoscaling"].(map[string]any)
	if va == nil {
		va = map[string]any{}
	}
	if prev, ok := va["cpuMode"].(string); ok && prev != "" && prev != runtimeProductionCPUMode {
		warnings = append(warnings, fmt.Sprintf("prod policy: %s cpuMode %s→%s", r.ProdHostname, prev, runtimeProductionCPUMode))
	}
	va["cpuMode"] = runtimeProductionCPUMode
	entry["verticalAutoscaling"] = va

	svcSecrets, svcWarnings := composeServiceEnvSecrets(r.ServiceEnvs, classifications)
	warnings = append(warnings, svcWarnings...)
	if len(svcSecrets) > 0 {
		entry["envSecrets"] = svcSecrets
	}
	return entry, warnings
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

// collectZeropsYAMLRunEnvRefs scans each distinct runtime zerops.yaml body
// independently and unions the run.envVariables ${...} references. Bodies
// are deduped (monorepo: all runtimes share one body) so a shared body is
// scanned once. Scanning per-body — rather than concatenating — is required
// because each body is its own YAML document with a top-level `zerops:` key.
func collectZeropsYAMLRunEnvRefs(runtimes []LaunchRuntimeInput) map[string]bool {
	refs := map[string]bool{}
	seen := make(map[string]bool, len(runtimes))
	for _, r := range runtimes {
		if r.ZeropsYAMLBody == "" || seen[r.ZeropsYAMLBody] {
			continue
		}
		seen[r.ZeropsYAMLBody] = true
		for name := range extractZeropsYAMLRunEnvRefs(r.ZeropsYAMLBody) {
			refs[name] = true
		}
	}
	return refs
}
