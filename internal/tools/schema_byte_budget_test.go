package tools_test

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/zeropsio/zcp/internal/runtime"
)

// TestInputSchemaByteBudget is a RATCHET on the static context cost of the
// non-authoring tool surface: the marshaled JSON byte size of each tool's
// published InputSchema. That schema ships to every MCP client at
// `tools/list` and is paid on EVERY session start — measured at ~9.9k tokens
// of input-schema across 19 local tools, of which `zerops_workflow` alone is
// ~42% (audit 2026-06-13, plans/nonauthoring-context-audit-2026-06-13.md).
//
// The budget is a CEILING, not a target: a tool's schema may shrink freely,
// but may not GROW past its recorded ceiling without a deliberate bump here.
// This makes the planned Phase-1 description trims measurable (the number
// drops, and the ceiling drops with it in the same PR) and prevents silent
// regrowth of the surface the audit just measured.
//
// Why bytes-of-schema, not words-of-description: the audit's headline cost is
// the per-field jsonschema descriptions embedded in the InputSchema (essays,
// inline examples, UI walkthroughs), NOT the tool-level Description — the
// latter is already capped by TestAnnotations_DescriptionWordCount. The
// input-schema bytes are the surface that was both the largest AND unguarded.
//
// Variant invariance: field jsonschema tags are compiled-in struct tags,
// identical across container/local runtime. The only schema that differs by
// variant is `zerops_deploy` (SSH adds sourceService → larger). listAllTools
// here builds the SSH/container-capable variant (sshDeployer non-nil), whose
// per-tool schema bytes are >= the local variant pointwise (local deploy is
// strictly smaller; deploy_batch + dev_server are SSH-only). So a ceiling
// satisfied here is satisfied in local mode too — one baseline binds both.
//
// `zerops_browser` is exempt: it registers only InContainer + agent-browser
// on PATH (gated, absent here), and its annotations are pinned separately by
// TestAnnotations_BrowserTool.
//
// To LOWER a ceiling (the Phase-1 goal): trim the field descriptions, then
// update the number here in the SAME commit. To ADD a tool: add its measured
// ceiling. Never raise an existing ceiling without an explicit rationale in
// the commit — a raise is the audit's "validation set as presentation set"
// regression creeping back.
func TestInputSchemaByteBudget(t *testing.T) {
	t.Parallel()

	// Per-tool ceiling = marshaled InputSchema bytes, measured 2026-06-13
	// after P0.1 (Variant desc trim) + P0.3, max across local/SSH variants.
	ceilings := map[string]int{
		"zerops_workflow":           17128,
		"zerops_record_fact":        3299,
		"zerops_dev_server":         3220,
		"zerops_knowledge":          2945,
		"zerops_deploy":             1908,
		"zerops_env":                2484,
		"zerops_scale":              2242,
		"zerops_import":             1910,
		"zerops_discover":           879,
		"zerops_preprocess":         811,
		"zerops_workspace_manifest": 716,
		"zerops_logs":               712,
		"zerops_deploy_batch":       608,
		"zerops_manage":             453,
		"zerops_subdomain":          393,
		"zerops_mount":              306,
		"zerops_process":            266,
		"zerops_events":             259,
		"zerops_delete":             179,
		"zerops_export":             177,
		"zerops_verify":             177,
	}

	// browser is container+binary-gated; covered by TestAnnotations_BrowserTool.
	exempt := map[string]bool{"zerops_browser": true}

	toolMap := listAllTools(t, runtime.Info{})

	// Every advertised tool must carry a ceiling (catches new bloat from day
	// one) and stay under it.
	for name, tool := range toolMap {
		if exempt[name] {
			continue
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b := []byte("null")
			if tool.InputSchema != nil {
				marshaled, err := json.Marshal(tool.InputSchema)
				if err != nil {
					t.Fatalf("marshal InputSchema for %s: %v", name, err)
				}
				b = marshaled
			}
			ceiling, ok := ceilings[name]
			if !ok {
				t.Fatalf("tool %s: no input-schema byte ceiling recorded (got %d bytes). "+
					"Add an entry to `ceilings` with the measured size — new tools must "+
					"carry a budget from day one so schema bloat can't grow unreviewed.", name, len(b))
			}
			if len(b) > ceiling {
				t.Errorf("tool %s: InputSchema is %d bytes, exceeds ceiling %d (+%d). "+
					"The static per-session context cost grew. Trim field jsonschema "+
					"descriptions (drop examples / UI walkthroughs / enum rationale → atoms), "+
					"OR if the growth is a deliberate new field, raise the ceiling here WITH "+
					"a rationale in the commit message.", name, len(b), ceiling, len(b)-ceiling)
			}
		})
	}

	// A ceiling for a tool that no longer registers is dead config — flag it
	// so removals stay clean (Clean Code, Not Partial Patches).
	t.Run("no_stale_ceilings", func(t *testing.T) {
		t.Parallel()
		var stale []string
		for name := range ceilings {
			if _, ok := toolMap[name]; !ok {
				stale = append(stale, name)
			}
		}
		sort.Strings(stale)
		if len(stale) > 0 {
			t.Errorf("ceilings has entries for unregistered tools (remove them): %v", stale)
		}
	})
}
