package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/workflow"
)

// checkManifestRouteToPopulated asserts every entry in
// ZCP_CONTENT_MANIFEST.json carries a non-empty `routed_to` that matches the
// FactRouteTo* enum. Closes the v34 class where the writer subagent emitted
// a manifest whose claude_md-routed facts then appeared in the published
// gotchas list — the drift was visible in the manifest (empty or off-enum
// routed_to) but nothing gated on it.
//
// Upstream concerns (file missing / invalid JSON) are the responsibility of
// the writer_content_manifest_exists + writer_content_manifest_valid checks
// from C-5's content-manifest battery. This check passes silently on those
// surfaces so we don't pile multiple fails onto the same root cause.
func checkManifestRouteToPopulated(manifestPath string) []workflow.StepCheck {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return []workflow.StepCheck{{
			Name:   "manifest_route_to_populated",
			Status: statusPass,
		}}
	}
	var manifest contentManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return []workflow.StepCheck{{
			Name:   "manifest_route_to_populated",
			Status: statusPass,
		}}
	}

	type violation struct {
		title  string
		reason string
	}
	var viols []violation
	for _, entry := range manifest.Facts {
		title := strings.TrimSpace(entry.FactTitle)
		if title == "" {
			title = "(untitled)"
		}
		routed := strings.TrimSpace(entry.RoutedTo)
		if routed == "" {
			viols = append(viols, violation{title: title, reason: "empty routed_to"})
			continue
		}
		if !ops.IsKnownFactRouteTo(routed) {
			viols = append(viols, violation{title: title, reason: fmt.Sprintf("unknown routed_to=%q", routed)})
		}
	}
	if len(viols) == 0 {
		return []workflow.StepCheck{{
			Name:   "manifest_route_to_populated",
			Status: statusPass,
		}}
	}

	sort.Slice(viols, func(i, j int) bool {
		if viols[i].title != viols[j].title {
			return viols[i].title < viols[j].title
		}
		return viols[i].reason < viols[j].reason
	})
	labels := make([]string, 0, len(viols))
	for _, v := range viols {
		labels = append(labels, fmt.Sprintf("%q: %s", v.title, v.reason))
	}
	return []workflow.StepCheck{{
		Name:   "manifest_route_to_populated",
		Status: statusFail,
		Detail: fmt.Sprintf(
			"ZCP_CONTENT_MANIFEST.json has %d entry/entries with empty or off-enum routed_to: %s. Every manifest fact must declare a valid routing destination from the FactRouteTo* enum (content_gotcha, content_intro, content_ig, content_env_comment, claude_md, zerops_yaml_comment, scaffold_preamble, feature_preamble, discarded). v34's DB_PASS class shipped with claude_md-routed facts leaking into the published gotcha list because the drift was visible in the manifest but nothing gated on populated + valid routed_to.",
			len(viols), strings.Join(labels, "; "),
		),
	}}
}
