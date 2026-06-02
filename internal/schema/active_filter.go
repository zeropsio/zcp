package schema

import (
	"context"
	"slices"
	"strings"

	"github.com/zeropsio/zcp/internal/topology"
)

// ActiveVersionsProvider returns the platform's ACTIVE concrete/rolling service
// type versions. The schema package accepts it as a function to keep platform
// client imports out of this package.
type ActiveVersionsProvider func(context.Context) ([]string, error)

// FilterToActive returns a copy of schemas whose import.yaml service type enum
// contains only schema forms covered by the platform ACTIVE-version set.
//
// Scope note: this filters ImportYml.ServiceTypes only. ZeropsYml BuildBases /
// RunBases carry a related enum surface, but P0a's importability bug is caused
// by services[].type accepting inactive versions.
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
		if active[serviceType] {
			kept = append(kept, serviceType)
		}
	}
	out.ImportYml.ServiceTypes = kept
	out.ImportYml.serviceTypeSet = makeStringSet(kept)
	return out
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
		zy.buildBaseSet = baseNameSet(zy.BuildBases)
		zy.buildBaseVersionSet = makeStringSet(zy.BuildBases)
		zy.runBaseSet = makeStringSet(zy.RunBases)
		out.ZeropsYml = &zy
	}
	if s.ImportYml != nil {
		iy := *s.ImportYml
		iy.ServiceTypes = slices.Clone(s.ImportYml.ServiceTypes)
		iy.Modes = slices.Clone(s.ImportYml.Modes)
		iy.CorePackages = slices.Clone(s.ImportYml.CorePackages)
		iy.StoragePolicies = slices.Clone(s.ImportYml.StoragePolicies)
		iy.serviceTypeSet = makeStringSet(iy.ServiceTypes)
		out.ImportYml = &iy
	}
	return out
}
