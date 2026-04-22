package content

import (
	"io/fs"
	"strings"
	"testing"
)

// TestWriterAtoms_NoStandaloneFilePrescription is the Cx-STANDALONE-FILES-REMOVED
// RED→GREEN test. v36 F-13: writer atoms prescribed standalone
// `INTEGRATION-GUIDE.md` + `GOTCHAS.md` per codebase that duplicated
// the fragment content inside the README. No publish pipeline
// consumed these files — the fragments inside README.md are the only
// path to the recipe page. Writer authored 6 dead files per showcase
// run (3 codebases × 2 files). Fix: delete standalone prescription
// from every writer atom.
//
// Test walks the writer atom tree; any reference to the two forbidden
// filenames as an authored output is a regression. Prose references
// (e.g. "existing files named INTEGRATION-GUIDE.md") are flagged too
// — the cleaner policy is no mention at all.
func TestWriterAtoms_NoStandaloneFilePrescription(t *testing.T) {
	t.Parallel()

	const writerDir = "workflows/recipe/briefs/writer"
	forbidden := []string{"INTEGRATION-GUIDE.md", "GOTCHAS.md"}

	var offenders []string
	err := fs.WalkDir(RecipeAtomsFS, writerDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		data, rerr := fs.ReadFile(RecipeAtomsFS, path)
		if rerr != nil {
			return rerr
		}
		body := string(data)
		for _, f := range forbidden {
			if strings.Contains(body, f) {
				offenders = append(offenders, path+" mentions "+f)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("%d writer atom(s) still prescribe standalone files:\n  %s",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}

// TestWriterAtoms_NoRootOrEnvReadmes is the Cx-WRITER-SCOPE-REDUCTION
// RED→GREEN test. v37 F-9 mutated: writer authored env READMEs under
// `/var/www/environments/{slug}/` producing an `environments-generated/`
// parallel tree; v37 F-10 residual: writer authored a root README
// duplicating finalize's output. The root README + 6 per-env READMEs
// are finalize's responsibility (see recipe_templates.go); the writer
// duplicating them is the entire hallucination surface for env slug
// names. Cx-1 removes the prescription from every writer atom so there
// is nothing for the main-agent paraphrase to reach back into memory
// about.
func TestWriterAtoms_NoRootOrEnvReadmes(t *testing.T) {
	t.Parallel()

	const writerDir = "workflows/recipe/briefs/writer"
	forbidden := []string{
		"Root README",
		"Surface 1 — Root",
		"Surface 2 — Per-env",
		"{{.ProjectRoot}}/README.md",
		"/var/www/README.md",
		"/var/www/environments/",
		"{{.ProjectRoot}}/environments/",
		"{{$.ProjectRoot}}/environments/",
	}

	var offenders []string
	err := fs.WalkDir(RecipeAtomsFS, writerDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		data, rerr := fs.ReadFile(RecipeAtomsFS, path)
		if rerr != nil {
			return rerr
		}
		body := string(data)
		for _, f := range forbidden {
			if strings.Contains(body, f) {
				offenders = append(offenders, path+" contains forbidden reference: "+f)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("%d writer atom(s) still prescribe root README or per-env README:\n  %s",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}

// TestWriterAtoms_NoEnvFolderSlugs guards against the main-agent
// paraphrase reaching into memory for env-slug vocabulary when
// composing the writer Task prompt. The canonical env folder names
// come from finalize via CanonicalEnvFolders() — the writer atom
// corpus must never name them in slug form ("ai-agent", "remote-dev"),
// lest the paraphrase substitute slug names the writer then creates
// on the mount as ghost directories. v36 / v37 both surfaced this
// failure mode. Cf. verdict.md §4 F-9.
func TestWriterAtoms_NoEnvFolderSlugs(t *testing.T) {
	t.Parallel()

	const writerDir = "workflows/recipe/briefs/writer"
	forbiddenSlugs := []string{
		"ai-agent",
		"remote-dev",
		"local-dev",
		"small-prod",
		"prod-ha",
		"stage-only",
		"dev-and-stage-hypercde",
	}

	var offenders []string
	err := fs.WalkDir(RecipeAtomsFS, writerDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		data, rerr := fs.ReadFile(RecipeAtomsFS, path)
		if rerr != nil {
			return rerr
		}
		body := string(data)
		for _, s := range forbiddenSlugs {
			if strings.Contains(body, s) {
				offenders = append(offenders, path+" mentions slug "+s)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("%d writer atom(s) mention env-folder slug names (main-agent paraphrase risk):\n  %s",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}
