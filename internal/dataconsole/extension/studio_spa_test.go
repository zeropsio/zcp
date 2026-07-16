package extension

import (
	"bytes"
	"io/fs"
	"testing"

	"github.com/zeropsio/zcp/internal/dataconsole/console/webui"
)

// TestReadStudioExtensionTree_MaterializesDataConsoleSPA pins the single-source
// materialization: the native console WebviewPanel loads the SPA as webview
// resources from media/dataconsole/, and those bytes MUST equal webui.FS() (the
// same SPA the standalone `console serve` ships). A drift here would mean the
// embedded console renders a different SPA than the standalone one.
func TestReadStudioExtensionTree_MaterializesDataConsoleSPA(t *testing.T) {
	t.Parallel()
	files, err := ReadStudioExtensionTree()
	if err != nil {
		t.Fatalf("ReadStudioExtensionTree: %v", err)
	}
	byPath := make(map[string][]byte, len(files))
	for _, f := range files {
		byPath[f.RelPath] = f.Content
	}

	spa := webui.FS()
	count := 0
	walkErr := fs.WalkDir(spa, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		want, readErr := fs.ReadFile(spa, p)
		if readErr != nil {
			return readErr
		}
		got, ok := byPath["media/dataconsole/"+p]
		if !ok {
			t.Fatalf("SPA asset %q is not materialized under media/dataconsole/", p)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("materialized media/dataconsole/%s drifted from webui.FS()", p)
		}
		count++
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk SPA: %v", walkErr)
	}
	if count == 0 {
		t.Fatal("no SPA assets materialized into the extension tree")
	}
	for _, want := range []string{"media/dataconsole/index.html", "media/dataconsole/app.js"} {
		if _, ok := byPath[want]; !ok {
			t.Fatalf("extension tree missing %q", want)
		}
	}
}
