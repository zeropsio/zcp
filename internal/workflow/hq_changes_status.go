package workflow

import (
	"fmt"
	"strings"
)

// HQRepoChanges is one repository's changes as status names them, in one
// line: each change the ledger keeps there, and each branch zcp's leases hold
// there with no change yet.
type HQRepoChanges struct {
	Repo     string         `json:"repo"`
	Changes  []HQChangeLine `json:"changes"`
	Branches []HQBranchLine `json:"branches"`
}

// HQChangeLine is one change: its number and branch, its standing (ready or
// draft, behind main, conflicting; an old one's age), and the leases on its
// branch, by alias. Merged is a change HQ merged since zcp last told it: its
// leases hold merged work.
type HQChangeLine struct {
	Number int      `json:"number"`
	Branch string   `json:"branch"`
	Labels []string `json:"labels"`
	Leases []string `json:"leases"`
	Merged bool     `json:"merged"`
}

// HQBranchLine is a branch leases hold with no change of the Mate's yet.
type HQBranchLine struct {
	Branch string   `json:"branch"`
	Leases []string `json:"leases"`
}

// renderHQChanges is status's Changes section: one line per repository,
// skipped when the Mate keeps no change and leases no branch in HQ, labelled
// as last read when HQ did not answer status's read of it.
func renderHQChanges(b *strings.Builder, env StateEnvelope) {
	if len(env.HQChanges) == 0 {
		return
	}
	if env.HQChangesAsLastRead {
		fmt.Fprintln(b, "Changes (as last read — HQ did not answer now):")
	} else {
		fmt.Fprintln(b, "Changes:")
	}
	for _, repo := range env.HQChanges {
		items := make([]string, 0, len(repo.Changes)+len(repo.Branches))
		for _, c := range repo.Changes {
			item := fmt.Sprintf("#%d %s", c.Number, c.Branch)
			if c.Branch == "" {
				item = fmt.Sprintf("#%d", c.Number)
			}
			if c.Merged {
				items = append(items, item+mergedWords(c.Leases))
				continue
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

// mergedWords is what a merged change's item says after its number and
// branch: the leases still on its branch hold merged work, to release, and new
// work starts from main.
func mergedWords(leases []string) string {
	if len(leases) == 0 {
		return ": merged — start new work from main"
	}
	return fmt.Sprintf(" — leases %s: merged — release them, or start new work from main", strings.Join(leases, ", "))
}
