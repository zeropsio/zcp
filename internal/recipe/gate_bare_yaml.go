package recipe

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// gate_bare_yaml.go — Run-20 C3 closure.
//
// Run-19 scaffold sub-agents wrote `# causal comment` lines into
// `<SourceRoot>/zerops.yaml` despite the spec contract that scaffold
// produces bare yaml. The codebase-content phase later authors block-
// level comment fragments which the engine stamps back via
// WriteCodebaseYAMLWithComments (strip-then-inject); the scaffold-leaked
// comments collide with that pipeline (visible in run-19 IG #1 inline
// yaml as duplicate blocks until E1 lands).
//
// `principles/bare-yaml-prohibition.md` (now wired into the scaffold
// brief) teaches the rule. This gate enforces it at scaffold complete-
// phase: scan every committed `<SourceRoot>/zerops.yaml` for
// `^\s+# ` lines and refuse with the violating line numbers.
//
// Carve-outs match the principle:
//   1. The `#zeropsPreprocessor=on` shebang at line 0 (no leading
//      whitespace) — directive, not causal comment.
//   2. Trailing comments on data lines (`port: 3000  # note`) — the
//      `^\s+# ` regex requires the `#` be the first non-whitespace
//      character, so trailing comments naturally pass.

// gateScaffoldBareYAML refuses scaffold complete-phase when any
// committed codebase zerops.yaml has single-hash causal comments
// above directives. Run-20 C3.
func gateScaffoldBareYAML(ctx GateContext) []Violation {
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
			// Pre-scaffold or unreadable — gateFactsRecorded already
			// surfaces missing scaffold artifacts.
			continue
		}
		violations := scanBareYAMLViolations(string(raw))
		if len(violations) == 0 {
			continue
		}
		var lineNums []string
		for _, v := range violations {
			lineNums = append(lineNums, fmt.Sprintf("L%d: %s", v.line, v.text))
		}
		out = append(out, Violation{
			Code: "scaffold-yaml-leaked-comment",
			Path: yamlPath,
			Message: fmt.Sprintf(
				"scaffold-authored zerops.yaml contains %d single-`#` causal comment line(s) — bare yaml is the scaffold contract per `principles/bare-yaml-prohibition.md`. Causal comments are authored later as `codebase/%s/zerops-yaml-comments/<block>` fragments and stamped by the engine's stitch step. Strip these lines:\n  %s",
				len(violations), cb.Hostname, strings.Join(lineNums, "\n  "),
			),
		})
	}
	return out
}

type bareYAMLViolation struct {
	line int
	text string
}

// scanBareYAMLViolations returns the 1-indexed line numbers + verbatim
// text of every `^\s+# ` causal-comment line in body. Carve-outs:
//
//   - Line 1 with `#zeropsPreprocessor=` prefix (the shebang) — passes,
//     it has no leading whitespace by definition.
//   - Lines whose `#` is preceded by data on the same line (trailing
//     comment on a directive line) — the `^\s+#` anchor naturally
//     excludes them.
//   - Empty body — no violations.
//
// The check is a pure-text scan; no yaml AST involvement.
func scanBareYAMLViolations(body string) []bareYAMLViolation {
	var out []bareYAMLViolation
	for i, raw := range strings.Split(body, "\n") {
		lineNum := i + 1
		if raw == "" {
			continue
		}
		// Shebang carve-out: line 1, no leading whitespace, starts
		// with `#zeropsPreprocessor=`.
		if lineNum == 1 && strings.HasPrefix(raw, "#zeropsPreprocessor=") {
			continue
		}
		// `^\s+# ` — `#` is the first non-whitespace character AND at
		// least one whitespace precedes it.
		trimmedLeft := strings.TrimLeft(raw, " \t")
		if trimmedLeft == raw {
			// No leading whitespace — either bare directive or the
			// shebang (already handled). Not a violation.
			continue
		}
		if !strings.HasPrefix(trimmedLeft, "#") {
			continue
		}
		// Skip trailing comments riding on a data line — ApplyEnvComment
		// won't reach here because trimmedLeft starts with `#`. This
		// branch is the indented bare-comment shape.
		out = append(out, bareYAMLViolation{
			line: lineNum,
			text: strings.TrimSpace(raw),
		})
	}
	return out
}
