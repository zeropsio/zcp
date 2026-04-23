package recipe

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strings"
)

// Brief cap constants (plan §8). Enforced at dispatch time; the composer
// produces a Brief whose Bytes field the caller compares against the cap.
const (
	ScaffoldBriefCap = 3 * 1024
	FeatureBriefCap  = 4 * 1024
	WriterBriefCap   = 10 * 1024
)

// BriefKind identifies one of three sub-agent roles.
type BriefKind string

const (
	BriefScaffold BriefKind = "scaffold"
	BriefFeature  BriefKind = "feature"
	BriefWriter   BriefKind = "writer"
)

// Brief is a composed sub-agent brief. Body is the final text handed to
// the Agent tool; Parts lists the section identifiers in order so the
// harness can trace what came from where.
type Brief struct {
	Kind  BriefKind
	Body  string
	Bytes int
	Parts []string
}

//go:embed all:content
var recipeV3Content embed.FS

// readAtom reads an atom at a path relative to internal/recipe/content/.
// Read-only access to the embedded atom tree.
func readAtom(rel string) (string, error) {
	b, err := fs.ReadFile(recipeV3Content, "content/"+rel)
	if err != nil {
		return "", fmt.Errorf("read atom %q: %w", rel, err)
	}
	return string(b), nil
}

