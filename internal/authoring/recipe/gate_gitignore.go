package recipe

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// gate_gitignore.go — Run-28 fix #4 closure.
//
// Run-27 workerdev shipped without a `.gitignore`, so the recipe
// deliverable carried 242 node_modules directories + dist/ committed
// alongside source. The scaffold preship_contract atom teaches the
// porter to author one, but no engine-side gate refused the missing
// file at scaffold complete-phase. This gate fires when any codebase
// running an artifact-producing runtime base lacks a non-empty
// `.gitignore` at its SourceRoot.
//
// Static-only runtimes (`base: static`, `base: nginx`) skip the gate
// — they don't produce build artifacts in the working tree (the build
// stage emits `public/` or similar deployFiles directly into the
// container image).
//
// Recovery hint travels in the Violation Message text per the run-28
// codex revision (Violation has no Recovery field today).

// runtimeFamilyGitignoreContent returns a one-line recommended
// .gitignore body for a recognized artifact-producing runtime family,
// or empty string when the family does not need one.
//
// Families chosen to cover the stack runtimes Zerops ships today;
// extend when a new family's standard ignore-set becomes load-bearing.
var runtimeFamilyGitignoreContent = map[string]string{
	"nodejs": "node_modules/\ndist/\n*.tsbuildinfo\n.next/\n",
	"deno":   "node_modules/\n.deno/\n",
	"bun":    "node_modules/\ndist/\n",
	"python": "__pycache__/\n*.pyc\n.venv/\nvenv/\n",
	"go":     "/bin/\n/dist/\n",
	"ruby":   "vendor/bundle/\nlog/\ntmp/\n",
	"php":    "vendor/\n",
	"rust":   "target/\n",
	"elixir": "_build/\ndeps/\n",
}

// staticOnlyRuntimeBases — runtimes that don't produce committed
// build artifacts in the codebase working tree. The build phase
// composes the served files into a separate location.
var staticOnlyRuntimeBases = map[string]bool{
	"static": true,
	"nginx":  true,
}

// gateGitignorePresent — Run-28 fix #4. Refuses scaffold complete-phase
// when an artifact-producing runtime codebase has no non-empty
// `<SourceRoot>/.gitignore`. Idempotent on re-runs (no phase coupling).
func gateGitignorePresent(ctx GateContext) []Violation {
	if ctx.Plan == nil {
		return nil
	}
	var out []Violation
	for _, cb := range ctx.Plan.Codebases {
		if cb.Hostname == "" || cb.SourceRoot == "" {
			continue
		}
		yamlPath := filepath.Join(cb.SourceRoot, "zerops.yaml")
		raw, err := os.ReadFile(yamlPath)
		if err != nil {
			// Pre-scaffold or unreadable — `gateFactsRecorded` already
			// covers missing scaffold artifacts.
			continue
		}
		families := extractRuntimeFamilies(string(raw))
		family, needsGitignore := pickArtifactProducingFamily(families)
		if !needsGitignore {
			continue
		}
		gitignorePath := filepath.Join(cb.SourceRoot, ".gitignore")
		info, err := os.Stat(gitignorePath)
		switch {
		case err == nil && info.Size() == 0:
			out = append(out, gitignoreMissingViolation(gitignorePath, family))
		case err == nil:
			// Non-empty file present — gate passes.
			continue
		case os.IsNotExist(err):
			out = append(out, gitignoreMissingViolation(gitignorePath, family))
		default:
			out = append(out, Violation{
				Code:    "gitignore-stat-failed",
				Path:    gitignorePath,
				Message: err.Error(),
			})
		}
	}
	return out
}

func gitignoreMissingViolation(path, family string) Violation {
	suggestion := runtimeFamilyGitignoreContent[family]
	return Violation{
		Code:     "MISSING_GITIGNORE",
		Path:     path,
		Severity: SeverityBlocking,
		Message: fmt.Sprintf(
			"missing or empty .gitignore — codebase runs `%s` and produces build artifacts in the working tree; commit a `.gitignore` with at minimum:\n%s",
			family, suggestion,
		),
	}
}

// extractRuntimeFamilies walks the parsed yaml's `zerops:` array and
// collects every `run.base` and `build.base` runtime identifier. Both
// scalar and sequence forms are flattened. Families are returned as
// the leading token (`nodejs@22` → `nodejs`).
func extractRuntimeFamilies(body string) []string {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		return nil
	}
	bases := map[string]bool{}
	walkRuntimeBases(&doc, bases)
	out := make([]string, 0, len(bases))
	for b := range bases {
		out = append(out, b)
	}
	return out
}

func walkRuntimeBases(n *yaml.Node, bases map[string]bool) {
	if n == nil {
		return
	}
	if n.Kind == yaml.DocumentNode {
		for _, c := range n.Content {
			walkRuntimeBases(c, bases)
		}
		return
	}
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i]
			val := n.Content[i+1]
			if key.Value == "base" && (val.Kind == yaml.ScalarNode || val.Kind == yaml.SequenceNode) {
				addBaseValues(val, bases)
				continue
			}
			walkRuntimeBases(val, bases)
		}
		return
	}
	if n.Kind == yaml.SequenceNode {
		for _, c := range n.Content {
			walkRuntimeBases(c, bases)
		}
	}
}

func addBaseValues(n *yaml.Node, bases map[string]bool) {
	if n == nil {
		return
	}
	switch n.Kind {
	case yaml.ScalarNode:
		fam := strings.SplitN(n.Value, "@", 2)[0]
		fam = strings.TrimSpace(fam)
		if fam != "" {
			bases[fam] = true
		}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			addBaseValues(c, bases)
		}
	case yaml.DocumentNode, yaml.MappingNode, yaml.AliasNode:
		// `base:` values are scalar or sequence by spec; document /
		// mapping / alias shapes don't appear in this position. Falling
		// through silently keeps the gate conservative — unparseable
		// shapes get treated as "no base", scaffold runs un-gated.
	}
}

// pickArtifactProducingFamily reports the first artifact-producing
// runtime family found in the codebase's bases AND whether the
// codebase needs a .gitignore. When the codebase mixes static + a
// runtime family (e.g. multi-base php@8.4 + static), the runtime
// family wins because it produces vendor/ etc. in the tree.
func pickArtifactProducingFamily(families []string) (string, bool) {
	if len(families) == 0 {
		return "", false
	}
	for _, f := range families {
		if _, ok := runtimeFamilyGitignoreContent[f]; ok {
			return f, true
		}
	}
	// All bases were static-only — no gate fires.
	for _, f := range families {
		if !staticOnlyRuntimeBases[f] {
			// Unknown family (recipe authored with a runtime ZCP doesn't
			// recognize). Default to skip — the gate is conservative;
			// false negatives are preferable to false positives on a
			// runtime whose ignore-set we haven't catalogued.
			return "", false
		}
	}
	return "", false
}
