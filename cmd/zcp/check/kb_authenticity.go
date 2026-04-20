package check

import (
	"context"
	"fmt"
	"io"

	opschecks "github.com/zeropsio/zcp/internal/ops/checks"
)

// kb-authenticity scores the knowledge-base fragment's gotchas for
// platform-anchored / failure-mode-described shape. Fails when fewer
// than minAuthenticGotchas qualify. Predicate returns nil (SKIP) when
// no gotchas are extractable — that's the fragment-exists check's
// surface, so the shim doesn't pile-on.
func init() {
	registerCheck("kb-authenticity", runKBAuthenticity)
}

func runKBAuthenticity(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("kb-authenticity", stderr)
	cf := addCommonFlags(fs)
	hostname := fs.String("hostname", "", "codebase hostname (required)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *hostname == "" {
		fmt.Fprintln(stderr, "kb-authenticity: --hostname is required")
		return 1
	}
	readmePath, content, err := readHostnameReadme(cf.path, *hostname)
	if err != nil {
		fmt.Fprintf(stderr, "kb-authenticity: reading %s: %v\n", readmePath, err)
		return 1
	}
	kb := extractFragmentBody(content, "knowledge-base")
	if kb == "" {
		fmt.Fprintln(stderr, "kb-authenticity: SKIP — no knowledge-base fragment found")
		return 0
	}
	checks := opschecks.CheckKnowledgeBaseAuthenticity(ctx, kb, *hostname)
	return emitResults(stdout, cf.json, checks)
}
