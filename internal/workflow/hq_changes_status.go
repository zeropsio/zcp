package workflow

import (
	"fmt"
	"strings"
)

// HQRepoChanges is one repository's changes as status names them, in one
// line: each change the ledger keeps there, and each branch zcp's leases hold
// there with no change yet.
type HQRepoChanges struct {
	Repo     string
	Changes  []HQChangeLine
	Branches []HQBranchLine
}

// HQChangeLine is one change: its number and branch, its standing as the
// agent was last told it (ready or draft, behind main, conflicting; an old
// one's age), and the leases on its branch, by alias.
type HQChangeLine struct {
	Number int
	Branch string
	Labels []string
	Leases []string
}

// HQBranchLine is a branch leases hold with no change of the Mate's yet.
type HQBranchLine struct {
	Branch string
	Leases []string
}

// renderHQChanges is status's Changes section: one line per repository,
// skipped when the Mate keeps no change and leases no branch in HQ.
func renderHQChanges(b *strings.Builder, env StateEnvelope) {
	if len(env.HQChanges) == 0 {
		return
	}
	fmt.Fprintln(b, "Changes:")
	for _, repo := range env.HQChanges {
		items := make([]string, 0, len(repo.Changes)+len(repo.Branches))
		for _, c := range repo.Changes {
			item := fmt.Sprintf("#%d %s", c.Number, c.Branch)
			if c.Branch == "" {
				item = fmt.Sprintf("#%d", c.Number)
			}
			if len(c.Labels) > 0 {
				item += " (" + strings.Join(c.Labels, " · ") + ")"
			}
			if len(c.Leases) > 0 {
				item += " — leases " + strings.Join(c.Leases, ", ")
			}
			items = append(items, item)
		}
		for _, br := range repo.Branches {
			items = append(items, fmt.Sprintf("branch %s — leases %s: no change yet", br.Branch, strings.Join(br.Leases, ", ")))
		}
		fmt.Fprintf(b, "  - HQ %s: %s\n", repo.Repo, strings.Join(items, "; "))
	}
}
