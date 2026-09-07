// Package recipe composes the on-disk shape a Zerops recipe is published in.
//
// A recipe is not one document. It is a directory of environment tiers —
// `0 — AI Agent`, `3 — Stage`, `4 — Small Production` — each holding the
// whole-project `import.yaml` that creates that tier and a README describing
// it, under a top-level README that introduces the app. All 55 recipes in
// `zeropsio/recipes` are shaped this way: 306 import.yaml files, one per tier
// (surveyed 2026-09-06).
//
// # Why zcp is the one that can write it
//
// A tier is a **decision**, not an observation. Which services exist at all,
// `mode: HA` against a single node, `minContainers: 2`, autoscaling bounds —
// none of it is derivable from a running project, because you can read what
// production *is* and never what a production *should be* for an app that has
// none yet. Something has to author it, and zcp is the only party that sees
// the application: the client reads service shapes off the platform and has no
// idea what the app needs.
//
// zcp already composes the two hard tiers. `bundle.BuildExport` produces the
// source-shaped import, and `bundle.BuildLaunch` applies the production
// transforms — HA promotion, DEDICATED cpuMode, minContainers, pipeline-first
// `startWithoutCode`. This package is the missing half: it turns composed
// tiers into the files a recipe is made of, so what an agent builds can be
// stored, re-created, or published without a second format being invented for
// it.
//
// # Why the layout and not a private shape
//
// The published layout is already the thing Zerops reads. Emitting it means a
// Mate's app can become a recipe with no conversion, and the `#ZEROPS_EXTRACT#`
// markers keep the human-facing text where the recipe pages already look for
// it. A private JSON blob would have to be translated to get there, and
// translations rot.
//
// No I/O here: this composes bytes and returns them. Where they are written —
// the working tree, a store, a git host — is the caller's, and is the one part
// of this that is still an open question.
package recipe

import (
	"fmt"
	"sort"
	"strings"
)

// introStart and introEnd bracket the text the recipe pages lift out of a
// README. Both must be present and in order or the extraction takes nothing.
const (
	introStart = "<!-- #ZEROPS_EXTRACT_START:intro# -->"
	introEnd   = "<!-- #ZEROPS_EXTRACT_END:intro# -->"
)

// Tier is one environment a recipe can create.
type Tier struct {
	// Index orders the directories: `0 — AI Agent` before `1 — Remote (CDE)`.
	// Published recipes number from zero and the number is part of the name.
	Index int
	// Title is the tier's human name — "Small Production". The directory is
	// "<Index> — <Title>", em dash included, exactly as published.
	Title string
	// Slug addresses the tier in a deploy link: `?environment=small-production`.
	Slug string
	// ImportYAML is the whole-project document that creates this tier,
	// composed upstream (bundle.BuildExport / bundle.BuildLaunch).
	ImportYAML string
	// Summary is one sentence, lifted into the recipe page for this tier.
	Summary string
}

// File is one composed file, path relative to the recipe root.
type File struct {
	Path string
	Body string
}

// Layout describes the recipe as a whole.
type Layout struct {
	// Name is the recipe's identity in a deploy link and its page URL.
	Name string
	// Title heads the top-level README — "Laravel Minimal".
	Title string
	// Intro is the paragraph the recipe page lifts from that README.
	Intro string
	Tiers []Tier
}

// dirName is the directory a tier lives in, which is also how it is ordered
// on disk and in the listing: the number is part of the name.
func (t Tier) dirName() string {
	return fmt.Sprintf("%d — %s", t.Index, t.Title)
}

// Build composes every file of the recipe, sorted by path so the same input
// always produces the same bytes — a recipe that rewrites itself on each pass
// makes a diff that says nothing.
func Build(layout Layout) ([]File, error) {
	if strings.TrimSpace(layout.Name) == "" {
		return nil, fmt.Errorf("recipe: Name required")
	}
	if len(layout.Tiers) == 0 {
		return nil, fmt.Errorf("recipe %q: at least one tier required", layout.Name)
	}

	seen := make(map[string]struct{}, len(layout.Tiers))
	for _, tier := range layout.Tiers {
		if strings.TrimSpace(tier.Title) == "" || strings.TrimSpace(tier.Slug) == "" {
			return nil, fmt.Errorf("recipe %q: every tier needs a Title and a Slug", layout.Name)
		}
		if strings.TrimSpace(tier.ImportYAML) == "" {
			return nil, fmt.Errorf("recipe %q: tier %q has no import yaml", layout.Name, tier.Title)
		}
		if _, dup := seen[tier.dirName()]; dup {
			return nil, fmt.Errorf("recipe %q: two tiers named %q", layout.Name, tier.dirName())
		}
		seen[tier.dirName()] = struct{}{}
	}

	tiers := append([]Tier(nil), layout.Tiers...)
	sort.SliceStable(tiers, func(i, j int) bool { return tiers[i].Index < tiers[j].Index })

	files := []File{{Path: "README.md", Body: rootReadme(layout, tiers)}}
	for _, tier := range tiers {
		files = append(files,
			File{Path: tier.dirName() + "/import.yaml", Body: ensureTrailingNewline(tier.ImportYAML)},
			File{Path: tier.dirName() + "/README.md", Body: tierReadme(layout, tier)},
		)
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func deployURL(recipeName, slug string) string {
	return fmt.Sprintf("https://app.zerops.io/recipes/%s?environment=%s", recipeName, slug)
}

func rootReadme(layout Layout, tiers []Tier) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", firstNonEmpty(layout.Title, layout.Name))
	writeIntro(&b, firstNonEmpty(layout.Intro, "An application running on [Zerops](https://zerops.io)."))
	b.WriteString("\n")
	for _, tier := range tiers {
		fmt.Fprintf(&b, "- **%s** — [[deploy]](%s)\n", tier.Title, deployURL(layout.Name, tier.Slug))
	}
	return b.String()
}

func tierReadme(layout Layout, tier Tier) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s — %s Environment\n\n", firstNonEmpty(layout.Title, layout.Name), tier.Title)
	fmt.Fprintf(&b, "This is the %s environment for [%s (info + deploy)](%s).\n\n",
		strings.ToLower(tier.Title), firstNonEmpty(layout.Title, layout.Name),
		deployURL(layout.Name, tier.Slug))
	writeIntro(&b, firstNonEmpty(tier.Summary, fmt.Sprintf("**%s** environment.", tier.Title)))
	return b.String()
}

// writeIntro brackets a paragraph in the markers the recipe pages extract on.
func writeIntro(b *strings.Builder, text string) {
	fmt.Fprintf(b, "%s\n%s\n%s\n", introStart, strings.TrimSpace(text), introEnd)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func ensureTrailingNewline(body string) string {
	if strings.HasSuffix(body, "\n") {
		return body
	}
	return body + "\n"
}
