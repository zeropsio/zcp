package ops

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// repositoryRootMarker prefixes each root commit ReadRepositoryRoots reads,
// so nothing else a session prints is taken for one.
const repositoryRootMarker = "@@zcp-root "

// ReadRepositoryRoots reads the root commits of the checkout at workingDir
// on hostname that carry code — `git rev-list --max-parents=0 HEAD`, less
// every root whose tree is the empty tree — in one SSH round trip that
// writes nothing. Two checkouts sharing one of them are one repository, the
// reading a code volume's checkout is proven by (volumeContentShell). A root
// of the empty tree names no code: HQ seeds every repository's `main` with
// one, and `zcp init` makes one in every fresh checkout — the same commit in
// every checkout initialised in the same second — so it is never evidence of
// one repository. A checkout with no commit, or none but such roots, reads
// nil; an SSH call that fails is an error.
func ReadRepositoryRoots(ctx context.Context, ssh SSHDeployer, hostname, workingDir string) ([]string, error) {
	out, err := ssh.ExecSSH(ctx, hostname, repositoryRootsCommand(workingDir))
	if err != nil {
		return nil, fmt.Errorf("read the root commits of %s on %s: %w", workingDir, hostname, err)
	}
	var roots []string
	for line := range strings.Lines(string(out)) {
		if root, ok := strings.CutPrefix(strings.TrimSpace(line), repositoryRootMarker); ok && root != "" && !slices.Contains(roots, root) {
			roots = append(roots, root)
		}
	}
	return roots, nil
}

// repositoryRootsCommand prints, marked, each root commit of HEAD in
// workingDir whose tree is not the empty tree. GIT_OPTIONAL_LOCKS=0 keeps
// the read from writing into the repository; an unborn HEAD prints none.
func repositoryRootsCommand(workingDir string) string {
	return "cd " + shellQuote(workingDir) + ` && export GIT_OPTIONAL_LOCKS=0 && e=$(git hash-object -t tree /dev/null) && ` +
		`for c in $(git rev-list --max-parents=0 HEAD 2>/dev/null); do ` +
		`[ "$(git rev-parse "$c^{tree}")" = "$e" ] || echo "` + repositoryRootMarker + `$c"; done`
}

// RepositoryRootsShared reports whether two checkouts' roots, as
// ReadRepositoryRoots reads them, name one repository: a commit in both.
func RepositoryRootsShared(a, b []string) bool {
	return slices.ContainsFunc(a, func(root string) bool { return slices.Contains(b, root) })
}
