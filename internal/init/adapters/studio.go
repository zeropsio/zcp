package adapters

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/zeropsio/zcp/internal/dataconsole/extension"
)

const (
	// studioExtID identifies the Zerops Studio desktop VS Code extension. It is
	// publisher.name and doubles as the install-dir prefix and the
	// extensions.json identifier.
	studioExtID = "zerops.zcp-studio"
	// studioExtVersion is parity-pinned to templates/vscode-studio/package.json's
	// "version" (P0 / R-DRIFT-LOCAL — TestStudioExtVersion_ParityWithPackageJSON).
	// A second version surface (Go const + manifest) that drifts means a new
	// extension.js may not reload; the test forbids the drift.
	studioExtVersion = "0.1.3"
)

// studioExtDirName is the version-stamped install directory name, e.g.
// "zerops.zcp-studio-0.1.0". VS Code resolves an extension's relativeLocation
// under the extensions dir, so this is also the manifest's relativeLocation.
func studioExtDirName() string { return studioExtID + "-" + studioExtVersion }

// InstallStudioExtension materializes the Zerops Studio extension into the
// user's STOCK DESKTOP VS Code extensions dir (~/.vscode/extensions/) and
// registers it in the profile manifest so it loads on next launch.
//
// Idempotent: prior zerops.zcp-studio-* version dirs are removed before the
// fresh tree is written, so re-runs and version bumps never leave orphans
// (mirrors generateGuidedSkill's RemoveAll-reset).
//
// This is desktop-only and deliberately separate from installBootstrapExtension
// (which targets code-server paths inside a Zerops container). The folder write
// is the success gate; registering in extensions.json is best-effort and never
// fatal — a registration failure leaves the user's other extensions untouched
// (see registerStudioInExtensionsIndex) and is surfaced as a reload hint.
func InstallStudioExtension(home string) error {
	extRoot := filepath.Join(home, ".vscode", "extensions")
	if err := EnsureDir(extRoot); err != nil {
		return fmt.Errorf("mkdir extensions dir: %w", err)
	}

	// Reset prior versions of THIS extension only (never touch others).
	prior, _ := filepath.Glob(filepath.Join(extRoot, studioExtID+"-*"))
	for _, p := range prior {
		if err := os.RemoveAll(p); err != nil {
			return fmt.Errorf("remove prior studio dir %s: %w", p, err)
		}
	}

	extDir := filepath.Join(extRoot, studioExtDirName())
	files, err := extension.ReadStudioExtensionTree()
	if err != nil {
		return fmt.Errorf("read studio extension tree: %w", err)
	}
	for _, f := range files {
		dest := filepath.Join(extDir, filepath.FromSlash(f.RelPath))
		if err := EnsureDir(filepath.Dir(dest)); err != nil {
			return fmt.Errorf("mkdir studio subdir: %w", err)
		}
		if err := os.WriteFile(dest, f.Content, 0o644); err != nil { //nolint:gosec // G306: extension assets must be readable
			return fmt.Errorf("write studio file %s: %w", f.RelPath, err)
		}
	}

	// Register in the profile manifest. Best-effort: current VS Code loads only
	// extensions listed in extensions.json, but a corrupt manifest fails the
	// WHOLE profile — so a pre-existing-invalid manifest is left alone and the
	// failure is a reload hint, not a fatal init error.
	if err := registerStudioInExtensionsIndex(extRoot, extDir); err != nil {
		fmt.Fprintf(os.Stderr,
			"    note: installed to %s, but registering in VS Code failed: %v\n"+
				"    The extension is on disk; reload VS Code, or repair ~/.vscode/extensions/extensions.json.\n",
			extDir, err)
	}
	return nil
}

