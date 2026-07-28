package skillpacks

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
)

// State is a pack's overall lifecycle state, as reported by pack-status and
// used internally to decide what Add/Remove may do.
type State string

const (
	StateAbsent     State = "absent"
	StateInstalled  State = "installed"
	StateIncomplete State = "incomplete"
	StateModified   State = "modified"
	StateBroken     State = "broken"
)

// PackStatus is one pack's reported status.
type PackStatus struct {
	ID         string
	State      State
	Managed    bool // a manifest file exists for this id, regardless of its validity
	Retired    bool // a manifest exists but id is no longer in the active catalog
	Commit     string
	SkillCount int
	Warnings   []string
	// Revision is pack-set's opaque selection revision (spec-skill-packs.md
	// §3.1) — empty only for a manifestLegacy/manifestCorrupt pack, where no
	// selection state can be trusted. See computeRevision.
	Revision string
	// Selected is the exact installed skill-name set, sorted; nil for an
	// absent, legacy, or corrupt pack.
	Selected []string
	// Catalog is the reviewed skill metadata a picker needs to render a
	// selection UI — populated only for a ReviewSkillLevel catalog pack,
	// regardless of its current install state.
	Catalog []CatalogSkill
}

// Status reports the current status of one pack id — installed, retired,
// or never seen. It performs no locking (read-only) and no network access.
func Status(cwd, id string) (PackStatus, error) {
	root, err := openWorkspaceRoot(cwd)
	if err != nil {
		return PackStatus{}, err
	}
	defer func() { _ = root.Close() }()
	return statusFor(root, id)
}

// StatusAll reports the status of every pack id worth showing: every
// catalog pack (even if never installed) plus every manifest-only id found
// on disk that has since been retired from the catalog.
func StatusAll(cwd string) ([]PackStatus, error) {
	root, err := openWorkspaceRoot(cwd)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()

	idSet := make(map[string]bool, len(catalog))
	for _, p := range catalog {
		idSet[p.ID] = true
	}
	manifestIDs, err := listManifestIDs(root)
	if err != nil {
		return nil, err
	}
	for _, id := range manifestIDs {
		idSet[id] = true
	}

	ids := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	statuses := make([]PackStatus, 0, len(ids))
	for _, id := range ids {
		st, err := statusFor(root, id)
		if err != nil {
			return nil, err
		}
		statuses = append(statuses, st)
	}
	return statuses, nil
}

func statusFor(root *os.Root, id string) (PackStatus, error) {
	_, inCatalog := Lookup(id)
	catalogSkills := catalogSkillsFor(id)
	m, mstate, err := loadManifest(root, id)
	if err != nil {
		return PackStatus{}, err
	}

	switch mstate {
	case manifestAbsent:
		return PackStatus{
			ID: id, State: StateAbsent, Revision: computeRevision(id, "", nil), Catalog: catalogSkills,
		}, nil
	case manifestLegacy:
		return PackStatus{
			ID: id, State: StateBroken, Managed: true, Retired: !inCatalog, Catalog: catalogSkills,
			Warnings: []string{fmt.Sprintf("legacy (pre-v2) manifest; run `zcp skills pack-remove %s` for manual-cleanup instructions", id)},
		}, nil
	case manifestCorrupt:
		return PackStatus{
			ID: id, State: StateBroken, Managed: true, Retired: !inCatalog, Catalog: catalogSkills,
			Warnings: []string{fmt.Sprintf("corrupt manifest; run `zcp skills pack-remove %s` for manual-cleanup instructions", id)},
		}, nil
	case manifestValid:
		// proceed below
	}

	overall, warnings, err := auditManifest(root, m)
	if err != nil {
		return PackStatus{}, err
	}
	selected := selectedSkillNames(m)
	return PackStatus{
		ID: id, State: overall, Managed: true, Retired: !inCatalog,
		Commit: m.Source.Commit, SkillCount: len(m.Skills), Warnings: warnings,
		Revision: computeRevision(id, m.Generation, selected), Selected: selected, Catalog: catalogSkills,
	}, nil
}

