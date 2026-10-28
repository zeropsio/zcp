package bundle

import (
	"fmt"
	"maps"
	"slices"
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

// LaunchMarker is the production project's description a launch creates it
// with: what tells the launch's own project from any other of the same name
// when the call that created it ended before recording it.
func LaunchMarker(launchID string) string {
	return "Created by zcp launch-production " + launchID
}

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
//  3. Classify project envs via composeProjectVault.
//  4. Loop runtimes — one services[] entry per LaunchRuntimeInput with
//     startWithoutCode + minContainers (pipeline-first: no buildFromGit
//     and no zeropsSetup — both are pipelineConfig, which the import API
//     rejects without a repo; the first prod build arrives via the
//     production pipeline, which owns the setup reference).
//  5. Append managed deps (deduplicated by hostname so shared infra
//     across multiple promoted runtimes lands once) with HA promotion
//     per ServiceTypeRules; opt-out via KeepNonHA. Then every Zerops Box a
//     launched runtime reads (P-LP-19) — never a pool — after refusing a
//     launch whose runtime reads a source service the bundle does not
//     carry, and a launch-new Box outside the regions a Box exists in.
//  6. Compose project block — name + tags + vault (omitted
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
	if err := refuseNonRuntimeLaunchRuntimes(inputs.Runtimes); err != nil {
		return nil, err
	}
	managedServices, poolsAndBoxes := splitNotCarriedServices(inputs.ManagedServices)
	inputs.ManagedServices = managedServices
	var setupAdoptions []string
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
		resolvedSetup, adopted, err := reconcileZeropsYAMLSetup(r.ZeropsYAMLBody, r.SetupName)
		if err != nil {
			return nil, fmt.Errorf("launch bundle: runtime[%d] (%s): %w", i, r.ProdHostname, err)
		}
		if adopted {
			setupAdoptions = append(setupAdoptions, fmt.Sprintf("runtime %q: requested prod setup %q is not in zerops.yaml; adopted the only declared setup %q", r.ProdHostname, r.SetupName, resolvedSetup))
			inputs.Runtimes[i].SetupName = resolvedSetup
		}
	}

	variant := inputs.Variant
	if !variant.IsLaunch() {
		variant = VariantLaunchNew
	}
	excludeSet := make(map[string]bool, len(inputs.ExcludeManaged))
	for _, h := range inputs.ExcludeManaged {
		excludeSet[h] = true
	}
	carry, err := launchReads(inputs, classifications, variant, poolsAndBoxes, excludeSet)
	if err != nil {
		return nil, err
	}

	bundle := &LaunchBundle{
		TargetProjectName: inputs.TargetProjectName,
		SourceProjectID:   inputs.SourceProjectID,
		Classifications:   classifications,
		Boxes:             carry.boxes,
	}

	bundle.Warnings = append(bundle.Warnings, setupAdoptions...)
	bundle.Warnings = append(bundle.Warnings, carry.warnings...)

	projectVault, envWarnings := composeProjectVault(inputs.ProjectEnvs, classifications)
	bundle.Warnings = append(bundle.Warnings, envWarnings...)

	// Cross-service env refs scan: scan EACH distinct runtime zerops.yaml
	// body separately and union the ref sets. The scanner yaml.Unmarshals
	// each body into a top-level `zerops:` mapping, so concatenating two
	// bodies produces a duplicate-key document the parser rejects — the old
	// merge-then-scan returned an empty ref set for any multi-runtime +
	// separate-repo launch, blinding detectDroppedEnvReferences and
	// emitting false "nothing references ${x_*}" warnings.
	zeropsRefs := collectZeropsYAMLRunEnvRefs(inputs.Runtimes)
	volumeHosts := collectZeropsYAMLRunVolumeHosts(inputs.Runtimes)
	bundle.Warnings = append(bundle.Warnings, detectDroppedEnvReferences(inputs.ProjectEnvs, classifications, zeropsRefs)...)

	// PR-4: a promoted managed dep no runtime references via ${host_*} is
	// unreachable under the default service isolation. Scan run.envVariables
	// refs ∪ kept project-env refs so wiring via either surface counts.
	// The structured per-dep state lands on the bundle; the warning text
	// derives from it (single owner of the prefix-match).
	allEnvRefs := unionEnvRefs(zeropsRefs, projectVault)
	bundle.ManagedDeps = ManagedDepReferences(inputs.ManagedServices, allEnvRefs, volumeHosts)
	bundle.Warnings = append(bundle.Warnings, unreferencedManagedDepWarnings(bundle.ManagedDeps)...)

	keepNonHASet := make(map[string]bool, len(inputs.KeepNonHA))
	for _, h := range inputs.KeepNonHA {
		keepNonHASet[h] = true
	}
	haIncapableSet := make(map[string]bool, len(inputs.HAIncapable))
	for _, h := range inputs.HAIncapable {
		haIncapableSet[h] = true
	}

	services := make([]any, 0, len(inputs.Runtimes)+len(inputs.ManagedServices)+len(carry.entries))
	allServiceVaults := map[string]vaultValue{}
	for _, r := range inputs.Runtimes {
		entry, svcWarnings := runtimeEntryFromInput(r, classifications)
		bundle.Warnings = append(bundle.Warnings, svcWarnings...)
		if vault, ok := entry["vault"].(map[string]vaultValue); ok {
			maps.Copy(allServiceVaults, vault)
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
		// Keep single-node when the user opted out (KeepNonHA) OR the type has
		// no `:ha` variant on the platform (HAIncapable). The latter warns: the
		// operator gets HA everywhere it's possible, and a clear reason where it
		// isn't — instead of a silent platform import rejection on a fabricated
		// `<type>:ha`.
		keepSingle := keepNonHASet[m.Hostname]
		if !keepSingle && haIncapableSet[m.Hostname] {
			keepSingle = true
			bundle.Warnings = append(bundle.Warnings, fmt.Sprintf(
				"managed dep %q (%s) kept single-node — the platform has no HA variant for this type",
				m.Hostname, m.Type))
		}
		entry := managedEntryWithRules(m, true /*launch*/, keepSingle)
		services = append(services, entry)
	}
	services = append(services, carry.entries...)

	doc := map[string]any{
		"services": services,
	}
	if variant == VariantLaunchNew {
		corePackage := strings.TrimSpace(inputs.CorePackage)
		if corePackage == "" {
			corePackage = productionDefaultCorePackage
		}
		projectBlock := map[string]any{
			"name":        inputs.TargetProjectName,
			"tags":        composeLaunchTags(inputs.SourceProjectID, inputs.AdditionalTags),
			"corePackage": corePackage,
			"location":    launchLocation(inputs.Location),
		}
		if inputs.LaunchID != "" {
			projectBlock["description"] = LaunchMarker(inputs.LaunchID)
		}
		if len(projectVault) > 0 {
			projectBlock["vault"] = projectVault
		}
		doc["project"] = projectBlock
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
	body = addPreprocessorHeader(body, projectVault, allServiceVaults)

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
// buckets the per-runtime ServiceEnvs into the runtime's vault
// (GAP0-1); svcWarnings carries any per-env review advisories.
func runtimeEntryFromInput(r LaunchRuntimeInput, classifications map[string]topology.SecretClassification) (map[string]any, []string) {
	var warnings []string
	// Pipeline-first (plans/archive/launch-pipeline-first-2026-06-11.md): the
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

	svcVault, svcWarnings := composeServiceVault(r.ServiceEnvs, classifications)
	warnings = append(warnings, svcWarnings...)
	if len(svcVault) > 0 {
		entry["vault"] = svcVault
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

func collectZeropsYAMLRunVolumeHosts(runtimes []LaunchRuntimeInput) map[string]bool {
	hosts := map[string]bool{}
	seen := make(map[string]bool, len(runtimes))
	for _, r := range runtimes {
		if r.ZeropsYAMLBody == "" || seen[r.ZeropsYAMLBody] {
			continue
		}
		seen[r.ZeropsYAMLBody] = true
		for hostname := range extractZeropsYAMLRunVolumeHosts(r.ZeropsYAMLBody) {
			hosts[hostname] = true
		}
	}
	return hosts
}

// splitNotCarriedServices separates the Swarm pools and Zerops Boxes from the
// managed dependencies the composer promotes. Neither is promoted as one: a
// pool is never carried — its orchestrator and containers are no deploy
// target, so the production import must declare one itself — and a Box is
// carried only where a launched runtime reads it (launchBoxes).
func splitNotCarriedServices(in []ManagedServiceEntry) (managed, poolsAndBoxes []ManagedServiceEntry) {
	for _, m := range in {
		if topology.IsPoolOrSandboxType(m.Type) {
			poolsAndBoxes = append(poolsAndBoxes, m)
			continue
		}
		managed = append(managed, m)
	}
	return managed, poolsAndBoxes
}

// launchProjectServices is every service the source project runs, by
// hostname: ProjectHostnames, and every managed dependency, pool and Box
// handed in — whether listed there or not.
func launchProjectServices(inputs LaunchBundleInputs, poolsAndBoxes []ManagedServiceEntry) map[string]bool {
	project := map[string]bool{}
	for _, host := range inputs.ProjectHostnames {
		if host = strings.TrimSpace(host); host != "" {
			project[host] = true
		}
	}
	for _, m := range append(slices.Clone(inputs.ManagedServices), poolsAndBoxes...) {
		if host := strings.TrimSpace(m.Hostname); host != "" {
			project[host] = true
		}
	}
	return project
}

// launchCarry is what launchReads hands BuildLaunch: the services[] entries
// of the Boxes the bundle carries, their hostnames, and the warnings naming
// the pools and Boxes it leaves out (PS-3) and the Boxes it carries.
type launchCarry struct {
	entries  []any
	boxes    []string
	warnings []string
}

// launchReads is P-LP-19 over the launched runtimes, read as a group recipe
// tier's are (group_reads.go): the Boxes they read, carried — or the refusal
// of a launch whose runtime reads a service the source project runs that
// the bundle does not carry, or of a launch-new Box outside the regions a
// Box exists in, before anything is created. inputs.ManagedServices holds
// the managed dependencies alone; poolsAndBoxes the rest.
func launchReads(
	inputs LaunchBundleInputs,
	classifications map[string]topology.SecretClassification,
	variant Variant,
	poolsAndBoxes []ManagedServiceEntry,
	excluded map[string]bool,
) (launchCarry, error) {
	project := launchProjectServices(inputs, poolsAndBoxes)
	reader := newServiceReader(slices.Sorted(maps.Keys(project)))
	readers := launchReaders(inputs.Runtimes, classifications)
	boxes, notCarried := launchBoxes(reader, readers, poolsAndBoxes, excluded)
	if err := launchClosure(reader, readers, inputs, boxes, poolsAndBoxes, project, excluded); err != nil {
		return launchCarry{}, err
	}
	if variant == VariantLaunchNew {
		if err := launchBoxRegion(boxes, inputs.Location); err != nil {
			return launchCarry{}, err
		}
	}
	// A carried Box as a Box import takes it, under the launch's rules: no
	// build (pipeline-first) and no public access (P-PROD-2) — the source's
	// subdomain is never handed in, so none is written.
	carry := launchCarry{
		entries:  make([]any, 0, len(boxes)),
		boxes:    make([]string, 0, len(boxes)),
		warnings: append(notCarriedWarnings(notCarried), carriedBoxWarnings(boxes)...),
	}
	for _, b := range boxes {
		carry.entries = append(carry.entries, boxImportEntry(GroupBox{Hostname: b.service.Hostname, ServiceType: b.service.Type}))
		carry.boxes = append(carry.boxes, b.service.Hostname)
	}
	return carry, nil
}

// launchReaders is each launched runtime as a tier's reader (tierReader):
// the setup the launch builds for it, in its zerops.yaml, and the values its
// own variables are written with into its production vault — a placeholder
// or a generated secret names nothing.
func launchReaders(runtimes []LaunchRuntimeInput, classifications map[string]topology.SecretClassification) []tierReader {
	readers := make([]tierReader, 0, len(runtimes))
	for _, r := range runtimes {
		vault, _ := composeServiceVault(r.ServiceEnvs, classifications)
		values := make([]string, 0, len(vault))
		for _, key := range slices.Sorted(maps.Keys(vault)) {
			values = append(values, vault[key].value)
		}
		readers = append(readers, tierReader{hostname: r.ProdHostname, setup: r.SetupName, body: r.ZeropsYAMLBody, vault: values})
	}
	return readers
}

// launchBox is a Zerops Box the bundle carries and the launched runtimes that
// read it, in launch order.
type launchBox struct {
	service ManagedServiceEntry
	readers []string
}

// launchBoxes is the Boxes among poolsAndBoxes a launched runtime reads,
// each once — through the setup the launch builds for it or a value of its
// own vault (tierReads) — and what the bundle leaves out, in input order:
// every pool, a Box nothing launched reads, and a Box the person excluded.
func launchBoxes(sr *serviceReader, readers []tierReader, poolsAndBoxes []ManagedServiceEntry, excluded map[string]bool) (boxes []launchBox, notCarried []ManagedServiceEntry) {
	read := tierReads(sr, readers)
	carried := map[string]bool{}
	for _, s := range poolsAndBoxes {
		if !topology.IsSandboxType(s.Type) || excluded[s.Hostname] || len(read[s.Hostname]) == 0 {
			notCarried = append(notCarried, s)
			continue
		}
		if !carried[s.Hostname] {
			carried[s.Hostname] = true
			boxes = append(boxes, launchBox{service: s, readers: read[s.Hostname]})
		}
	}
	return boxes, notCarried
}

// launchClosure refuses a launch whose runtime reads, through the setup the
// launch builds for it, a service the source project runs (project) that the
// bundle does not carry (closureGaps, PS-3a's check): production would start
// an app that cannot reach it. Carried are the launched runtimes under their
// production hostnames, the managed dependencies the person did not exclude,
// the Boxes the bundle carries, and what an existing production project
// already runs that the bundle leaves as it is (TargetHostnames).
func launchClosure(sr *serviceReader, readers []tierReader, inputs LaunchBundleInputs, boxes []launchBox, poolsAndBoxes []ManagedServiceEntry, project, excluded map[string]bool) error {
	carried := map[string]bool{}
	for _, r := range inputs.Runtimes {
		carried[r.ProdHostname] = true
	}
	for _, m := range inputs.ManagedServices {
		if !excluded[m.Hostname] {
			carried[m.Hostname] = true
		}
	}
	for _, b := range boxes {
		carried[b.service.Hostname] = true
	}
	for _, host := range inputs.TargetHostnames {
		carried[host] = true
	}
	pools := map[string]bool{}
	for _, s := range poolsAndBoxes {
		if topology.IsPoolType(s.Type) {
			pools[s.Hostname] = true
		}
	}
	gaps := closureGaps(sr, readers, carried, project)
	if len(gaps) == 0 {
		return nil
	}
	refusal := &LaunchClosureError{Gaps: make([]LaunchClosureGap, 0, len(gaps))}
	for _, gap := range gaps {
		var why string
		switch host := gap.read.host; {
		case excluded[host]:
			why = "which the launch excluded by user decision (managedDeps)"
		case pools[host]:
			why = "a Swarm pool, which a launch never carries"
		}
		refusal.Gaps = append(refusal.Gaps, LaunchClosureGap{Read: gap.String(), Service: gap.read.host, Why: why})
	}
	return refusal
}

// LaunchClosureError is BuildLaunch's refusal of a launch whose runtime
// reads, through the setup the launch builds for it, a service the source
// project runs that the bundle does not carry (P-LP-19). Each gap names a
// hostname production would have to run, so an existing project that runs
// it closes the gap (TargetHostnames) and a new project never does: a caller
// that does not know the production project yet tells it from the
// composer's other refusals by its type.
type LaunchClosureError struct {
	Gaps []LaunchClosureGap
}

// LaunchClosureGap is one read of a service the bundle does not carry.
type LaunchClosureGap struct {
	// Read is the read in the composer's words: `runtime "<host>" builds
	// setup "<setup>", whose <variable> names "<service>"`.
	Read string
	// Service is the hostname read.
	Service string
	// Why says why the bundle leaves it out when that is more than
	// production not running it: the person excluded it, or it is a pool.
	Why string
}

// String says the gap with why production lacks the service.
func (g LaunchClosureGap) String() string {
	if g.Why == "" {
		return g.Read + ", which production does not run under that name"
	}
	return g.Read + ", " + g.Why
}

func (e *LaunchClosureError) Error() string {
	lines := make([]string, 0, len(e.Gaps))
	for _, gap := range e.Gaps {
		lines = append(lines, gap.String())
	}
	return fmt.Sprintf(
		"launch bundle: a launched runtime reads a service the source project runs and the production bundle does not carry, so production would start an app that cannot reach it — nothing is launched until the bundle carries what its runtimes read: %s. Launch the runtime that serves it, include the managed dependency, or stop the setup reading it",
		strings.Join(lines, "; "))
}

// launchLocation is the production project's location: the one asked for,
// else productionDefaultLocation.
func launchLocation(location string) string {
	if location = strings.TrimSpace(location); location == "" {
		return productionDefaultLocation
	}
	return location
}

// launchBoxRegion refuses a launch-new bundle that carries a Box into a
// production project whose location no Box can be created in (PS-16): the
// launch's import is not zcp's zerops_import, which refuses it there, and
// the platform fails the Box's create with a capacity error and removes it.
func launchBoxRegion(boxes []launchBox, location string) error {
	location = launchLocation(location)
	if len(boxes) == 0 || topology.BoxLocationAllowed(location) {
		return nil
	}
	named := make([]string, 0, len(boxes))
	for _, b := range boxes {
		named = append(named, fmt.Sprintf("Zerops Box %q (%s it)", b.service.Hostname, readersOf(b)))
	}
	return fmt.Errorf(
		"launch bundle: the production bundle carries %s, and a Zerops Box can be created only in a project in region %s, but the production project's location is %q — launch into the default location %q, or stop the runtime reading the Box",
		strings.Join(named, ", "), strings.Join(topology.BoxRegions(), " or "), location, productionDefaultLocation)
}

// carriedBoxWarnings says of each Box the bundle carries which launched
// runtime reads it, and what the person still does in production: the
// launch import creates a Box with no base and no domain and does not set
// it up, and leaves its public access off (P-PROD-2).
func carriedBoxWarnings(boxes []launchBox) []string {
	out := make([]string, 0, len(boxes))
	for _, b := range boxes {
		out = append(out, fmt.Sprintf(
			"Zerops Box %q (%s) is carried into the production bundle: %s it. It comes up with no base and no domain, and its public access off — finish its setup in the production project with `zbox setup` before the app starts a sandbox there",
			b.service.Hostname, b.service.Type, readersOf(b)))
	}
	return out
}

// readersOf names the launched runtimes that read a Box, with the verb:
// `runtime "a" reads`, `runtimes "a" and "b" read`.
func readersOf(b launchBox) string {
	if len(b.readers) > 1 {
		return "runtimes " + quoteAnd(b.readers) + " read"
	}
	return "runtime " + quoteAnd(b.readers) + " reads"
}

// notCarriedKind is the human name of a service type the launch bundle does
// not carry: "Swarm pool" or "Zerops Box".
func notCarriedKind(serviceType string) string {
	if topology.IsPoolType(serviceType) {
		return "Swarm pool"
	}
	return "Zerops Box"
}

// refuseNonRuntimeLaunchRuntimes refuses a pool or Box passed as a launch
// runtime (PS-3): neither is an app, whatever the caller composed.
func refuseNonRuntimeLaunchRuntimes(runtimes []LaunchRuntimeInput) error {
	for _, r := range runtimes {
		if topology.IsPoolOrSandboxType(r.ServiceType) {
			return fmt.Errorf("launch bundle: runtime %q is a %s (%s), not an app runtime — a pool or Box cannot be a launch runtime", r.ProdHostname, notCarriedKind(r.ServiceType), r.ServiceType)
		}
	}
	return nil
}

// notCarriedWarnings names each pool / Box the bundle leaves out, so the
// omission is never silent (PS-3).
func notCarriedWarnings(notCarried []ManagedServiceEntry) []string {
	out := make([]string, 0, len(notCarried))
	for _, n := range notCarried {
		out = append(out, fmt.Sprintf(
			"%s %q (%s) is not carried into the production bundle — add it to the production import yourself",
			notCarriedKind(n.Type), n.Hostname, n.Type))
	}
	return out
}
