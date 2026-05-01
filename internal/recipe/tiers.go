// Package recipe is zcprecipator3's engine. It orchestrates recipe runs
// against a typed 5-phase state machine, a typed surface registry, and
// a yaml emitter derived from Zerops platform schemas. See
// docs/zcprecipator3/plan.md for the architectural stays-list and
// boundary rules this package enforces.
package recipe

import (
	"fmt"
	"strconv"
)

// Tier is a Zerops environment tier — one of six product-defined scales
// from AI-agent dev to HA production. Fields encode platform decisions
// (mode, cpu mode, scale defaults). An agent authoring env-README prose
// reads Diff() output, not these fields directly.
type Tier struct {
	Index                int
	Folder               string
	Label                string
	Suffix               string
	RunsDevContainer     bool
	ServiceMode          string
	RuntimeMinContainers int
	CPUMode              string
	CorePackage          string
	MinFreeRAMGB         float64
	RuntimeMinRAM        float64
	ManagedMinRAM        float64
}

var tiers = [6]Tier{
	{
		// Run-23 fix-9 — audience-first label. Folder name (the
		// spec-fixed "0 — AI Agent" published path) keeps its canonical
		// shape; the audience Label that lands in tier README headings,
		// brief intros, and engine fact strings names WHO the tier is
		// for (porter brings coding agents into the dev loop), not
		// WHAT the tier is.
		Index: 0, Folder: "0 — AI Agent", Label: "Include Coding Agents", Suffix: "agent",
		RunsDevContainer: true, ServiceMode: "NON_HA",
		RuntimeMinContainers: 1,
		RuntimeMinRAM:        0.5, ManagedMinRAM: 0.25,
	},
	{
		// Run-23 fix-9 — audience-first label. Folder stays canonical;
		// audience Label names the developer affordance (porter brings
		// a Cloud IDE into the loop, e.g. VS Code Remote / Cursor over
		// SSH) rather than the protocol acronym ("Remote (CDE)").
		Index: 1, Folder: "1 — Remote (CDE)", Label: "Include Cloud IDE", Suffix: "remote",
		RunsDevContainer: true, ServiceMode: "NON_HA",
		RuntimeMinContainers: 1,
		RuntimeMinRAM:        0.5, ManagedMinRAM: 0.25,
	},
	{
		Index: 2, Folder: "2 — Local", Label: "Local", Suffix: "local",
		ServiceMode:          "NON_HA",
		RuntimeMinContainers: 1,
		RuntimeMinRAM:        0.5, ManagedMinRAM: 0.25,
	},
	{
		Index: 3, Folder: "3 — Stage", Label: "Stage", Suffix: "stage",
		ServiceMode:          "NON_HA",
		RuntimeMinContainers: 1,
		MinFreeRAMGB:         0.25,
		RuntimeMinRAM:        0.5, ManagedMinRAM: 0.25,
	},
	{
		Index: 4, Folder: "4 — Small Production", Label: "Small Production", Suffix: "small-prod",
		ServiceMode:          "NON_HA",
		RuntimeMinContainers: 2,
		MinFreeRAMGB:         0.25,
		RuntimeMinRAM:        0.5, ManagedMinRAM: 0.25,
	},
	{
		Index: 5, Folder: "5 — Highly-available Production", Label: "Highly-available Production", Suffix: "ha-prod",
		ServiceMode:          "HA",
		RuntimeMinContainers: 2,
		CPUMode:              "DEDICATED",
		CorePackage:          "SERIOUS",
		MinFreeRAMGB:         0.5,
		RuntimeMinRAM:        0.5, ManagedMinRAM: 1,
	},
}

// Tiers returns the six tiers in order.
func Tiers() []Tier {
	out := make([]Tier, len(tiers))
	copy(out, tiers[:])
	return out
}

// TierAt returns the tier at the given index, or false if out of range.
func TierAt(index int) (Tier, bool) {
	if index < 0 || index >= len(tiers) {
		return Tier{}, false
	}
	return tiers[index], true
}

// FieldChange is one behavior delta between two tiers. Kind indicates
// which service class the delta applies to: "runtime" (runtime services),
// "managed" (managed services with mode field), "project" (project-scoped),
// or "all" (across service kinds).
type FieldChange struct {
	Field string
	From  string
	To    string
	Kind  string
}

// TierDiff is the behavior delta from tier FromIndex to ToIndex.
// Metadata fields (Index/Folder/Label/Suffix) are excluded — those are
// addressing, not behavior.
type TierDiff struct {
	FromIndex int
	ToIndex   int
	Changes   []FieldChange
}

// Diff returns the behavior delta from a to b. Emits changes in
// struct-field order; identical fields are omitted.
func Diff(a, b Tier) TierDiff {
	d := TierDiff{FromIndex: a.Index, ToIndex: b.Index}
	add := func(field, from, to, kind string) {
		if from != to {
			d.Changes = append(d.Changes, FieldChange{Field: field, From: from, To: to, Kind: kind})
		}
	}
	add("RunsDevContainer", strconv.FormatBool(a.RunsDevContainer), strconv.FormatBool(b.RunsDevContainer), "runtime")
	add("ServiceMode", a.ServiceMode, b.ServiceMode, "managed")
	add("RuntimeMinContainers", strconv.Itoa(a.RuntimeMinContainers), strconv.Itoa(b.RuntimeMinContainers), "runtime")
	add("CPUMode", a.CPUMode, b.CPUMode, "runtime")
	add("CorePackage", a.CorePackage, b.CorePackage, "project")
	add("MinFreeRAMGB", fmtFloat(a.MinFreeRAMGB), fmtFloat(b.MinFreeRAMGB), "all")
	add("RuntimeMinRAM", fmtFloat(a.RuntimeMinRAM), fmtFloat(b.RuntimeMinRAM), "runtime")
	add("ManagedMinRAM", fmtFloat(a.ManagedMinRAM), fmtFloat(b.ManagedMinRAM), "managed")
	return d
}

// fmtFloat formats a tier-float in the short form ("0", "0.25", "1") so
// field-change strings read cleanly.
func fmtFloat(v float64) string {
	return fmt.Sprintf("%g", v)
}
