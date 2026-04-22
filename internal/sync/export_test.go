package sync

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile is a test helper that creates parent dirs and writes a file.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// readArchivePaths returns all file paths inside a gzipped tar archive.
func readArchivePaths(t *testing.T, archivePath string) []string {
	t.Helper()
	f, err := os.Open(archivePath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	var paths []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		paths = append(paths, h.Name)
	}
	return paths
}

func TestExportRecipe(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(t *testing.T, root string) ExportOpts
		wantIn    []string // substrings that must appear in archive paths
		wantNotIn []string // substrings that must NOT appear
	}{
		{
			name: "single app dir",
			setup: func(t *testing.T, root string) ExportOpts {
				t.Helper()
				recipeDir := filepath.Join(root, "nestjs-showcase")
				writeFile(t, filepath.Join(recipeDir, "README.md"), "# root")
				writeFile(t, filepath.Join(recipeDir, "environments", "0 \u2014 AI Agent", "import.yaml"), "project:\n")

				appDir := filepath.Join(root, "appdev")
				writeFile(t, filepath.Join(appDir, "src", "main.ts"), "console.log('ok');")
				writeFile(t, filepath.Join(appDir, "package.json"), "{}")

				return ExportOpts{RecipeDir: recipeDir, AppDirs: []string{appDir}}
			},
			wantIn: []string{
				"nestjs-showcase-zcprecipator/README.md",
				"nestjs-showcase-zcprecipator/environments/0 \u2014 AI Agent/import.yaml",
				"nestjs-showcase-zcprecipator/appdev/src/main.ts",
				"nestjs-showcase-zcprecipator/appdev/package.json",
			},
		},
		{
			name: "multiple app dirs (dual-runtime)",
			setup: func(t *testing.T, root string) ExportOpts {
				t.Helper()
				recipeDir := filepath.Join(root, "nestjs-showcase")
				writeFile(t, filepath.Join(recipeDir, "README.md"), "# root")
				writeFile(t, filepath.Join(recipeDir, "environments", "0 \u2014 AI Agent", "import.yaml"), "project:\n")

				apiDir := filepath.Join(root, "apidev")
				writeFile(t, filepath.Join(apiDir, "src", "main.ts"), "bootstrap()")
				writeFile(t, filepath.Join(apiDir, "package.json"), "{\"name\":\"api\"}")

				appDir := filepath.Join(root, "appdev")
				writeFile(t, filepath.Join(appDir, "src", "App.svelte"), "<main />")
				writeFile(t, filepath.Join(appDir, "package.json"), "{\"name\":\"app\"}")

				return ExportOpts{RecipeDir: recipeDir, AppDirs: []string{apiDir, appDir}}
			},
			wantIn: []string{
				"nestjs-showcase-zcprecipator/README.md",
				"nestjs-showcase-zcprecipator/environments/0 \u2014 AI Agent/import.yaml",
				"nestjs-showcase-zcprecipator/apidev/src/main.ts",
				"nestjs-showcase-zcprecipator/apidev/package.json",
				"nestjs-showcase-zcprecipator/appdev/src/App.svelte",
				"nestjs-showcase-zcprecipator/appdev/package.json",
			},
		},
		{
			name: "no app dirs",
			setup: func(t *testing.T, root string) ExportOpts {
				t.Helper()
				recipeDir := filepath.Join(root, "bun-hello-world")
				writeFile(t, filepath.Join(recipeDir, "README.md"), "# root")
				writeFile(t, filepath.Join(recipeDir, "environments", "0 \u2014 AI Agent", "import.yaml"), "project:\n")
				return ExportOpts{RecipeDir: recipeDir}
			},
			wantIn: []string{
				"bun-hello-world-zcprecipator/README.md",
				"bun-hello-world-zcprecipator/environments/0 \u2014 AI Agent/import.yaml",
			},
			wantNotIn: []string{
				"appdev",
				"apidev",
			},
		},
		{
			name: "duplicate basenames rejected",
			setup: func(t *testing.T, root string) ExportOpts {
				t.Helper()
				recipeDir := filepath.Join(root, "dup")
				writeFile(t, filepath.Join(recipeDir, "README.md"), "# root")

				a := filepath.Join(root, "a", "appdev")
				b := filepath.Join(root, "b", "appdev")
				writeFile(t, filepath.Join(a, "package.json"), "{}")
				writeFile(t, filepath.Join(b, "package.json"), "{}")

				return ExportOpts{RecipeDir: recipeDir, AppDirs: []string{a, b}}
			},
		},
		{
			// v39 Commit 2 — F-23 close. Cx-4 MANIFEST-OVERLAY in v8.112.0
			// stages ZCP_CONTENT_MANIFEST.json into the recipe output dir but
			// the root-file whitelist at export time only passed TIMELINE.md +
			// README.md; the manifest landed in the output directory and was
			// dropped at tarball creation. This case asserts the manifest is
			// now in the archive root.
			name: "root manifest included",
			setup: func(t *testing.T, root string) ExportOpts {
				t.Helper()
				recipeDir := filepath.Join(root, "nestjs-showcase")
				writeFile(t, filepath.Join(recipeDir, "README.md"), "# root")
				writeFile(t, filepath.Join(recipeDir, "ZCP_CONTENT_MANIFEST.json"), `{"v":1,"codebases":[]}`)
				writeFile(t, filepath.Join(recipeDir, "environments", "0 \u2014 AI Agent", "import.yaml"), "project:\n")
				return ExportOpts{RecipeDir: recipeDir}
			},
			wantIn: []string{
				"nestjs-showcase-zcprecipator/README.md",
				"nestjs-showcase-zcprecipator/ZCP_CONTENT_MANIFEST.json",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Work in a temp CWD because ExportRecipe writes the archive to CWD.
			root := t.TempDir()
			t.Chdir(root)

			opts := tt.setup(t, root)
			result, err := ExportRecipe(opts)

			// "duplicate basenames rejected" expects an error.
			if tt.name == "duplicate basenames rejected" {
				if err == nil {
					t.Fatalf("expected error for duplicate basenames, got nil")
				}
				if !strings.Contains(err.Error(), "duplicate") {
					t.Fatalf("expected duplicate error, got: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("export failed: %v", err)
			}
			if result == nil || result.ArchivePath == "" {
				t.Fatalf("expected archive, got nil/empty")
			}

			paths := readArchivePaths(t, result.ArchivePath)
			joined := strings.Join(paths, "\n")

			for _, want := range tt.wantIn {
				if !strings.Contains(joined, want) {
					t.Errorf("archive missing %q\nall paths:\n%s", want, joined)
				}
			}
			for _, notWant := range tt.wantNotIn {
				if strings.Contains(joined, notWant) {
					t.Errorf("archive unexpectedly contains %q", notWant)
				}
			}
		})
	}
}
