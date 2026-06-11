package recipe

import "strings"

// frameworkLabel maps a framework slug to its human-readable label.
// Used by HumanName to render the framework segment of a slug
// (e.g. "nestjs-showcase") with its canonical brand casing
// ("NestJS Showcase", not "Nestjs Showcase").
//
// Unknown frameworks fall through to title-case at the segment level —
// adding an entry here is opt-in polish for a specific brand. The lookup
// is intentionally small; fallback shape is acceptable for any framework
// not yet enumerated.
var frameworkLabel = map[string]string{
	"nestjs":     "NestJS",
	"nextjs":     "Next.js",
	"nuxtjs":     "Nuxt.js",
	"laravel":    "Laravel",
	"django":     "Django",
	"flask":      "Flask",
	"fastapi":    "FastAPI",
	"rails":      "Rails",
	"express":    "Express",
	"fastify":    "Fastify",
	"svelte":     "Svelte",
	"sveltekit":  "SvelteKit",
	"vue":        "Vue.js",
	"react":      "React",
	"phoenix":    "Phoenix",
	"go":         "Go",
	"golang":     "Go",
	"rust":       "Rust",
	"deno":       "Deno",
	"bun":        "Bun",
	"hono":       "Hono",
	"remix":      "Remix",
	"astro":      "Astro",
	"angular":    "Angular",
	"strapi":     "Strapi",
	"nodejs":     "Node.js",
	"node":       "Node.js",
	"php":        "PHP",
	"dotnet":     ".NET",
	"aspnet":     "ASP.NET",
	"aspnetcore": "ASP.NET Core",
	"graphql":    "GraphQL",
}

// HumanName returns the human-readable recipe name rendered in
// markdown H1 titles. Order of resolution:
//  1. Plan.Name when explicitly set by the research-phase agent
//  2. Slug + Framework derivation: split on `-`, replace the
//     framework-matching segment with its canonical label, title-case
//     the remaining segments, join with single space
//
// Examples:
//
//	slug="nestjs-showcase" framework="nestjs" → "NestJS Showcase"
//	slug="laravel-jetstream" framework="laravel" → "Laravel Jetstream"
//	slug="myapp-foo" framework="" → "Myapp Foo"
//	slug="" → ""
func HumanName(plan *Plan) string {
	if plan == nil {
		return ""
	}
	if plan.Name != "" {
		return plan.Name
	}
	return deriveHumanName(plan.Slug, plan.Framework)
}

func deriveHumanName(slug, framework string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return ""
	}
	parts := strings.Split(slug, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		// Framework match — apply canonical brand label.
		if framework != "" && strings.EqualFold(p, framework) {
			if label, ok := frameworkLabel[strings.ToLower(p)]; ok {
				parts[i] = label
				continue
			}
		}
		// Known framework even without explicit framework field.
		if label, ok := frameworkLabel[strings.ToLower(p)]; ok {
			parts[i] = label
			continue
		}
		// Generic title-case.
		parts[i] = titleCaseSegment(p)
	}
	return strings.Join(parts, " ")
}

// titleCaseSegment uppercases the first rune; leaves the rest as-is.
// Avoids golang.org/x/text/cases dependency for one segment.
func titleCaseSegment(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'a' && r[0] <= 'z' {
		r[0] -= 'a' - 'A'
	}
	return string(r)
}

// CodebaseRoleLabel returns the human-readable role suffix appended to
// the recipe name in apps-repo H1 titles for multi-codebase recipes.
// Single-codebase recipes return "" (no suffix; jetstream golden shape).
//
// Used by AssembleCodebaseREADME — the rendered title becomes
// "Zerops x <NAME> <ROLE_LABEL>" where ROLE_LABEL is " API"/" Frontend"/
// " Worker"/" Monolith" with a leading space, or "" when only one
// codebase exists in the plan.
func CodebaseRoleLabel(plan *Plan, hostname string) string {
	if plan == nil {
		return ""
	}
	// Single-codebase recipes don't need a role suffix.
	if len(plan.Codebases) <= 1 {
		return ""
	}
	for _, cb := range plan.Codebases {
		if cb.Hostname != hostname {
			continue
		}
		switch cb.Role {
		case RoleAPI:
			return " API"
		case RoleFrontend:
			return " Frontend"
		case RoleWorker:
			return " Worker"
		case RoleMonolith:
			return " Monolith"
		}
	}
	return ""
}
