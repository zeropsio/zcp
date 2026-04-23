package analyze

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/recipe"
)

// SurfaceReport captures one surface's validation state across the
// recipe output tree. Violations aggregate ValidateFn output across
// every file owned by the surface.
type SurfaceReport struct {
	Surface    string             `json:"surface"`
	Author     string             `json:"author"`
	FileCount  int                `json:"fileCount"`
	Files      []string           `json:"files,omitempty"`
	Violations []recipe.Violation `json:"violations,omitempty"`
}

// WriteSurfaceReports walks every surface, finds owned files under the
// deliverable root, runs the registered ValidateFn if one exists, and
// writes surfaces.json + surfaces.md under <outputDir>/.
func WriteSurfaceReports(deliverableRoot, outputDir string, inputs recipe.SurfaceInputs) ([]SurfaceReport, error) {
	reports := make([]SurfaceReport, 0, len(recipe.Surfaces()))
	for _, s := range recipe.Surfaces() {
		c, _ := recipe.ContractFor(s)
		files := findOwnedFiles(deliverableRoot, c.Owns)
		r := SurfaceReport{
			Surface:   string(s),
			Author:    string(c.Author),
			Files:     files,
			FileCount: len(files),
		}
		if validator := recipe.ValidatorFor(s); validator != nil {
			for _, path := range files {
				content, err := os.ReadFile(filepath.Join(deliverableRoot, path))
				if err != nil {
					continue
				}
				violations, err := validator(context.Background(), path, content, inputs)
				if err != nil {
					r.Violations = append(r.Violations, recipe.Violation{
						Code: "validator-error", Path: path, Message: err.Error(),
					})
					continue
				}
				r.Violations = append(r.Violations, violations...)
			}
		}
		reports = append(reports, r)
	}

	if err := writeJSON(filepath.Join(outputDir, "surfaces.json"), reports); err != nil {
		return nil, err
	}
	return reports, writeSurfacesMarkdown(filepath.Join(outputDir, "surfaces.md"), reports)
}

// findOwnedFiles walks deliverableRoot and returns paths matching any
// of the surface's Owns patterns. Patterns anchor-match against the
// path relative to root; `*` matches one path segment. README fragments
// (README.md#foo) are treated as the README file itself.
func findOwnedFiles(root string, patterns []string) []string {
	if len(patterns) == 0 {
		return nil
	}
	var out []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // walk-tolerant
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil //nolint:nilerr // walk-tolerant
		}
		for _, pat := range patterns {
			if matchOwned(pat, rel) {
				out = append(out, rel)
				return nil
			}
		}
		return nil
	})
	return out
}

// matchOwned is a simple glob matcher — mirrors internal/recipe.matchOwnedPath.
func matchOwned(pattern, path string) bool {
	pattern = strings.TrimSuffix(strings.SplitN(pattern, "#", 2)[0], "/")
	if pattern == "" {
		return false
	}
	if !strings.Contains(pattern, "*") {
		return strings.HasSuffix(path, pattern)
	}
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

func writeSurfacesMarkdown(path string, reports []SurfaceReport) error {
	var b strings.Builder
	b.WriteString("# Surface validation\n\n")
	for _, r := range reports {
		fmt.Fprintf(&b, "## %s (%s) — %d file(s), %d violation(s)\n\n",
			r.Surface, r.Author, r.FileCount, len(r.Violations))
		for _, v := range r.Violations {
			fmt.Fprintf(&b, "- [%s] %s: %s\n", v.Code, v.Path, v.Message)
		}
		if len(r.Violations) == 0 && r.FileCount > 0 {
			b.WriteString("- (clean)\n")
		}
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}
