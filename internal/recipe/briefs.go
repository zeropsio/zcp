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
// The writer brief is deleted in run-8-readiness Workstream A1: fragment
// authorship is pinned to whoever holds the densest context at the moment
// of authorship, not to a post-hoc writer sub-agent.
const (
	ScaffoldBriefCap = 3 * 1024
	FeatureBriefCap  = 4 * 1024
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
