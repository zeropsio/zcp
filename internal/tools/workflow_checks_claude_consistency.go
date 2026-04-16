package tools

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// checkClaudeReadmeConsistency detects when CLAUDE.md procedures use
// code-level patterns known to be hazardous in production without a
// cross-reference marker acknowledging the restriction. CLAUDE.md is
// the ambient context an agent reads when operating a codebase; if it
// teaches a pattern unsafe for prod without a dev-only marker, the
// agent will propagate the pattern into prod-affecting changes.
//
// v8.80 reform: pattern-driven detection + marker-driven exemption.
// The previous v8.78 shape relied on matching a narrow regex keyed on
// specific README phrasings (e.g. “ `X` must be off in production “),
// which returned zero events across all v21 codebases and only ever
// matched one v20 string verbatim. The new shape reads CLAUDE.md
// directly: each entry in the `knownForbiddenInProd` list has a
// stable regex for the pattern and a per-pattern detail explaining
// the production hazard. The list is curated — add entries only when
// a post-mortem surfaces a new drift class.
//
// The readmeContent parameter is retained for caller symmetry but is
// not consulted; detection no longer depends on README phrasing.
//
// hostname scopes the check name so multi-codebase recipes surface
// per-codebase failures.
func checkClaudeReadmeConsistency(_ /* readmeContent */, claudeContent, hostname string) []workflow.StepCheck {
	checkName := hostname + "_claude_readme_consistency"
	if claudeContent == "" {
		return nil
	}

	hasMarker := containsCrossReferenceMarker(claudeContent)

	var conflicts []string
	for _, p := range knownForbiddenInProd {
		if !p.Pattern.MatchString(claudeContent) {
			continue
		}
		if hasMarker {
			// Whole-document marker authorizes uses throughout.
			continue
		}
		conflicts = append(conflicts, fmt.Sprintf("%s (%s)", p.Name, p.Production))
	}
	if len(conflicts) == 0 {
		return []workflow.StepCheck{{Name: checkName, Status: statusPass}}
	}
	sort.Strings(conflicts)
	return []workflow.StepCheck{{
		Name:   checkName,
		Status: statusFail,
		Detail: fmt.Sprintf(
			"%s CLAUDE.md uses pattern(s) known to be hazardous in production: %s. CLAUDE.md is the ambient context an agent reads when operating this codebase; teaching a pattern unsafe for prod without an explicit dev-only marker propagates it into prod-affecting changes. Either (a) replace the procedure with the production-equivalent path (real migrations, versioned schema changes), or (b) add a cross-reference marker anywhere in CLAUDE.md — `(dev only — see README)`, `(warned against in production)`, etc. — so a reader sees the restriction.",
			hostname, strings.Join(conflicts, "; "),
		),
	}}
}

// knownForbiddenInProd lists code-level patterns that are unsafe in
// production AND tend to drift between README and CLAUDE.md. Curated —
// each entry has a stable regex, a human-readable name, and a per-
// pattern failure detail explaining the production consequence. Add
// entries ONLY when a post-mortem surfaces a new drift class.
//
// Pattern-based (not framework-keyed) so a framework that uses
// `synchronize` / `db:reset` / `drop table cascade` shares the hazard
// regardless of language.
var knownForbiddenInProd = []struct {
	Name       string
	Pattern    *regexp.Regexp
	Production string
}{
	{
		Name:       "TypeORM synchronize",
		Pattern:    regexp.MustCompile(`(?i)\bsynchronize\s*:\s*true\b|\bds\.synchronize\(|\bawait\s+\w+\.synchronize\(`),
		Production: "Auto-sync silently coerces column type mismatches and deadlocks under concurrent container start. Use migrations owned by `zsc execOnce` in production.",
	},
	{
		Name:       "Django syncdb / runserver in prod",
		Pattern:    regexp.MustCompile(`(?i)\bsyncdb\b|manage\.py\s+runserver`),
		Production: "`syncdb` was removed in Django 1.9; `runserver` is the dev server, never a prod WSGI/ASGI path.",
	},
	{
		Name:       "Rails db:reset / db:drop",
		Pattern:    regexp.MustCompile(`(?i)\b(?:rails|rake|bundle\s+exec\s+rails)\s+db:(?:reset|drop)\b`),
		Production: "`db:reset`/`db:drop` wipe data. Prod schema changes go via versioned migrations, never resets.",
	},
	{
		Name:       "drop all tables / rm -rf dbfile",
		Pattern:    regexp.MustCompile(`(?i)\bdrop\s+table\s+(?:all|\w+\s+cascade)|\btruncate\s+(?:all|\w+\s+cascade)|\brm\s+-rf\s+[^\s]*\.db\b`),
		Production: "Mass destructive ops have no place in a deploy path — prod recovery uses backups and versioned schema.",
	},
}

// crossReferenceMarkers signal that the CLAUDE.md author has
// explicitly acknowledged the restriction. Whole-document matching:
// a single marker anywhere in the file authorizes uses throughout.
// List intentionally inclusive of the natural ways the agent phrases
// this caveat.
var crossReferenceMarkers = []string{
	"dev only",
	"dev-only",
	"in dev",
	"development only",
	"see readme",
	"readme gotcha",
	"warned against",
	"warning in production",
	"forbidden in production",
	"do not use in production",
	"never in production",
	"shortcut for dev",
	"dev shortcut",
}

func containsCrossReferenceMarker(body string) bool {
	low := strings.ToLower(body)
	for _, m := range crossReferenceMarkers {
		if strings.Contains(low, m) {
			return true
		}
	}
	return false
}
