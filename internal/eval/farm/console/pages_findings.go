// Package console: this file, pages_findings.go, owns GET /findings (§8.3
// FM-51's findings page) — split out of pages.go (S-SHELL item 1) so each
// page owns one file.
package console

import "net/http"

// findingOwners is §7.5's owner vocabulary, in the order the findings page
// offers it as a filter.
var findingOwners = []string{"zcp-guidance", "zcp-tool", "platform", "agent", "scenario", "evaluator"}

// findingWindows are the findings page's window shortcuts (§8.3 FM-51).
var findingWindows = []string{"24h", "7d", "30d"}

// findingsPageData is GET /findings (§8.3 FM-51). The findings themselves
// reach the template only as Groups (owner-then-severity, §8.4) — there is
// no ungrouped Findings field, since findings.html never reads one.
type findingsPageData struct {
	Meta    pageMeta
	Since   string
	Owner   string
	Windows []string
	Owners  []string
	Groups  []findingGroup
}

// findingGroup is one owner's findings, in the read model's order.
type findingGroup struct {
	Owner string
	Items []FindingItem
}

func groupFindingsByOwner(items []FindingItem) []findingGroup {
	var groups []findingGroup
	for _, it := range items {
		if len(groups) == 0 || groups[len(groups)-1].Owner != it.Owner {
			groups = append(groups, findingGroup{Owner: it.Owner})
		}
		groups[len(groups)-1].Items = append(groups[len(groups)-1].Items, it)
	}
	return groups
}

func (s *Server) handleFindingsPage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	window, err := ParseWindow(q.Get("since"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rows, err := rowsSinceWindow(r.Context(), s.cfg.Store, s.cfg.ObserverDisabled, window, s.now(), s.queueState, s.runCache, s.summaryCache, s.logf)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	items := findingItemsFromRows(rows)

	owner := q.Get("owner")
	if owner != "" {
		filtered := make([]FindingItem, 0, len(items))
		for _, it := range items {
			if it.Owner == owner {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}

	since := q.Get("since")
	if since == "" {
		since = "24h"
	}
	renderPage(w, "findings", findingsPageData{
		Meta:  s.pageMeta(r, "Findings", navFindings, false),
		Since: since, Owner: owner, Windows: findingWindows, Owners: findingOwners,
		Groups: groupFindingsByOwner(items),
	})
}
