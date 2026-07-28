package skillpacks

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"
)

// Result is the outcome of one Add, Remove call — the shared shape both the
// human CLI summary and the --json mutation object are built from (the CLI
// layer adds the "version" envelope field).
type Result struct {
	Operation  string
	PackID     string
	Code       string // empty means success
	Message    string
	State      State
	Commit     string
	SkillCount int
	Changed    bool
	Warnings   []string
	// Revision is PackSet's post-apply opaque selection revision (see
	// computeRevision) — empty for Add/Remove, which have no selection
	// concept of their own.
	Revision string
	// Selected is PackSet's exact post-apply installed skill-name set,
	// sorted — empty for Add/Remove.
	Selected []string
}

// OK reports whether this Result represents success — every failure path
// sets Code, so its absence is the single success signal.
func (r Result) OK() bool { return r.Code == "" }

// finishResult applies err's stable code/message onto res and returns both,
// so a caller can populate res's other fields ahead of time (Operation,
// PackID, whatever State/Commit/SkillCount was already known) and just
// thread failures through this one call.
func finishResult(res Result, err error) (Result, error) {
	if err == nil {
		return res, nil
	}
	res.Code, res.Message = codeAndMessage(err)
	return res, err
}

// Add installs the catalog pack id into cwd's two agent-discovery roots. A
// healthy existing install is a successful no-op (no network, no writes); a
// modified or incomplete install refuses without writes (see
// docs/spec-welcome-mode.md's pack lifecycle table for the full state
// matrix — pack-add's job here is only the absent→installed transition).
func Add(ctx context.Context, cwd, id string) (Result, error) {
	res := Result{Operation: "add", PackID: id}

	pack, ok := Lookup(id)
	if !ok {
		return finishResult(res, codedErrorf(CodeUnknownPack, "unknown skill pack %q (valid: %s)", id, strings.Join(ValidIDs(), ", ")))
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
		// proceed to addFresh below
	case manifestLegacy:
		return finishResult(res, codedErrorf(CodeLegacyState,
			"a legacy (pre-v2) manifest for %q already exists; run `zcp skills pack-remove %s` then `zcp skills pack-add %s` again to recover", id, id, id))
	case manifestCorrupt:
		return finishResult(res, codedErrorf(CodeCorruptState,
			"the manifest for %q is corrupt; run `zcp skills pack-remove %s` then `zcp skills pack-add %s` again to recover", id, id, id))
	case manifestValid:
		overall, warnings, auditErr := auditManifest(root, m)
		if auditErr != nil {
			return finishResult(res, auditErr)
		}
		res.State, res.Commit, res.SkillCount, res.Warnings = overall, m.Source.Commit, len(m.Skills), warnings
		switch overall {
		case StateInstalled:
			res.Changed = false
			return res, nil
		case StateModified:
			return finishResult(res, codedErrorf(CodeLocalChanges, "skill pack %q has local modifications; nothing was changed", id))
		case StateIncomplete:
			return finishResult(res, codedErrorf(CodeIncomplete,
				"skill pack %q is incomplete; run `zcp skills pack-remove %s` then `zcp skills pack-add %s` to repair", id, id, id))
		case StateAbsent, StateBroken:
			return finishResult(res, codedErrorf(CodeInternal, "unexpected pack state %q auditing %q", overall, id))
		}
	}

	return addFresh(ctx, root, pack, res)
}

