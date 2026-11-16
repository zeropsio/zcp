package ops

import (
	"context"
	"io"
)

// DeploySource yields the deploy source of a commit by HQ's rule: the
// repository's tree at the commit minus what its root `.deployignore` names,
// `export-ignore` off. It is the one interface CommitDeploy reads a commit
// through; today's implementation is zcp's relayed depth-1 scratch applying
// that rule, and an HQ read of the filtered source replaces it in one wiring
// place.
type DeploySource interface {
	// Open streams the deploy source of commit in repository (the name
	// ServiceMeta holds for the service) as the archive the Zerops upload
	// takes. The caller closes it.
	Open(ctx context.Context, repository, commit string) (io.ReadCloser, error)
}
