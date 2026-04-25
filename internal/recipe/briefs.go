package recipe

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strings"
)

// Brief cap constants. Enforced at dispatch time; the composer produces
// a Brief whose Bytes field the caller compares against the cap.
//
// Run-9-readiness raised the scaffold cap from 5 KB → 8 KB to fit the
// tranche-2 principle atoms (dev-loop, mount-vs-container, yaml-
// comment-style) alongside the existing scaffold content. Feature cap
// held at 5 KB — the feature brief adds only the v3 fact-recording
// section in tranche 1 and still fits.
//
// Run-8-readiness Workstream F raised both caps from 3 KB → 5 KB to
// accommodate the content-authoring placement rubric + the execOnce
// key-shape concept atom.
//
// The writer brief was deleted in A1: fragment authorship is pinned to
// whoever holds the densest context at the moment of authorship, not to
// a post-hoc writer sub-agent.
//
// Run-11 V-5 raised ScaffoldBriefCap from 12 KB → 14 KB to fit the
// Self-inflicted litmus subsection. Run-11 O-1 raised it again to
// 16 KB to fit the Citation map subsection (citations are author-time
// signals, not render output).
const (
	ScaffoldBriefCap = 16 * 1024
	FeatureBriefCap  = 10 * 1024
)

// BriefKind identifies one of two sub-agent roles. The writer role was
// removed — fragments are authored in-phase by scaffold/feature sub-agents
// and by the main agent at finalize (run-8-readiness §2.0).
type BriefKind string

const (
	BriefScaffold BriefKind = "scaffold"
	BriefFeature  BriefKind = "feature"
)

// Brief is a composed sub-agent brief. Body is the final text handed to
// the Agent tool; Parts lists the section identifiers in order so the
// harness can trace what came from where.
type Brief struct {
	Kind  BriefKind `json:"kind"`
	Body  string    `json:"body"`
	Bytes int       `json:"bytes"`
	Parts []string  `json:"parts,omitempty"`
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

	atoms := []string{
		"briefs/scaffold/platform_principles.md",
		"briefs/scaffold/preship_contract.md",
		"briefs/scaffold/fact_recording.md",
		"briefs/scaffold/content_authoring.md",
		"principles/dev-loop.md",
		"principles/mount-vs-container.md",
		"principles/yaml-comment-style.md",
	}
	if anyCodebaseHasInitCommands(plan) {
		atoms = append(atoms, "principles/init-commands-model.md")
	}
	for _, atom := range atoms {
		body, err := readAtom(atom)
		if err != nil {
			return Brief{}, err
		}
		if atom == "briefs/scaffold/platform_principles.md" && !contract.ServesHTTP {
			body = stripHTTPSection(body)
		}
		b.WriteString(body)
		if !strings.HasSuffix(body, "\n") {
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
		parts = append(parts, atom)
	}

	b.WriteString("## Citation map\n\nTopics covered by a `zerops_knowledge` guide: ")
	b.WriteString(strings.Join(citationGuides(), ", "))
	b.WriteString(". When your KB fragment touches any of them, call `zerops_knowledge` on the matching guide id first and cite it by name.\n\n")
	parts = append(parts, "citation_map")

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

	atoms := []string{
		"briefs/feature/feature_kinds.md",
		"briefs/feature/content_extension.md",
		"principles/mount-vs-container.md",
		"principles/yaml-comment-style.md",
	}
	if planDeclaresSeed(plan) {
		atoms = append(atoms, "principles/init-commands-model.md")
	}
	for _, atom := range atoms {
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

// stripHTTPSection removes the `## HTTP` section from the platform-
// principles atom when the codebase's role has ServesHTTP=false. The
// section lives between `<!-- HTTP_SECTION_START -->` and
// `<!-- HTTP_SECTION_END -->` markers in the atom. Run-10-readiness §Q1.
func stripHTTPSection(body string) string {
	const (
		start = "<!-- HTTP_SECTION_START -->"
		end   = "<!-- HTTP_SECTION_END -->"
	)
	before, rest, ok := strings.Cut(body, start)
	if !ok {
		return body
	}
	_, after, ok := strings.Cut(rest, end)
	if !ok {
		return body
	}
	out := strings.TrimRight(before, "\n") + "\n" + strings.TrimLeft(after, "\n")
	return out
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

// anyCodebaseHasInitCommands reports whether any codebase in the plan
// authors initCommands. Briefs use this to decide whether to inject the
// execOnce key-shape concept atom — run-8-readiness §2.F.
func anyCodebaseHasInitCommands(plan *Plan) bool {
	if plan == nil {
		return false
	}
	for _, cb := range plan.Codebases {
		if cb.HasInitCommands {
			return true
		}
	}
	return false
}

// citationGuides returns the unique guide ids from CitationMap in
// deterministic order so brief composition is reproducible.
func citationGuides() []string {
	seen := map[string]bool{}
	var out []string
	for _, g := range CitationMap {
		if seen[g] {
			continue
		}
		seen[g] = true
		out = append(out, g)
	}
	// Sort for deterministic output — map iteration is non-deterministic.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// planDeclaresSeed reports whether the plan's feature kinds include
// anything that authors new initCommands (seed, scout-import). Drives
// the execOnce concept-atom injection into the feature brief.
func planDeclaresSeed(plan *Plan) bool {
	if plan == nil {
		return false
	}
	for _, k := range plan.FeatureKinds {
		switch k {
		case "seed", "scout-import", "bootstrap":
			return true
		}
	}
	return false
}
