package skillpacks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// removalAction is the read-only preflight's verdict for one (skill,
// target) copy.
type removalAction int

const (
	actionDelete   removalAction = iota // clean: exact marker + exact digest — safe to remove entirely
	actionDetach                        // owned marker, but content has drifted — preserve content, remove only the marker
	actionPreserve                      // missing/foreign/malformed/symlinked marker — leave the whole directory untouched
	actionMissing                       // the copy is already absent — nothing to do, just a warning
)

type removalStep struct {
	target Target
	name   string
	relDir string
	action removalAction
	note   string
}

// Remove uninstalls id: a user-preserving, read-before-write operation.
// Every copy is inspected before anything is deleted; a clean copy is
// removed, a modified copy is preserved and only detached (its ZCP marker
// deleted), and anything else (missing, foreign, or unusable marker) is
// left completely untouched. The manifest is removed last, after every
// copy has been resolved.
//
// Unlike Add, Remove does not require id to be in the active catalog — a
// retired pack (manifest still present, id no longer offered) must remain
// removable — and it still proceeds against an incomplete pack (that is
// exactly the recovery path Add's own "incomplete" refusal points to).
func Remove(ctx context.Context, cwd, id string) (Result, error) {
	res := Result{Operation: "remove", PackID: id}
	if err := ctx.Err(); err != nil {
		return finishResult(res, wrapCoded(CodeInternal, err, "context canceled before starting"))
	}

	root, err := openWorkspaceRoot(cwd)
	if err != nil {
		return finishResult(res, err)
	}
	defer func() { _ = root.Close() }()

	lock, err := acquirePackLock(cwd, lockAcquireTimeout)
	if err != nil {
		return finishResult(res, err)
	}
	defer func() { _ = lock.release() }()

	m, mstate, err := loadManifest(root, id)
	if err != nil {
		return finishResult(res, err)
	}

	switch mstate {
	case manifestAbsent:
		res.State = StateAbsent
		res.Changed = false
		return res, nil
	case manifestLegacy:
		res.State = StateBroken
		return finishResult(res, codedErrorf(CodeLegacyState,
			"the manifest for %q is a legacy (pre-v2) format; manual cleanup is required — remove %s and the corresponding directories under .agents/skills/ and .claude/skills/ by hand",
			id, manifestRelPath(id)))
	case manifestCorrupt:
		res.State = StateBroken
		return finishResult(res, codedErrorf(CodeCorruptState,
			"the manifest for %q is corrupt; manual cleanup is required — inspect and remove %s and the corresponding directories under .agents/skills/ and .claude/skills/ by hand",
			id, manifestRelPath(id)))
	case manifestValid:
		// proceed below
	}

	res.Commit = m.Source.Commit
	res.SkillCount = len(m.Skills)

	steps, err := planRemoval(root, m)
	if err != nil {
		return finishResult(res, err)
	}

	removed, preserved, missing, warnings, err := executeRemoval(root, steps)
	res.Warnings = warnings
	if err != nil {
		return finishResult(res, err)
	}

	if err := removeManifestFile(root, id); err != nil {
		return finishResult(res, err)
	}

	res.State = StateAbsent
	res.Changed = true
	res.Message = summarizeRemoval(removed, missing, preserved)
	return res, nil
}

// planRemoval is Remove's read-only preflight: it decides delete/detach/
// preserve/missing for every (skill, target) copy and returns the full plan
// only if every copy could be inspected cleanly. A hard read error on ANY
// copy — as opposed to a clean "missing" or "marker mismatch" — aborts
// immediately with no partial plan, so the caller performs zero deletions.
func planRemoval(root *os.Root, m *Manifest) ([]removalStep, error) {
	steps := make([]removalStep, 0, len(m.Skills)*len(targets))
	for _, sk := range m.Skills {
		for _, t := range targets {
			relDir := targetSkillDest(t, sk.Name)
			exists, err := rootExists(root, relDir)
			if err != nil {
				return nil, err
			}
			if !exists {
				steps = append(steps, removalStep{
					target: t, name: sk.Name, relDir: relDir, action: actionMissing,
					note: fmt.Sprintf("%s copy of %q is already missing", t, sk.Name),
				})
				continue
			}

			want := markerIdentity{packID: m.ID, generation: m.Generation, target: string(t), skillName: sk.Name}
			outcome, marker, err := readCopyMarker(root, relDir, want)
			if err != nil {
				return nil, err
			}
			switch outcome {
			case markerMissing, markerUnusable, markerForeign:
				steps = append(steps, removalStep{
					target: t, name: sk.Name, relDir: relDir, action: actionPreserve,
					note: fmt.Sprintf("%s copy of %q was left untouched (no matching ownership marker)", t, sk.Name),
				})
			case markerOwned:
				digest, err := treeDigest(root, relDir)
				if err != nil {
					return nil, err
				}
				markerMatchesContent := digest == marker.Digest
				manifestMatchesContent := digest == sk.Digest
				if markerMatchesContent && manifestMatchesContent {
					steps = append(steps, removalStep{target: t, name: sk.Name, relDir: relDir, action: actionDelete})
				} else {
					steps = append(steps, removalStep{
						target: t, name: sk.Name, relDir: relDir, action: actionDetach,
						note: fmt.Sprintf("%s copy of %q was preserved (locally modified) and detached", t, sk.Name),
					})
				}
			}
		}
	}
	return steps, nil
}

// executeRemoval is Remove's phase 2: it performs exactly the decisions
// planRemoval made. A failure partway through is returned immediately —
// whatever was already deleted/detached stays that way (each step is
// independent and idempotent to re-run), but the manifest is only removed
// by the caller after this returns cleanly.
func executeRemoval(root *os.Root, steps []removalStep) (removed, preserved, missing int, warnings []string, err error) {
	for _, s := range steps {
		switch s.action {
		case actionDelete:
			if rmErr := root.RemoveAll(s.relDir); rmErr != nil {
				return removed, preserved, missing, warnings, wrapCoded(CodeFilesystem, rmErr, "remove %s", s.relDir)
			}
			removed++
		case actionDetach:
			markerRel := filepath.Join(s.relDir, markerFileName)
			if rmErr := root.Remove(markerRel); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
				return removed, preserved, missing, warnings, wrapCoded(CodeFilesystem, rmErr, "detach marker at %s", s.relDir)
			}
			preserved++
			warnings = append(warnings, s.note)
		case actionPreserve:
			preserved++
			warnings = append(warnings, s.note)
		case actionMissing:
			missing++
			warnings = append(warnings, s.note)
		}
	}
	return removed, preserved, missing, warnings, nil
}

func summarizeRemoval(removed, missing, preserved int) string {
	msg := "Removed pack management"
	if removed > 0 {
		msg = fmt.Sprintf("Removed pack management (%d skill cop%s)", removed, pluralIES(removed))
	}
	var extra []string
	if preserved > 0 {
		extra = append(extra, fmt.Sprintf("preserved %d locally modified skill cop%s", preserved, pluralIES(preserved)))
	}
	if missing > 0 {
		extra = append(extra, fmt.Sprintf("%d cop%s already missing", missing, pluralIES(missing)))
	}
	if len(extra) > 0 {
		msg += "; " + strings.Join(extra, "; ")
	}
	return msg + "."
}

func pluralIES(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
