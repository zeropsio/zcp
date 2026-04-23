package recipe

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Gate is a mechanical check — file existence, marker form, JSON shape,
// citation timestamp, writer-owned-path authorship. Gates do not judge
// prose quality (that's the editorial-review step); they only attest
// structural invariants.
type Gate struct {
	Name string
	Run  func(ctx GateContext) []Violation
}

// GateContext carries the data gates need: the plan, the recipe output
// tree root on disk, an optional facts-log, and optional parent recipe.
type GateContext struct {
	Plan       *Plan
	OutputRoot string
	FactsLog   *FactsLog
	Parent     *ParentRecipe
}

// RunGates runs every gate and collects violations. Violations do not
// abort — the caller decides whether to block a phase transition.
func RunGates(gates []Gate, ctx GateContext) []Violation {
	out := make([]Violation, 0, len(gates))
	for _, g := range gates {
		out = append(out, g.Run(ctx)...)
	}
	return out
}

// DefaultGates returns the mechanical gate set that runs at every phase
// close. Phase-specific gates (research classification, finalize file
// presence) are added by gatesForPhase on top of this base.
func DefaultGates() []Gate {
	return []Gate{
		{Name: "citations-timestamped", Run: gateCitationsTimestamped},
		{Name: "fact-required-fields", Run: gateFactsValid},
		{Name: "payload-schema-valid", Run: gatePayloadSchema},
	}
}

// FinalizeGates returns the additional gate set that runs only at
// finalize close — after the writer sub-agent has emitted all six
// import.yaml files to the output tree.
func FinalizeGates() []Gate {
	return []Gate{
		{Name: "env-imports-present", Run: gateEnvImportsPresent},
	}
}

// gateEnvImportsPresent — every tier must have an import.yaml file in the
// output tree.
func gateEnvImportsPresent(ctx GateContext) []Violation {
	var out []Violation
	for i := range 6 {
		tier, _ := TierAt(i)
		path := filepath.Join(ctx.OutputRoot, tier.Folder, "import.yaml")
		if _, err := os.Stat(path); err != nil {
			out = append(out, Violation{
				Code:    "env-import-missing",
				Path:    path,
				Message: fmt.Sprintf("tier %d: import.yaml not found", i),
			})
		}
	}
	return out
}

// gateCitationsTimestamped — every fact with a citation MUST have a
// RecordedAt in RFC3339 form, so downstream analysis can order facts
// chronologically.
func gateCitationsTimestamped(ctx GateContext) []Violation {
	if ctx.FactsLog == nil {
		return nil
	}
	records, err := ctx.FactsLog.Read()
	if err != nil {
		return []Violation{{Code: "facts-read-failure", Message: err.Error()}}
	}
	var out []Violation
	for i, r := range records {
		if r.Citation == "" {
			continue
		}
		if r.RecordedAt == "" {
			out = append(out, Violation{
				Code:    "citation-missing-timestamp",
				Path:    fmt.Sprintf("facts[%d]", i),
				Message: "citation present but recorded_at empty",
			})
			continue
		}
		if _, err := time.Parse(time.RFC3339, r.RecordedAt); err != nil {
			out = append(out, Violation{
				Code:    "citation-bad-timestamp",
				Path:    fmt.Sprintf("facts[%d]", i),
				Message: err.Error(),
			})
		}
	}
	return out
}

// gateFactsValid — every fact record round-trips its own Validate(). Any
// fact missing a required field is a writer-brief routing risk.
func gateFactsValid(ctx GateContext) []Violation {
	if ctx.FactsLog == nil {
		return nil
	}
	records, err := ctx.FactsLog.Read()
	if err != nil {
		return []Violation{{Code: "facts-read-failure", Message: err.Error()}}
	}
	var out []Violation
	for i, r := range records {
		if err := r.Validate(); err != nil {
			out = append(out, Violation{
				Code:    "fact-invalid",
				Path:    fmt.Sprintf("facts[%d]", i),
				Message: err.Error(),
			})
		}
	}
	return out
}

// gatePayloadSchema — the writer's completion payload must parse as JSON
// and carry every top-level key the completion-payload atom names.
func gatePayloadSchema(ctx GateContext) []Violation {
	path := filepath.Join(ctx.OutputRoot, ".writer-payload.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // not yet authored; not a gate failure at this phase
		}
		return []Violation{{Code: "payload-read-failure", Path: path, Message: err.Error()}}
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return []Violation{{Code: "payload-not-json", Path: path, Message: err.Error()}}
	}
	required := []string{
		"root_readme", "env_readmes", "env_import_comments",
		"codebase_readmes", "codebase_claude",
		"codebase_zerops_yaml_comments", "citations", "manifest",
	}
	var out []Violation
	for _, key := range required {
		if _, ok := obj[key]; !ok {
			out = append(out, Violation{
				Code:    "payload-missing-key",
				Path:    path,
				Message: fmt.Sprintf("completion payload missing %q", key),
			})
		}
	}
	return out
}

// MainAgentRewroteWriterPath reports whether the main agent edited a path
// that belongs to a writer-owned surface. Inputs: the file's recorded
// author, the path, and the registry. Returns a Violation if true.
// Rule: writer-owned paths are locked at the engine boundary; any edit
// by the main agent after writer completion is a violation.
func MainAgentRewroteWriterPath(path, author string) *Violation {
	if author != "main" {
		return nil
	}
	for _, s := range Surfaces() {
		c, _ := ContractFor(s)
		if c.Author != AuthorWriter {
			continue
		}
		for _, pat := range c.Owns {
			if matchOwnedPath(pat, path) {
				return &Violation{
					Code:    "main-agent-rewrote-writer-path",
					Path:    path,
					Message: fmt.Sprintf("surface %q is writer-owned", s),
				}
			}
		}
	}
	return nil
}

// matchOwnedPath does a simple glob-style check. Supports `*/foo.md` (one
// path segment wildcard) and literal suffixes. Good enough for the
// surface registry's patterns; extend if new surface globs need finer
// matching.
func matchOwnedPath(pattern, path string) bool {
	pattern = strings.TrimSuffix(strings.SplitN(pattern, "#", 2)[0], "/")
	if pattern == "" {
		return false
	}
	if !strings.Contains(pattern, "*") {
		return strings.HasSuffix(path, pattern)
	}
	// Reduce one-star glob: "*/foo.md" matches any "x/foo.md" segment.
	parts := strings.Split(pattern, "/")
	segs := strings.Split(path, "/")
	if len(parts) > len(segs) {
		return false
	}
	segs = segs[len(segs)-len(parts):]
	for i, p := range parts {
		if p == "*" {
			continue
		}
		if p != segs[i] {
			return false
		}
	}
	return true
}
