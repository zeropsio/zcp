package workflow

import (
	"fmt"
	"sort"
	"strings"
)

// CoupledNames returns, for every failed check in the batch, the names of
// other checks in the same emit batch that declare an identical ReadSurface.
// The match is an exact-string equality on ReadSurface; callers should keep
// ReadSurface values stable (no per-run absolute paths, timestamps, or
// dynamically-generated IDs). Checks that pass or that declare an empty
// ReadSurface are never stamped.
//
// v8.97 Fix 4 — the coupling graph is a function of the checks' own surface
// declarations, not a hand-maintained cluster table. Any two checks that
// read the same surface are coupled because an edit to that surface to
// satisfy one may destabilize the other. Every historical cascade class
// (worker-gotcha triad, env4 comment triad, readme-fragments pair) is
// covered as a consequence of shared surfaces; any future cascade on a
// novel surface is covered the moment the checks declare it.
func CoupledNames(checks []StepCheck) map[string][]string {
	bySurface := map[string][]string{}
	for _, c := range checks {
		if c.ReadSurface == "" {
			continue
		}
		bySurface[c.ReadSurface] = append(bySurface[c.ReadSurface], c.Name)
	}
	out := map[string][]string{}
	for _, c := range checks {
		if c.Status != "fail" || c.ReadSurface == "" {
			continue
		}
		siblings := bySurface[c.ReadSurface]
		for _, sibling := range siblings {
			if sibling != c.Name {
				out[c.Name] = append(out[c.Name], sibling)
			}
		}
	}
	// Deterministic ordering per check for stable test output.
	for name, sibs := range out {
		sort.Strings(sibs)
		out[name] = sibs
	}
	return out
}

// StampCoupling returns a new slice where every failed check with at least
// one coupled sibling carries CoupledWith populated and a standardized
// tail appended to HowToFix. Passing checks and checks with empty
// ReadSurface are returned unchanged. Idempotent: re-calling StampCoupling
// on an already-stamped slice leaves the tail intact (it is only appended
// when absent).
//
// The standardized tail names every coupled sibling by full check name so
// the author reads the names verbatim in the failure payload — paraphrased
// coupling gets ignored.
func StampCoupling(checks []StepCheck) []StepCheck {
	if len(checks) == 0 {
		return checks
	}
	coupled := CoupledNames(checks)
	if len(coupled) == 0 {
		return checks
	}
	out := make([]StepCheck, len(checks))
	copy(out, checks)
	for i := range out {
		sibs, ok := coupled[out[i].Name]
		if !ok || len(sibs) == 0 {
			continue
		}
		// Merge with any pre-populated CoupledWith (e.g. hand-authored
		// couplings on individual checks) without duplicates.
		seen := map[string]struct{}{}
		for _, s := range out[i].CoupledWith {
			seen[s] = struct{}{}
		}
		for _, s := range sibs {
			if _, dup := seen[s]; dup {
				continue
			}
			out[i].CoupledWith = append(out[i].CoupledWith, s)
			seen[s] = struct{}{}
		}
		// Append the standardized tail to HowToFix iff not already
		// present. The tail names the surface and every coupled
		// sibling so the author can read the coupling verbatim.
		tail := buildCouplingTail(out[i].ReadSurface, out[i].CoupledWith)
		if !strings.Contains(out[i].HowToFix, tail) {
			if out[i].HowToFix == "" {
				out[i].HowToFix = tail
			} else {
				out[i].HowToFix = out[i].HowToFix + "\n\n" + tail
			}
		}
	}
	return out
}

// buildCouplingTail returns the standardized HowToFix tail naming the
// coupled siblings. Separated so the test can match against the exact
// emitted text.
func buildCouplingTail(surface string, siblings []string) string {
	// Format sibling list with backticks for readability.
	parts := make([]string, len(siblings))
	for i, s := range siblings {
		parts[i] = "`" + s + "`"
	}
	return fmt.Sprintf(
		"**Coupled checks on same surface (`%s`)**: %s. An edit to this surface that satisfies this check may destabilize the coupled checks. Read their `Required` fields before editing; re-run and verify all pass after your edit.",
		surface, strings.Join(parts, ", "),
	)
}
