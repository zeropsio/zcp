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

// ReviewGranularity is the unit at which ZCP's catalog reviews a pack's
// upstream content — spec-skill-packs.md §1. This is deliberately a
// separate axis from SELECTION granularity (whether an installed pack
// offers a user-facing subset): Superpowers is reviewed at skill level
// (every installable skill is individually enumerated below) yet is
// installed as one atomic unit, never per-skill or per-category. A pack's
// Review field must never be read as "the user can pick individual
// skills" — that is a distinct, not-yet-modeled decision a later slice
// owns.
type ReviewGranularity int

const (
	// ReviewRepositoryLevel — the whole reviewed repository is the review
	// unit, and its complete discovered skill set installs together. This
	// is the zero value: a Pack with no declared Skills is
	// repository-level by construction (Skills has nothing to enumerate).
	ReviewRepositoryLevel ReviewGranularity = iota
	// ReviewSkillLevel — the catalog enumerates every installable skill by
	// name and source path; nothing outside that list is ever installed,
	// whatever the upstream repository contains.
	ReviewSkillLevel
)

// CatalogSkill is one individually reviewed, installable skill within a
// ReviewSkillLevel Pack. Category is presentation and bulk-selection
// metadata only (spec-skill-packs.md §3) — it is never used to build a
// filesystem path; the on-disk destination is always SourcePath's leaf
// component (the flattened skill directory name), via discoverSkills.
type CatalogSkill struct {
	Name        string // stable destination name; matches the upstream SKILL.md frontmatter name
	SourcePath  string // slash-relative path from the repo root, e.g. "skills/engineering/tdd"
	Category    string // display category (presentation/bulk-selection aid, never a filesystem path)
	Description string // short user-facing description
}

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
	Review      ReviewGranularity
	// Selection is how much of the reviewed set the USER may choose from.
	// It is a separate axis from Review: Superpowers is reviewed skill by
	// skill yet offers no subset (spec-skill-packs.md §5).
	Selection SelectionGranularity
	// Skills is the reviewed, installable set for a ReviewSkillLevel pack.
	// It is empty for a ReviewRepositoryLevel pack — that pack's complete
	// discovered set installs together instead (spec-skill-packs.md §1).
	Skills []CatalogSkill
}

// SelectionGranularity is how much of a pack's reviewed set the user may
// choose from — spec-skill-packs.md §1's closing rule that selection
// granularity is an axis of its own, distinct from ReviewGranularity.
type SelectionGranularity int

const (
	// SelectionAtomic — the pack installs as a whole or not at all. This is
	// the zero value, so a pack must opt IN to offering a subset.
	SelectionAtomic SelectionGranularity = iota
	// SelectionSubset — the user chooses an explicit subset of the pack's
	// reviewed skills (spec-skill-packs.md §4.2).
	SelectionSubset
)

// IsAtomic reports whether p's selection is all-or-nothing.
func (p Pack) IsAtomic() bool { return p.Selection == SelectionAtomic }

// catalog is the enum-closed set of installable skill packs (verified live
// 2026-07-23; Matt/Superpowers skill lists verified live 2026-07-27 against
// the pinned commits in spec-skill-packs.md §4.1/§5). Adding an entry here
// is the only way to make a new pack installable — pack-add refuses any id
// outside this set. pack-remove does NOT consult this set (a retired id —
// removed from the catalog but still manifest-recorded — must remain
// removable), see Remove.
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
		Review:      ReviewSkillLevel,
		Selection:   SelectionSubset,
		Skills:      mattPocockSkills,
	},
	{
		ID: "superpowers", Repo: "obra/superpowers",
		CloneURL: "https://github.com/obra/superpowers", Ref: "main",
		Title:       "Superpowers",
		Description: "TDD, systematic debugging, review, and planning",
		Review:      ReviewSkillLevel,
		Skills:      superpowersSkills,
	},
	{
		ID: "andrej-karpathy-skills", Repo: "multica-ai/andrej-karpathy-skills",
		CloneURL: "https://github.com/multica-ai/andrej-karpathy-skills", Ref: "main",
		Title:       "Andrej Karpathy's Skills",
		Description: "LLM/ML research and explanation skills",
		Review:      ReviewRepositoryLevel,
	},
}

