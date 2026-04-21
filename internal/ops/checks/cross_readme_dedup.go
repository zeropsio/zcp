package checks

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// CheckCrossReadmeGotchaUniqueness is the cross-codebase duplicate-gotcha
// check. Each per-codebase README gets its own predecessor-floor and
// authenticity scoring in isolation, so the optimal agent strategy to
// hit both floors is to stamp the same 3-4 authentic gotchas into every
// README. v15's nestjs-showcase had the NATS-credential fact appearing
// in api + worker, SSHFS ownership in api + worker, and zsc-execOnce
// burn in api + worker — five total mentions of facts that belong in
// one place.
//
// The rule: each normalized gotcha stem may appear in at most one
// codebase's README. A fact that applies to multiple codebases (NATS
// client credentials, execOnce burn recovery, SSHFS uid fix) lives in
// exactly one README — by convention, the service that owns the
// primary integration — and the others cross-reference it.
//
// Normalization uses the same token-set intersection as the
// predecessor-floor check (workflow.NormalizeStem + workflow.StemsMatch),
// so lightly-reworded clones still collide.
//
// The check is skipped when only one README has any gotchas — with
// fewer than two populated knowledge-base fragments there is nothing
// to deduplicate.
func CheckCrossReadmeGotchaUniqueness(_ context.Context, readmes map[string]string) []workflow.StepCheck {
	type stemLoc struct {
		hostname string
		raw      string
		norm     []string
	}
	// Collect hostnames in a deterministic order so the failure detail
	// is stable across runs.
	hostnames := make([]string, 0, len(readmes))
	for h := range readmes {
		hostnames = append(hostnames, h)
	}
	sort.Strings(hostnames)

	var all []stemLoc
	populatedHosts := 0
	for _, h := range hostnames {
		kb := extractFragmentContent(readmes[h], "knowledge-base")
		if kb == "" {
			continue
		}
		stems := workflow.ExtractGotchaStems(kb)
		if len(stems) == 0 {
			continue
		}
		populatedHosts++
		for _, s := range stems {
			norm := workflow.NormalizeStem(s)
			if len(norm) == 0 {
				continue
			}
			all = append(all, stemLoc{hostname: h, raw: s, norm: norm})
		}
	}
	// No cross-comparison is possible with fewer than two READMEs that
	// actually have gotchas. Pass so the result surface stays consistent
	// regardless of codebase count.
	if populatedHosts < 2 {
		return []workflow.StepCheck{{
			Name:   "cross_readme_gotcha_uniqueness",
			Status: StatusPass,
		}}
	}

	// Pairwise comparison across distinct hostnames. Each duplicate
	// pair is reported once.
	type pair struct{ i, j int }
	reported := map[pair]bool{}
	var dups []string
	for i := 0; i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			if all[i].hostname == all[j].hostname {
				continue
			}
			if !workflow.StemsMatch(all[i].norm, all[j].norm) {
				continue
			}
			if reported[pair{i, j}] {
				continue
			}
			reported[pair{i, j}] = true
			dups = append(dups, fmt.Sprintf(
				"%s %q ≈ %s %q",
				all[i].hostname, all[i].raw,
				all[j].hostname, all[j].raw,
			))
		}
	}

	if len(dups) == 0 {
		return []workflow.StepCheck{{
			Name:   "cross_readme_gotcha_uniqueness",
			Status: StatusPass,
		}}
	}
	return []workflow.StepCheck{{
		Name:   "cross_readme_gotcha_uniqueness",
		Status: StatusFail,
		Detail: fmt.Sprintf(
			"gotcha stems appear in multiple codebase READMEs — each fact must live in exactly ONE README. Pick the codebase most responsible for the fact (server for NATS client config, app for Vite allowedHosts), keep it there, delete it from the others, and replace in the others with a cross-reference: \"See apidev/README.md §Gotchas for the NATS credential format.\" README.md is PUBLISHED content extracted to zerops.io/recipes — readers read the recipe page top-to-bottom, so duplicate facts waste publication surface and train readers to skim. Duplicates: %s",
			strings.Join(dups, "; "),
		),
	}}
}
