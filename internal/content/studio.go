package content

import (
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/dataconsole/console/webui"
)

// studioExtEmbedRoot is the embedded path of the Zerops Studio VS Code
// extension subtree (the local-mode-prototype cockpit). It is a directory tree
// — extension.js + package.json + logo.svg + lib/*.js + cards/*.js — that the
// install path materializes verbatim into the user's desktop VS Code extensions
// dir. The cards/ and handlers/ directories are extension points later slices
// add files to (directory discovery, no central registration).
const studioExtEmbedRoot = "templates/vscode-studio"

// StudioExtFile is one file in the embedded Studio extension subtree, with its
// path relative to the extension root (e.g. "extension.js", "cards/runtime.js").
type StudioExtFile struct {
	RelPath string
	Content []byte
}

// ReadStudioExtensionTree returns every shippable file in the embedded Studio
// extension subtree, each path relative to the extension root, sorted for
// deterministic write order. Test-only files are excluded so they never reach
// the installed extension — critically, the cards/ enumerator scans cards/*.js
// at runtime, so a stray cards/*.test.js would otherwise try to register as a
// card. The repo's JS tests live OUTSIDE this subtree (internal/content/
// studiojs/) and are never embedded or materialized.
func ReadStudioExtensionTree() ([]StudioExtFile, error) {
	var out []StudioExtFile
	walkErr := fs.WalkDir(templateFS, studioExtEmbedRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if isStudioTestArtifact(p) {
			return nil
		}
		data, readErr := templateFS.ReadFile(p)
		if readErr != nil {
			return fmt.Errorf("read studio ext file %s: %w", p, readErr)
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p, studioExtEmbedRoot), "/")
		out = append(out, StudioExtFile{RelPath: rel, Content: data})
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walk studio ext tree: %w", walkErr)
	}

	// Materialize the Data Console SPA under media/dataconsole/. The SINGLE source
	// is webui.FS() (the same bytes the standalone `console serve` ships) — no
	// duplicate copy in the template tree. The native console WebviewPanel loads
	// these as webview resources via asWebviewUri.
	spa, spaErr := readDataConsoleSPA()
	if spaErr != nil {
		return nil, spaErr
	}
	out = append(out, spa...)

	sort.Slice(out, func(i, j int) bool { return out[i].RelPath < out[j].RelPath })
	return out, nil
}

// readDataConsoleSPA returns every Data Console SPA asset, each path rooted at
// media/dataconsole/ within the extension tree. Single source of truth: webui.FS().
func readDataConsoleSPA() ([]StudioExtFile, error) {
	spaFS := webui.FS()
	var out []StudioExtFile
	err := fs.WalkDir(spaFS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, readErr := fs.ReadFile(spaFS, p)
		if readErr != nil {
			return fmt.Errorf("read data console SPA file %s: %w", p, readErr)
		}
		out = append(out, StudioExtFile{RelPath: "media/dataconsole/" + p, Content: data})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk data console SPA: %w", err)
	}
	return out, nil
}

// isStudioTestArtifact reports whether an embedded path is a test-only file that
// must not ship in the installed extension.
func isStudioTestArtifact(p string) bool {
	return strings.HasSuffix(p, ".test.js")
}

// StudioPackageJSON returns the embedded Studio extension package.json content
// — the single source of the extension's manifest version, which the install
// path's studioExtVersion const is parity-pinned against (P0 / R-DRIFT-LOCAL).
func StudioPackageJSON() (string, error) {
	return GetTemplate("vscode-studio/package.json")
}
