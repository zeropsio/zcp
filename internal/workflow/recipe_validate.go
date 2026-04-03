package workflow

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// slugPattern validates recipe slug format: {framework}-hello-world or {framework}-showcase.
var slugPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*-(hello-world|showcase)$`)

// ValidateRecipePlan validates a recipe plan against structural rules and optionally
// against live service types from the Zerops API catalog.
// Returns a slice of validation errors (empty = valid).
func ValidateRecipePlan(plan RecipePlan, liveTypes []platform.ServiceStackType) []string {
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
		errs = append(errs, fmt.Sprintf("slug %q must match pattern {framework}-hello-world or {framework}-showcase", plan.Slug))
	}

	// RuntimeType.
	if plan.RuntimeType == "" {
		errs = append(errs, "runtimeType is required")
	} else if liveTypes != nil && !typeExists(plan.RuntimeType, liveTypes) {
		errs = append(errs, fmt.Sprintf("runtimeType %q not found in available service types", plan.RuntimeType))
	}

	// BuildBases: best-effort validation. Build bases (zerops.yml build.base values)
	// may include build-only types not present as service stack types in the catalog.
	// Only warn if the base name prefix doesn't match any known stack type.
	if liveTypes != nil {
		for _, bb := range plan.BuildBases {
			base, _, _ := strings.Cut(bb, "@")
			found := false
			for _, st := range liveTypes {
				if st.Name == base {
					found = true
					break
				}
			}
			if !found {
				errs = append(errs, fmt.Sprintf("buildBase %q: base name %q not found in available service types", bb, base))
			}
		}
	}

	// Research fields — required for all tiers.
	errs = append(errs, validateResearchFields(plan.Research, plan.Tier, plan.RuntimeType)...)

	// Targets validation.
	if len(plan.Targets) == 0 {
		errs = append(errs, "at least one target is required")
	}
	for i, t := range plan.Targets {
		if t.Hostname == "" {
			errs = append(errs, fmt.Sprintf("target[%d]: hostname is required", i))
		}
		if t.Type == "" {
			errs = append(errs, fmt.Sprintf("target[%d]: type is required", i))
		}
		if t.Role == "" {
			errs = append(errs, fmt.Sprintf("target[%d]: role is required", i))
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
	}
	for _, c := range checks {
		if strings.TrimSpace(c.value) == "" {
			missing = append(missing, c.name)
		}
	}
	return missing
}