// addFresh performs the absent→installed install: clone, discover, collision
// preflight, staged-build-then-publish, manifest commit. res arrives with
// Operation/PackID already set (and, on the manifestValid/StateInstalled
// path above, this is never reached).
func addFresh(ctx context.Context, root *os.Root, pack Pack, res Result) (Result, error) {
	res.State = StateAbsent

	tmpDir, err := os.MkdirTemp("", "zcp-skillpack-*")
	if err != nil {
		return finishResult(res, wrapCoded(CodeFilesystem, err, "create temp clone dir"))
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	if err := cloneRepo(ctx, pack.CloneURL, pack.Ref, tmpDir); err != nil {
		return finishResult(res, err)
	}
	commit, err := headCommit(ctx, tmpDir)
	if err != nil {
		return finishResult(res, err)
	}

	candidates, err := discoverSkills(tmpDir)
	if err != nil {
		return finishResult(res, err)
	}
	candidates, err = filterDiscoveredToCatalog(pack, candidates)
	if err != nil {
		return finishResult(res, err)
	}
	if err := preflightCollisions(root, candidates); err != nil {
		return finishResult(res, err)
	}

	generation := uuid.NewString()
	skillEntries, err := publishPack(root, pack, candidates, generation, commit)
	if err != nil {
		return finishResult(res, err)
	}

	manifest := Manifest{
		SchemaVersion: manifestSchemaVersion,
		ID:            pack.ID,
		Generation:    generation,
		Source:        SourceRef{Repo: pack.Repo, CloneURL: pack.CloneURL, Ref: pack.Ref, Commit: commit},
		Targets:       []string{string(TargetAgents), string(TargetClaude)},
		Skills:        skillEntries,
	}
	if err := writeManifest(root, manifest); err != nil {
		cleanupPublishedCopies(root, skillEntries)
		return finishResult(res, err)
	}

	res.State = StateInstalled
	res.Commit = commit
	res.SkillCount = len(skillEntries)
	res.Changed = true
	return res, nil
}

// preflightCollisions checks every candidate's destination name against
// BOTH target roots before any write. Any collision, in either root, aborts
// the whole pack with zero writes — there is no per-skill rename fallback.
func preflightCollisions(root *os.Root, candidates []Candidate) error {
	for _, c := range candidates {
		for _, t := range targets {
			exists, err := rootExists(root, targetSkillDest(t, c.Name))
			if err != nil {
				return err
			}
			if exists {
				return codedErrorf(CodeCollision, "skill %q already exists under %s; nothing was changed", c.Name, targetSkillsDir(t))
			}
		}
	}
	return nil
}

// publishPack builds every candidate's copy for every target in a hidden,
// generation-scoped staging directory, then publishes each with a
// no-replace rename — so a final destination only ever appears via one
// rename of an already-complete tree, never a partially-copied directory.
// Any failure rolls back everything this call itself created (staged and
// already-published alike) before returning.
func publishPack(root *os.Root, pack Pack, candidates []Candidate, generation, commit string) ([]SkillEntry, error) {
	var publishedFinal []string
	stagingRoots := make(map[Target]string, len(targets))

	cleanup := func() {
		for _, rel := range publishedFinal {
			_ = root.RemoveAll(rel)
		}
		for _, t := range targets {
			_ = root.RemoveAll(targetStagingParent(t))
		}
	}

	for _, t := range targets {
		if err := root.MkdirAll(targetSkillsDir(t), 0o755); err != nil {
			cleanup()
			return nil, wrapCoded(CodeFilesystem, err, "create %s", targetSkillsDir(t))
		}
		stagingRoot := targetStagingDir(t, generation)
		stagingRoots[t] = stagingRoot
		if err := root.MkdirAll(stagingRoot, 0o755); err != nil {
			cleanup()
			return nil, wrapCoded(CodeFilesystem, err, "create staging directory for %s", t)
		}
	}

	entries := make([]SkillEntry, 0, len(candidates))
	for _, c := range candidates {
		digest, err := publishOneSkill(root, pack, c, generation, commit, stagingRoots, &publishedFinal)
		if err != nil {
			cleanup()
			return nil, err
		}
		entries = append(entries, SkillEntry{Name: c.Name, SourcePath: c.SourcePath, Digest: digest})
	}

	for _, t := range targets {
		_ = root.RemoveAll(targetStagingParent(t))
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

// publishOneSkill builds and publishes candidate c's copy for every target,
// verifying both physical copies hash identically before either is
// published (their marker's target field is the only thing that differs).
func publishOneSkill(
	root *os.Root, pack Pack, c Candidate, generation, commit string,
	stagingRoots map[Target]string, publishedFinal *[]string,
) (string, error) {
	digest := ""
	for _, t := range targets {
		stagingSkillRel := filepath.Join(stagingRoots[t], c.Name)
		if err := copyTreeIntoRoot(root, c.SourceDir, stagingSkillRel); err != nil {
			return "", err
		}
		d, err := treeDigest(root, stagingSkillRel)
		if err != nil {
			return "", err
		}
		if digest == "" {
			digest = d
		} else if digest != d {
			return "", codedErrorf(CodeInternal, "content digest differs between .agents and .claude copies of %q", c.Name)
		}

		marker := Marker{
			SchemaVersion: markerSchemaVersion, PackID: pack.ID, Generation: generation,
			Target: string(t), SkillName: c.Name, SourcePath: c.SourcePath, Commit: commit, Digest: d,
		}
		if err := writeMarker(root, stagingSkillRel, marker); err != nil {
			return "", err
		}

		finalRel := targetSkillDest(t, c.Name)
		if err := renameNoReplace(root, stagingSkillRel, finalRel); err != nil {
			return "", err
		}
		*publishedFinal = append(*publishedFinal, finalRel)
	}
	return digest, nil
}

// cleanupPublishedCopies best-effort removes every published skill
// directory — used only when the manifest commit itself fails after every
// copy already landed at its final location.
func cleanupPublishedCopies(root *os.Root, entries []SkillEntry) {
	for _, e := range entries {
		for _, t := range targets {
			_ = root.RemoveAll(targetSkillDest(t, e.Name))
		}
	}
}
