package skillpacks

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Target is a closed enum of the two physical agent-discovery roots every
// pack installs into. It is never manifest- or marker-supplied as an
// arbitrary path — every directory this package touches is derived from one
// of these two constants plus a validated skill name.
type Target string

const (
	TargetAgents Target = "agents"
	TargetClaude Target = "claude"
)

// targets is the fixed, ordered set of physical copies every pack
// materializes.
var targets = []Target{TargetAgents, TargetClaude}

// targetRootDir returns t's workspace-relative root directory: ".agents" or
// ".claude".
func targetRootDir(t Target) string {
	switch t {
	case TargetAgents:
		return ".agents"
	case TargetClaude:
		return ".claude"
	default:
		panic(fmt.Sprintf("skillpacks: unknown target %q", t)) // unreachable: Target is closed to this package
	}
}

// targetSkillsDir returns t's workspace-relative skills directory:
// ".agents/skills" or ".claude/skills".
func targetSkillsDir(t Target) string {
	return filepath.Join(targetRootDir(t), "skills")
}

// targetStagingParent returns t's hidden staging container — everything
// under it is ephemeral (created and fully removed within a single Add
// call, always while holding the exclusive skill-packs lock), so it is
// always safe to remove in its entirety once that call is done, rather than
// leaving an empty generation-scoped husk behind.
func targetStagingParent(t Target) string {
	return filepath.Join(targetRootDir(t), ".zcp-skillpacks-staging")
}

// targetStagingDir returns a hidden, generation-scoped staging directory
// under t's own root — same physical root as the final destination, so
// publishing a staged skill copy is always an intra-filesystem rename.
func targetStagingDir(t Target, generation string) string {
	return filepath.Join(targetStagingParent(t), generation)
}

// targetSkillDest returns t's workspace-relative path for the installed
// skill directory named name.
func targetSkillDest(t Target, name string) string {
	return filepath.Join(targetSkillsDir(t), name)
}

// workspaceGuardedPaths are every ancestor a symlink must never be found at
// before skillpacks trusts the workspace: os.Root's own escape-detection
// only refuses a symlink that would resolve OUTSIDE the workspace tree, but
// a symlink pointing at another path INSIDE the workspace (e.g.
// ".claude -> .agents") would not be caught that way and would silently
// collapse the "two physical copies" invariant. These five paths are
// Lstat-checked explicitly, every call, before any mutation.
func workspaceGuardedPaths() []string {
	return []string{
		targetRootDir(TargetAgents),
		targetSkillsDir(TargetAgents),
		targetRootDir(TargetClaude),
		targetSkillsDir(TargetClaude),
		".zcp",
	}
}

// openWorkspaceRoot opens the project workspace (cwd, as passed by the
// CLI — trusted, not attacker input) as an os.Root spanning .agents,
// .claude, and .zcp, and verifies none of the five workspace-guarded paths
// is itself a symlink. Every skillpacks filesystem operation resolves its
// paths through this Root rather than raw os/filepath calls on an absolute
// path: os.Root refuses to resolve any path whose resolution would leave
// the root directory through an intermediate symlinked component, closing
// the escape-to-outside-the-workspace case; the explicit Lstat pass below
// closes the complementary in-workspace case os.Root does not cover (see
// workspaceGuardedPaths).
func openWorkspaceRoot(cwd string) (*os.Root, error) {
	root, err := os.OpenRoot(cwd)
	if err != nil {
		return nil, wrapCoded(CodeFilesystem, err, "open workspace %q", cwd)
	}
	if err := rejectSymlinkedGuardedPaths(root); err != nil {
		_ = root.Close()
		return nil, err
	}
	return root, nil
}

func rejectSymlinkedGuardedPaths(root *os.Root) error {
	for _, rel := range workspaceGuardedPaths() {
		info, err := root.Lstat(rel)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return wrapCoded(CodeFilesystem, err, "check workspace path %q", rel)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return codedErrorf(CodeFilesystem, "%q is a symlink; skill packs require a real directory there", rel)
		}
	}
	return nil
}

// rootExists reports whether relPath exists within root (Lstat, so a
// symlink itself counts as existing without following it), treating "does
// not exist" as false with no error. Any other error — including os.Root's
// own escape-detection — is returned so the caller fails closed.
func rootExists(root *os.Root, relPath string) (bool, error) {
	_, err := root.Lstat(relPath)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, wrapCoded(CodeFilesystem, err, "check %s", relPath)
}
