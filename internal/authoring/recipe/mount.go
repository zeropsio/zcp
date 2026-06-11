package recipe

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// recipeMountBase is the SSHFS mount base in the ZCP container — the
// directory that hosts per-codebase dev mounts (`apidev/`, `appdev/`,
// `workerdev/`). Recipe outputs MUST nest under it (canonical shape:
// `<recipeMountBase>/zcprecipator/<slug>/`); writing AT the mount base
// or any ancestor would shadow the porter's source tree with stitched
// recipe artifacts.
//
// This duplicates `internal/workflow.recipeMountBase` and
// `internal/ops.mountBase` deliberately — `internal/recipe/` is a peer
// of `internal/workflow/` per the layered-architecture contract in
// CLAUDE.md, so cross-imports between peers are forbidden. The constant
// is small enough to re-declare; the alternative (promoting it to
// `internal/topology/`) would over-elevate a filesystem path that has
// no platform-vocabulary meaning.
const recipeMountBase = "/var/www"

// refuseNonCanonicalMountOutput returns a refusal error when outputRoot is
// not a safe recipe-output location relative to the SSHFS mount base. Two
// shadowing shapes are refused:
//
//   - equal-to or ancestor-of the mount base (`/var/www`, `/var`, `/`) —
//     F-28 (run-25 ANALYSIS.md §Axis Z): a deliverable written to
//     `/var/www/` collapsed stitched output into the dev-codebase tree.
//   - under the mount base but OUTSIDE the canonical `zcprecipator/`
//     subtree (e.g. `/var/www/<slug>`) — run-50: the same shadowing one
//     directory shallower, produced by an agent that dropped the
//     `zcprecipator/` path segment the guidance specifies.
//
// Paths off the mount base entirely (local dev, CI tmpdirs) and paths
// under `/var/www/zcprecipator/` are accepted. Substring lookalikes
// (`/var/www2`, `/var/www-other`) are not under the mount base and pass —
// the trailing-separator comparison is what differentiates them.
func refuseNonCanonicalMountOutput(outputRoot, slug string) error {
	cleaned := filepath.Clean(outputRoot)
	sep := string(os.PathSeparator)
	canonicalBase := filepath.Join(recipeMountBase, "zcprecipator")
	canonical := filepath.Join(canonicalBase, slug)

	// Filesystem root is an ancestor of every absolute path. filepath.Clean
	// turns "/" into sep, where the trailing-separator prefix trick below
	// degenerates ("//"), so refuse it explicitly.
	if cleaned == sep {
		return nonCanonicalMountErr(outputRoot, canonical)
	}
	// Canonical zcprecipator/<slug> subtree → the intended output location.
	// The bare base (cleaned == canonicalBase) is deliberately NOT accepted:
	// the canonical shape requires a slug segment, so a slug-less base falls
	// through to refusal below rather than letting artifacts collide in the
	// shared zcprecipator/ dir.
	if strings.HasPrefix(cleaned, canonicalBase+sep) {
		return nil
	}
	// Under the mount base but outside zcprecipator/ → shadows dev mounts.
	if cleaned == recipeMountBase || strings.HasPrefix(cleaned, recipeMountBase+sep) {
		return nonCanonicalMountErr(outputRoot, canonical)
	}
	// Ancestor of the mount base (e.g. `/var`) → would contain the mounts.
	if strings.HasPrefix(recipeMountBase+sep, cleaned+sep) {
		return nonCanonicalMountErr(outputRoot, canonical)
	}
	// Off the mount base entirely → no constraint.
	return nil
}

func nonCanonicalMountErr(outputRoot, canonical string) error {
	return fmt.Errorf(
		"outputRoot %q must nest under %q so it doesn't shadow the SSHFS dev codebase mounts under %q — retry action=start with outputRoot=%q",
		outputRoot, recipeMountBase+"/zcprecipator/", recipeMountBase, canonical,
	)
}
