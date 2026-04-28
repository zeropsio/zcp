package recipe

import "strings"

// loadPhaseEntry returns the embedded phase-entry atom for the given
// phase. Phase-entry atoms teach the agent what the phase requires
// (classification rule in research, provision steps, scaffold dispatch
// pattern, feature kinds, finalize stitch flow). Delivered by handlers
// at action=start, enter-phase, complete-phase (next phase's atom), and
// status — so the guidance is always reachable without freeform
// knowledge searches.
func loadPhaseEntry(p Phase) string {
	name := string(p)
	body, err := readAtom("phase_entry/" + name + ".md")
	if err != nil {
		return ""
	}
	return body
}

// gatesForPhase picks the gate set that runs at complete-phase. Every
// phase gets the default gates plus phase-specific checks.
//
// Run-12 §G: scaffold + feature run codebase-scoped surface validators
// at complete-phase so the right author (sub-agent in-session) sees
// violations on content they can fix via record-fragment mode=replace.
// Finalize re-runs codebase gates (catches feature appends) plus env
// gates (root + env surfaces are finalize-authored).
func gatesForPhase(p Phase) []Gate {
	base := DefaultGates()
	switch p {
	case PhaseResearch:
		return append(base, researchGates()...)
	case PhaseScaffold, PhaseFeature:
		// Run-16 §6.1 — scaffold/feature stop authoring fragments. The
		// codebase-validators set still runs so source / zerops.yaml
		// invariants stay enforced; surface validators (IG/KB/CLAUDE)
		// run at the per-codebase content phase below.
		return append(base, CodebaseGates()...)
	case PhaseCodebaseContent:
		// Surface validators run after the content sub-agents author
		// IG/KB/CLAUDE fragments. Codebase-scoped — env surfaces are
		// still ahead.
		return append(base, CodebaseGates()...)
	case PhaseEnvContent:
		// Env-content phase authors root + per-tier surfaces.
		return append(base, EnvGates()...)
	case PhaseFinalize:
		// Finalize re-runs the full set as a backstop; stitch + validate
		// only at this phase post-run-16.
		base = append(base, CodebaseGates()...)
		return append(base, EnvGates()...)
	case PhaseProvision:
		return base
	}
	return base
}

// researchGates enforces classification + shape + service-set
// consistency before the research phase can complete. Without these,
// the v1 dogfood drift pattern (Handlebars UI, Mailpit, single-
// codebase NestJS showcase) ships silently.
func researchGates() []Gate {
	return []Gate{
		{Name: "plan-framework-set", Run: gatePlanFrameworkSet},
		{Name: "plan-codebase-shape-set", Run: gatePlanShapeSet},
		{Name: "plan-codebase-count-matches-shape", Run: gateShapeMatchesCodebases},
		{Name: "plan-tier-service-set", Run: gateTierServiceSet},
		{Name: "plan-shape-classification-consistency", Run: gateShapeClassificationConsistency},
	}
}

func gatePlanFrameworkSet(ctx GateContext) []Violation {
	if ctx.Plan == nil || ctx.Plan.Framework == "" {
		return []Violation{{
			Code:    "plan-framework-missing",
			Message: "Plan.Framework is empty — call action=update-plan with framework set (e.g. \"nestjs\").",
		}}
	}
	return nil
}

func gatePlanShapeSet(ctx GateContext) []Violation {
	if ctx.Plan == nil {
		return nil
	}
	shape := ctx.Plan.Research.CodebaseShape
	if shape != "1" && shape != "2" && shape != "3" {
		return []Violation{{
			Code:    "plan-codebase-shape-invalid",
			Message: "Plan.Research.CodebaseShape must be \"1\" (monolith), \"2\" (api+frontend, worker shares api), or \"3\" (api+frontend+worker-separate). See content/phase_entry/research.md.",
		}}
	}
	return nil
}

// gateShapeMatchesCodebases verifies the declared codebase count lines
// up with the declared shape. Catches the v1 dogfood pattern: agent
// says shape=1 for NestJS but drops in a frontend+worker anyway, or
// declares shape=3 with only 1 codebase.
func gateShapeMatchesCodebases(ctx GateContext) []Violation {
	if ctx.Plan == nil {
		return nil
	}
	shape := ctx.Plan.Research.CodebaseShape
	n := len(ctx.Plan.Codebases)
	switch shape {
	case "1":
		if n != 1 {
			return []Violation{{
				Code:    "shape-codebase-mismatch",
				Message: shapeMismatchMessage("1 (monolith)", 1, n),
			}}
		}
	case "2":
		if n != 2 && n != 3 { // 2-codebase with optional shared worker is also 2
			return []Violation{{
				Code:    "shape-codebase-mismatch",
				Message: shapeMismatchMessage("2 (api+frontend, worker shares api)", 2, n),
			}}
		}
	case "3":
		if n != 3 {
			return []Violation{{
				Code:    "shape-codebase-mismatch",
				Message: shapeMismatchMessage("3 (api+frontend+worker-separate)", 3, n),
			}}
		}
	}
	return nil
}

