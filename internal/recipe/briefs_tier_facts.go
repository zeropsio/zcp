package recipe

import (
	"fmt"
	"sort"
	"strings"
)

// BuildTierFactTable returns the engine-resolved tier capability matrix
// — the literal field values the yaml emitter writes per tier, plus the
// per-managed-service HA downgrade table the tier-5 emit applies.
// Composed from `tiers.go::Tiers()` + `plan.go::managedServiceSupportsHA`
// + `Plan.Services` (so explicit Service.SupportsHA overrides the
// family-table conservative default).
//
// Run-13 §T — closes the run-12 prose-vs-emit divergence at the source.
// The agent's tier-aware prose extrapolated from `tierAudienceLine()`'s
// fuzzy summary ("production replicas") and shipped invented numbers
// (3 replicas, "Meilisearch keeps a backup"); the engine's literal
// emit is 2 replicas + `mode: NON_HA`. Pushing the resolved matrix
// into scaffold (frontend) and finalize briefs lets the agent author
// against truth.
func BuildTierFactTable(plan *Plan) string {
	var b strings.Builder
	b.WriteString("## Tier capability matrix\n\n")
	b.WriteString("The engine emits these field values per tier — your prose MUST match.\n\n")
	b.WriteString("| Tier | RuntimeMinContainers | ServiceMode | CPUMode | CorePackage | RunsDevContainer | MinFreeRAMGB |\n")
	b.WriteString("|------|----------------------|-------------|---------|-------------|------------------|--------------|\n")
	for _, t := range Tiers() {
		fmt.Fprintf(&b, "| %d | %d | %s | %s | %s | %s | %s |\n",
			t.Index,
			t.RuntimeMinContainers,
			fallbackDash(t.ServiceMode),
			fallbackDash(t.CPUMode, "(shared)"),
			fallbackDash(t.CorePackage),
			devContainerCell(t.RunsDevContainer),
			minFreeRAMCell(t.MinFreeRAMGB),
		)
	}
	b.WriteByte('\n')

	b.WriteString("## Per-service capability adjustments\n\n")
	b.WriteString("At tier 5 (`ServiceMode: HA`), the engine downgrades non-HA-capable\n")
	b.WriteString("managed-service families to `NON_HA` at emit time. Your prose MUST\n")
	b.WriteString("reflect the EMITTED mode, not the tier-baseline mode.\n\n")
	b.WriteString("| Family | HA-capable | At tier 5 emits |\n")
	b.WriteString("|--------|------------|-----------------|\n")
	for _, fam := range haCapableFamilies() {
		fmt.Fprintf(&b, "| %s | yes | `mode: HA` |\n", fam)
	}
	for _, fam := range knownNonHAFamilies() {
		fmt.Fprintf(&b, "| %s | NO | `mode: NON_HA` |\n", fam)
	}
	b.WriteString("| (other / unknown) | NO (conservative default) | `mode: NON_HA` |\n")

	// Plan-overridden services — when the agent declares
	// Service.SupportsHA explicitly (force-override), the table reflects
	// the override so prose matches the actual emit instead of the
	// family-table fallback.
	if overrides := planManagedHAOverrides(plan); len(overrides) > 0 {
		b.WriteByte('\n')
		b.WriteString("Plan-overridden services (explicit `Service.SupportsHA`):\n\n")
		for _, o := range overrides {
			fmt.Fprintf(&b, "- `%s` (%s) (plan-overridden) — emits `mode: %s` at tier 5\n",
				o.Hostname, o.Type, o.Mode)
		}
	}
	b.WriteByte('\n')

	b.WriteString("## Storage / quota fields the engine fixes\n\n")
	b.WriteString("Object-storage emits `objectStorageSize: 1` + `objectStoragePolicy:\n")
	b.WriteString("private` UNIFORMLY across all tiers. Do NOT claim larger quotas\n")
	b.WriteString("(\"10 GB\", \"50 GB\") or replication for storage at any tier — those\n")
	b.WriteString("fields don't exist on `ServiceKindStorage` in the current emit.\n\n")

	b.WriteString("## In your prose\n\n")
	b.WriteString("When a tier README, env import-comment, or codebase IG yaml-block-\n")
	b.WriteString("comment claims a number or category for any of these fields, the\n")
	b.WriteString("claim MUST match the table. \"Three replicas\" prose paired with\n")
	b.WriteString("`minContainers: 2` field is a defect — pick \"two replicas\" or\n")
	b.WriteString("restructure prose to omit the number.\n")
	return b.String()
}

// haOverride is one (hostname, type, emit-mode) triple for a Plan
// service whose Service.SupportsHA was set explicitly. Surfaced in the
// tier-fact table so prose authored for that specific service matches
// the emit (not the family-table fallback).
type haOverride struct {
	Hostname string
	Type     string
	Mode     string
}

// planManagedHAOverrides returns the managed services whose explicit
// Service.SupportsHA disagrees with the family-table default. Sorted
// by hostname for deterministic table output.
func planManagedHAOverrides(plan *Plan) []haOverride {
	if plan == nil {
		return nil
	}
	var out []haOverride
	for _, s := range plan.Services {
		if s.Kind != ServiceKindManaged {
			continue
		}
		family := managedServiceSupportsHA(s.Type)
		if s.SupportsHA == family {
			continue
		}
		mode := "NON_HA"
		if s.SupportsHA {
			mode = "HA"
		}
		out = append(out, haOverride{Hostname: s.Hostname, Type: s.Type, Mode: mode})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Hostname < out[j].Hostname })
	return out
}

// haCapableFamilies returns the managed-service families that
// `managedServiceSupportsHA` reports as HA-capable. Sorted for
// deterministic table output.
func haCapableFamilies() []string {
	out := []string{
		"postgresql", "valkey", "redis", "nats", "rabbitmq", "elasticsearch",
	}
	sort.Strings(out)
	return out
}

// knownNonHAFamilies returns the canonical list of managed-service
// families seen in run-12 dogfood that emit NON_HA at tier 5. Sorted
// for deterministic output.
func knownNonHAFamilies() []string {
	out := []string{"meilisearch", "kafka"}
	sort.Strings(out)
	return out
}

// fallbackDash renders empty strings as a literal dash so table cells
// stay visually aligned. Optional second arg overrides the default
// dash with a parenthesised label (e.g. "(shared)" for CPUMode).
func fallbackDash(v string, alt ...string) string {
	if v != "" {
		return v
	}
	if len(alt) > 0 {
		return alt[0]
	}
	return "-"
}

// devContainerCell formats the boolean RunsDevContainer field with the
// tier-shape qualifier the brief readers expect.
func devContainerCell(runs bool) string {
	if runs {
		return "yes (dev-pair)"
	}
	return "no (single-slot)"
}

// minFreeRAMCell renders the float MinFreeRAMGB as a short string,
// trimming trailing zeros so table cells read cleanly.
func minFreeRAMCell(v float64) string {
	if v == 0 {
		return "-"
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", v), "0"), ".")
}
