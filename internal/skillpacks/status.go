package skillpacks

import (
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
	m, mstate, err := loadManifest(root, id)
	if err != nil {
		return PackStatus{}, err
	}

	switch mstate {
	case manifestAbsent:
		return PackStatus{ID: id, State: StateAbsent}, nil
	case manifestLegacy:
		return PackStatus{
			ID: id, State: StateBroken, Managed: true, Retired: !inCatalog,
			Warnings: []string{fmt.Sprintf("legacy (pre-v2) manifest; run `zcp skills pack-remove %s` for manual-cleanup instructions", id)},
		}, nil
	case manifestCorrupt:
		return PackStatus{
			ID: id, State: StateBroken, Managed: true, Retired: !inCatalog,
			Warnings: []string{fmt.Sprintf("corrupt manifest; run `zcp skills pack-remove %s` for manual-cleanup instructions", id)},
		}, nil
	case manifestValid:
		// proceed below
	}

	overall, warnings, err := auditManifest(root, m)
	if err != nil {
		return PackStatus{}, err
	}
	return PackStatus{
		ID: id, State: overall, Managed: true, Retired: !inCatalog,
		Commit: m.Source.Commit, SkillCount: len(m.Skills), Warnings: warnings,
	}, nil
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
