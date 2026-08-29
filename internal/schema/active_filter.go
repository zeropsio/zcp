package schema

import (
	"context"
	"slices"
	"strings"

	"github.com/zeropsio/zcp/internal/topology"
)

// ActiveVersionsProvider returns the platform's ACTIVE concrete and rolling
// service type versions. Runtime cache composition accepts it as a function so
// the schema package stays independent of the platform client package.
// Developer schema sync/check deliberately do not use this provider: their
// authority is the exact public schema, not runtime availability.
type ActiveVersionsProvider func(context.Context) ([]string, error)

// FilterToActive returns a copy of schemas whose import.yaml service type enum
// contains only concrete-versioned forms covered by the platform ACTIVE set.
// Versionless forms and rolling tags have no concrete activity identity and
// survive unchanged; this also preserves public storage aliases whose internal
// platform category name differs from their authored schema spelling.
//
// ZeropsYml build/run bases are not filtered. The ACTIVE endpoint describes
// service-stack versions, while the public zerops.yaml schema remains the
// authority for build.base and run.base membership.
func FilterToActive(schemas *Schemas, activeForms []string) *Schemas {
	if schemas == nil {
		return nil
	}
	out := copySchemas(schemas)
	if out.ImportYml == nil {
		return out
	}

	active := expandedActiveFormSet(activeForms)
	kept := make([]string, 0, len(out.ImportYml.ServiceTypes))
	for _, serviceType := range out.ImportYml.ServiceTypes {
		if active[serviceType] || !hasConcreteVersion(serviceType) {
			kept = append(kept, serviceType)
		}
	}
	out.ImportYml.ServiceTypes = kept
	return out
}

// hasConcreteVersion reports whether the enum member pins a concrete version.
// Versionless and rolling members have no activity semantics and bypass the
// runtime filter.
func hasConcreteVersion(serviceType string) bool {
	_, version, ok := strings.Cut(serviceType, "@")
	if !ok || version == "" {
		return false
	}
	return !isRollingVersion(version)
}

func expandedActiveFormSet(activeForms []string) map[string]bool {
	set := make(map[string]bool)
	for _, form := range activeForms {
		for _, expanded := range expandActiveForm(form) {
			set[expanded] = true
		}
	}
	return set
}

func expandActiveForm(form string) []string {
	form = strings.TrimSpace(form)
	if form == "" {
		return nil
	}
	name, version, ok := strings.Cut(form, "@")
	if !ok || version == "" {
		return uniqueForms(form, topology.CanonicalBareForm(form))
	}
	if isRollingVersion(version) {
		return uniqueForms(form, topology.CanonicalBareForm(form))
	}

	var out []string
	for _, prefix := range dotVersionPrefixes(version) {
		composite := name + "@" + prefix
		out = append(out, composite, topology.CanonicalBareForm(composite))
	}
	return uniqueForms(out...)
}

func dotVersionPrefixes(version string) []string {
	parts := strings.Split(version, ".")
	out := make([]string, 0, len(parts))
	for i := range parts {
		out = append(out, strings.Join(parts[:i+1], "."))
	}
	return out
}

func isRollingVersion(version string) bool {
	switch strings.ToLower(version) {
	case "canary", "nightly", "latest":
		return true
	default:
		return false
	}
}

func uniqueForms(forms ...string) []string {
	seen := make(map[string]bool, len(forms))
	out := make([]string, 0, len(forms))
	for _, form := range forms {
		form = strings.TrimSpace(form)
		if form == "" || seen[form] {
			continue
		}
		seen[form] = true
		out = append(out, form)
	}
	return out
}

func copySchemas(s *Schemas) *Schemas {
	out := &Schemas{}
	if s.ZeropsYml != nil {
		zy := *s.ZeropsYml
		zy.BuildBases = slices.Clone(s.ZeropsYml.BuildBases)
		zy.RunBases = slices.Clone(s.ZeropsYml.RunBases)
		out.ZeropsYml = &zy
	}
	if s.ImportYml != nil {
		iy := *s.ImportYml
		iy.ServiceTypes = slices.Clone(s.ImportYml.ServiceTypes)
		iy.Modes = slices.Clone(s.ImportYml.Modes)
		iy.CorePackages = slices.Clone(s.ImportYml.CorePackages)
		iy.Locations = slices.Clone(s.ImportYml.Locations)
		iy.StoragePolicies = slices.Clone(s.ImportYml.StoragePolicies)
		out.ImportYml = &iy
	}
	return out
}