// InstallStudioExtensionContainer materializes the Zerops Studio (Managed Data)
// extension into code-server's extensions dir INSIDE a Zerops container
// (~/.local/share/code-server/extensions/zerops.zcp-studio-<ver>/) and registers
// it in that dir's extensions.json using the CODE-SERVER entry shape
// (location.{$mid,fsPath,external,path,scheme} + metadata) — NOT the desktop
// entry shape InstallStudioExtension writes.
//
// It is the container sibling of InstallStudioExtension: the materialized tree
// is identical (extension.ReadStudioExtensionTree()); only the target dir and
// the manifest entry shape differ (code-server vs stock desktop VS Code). The
// two are deliberately separate because the editor host and its extension index
// contract differ between the two environments.
//
// Idempotent: prior zerops.zcp-studio-* version dirs are removed before the
// fresh tree is written (mirrors InstallStudioExtension), and the index entry is
// upserted in place (single entry keyed on studioExtID, installedTimestamp
// preserved across re-runs). The folder write is the success gate; registering
// in extensions.json is best-effort and never fatal — a pre-existing malformed
// index is left untouched (upsertExtensionsIndex refuses to overwrite it) and
// the failure is surfaced as a reload hint, so other extensions are never
// disturbed.
func InstallStudioExtensionContainer(home string) error {
	extRoot := filepath.Join(home, ".local", "share", "code-server", "extensions")
	if err := EnsureDir(extRoot); err != nil {
		return fmt.Errorf("mkdir code-server extensions dir: %w", err)
	}

	// Reset prior versions of THIS extension only (never touch others).
	prior, _ := filepath.Glob(filepath.Join(extRoot, studioExtID+"-*"))
	for _, p := range prior {
		if err := os.RemoveAll(p); err != nil {
			return fmt.Errorf("remove prior studio dir %s: %w", p, err)
		}
	}

	extDir := filepath.Join(extRoot, studioExtDirName())
	files, err := extension.ReadStudioExtensionTree()
	if err != nil {
		return fmt.Errorf("read studio extension tree: %w", err)
	}
	for _, f := range files {
		dest := filepath.Join(extDir, filepath.FromSlash(f.RelPath))
		if err := EnsureDir(filepath.Dir(dest)); err != nil {
			return fmt.Errorf("mkdir studio subdir: %w", err)
		}
		if err := os.WriteFile(dest, f.Content, 0o644); err != nil { //nolint:gosec // G306: extension assets must be readable
			return fmt.Errorf("write studio file %s: %w", f.RelPath, err)
		}
	}

	// Register in code-server's index (code-server entry shape). Best-effort:
	// a malformed pre-existing index is left as-is (refused, not overwritten)
	// so the whole profile's blast radius is never ours — the folder is on disk
	// and a reload hint is surfaced instead of failing init.
	indexPath := filepath.Join(extRoot, "extensions.json")
	if err := upsertExtensionsIndex(indexPath, studioExtID, studioExtVersion, studioExtDirName(), extDir); err != nil {
		fmt.Fprintf(os.Stderr,
			"    note: installed Managed Data extension to %s, but registering in code-server failed: %v\n"+
				"    The extension is on disk; reload the editor, or repair ~/.local/share/code-server/extensions/extensions.json.\n",
			extDir, err)
	}
	return nil
}

// registerStudioInExtensionsIndex adds (or replaces) our entry in
// ~/.vscode/extensions/extensions.json, the profile manifest current VS Code
// scans to decide which extensions to load.
//
// Guardrails (the manifest is high-risk — a malformed file fails every
// extension in the profile, not just ours):
//   - Existing entries are preserved BYTE-FOR-BYTE (json.RawMessage) — we never
//     re-serialize another extension's entry, so we cannot drift it.
//   - If the existing file is present but not a valid JSON array, we refuse to
//     overwrite (the profile is already broken; masking it would own the blast
//     radius) and return an error so the caller surfaces a repair hint.
//   - Only our own identifier.id is replaced (dedupe across versions).
//   - Atomic temp-file + rename; a rolling backup (.zcp-bak) of the prior file.
func registerStudioInExtensionsIndex(extRoot, extDir string) error {
	indexPath := filepath.Join(extRoot, "extensions.json")

	var entries []json.RawMessage
	raw, readErr := os.ReadFile(indexPath)
	switch {
	case readErr == nil:
		if err := json.Unmarshal(raw, &entries); err != nil {
			return fmt.Errorf("existing extensions.json is not a valid JSON array, refusing to overwrite: %w", err)
		}
	case os.IsNotExist(readErr):
		entries = nil
	default:
		return fmt.Errorf("read extensions.json: %w", readErr)
	}

	// Drop any prior entry with our id; keep every other entry's exact bytes.
	kept := make([]json.RawMessage, 0, len(entries)+1)
	for _, e := range entries {
		if rawEntryHasID(e, studioExtID) {
			continue
		}
		kept = append(kept, e)
	}

	entry, err := json.Marshal(map[string]any{
		"identifier":       map[string]any{"id": studioExtID},
		"version":          studioExtVersion,
		"location":         map[string]any{"$mid": 1, "path": extDir, "scheme": "file"},
		"relativeLocation": studioExtDirName(),
	})
	if err != nil {
		return fmt.Errorf("marshal studio index entry: %w", err)
	}
	kept = append(kept, entry)

	out, err := json.MarshalIndent(kept, "", "\t")
	if err != nil {
		return fmt.Errorf("marshal extensions index: %w", err)
	}
	// Re-validate our own output parses as an array before touching disk.
	var check []json.RawMessage
	if err := json.Unmarshal(out, &check); err != nil {
		return fmt.Errorf("internal: produced invalid extensions index: %w", err)
	}

	if len(raw) > 0 {
		_ = os.WriteFile(indexPath+".zcp-bak", raw, 0o644) //nolint:gosec // G306: rolling backup mirrors the manifest's own perms
	}
	tmp, err := os.CreateTemp(extRoot, ".extensions-json-*")
	if err != nil {
		return fmt.Errorf("create temp index: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp index: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, indexPath); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename index into place: %w", err)
	}
	return nil
}

// rawEntryHasID reports whether a raw extensions.json entry's identifier.id
// equals id, without disturbing the rest of its bytes.
func rawEntryHasID(e json.RawMessage, id string) bool {
	var probe struct {
		Identifier struct {
			ID string `json:"id"`
		} `json:"identifier"`
	}
	if err := json.Unmarshal(e, &probe); err != nil {
		return false
	}
	return probe.Identifier.ID == id
}
