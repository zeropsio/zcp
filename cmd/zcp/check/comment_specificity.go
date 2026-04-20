package check

import (
	"context"
	"fmt"
	"io"

	opschecks "github.com/zeropsio/zcp/internal/ops/checks"
)

// comment-specificity grades the yaml block inside the integration-guide
// fragment for zerops-specific reasoning comments. The shim extracts
// the integration-guide → first ```yaml block, identical to the gate
// wrapper in checkCodebaseReadme.
//
// --showcase flag maps to the `isShowcase` predicate input.
func init() {
	registerCheck("comment-specificity", runCommentSpecificity)
}

func runCommentSpecificity(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("comment-specificity", stderr)
	cf := addCommonFlags(fs)
	hostname := fs.String("hostname", "", "codebase hostname (required)")
	showcase := fs.Bool("showcase", false, "treat as showcase tier (apply stricter floors)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *hostname == "" {
		fmt.Fprintln(stderr, "comment-specificity: --hostname is required")
		return 1
	}
	readmePath, content, err := readHostnameReadme(cf.path, *hostname)
	if err != nil {
		fmt.Fprintf(stderr, "comment-specificity: reading %s: %v\n", readmePath, err)
		return 1
	}
	igContent := extractFragmentBody(content, "integration-guide")
	if igContent == "" {
		fmt.Fprintln(stderr, "comment-specificity: SKIP — no integration-guide fragment found")
		return 0
	}
	yamlBlock := extractYAMLBlock(igContent)
	if yamlBlock == "" {
		fmt.Fprintln(stderr, "comment-specificity: SKIP — no ```yaml block in integration-guide")
		return 0
	}
	checks := opschecks.CheckCommentSpecificity(ctx, yamlBlock, *showcase)
	return emitResults(stdout, cf.json, checks)
}