// mattPocockSkills is the exact 22-skill supported surface from
// spec-skill-packs.md §4.1 — 17 Engineering + 5 Productivity. Matt's
// upstream repository also carries personal, miscellaneous, in-progress,
// and deprecated skills under skills/{personal,misc,in-progress,deprecated}/
// that are deliberately excluded and must never appear here.
var mattPocockSkills = []CatalogSkill{
	{Name: "ask-matt", SourcePath: "skills/engineering/ask-matt", Category: "Engineering",
		Description: "Router that recommends which Matt Pocock skill or flow fits the situation"},
	{Name: "diagnosing-bugs", SourcePath: "skills/engineering/diagnosing-bugs", Category: "Engineering",
		Description: "Structured diagnosis loop for hard bugs and performance regressions"},
	{Name: "grill-with-docs", SourcePath: "skills/engineering/grill-with-docs", Category: "Engineering",
		Description: "Interview-driven design sharpening that also produces ADRs and a glossary"},
	{Name: "triage", SourcePath: "skills/engineering/triage", Category: "Engineering",
		Description: "State-machine triage for issues and external PRs: categorize, verify, brief"},
	{Name: "improve-codebase-architecture", SourcePath: "skills/engineering/improve-codebase-architecture", Category: "Engineering",
		Description: "Scans a codebase for deepening opportunities and reports them visually"},
	{Name: "setup-matt-pocock-skills", SourcePath: "skills/engineering/setup-matt-pocock-skills", Category: "Engineering",
		Description: "One-time repo setup for issue tracker, triage labels, and domain docs"},
	{Name: "tdd", SourcePath: "skills/engineering/tdd", Category: "Engineering",
		Description: "Test-driven development workflow (red-green-refactor)"},
	{Name: "to-spec", SourcePath: "skills/engineering/to-spec", Category: "Engineering",
		Description: "Turns the current conversation into a spec and publishes it to the issue tracker"},
	{Name: "to-tickets", SourcePath: "skills/engineering/to-tickets", Category: "Engineering",
		Description: "Breaks a plan or spec into tracer-bullet tickets with blocking edges"},
	{Name: "wayfinder", SourcePath: "skills/engineering/wayfinder", Category: "Engineering",
		Description: "Plans large multi-session work as a shared map of decision tickets"},
	{Name: "implement", SourcePath: "skills/engineering/implement", Category: "Engineering",
		Description: "Implements a piece of work from a spec or a set of tickets"},
	{Name: "prototype", SourcePath: "skills/engineering/prototype", Category: "Engineering",
		Description: "Builds a throwaway prototype to answer a design question"},
	{Name: "research", SourcePath: "skills/engineering/research", Category: "Engineering",
		Description: "Investigates a question against primary sources and writes up the findings"},
	{Name: "domain-modeling", SourcePath: "skills/engineering/domain-modeling", Category: "Engineering",
		Description: "Builds and sharpens a project's domain model and ubiquitous language"},
	{Name: "codebase-design", SourcePath: "skills/engineering/codebase-design", Category: "Engineering",
		Description: "Shared vocabulary for designing deep, testable module interfaces"},
	{Name: "code-review", SourcePath: "skills/engineering/code-review", Category: "Engineering",
		Description: "Reviews changes since a fixed point against coding standards and spec"},
	{Name: "resolving-merge-conflicts", SourcePath: "skills/engineering/resolving-merge-conflicts", Category: "Engineering",
		Description: "Resolves an in-progress git merge or rebase conflict"},
	{Name: "grill-me", SourcePath: "skills/productivity/grill-me", Category: "Productivity",
		Description: "Relentless interview to sharpen a plan or design"},
	{Name: "grilling", SourcePath: "skills/productivity/grilling", Category: "Productivity",
		Description: "Stress-tests the user's thinking about a plan, decision, or idea"},
	{Name: "handoff", SourcePath: "skills/productivity/handoff", Category: "Productivity",
		Description: "Compacts the current conversation into a handoff document for another agent"},
	{Name: "teach", SourcePath: "skills/productivity/teach", Category: "Productivity",
		Description: "Teaches the user a new skill or concept within the workspace"},
	{Name: "writing-great-skills", SourcePath: "skills/productivity/writing-great-skills", Category: "Productivity",
		Description: "Reference for writing and editing predictable, well-structured skills"},
}

// superpowersSkills is the exact 14-skill supported set from
// spec-skill-packs.md §5. Categories are descriptive only (§5's closing
// paragraph) — Superpowers has one install/remove control, never
// per-category or per-skill selection.
var superpowersSkills = []CatalogSkill{
	{Name: "test-driven-development", SourcePath: "skills/test-driven-development", Category: "Testing",
		Description: "Write the test first, watch it fail, then implement"},
	{Name: "systematic-debugging", SourcePath: "skills/systematic-debugging", Category: "Debugging",
		Description: "Root-causes any bug or test failure before proposing a fix"},
	{Name: "verification-before-completion", SourcePath: "skills/verification-before-completion", Category: "Debugging",
		Description: "Runs and confirms verification commands before claiming work is done"},
	{Name: "brainstorming", SourcePath: "skills/brainstorming", Category: "Collaboration",
		Description: "Explores intent, requirements, and design before any creative implementation"},
	{Name: "writing-plans", SourcePath: "skills/writing-plans", Category: "Collaboration",
		Description: "Turns a spec or requirement into a written implementation plan"},
	{Name: "executing-plans", SourcePath: "skills/executing-plans", Category: "Collaboration",
		Description: "Executes a written implementation plan with review checkpoints"},
	{Name: "dispatching-parallel-agents", SourcePath: "skills/dispatching-parallel-agents", Category: "Collaboration",
		Description: "Dispatches independent tasks to parallel agents"},
	{Name: "requesting-code-review", SourcePath: "skills/requesting-code-review", Category: "Collaboration",
		Description: "Requests a code review before merging or completing work"},
	{Name: "receiving-code-review", SourcePath: "skills/receiving-code-review", Category: "Collaboration",
		Description: "Verifies and applies code-review feedback with technical rigor"},
	{Name: "using-git-worktrees", SourcePath: "skills/using-git-worktrees", Category: "Collaboration",
		Description: "Sets up an isolated git worktree for feature work"},
	{Name: "finishing-a-development-branch", SourcePath: "skills/finishing-a-development-branch", Category: "Collaboration",
		Description: "Decides how to integrate finished, tested work"},
	{Name: "subagent-driven-development", SourcePath: "skills/subagent-driven-development", Category: "Collaboration",
		Description: "Dispatches one subagent per task, with review between tasks"},
	{Name: "writing-skills", SourcePath: "skills/writing-skills", Category: "Meta",
		Description: "Guidance for creating, editing, and verifying skills before deployment"},
	{Name: "using-superpowers", SourcePath: "skills/using-superpowers", Category: "Meta",
		Description: "Establishes how to discover and invoke Superpowers skills at conversation start"},
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