func shapeMismatchMessage(shape string, expected, got int) string {
	return "CodebaseShape=" + shape +
		" expects " + fmtInt(expected) + " codebases; plan declares " + fmtInt(got) + "."
}

func fmtInt(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	if i < 10 {
		return string(digits[i])
	}
	// Two-digit fallback — codebase counts never exceed single digits in
	// this codebase, but keep the helper total by not pulling in strconv.
	return string(digits[i/10]) + string(digits[i%10])
}

// gateTierServiceSet enforces the minimum managed-service set per tier.
// hello-world: 0 managed. minimal: 1+. showcase: 5 canonical services.
func gateTierServiceSet(ctx GateContext) []Violation {
	if ctx.Plan == nil {
		return nil
	}
	managed := 0
	for _, s := range ctx.Plan.Services {
		if s.Kind == ServiceKindManaged {
			managed++
		}
	}
	switch ctx.Plan.Tier {
	case tierHelloWorld:
		if managed > 0 {
			return []Violation{{
				Code:    "hello-world-no-managed-services",
				Message: "hello-world tier must declare zero managed services — remove them or switch tier to \"minimal\".",
			}}
		}
	case tierMinimal:
		if managed < 1 {
			return []Violation{{
				Code:    "minimal-needs-one-managed-service",
				Message: "minimal tier must declare at least one managed service (typically a database).",
			}}
		}
	case tierShowcase:
		want := map[string]bool{"db": false, "cache": false, "broker": false, "storage": false, "search": false}
		for _, s := range ctx.Plan.Services {
			if _, ok := want[s.Hostname]; ok {
				want[s.Hostname] = true
			}
		}
		var missing []string
		for k, seen := range want {
			if !seen {
				missing = append(missing, k)
			}
		}
		if len(missing) > 0 {
			return []Violation{{
				Code:    "showcase-service-set-incomplete",
				Message: "showcase tier requires canonical services db/cache/broker/storage/search; missing: " + joinSorted(missing),
			}}
		}
	}
	return nil
}

// gateShapeClassificationConsistency checks that the declared role
// assignments match the declared shape. Shape 1 → exactly one
// role=monolith codebase. Shape 2/3 → exactly one role=api + at least
// one role=frontend. Shape 3 → at least one role=worker with
// sharesCodebaseWith="".
func gateShapeClassificationConsistency(ctx GateContext) []Violation {
	if ctx.Plan == nil {
		return nil
	}
	shape := ctx.Plan.Research.CodebaseShape
	roles := map[Role]int{}
	var separateWorkerCount int
	for _, cb := range ctx.Plan.Codebases {
		roles[cb.Role]++
		if cb.IsWorker && cb.SharesCodebaseWith == "" {
			separateWorkerCount++
		}
	}
	var violations []Violation
	switch shape {
	case "1":
		if roles[RoleMonolith] != 1 {
			violations = append(violations, Violation{
				Code:    "shape1-needs-monolith",
				Message: "Shape 1 requires exactly one codebase with role=monolith.",
			})
		}
	case "2", "3":
		if roles[RoleAPI] != 1 {
			violations = append(violations, Violation{
				Code:    "api-first-needs-one-api",
				Message: "Shape 2/3 requires exactly one codebase with role=api.",
			})
		}
		if roles[RoleFrontend] < 1 {
			violations = append(violations, Violation{
				Code:    "api-first-needs-frontend",
				Message: "Shape 2/3 requires at least one codebase with role=frontend.",
			})
		}
		if shape == "3" && separateWorkerCount != 1 {
			violations = append(violations, Violation{
				Code:    "shape3-needs-separate-worker",
				Message: "Shape 3 requires exactly one codebase with role=worker and empty sharesCodebaseWith.",
			})
		}
	}
	return violations
}

// joinSorted concatenates a slice of strings with ", " after sorting,
// for stable error messages.
func joinSorted(items []string) string {
	// Inline insertion sort to avoid pulling in sort for a short list.
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j-1] > items[j]; j-- {
			items[j-1], items[j] = items[j], items[j-1]
		}
	}
	var b strings.Builder
	for i, s := range items {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(s)
	}
	return b.String()
}