// selectedSkillNames returns m's exact installed skill-name set, sorted —
// the "current selection" both Status and PackSet's revision gate read.
func selectedSkillNames(m *Manifest) []string {
	names := make([]string, len(m.Skills))
	for i, sk := range m.Skills {
		names[i] = sk.Name
	}
	sort.Strings(names) // already sorted by validateManifest, but this must hold regardless of that invariant
	return names
}

// catalogSkillsFor returns id's reviewed skill metadata for a ReviewSkillLevel
// catalog pack — the picker metadata spec-skill-packs.md §3.1 says a read
// must carry so the caller never needs a second source of truth. nil for a
// repository-level or unknown-id pack (spec-skill-packs.md §1: only a
// skill-level pack ever offers a subset).
func catalogSkillsFor(id string) []CatalogSkill {
	p, ok := Lookup(id)
	if !ok || p.Review != ReviewSkillLevel {
		return nil
	}
	return p.Skills
}

// computeRevision derives pack-set's opaque selection revision
// (spec-skill-packs.md §3.1): a pure function of the pack's own persisted
// identity (id, manifest generation) and its exact installed skill-name set.
// generation is "" for a never-installed (absent) pack. This is deliberately
// NOT the raw marker generation alone — that value records installation
// ownership, not selection history (§3.1) — so it is combined with the
// sorted name set: identical inputs always yield an identical revision, and
// any change to the installed selection (a name added or removed) yields a
// different one, even when the generation itself is unchanged.
func computeRevision(id, generation string, skillNames []string) string {
	sorted := append([]string(nil), skillNames...)
	sort.Strings(sorted)
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s\x00%s\x00", id, generation)
	for _, n := range sorted {
		_, _ = fmt.Fprintf(h, "%s\x00", n)
	}
	return "rev:" + hex.EncodeToString(h.Sum(nil))
}

// auditManifest re-scans every (skill, target) copy a valid manifest
// records and derives the pack's overall State: modified beats incomplete
// (a present-but-drifted copy is a more urgent signal than a missing one),
// which beats installed.
func auditManifest(root *os.Root, m *Manifest) (State, []string, error) {
	var warnings []string
	var anyMissing, anyDrifted bool

	for _, sk := range m.Skills {
		for _, t := range targets {
			relDir := targetSkillDest(t, sk.Name)
			exists, err := rootExists(root, relDir)
			if err != nil {
				return "", nil, err
			}
			if !exists {
				anyMissing = true
				warnings = append(warnings, fmt.Sprintf("%s copy of %q is missing", t, sk.Name))
				continue
			}

			want := markerIdentity{packID: m.ID, generation: m.Generation, target: string(t), skillName: sk.Name}
			outcome, marker, err := readCopyMarker(root, relDir, want)
			if err != nil {
				return "", nil, err
			}
			switch outcome {
			case markerMissing:
				anyDrifted = true
				warnings = append(warnings, fmt.Sprintf("%s copy of %q has no ownership marker", t, sk.Name))
			case markerUnusable:
				anyDrifted = true
				warnings = append(warnings, fmt.Sprintf("%s copy of %q has an unusable ownership marker", t, sk.Name))
			case markerForeign:
				anyDrifted = true
				warnings = append(warnings, fmt.Sprintf("%s copy of %q is owned by a different pack or generation", t, sk.Name))
			case markerOwned:
				digest, err := treeDigest(root, relDir)
				if err != nil {
					return "", nil, err
				}
				if digest != sk.Digest || marker.Digest != sk.Digest {
					anyDrifted = true
					warnings = append(warnings, fmt.Sprintf("%s copy of %q has local modifications", t, sk.Name))
				}
			}
		}
	}

	switch {
	case anyDrifted:
		return StateModified, warnings, nil
	case anyMissing:
		return StateIncomplete, warnings, nil
	default:
		return StateInstalled, warnings, nil
	}
}
