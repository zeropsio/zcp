package ops

import (
	"encoding/base64"
	"fmt"
	"path"
	"strings"
)

// BuildWriteRepoFileCommand writes body at relPath inside workingDir — and
// writes NOTHING when the file is already byte-identical.
//
// The "only when it differs" part is the whole point: this runs on a
// reconcile pass that may fire many times, in a directory that IS a git
// working tree, and a rewrite of identical bytes would show up as a modified
// file in `git status` and in the deploy's dirty-tree warning, telling the
// Mate it has work to commit that it does not.
//
// The body travels base64-encoded rather than shell-quoted: the files this
// writes are YAML the caller composes, and an encoder that cannot be confused
// by a quote, a backtick or a `$` is worth more than a readable command line.
// The decode lands on a scratch file beside the target so a half-written file
// can never be what the tree ends up with.
func BuildWriteRepoFileCommand(workingDir, relPath, body string) string {
	dir := path.Dir(relPath)
	scratch := relPath + ".zcp-new"
	parts := []string{"cd " + shellQuote(workingDir)}
	if dir != "" && dir != "." {
		parts = append(parts, "mkdir -p "+shellQuote(dir))
	}
	parts = append(parts,
		fmt.Sprintf("printf %%s %s | base64 -d > %s",
			shellQuote(base64.StdEncoding.EncodeToString([]byte(body))), shellQuote(scratch)),
		fmt.Sprintf("(cmp -s %s %s && rm -f %s || mv -f %s %s)",
			shellQuote(scratch), shellQuote(relPath), shellQuote(scratch),
			shellQuote(scratch), shellQuote(relPath)),
	)
	return strings.Join(parts, " && ")
}
