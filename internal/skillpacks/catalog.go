// Package skillpacks implements `zcp skills pack-add`/`pack-remove`/
// `pack-status`: install/uninstall/inspect curated community skill repos
// into a project workspace's two agent-discovery roots, .agents/skills/ and
// .claude/skills/. It owns the enum-closed catalog of installable packs, the
// recursive skill-root discovery pipeline, the staged-build-then-publish
// materialization, and the versioned manifest/marker files that let
// pack-status and pack-remove tell ZCP-owned content apart from a user's own
// edits.
package skillpacks

import "sort"

// Pack describes one curated, installable community skill-pack repo entry.
// The catalog (below) is the single authority for this data on the Go side;
// the welcome extension keeps its own parallel id list (drift-guarded by
// TestCatalogIDs_MatchWelcomeExtensionAllowlist).
type Pack struct {
	ID          string
	Repo        string // "owner/name"
	CloneURL    string
	Ref         string // branch/tag to clone; the installed commit is pinned separately in the manifest
	Title       string
	Description string
}

// catalog is the enum-closed set of installable skill packs (verified live
// 2026-07-23). Adding an entry here is the only way to make a new pack
// installable — pack-add refuses any id outside this set. pack-remove does
// NOT consult this set (a retired id — removed from the catalog but still
// manifest-recorded — must remain removable), see Remove.
//
// gstack (garrytan/gstack) was deliberately excluded: it has no top-level
// skills/ directory, so the whole repo would flatten into one root skill —
// and its repo is a ~56MB application monorepo, not a skills collection,
// live-verified 2026-07-23. Dumping that wholesale into a user's skill roots
// on one command is the wrong product shape.
var catalog = []Pack{
	{
		ID: "matt-pocock-skills", Repo: "mattpocock/skills",
		CloneURL: "https://github.com/mattpocock/skills", Ref: "main",
		Title:       "Matt Pocock's Skills",
		Description: "TypeScript, AI SDK, and dev-workflow skills",
	},
	{
		ID: "superpowers", Repo: "obra/superpowers",
		CloneURL: "https://github.com/obra/superpowers", Ref: "main",
		Title:       "Superpowers",
		Description: "TDD, systematic debugging, review, and planning",
	},
	{
		ID: "andrej-karpathy-skills", Repo: "multica-ai/andrej-karpathy-skills",
		CloneURL: "https://github.com/multica-ai/andrej-karpathy-skills", Ref: "main",
		Title:       "Andrej Karpathy's Skills",
		Description: "LLM/ML research and explanation skills",
	},
	{
		ID: "anthropic-skills", Repo: "anthropics/skills",
		CloneURL: "https://github.com/anthropics/skills", Ref: "main",
		Title:       "Anthropic Skills",
		Description: "Document, data, and productivity skills from Anthropic",
	},
}

// Lookup returns the catalog entry for id.
func Lookup(id string) (Pack, bool) {
	for _, p := range catalog {
		if p.ID == id {
			return p, true
		}
	}
	return Pack{}, false
}

// ValidIDs returns every installable id, sorted for a deterministic usage
// message and JSON output.
func ValidIDs() []string {
	ids := make([]string, len(catalog))
	for i, p := range catalog {
		ids[i] = p.ID
	}
	sort.Strings(ids)
	return ids
}
