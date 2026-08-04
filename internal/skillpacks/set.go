package skillpacks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"
)

// PackSet declaratively reconciles id's installed selection to exactly
// desired (spec-skill-packs.md §3.1): the caller states the desired
// installed set and PackSet derives the additions and removals itself. It is
// never additive — a name missing from desired that is currently installed
// is removed (deleted if clean, detached if locally modified).
//
// expectedRevision is mandatory and is compared, under the pack lock,
// against the freshly recomputed current revision; a mismatch returns a
// CodeConflict Result with zero writes. Only a ReviewSkillLevel catalog pack
// supports a selection at all (§1); an atomic pack (§5) accepts only its
// complete supported set or an empty selection.
func PackSet(ctx context.Context, cwd, id string, desired []string, expectedRevision string) (Result, error) {
	res := Result{Operation: "set", PackID: id}

	pack, ok := Lookup(id)
	if !ok {
		// A retired pack (no longer in the catalog) must still be removable
		// via pack-set — spec-welcome-mode.md §7, mirroring Remove's own
		// "no catalog gate" contract (see remove.go). Only the declarative
		// EMPTY selection is meaningful here: there is no catalog left to
		// validate a non-empty --skills list's names against.
		if len(desired) == 0 {
			return packSetRemoveRetired(ctx, cwd, id, expectedRevision)
		}
		return finishResult(res, codedErrorf(CodeUnknownPack, "unknown skill pack %q (valid: %s)", id, strings.Join(ValidIDs(), ", ")))
	}
	if pack.Review != ReviewSkillLevel {
		return finishResult(res, codedErrorf(CodeNotSkillLevel,
			"skill pack %q is reviewed at repository level; it does not support a partial selection", id))
	}

	names, err := normalizeDesiredSkills(pack, desired)
	if err != nil {
		return finishResult(res, err)
	}
	if err := validateSelectionGranularity(pack, names); err != nil {
		return finishResult(res, err)
	}
	if err := validateSelectionClosure(pack, names); err != nil {
		return finishResult(res, err)
	}

	return packSetForPack(ctx, cwd, pack, names, expectedRevision)
}

// packSetRemoveRetired is PackSet's declarative-empty-selection path for a
// pack id no longer in the catalog (spec-welcome-mode.md §7: "a retired
// ReviewSkillLevel pack must still be removable via pack-set"). Unlike
// packSetForPack's legacyExtra handling — which force-detaches skills
// outside a STILL-catalogued pack's current review, because that pack's
// catalog narrowed but the pack itself remains reviewed — a fully retired
// id has no reviewed/legacy distinction left to draw at all: every
// currently installed skill is removed via the exact same
// delete/detach/preserve/missing classification Remove() itself uses
// (planRemoval + executeRemoval), not a forced detach. The one thing PackSet
// adds on top of plain Remove is the revision-conflict gate, so a picker
// that read a retired pack's status before someone else changed it still
// gets a stable, zero-write "conflict" rather than an unconditional removal.
func packSetRemoveRetired(ctx context.Context, cwd, id, expectedRevision string) (Result, error) {
	res := Result{Operation: "set", PackID: id}
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
	case manifestLegacy:
		return finishResult(res, codedErrorf(CodeLegacyState,
			"a legacy (pre-v2) manifest for %q already exists; run `zcp skills pack-remove %s` then reapply your selection to recover", id, id))
	case manifestCorrupt:
		return finishResult(res, codedErrorf(CodeCorruptState,
			"the manifest for %q is corrupt; run `zcp skills pack-remove %s` then reapply your selection to recover", id, id))
	case manifestAbsent:
		currentRevision := computeRevision(id, "", nil)
		if currentRevision != expectedRevision {
			return finishResult(res, codedErrorf(CodeConflict,
				"skill pack %q selection has changed since it was last read; re-read pack-status and try again", id))
		}
		res.State, res.Selected, res.Revision = StateAbsent, []string{}, currentRevision
		return res, nil
	case manifestValid:
		// proceed below
	}

	currentRevision := computeRevision(id, m.Generation, selectedSkillNames(m))
	if currentRevision != expectedRevision {
		return finishResult(res, codedErrorf(CodeConflict,
			"skill pack %q selection has changed since it was last read; re-read pack-status and try again", id))
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
	res.Selected = []string{}
	res.Revision = computeRevision(id, "", nil)
	res.Message = summarizeRemoval(removed, missing, preserved)
	return res, nil
}

