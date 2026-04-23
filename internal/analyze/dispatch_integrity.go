package analyze

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DispatchReport captures one sub-agent dispatch: the bytes the main
// agent actually sent through the Agent tool, and an engine-rebuilt
// reference for comparison. Classifications: clean / encoding-only /
// trailing-newline / semantic-paraphrase.
type DispatchReport struct {
	Role               string `json:"role"`
	DispatchedSHA      string `json:"dispatchedSHA"`
	EngineSHA          string `json:"engineSHA"`
	NormalizedEqual    bool   `json:"normalizedEqual"`
	ExactEqual         bool   `json:"exactEqual"`
	Classification     string `json:"classification"`
	DispatchedBytes    int    `json:"dispatchedBytes"`
	EngineBytes        int    `json:"engineBytes"`
	UnifiedDiffPreview string `json:"unifiedDiffPreview,omitempty"`
}

// WriteDispatchReports walks the agent summaries, extracts their
// captured dispatch prompts, and writes per-role compare files under
// <outputDir>/dispatches/<role>/. engineBuilds maps role → engine-built
// brief body; callers pass the live recipe-engine rebuilds as input.
func WriteDispatchReports(summaries []AgentSummary, engineBuilds map[string]string, outputDir string) ([]DispatchReport, error) {
	root := filepath.Join(outputDir, "dispatches")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir dispatches: %w", err)
	}

	var reports []DispatchReport
	for _, s := range summaries {
		if s.DispatchPrompt == "" {
			continue
		}
		engine := engineBuilds[s.Role]
		report := compareDispatch(s.Role, s.DispatchPrompt, engine)
		reports = append(reports, report)

		roleDir := filepath.Join(root, s.Role)
		if err := os.MkdirAll(roleDir, 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(roleDir, "dispatched.txt"),
			[]byte(s.DispatchPrompt), 0o600); err != nil {
			return nil, err
		}
		if engine != "" {
			if err := os.WriteFile(filepath.Join(roleDir, "engine-built.txt"),
				[]byte(engine), 0o600); err != nil {
				return nil, err
			}
		}
		if err := writeJSON(filepath.Join(roleDir, "report.json"), report); err != nil {
			return nil, err
		}
	}
	return reports, nil
}

func compareDispatch(role, dispatched, engine string) DispatchReport {
	d := DispatchReport{
		Role:            role,
		DispatchedSHA:   sha(dispatched),
		EngineSHA:       sha(engine),
		DispatchedBytes: len(dispatched),
		EngineBytes:     len(engine),
	}
	if engine == "" {
		d.Classification = "no-engine-build"
		return d
	}
	d.ExactEqual = dispatched == engine
	normD := normalize(dispatched)
	normE := normalize(engine)
	d.NormalizedEqual = normD == normE
	switch {
	case d.ExactEqual:
		d.Classification = "clean"
	case d.NormalizedEqual:
		d.Classification = "encoding-only"
	case strings.TrimSpace(dispatched) == strings.TrimSpace(engine):
		d.Classification = "trailing-newline"
	default:
		d.Classification = "semantic-paraphrase"
		d.UnifiedDiffPreview = shortDiff(dispatched, engine)
	}
	return d
}

// normalize collapses line endings and strips trailing whitespace so
// encoding-only drift (CRLF vs LF, tab→spaces, BOM) doesn't show as
// semantic.
func normalize(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.Join(lines, "\n")
}

func sha(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// shortDiff returns a minimal unified-like summary (first ~60 lines of
// diverging content) for analyst review. Not a full diff — enough to
// orient; full diff runs separately via `diff` or an editor.
func shortDiff(a, b string) string {
	al := strings.Split(a, "\n")
	bl := strings.Split(b, "\n")
	var out []string
	const limit = 30
	for i := range limit {
		if i >= len(al) && i >= len(bl) {
			break
		}
		var av, bv string
		if i < len(al) {
			av = al[i]
		}
		if i < len(bl) {
			bv = bl[i]
		}
		if av == bv {
			continue
		}
		if av != "" {
			out = append(out, "-"+av)
		}
		if bv != "" {
			out = append(out, "+"+bv)
		}
	}
	if len(out) == 0 {
		return "(tail-only divergence)"
	}
	return strings.Join(out, "\n")
}
