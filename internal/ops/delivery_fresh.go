package ops

import (
	"fmt"
)

// freshDeliveryBase is part of an explicit delivery, after main has been merged.
// A landed change's history is kept under refs/zcp/landed/<squash>. The next
// change starts at main, with any newer work carried as the delivery's commit.
// Only ancestry proved to include the landed head is cut; a different checkout
// is left for the ordinary merge. The index and working tree are never reset.
func freshDeliveryBase(commit, head, message string) string {
	if commit == "" || head == "" {
		return ":"
	}
	return `{ landed=` + shellQuote(commit) + `; landed_head=` + shellQuote(head) + `; ` +
		`if git merge-base --is-ancestor "$landed_head" HEAD && git merge-base --is-ancestor "$landed" origin/main; then ` +
		`old=$(git rev-parse HEAD) && base=$(git rev-parse origin/main) && tree=$(git rev-parse "HEAD^{tree}") && ` +
		`if [ "$tree" = "$(git rev-parse "origin/main^{tree}")" ]; then next=$base; ` +
		`else next=$(git commit-tree "$tree" -p "$base" -m ` + shellQuote(message) + `); fi && ` +
		`git update-ref "refs/zcp/landed/$landed" "$old" && git update-ref HEAD "$next" "$old" && ` +
		`echo "ZCP_FRESH_BASE:$base"; fi; }`
}

// DeliveryFreshBase reads the main head the explicit delivery started from.
func DeliveryFreshBase(output string) string { return markedLine(output, "ZCP_FRESH_BASE:") }

// BuildDeliveryTreeCommand names the exact content delivered to HQ, regardless of history.
func BuildDeliveryTreeCommand(workingDir string) string {
	return fmt.Sprintf("git -C %s rev-parse 'HEAD^{tree}'", shellQuote(workingDir))
}
