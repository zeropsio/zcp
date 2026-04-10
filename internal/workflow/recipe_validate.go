package workflow

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/schema"
)

// slugPattern validates recipe slug format:
// {runtime}-hello-world (bare runtime), {framework}-minimal, or {framework}-showcase.
var slugPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*-(hello-world|minimal|showcase)$`)

// ValidateRecipePlan validates a recipe plan against structural rules.
// Uses live JSON schemas (preferred) or API service types (fallback) for type validation.
// Returns a slice of validation errors (empty = valid).
func ValidateRecipePlan(plan RecipePlan, liveTypes []platform.ServiceStackType, schemas *schema.Schemas) []string {
	var errs []string

	// Framework.
	if plan.Framework == "" {
		errs = append(errs, "framework is required")
	}

	// Tier.
	if plan.Tier != RecipeTierMinimal && plan.Tier != RecipeTierShowcase {
		errs = append(errs, fmt.Sprintf("tier must be %q or %q, got %q", RecipeTierMinimal, RecipeTierShowcase, plan.Tier))
	}

	// Slug format.
	if plan.Slug == "" {
		errs = append(errs, "slug is required")
	} else if !slugPattern.MatchString(plan.Slug) {
		errs = append(errs, fmt.Sprintf("slug %q must match pattern {runtime}-hello-world, {framework}-minimal, or {framework}-showcase", plan.Slug))
	}

	// RuntimeType — validate against schema run.base enum, then import types, then liveTypes.
	if plan.RuntimeType == "" {
		errs = append(errs, "runtimeType is required")
	} else {
		errs = append(errs, validateRuntimeType(plan.RuntimeType, schemas, liveTypes)...)
	}

	// BuildBases — validate against schema build.base enum, falling back to liveTypes.
	errs = append(errs, validateBuildBases(plan.BuildBases, schemas, liveTypes)...)

	// Targets — validate against schema import service types when available.
	errs = append(errs, validateTargets(plan.Targets, schemas)...)

	// Worker codebase references — every SharesCodebaseWith must resolve to a
	// valid host target. Runs unconditionally (applies to minimal tier too,
	// even though workers are showcase-only — cheap to verify, and if a
	// minimal plan sneaks in a worker, we still want a clean error).
	errs = append(errs, validateWorkerCodebaseRefs(plan.Targets)...)

	// Showcase-specific: enforce all required service kinds.
	if plan.Tier == RecipeTierShowcase {
		errs = append(errs, validateShowcaseServices(plan.Targets)...)
	}

	// Research fields — required for all tiers.
	errs = append(errs, validateResearchFields(plan.Research, plan.Tier, plan.RuntimeType)...)

	return errs
}

// validateRuntimeType checks the runtime type against schema enums or liveTypes.
func validateRuntimeType(rt string, schemas *schema.Schemas, liveTypes []platform.ServiceStackType) []string {
	// Prefer schema: check import.yaml service types (authoritative for what can be created).
	if schemas != nil && schemas.ImportYml != nil {
		if !schemas.ImportYml.ServiceTypeSet()[rt] {
			return []string{fmt.Sprintf("runtimeType %q not found in available service types (schema)", rt)}
		}
		return nil
	}
	// Fallback: liveTypes from API.
	if liveTypes != nil && !typeExists(rt, liveTypes) {
		return []string{fmt.Sprintf("runtimeType %q not found in available service types", rt)}
	}
	return nil
}

// validateBuildBases checks build bases against schema build.base enum or liveTypes.
// liveTypes fallback: scans Version.Name (not ServiceStackType.Name) because build bases
// like "php@8.4" appear as version names under BUILD-category types (e.g., "zbuild php"),
// not as top-level type names.
func validateBuildBases(bases []string, schemas *schema.Schemas, liveTypes []platform.ServiceStackType) []string {
	if len(bases) == 0 {
		return nil
	}

	// Prefer schema: zerops.yaml build.base enum is the authoritative list.
	if schemas != nil && schemas.ZeropsYml != nil {
		baseSet := schemas.ZeropsYml.BuildBaseSet()
		var errs []string
		for _, bb := range bases {
			base, _, _ := strings.Cut(bb, "@")
			if !baseSet[base] {
				errs = append(errs, fmt.Sprintf("buildBase %q: base name %q not found in zerops.yaml schema", bb, base))
			}
		}
		return errs
	}

	// Fallback: check version name bases across all API types.
	if liveTypes == nil {
		return nil
	}
	var errs []string
	for _, bb := range bases {
		base, _, _ := strings.Cut(bb, "@")
		found := false
		for _, st := range liveTypes {
			for _, v := range st.Versions {
				vBase, _, _ := strings.Cut(v.Name, "@")
				if vBase == base {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			errs = append(errs, fmt.Sprintf("buildBase %q: base name %q not found in available service types", bb, base))
		}
	}
	return errs
}

// validateTargets checks target fields and optionally types against schema.
func validateTargets(targets []RecipeTarget, schemas *schema.Schemas) []string {
	if len(targets) == 0 {
		return []string{"at least one target is required"}
	}

	var svcTypeSet map[string]bool
	if schemas != nil && schemas.ImportYml != nil {
		svcTypeSet = schemas.ImportYml.ServiceTypeSet()
	}

	var errs []string
	for i, t := range targets {
		if t.Hostname == "" {
			errs = append(errs, fmt.Sprintf("target[%d]: hostname is required", i))
		}
		if t.Type == "" {
			errs = append(errs, fmt.Sprintf("target[%d]: type is required", i))
			continue
		}
		if svcTypeSet != nil && !svcTypeSet[t.Type] {
			errs = append(errs, fmt.Sprintf("target[%d]: type %q not found in import.yaml schema", i, t.Type))
		}
		// Managed and utility services must resolve to a serviceTypeKind so
		// comments render correctly. If a new managed type is added to
		// managedServicePrefixes without a matching entry in serviceTypeKind,
		// this fails at plan submission instead of at comment-generation.
		if (IsManagedService(t.Type) || IsUtilityType(t.Type)) && serviceTypeKind(t.Type) == "" {
			errs = append(errs, fmt.Sprintf("target[%d]: service type %q has no serviceTypeKind — add it to recipe_service_types.go (this is a tool bug, not an agent error)", i, t.Type))
		}
	}
	return errs
}

// validateWorkerCodebaseRefs enforces the semantics of RecipeTarget.SharesCodebaseWith.
// Rules:
//  1. SharesCodebaseWith is only meaningful for workers (IsWorker=true). If a
//     non-worker target sets it, reject — the field has no meaning there and
//     silently ignoring is a footgun (the agent will think it took effect).
//  2. A non-empty SharesCodebaseWith must reference an existing target in the
//     plan. Dangling references are rejected.
//  3. The referenced target must NOT itself be a worker (chain sharing is
//     invalid — there's no codebase behind a worker to share with).
//  4. The referenced target must be a runtime type (sharing with a database
//     or object-storage makes no sense).
//  5. The referenced target's base runtime must match this worker's base
//     runtime. You cannot share code between, e.g., a Node.js app and a Python
//     worker — they're different codebases by definition.
//
// This is the single source of truth for "is this shared-codebase link valid?"
// The template layer (runtimeRepoSuffix, TargetHostsSharedWorker) trusts the
// validation result and never re-checks.
func validateWorkerCodebaseRefs(targets []RecipeTarget) []string {
	if len(targets) == 0 {
		return nil
	}

	// Index targets by hostname for O(1) lookup. We build the index once per
	// plan so a plan with N shared workers is still O(N) total.
	byName := make(map[string]RecipeTarget, len(targets))
	for _, t := range targets {
		byName[t.Hostname] = t
	}

	var errs []string
	for i, t := range targets {
		// Rule 1: non-worker with SharesCodebaseWith set.
		if !t.IsWorker && t.SharesCodebaseWith != "" {
			errs = append(errs, fmt.Sprintf(
				"target[%d] %q: sharesCodebaseWith is only valid on worker targets (isWorker=true)",
				i, t.Hostname))
			continue
		}
		if t.SharesCodebaseWith == "" {
			continue // separate codebase — nothing to validate
		}

		// Rule 2: reference must exist.
		host, ok := byName[t.SharesCodebaseWith]
		if !ok {
			errs = append(errs, fmt.Sprintf(
				"target[%d] %q: sharesCodebaseWith references unknown target %q",
				i, t.Hostname, t.SharesCodebaseWith))
			continue
		}

		// Rule 3: host must not itself be a worker.
		if host.IsWorker {
			errs = append(errs, fmt.Sprintf(
				"target[%d] %q: sharesCodebaseWith points at another worker %q — workers cannot host workers",
				i, t.Hostname, host.Hostname))
			continue
		}

		// Rule 4: host must be a runtime type.
		if !IsRuntimeType(host.Type) {
			errs = append(errs, fmt.Sprintf(
				"target[%d] %q: sharesCodebaseWith points at non-runtime target %q (type %q) — only runtime targets host codebases",
				i, t.Hostname, host.Hostname, host.Type))
			continue
		}

		// Rule 5: base runtime must match (no cross-language sharing).
		workerBase, _, _ := strings.Cut(t.Type, "@")
		hostBase, _, _ := strings.Cut(host.Type, "@")
		if workerBase != hostBase {
			errs = append(errs, fmt.Sprintf(
				"target[%d] %q: sharesCodebaseWith %q has base runtime %q but this worker is %q — shared codebases must use the same base runtime",
				i, t.Hostname, host.Hostname, hostBase, workerBase))
		}
	}
	return errs
}

// validateShowcaseServices checks that showcase recipes include all required service kinds:
// an HTTP app, a worker, and one each of database, cache, storage, search engine, messaging.
// Dual-runtime showcases (frontend + API) have two non-worker runtimes — this is valid.
//
// kindMessaging is required as of the NATS-first showcase rework: every showcase
// now provisions a dedicated broker (NATS by default — see choose-queue knowledge
// decision). The broker is the worker's queue source and also the test surface
// for the "dispatch-a-job" feature on the dashboard. Collapsing messaging into
// the cache service (Redis polymorphism) is no longer acceptable — it produced
// fuzzy showcases where the dashboard demoed the "queue" section without a real
// broker in the topology.
func validateShowcaseServices(targets []RecipeTarget) []string {
	requiredKinds := map[string]bool{
		kindDatabase:     false,
		kindCache:        false,
		kindStorage:      false,
		kindSearchEngine: false,
		kindMessaging:    false,
	}
	hasApp, hasWorker := false, false

	for _, t := range targets {
		if IsRuntimeType(t.Type) {
			if t.IsWorker {
				hasWorker = true
			} else {
				hasApp = true
			}
		}
		if kind := serviceTypeKind(t.Type); kind != "" {
			requiredKinds[kind] = true
		}
	}

	var errs []string
	if !hasApp {
		errs = append(errs, "showcase requires at least one runtime app target (isWorker=false)")
	}
	if !hasWorker {
		errs = append(errs, "showcase requires at least one runtime worker target (isWorker=true)")
	}
	for kind, found := range requiredKinds {
		if !found {
			errs = append(errs, fmt.Sprintf("showcase requires a %s service", kind))
		}
	}
	return errs
}

// hasImplicitWebServer returns true for runtime types where nginx/apache manages HTTP
// and no explicit start command is needed (php-nginx, php-apache, nginx, static).
func hasImplicitWebServer(runtimeType string) bool {
	base, _, _ := strings.Cut(runtimeType, "@")
	switch base {
	case "php-nginx", "php-apache", "nginx", "static":
		return true
	}
	return false
}

// validateResearchFields checks that all required research fields are present.
func validateResearchFields(r ResearchData, tier, runtimeType string) []string {
	var errs []string

	if r.ServiceType == "" {
		errs = append(errs, "research.serviceType is required")
	}
	if r.PackageManager == "" {
		errs = append(errs, "research.packageManager is required")
	}
	if r.HTTPPort == 0 {
		errs = append(errs, "research.httpPort is required")
	}
	if len(r.BuildCommands) == 0 {
		errs = append(errs, "research.buildCommands is required (at least one)")
	}
	if len(r.DeployFiles) == 0 {
		errs = append(errs, "research.deployFiles is required (at least one)")
	}
	if r.StartCommand == "" && !hasImplicitWebServer(runtimeType) {
		errs = append(errs, "research.startCommand is required")
	}

	// Showcase-specific fields.
	if tier == RecipeTierShowcase {
		missing := showcaseMissing(r)
		for _, field := range missing {
			errs = append(errs, fmt.Sprintf("research.%s is required for showcase tier", field))
		}
	}

	return errs
}

// showcaseMissing returns the names of showcase-required fields that are empty.
func showcaseMissing(r ResearchData) []string {
	var missing []string
	checks := []struct {
		name  string
		value string
	}{
		{"cacheLib", r.CacheLib},
		{"sessionDriver", r.SessionDriver},
		{"queueDriver", r.QueueDriver},
		{"storageDriver", r.StorageDriver},
		{"searchLib", r.SearchLib},
	}
	for _, c := range checks {
		if strings.TrimSpace(c.value) == "" {
			missing = append(missing, c.name)
		}
	}
	return missing
}