// normalizeDesiredSkills validates every name in raw against pack's reviewed
// catalog (spec-skill-packs.md §1: only a catalogued name is ever
// installable) and rejects a duplicate, returning the sorted, deduplicated
// desired set. A nil/empty raw is a valid, normal input — the empty
// selection (spec-skill-packs.md §3: removal of the pack).
func normalizeDesiredSkills(pack Pack, raw []string) ([]string, error) {
	catalogNames := make(map[string]bool, len(pack.Skills))
	for _, sk := range pack.Skills {
		catalogNames[sk.Name] = true
	}

	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	for _, name := range raw {
		if !catalogNames[name] {
			return nil, codedErrorf(CodeUnknownSkill, "skill %q is not in the %q catalog", name, pack.ID)
		}
		if seen[name] {
			return nil, codedErrorf(CodeDuplicateSkill, "skill %q is listed more than once", name)
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

// validateSelectionGranularity enforces spec-skill-packs.md §5: an atomic
// pack (Superpowers) accepts only its complete supported set or an empty
// selection — never a partial one. names is already validated (by
// normalizeDesiredSkills) to be a duplicate-free subset of pack.Skills'
// names, so a length match against the full catalog necessarily means an
// exact match.
func validateSelectionGranularity(pack Pack, names []string) error {
	if !pack.IsAtomic() {
		return nil
	}
	if len(names) == 0 || len(names) == len(pack.Skills) {
		return nil
	}
	return codedErrorf(CodeAtomicPartial,
		"skill pack %q must be selected in full or not at all (got %d of %d skills)", pack.ID, len(names), len(pack.Skills))
}

// validateSelectionClosure enforces spec-skill-packs.md §3.1: the
// caller-stated set must be dependency-closed over pack's declared Requires
// edges (§4.2). It is pure input validation over pack and names (already
// normalized by normalizeDesiredSkills) only — no manifest, lock, or
// revision is consulted — so it runs before both the lock acquisition and
// the revision compare, and a stale revision combined with a non-closed set
// returns CodeUnclosedSelection, never CodeConflict (§7 proof 14). The
// implementation never expands names itself; a caller that wants the
// closure applied re-issues --skills with the reported names included
// (§3.1's "the implementation never expands the caller's set").
func validateSelectionClosure(pack Pack, names []string) error {
	violations := transitiveViolations(pack, names)
	if len(violations) == 0 {
		return nil
	}
	return &CodedError{Code: CodeUnclosedSelection, Message: formatViolations(violations)}
}

// packSetForPack is PackSet's implementation, taking an already-validated
// Pack and an already-normalized (sorted, deduplicated, catalog-checked)
// desired set — tests exercise it directly with a fixture-backed Pack the
// same way addPackForTest bypasses the catalog Lookup for Add.
//
// The full pipeline, in order: acquire the cross-process lock; load and
// classify the manifest; compute the current revision and compare it to
// expectedRevision (CodeConflict, zero writes, on mismatch); split the
// current install into "reviewed" (still catalogued) and "legacy extra"
// (predates skill-level review — always migrated via forced detach, never
// silently dropped or kept selected, spec-skill-packs.md §3.1); derive
// additions/removals from desired vs. reviewed; preflight the WHOLE
// reconciliation (every addition's content + collisions, every removal's
// delete/detach/preserve/missing classification) with zero writes; only then
// execute, additions first, then removals via a quarantine-rename that keeps
// every removal step reversible until the new manifest itself is
// successfully written.
func packSetForPack(ctx context.Context, cwd string, pack Pack, desired []string, expectedRevision string) (Result, error) {
	res := Result{Operation: "set", PackID: pack.ID}
	if err := ctx.Err(); err != nil {
		return finishResult(res, wrapCoded(CodeInternal, err, "context canceled before starting"))
	}
	desired = append([]string(nil), desired...)
	sort.Strings(desired)

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

	m, mstate, err := loadManifest(root, pack.ID)
	if err != nil {
		return finishResult(res, err)
	}
	switch mstate {
	case manifestLegacy:
		return finishResult(res, codedErrorf(CodeLegacyState,
			"a legacy (pre-v2) manifest for %q already exists; run `zcp skills pack-remove %s` then reapply your selection to recover", pack.ID, pack.ID))
	case manifestCorrupt:
		return finishResult(res, codedErrorf(CodeCorruptState,
			"the manifest for %q is corrupt; run `zcp skills pack-remove %s` then reapply your selection to recover", pack.ID, pack.ID))
	case manifestAbsent, manifestValid:
		// proceed below
	}

	currentGeneration := ""
	var currentNames []string
	originalCommit := ""
	if mstate == manifestValid {
		currentGeneration = m.Generation
		currentNames = selectedSkillNames(m)
		originalCommit = m.Source.Commit
	}
	currentRevision := computeRevision(pack.ID, currentGeneration, currentNames)
	if currentRevision != expectedRevision {
		return finishResult(res, codedErrorf(CodeConflict,
			"skill pack %q selection has changed since it was last read; re-read pack-status and try again", pack.ID))
	}

	reviewed, legacyExtra := splitReviewedAndLegacyExtra(pack, m, mstate)
	additionNames, toRemove, desiredNames := diffSelection(reviewed, desired)

	if len(additionNames) == 0 && len(toRemove) == 0 && len(legacyExtra) == 0 {
		return noopSetResult(res, desired, reviewed, originalCommit, currentRevision), nil
	}

	// ---- preflight: read-only workspace checks + network fetch, zero writes ----
	removalSteps, err := planRemoval(root, &Manifest{ID: pack.ID, Generation: currentGeneration, Skills: toRemove})
	if err != nil {
		return finishResult(res, err)
	}
	legacyExtraSteps, err := planRemoval(root, &Manifest{ID: pack.ID, Generation: currentGeneration, Skills: legacyExtra})
	if err != nil {
		return finishResult(res, err)
	}
	legacyExtraSteps = forceDetachSteps(legacyExtraSteps, pack.ID)

	plan, err := preflightAdditions(ctx, pack, mstate, originalCommit, additionNames)
	if plan.tmpDir != "" {
		// plan.candidates' SourceDir points inside tmpDir, and publishPack
		// (execute, below) still needs to read from it — cleanup must wait
		// until THIS function returns, not until preflightAdditions does.
		defer func() { _ = os.RemoveAll(plan.tmpDir) }()
	}
	if err != nil {
		return finishResult(res, err)
	}
	if len(plan.candidates) > 0 {
		if err := preflightCollisions(root, plan.candidates); err != nil {
			return finishResult(res, err)
		}
	}

	generation := currentGeneration
	if mstate == manifestAbsent {
		generation = uuid.NewString()
	}
	commit := originalCommit
	if plan.commit != "" {
		commit = plan.commit
	}

	// ---- execute: additions first, then a reversible (quarantine-rename) removal pass ----
	var addedEntries []SkillEntry
	if len(plan.candidates) > 0 {
		addedEntries, err = publishPack(root, pack, plan.candidates, generation, commit)
		if err != nil {
			return finishResult(res, err)
		}
	}

	allRemovalSteps := append(append([]removalStep(nil), removalSteps...), legacyExtraSteps...)
	quarantined, removedCount, detachedCount, missingCount, removalWarnings, rmErr := executeRemovalQuarantined(root, allRemovalSteps, generation)
	if rmErr != nil {
		rollbackQuarantine(root, quarantined)
		cleanupPublishedCopies(root, addedEntries)
		return finishResult(res, rmErr)
	}

	newSkills := mergeKeptAndAdded(reviewed, desiredNames, addedEntries)
	if writeErr := commitSelection(root, pack, generation, commit, newSkills); writeErr != nil {
		rollbackQuarantine(root, quarantined)
		cleanupPublishedCopies(root, addedEntries)
		return finishResult(res, writeErr)
	}
	commitQuarantine(root)

	return finalSetResult(res, newSkills, generation, commit, originalCommit, len(addedEntries), removedCount, detachedCount, missingCount, removalWarnings), nil
}

// splitReviewedAndLegacyExtra separates a manifestValid pack's on-disk
// skills into "reviewed" (still present in pack's current catalog) and
// "legacy extra" (a whole-repository install from before skill-level review
// existed for this pack — spec-skill-packs.md §3.1's migration case). Both
// are nil for a manifestAbsent pack.
func splitReviewedAndLegacyExtra(pack Pack, m *Manifest, mstate manifestState) (reviewed, legacyExtra []SkillEntry) {
	if mstate != manifestValid {
		return nil, nil
	}
	catalogNames := make(map[string]bool, len(pack.Skills))
	for _, sk := range pack.Skills {
		catalogNames[sk.Name] = true
	}
	for _, sk := range m.Skills {
		if catalogNames[sk.Name] {
			reviewed = append(reviewed, sk)
		} else {
			legacyExtra = append(legacyExtra, sk)
		}
	}
	return reviewed, legacyExtra
}

// diffSelection derives PackSet's additions/removals: a desired name not
// currently reviewed-and-installed is an addition; a reviewed-and-installed
// skill not in desired is a removal.
func diffSelection(reviewed []SkillEntry, desired []string) (additionNames []string, toRemove []SkillEntry, desiredNames map[string]bool) {
	reviewedNames := make(map[string]bool, len(reviewed))
	for _, sk := range reviewed {
		reviewedNames[sk.Name] = true
	}
	desiredNames = make(map[string]bool, len(desired))
	for _, n := range desired {
		desiredNames[n] = true
	}
	for _, n := range desired {
		if !reviewedNames[n] {
			additionNames = append(additionNames, n)
		}
	}
	for _, sk := range reviewed {
		if !desiredNames[sk.Name] {
			toRemove = append(toRemove, sk)
		}
	}
	return additionNames, toRemove, desiredNames
}

// noopSetResult builds the result for a request whose desired set already
// exactly matches the current reviewed install with no legacy extras to
// migrate — a successful no-op, zero writes.
func noopSetResult(res Result, desired []string, reviewed []SkillEntry, originalCommit, currentRevision string) Result {
	res.Changed = false
	res.SkillCount = len(reviewed)
	res.Selected = append([]string(nil), desired...)
	res.Revision = currentRevision
	if len(reviewed) == 0 {
		res.State = StateAbsent
	} else {
		res.State = StateInstalled
		res.Commit = originalCommit
	}
	return res
}

// forceDetachSteps overrides a plain "clean → delete" verdict to "detach"
// for every step, and rewrites its note — spec-skill-packs.md §3.1's
// migration rule: a legacy extra is reported and detached, NEVER silently
// deleted, regardless of whether its content has drifted. preserve/missing
// steps pass through unchanged (nothing to force: there is no content to
// protect from deletion in either case).
func forceDetachSteps(steps []removalStep, packID string) []removalStep {
	out := make([]removalStep, len(steps))
	for i, s := range steps {
		if s.action == actionDelete || s.action == actionDetach {
			s.action = actionDetach
			s.note = fmt.Sprintf("%s copy of %q is outside %q's reviewed catalog and was preserved and detached", s.target, s.name, packID)
		}
		out[i] = s
	}
	return out
}

// mergeKeptAndAdded builds the new manifest's skill list: every reviewed
// entry that stays selected, plus every freshly added entry, sorted by name.
func mergeKeptAndAdded(reviewed []SkillEntry, desiredNames map[string]bool, added []SkillEntry) []SkillEntry {
	out := make([]SkillEntry, 0, len(reviewed)+len(added))
	for _, sk := range reviewed {
		if desiredNames[sk.Name] {
			out = append(out, sk)
		}
	}
	out = append(out, added...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// commitSelection writes the final manifest state: a full removal (empty
// newSkills) deletes the manifest file entirely (a schema-valid manifest
// v2 can never have zero skills — validateManifest enforces this), anything
// else writes the reconciled manifest.
func commitSelection(root *os.Root, pack Pack, generation, commit string, newSkills []SkillEntry) error {
	if len(newSkills) == 0 {
		return removeManifestFile(root, pack.ID)
	}
	newManifest := Manifest{
		SchemaVersion: manifestSchemaVersion, ID: pack.ID, Generation: generation,
		Source:  SourceRef{Repo: pack.Repo, CloneURL: pack.CloneURL, Ref: pack.Ref, Commit: commit},
		Targets: []string{string(TargetAgents), string(TargetClaude)},
		Skills:  newSkills,
	}
	return writeManifest(root, newManifest)
}

// finalSetResult builds the successful-apply Result, including the
// post-apply revision and selection so a caller never needs a follow-up
// Status read to keep going.
func finalSetResult(res Result, newSkills []SkillEntry, generation, commit, originalCommit string, added, removed, detached, missing int, warnings []string) Result {
	res.Changed = true
	res.SkillCount = len(newSkills)
	res.Warnings = warnings
	selectedNames := make([]string, len(newSkills))
	for i, sk := range newSkills {
		selectedNames[i] = sk.Name
	}
	sort.Strings(selectedNames)
	res.Selected = selectedNames
	if len(newSkills) == 0 {
		res.State = StateAbsent
		res.Commit = originalCommit
		res.Revision = computeRevision(res.PackID, "", nil)
	} else {
		res.State = StateInstalled
		res.Commit = commit
		res.Revision = computeRevision(res.PackID, generation, selectedNames)
	}
	res.Message = summarizeSet(added, removed, detached, missing)
	return res
}

func summarizeSet(added, removed, detached, missing int) string {
	var parts []string
	if added > 0 {
		parts = append(parts, fmt.Sprintf("%d added", added))
	}
	if removed > 0 {
		parts = append(parts, fmt.Sprintf("%d removed", removed))
	}
	if detached > 0 {
		parts = append(parts, fmt.Sprintf("%d preserved and detached", detached))
	}
	if missing > 0 {
		parts = append(parts, fmt.Sprintf("%d already missing", missing))
	}
	if len(parts) == 0 {
		return "Selection unchanged."
	}
	return "Selection updated: " + strings.Join(parts, "; ") + "."
}

// additionPlan is preflightAdditions' output: exactly what execute needs to
// publish, with every network access already done during preflight.
// candidates' SourceDir points inside tmpDir, so the caller must keep tmpDir
// alive (and is responsible for removing it) until publishPack has finished
// reading from it — preflightAdditions itself must NOT delete tmpDir before
// returning.
type additionPlan struct {
	candidates []Candidate
	commit     string // "" when there is nothing to add
	tmpDir     string // "" when there is nothing to add; caller owns cleanup
}

// preflightAdditions resolves additionNames' content without touching the
// workspace. For an existing pack (mstate == manifestValid) it fetches
// EXACTLY pinnedCommit (never the branch tip — spec-skill-packs.md §3.1: a
// selection change is not an update); for a brand-new install
// (manifestAbsent) there is nothing pinned yet, so it clones pack.Ref's tip,
// exactly like Add's own fresh-install path, and that clone's HEAD becomes
// the newly pinned commit.
func preflightAdditions(ctx context.Context, pack Pack, mstate manifestState, pinnedCommit string, additionNames []string) (additionPlan, error) {
	if len(additionNames) == 0 {
		return additionPlan{}, nil
	}
	bySkillName := make(map[string]CatalogSkill, len(pack.Skills))
	for _, sk := range pack.Skills {
		bySkillName[sk.Name] = sk
	}
	wanted := make([]CatalogSkill, 0, len(additionNames))
	for _, n := range additionNames {
		wanted = append(wanted, bySkillName[n]) // present by construction: n passed normalizeDesiredSkills' catalog check
	}

	tmpDir, err := os.MkdirTemp("", "zcp-skillpack-set-*")
	if err != nil {
		return additionPlan{}, wrapCoded(CodeFilesystem, err, "create temp fetch dir")
	}
	// Only an ERROR return cleans up tmpDir here: candidates' SourceDir
	// points inside it, so a SUCCESSFUL return hands tmpDir to the caller,
	// which must keep it alive until publishPack has read from it.
	fail := func(err error) (additionPlan, error) {
		_ = os.RemoveAll(tmpDir)
		return additionPlan{}, err
	}

	var commit string
	if mstate == manifestValid {
		commit = pinnedCommit
		if err := fetchCommit(ctx, pack.CloneURL, commit, tmpDir); err != nil {
			return fail(err)
		}
	} else {
		if err := cloneRepo(ctx, pack.CloneURL, pack.Ref, tmpDir); err != nil {
			return fail(err)
		}
		commit, err = headCommit(ctx, tmpDir)
		if err != nil {
			return fail(err)
		}
	}

	discovered, err := discoverSkills(tmpDir)
	if err != nil {
		return fail(err)
	}
	candidates, err := filterDiscoveredToNames(discovered, wanted)
	if err != nil {
		return fail(err)
	}
	return additionPlan{candidates: candidates, commit: commit, tmpDir: tmpDir}, nil
}

// filterDiscoveredToNames is preflightAdditions' catalog-intersection step
// (the addition-scoped counterpart to discover.go's
// filterDiscoveredToCatalog, which always checks the WHOLE pack catalog — a
// mismatch here would install a strict subset the caller never asked for).
// A wanted skill absent from what was actually discovered at the fetched
// commit is a hard error naming the skill, never a silent partial result.
func filterDiscoveredToNames(discovered []Candidate, want []CatalogSkill) ([]Candidate, error) {
	byKey := make(map[string]Candidate, len(discovered))
	for _, c := range discovered {
		byKey[c.Name+"\x00"+c.SourcePath] = c
	}
	filtered := make([]Candidate, 0, len(want))
	var missing []string
	for _, sk := range want {
		c, ok := byKey[sk.Name+"\x00"+sk.SourcePath]
		if !ok {
			missing = append(missing, sk.Name)
			continue
		}
		filtered = append(filtered, c)
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, codedErrorf(CodeInvalidSource, "catalogued skill(s) not found at the pinned commit: %s", strings.Join(missing, ", "))
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].SourcePath < filtered[j].SourcePath })
	return filtered, nil
}

// targetQuarantineParent is the removal-side counterpart to
// targetStagingParent: every removal/detach this call performs is first
// moved here (a single atomic rename, never a recursive delete) so it can be
// moved back if a LATER step in the same reconciliation fails.
func targetQuarantineParent(t Target) string {
	return filepath.Join(targetRootDir(t), ".zcp-skillpacks-quarantine")
}

func targetQuarantineDir(t Target, generation string) string {
	return filepath.Join(targetQuarantineParent(t), generation)
}

// quarantinedEntry records one removal/detach step that has already been
// moved into quarantine, so rollbackQuarantine can move it back.
type quarantinedEntry struct {
	step          removalStep
	quarantineRel string
}

// executeRemovalQuarantined performs planRemoval's decisions via an atomic
// rename into a per-generation quarantine directory instead of an in-place
// delete: a delete step's whole directory is renamed out of its live
// location; a detach step's marker FILE alone is renamed out (leaving its
// content untouched in place, exactly like the in-place detach it replaces).
// Every successfully quarantined step is appended to the returned slice
// BEFORE the next step is attempted, so a failure partway through never
// loses track of what must be rolled back. A permanent, best-effort delete
// of the quarantine tree only happens once the caller's new manifest state
// is itself durably committed (see commitQuarantine).
func executeRemovalQuarantined(root *os.Root, steps []removalStep, generation string) (quarantined []quarantinedEntry, removed, detached, missing int, warnings []string, err error) {
	for _, s := range steps {
		switch s.action {
		case actionDelete:
			qParent := targetQuarantineDir(s.target, generation)
			if mkErr := root.MkdirAll(qParent, 0o755); mkErr != nil {
				return quarantined, removed, detached, missing, warnings, wrapCoded(CodeFilesystem, mkErr, "create quarantine dir for %s", s.relDir)
			}
			qRel := filepath.Join(qParent, s.name)
			if rnErr := root.Rename(s.relDir, qRel); rnErr != nil {
				return quarantined, removed, detached, missing, warnings, wrapCoded(CodeFilesystem, rnErr, "quarantine %s for removal", s.relDir)
			}
			quarantined = append(quarantined, quarantinedEntry{step: s, quarantineRel: qRel})
			removed++
		case actionDetach:
			qParent := targetQuarantineDir(s.target, generation)
			if mkErr := root.MkdirAll(qParent, 0o755); mkErr != nil {
				return quarantined, removed, detached, missing, warnings, wrapCoded(CodeFilesystem, mkErr, "create quarantine dir for %s", s.relDir)
			}
			markerRel := filepath.Join(s.relDir, markerFileName)
			qRel := filepath.Join(qParent, s.name+".marker")
			if rnErr := root.Rename(markerRel, qRel); rnErr != nil {
				return quarantined, removed, detached, missing, warnings, wrapCoded(CodeFilesystem, rnErr, "detach marker at %s", s.relDir)
			}
			quarantined = append(quarantined, quarantinedEntry{step: s, quarantineRel: qRel})
			detached++
			warnings = append(warnings, s.note)
		case actionPreserve:
			warnings = append(warnings, s.note)
		case actionMissing:
			missing++
			warnings = append(warnings, s.note)
		}
	}
	return quarantined, removed, detached, missing, warnings, nil
}

// rollbackQuarantine reverses every already-quarantined step, most recent
// first, moving each removal target (or detached marker) back to its
// original location — best-effort, since it only ever runs after a failure
// that is itself already being reported.
func rollbackQuarantine(root *os.Root, quarantined []quarantinedEntry) {
	for i := len(quarantined) - 1; i >= 0; i-- {
		q := quarantined[i]
		switch q.step.action {
		case actionDelete:
			_ = root.Rename(q.quarantineRel, q.step.relDir)
		case actionDetach:
			_ = root.Rename(q.quarantineRel, filepath.Join(q.step.relDir, markerFileName))
		case actionPreserve, actionMissing:
			// never quarantined; nothing to roll back.
		}
	}
}

// commitQuarantine permanently discards every target's quarantine tree —
// called only once the new manifest state (or its removal) has already been
// durably written, mirroring publishPack's own best-effort staging cleanup.
func commitQuarantine(root *os.Root) {
	for _, t := range targets {
		_ = root.RemoveAll(targetQuarantineParent(t))
	}
}
