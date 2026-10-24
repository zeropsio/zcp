package topology

import (
	"sort"
	"strings"
)

// boxLocations is every project location a Zerops Box can be created in,
// keyed by the location id the project record reports
// (`primaryInstanceLocation.id`) and valued by its region's name. A Box can
// be created only in a project in region prg1 (owner-stated platform fact,
// 2026-10-24); prg1 is the location `eu-central`, which the platform names
// "EU Central (prg1)" (live-verified 2026-10-24 — ny1 reads `us-east-1`).
// When the platform offers Box in another region, that region is one more
// entry here. Spec: spec-workflows.md §8 PS-16.
//
//nolint:gochecknoglobals // value-only table, never mutated.
var boxLocations = map[string]string{
	"eu-central": "prg1",
}

// BoxLocationAllowed reports whether a Zerops Box can be created in a project
// whose record reports locationID. Case and surrounding space are ignored;
// an empty id is no location.
func BoxLocationAllowed(locationID string) bool {
	_, ok := boxLocations[strings.ToLower(strings.TrimSpace(locationID))]
	return ok
}

// BoxRegions returns the names of the regions a Zerops Box can be created
// in, sorted — what a refusal names.
func BoxRegions() []string {
	out := make([]string, 0, len(boxLocations))
	for _, region := range boxLocations {
		out = append(out, region)
	}
	sort.Strings(out)
	return out
}
