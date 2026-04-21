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