// BuildScaffoldBrief composes a scaffold sub-agent brief for one codebase.
// Content: role contract + platform principles atom + preship contract
// atom + fact-recording atom + parent codebase excerpt (if chain hit).
func BuildScaffoldBrief(plan *Plan, cb Codebase, parent *ParentRecipe) (Brief, error) {
	if plan == nil {
		return Brief{}, errors.New("nil plan")
	}
	contract, ok := cb.Role.Contract()
	if !ok {
		return Brief{}, fmt.Errorf("unknown role %q", cb.Role)
	}

	var b strings.Builder
	var parts []string

	fmt.Fprintf(&b, "# Scaffold brief — %s (%s)\n\n", cb.Hostname, cb.Role)
	fmt.Fprintf(&b, "Recipe: %s · Framework: %s · Tier: %s\n\n",
		plan.Slug, plan.Framework, plan.Tier)
	parts = append(parts, "header")

	b.WriteString("## Role contract\n")
	fmt.Fprintf(&b, "- ServesHTTP: %t\n", contract.ServesHTTP)
	fmt.Fprintf(&b, "- RequiresSubdomain: %t\n", contract.RequiresSubdomain)
	fmt.Fprintf(&b, "- ProcessModel: %s\n", contract.ProcessModel)
	fmt.Fprintf(&b, "- zeropsSetup dev: %s, prod: %s\n\n",
		contract.ZeropsSetupDev, contract.ZeropsSetupProd)
	parts = append(parts, "role_contract")

	for _, atom := range []string{
		"briefs/scaffold/platform_principles.md",
		"briefs/scaffold/preship_contract.md",
		"briefs/scaffold/fact_recording.md",
	} {
		body, err := readAtom(atom)
		if err != nil {
			return Brief{}, err
		}
		b.WriteString(body)
		if !strings.HasSuffix(body, "\n") {
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
		parts = append(parts, atom)
	}

	if parent != nil {
		if pc, ok := parent.Codebases[cb.Hostname]; ok {
			b.WriteString("## Parent recipe excerpt\n\n")
			b.WriteString("Parent: " + parent.Slug + " (" + parent.Tier + ")\n\n")
			excerpt := excerptREADME(pc.README, 1500)
			if excerpt != "" {
				b.WriteString("```\n")
				b.WriteString(excerpt)
				if !strings.HasSuffix(excerpt, "\n") {
					b.WriteByte('\n')
				}
				b.WriteString("```\n\n")
			}
			parts = append(parts, "parent_excerpt")
		}
	}

	body := b.String()
	return Brief{Kind: BriefScaffold, Body: body, Bytes: len(body), Parts: parts}, nil
}

// BuildFeatureBrief composes the feature sub-agent brief. Feature kind
// catalog + scaffold symbol table from the plan's codebases + services.
// Symbol table is flat data — no framework instructions.
func BuildFeatureBrief(plan *Plan) (Brief, error) {
	if plan == nil {
		return Brief{}, errors.New("nil plan")
	}
	var b strings.Builder
	var parts []string

	fmt.Fprintf(&b, "# Feature brief — %s\n\n", plan.Slug)
	parts = append(parts, "header")

	body, err := readAtom("briefs/feature/feature_kinds.md")
	if err != nil {
		return Brief{}, err
	}
	b.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	parts = append(parts, "briefs/feature/feature_kinds.md")

	b.WriteString("## Symbol table (from scaffold phase)\n\n")
	b.WriteString("### Codebases\n\n")
	for _, cb := range plan.Codebases {
		fmt.Fprintf(&b, "- %s · role=%s · runtime=%s · worker=%t",
			cb.Hostname, cb.Role, cb.BaseRuntime, cb.IsWorker)
		if cb.SharesCodebaseWith != "" {
			fmt.Fprintf(&b, " · shares=%s", cb.SharesCodebaseWith)
		}
		b.WriteByte('\n')
	}
	b.WriteString("\n### Services\n\n")
	for _, svc := range plan.Services {
		fmt.Fprintf(&b, "- %s · type=%s · kind=%s\n",
			svc.Hostname, svc.Type, svc.Kind)
	}
	parts = append(parts, "symbol_table")

	out := b.String()
	return Brief{Kind: BriefFeature, Body: out, Bytes: len(out), Parts: parts}, nil
}

// BuildWriterBrief composes the writer sub-agent brief. Walks the surface
// registry, inlines each surface's examples atom, filters facts by
// surface hint, and appends completion payload + citation topics atoms.
func BuildWriterBrief(plan *Plan, facts []FactRecord, parent *ParentRecipe) (Brief, error) {
	if plan == nil {
		return Brief{}, errors.New("nil plan")
	}
	var b strings.Builder
	var parts []string

	fmt.Fprintf(&b, "# Writer brief — %s\n\n", plan.Slug)
	fmt.Fprintf(&b, "Recipe: %s · Framework: %s · Tier: %s · Codebases: %d\n\n",
		plan.Slug, plan.Framework, plan.Tier, len(plan.Codebases))
	parts = append(parts, "header")

	b.WriteString("## Surface registry\n\n")
	for _, s := range Surfaces() {
		c, _ := ContractFor(s)
		fmt.Fprintf(&b, "### %s — author=%s\n", c.Name, c.Author)
		fmt.Fprintf(&b, "- FormatSpec: %s\n", c.FormatSpec)
		fmt.Fprintf(&b, "- Owns: %s\n", strings.Join(c.Owns, ", "))
		fmt.Fprintf(&b, "- FactHint: %s\n", c.FactHint)
		if len(c.AdjacentSurfaces) > 0 {
			adjacent := make([]string, 0, len(c.AdjacentSurfaces))
			for _, a := range c.AdjacentSurfaces {
				adjacent = append(adjacent, string(a))
			}
			fmt.Fprintf(&b, "- Adjacent (cross-reference, don't duplicate): %s\n",
				strings.Join(adjacent, ", "))
		}
		surfaceFacts := FilterByHint(facts, c.FactHint)
		if len(surfaceFacts) > 0 {
			fmt.Fprintf(&b, "- Facts routed here: %d\n", len(surfaceFacts))
			for _, f := range surfaceFacts {
				fmt.Fprintf(&b, "  - %s — citation: %s\n", f.Topic, f.Citation)
			}
		}
		if body, err := readAtom("briefs/writer/examples/" + string(s) + ".md"); err == nil {
			b.WriteString("\n")
			b.WriteString(body)
			if !strings.HasSuffix(body, "\n") {
				b.WriteByte('\n')
			}
		}
		b.WriteByte('\n')
		parts = append(parts, string(s))
	}

	for _, atom := range []string{
		"briefs/writer/citation_topics.md",
		"briefs/writer/completion_payload.md",
	} {
		body, err := readAtom(atom)
		if err != nil {
			return Brief{}, err
		}
		b.WriteString(body)
		if !strings.HasSuffix(body, "\n") {
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
		parts = append(parts, atom)
	}

	if parent != nil {
		fmt.Fprintf(&b, "## Parent recipe for cross-reference\n\nParent: %s (%s). %d codebases, %d env imports.\n\nCross-reference parent's IG/gotcha items rather than re-authoring. If your recipe's fact duplicates a parent-level fact, route it as a cross-reference.\n\n",
			parent.Slug, parent.Tier, len(parent.Codebases), len(parent.EnvImports))
		parts = append(parts, "parent_reference")
	}

	out := b.String()
	return Brief{Kind: BriefWriter, Body: out, Bytes: len(out), Parts: parts}, nil
}

// excerptREADME trims a parent README to at most n bytes, cutting at
// line boundaries so no partial sentence lands in the excerpt.
func excerptREADME(body string, n int) string {
	if len(body) <= n {
		return body
	}
	cut := body[:n]
	if i := strings.LastIndexByte(cut, '\n'); i > 0 {
		cut = cut[:i]
	}
	return cut
}
